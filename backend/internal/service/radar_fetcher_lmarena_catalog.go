package service

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"time"
)

// RadarPublicModelCatalog is the passive, public-only catalog contract used by
// the LMArena background job. Implementations must not start upstream model
// discovery from this call.
type RadarPublicModelCatalog interface {
	ListPublicPassive(context.Context) (map[string][]string, error)
}

type catalogMatchedLMArenaFetcher struct {
	inner   RadarFetcher
	catalog RadarPublicModelCatalog
}

func newCatalogMatchedLMArenaFetcher(
	inner RadarFetcher,
	catalog RadarPublicModelCatalog,
) (RadarFetcher, error) {
	if isNilRadarFetcher(inner) {
		return nil, &RadarFetcherConfigError{Field: "lmarena_fetcher"}
	}
	if isNilRadarPublicModelCatalog(catalog) {
		return nil, &RadarFetcherConfigError{Field: "public_model_catalog"}
	}
	return &catalogMatchedLMArenaFetcher{inner: inner, catalog: catalog}, nil
}

func isNilRadarPublicModelCatalog(catalog RadarPublicModelCatalog) bool {
	if catalog == nil {
		return true
	}
	value := reflect.ValueOf(catalog)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (f *catalogMatchedLMArenaFetcher) Source() RadarSourceKey {
	return f.inner.Source()
}

func (f *catalogMatchedLMArenaFetcher) Interval() time.Duration {
	return f.inner.Interval()
}

func (f *catalogMatchedLMArenaFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	payload, meta, err := f.inner.Fetch(ctx)
	if err != nil {
		return payload, meta, err
	}

	byPlatform, err := f.catalog.ListPublicPassive(ctx)
	if err != nil {
		if cause := radarContextCause(ctx, err); cause != nil {
			return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, cause)
		}
		return radarFetchFailure(meta, DataSourceErrorCodeUpstreamError, nil)
	}

	snapshot, err := DecodeLMArena(payload)
	if err != nil {
		return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	dto, err := MapLMArena(snapshot)
	if err != nil {
		return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	matched := MatchLMArenaCatalog(dto, flattenRadarPublicModelCatalog(byPlatform))
	payload, err = encodeMatchedLMArena(matched)
	if err != nil {
		return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	return payload, meta, nil
}

func flattenRadarPublicModelCatalog(byPlatform map[string][]string) []string {
	platforms := make([]string, 0, len(byPlatform))
	for platform := range byPlatform {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	models := make([]string, 0)
	for _, platform := range platforms {
		for _, model := range byPlatform[platform] {
			if !IsPublicCatalogRoutingOnlyModelID(model) {
				models = append(models, model)
			}
		}
	}
	return normalizeCatalogModelIDs(models)
}

type matchedLMArenaEnvelope struct {
	Meta   matchedLMArenaMeta    `json:"meta"`
	Models []matchedLMArenaModel `json:"models"`
}

type matchedLMArenaMeta struct {
	FetchedAt   string  `json:"fetched_at"`
	LastUpdated *string `json:"last_updated"`
	ModelCount  int     `json:"model_count"`
}

type matchedLMArenaModel struct {
	Rank   int     `json:"rank"`
	Model  string  `json:"model"`
	Vendor *string `json:"vendor,omitempty"`
	Score  float64 `json:"score"`
	CI     float64 `json:"ci"`
	Votes  *int64  `json:"votes"`
}

func encodeMatchedLMArena(dto LMArenaDTO) ([]byte, error) {
	if dto.FetchedAt == nil || dto.FetchedAt.IsZero() {
		return nil, errInvalidLMArenaResponse
	}
	var lastUpdated *string
	if dto.LastUpdatedAt != nil {
		if dto.LastUpdatedAt.IsZero() {
			return nil, errInvalidLMArenaResponse
		}
		value := dto.LastUpdatedAt.UTC().Format(lmarenaLastUpdatedLayout)
		lastUpdated = &value
	}

	models := make([]matchedLMArenaModel, 0, len(dto.Leaderboard))
	for _, entry := range dto.Leaderboard {
		if entry.Elo == nil || entry.CILower == nil || entry.CIUpper == nil ||
			!isFiniteRadarFloat(*entry.Elo) || !isFiniteRadarFloat(*entry.CILower) || !isFiniteRadarFloat(*entry.CIUpper) ||
			*entry.CILower > *entry.Elo || *entry.CIUpper < *entry.Elo {
			return nil, errInvalidLMArenaResponse
		}
		ci := math.Max(*entry.Elo-*entry.CILower, *entry.CIUpper-*entry.Elo)
		if !isFiniteRadarFloat(ci) || ci < 0 {
			return nil, errInvalidLMArenaResponse
		}
		models = append(models, matchedLMArenaModel{
			Rank:   entry.Rank,
			Model:  entry.Model,
			Vendor: cloneRadarLMArenaCatalogString(entry.Vendor),
			Score:  *entry.Elo,
			CI:     ci,
			Votes:  cloneRadarLMArenaCatalogInt64(entry.Votes),
		})
	}

	envelope := matchedLMArenaEnvelope{
		Meta: matchedLMArenaMeta{
			FetchedAt:   dto.FetchedAt.UTC().Format(time.RFC3339Nano),
			LastUpdated: lastUpdated,
			ModelCount:  len(models),
		},
		Models: models,
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

var _ RadarFetcher = (*catalogMatchedLMArenaFetcher)(nil)
