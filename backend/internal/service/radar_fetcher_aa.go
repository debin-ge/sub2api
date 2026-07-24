package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	artificialAnalysisModelsURL = "https://artificialanalysis.ai/api/v2/language/models/free"
	// The overview renders at most six radar series. When operators configure
	// only an AA key, keep the automatic response bounded to that same size.
	artificialAnalysisAutoModelLimit = 6
	artificialAnalysisMaxPages       = 20
)

var errInvalidArtificialAnalysisModelsResponse = errors.New("invalid Artificial Analysis models response")

// ArtificialAnalysisModel is the normalized representation of one validated
// model record returned by the Artificial Analysis models endpoint. The
// decoder accepts both the current nested v2 response and the legacy flat
// response so already-cached payloads remain readable.
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

type artificialAnalysisModelsEnvelope struct {
	Data                     *[]artificialAnalysisModelWire    `json:"data"`
	IntelligenceIndexVersion *float64                          `json:"intelligence_index_version,omitempty"`
	Tier                     string                            `json:"tier,omitempty"`
	Pagination               *artificialAnalysisPaginationWire `json:"pagination,omitempty"`
}

type artificialAnalysisPaginationWire struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	TotalPages int  `json:"total_pages"`
	HasMore    bool `json:"has_more"`
}

type artificialAnalysisModelWire struct {
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

	ReleaseDate  string                              `json:"release_date"`
	ModelCreator *artificialAnalysisModelCreatorWire `json:"model_creator"`
	Evaluations  *artificialAnalysisEvaluationsWire  `json:"evaluations"`
	Pricing      *artificialAnalysisPricingWire      `json:"pricing"`
}

type artificialAnalysisModelCreatorWire struct {
	Name string `json:"name"`
}

type artificialAnalysisEvaluationsWire struct {
	IntelligenceIndex *float64 `json:"artificial_analysis_intelligence_index"`
	CodingIndex       *float64 `json:"artificial_analysis_coding_index"`
	AgenticIndex      *float64 `json:"artificial_analysis_agentic_index"`
}

type artificialAnalysisPricingWire struct {
	PriceInputPer1M  *float64 `json:"price_1m_input_tokens"`
	PriceOutputPer1M *float64 `json:"price_1m_output_tokens"`
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
	return &artificialAnalysisModelsFetcher{
		interval:         time.Duration(cfg.Radar.ArtificialAnalysisModelsIntervalMinutes) * time.Minute,
		client:           client,
		headers:          headers,
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		sleep:            radarSleepWithContext,
		now:              time.Now,
	}, nil
}

// artificialAnalysisModelsFetcher reads every page from the documented Free
// endpoint and returns one normalized snapshot only after the complete batch
// has validated. The runner therefore never replaces a good Redis snapshot
// with a partial page set.
type artificialAnalysisModelsFetcher struct {
	interval         time.Duration
	client           RadarHTTPDoer
	headers          http.Header
	maxResponseBytes int64
	sleep            RadarSleepFunc
	now              func() time.Time
}

func (f *artificialAnalysisModelsFetcher) Source() RadarSourceKey  { return RadarSourceAA }
func (f *artificialAnalysisModelsFetcher) Interval() time.Duration { return f.interval }

func (f *artificialAnalysisModelsFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var combined artificialAnalysisModelsEnvelope
	allModels := make([]artificialAnalysisModelWire, 0, 600)
	var latestMeta SourceFetchMeta
	expectedTotalPages := 0
	var aggregateResponseBytes int64
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s?page=%d", artificialAnalysisModelsURL, page)
		pageFetcher, err := newRadarHTTPFetcher(radarHTTPFetcherOptions{
			source: RadarSourceAA, interval: f.interval, client: f.client,
			endpoint: endpoint, headers: f.headers, maxResponseBytes: f.maxResponseBytes,
			sleep: f.sleep, now: f.now,
			validate: func(payload []byte) error {
				_, decodeErr := decodeArtificialAnalysisModelsPage(payload, page)
				return decodeErr
			},
		})
		if err != nil {
			return radarFetchFailure(latestMeta, DataSourceErrorCodeInvalidResponse, nil)
		}
		payload, meta, err := pageFetcher.Fetch(ctx)
		latestMeta = meta
		if err != nil {
			return nil, meta, err
		}
		if int64(len(payload)) > f.maxResponseBytes-aggregateResponseBytes {
			return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
		}
		aggregateResponseBytes += int64(len(payload))
		envelope, err := decodeArtificialAnalysisModelsPage(payload, page)
		if err != nil {
			return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
		}
		if page == 1 {
			combined.IntelligenceIndexVersion = envelope.IntelligenceIndexVersion
			combined.Tier = envelope.Tier
			expectedTotalPages = envelope.Pagination.TotalPages
		} else if !sameArtificialAnalysisSnapshot(envelope, combined, expectedTotalPages) {
			return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
		}
		allModels = append(allModels, (*envelope.Data)...)
		if !envelope.Pagination.HasMore {
			break
		}
	}
	combined.Data = &allModels
	combined.Pagination = &artificialAnalysisPaginationWire{
		Page: 1, PageSize: len(allModels), TotalPages: 1, HasMore: false,
	}
	payload, err := json.Marshal(combined)
	if err != nil || int64(len(payload)) > f.maxResponseBytes {
		return radarFetchFailure(latestMeta, DataSourceErrorCodeInvalidResponse, nil)
	}
	if _, err := DecodeArtificialAnalysisModels(payload); err != nil {
		return radarFetchFailure(latestMeta, DataSourceErrorCodeInvalidResponse, nil)
	}
	return payload, latestMeta, nil
}

func decodeArtificialAnalysisModelsPage(payload []byte, expectedPage int) (artificialAnalysisModelsEnvelope, error) {
	var envelope artificialAnalysisModelsEnvelope
	if !decodeArtificialAnalysisJSON(payload, &envelope) || envelope.Data == nil || envelope.Pagination == nil ||
		envelope.Tier != "free" || envelope.IntelligenceIndexVersion == nil ||
		envelope.Pagination.Page != expectedPage || envelope.Pagination.PageSize < 1 ||
		envelope.Pagination.TotalPages < 1 || envelope.Pagination.TotalPages > artificialAnalysisMaxPages ||
		expectedPage > envelope.Pagination.TotalPages ||
		envelope.Pagination.HasMore != (expectedPage < envelope.Pagination.TotalPages) ||
		len(*envelope.Data) == 0 || len(*envelope.Data) > envelope.Pagination.PageSize {
		return artificialAnalysisModelsEnvelope{}, errInvalidArtificialAnalysisModelsResponse
	}
	models := make([]ArtificialAnalysisModel, len(*envelope.Data))
	for i, wire := range *envelope.Data {
		models[i] = normalizeArtificialAnalysisModel(wire)
	}
	if err := validateArtificialAnalysisModels(models); err != nil {
		return artificialAnalysisModelsEnvelope{}, errInvalidArtificialAnalysisModelsResponse
	}
	return envelope, nil
}

func sameArtificialAnalysisSnapshot(page, first artificialAnalysisModelsEnvelope, totalPages int) bool {
	return page.Tier == first.Tier && page.IntelligenceIndexVersion != nil &&
		first.IntelligenceIndexVersion != nil && *page.IntelligenceIndexVersion == *first.IntelligenceIndexVersion &&
		page.Pagination != nil && page.Pagination.TotalPages == totalPages
}

func validateArtificialAnalysisFetcherConfig(cfg *config.Config, client RadarHTTPDoer) (string, error) {
	if cfg == nil {
		return "", &RadarFetcherConfigError{Field: "config"}
	}
	apiKey := strings.TrimSpace(cfg.Radar.ArtificialAnalysisAPIKey)
	if apiKey == "" {
		return "", &RadarFetcherConfigError{Field: "radar.artificial_analysis_api_key"}
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
	models, _, err := DecodeArtificialAnalysisSnapshot(payload)
	return models, err
}

// DecodeArtificialAnalysisSnapshot also exposes the documented index version.
// Legacy cached payloads remain readable and simply report a nil version.
func DecodeArtificialAnalysisSnapshot(payload []byte) ([]ArtificialAnalysisModel, *float64, error) {
	var envelope artificialAnalysisModelsEnvelope
	if !decodeArtificialAnalysisJSON(payload, &envelope) || envelope.Data == nil {
		return nil, nil, errInvalidArtificialAnalysisModelsResponse
	}
	wires := *envelope.Data
	models := make([]ArtificialAnalysisModel, len(wires))
	for index, wire := range wires {
		models[index] = normalizeArtificialAnalysisModel(wire)
	}
	if err := validateArtificialAnalysisModels(models); err != nil {
		return nil, nil, errInvalidArtificialAnalysisModelsResponse
	}

	result := make([]ArtificialAnalysisModel, len(models))
	copy(result, models)
	return result, cloneRadarFloat(envelope.IntelligenceIndexVersion), nil
}

func normalizeArtificialAnalysisModel(wire artificialAnalysisModelWire) ArtificialAnalysisModel {
	model := ArtificialAnalysisModel{
		Slug:              wire.Slug,
		Name:              wire.Name,
		Creator:           wire.Creator,
		ReleasedAt:        wire.ReleasedAt,
		IntelligenceIndex: wire.IntelligenceIndex,
		CodingIndex:       wire.CodingIndex,
		AgenticIndex:      wire.AgenticIndex,
		PriceInputPer1M:   wire.PriceInputPer1M,
		PriceOutputPer1M:  wire.PriceOutputPer1M,
		LastUpdatedAt:     wire.LastUpdatedAt,
	}
	if wire.ReleaseDate != "" {
		model.ReleasedAt = wire.ReleaseDate
	}
	if wire.ModelCreator != nil {
		model.Creator = wire.ModelCreator.Name
	}
	if wire.Evaluations != nil {
		model.IntelligenceIndex = wire.Evaluations.IntelligenceIndex
		model.CodingIndex = wire.Evaluations.CodingIndex
		model.AgenticIndex = wire.Evaluations.AgenticIndex
	}
	if wire.Pricing != nil {
		model.PriceInputPer1M = wire.Pricing.PriceInputPer1M
		model.PriceOutputPer1M = wire.Pricing.PriceOutputPer1M
	}
	return model
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
		CatalogMatches:    make([]DegradationCatalogMatchDTO, 0),
	}, nil
}

// MapArtificialAnalysisModels provides the bounded legacy fallback used only
// when no public catalog dependency is available. Production responses use
// MatchArtificialAnalysisCatalog instead.
func MapArtificialAnalysisModels(models []ArtificialAnalysisModel) ([]DegradationModelDTO, error) {
	if err := validateArtificialAnalysisModels(models); err != nil {
		return nil, errInvalidArtificialAnalysisModelsResponse
	}
	return mapAutomaticArtificialAnalysisModels(models)
}

func mapAutomaticArtificialAnalysisModels(models []ArtificialAnalysisModel) ([]DegradationModelDTO, error) {
	candidates := make([]ArtificialAnalysisModel, 0, len(models))
	for _, model := range models {
		if model.IntelligenceIndex == nil || model.CodingIndex == nil || model.AgenticIndex == nil {
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
