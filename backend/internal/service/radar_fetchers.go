package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// NewRadarFetchers builds the shared hardened Radar HTTP client and atomically
// assembles external fetchers in stable source order. The injected catalog is
// read passively by the LMArena background job before its result is committed.
func NewRadarFetchers(cfg *config.Config, catalog RadarPublicModelCatalog) ([]RadarFetcher, error) {
	client, err := NewRadarHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return newRadarFetchers(cfg, client, catalog)
}

// newRadarFetchers is the injectable assembly seam used by unit tests. Without
// an AA key, both AA models and performance sources are deliberately omitted so
// no unauthenticated request can be scheduled.
func newRadarFetchers(
	cfg *config.Config,
	client RadarHTTPDoer,
	catalog RadarPublicModelCatalog,
) ([]RadarFetcher, error) {
	if cfg == nil {
		return nil, &RadarFetcherConfigError{Field: "config"}
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, &RadarFetcherConfigError{Field: "radar"}
	}
	if isNilRadarHTTPDoer(client) {
		return nil, &RadarFetcherConfigError{Field: "http_client"}
	}
	if isNilRadarPublicModelCatalog(catalog) {
		return nil, &RadarFetcherConfigError{Field: "public_model_catalog"}
	}

	fetchers := make([]RadarFetcher, 0, 8+len(cfg.Radar.ArtificialAnalysisModelSlugs))
	if strings.TrimSpace(cfg.Radar.ArtificialAnalysisAPIKey) != "" {
		allowedSlugs, err := normalizeArtificialAnalysisAllowedSlugs(cfg.Radar.ArtificialAnalysisModelSlugs)
		if err != nil {
			return nil, err
		}

		modelsFetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
		if err != nil {
			return nil, err
		}
		fetchers = append(fetchers, modelsFetcher)
		for _, slug := range allowedSlugs {
			performanceFetcher, err := NewArtificialAnalysisPerformanceFetcher(cfg, slug, client)
			if err != nil {
				return nil, err
			}
			fetchers = append(fetchers, performanceFetcher)
		}
	}

	lmarenaSourceFetcher, err := NewLMArenaFetcher(cfg, client)
	if err != nil {
		return nil, err
	}
	lmarenaFetcher, err := newCatalogMatchedLMArenaFetcher(lmarenaSourceFetcher, catalog)
	if err != nil {
		return nil, err
	}
	fetchers = append(fetchers, lmarenaFetcher)

	claudeFetcher, err := NewStatuspageFetcher(cfg, RadarSourceStatusClaude, client)
	if err != nil {
		return nil, err
	}
	fetchers = append(fetchers, claudeFetcher)

	openAIFetcher, err := NewStatuspageFetcher(cfg, RadarSourceStatusOpenAI, client)
	if err != nil {
		return nil, err
	}
	fetchers = append(fetchers, openAIFetcher)

	for _, source := range []RadarSourceKey{
		RadarSourceStatusWindsurf,
		RadarSourceStatusKimi,
		RadarSourceStatusMiniMaxChina,
	} {
		fetcher, err := NewStatuspageFetcher(cfg, source, client)
		if err != nil {
			return nil, err
		}
		fetchers = append(fetchers, fetcher)
	}

	deepSeekFetcher, err := NewDeepSeekStatusFetcher(cfg, client)
	if err != nil {
		return nil, err
	}
	fetchers = append(fetchers, deepSeekFetcher)

	seen := make(map[RadarSourceKey]struct{}, len(fetchers))
	for _, fetcher := range fetchers {
		if fetcher == nil {
			return nil, &RadarFetcherConfigError{Field: "fetcher_sources"}
		}
		if _, duplicate := seen[fetcher.Source()]; duplicate {
			return nil, &RadarFetcherConfigError{Field: "fetcher_sources"}
		}
		seen[fetcher.Source()] = struct{}{}
	}
	return fetchers, nil
}
