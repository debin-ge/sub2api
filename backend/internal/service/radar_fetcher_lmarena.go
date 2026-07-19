package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	lmarenaLastUpdatedLayout    = "Jan 2, 2006"
	lmarenaHFPublishDateLayout  = "2006-01-02"
	lmarenaHFPageSize           = 100
	lmarenaHFMaxModels          = 10_000
	lmarenaHFMaxModelNameLength = 256
	lmarenaHFMaxVendorLength    = 256
	lmarenaHFDataset            = "lmarena-ai/leaderboard-dataset"
	lmarenaHFConfig             = "text_style_control"
	lmarenaHFSplit              = "latest"
	lmarenaHFFilter             = `"category" = 'overall'`
	lmarenaHFOrderBy            = `"rank" ASC`
)

var errInvalidLMArenaResponse = errors.New("invalid LMArena response")

// LMArenaWarningCode is a safe structural warning produced while decoding a
// usable leaderboard. Codes deliberately contain no upstream payload text.
type LMArenaWarningCode string

const (
	LMArenaWarningDuplicateRank      LMArenaWarningCode = "duplicate_rank"
	LMArenaWarningRankGap            LMArenaWarningCode = "rank_gap"
	LMArenaWarningModelCountMismatch LMArenaWarningCode = "model_count_mismatch"
)

// LMArenaModel is one validated row from the current LMArena feed.
type LMArenaModel struct {
	Rank   int
	Model  string
	Vendor string
	Score  float64
	CI     float64
	Votes  *int64
}

// LMArenaSnapshot is a validated, rank-sorted LMArena feed. FetchedAt and a
// present LastUpdatedAt are always normalized to UTC. The upstream feed may
// report an unknown last-updated date as null or by omitting the field.
type LMArenaSnapshot struct {
	FetchedAt     time.Time
	LastUpdatedAt *time.Time
	ModelCount    int
	Models        []LMArenaModel
	Warnings      []LMArenaWarningCode
}

type lmarenaEnvelopeWire struct {
	Meta   *lmarenaMetaWire    `json:"meta"`
	Models *[]lmarenaModelWire `json:"models"`
}

type lmarenaMetaWire struct {
	FetchedAt   string  `json:"fetched_at"`
	LastUpdated *string `json:"last_updated"`
	ModelCount  *int    `json:"model_count"`
}

type lmarenaModelWire struct {
	Rank   *int     `json:"rank"`
	Model  *string  `json:"model"`
	Vendor *string  `json:"vendor"`
	Score  *float64 `json:"score"`
	CI     *float64 `json:"ci"`
	Votes  *int64   `json:"votes"`
}

type lmarenaHFFeatureWire struct {
	Name string `json:"name"`
	Type struct {
		DType string `json:"dtype"`
		Type  string `json:"_type"`
	} `json:"type"`
}

type lmarenaHFPageWire struct {
	Features       *[]lmarenaHFFeatureWire `json:"features"`
	Rows           *[]lmarenaHFRowWire     `json:"rows"`
	NumRowsTotal   *int                    `json:"num_rows_total"`
	NumRowsPerPage *int                    `json:"num_rows_per_page"`
	Partial        *bool                   `json:"partial"`
}

type lmarenaHFRowWire struct {
	RowIndex       *int                `json:"row_idx"`
	Row            *lmarenaHFModelWire `json:"row"`
	TruncatedCells *[]json.RawMessage  `json:"truncated_cells"`
}

type lmarenaHFModelWire struct {
	ModelName              *string  `json:"model_name"`
	Organization           *string  `json:"organization"`
	License                *string  `json:"license"`
	Rating                 *float64 `json:"rating"`
	RatingLower            *float64 `json:"rating_lower"`
	RatingUpper            *float64 `json:"rating_upper"`
	Variance               *float64 `json:"variance"`
	VoteCount              *float64 `json:"vote_count"`
	Rank                   *int     `json:"rank"`
	Category               *string  `json:"category"`
	LeaderboardPublishDate *string  `json:"leaderboard_publish_date"`
}

type lmarenaHFModel struct {
	RowIndex    int
	Rank        int
	Model       string
	Vendor      string
	Rating      float64
	RatingLower float64
	RatingUpper float64
	Votes       int64
	PublishedAt time.Time
}

type lmarenaFetcher struct {
	interval         time.Duration
	client           RadarHTTPDoer
	endpoint         string
	maxResponseBytes int64
	now              func() time.Time
}

type lmarenaHeaderCapturingDoer struct {
	base        RadarHTTPDoer
	revision    string
	contentType string
}

func (d *lmarenaHeaderCapturingDoer) Do(req *http.Request) (*http.Response, error) {
	response, err := d.base.Do(req)
	if response != nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		d.revision = strings.TrimSpace(response.Header.Get("X-Revision"))
		d.contentType = strings.TrimSpace(response.Header.Get("Content-Type"))
	}
	return response, err
}

// NewLMArenaFetcher constructs the official, complete LMArena leaderboard
// source. Dataset identity, split, category, ordering, and pagination are fixed
// in code; config can only select the pinned Hugging Face datasets-server host.
func NewLMArenaFetcher(cfg *config.Config, client RadarHTTPDoer) (RadarFetcher, error) {
	if cfg == nil {
		return nil, &RadarFetcherConfigError{Field: "config"}
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, &RadarFetcherConfigError{Field: "radar"}
	}
	if isNilRadarHTTPDoer(client) {
		return nil, &RadarFetcherConfigError{Field: "http_client"}
	}

	return &lmarenaFetcher{
		interval:         time.Duration(cfg.Radar.LMArenaIntervalMinutes) * time.Minute,
		client:           client,
		endpoint:         strings.TrimSpace(cfg.Radar.LMArenaURL),
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		now:              time.Now,
	}, nil
}

func (f *lmarenaFetcher) Source() RadarSourceKey { return RadarSourceLMArena }

func (f *lmarenaFetcher) Interval() time.Duration { return f.interval }

func (f *lmarenaFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	expectedRevision := ""
	expectedTotal := -1
	expectedFeatureSignature := ""
	models := make([]lmarenaHFModel, 0)
	var aggregateResponseBytes int64
	var resultMeta SourceFetchMeta

	for offset := 0; expectedTotal < 0 || offset < expectedTotal; offset += lmarenaHFPageSize {
		endpoint, err := lmarenaHFPageURL(f.endpoint, offset)
		if err != nil {
			return radarFetchFailure(resultMeta, DataSourceErrorCodeInvalidResponse, nil)
		}
		capturingClient := &lmarenaHeaderCapturingDoer{base: f.client}
		pageFetcher, err := newRadarHTTPFetcher(radarHTTPFetcherOptions{
			source:           RadarSourceLMArena,
			interval:         f.interval,
			client:           capturingClient,
			endpoint:         endpoint,
			headers:          http.Header{"Accept": []string{"application/json"}},
			maxResponseBytes: f.maxResponseBytes,
			validate: func(payload []byte) error {
				_, _, err := decodeAndValidateLMArenaHFPage(payload, offset)
				return err
			},
			now: f.now,
		})
		if err != nil {
			return radarFetchFailure(resultMeta, DataSourceErrorCodeInvalidResponse, nil)
		}

		payload, pageMeta, err := pageFetcher.Fetch(ctx)
		resultMeta = pageMeta
		if err != nil {
			return nil, pageMeta, err
		}
		if int64(len(payload)) > f.maxResponseBytes-aggregateResponseBytes {
			return radarFetchFailure(pageMeta, DataSourceErrorCodeInvalidResponse, nil)
		}
		aggregateResponseBytes += int64(len(payload))
		if !isLMArenaHFJSONContentType(capturingClient.contentType) || !isLMArenaHFRevision(capturingClient.revision) {
			return radarFetchFailure(pageMeta, DataSourceErrorCodeInvalidResponse, nil)
		}
		page, signature, err := decodeAndValidateLMArenaHFPage(payload, offset)
		if err != nil {
			return radarFetchFailure(pageMeta, DataSourceErrorCodeInvalidResponse, nil)
		}
		if expectedRevision == "" {
			expectedRevision = capturingClient.revision
			expectedTotal = *page.NumRowsTotal
			expectedFeatureSignature = signature
			models = make([]lmarenaHFModel, 0, expectedTotal)
		} else if capturingClient.revision != expectedRevision || *page.NumRowsTotal != expectedTotal || signature != expectedFeatureSignature {
			return radarFetchFailure(pageMeta, DataSourceErrorCodeInvalidResponse, nil)
		}

		for _, row := range *page.Rows {
			model, err := normalizeLMArenaHFRow(row)
			if err != nil {
				return radarFetchFailure(pageMeta, DataSourceErrorCodeInvalidResponse, nil)
			}
			models = append(models, model)
		}
	}

	if len(models) != expectedTotal || resultMeta.LastSuccessAt == nil {
		return radarFetchFailure(resultMeta, DataSourceErrorCodeInvalidResponse, nil)
	}
	payload, err := canonicalLMArenaPayload(models, *resultMeta.LastSuccessAt)
	if err != nil {
		return radarFetchFailure(resultMeta, DataSourceErrorCodeInvalidResponse, nil)
	}
	return payload, resultMeta, nil
}

func lmarenaHFPageURL(base string, offset int) (string, error) {
	endpoint, err := url.Parse(base)
	if err != nil {
		return "", errInvalidLMArenaResponse
	}
	query := endpoint.Query()
	query.Set("dataset", lmarenaHFDataset)
	query.Set("config", lmarenaHFConfig)
	query.Set("split", lmarenaHFSplit)
	query.Set("where", lmarenaHFFilter)
	query.Set("orderby", lmarenaHFOrderBy)
	query.Set("offset", strconv.Itoa(offset))
	query.Set("length", strconv.Itoa(lmarenaHFPageSize))
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func decodeAndValidateLMArenaHFPage(payload []byte, offset int) (lmarenaHFPageWire, string, error) {
	var page lmarenaHFPageWire
	if !decodeLMArenaJSON(payload, &page) || page.Features == nil || page.Rows == nil ||
		page.NumRowsTotal == nil || page.NumRowsPerPage == nil || page.Partial == nil || *page.Partial ||
		*page.NumRowsTotal <= 0 || *page.NumRowsTotal > lmarenaHFMaxModels ||
		*page.NumRowsPerPage != lmarenaHFPageSize || offset < 0 || offset >= *page.NumRowsTotal {
		return lmarenaHFPageWire{}, "", errInvalidLMArenaResponse
	}
	signature, err := lmarenaHFFeatureSignature(*page.Features)
	if err != nil {
		return lmarenaHFPageWire{}, "", errInvalidLMArenaResponse
	}
	expectedRows := lmarenaHFPageSize
	if remaining := *page.NumRowsTotal - offset; remaining < expectedRows {
		expectedRows = remaining
	}
	if len(*page.Rows) != expectedRows {
		return lmarenaHFPageWire{}, "", errInvalidLMArenaResponse
	}
	for _, row := range *page.Rows {
		if _, err := normalizeLMArenaHFRow(row); err != nil {
			return lmarenaHFPageWire{}, "", errInvalidLMArenaResponse
		}
	}
	return page, signature, nil
}

func lmarenaHFFeatureSignature(features []lmarenaHFFeatureWire) (string, error) {
	required := map[string]string{
		"model_name": "string", "organization": "string", "license": "string", "rating": "float64",
		"rating_lower": "float64", "rating_upper": "float64", "vote_count": "float64",
		"variance": "float64", "rank": "int64", "category": "string", "leaderboard_publish_date": "string",
	}
	seen := make(map[string]string, len(features))
	for _, feature := range features {
		name := strings.TrimSpace(feature.Name)
		if name == "" || feature.Type.Type != "Value" {
			return "", errInvalidLMArenaResponse
		}
		if _, duplicate := seen[name]; duplicate {
			return "", errInvalidLMArenaResponse
		}
		seen[name] = feature.Type.DType
	}
	keys := make([]string, 0, len(seen))
	for name, dtype := range required {
		if seen[name] != dtype {
			return "", errInvalidLMArenaResponse
		}
	}
	for name := range seen {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var signature strings.Builder
	for _, key := range keys {
		signature.WriteString(key)
		signature.WriteByte('=')
		signature.WriteString(seen[key])
		signature.WriteByte(';')
	}
	return signature.String(), nil
}

func normalizeLMArenaHFRow(wire lmarenaHFRowWire) (lmarenaHFModel, error) {
	if wire.RowIndex == nil || *wire.RowIndex < 0 || wire.Row == nil || wire.TruncatedCells == nil || len(*wire.TruncatedCells) != 0 {
		return lmarenaHFModel{}, errInvalidLMArenaResponse
	}
	row := wire.Row
	if row.ModelName == nil || row.Organization == nil || row.License == nil || row.Rating == nil || row.RatingLower == nil ||
		row.RatingUpper == nil || row.Variance == nil || row.VoteCount == nil || row.Rank == nil || row.Category == nil || row.LeaderboardPublishDate == nil {
		return lmarenaHFModel{}, errInvalidLMArenaResponse
	}
	model := strings.TrimSpace(*row.ModelName)
	vendor := strings.TrimSpace(*row.Organization)
	license := strings.TrimSpace(*row.License)
	if model == "" || len(model) > lmarenaHFMaxModelNameLength || len(vendor) > lmarenaHFMaxVendorLength ||
		license == "" || len(license) > lmarenaHFMaxVendorLength ||
		strings.IndexFunc(model, unicode.IsControl) >= 0 || strings.IndexFunc(vendor, unicode.IsControl) >= 0 || strings.IndexFunc(license, unicode.IsControl) >= 0 ||
		*row.Rank <= 0 || strings.TrimSpace(*row.Category) != "overall" ||
		!isFiniteRadarFloat(*row.Rating) || !isFiniteRadarFloat(*row.RatingLower) || !isFiniteRadarFloat(*row.RatingUpper) ||
		*row.RatingLower > *row.Rating || *row.Rating > *row.RatingUpper ||
		!isFiniteRadarFloat(*row.Variance) || *row.Variance < 0 ||
		!isFiniteRadarFloat(*row.VoteCount) || *row.VoteCount < 0 || *row.VoteCount != math.Trunc(*row.VoteCount) ||
		*row.VoteCount >= float64(math.MaxInt64) {
		return lmarenaHFModel{}, errInvalidLMArenaResponse
	}
	publishedAt, err := time.Parse(lmarenaHFPublishDateLayout, *row.LeaderboardPublishDate)
	if err != nil || publishedAt.IsZero() || publishedAt.Format(lmarenaHFPublishDateLayout) != *row.LeaderboardPublishDate {
		return lmarenaHFModel{}, errInvalidLMArenaResponse
	}
	return lmarenaHFModel{
		RowIndex: *wire.RowIndex, Rank: *row.Rank, Model: model, Vendor: vendor,
		Rating: *row.Rating, RatingLower: *row.RatingLower, RatingUpper: *row.RatingUpper,
		Votes: int64(*row.VoteCount), PublishedAt: publishedAt.UTC(),
	}, nil
}

func canonicalLMArenaPayload(models []lmarenaHFModel, fetchedAt time.Time) ([]byte, error) {
	if len(models) == 0 || fetchedAt.IsZero() {
		return nil, errInvalidLMArenaResponse
	}
	models = append([]lmarenaHFModel(nil), models...)
	sort.Slice(models, func(i, j int) bool { return models[i].Rank < models[j].Rank })
	seenRows := make(map[int]struct{}, len(models))
	seenRanks := make(map[int]struct{}, len(models))
	seenModels := make(map[string]struct{}, len(models))
	publishedAt := models[0].PublishedAt
	wireModels := make([]lmarenaModelWire, 0, len(models))
	for index, model := range models {
		if model.Rank != index+1 || !model.PublishedAt.Equal(publishedAt) {
			return nil, errInvalidLMArenaResponse
		}
		modelKey := strings.ToLower(model.Model)
		if _, duplicate := seenRows[model.RowIndex]; duplicate {
			return nil, errInvalidLMArenaResponse
		}
		if _, duplicate := seenRanks[model.Rank]; duplicate {
			return nil, errInvalidLMArenaResponse
		}
		if _, duplicate := seenModels[modelKey]; duplicate {
			return nil, errInvalidLMArenaResponse
		}
		seenRows[model.RowIndex] = struct{}{}
		seenRanks[model.Rank] = struct{}{}
		seenModels[modelKey] = struct{}{}
		rank, name, vendor, score, ci, votes := model.Rank, model.Model, model.Vendor, model.Rating, (model.RatingUpper-model.RatingLower)/2, model.Votes
		wireModels = append(wireModels, lmarenaModelWire{Rank: &rank, Model: &name, Vendor: &vendor, Score: &score, CI: &ci, Votes: &votes})
	}
	modelCount := len(wireModels)
	lastUpdated := publishedAt.Format(lmarenaLastUpdatedLayout)
	envelope := lmarenaEnvelopeWire{
		Meta:   &lmarenaMetaWire{FetchedAt: fetchedAt.UTC().Format(time.RFC3339Nano), LastUpdated: &lastUpdated, ModelCount: &modelCount},
		Models: &wireModels,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, errInvalidLMArenaResponse
	}
	if _, err := DecodeLMArena(payload); err != nil {
		return nil, errInvalidLMArenaResponse
	}
	return payload, nil
}

func isLMArenaHFRevision(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

func isLMArenaHFJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

var _ RadarFetcher = (*lmarenaFetcher)(nil)

// DecodeLMArena validates the current meta/models schema and returns a stable
// rank sort. Duplicate ranks, rank gaps, and model-count drift are usable but
// surfaced as structured warning codes.
func DecodeLMArena(payload []byte) (LMArenaSnapshot, error) {
	var envelope lmarenaEnvelopeWire
	if !decodeLMArenaJSON(payload, &envelope) || envelope.Meta == nil || envelope.Models == nil {
		return LMArenaSnapshot{}, errInvalidLMArenaResponse
	}
	if envelope.Meta.ModelCount == nil || *envelope.Meta.ModelCount < 0 {
		return LMArenaSnapshot{}, errInvalidLMArenaResponse
	}

	fetchedAt, err := time.Parse(time.RFC3339Nano, envelope.Meta.FetchedAt)
	if err != nil || fetchedAt.IsZero() {
		return LMArenaSnapshot{}, errInvalidLMArenaResponse
	}
	var lastUpdatedAt *time.Time
	if envelope.Meta.LastUpdated != nil {
		parsed, err := time.Parse(lmarenaLastUpdatedLayout, *envelope.Meta.LastUpdated)
		if err != nil || parsed.IsZero() || parsed.Format(lmarenaLastUpdatedLayout) != *envelope.Meta.LastUpdated {
			return LMArenaSnapshot{}, errInvalidLMArenaResponse
		}
		normalized := parsed.UTC()
		lastUpdatedAt = &normalized
	}

	wireModels := *envelope.Models
	models := make([]LMArenaModel, 0, len(wireModels))
	for _, wireModel := range wireModels {
		model, err := validateAndNormalizeLMArenaModel(wireModel)
		if err != nil {
			return LMArenaSnapshot{}, errInvalidLMArenaResponse
		}
		models = append(models, model)
	}
	if _, err := lmarenaTotalVotes(models); err != nil {
		return LMArenaSnapshot{}, errInvalidLMArenaResponse
	}
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].Rank < models[j].Rank
	})

	warnings := lmarenaStructuralWarnings(models, *envelope.Meta.ModelCount)
	return LMArenaSnapshot{
		FetchedAt:     fetchedAt.UTC(),
		LastUpdatedAt: lastUpdatedAt,
		ModelCount:    *envelope.Meta.ModelCount,
		Models:        models,
		Warnings:      warnings,
	}, nil
}

// MapLMArena maps a validated snapshot to the public DTO contract. CI is a
// symmetric radius in the current feed, so public bounds are score-ci and
// score+ci. The returned pointers never alias the input snapshot.
func MapLMArena(snapshot LMArenaSnapshot) (LMArenaDTO, error) {
	if snapshot.FetchedAt.IsZero() || snapshot.ModelCount < 0 ||
		snapshot.LastUpdatedAt != nil && snapshot.LastUpdatedAt.IsZero() {
		return LMArenaDTO{}, errInvalidLMArenaResponse
	}

	models := append([]LMArenaModel(nil), snapshot.Models...)
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].Rank < models[j].Rank
	})
	leaderboard := make([]LMArenaEntryDTO, 0, len(models))
	for _, model := range models {
		if err := validateLMArenaModel(model); err != nil {
			return LMArenaDTO{}, errInvalidLMArenaResponse
		}

		score := model.Score
		ciLower := model.Score - model.CI
		ciUpper := model.Score + model.CI
		var vendor *string
		if model.Vendor != "" {
			value := model.Vendor
			vendor = &value
		}
		leaderboard = append(leaderboard, LMArenaEntryDTO{
			Rank:    model.Rank,
			Model:   model.Model,
			Vendor:  vendor,
			Elo:     &score,
			CILower: &ciLower,
			CIUpper: &ciUpper,
			Votes:   cloneRadarInt64(model.Votes),
		})
	}
	totalVotes, err := lmarenaTotalVotes(models)
	if err != nil {
		return LMArenaDTO{}, errInvalidLMArenaResponse
	}

	var lastUpdatedAt *time.Time
	if snapshot.LastUpdatedAt != nil {
		normalized := snapshot.LastUpdatedAt.UTC()
		lastUpdatedAt = &normalized
	}
	fetchedAt := snapshot.FetchedAt.UTC()
	return LMArenaDTO{
		Leaderboard:   leaderboard,
		TotalVotes:    totalVotes,
		LastUpdatedAt: lastUpdatedAt,
		FetchedAt:     &fetchedAt,
		Stale:         false,
	}, nil
}

func decodeLMArenaJSON(payload []byte, destination any) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	var trailing json.RawMessage
	return decoder.Decode(&trailing) == io.EOF
}

func validateAndNormalizeLMArenaModel(wireModel lmarenaModelWire) (LMArenaModel, error) {
	if wireModel.Rank == nil || wireModel.Model == nil || wireModel.Score == nil || wireModel.CI == nil {
		return LMArenaModel{}, errInvalidLMArenaResponse
	}
	vendor := ""
	if wireModel.Vendor != nil {
		vendor = strings.TrimSpace(*wireModel.Vendor)
	}
	model := LMArenaModel{
		Rank:   *wireModel.Rank,
		Model:  strings.TrimSpace(*wireModel.Model),
		Vendor: vendor,
		Score:  *wireModel.Score,
		CI:     *wireModel.CI,
		Votes:  cloneRadarInt64(wireModel.Votes),
	}
	if err := validateLMArenaModel(model); err != nil {
		return LMArenaModel{}, errInvalidLMArenaResponse
	}
	return model, nil
}

func validateLMArenaModel(model LMArenaModel) error {
	if model.Rank <= 0 || strings.TrimSpace(model.Model) == "" {
		return errInvalidLMArenaResponse
	}
	if !isFiniteRadarFloat(model.Score) || !isFiniteRadarFloat(model.CI) || model.CI < 0 ||
		model.Votes != nil && *model.Votes < 0 {
		return errInvalidLMArenaResponse
	}
	if !isFiniteRadarFloat(model.Score-model.CI) || !isFiniteRadarFloat(model.Score+model.CI) {
		return errInvalidLMArenaResponse
	}
	return nil
}

func lmarenaTotalVotes(models []LMArenaModel) (*int64, error) {
	for _, model := range models {
		if model.Votes == nil {
			return nil, nil
		}
	}

	var total int64
	for _, model := range models {
		if *model.Votes > math.MaxInt64-total {
			return nil, errInvalidLMArenaResponse
		}
		total += *model.Votes
	}
	return &total, nil
}

func cloneRadarInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func lmarenaStructuralWarnings(models []LMArenaModel, modelCount int) []LMArenaWarningCode {
	duplicateRank := false
	rankGap := false
	previousRank := 0
	for _, model := range models {
		switch {
		case model.Rank == previousRank:
			duplicateRank = true
		case model.Rank != previousRank+1:
			rankGap = true
		}
		if model.Rank > previousRank {
			previousRank = model.Rank
		}
	}

	warnings := make([]LMArenaWarningCode, 0, 3)
	if duplicateRank {
		warnings = append(warnings, LMArenaWarningDuplicateRank)
	}
	if rankGap {
		warnings = append(warnings, LMArenaWarningRankGap)
	}
	if modelCount != len(models) {
		warnings = append(warnings, LMArenaWarningModelCountMismatch)
	}
	return warnings
}
