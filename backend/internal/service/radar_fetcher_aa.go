package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	artificialAnalysisModelsURL      = "https://artificialanalysis.ai/api/v2/data/llms/models"
	artificialAnalysisPerformanceURL = "https://artificialanalysis.ai/api/v2/language/models"
	artificialAnalysisWindow         = "90d"
	artificialAnalysisInterval       = "daily"
	// The overview renders at most six radar series. When operators configure
	// only an AA key, keep the automatic response bounded to that same size.
	artificialAnalysisAutoModelLimit = 6
)

var (
	errInvalidArtificialAnalysisModelsResponse      = errors.New("invalid Artificial Analysis models response")
	errInvalidArtificialAnalysisPerformanceResponse = errors.New("invalid Artificial Analysis performance response")
)

// ArtificialAnalysisModel is one validated model record returned by the
// Artificial Analysis models endpoint. released_at is a date or RFC3339;
// last_updated_at is RFC3339 and is normalized when mapped to public DTOs.
type ArtificialAnalysisModel struct {
	Slug              string   `json:"slug"`
	Name              string   `json:"name"`
	Creator           string   `json:"creator"`
	ReleasedAt        string   `json:"released_at"`
	IntelligenceIndex *float64 `json:"intelligence_index"`
	CodingIndex       *float64 `json:"coding_index"`
	AgenticIndex      *float64 `json:"agentic_index"`
	PriceInputPer1M   *float64 `json:"price_input_per_1m"`
	PriceOutputPer1M  *float64 `json:"price_output_per_1m"`
	LastUpdatedAt     string   `json:"last_updated_at"`
}

// ArtificialAnalysisPerformancePoint is one validated daily metric record.
type ArtificialAnalysisPerformancePoint struct {
	Date              string   `json:"date"`
	IntelligenceIndex *float64 `json:"intelligence_index"`
	CodingIndex       *float64 `json:"coding_index"`
	AgenticIndex      *float64 `json:"agentic_index"`
}

// ArtificialAnalysisPerformance is one validated 90-day daily series.
type ArtificialAnalysisPerformance struct {
	ModelSlug  string                               `json:"model_slug"`
	Window     string                               `json:"window"`
	Interval   string                               `json:"interval"`
	DataPoints []ArtificialAnalysisPerformancePoint `json:"data_points"`
}

type artificialAnalysisModelsEnvelope struct {
	Data *[]ArtificialAnalysisModel `json:"data"`
}

type artificialAnalysisPerformanceWire struct {
	ModelSlug  string                                `json:"model_slug"`
	Window     string                                `json:"window"`
	Interval   string                                `json:"interval"`
	DataPoints *[]ArtificialAnalysisPerformancePoint `json:"data_points"`
}

// NewArtificialAnalysisModelsFetcher constructs the AA models source. An
// empty key is a configuration error so assembly code can explicitly skip the
// source instead of accidentally issuing unauthenticated traffic.
func NewArtificialAnalysisModelsFetcher(cfg *config.Config, client RadarHTTPDoer) (RadarFetcher, error) {
	apiKey, err := validateArtificialAnalysisFetcherConfig(cfg, client)
	if err != nil {
		return nil, err
	}

	headers := make(http.Header)
	headers.Set("x-api-key", apiKey)
	return newRadarHTTPFetcher(radarHTTPFetcherOptions{
		source:           RadarSourceAA,
		interval:         time.Duration(cfg.Radar.ArtificialAnalysisModelsIntervalMinutes) * time.Minute,
		client:           client,
		endpoint:         artificialAnalysisModelsURL,
		headers:          headers,
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		validate: func(payload []byte) error {
			_, err := DecodeArtificialAnalysisModels(payload)
			return err
		},
	})
}

// NewArtificialAnalysisPerformanceFetcher constructs one AA daily performance
// source. The normalized slug must be present in the configured allowlist and
// is validated before it is escaped into the fixed trusted endpoint.
func NewArtificialAnalysisPerformanceFetcher(cfg *config.Config, modelSlug string, client RadarHTTPDoer) (RadarFetcher, error) {
	canonicalModelSlug := strings.TrimSpace(modelSlug)
	source, err := RadarAAPerformanceSource(canonicalModelSlug)
	if err != nil {
		return nil, err
	}
	apiKey, err := validateArtificialAnalysisFetcherConfig(cfg, client)
	if err != nil {
		return nil, err
	}
	allowedSlugs, err := normalizeArtificialAnalysisAllowedSlugs(cfg.Radar.ArtificialAnalysisModelSlugs)
	if err != nil {
		return nil, err
	}
	if !containsArtificialAnalysisSlug(allowedSlugs, canonicalModelSlug) {
		return nil, &RadarFetcherConfigError{Field: "radar.artificial_analysis_model_slugs"}
	}

	headers := make(http.Header)
	headers.Set("x-api-key", apiKey)
	endpoint := fmt.Sprintf(
		"%s/%s/performance?window=%s&interval=%s",
		artificialAnalysisPerformanceURL,
		url.PathEscape(canonicalModelSlug),
		artificialAnalysisWindow,
		artificialAnalysisInterval,
	)
	return newRadarHTTPFetcher(radarHTTPFetcherOptions{
		source:           source,
		interval:         time.Duration(cfg.Radar.ArtificialAnalysisPerformanceIntervalMinutes) * time.Minute,
		client:           client,
		endpoint:         endpoint,
		headers:          headers,
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		validate: func(payload []byte) error {
			_, err := DecodeArtificialAnalysisPerformance(payload, canonicalModelSlug)
			return err
		},
	})
}

func validateArtificialAnalysisFetcherConfig(cfg *config.Config, client RadarHTTPDoer) (string, error) {
	if cfg == nil {
		return "", &RadarFetcherConfigError{Field: "config"}
	}
	apiKey := strings.TrimSpace(cfg.Radar.ArtificialAnalysisAPIKey)
	if apiKey == "" {
		return "", &RadarFetcherConfigError{Field: "radar.artificial_analysis_api_key"}
	}
	if _, err := normalizeArtificialAnalysisAllowedSlugs(cfg.Radar.ArtificialAnalysisModelSlugs); err != nil {
		return "", err
	}
	if err := cfg.Radar.Validate(); err != nil {
		return "", &RadarFetcherConfigError{Field: "radar"}
	}
	if isNilRadarHTTPDoer(client) {
		return "", &RadarFetcherConfigError{Field: "http_client"}
	}
	return apiKey, nil
}

// DecodeArtificialAnalysisModels parses and validates the documented models
// wrapper. It rejects ambiguous duplicate slugs and never includes payload
// contents in returned errors.
func DecodeArtificialAnalysisModels(payload []byte) ([]ArtificialAnalysisModel, error) {
	var envelope artificialAnalysisModelsEnvelope
	if !decodeArtificialAnalysisJSON(payload, &envelope) || envelope.Data == nil {
		return nil, errInvalidArtificialAnalysisModelsResponse
	}
	models := *envelope.Data
	if err := validateArtificialAnalysisModels(models); err != nil {
		return nil, errInvalidArtificialAnalysisModelsResponse
	}

	result := make([]ArtificialAnalysisModel, len(models))
	copy(result, models)
	return result, nil
}

func mapArtificialAnalysisModel(model ArtificialAnalysisModel) (DegradationModelDTO, error) {
	if err := validateArtificialAnalysisModel(model); err != nil {
		return DegradationModelDTO{}, errInvalidArtificialAnalysisModelsResponse
	}
	lastUpdatedAt, err := normalizedArtificialAnalysisTimestamp(model.LastUpdatedAt)
	if err != nil {
		return DegradationModelDTO{}, errInvalidArtificialAnalysisModelsResponse
	}

	return DegradationModelDTO{
		Slug:              model.Slug,
		Name:              model.Name,
		Vendor:            model.Creator,
		IntelligenceIndex: cloneRadarFloat(model.IntelligenceIndex),
		CodingIndex:       cloneRadarFloat(model.CodingIndex),
		AgenticIndex:      cloneRadarFloat(model.AgenticIndex),
		PriceInputPer1M:   cloneRadarFloat(model.PriceInputPer1M),
		PriceOutputPer1M:  cloneRadarFloat(model.PriceOutputPer1M),
		LastUpdatedAt:     lastUpdatedAt,
	}, nil
}

// MapArtificialAnalysisModels maps configured AA models to public degradation
// DTOs. Configuration order is authoritative when an allowlist is present. An
// empty allowlist selects a bounded, deterministic set of the strongest models
// that actually contain index data, so configuring only the API key cannot
// silently produce an empty overview. Optional metrics remain nil.
func MapArtificialAnalysisModels(models []ArtificialAnalysisModel, allowedSlugs []string) ([]DegradationModelDTO, error) {
	if err := validateArtificialAnalysisModels(models); err != nil {
		return nil, errInvalidArtificialAnalysisModelsResponse
	}
	canonicalAllowedSlugs, err := normalizeArtificialAnalysisAllowedSlugs(allowedSlugs)
	if err != nil {
		return nil, err
	}
	if len(canonicalAllowedSlugs) == 0 {
		return mapAutomaticArtificialAnalysisModels(models)
	}
	modelsBySlug := make(map[string]ArtificialAnalysisModel, len(models))
	for _, model := range models {
		modelsBySlug[model.Slug] = model
	}

	result := make([]DegradationModelDTO, 0, len(canonicalAllowedSlugs))
	for _, slug := range canonicalAllowedSlugs {
		model, present := modelsBySlug[slug]
		if !present {
			continue
		}
		mapped, err := mapArtificialAnalysisModel(model)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func mapAutomaticArtificialAnalysisModels(models []ArtificialAnalysisModel) ([]DegradationModelDTO, error) {
	candidates := make([]ArtificialAnalysisModel, 0, len(models))
	for _, model := range models {
		if model.IntelligenceIndex == nil && model.CodingIndex == nil && model.AgenticIndex == nil {
			continue
		}
		candidates = append(candidates, model)
	}
	sort.Slice(candidates, func(i, j int) bool {
		leftScore := artificialAnalysisModelIndexScore(candidates[i])
		rightScore := artificialAnalysisModelIndexScore(candidates[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		leftRelease := artificialAnalysisModelReleaseTime(candidates[i])
		rightRelease := artificialAnalysisModelReleaseTime(candidates[j])
		if !leftRelease.Equal(rightRelease) {
			return leftRelease.After(rightRelease)
		}
		return candidates[i].Slug < candidates[j].Slug
	})
	if len(candidates) > artificialAnalysisAutoModelLimit {
		candidates = candidates[:artificialAnalysisAutoModelLimit]
	}

	result := make([]DegradationModelDTO, 0, len(candidates))
	for _, model := range candidates {
		mapped, err := mapArtificialAnalysisModel(model)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func artificialAnalysisModelIndexScore(model ArtificialAnalysisModel) float64 {
	var total float64
	var count int
	for _, value := range []*float64{model.IntelligenceIndex, model.CodingIndex, model.AgenticIndex} {
		if value != nil {
			total += *value
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func artificialAnalysisModelReleaseTime(model ArtificialAnalysisModel) time.Time {
	if model.ReleasedAt == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse("2006-01-02", model.ReleasedAt); err == nil {
		return parsed.UTC()
	}
	parsed, _ := time.Parse(time.RFC3339Nano, model.ReleasedAt)
	return parsed.UTC()
}

func normalizeArtificialAnalysisAllowedSlugs(configuredSlugs []string) ([]string, error) {
	result := make([]string, 0, len(configuredSlugs))
	seen := make(map[string]struct{}, len(configuredSlugs))
	for _, configuredSlug := range configuredSlugs {
		canonicalSlug := strings.TrimSpace(configuredSlug)
		if _, err := RadarAAPerformanceSource(canonicalSlug); err != nil {
			return nil, &RadarFetcherConfigError{Field: "radar.artificial_analysis_model_slugs"}
		}
		if _, duplicate := seen[canonicalSlug]; duplicate {
			continue
		}
		seen[canonicalSlug] = struct{}{}
		result = append(result, canonicalSlug)
	}
	return result, nil
}

func containsArtificialAnalysisSlug(slugs []string, target string) bool {
	for _, slug := range slugs {
		if slug == target {
			return true
		}
	}
	return false
}

// DecodeArtificialAnalysisPerformance parses and validates a model's fixed
// 90-day daily response. The payload slug must match the configured source.
func DecodeArtificialAnalysisPerformance(payload []byte, expectedModelSlug string) (ArtificialAnalysisPerformance, error) {
	if !isSafeRadarModelSlug(expectedModelSlug) {
		return ArtificialAnalysisPerformance{}, errInvalidArtificialAnalysisPerformanceResponse
	}

	var wire artificialAnalysisPerformanceWire
	if !decodeArtificialAnalysisJSON(payload, &wire) || wire.DataPoints == nil {
		return ArtificialAnalysisPerformance{}, errInvalidArtificialAnalysisPerformanceResponse
	}
	if wire.ModelSlug != expectedModelSlug || wire.Window != artificialAnalysisWindow || wire.Interval != artificialAnalysisInterval {
		return ArtificialAnalysisPerformance{}, errInvalidArtificialAnalysisPerformanceResponse
	}

	points := *wire.DataPoints
	seenDates := make(map[string]struct{}, len(points))
	for _, point := range points {
		if !validArtificialAnalysisDate(point.Date) {
			return ArtificialAnalysisPerformance{}, errInvalidArtificialAnalysisPerformanceResponse
		}
		if _, duplicate := seenDates[point.Date]; duplicate {
			return ArtificialAnalysisPerformance{}, errInvalidArtificialAnalysisPerformanceResponse
		}
		seenDates[point.Date] = struct{}{}
		if !validArtificialAnalysisIndex(point.IntelligenceIndex) ||
			!validArtificialAnalysisIndex(point.CodingIndex) ||
			!validArtificialAnalysisIndex(point.AgenticIndex) {
			return ArtificialAnalysisPerformance{}, errInvalidArtificialAnalysisPerformanceResponse
		}
	}

	resultPoints := make([]ArtificialAnalysisPerformancePoint, len(points))
	copy(resultPoints, points)
	return ArtificialAnalysisPerformance{
		ModelSlug:  wire.ModelSlug,
		Window:     wire.Window,
		Interval:   wire.Interval,
		DataPoints: resultPoints,
	}, nil
}

func decodeArtificialAnalysisJSON(payload []byte, destination any) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	var trailing json.RawMessage
	return decoder.Decode(&trailing) == io.EOF
}

func validateArtificialAnalysisModels(models []ArtificialAnalysisModel) error {
	seenSlugs := make(map[string]struct{}, len(models))
	for _, model := range models {
		if err := validateArtificialAnalysisModel(model); err != nil {
			return err
		}
		if _, duplicate := seenSlugs[model.Slug]; duplicate {
			return errInvalidArtificialAnalysisModelsResponse
		}
		seenSlugs[model.Slug] = struct{}{}
	}
	return nil
}

func validateArtificialAnalysisModel(model ArtificialAnalysisModel) error {
	if !isSafeRadarModelSlug(model.Slug) || strings.TrimSpace(model.Name) == "" {
		return errInvalidArtificialAnalysisModelsResponse
	}
	if !validArtificialAnalysisReleasedAt(model.ReleasedAt) || !validArtificialAnalysisTimestamp(model.LastUpdatedAt) {
		return errInvalidArtificialAnalysisModelsResponse
	}
	if !validArtificialAnalysisIndex(model.IntelligenceIndex) ||
		!validArtificialAnalysisIndex(model.CodingIndex) ||
		!validArtificialAnalysisIndex(model.AgenticIndex) {
		return errInvalidArtificialAnalysisModelsResponse
	}
	if !validArtificialAnalysisPrice(model.PriceInputPer1M) || !validArtificialAnalysisPrice(model.PriceOutputPer1M) {
		return errInvalidArtificialAnalysisModelsResponse
	}
	return nil
}

func validArtificialAnalysisReleasedAt(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	if validArtificialAnalysisDate(value) {
		return true
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func validArtificialAnalysisTimestamp(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func normalizedArtificialAnalysisTimestamp(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, errInvalidArtificialAnalysisModelsResponse
	}
	utc := parsed.UTC()
	return &utc, nil
}

func validArtificialAnalysisDate(value string) bool {
	if len(value) != len("2006-01-02") {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func validArtificialAnalysisIndex(value *float64) bool {
	return value == nil || isFiniteRadarFloat(*value) && *value >= 0 && *value <= 100
}

func validArtificialAnalysisPrice(value *float64) bool {
	return value == nil || isFiniteRadarFloat(*value) && *value >= 0
}

func isFiniteRadarFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func cloneRadarFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
