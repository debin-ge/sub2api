package service

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/net/html"
)

const (
	claudeStatuspageAPIURL           = "https://status.claude.com/api/v2/summary.json"
	openAIStatuspageAPIURL           = "https://status.openai.com/api/v2/summary.json"
	windsurfStatuspageAPIURL         = "https://status.windsurf.com/api/v2/summary.json"
	kimiStatuspageAPIURL             = "https://status.moonshot.cn/api/v2/summary.json"
	miniMaxGlobalStatuspageAPIURL    = "https://status.minimax.io/api/v2/summary.json"
	miniMaxChinaStatuspageAPIURL     = "https://status.minimaxi.com/api/v2/summary.json"
	claudeStatuspageIncidentsURL     = "https://status.claude.com/api/v2/incidents.json"
	openAIStatuspageIncidentsURL     = "https://status.openai.com/api/v2/incidents.json"
	windsurfStatuspageIncidentsURL   = "https://status.windsurf.com/api/v2/incidents.json"
	kimiStatuspageIncidentsURL       = "https://status.moonshot.cn/api/v2/incidents.json"
	miniMaxGlobalIncidentsURL        = "https://status.minimax.io/api/v2/incidents.json"
	miniMaxChinaIncidentsURL         = "https://status.minimaxi.com/api/v2/incidents.json"
	openAIStatuspageFeedURL          = "https://status.openai.com/feed.atom"
	openAIStatusSummaryURL           = "https://status.openai.com/proxy/openai-1"
	openAIComponentImpactsURL        = "https://status.openai.com/proxy/openai-1/component_impacts"
	claudeAPIHistoryURL              = "https://status.claude.com/history?filter=k8w3r06qmzrp"
	claudeCodeHistoryURL             = "https://status.claude.com/history?filter=yyzkbfz2thpt"
	windsurfHistoryURL               = "https://status.windsurf.com/history?filter=8q19cygxvshj,r5wf1ykd7y1m"
	kimiHistoryURL                   = "https://status.moonshot.cn/history?filter=8psr5dfdld0s,8rkd3yj051gl,lk7q3z0fcylp,p1j9ttb7jwhp,rf64wcbxt3r2,wmn9wzv84k1v,x0zsqgy57b75,z2zfp65lvb2z"
	miniMaxGlobalLLMHistoryURL       = "https://status.minimax.io/history?filter=pr0d8qr59svt"
	miniMaxChinaLLMHistoryURL        = "https://status.minimaxi.com/history?filter=vwp8mgy34fck"
	claudeStatuspagePublicURL        = "https://status.claude.com"
	openAIStatuspagePublicURL        = "https://status.openai.com"
	windsurfStatuspagePublicURL      = "https://status.windsurf.com"
	deepSeekStatuspagePublicURL      = "https://status.deepseek.com"
	kimiStatuspagePublicURL          = "https://status.moonshot.cn"
	miniMaxGlobalStatuspagePublicURL = "https://status.minimax.io"
	miniMaxChinaStatuspagePublicURL  = "https://status.minimaxi.com"
	miniMaxChinaLLMComponentID       = "vwp8mgy34fck"
	miniMaxChinaLLMComponentName     = "大语言模型LLM"
)

const (
	serviceHealthHistoryDays       = 30
	statuspageIncidentHistoryLimit = 50
	openAIIncidentHistoryLimit     = 25
	openAIStatusFeedMaxEntries     = 500
	statuspageHistoryMaxMonths     = 6
	statuspageHistoryMaxIncidents  = 500
)

var errInvalidStatuspageResponse = errors.New("invalid Statuspage response")

var (
	statuspageHistoryTimestampPattern     = regexp.MustCompile(`^([A-Z][a-z]{2}) <var data-var='date'>([0-9]{1,2})</var>, <var data-var='time'>([0-9]{2}:[0-9]{2})</var> - (?:(?:([A-Z][a-z]{2}) <var data-var='date'>([0-9]{1,2})</var>, )?)<var data-var='time'>([0-9]{2}:[0-9]{2})</var> (UTC|CST)$`)
	statuspageHistoryOpenTimestampPattern = regexp.MustCompile(`^([A-Z][a-z]{2}) <var data-var='date'>([0-9]{1,2})</var>, <var data-var='time'>([0-9]{2}:[0-9]{2})</var> (UTC|CST)$`)
)

var (
	claudeAPIStatuspageAliases = map[string]struct{}{
		"claude api":                     {},
		"claude api (api.anthropic.com)": {},
	}
	claudeCodeStatuspageAliases = map[string]struct{}{
		"claude code": {},
	}
	codexWebStatuspageAliases = map[string]struct{}{
		"codex web":                {},
		"chatgpt codex":            {},
		"codex in chatgpt desktop": {},
	}
	openAIAPIStatuspageAliases = map[string]struct{}{
		"api":                  {},
		"apis":                 {},
		"openai api":           {},
		"codex api":            {},
		"responses":            {},
		"responses api":        {},
		"batch":                {},
		"batch api":            {},
		"audio":                {},
		"audio api":            {},
		"embeddings":           {},
		"embeddings api":       {},
		"moderations":          {},
		"moderations api":      {},
		"files":                {},
		"files api":            {},
		"fine-tuning":          {},
		"fine-tuning api":      {},
		"fine tuning":          {},
		"fine tuning api":      {},
		"chat completions":     {},
		"chat completions api": {},
		"completions":          {},
		"completions api":      {},
		"assistants":           {},
		"assistants api":       {},
		"images":               {},
		"images api":           {},
		"image generation":     {},
		"realtime":             {},
		"realtime api":         {},
		"uploads":              {},
		"uploads api":          {},
		"compliance api":       {},
		"ads api":              {},
	}
	windsurfStatuspageAliases = map[string]struct{}{
		"cascade":      {},
		"windsurf tab": {},
	}
	deepSeekStatuspageAliases = map[string]struct{}{
		"api service":          {},
		"api 服务 (api service)": {},
	}
	kimiStatuspageAliases = map[string]struct{}{
		"open api":       {},
		"api service":    {},
		"model":          {},
		"vision model":   {},
		"thinking model": {},
		"text model":     {},
		"research model": {},
		"k2 model":       {},
	}
	miniMaxGlobalStatuspageAliases = map[string]struct{}{
		"large language models (llm)": {},
	}
	miniMaxChinaStatuspageAliases = map[string]struct{}{
		"大语言模型llm": {},
	}
)

func statuspageRadarSources() []RadarSourceKey {
	return []RadarSourceKey{
		RadarSourceStatusClaude,
		RadarSourceStatusOpenAI,
		RadarSourceStatusWindsurf,
		RadarSourceStatusDeepSeek,
		RadarSourceStatusKimi,
		RadarSourceStatusMiniMaxChina,
	}
}

func statuspageSourceDescriptor(source RadarSourceKey) (string, string, []RadarServiceDescriptor, bool) {
	switch source {
	case RadarSourceStatusClaude:
		return claudeStatuspageAPIURL, claudeStatuspagePublicURL, CanonicalRadarServices()[:2], true
	case RadarSourceStatusOpenAI:
		return openAIStatuspageAPIURL, openAIStatuspagePublicURL, CanonicalRadarServices()[2:], true
	case RadarSourceStatusWindsurf:
		return windsurfStatuspageAPIURL, windsurfStatuspagePublicURL, []RadarServiceDescriptor{{Key: ServiceKeyWindsurf, Name: "Windsurf"}}, true
	case RadarSourceStatusKimi:
		return kimiStatuspageAPIURL, kimiStatuspagePublicURL, []RadarServiceDescriptor{{Key: ServiceKeyKimi, Name: "Kimi"}}, true
	case RadarSourceStatusMiniMaxGlobal:
		return miniMaxGlobalStatuspageAPIURL, miniMaxGlobalStatuspagePublicURL, []RadarServiceDescriptor{{Key: ServiceKeyMiniMax, Name: "MiniMax"}}, true
	case RadarSourceStatusMiniMaxChina:
		return miniMaxChinaStatuspageAPIURL, miniMaxChinaStatuspagePublicURL, []RadarServiceDescriptor{{Key: ServiceKeyMiniMax, Name: "MiniMax"}}, true
	case RadarSourceStatusDeepSeek:
		return "", deepSeekStatuspagePublicURL, []RadarServiceDescriptor{{Key: ServiceKeyDeepSeek, Name: "DeepSeek"}}, true
	default:
		return "", "", nil, false
	}
}

func statuspageIncidentsURL(source RadarSourceKey) (string, bool) {
	switch source {
	case RadarSourceStatusClaude:
		return claudeStatuspageIncidentsURL, true
	case RadarSourceStatusOpenAI:
		return openAIStatuspageIncidentsURL, true
	case RadarSourceStatusWindsurf:
		return windsurfStatuspageIncidentsURL, true
	case RadarSourceStatusKimi:
		return kimiStatuspageIncidentsURL, true
	case RadarSourceStatusMiniMaxGlobal:
		return miniMaxGlobalIncidentsURL, true
	case RadarSourceStatusMiniMaxChina:
		return miniMaxChinaIncidentsURL, true
	default:
		return "", false
	}
}

func statuspageCalendarSpecs(source RadarSourceKey) []statuspageCalendarSpec {
	switch source {
	case RadarSourceStatusClaude:
		return []statuspageCalendarSpec{
			{serviceKey: ServiceKeyClaudeAPI, endpoint: claudeAPIHistoryURL, componentIDs: []string{"k8w3r06qmzrp"}},
			{serviceKey: ServiceKeyClaudeCode, endpoint: claudeCodeHistoryURL, componentIDs: []string{"yyzkbfz2thpt"}},
		}
	case RadarSourceStatusWindsurf:
		return []statuspageCalendarSpec{{
			serviceKey: ServiceKeyWindsurf, endpoint: windsurfHistoryURL,
			componentIDs: []string{"8q19cygxvshj", "r5wf1ykd7y1m"},
		}}
	case RadarSourceStatusKimi:
		return []statuspageCalendarSpec{{
			serviceKey: ServiceKeyKimi, endpoint: kimiHistoryURL,
			componentIDs: []string{"8psr5dfdld0s", "8rkd3yj051gl", "lk7q3z0fcylp", "p1j9ttb7jwhp", "rf64wcbxt3r2", "wmn9wzv84k1v", "x0zsqgy57b75", "z2zfp65lvb2z"},
		}}
	case RadarSourceStatusMiniMaxGlobal:
		return []statuspageCalendarSpec{{
			serviceKey: ServiceKeyMiniMax, endpoint: miniMaxGlobalLLMHistoryURL, componentIDs: []string{"pr0d8qr59svt"},
		}}
	case RadarSourceStatusMiniMaxChina:
		return []statuspageCalendarSpec{{
			serviceKey: ServiceKeyMiniMax, endpoint: miniMaxChinaLLMHistoryURL, componentIDs: []string{miniMaxChinaLLMComponentID},
		}}
	default:
		return nil
	}
}

func statuspageSourcesForServiceKey(serviceKey ServiceKey) []RadarSourceKey {
	switch serviceKey {
	case ServiceKeyClaudeAPI, ServiceKeyClaudeCode:
		return []RadarSourceKey{RadarSourceStatusClaude}
	case ServiceKeyOpenAIAPI, ServiceKeyCodexWeb:
		return []RadarSourceKey{RadarSourceStatusOpenAI}
	case ServiceKeyWindsurf:
		return []RadarSourceKey{RadarSourceStatusWindsurf}
	case ServiceKeyDeepSeek:
		return []RadarSourceKey{RadarSourceStatusDeepSeek}
	case ServiceKeyKimi:
		return []RadarSourceKey{RadarSourceStatusKimi}
	case ServiceKeyMiniMax:
		return []RadarSourceKey{RadarSourceStatusMiniMaxChina}
	default:
		return nil
	}
}

// StatuspageSummary is the validated subset of a Statuspage v2 summary used
// by Radar. All timestamps are normalized to UTC.
type StatuspageSummary struct {
	Page                 StatuspagePage
	Status               StatuspageOverallStatus
	Components           []StatuspageComponent
	Incidents            []StatuspageIncident
	HistoryCoverageStart *time.Time
	ComponentImpacts     []StatuspageComponentImpact
	ComponentHistory     []StatuspageIncident
}

type StatuspagePage struct {
	ID        string
	Name      string
	URL       string
	UpdatedAt time.Time
}

type StatuspageOverallStatus struct {
	Indicator   string
	Description string
}

type StatuspageComponent struct {
	ID        string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Group     bool
}

type StatuspageIncident struct {
	ID         string
	Name       string
	Status     string
	Impact     string
	CreatedAt  time.Time
	ResolvedAt *time.Time
	Components []StatuspageIncidentComponent
}

type StatuspageIncidentComponent struct {
	ID   string
	Name string
}

type StatuspageComponentImpact struct {
	ID              string
	ComponentID     string
	ComponentName   string
	ComponentGroups []string
	IncidentID      string
	IncidentName    string
	IncidentStatus  string
	StartAt         time.Time
	EndAt           *time.Time
	Status          ServiceStatus
}

type statuspageSummaryWire struct {
	Page                      *statuspagePageWire        `json:"page"`
	Status                    *statuspageOverallWire     `json:"status"`
	Components                *[]statuspageComponentWire `json:"components"`
	Incidents                 []statuspageIncidentWire   `json:"incidents"`
	RadarHistoryCoverageStart *string                    `json:"radar_history_coverage_start,omitempty"`
	RadarComponentImpacts     *[]statuspageImpactWire    `json:"radar_component_impacts,omitempty"`
	RadarComponentHistory     *[]statuspageIncidentWire  `json:"radar_component_history,omitempty"`
}

type statuspageIncidentsWire struct {
	Page      *statuspagePageWire       `json:"page"`
	Incidents *[]statuspageIncidentWire `json:"incidents"`
}

type statuspagePageWire struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
}

type statuspageOverallWire struct {
	Indicator   string `json:"indicator"`
	Description string `json:"description"`
}

type statuspageComponentWire struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Group     bool   `json:"group"`
}

type statuspageIncidentWire struct {
	ID         string                            `json:"id"`
	Name       string                            `json:"name"`
	Status     string                            `json:"status"`
	Impact     string                            `json:"impact"`
	CreatedAt  string                            `json:"created_at"`
	ResolvedAt *string                           `json:"resolved_at"`
	Components []statuspageIncidentComponentWire `json:"components"`
}

type statuspageIncidentComponentWire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type statuspageImpactWire struct {
	ID              string   `json:"id"`
	ComponentID     string   `json:"component_id"`
	ComponentName   string   `json:"component_name,omitempty"`
	ComponentGroups []string `json:"component_groups,omitempty"`
	IncidentID      string   `json:"status_page_incident_id"`
	IncidentName    string   `json:"incident_name"`
	IncidentStatus  string   `json:"incident_status"`
	StartAt         string   `json:"start_at"`
	EndAt           *string  `json:"end_at,omitempty"`
	Status          string   `json:"status"`
}

type openAIComponentImpactsWire struct {
	ComponentImpacts   *[]statuspageImpactWire        `json:"component_impacts"`
	IncidentLinks      *[]openAIComponentIncidentLink `json:"incident_links"`
	RadarCoverageStart *string                        `json:"radar_coverage_start,omitempty"`
	RadarCoverageEnd   *string                        `json:"radar_coverage_end,omitempty"`
}

type openAIComponentIncidentLink struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	PublishedAt string `json:"published_at"`
}

type openAIStatusSummaryEnvelopeWire struct {
	Summary *openAIStatusSummaryWire `json:"summary"`
}

type openAIStatusSummaryWire struct {
	Components *[]openAIStatusComponentWire `json:"components"`
	Structure  *openAIStatusStructureWire   `json:"structure"`
}

type openAIStatusComponentWire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type openAIStatusStructureWire struct {
	Items *[]openAIStatusStructureItemWire `json:"items"`
}

type openAIStatusStructureItemWire struct {
	Group     *openAIStatusGroupWire          `json:"group"`
	Component *openAIStatusGroupComponentWire `json:"component"`
}

type openAIStatusGroupWire struct {
	ID         string                            `json:"id"`
	Name       string                            `json:"name"`
	Components *[]openAIStatusGroupComponentWire `json:"components"`
}

type openAIStatusGroupComponentWire struct {
	ComponentID string `json:"component_id"`
	Name        string `json:"name"`
}

type openAIStatusCatalogComponent struct {
	Name   string
	Groups []string
}

type statuspageCalendarBundleWire struct {
	CoverageStart string                   `json:"coverage_start"`
	CoverageEnd   string                   `json:"coverage_end"`
	Incidents     []statuspageIncidentWire `json:"incidents"`
}

type statuspageCalendarBundle struct {
	CoverageStart time.Time
	CoverageEnd   time.Time
	Incidents     []statuspageIncidentWire
}

type statuspageCalendarSpec struct {
	serviceKey   ServiceKey
	endpoint     string
	componentIDs []string
}

type statuspageHistoryFetcher struct {
	source    RadarSourceKey
	interval  time.Duration
	summary   RadarFetcher
	incidents RadarFetcher
	auxiliary RadarFetcher
	timeline  RadarFetcher
	catalog   RadarFetcher
	calendar  RadarFetcher
}

type openAIComponentImpactsFetcher struct {
	interval         time.Duration
	client           RadarHTTPDoer
	maxResponseBytes int64
	now              func() time.Time
}

type statuspageCalendarFetcher struct {
	source           RadarSourceKey
	interval         time.Duration
	client           RadarHTTPDoer
	maxResponseBytes int64
	specs            []statuspageCalendarSpec
}

// NewStatuspageFetcher constructs one of the allowlisted Statuspage JSON
// fetchers. Endpoints are fixed HTTPS URLs and never taken from upstream data.
func NewStatuspageFetcher(cfg *config.Config, source RadarSourceKey, client RadarHTTPDoer) (RadarFetcher, error) {
	if cfg == nil {
		return nil, &RadarFetcherConfigError{Field: "config"}
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, &RadarFetcherConfigError{Field: "radar"}
	}
	if isNilRadarHTTPDoer(client) {
		return nil, &RadarFetcherConfigError{Field: "http_client"}
	}

	endpoint, _, _, ok := statuspageSourceDescriptor(source)
	incidentsEndpoint, incidentsOK := statuspageIncidentsURL(source)
	if !ok || endpoint == "" || !incidentsOK || incidentsEndpoint == "" {
		return nil, &RadarFetcherConfigError{Field: "statuspage_source"}
	}

	interval := time.Duration(cfg.Radar.StatuspageIntervalMinutes) * time.Minute
	summary, err := newRadarHTTPFetcher(radarHTTPFetcherOptions{
		source:           source,
		interval:         interval,
		client:           client,
		endpoint:         endpoint,
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		validate: func(payload []byte) error {
			_, err := DecodeStatuspageSummary(payload)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	incidents, err := newRadarHTTPFetcher(radarHTTPFetcherOptions{
		source:           source,
		interval:         interval,
		client:           client,
		endpoint:         incidentsEndpoint,
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		validate: func(payload []byte) error {
			_, _, err := decodeStatuspageIncidents(payload)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	var auxiliary RadarFetcher
	var timeline RadarFetcher
	var catalog RadarFetcher
	var calendar RadarFetcher
	switch source {
	case RadarSourceStatusOpenAI:
		auxiliary, err = newRadarHTTPFetcher(radarHTTPFetcherOptions{
			source: source, interval: interval, client: client, endpoint: openAIStatuspageFeedURL,
			maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
			validate: func(payload []byte) error {
				_, err := decodeOpenAIStatusFeed(payload)
				return err
			},
		})
		timeline = &openAIComponentImpactsFetcher{
			interval: interval, client: client, maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes, now: time.Now,
		}
		catalog, err = newRadarHTTPFetcher(radarHTTPFetcherOptions{
			source: source, interval: interval, client: client, endpoint: openAIStatusSummaryURL,
			maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
			validate: func(payload []byte) error {
				_, err := decodeOpenAIStatusCatalog(payload)
				return err
			},
		})
	}
	if err != nil {
		return nil, err
	}
	if specs := statuspageCalendarSpecs(source); len(specs) > 0 {
		calendar = &statuspageCalendarFetcher{
			source: source, interval: interval, client: client,
			maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes, specs: specs,
		}
	}
	return &statuspageHistoryFetcher{
		source: source, interval: interval, summary: summary, incidents: incidents, auxiliary: auxiliary, timeline: timeline, catalog: catalog, calendar: calendar,
	}, nil
}

func (f *openAIComponentImpactsFetcher) Source() RadarSourceKey { return RadarSourceStatusOpenAI }

func (f *openAIComponentImpactsFetcher) Interval() time.Duration { return f.interval }

func (f *openAIComponentImpactsFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	now := time.Now().UTC()
	if f.now != nil {
		now = f.now().UTC()
	}
	start := utcDayStart(now).AddDate(0, 0, -(serviceHealthHistoryDays - 1))
	end := utcDayStart(now).AddDate(0, 0, 1)
	endpoint, err := url.Parse(openAIComponentImpactsURL)
	if err != nil {
		return nil, SourceFetchMeta{}, &RadarFetcherConfigError{Field: "openai_component_impacts_url"}
	}
	query := endpoint.Query()
	query.Set("start_at", start.Format(time.RFC3339Nano))
	query.Set("end_at", end.Format(time.RFC3339Nano))
	endpoint.RawQuery = query.Encode()
	fetcher, err := newRadarHTTPFetcher(radarHTTPFetcherOptions{
		source: RadarSourceStatusOpenAI, interval: f.interval, client: f.client, endpoint: endpoint.String(),
		maxResponseBytes: f.maxResponseBytes,
		validate: func(payload []byte) error {
			_, err := decodeOpenAIComponentImpacts(payload, nil)
			return err
		},
	})
	if err != nil {
		return nil, SourceFetchMeta{}, err
	}
	payload, meta, err := fetcher.Fetch(ctx)
	if err != nil {
		return nil, meta, err
	}
	var wire openAIComponentImpactsWire
	if !decodeStatuspageJSON(payload, &wire) {
		return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	coverageStart := start.Format(time.RFC3339Nano)
	coverageEnd := end.Format(time.RFC3339Nano)
	wire.RadarCoverageStart = &coverageStart
	wire.RadarCoverageEnd = &coverageEnd
	normalized, err := json.Marshal(wire)
	if err != nil {
		return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	return normalized, meta, nil
}

func (f *statuspageCalendarFetcher) Source() RadarSourceKey { return f.source }

func (f *statuspageCalendarFetcher) Interval() time.Duration { return f.interval }

func (f *statuspageCalendarFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	if len(f.specs) == 0 {
		return nil, SourceFetchMeta{}, &RadarFetcherConfigError{Field: "statuspage_calendar"}
	}
	type result struct {
		incidents     []StatuspageIncident
		coverageStart time.Time
		coverageEnd   time.Time
		meta          SourceFetchMeta
		err           error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, len(f.specs))
	for _, spec := range f.specs {
		spec := spec
		go func() {
			fetcher, err := newRadarHTTPFetcher(radarHTTPFetcherOptions{
				source: f.source, interval: f.interval, client: f.client, endpoint: spec.endpoint,
				maxResponseBytes: f.maxResponseBytes,
				validate: func(payload []byte) error {
					_, _, _, err := decodeStatuspageCalendarHistory(payload, f.source, spec.serviceKey, spec.componentIDs)
					return err
				},
			})
			if err != nil {
				results <- result{err: err}
				return
			}
			payload, meta, err := fetcher.Fetch(ctx)
			if err != nil {
				results <- result{meta: meta, err: err}
				return
			}
			incidents, coverageStart, coverageEnd, err := decodeStatuspageCalendarHistory(payload, f.source, spec.serviceKey, spec.componentIDs)
			results <- result{incidents: incidents, coverageStart: coverageStart, coverageEnd: coverageEnd, meta: meta, err: err}
		}()
	}
	var meta SourceFetchMeta
	var coverageStart time.Time
	var coverageEnd time.Time
	incidents := make([]StatuspageIncident, 0)
	for range f.specs {
		result := <-results
		if result.err != nil {
			cancel()
			return nil, result.meta, result.err
		}
		if meta.LastAttemptAt.IsZero() || result.meta.LastAttemptAt.After(meta.LastAttemptAt) {
			meta = result.meta
		}
		if coverageStart.IsZero() || result.coverageStart.After(coverageStart) {
			coverageStart = result.coverageStart
		}
		if coverageEnd.IsZero() || result.coverageEnd.Before(coverageEnd) {
			coverageEnd = result.coverageEnd
		}
		var err error
		incidents, err = mergeStatuspageCalendarIncidents(incidents, result.incidents)
		if err != nil {
			return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
		}
	}
	bundle := statuspageCalendarBundleWire{
		CoverageStart: coverageStart.UTC().Format(time.RFC3339Nano),
		CoverageEnd:   coverageEnd.UTC().Format(time.RFC3339Nano),
		Incidents:     encodeStatuspageIncidents(incidents),
	}
	payload, err := json.Marshal(bundle)
	if err != nil {
		return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	return payload, meta, nil
}

func (f *statuspageHistoryFetcher) Source() RadarSourceKey { return f.source }

func (f *statuspageHistoryFetcher) Interval() time.Duration { return f.interval }

func (f *statuspageHistoryFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		payload []byte
		meta    SourceFetchMeta
		err     error
	}
	summaryResult := make(chan result, 1)
	incidentResult := make(chan result, 1)
	auxiliaryResult := make(chan result, 1)
	timelineResult := make(chan result, 1)
	catalogResult := make(chan result, 1)
	calendarResult := make(chan result, 1)
	go func() {
		payload, meta, err := f.summary.Fetch(ctx)
		summaryResult <- result{payload: payload, meta: meta, err: err}
	}()
	go func() {
		payload, meta, err := f.incidents.Fetch(ctx)
		incidentResult <- result{payload: payload, meta: meta, err: err}
	}()
	if f.auxiliary != nil {
		go func() {
			payload, meta, err := f.auxiliary.Fetch(ctx)
			auxiliaryResult <- result{payload: payload, meta: meta, err: err}
		}()
	}
	if f.timeline != nil {
		go func() {
			payload, meta, err := f.timeline.Fetch(ctx)
			timelineResult <- result{payload: payload, meta: meta, err: err}
		}()
	}
	if f.catalog != nil {
		go func() {
			payload, meta, err := f.catalog.Fetch(ctx)
			catalogResult <- result{payload: payload, meta: meta, err: err}
		}()
	}
	if f.calendar != nil {
		go func() {
			payload, meta, err := f.calendar.Fetch(ctx)
			calendarResult <- result{payload: payload, meta: meta, err: err}
		}()
	}
	summary := <-summaryResult
	if summary.err != nil {
		cancel()
	}
	incidents := <-incidentResult
	if summary.err != nil {
		return nil, summary.meta, summary.err
	}
	if incidents.err != nil {
		return nil, incidents.meta, incidents.err
	}
	var auxiliary result
	if f.auxiliary != nil {
		auxiliary = <-auxiliaryResult
		if auxiliary.err != nil {
			return nil, auxiliary.meta, auxiliary.err
		}
	}
	var timeline result
	if f.timeline != nil {
		timeline = <-timelineResult
		if timeline.err != nil {
			return nil, timeline.meta, timeline.err
		}
	}
	var catalog result
	if f.catalog != nil {
		catalog = <-catalogResult
		if catalog.err != nil {
			return nil, catalog.meta, catalog.err
		}
	}
	var calendar result
	if f.calendar != nil {
		calendar = <-calendarResult
		if calendar.err != nil {
			return nil, calendar.meta, calendar.err
		}
	}
	merged, err := mergeStatuspageHistoryPayloads(f.source, summary.payload, incidents.payload, auxiliary.payload, timeline.payload, catalog.payload, calendar.payload)
	if err != nil {
		return radarFetchFailure(summary.meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	return merged, summary.meta, nil
}

var _ RadarFetcher = (*statuspageHistoryFetcher)(nil)

// DecodeStatuspageSummary parses the common Statuspage v2 schema. Components
// are required (an empty array is valid); incidents are optional because the
// current OpenAI response omits them.
func DecodeStatuspageSummary(payload []byte) (StatuspageSummary, error) {
	var wire statuspageSummaryWire
	if !decodeStatuspageJSON(payload, &wire) || wire.Page == nil || wire.Status == nil || wire.Components == nil {
		return StatuspageSummary{}, errInvalidStatuspageResponse
	}

	pageUpdatedAt, err := parseStatuspageTimestamp(wire.Page.UpdatedAt)
	if err != nil || strings.TrimSpace(wire.Page.ID) == "" || strings.TrimSpace(wire.Page.Name) == "" {
		return StatuspageSummary{}, errInvalidStatuspageResponse
	}
	if strings.TrimSpace(wire.Status.Indicator) == "" || strings.TrimSpace(wire.Status.Description) == "" {
		return StatuspageSummary{}, errInvalidStatuspageResponse
	}

	components := make([]StatuspageComponent, 0, len(*wire.Components))
	seenComponentIDs := make(map[string]struct{}, len(*wire.Components))
	for _, componentWire := range *wire.Components {
		component, err := decodeStatuspageComponent(componentWire)
		if err != nil {
			return StatuspageSummary{}, errInvalidStatuspageResponse
		}
		normalizedID := normalizeStatuspageComponentID(component.ID)
		if _, duplicate := seenComponentIDs[normalizedID]; duplicate {
			return StatuspageSummary{}, errInvalidStatuspageResponse
		}
		seenComponentIDs[normalizedID] = struct{}{}
		components = append(components, component)
	}

	incidents := make([]StatuspageIncident, 0, len(wire.Incidents))
	seenIncidentIDs := make(map[string]struct{}, len(wire.Incidents))
	for _, incidentWire := range wire.Incidents {
		incident, err := decodeStatuspageIncident(incidentWire)
		if err != nil {
			return StatuspageSummary{}, errInvalidStatuspageResponse
		}
		if _, duplicate := seenIncidentIDs[incident.ID]; duplicate {
			return StatuspageSummary{}, errInvalidStatuspageResponse
		}
		seenIncidentIDs[incident.ID] = struct{}{}
		incidents = append(incidents, incident)
	}
	var coverageStart *time.Time
	if wire.RadarHistoryCoverageStart != nil {
		parsed, err := parseStatuspageTimestamp(*wire.RadarHistoryCoverageStart)
		if err != nil || parsed.After(pageUpdatedAt) {
			return StatuspageSummary{}, errInvalidStatuspageResponse
		}
		coverageStart = &parsed
	}
	var componentImpacts []StatuspageComponentImpact
	if wire.RadarComponentImpacts != nil {
		componentImpacts = make([]StatuspageComponentImpact, 0, len(*wire.RadarComponentImpacts))
		seenImpactIDs := make(map[string]struct{}, len(*wire.RadarComponentImpacts))
		for _, impactWire := range *wire.RadarComponentImpacts {
			impact, err := decodeStatuspageImpact(impactWire)
			if err != nil {
				return StatuspageSummary{}, errInvalidStatuspageResponse
			}
			if _, duplicate := seenImpactIDs[impact.ID]; duplicate {
				return StatuspageSummary{}, errInvalidStatuspageResponse
			}
			seenImpactIDs[impact.ID] = struct{}{}
			componentImpacts = append(componentImpacts, impact)
		}
	}
	var componentHistory []StatuspageIncident
	if wire.RadarComponentHistory != nil {
		componentHistory = make([]StatuspageIncident, 0, len(*wire.RadarComponentHistory))
		seenHistoryIDs := make(map[string]struct{}, len(*wire.RadarComponentHistory))
		for _, incidentWire := range *wire.RadarComponentHistory {
			incident, err := decodeStatuspageIncident(incidentWire)
			if err != nil || len(incident.Components) == 0 {
				return StatuspageSummary{}, errInvalidStatuspageResponse
			}
			if _, duplicate := seenHistoryIDs[incident.ID]; duplicate {
				return StatuspageSummary{}, errInvalidStatuspageResponse
			}
			seenHistoryIDs[incident.ID] = struct{}{}
			componentHistory = append(componentHistory, incident)
		}
	}

	return StatuspageSummary{
		Page: StatuspagePage{
			ID:        strings.TrimSpace(wire.Page.ID),
			Name:      strings.TrimSpace(wire.Page.Name),
			URL:       strings.TrimSpace(wire.Page.URL),
			UpdatedAt: pageUpdatedAt,
		},
		Status: StatuspageOverallStatus{
			Indicator:   strings.TrimSpace(wire.Status.Indicator),
			Description: strings.TrimSpace(wire.Status.Description),
		},
		Components:           components,
		Incidents:            incidents,
		HistoryCoverageStart: coverageStart,
		ComponentImpacts:     componentImpacts,
		ComponentHistory:     componentHistory,
	}, nil
}

func decodeStatuspageImpact(wire statuspageImpactWire) (StatuspageComponentImpact, error) {
	wire.ID = strings.TrimSpace(wire.ID)
	wire.ComponentID = strings.TrimSpace(wire.ComponentID)
	wire.ComponentName = strings.TrimSpace(wire.ComponentName)
	wire.IncidentID = strings.TrimSpace(wire.IncidentID)
	wire.IncidentName = strings.TrimSpace(wire.IncidentName)
	wire.IncidentStatus = strings.TrimSpace(wire.IncidentStatus)
	status := normalizeStatuspageImpactStatus(wire.Status)
	if wire.ID == "" || wire.ComponentID == "" || wire.ComponentName == "" || wire.IncidentID == "" || wire.IncidentName == "" ||
		wire.IncidentStatus == "" || status == ServiceStatusUnknown {
		return StatuspageComponentImpact{}, errInvalidStatuspageResponse
	}
	groups := make([]string, 0, len(wire.ComponentGroups))
	seenGroups := make(map[string]struct{}, len(wire.ComponentGroups))
	for _, rawGroup := range wire.ComponentGroups {
		group := strings.TrimSpace(rawGroup)
		normalized := strings.ToLower(group)
		if group == "" {
			return StatuspageComponentImpact{}, errInvalidStatuspageResponse
		}
		if _, duplicate := seenGroups[normalized]; duplicate {
			return StatuspageComponentImpact{}, errInvalidStatuspageResponse
		}
		seenGroups[normalized] = struct{}{}
		groups = append(groups, group)
	}
	startAt, err := parseStatuspageTimestamp(wire.StartAt)
	if err != nil {
		return StatuspageComponentImpact{}, errInvalidStatuspageResponse
	}
	var endAt *time.Time
	if wire.EndAt != nil {
		parsed, err := parseStatuspageTimestamp(*wire.EndAt)
		if err != nil || parsed.Before(startAt) {
			return StatuspageComponentImpact{}, errInvalidStatuspageResponse
		}
		endAt = &parsed
	}
	return StatuspageComponentImpact{
		ID: wire.ID, ComponentID: wire.ComponentID, ComponentName: wire.ComponentName, ComponentGroups: groups, IncidentID: wire.IncidentID,
		IncidentName: wire.IncidentName, IncidentStatus: wire.IncidentStatus,
		StartAt: startAt, EndAt: endAt, Status: status,
	}, nil
}

func decodeStatuspageIncidents(payload []byte) (StatuspagePage, []StatuspageIncident, error) {
	var wire statuspageIncidentsWire
	if !decodeStatuspageJSON(payload, &wire) || wire.Page == nil || wire.Incidents == nil ||
		len(*wire.Incidents) > statuspageIncidentHistoryLimit {
		return StatuspagePage{}, nil, errInvalidStatuspageResponse
	}
	updatedAt, err := parseStatuspageTimestamp(wire.Page.UpdatedAt)
	if err != nil || strings.TrimSpace(wire.Page.ID) == "" || strings.TrimSpace(wire.Page.Name) == "" {
		return StatuspagePage{}, nil, errInvalidStatuspageResponse
	}
	incidents := make([]StatuspageIncident, 0, len(*wire.Incidents))
	seen := make(map[string]struct{}, len(*wire.Incidents))
	for _, incidentWire := range *wire.Incidents {
		incident, err := decodeStatuspageIncident(incidentWire)
		if err != nil {
			return StatuspagePage{}, nil, errInvalidStatuspageResponse
		}
		if _, duplicate := seen[incident.ID]; duplicate {
			return StatuspagePage{}, nil, errInvalidStatuspageResponse
		}
		seen[incident.ID] = struct{}{}
		incidents = append(incidents, incident)
	}
	return StatuspagePage{
		ID: strings.TrimSpace(wire.Page.ID), Name: strings.TrimSpace(wire.Page.Name),
		URL: strings.TrimSpace(wire.Page.URL), UpdatedAt: updatedAt,
	}, incidents, nil
}

func mergeStatuspageHistoryPayloads(source RadarSourceKey, summaryPayload, incidentsPayload, auxiliaryPayload, timelinePayload, catalogPayload, calendarPayload []byte) ([]byte, error) {
	var summaryWire statuspageSummaryWire
	if !decodeStatuspageJSON(summaryPayload, &summaryWire) {
		return nil, errInvalidStatuspageResponse
	}
	summary, err := DecodeStatuspageSummary(summaryPayload)
	if err != nil {
		return nil, errInvalidStatuspageResponse
	}
	historyPage, incidents, err := decodeStatuspageIncidents(incidentsPayload)
	if err != nil || historyPage.ID != summary.Page.ID || historyPage.Name != summary.Page.Name {
		return nil, errInvalidStatuspageResponse
	}
	effectiveUpdatedAt := summary.Page.UpdatedAt
	if historyPage.UpdatedAt.After(effectiveUpdatedAt) {
		effectiveUpdatedAt = historyPage.UpdatedAt
	}
	coverageLimit := statuspageIncidentHistoryLimit
	coverageItemCount := len(incidents)
	var explicitCoverageStart *time.Time
	authoritativeCoverage := false
	switch source {
	case RadarSourceStatusOpenAI:
		if len(calendarPayload) != 0 {
			return nil, errInvalidStatuspageResponse
		}
		feed, feedErr := decodeOpenAIStatusFeed(auxiliaryPayload)
		if feedErr != nil {
			return nil, errInvalidStatuspageResponse
		}
		if feed.UpdatedAt.After(effectiveUpdatedAt) {
			effectiveUpdatedAt = feed.UpdatedAt
		}
		incidents, err = bindOpenAIStatusFeedComponents(incidents, feed)
		if err != nil {
			return nil, errInvalidStatuspageResponse
		}
		catalog, catalogErr := decodeOpenAIStatusCatalog(catalogPayload)
		if catalogErr != nil {
			return nil, errInvalidStatuspageResponse
		}
		componentImpacts, impactsErr := decodeOpenAIComponentImpacts(timelinePayload, catalog)
		if impactsErr != nil {
			return nil, errInvalidStatuspageResponse
		}
		impactCoverageStart, impactCoverageEnd, coverageErr := decodeOpenAIComponentImpactCoverage(timelinePayload)
		if coverageErr != nil {
			return nil, errInvalidStatuspageResponse
		}
		coverageLastInstant := impactCoverageEnd.Add(-time.Nanosecond)
		if coverageLastInstant.After(effectiveUpdatedAt) {
			effectiveUpdatedAt = coverageLastInstant
		}
		explicitCoverageStart = &impactCoverageStart
		summaryWire.RadarComponentImpacts = &componentImpacts
		coverageLimit = openAIIncidentHistoryLimit
		authoritativeCoverage = true
	case RadarSourceStatusClaude, RadarSourceStatusWindsurf, RadarSourceStatusKimi, RadarSourceStatusMiniMaxGlobal, RadarSourceStatusMiniMaxChina:
		if len(auxiliaryPayload) != 0 || len(timelinePayload) != 0 || len(catalogPayload) != 0 {
			return nil, errInvalidStatuspageResponse
		}
		calendar, calendarErr := decodeStatuspageCalendarBundle(calendarPayload)
		if calendarErr != nil || calendar.CoverageEnd.Before(effectiveUpdatedAt) {
			return nil, errInvalidStatuspageResponse
		}
		calendar, calendarErr = reconcileOpenStatuspageCalendarIncidents(calendar, incidents)
		if calendarErr != nil {
			return nil, errInvalidStatuspageResponse
		}
		explicitCoverageStart = &calendar.CoverageStart
		summaryWire.RadarComponentHistory = &calendar.Incidents
		authoritativeCoverage = true
	default:
		return nil, &RadarFetcherConfigError{Field: "statuspage_source"}
	}
	summaryWire.Incidents = encodeStatuspageIncidents(incidents)
	summaryWire.Page.UpdatedAt = effectiveUpdatedAt.Format(time.RFC3339Nano)
	windowStart := statuspageDayStart(source, effectiveUpdatedAt).AddDate(0, 0, -(serviceHealthHistoryDays - 1))
	coverageStart := windowStart
	if authoritativeCoverage {
		if explicitCoverageStart == nil {
			return nil, errInvalidStatuspageResponse
		}
		coverageStart = statuspageDayStart(source, *explicitCoverageStart)
		if coverageStart.Before(windowStart) {
			coverageStart = windowStart
		}
	} else if explicitCoverageStart != nil {
		coverageStart = statuspageDayStart(source, *explicitCoverageStart)
		if coverageStart.Before(windowStart) {
			coverageStart = windowStart
		}
	} else if coverageItemCount >= coverageLimit {
		oldest := effectiveUpdatedAt
		for _, incident := range incidents {
			if incident.CreatedAt.Before(oldest) {
				oldest = incident.CreatedAt
			}
		}
		oldestDay := statuspageDayStart(source, oldest)
		if oldestDay.After(coverageStart) {
			coverageStart = oldestDay
		}
	}
	formattedCoverage := coverageStart.Format(time.RFC3339)
	summaryWire.RadarHistoryCoverageStart = &formattedCoverage
	merged, err := json.Marshal(summaryWire)
	if err != nil {
		return nil, errInvalidStatuspageResponse
	}
	if _, err := DecodeStatuspageSummary(merged); err != nil {
		return nil, errInvalidStatuspageResponse
	}
	return merged, nil
}

func decodeOpenAIComponentImpacts(payload []byte, catalog map[string]openAIStatusCatalogComponent) ([]statuspageImpactWire, error) {
	var wire openAIComponentImpactsWire
	if !decodeStatuspageJSON(payload, &wire) || wire.ComponentImpacts == nil || wire.IncidentLinks == nil ||
		len(*wire.ComponentImpacts) > statuspageHistoryMaxIncidents*64 || len(*wire.IncidentLinks) > statuspageHistoryMaxIncidents {
		return nil, errInvalidStatuspageResponse
	}
	links := make(map[string]openAIComponentIncidentLink, len(*wire.IncidentLinks))
	for _, link := range *wire.IncidentLinks {
		link.ID = strings.TrimSpace(link.ID)
		link.Name = strings.TrimSpace(link.Name)
		link.Status = strings.TrimSpace(link.Status)
		if link.ID == "" || link.Name == "" || link.Status == "" {
			return nil, errInvalidStatuspageResponse
		}
		if _, err := parseStatuspageTimestamp(link.PublishedAt); err != nil {
			return nil, errInvalidStatuspageResponse
		}
		if _, duplicate := links[link.ID]; duplicate {
			return nil, errInvalidStatuspageResponse
		}
		links[link.ID] = link
	}
	result := make([]statuspageImpactWire, 0, len(*wire.ComponentImpacts))
	seen := make(map[string]struct{}, len(*wire.ComponentImpacts))
	for _, impact := range *wire.ComponentImpacts {
		impact.ID = strings.TrimSpace(impact.ID)
		impact.ComponentID = strings.TrimSpace(impact.ComponentID)
		impact.IncidentID = strings.TrimSpace(impact.IncidentID)
		if impact.ID == "" || impact.ComponentID == "" || impact.IncidentID == "" ||
			normalizeStatuspageImpactStatus(impact.Status) == ServiceStatusUnknown {
			return nil, errInvalidStatuspageResponse
		}
		startAt, err := parseStatuspageTimestamp(impact.StartAt)
		if err != nil {
			return nil, errInvalidStatuspageResponse
		}
		if impact.EndAt != nil {
			endAt, err := parseStatuspageTimestamp(*impact.EndAt)
			if err != nil || endAt.Before(startAt) {
				return nil, errInvalidStatuspageResponse
			}
			formatted := endAt.Format(time.RFC3339Nano)
			impact.EndAt = &formatted
		}
		link, ok := links[impact.IncidentID]
		if !ok {
			return nil, errInvalidStatuspageResponse
		}
		if catalog != nil {
			component, ok := catalog[normalizeStatuspageComponentID(impact.ComponentID)]
			if !ok {
				// Silently dropping an impact whose component is no longer present in
				// the legacy summary would produce a false operational day. Require
				// the authoritative Incident.io catalog to classify every impact.
				return nil, errInvalidStatuspageResponse
			}
			impact.ComponentName = component.Name
			impact.ComponentGroups = append([]string(nil), component.Groups...)
		}
		if _, duplicate := seen[impact.ID]; duplicate {
			return nil, errInvalidStatuspageResponse
		}
		seen[impact.ID] = struct{}{}
		impact.IncidentName = link.Name
		impact.IncidentStatus = link.Status
		impact.StartAt = startAt.Format(time.RFC3339Nano)
		impact.Status = string(normalizeStatuspageImpactStatus(impact.Status))
		result = append(result, impact)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].StartAt != result[right].StartAt {
			return result[left].StartAt < result[right].StartAt
		}
		return result[left].ID < result[right].ID
	})
	return result, nil
}

func decodeOpenAIComponentImpactCoverage(payload []byte) (time.Time, time.Time, error) {
	var wire openAIComponentImpactsWire
	if !decodeStatuspageJSON(payload, &wire) || wire.RadarCoverageStart == nil || wire.RadarCoverageEnd == nil {
		return time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	start, err := parseStatuspageTimestamp(*wire.RadarCoverageStart)
	if err != nil {
		return time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	end, err := parseStatuspageTimestamp(*wire.RadarCoverageEnd)
	if err != nil || !end.After(start) || end.Sub(start) != serviceHealthHistoryDays*24*time.Hour ||
		!start.Equal(utcDayStart(start)) || !end.Equal(utcDayStart(end)) {
		return time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	return start, end, nil
}

func decodeOpenAIStatusCatalog(payload []byte) (map[string]openAIStatusCatalogComponent, error) {
	var envelope openAIStatusSummaryEnvelopeWire
	if !decodeStatuspageJSON(payload, &envelope) || envelope.Summary == nil || envelope.Summary.Components == nil ||
		envelope.Summary.Structure == nil || envelope.Summary.Structure.Items == nil ||
		len(*envelope.Summary.Components) == 0 || len(*envelope.Summary.Components) > statuspageHistoryMaxIncidents ||
		len(*envelope.Summary.Structure.Items) > statuspageHistoryMaxIncidents {
		return nil, errInvalidStatuspageResponse
	}
	catalog := make(map[string]openAIStatusCatalogComponent, len(*envelope.Summary.Components))
	for _, wire := range *envelope.Summary.Components {
		id := normalizeStatuspageComponentID(wire.ID)
		name := strings.TrimSpace(wire.Name)
		if id == "" || name == "" {
			return nil, errInvalidStatuspageResponse
		}
		if _, duplicate := catalog[id]; duplicate {
			return nil, errInvalidStatuspageResponse
		}
		catalog[id] = openAIStatusCatalogComponent{Name: name, Groups: make([]string, 0, 1)}
	}
	seenGroupIDs := make(map[string]struct{}, len(*envelope.Summary.Structure.Items))
	for _, item := range *envelope.Summary.Structure.Items {
		if (item.Group == nil) == (item.Component == nil) {
			return nil, errInvalidStatuspageResponse
		}
		if item.Component != nil {
			componentID := normalizeStatuspageComponentID(item.Component.ComponentID)
			componentName := strings.TrimSpace(item.Component.Name)
			component, ok := catalog[componentID]
			if componentID == "" || componentName == "" || !ok || component.Name != componentName {
				return nil, errInvalidStatuspageResponse
			}
			continue
		}
		if item.Group.Components == nil {
			return nil, errInvalidStatuspageResponse
		}
		groupID := normalizeStatuspageComponentID(item.Group.ID)
		groupName := strings.TrimSpace(item.Group.Name)
		if groupID == "" || groupName == "" || len(*item.Group.Components) > statuspageHistoryMaxIncidents {
			return nil, errInvalidStatuspageResponse
		}
		if _, duplicate := seenGroupIDs[groupID]; duplicate {
			return nil, errInvalidStatuspageResponse
		}
		seenGroupIDs[groupID] = struct{}{}
		seenComponentIDs := make(map[string]struct{}, len(*item.Group.Components))
		for _, groupComponent := range *item.Group.Components {
			componentID := normalizeStatuspageComponentID(groupComponent.ComponentID)
			componentName := strings.TrimSpace(groupComponent.Name)
			component, ok := catalog[componentID]
			if componentID == "" || componentName == "" || !ok || component.Name != componentName {
				return nil, errInvalidStatuspageResponse
			}
			if _, duplicate := seenComponentIDs[componentID]; duplicate {
				return nil, errInvalidStatuspageResponse
			}
			seenComponentIDs[componentID] = struct{}{}
			component.Groups = append(component.Groups, groupName)
			catalog[componentID] = component
		}
	}
	return catalog, nil
}

func normalizeStatuspageImpactStatus(raw string) ServiceStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "full_outage", string(ServiceStatusMajorOutage):
		return ServiceStatusMajorOutage
	case string(ServiceStatusPartialOutage):
		return ServiceStatusPartialOutage
	case string(ServiceStatusDegradedPerformance):
		return ServiceStatusDegradedPerformance
	case "maintenance", string(ServiceStatusUnderMaintenance):
		return ServiceStatusUnderMaintenance
	default:
		return ServiceStatusUnknown
	}
}

type openAIStatusFeed struct {
	UpdatedAt time.Time
	Entries   map[string]openAIStatusFeedEntry
}

type openAIStatusFeedEntry struct {
	Title      string
	Components []string
}

type openAIStatusFeedWire struct {
	XMLName   xml.Name                    `xml:"feed"`
	ID        string                      `xml:"id"`
	Updated   string                      `xml:"updated"`
	Generator string                      `xml:"generator"`
	Entries   []openAIStatusFeedEntryWire `xml:"entry"`
}

type openAIStatusFeedEntryWire struct {
	ID      string `xml:"id"`
	Title   string `xml:"title"`
	Updated string `xml:"updated"`
	Summary string `xml:"summary"`
	Content string `xml:"content"`
}

func decodeOpenAIStatusFeed(payload []byte) (openAIStatusFeed, error) {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	var wire openAIStatusFeedWire
	if err := decoder.Decode(&wire); err != nil || wire.XMLName.Space != "http://www.w3.org/2005/Atom" ||
		strings.TrimSpace(wire.ID) != "https://status.openai.com/" || strings.TrimSpace(wire.Generator) != "incident.io" ||
		len(wire.Entries) > openAIStatusFeedMaxEntries {
		return openAIStatusFeed{}, errInvalidStatuspageResponse
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return openAIStatusFeed{}, errInvalidStatuspageResponse
	}
	updatedAt, err := parseStatuspageTimestamp(wire.Updated)
	if err != nil {
		return openAIStatusFeed{}, errInvalidStatuspageResponse
	}
	entries := make(map[string]openAIStatusFeedEntry, len(wire.Entries))
	for _, entry := range wire.Entries {
		incidentID, err := openAIStatusFeedIncidentID(entry.ID)
		if err != nil || strings.TrimSpace(entry.Title) == "" {
			return openAIStatusFeed{}, errInvalidStatuspageResponse
		}
		if _, err := parseStatuspageTimestamp(entry.Updated); err != nil {
			return openAIStatusFeed{}, errInvalidStatuspageResponse
		}
		if _, duplicate := entries[incidentID]; duplicate {
			return openAIStatusFeed{}, errInvalidStatuspageResponse
		}
		htmlPayload := entry.Content
		if strings.TrimSpace(htmlPayload) == "" {
			htmlPayload = entry.Summary
		}
		components, err := statusFeedComponentNames(htmlPayload)
		if err != nil {
			return openAIStatusFeed{}, errInvalidStatuspageResponse
		}
		entries[incidentID] = openAIStatusFeedEntry{Title: strings.TrimSpace(entry.Title), Components: components}
	}
	return openAIStatusFeed{UpdatedAt: updatedAt, Entries: entries}, nil
}

func openAIStatusFeedIncidentID(raw string) (string, error) {
	const singleSlash = "https://status.openai.com/incidents/"
	const doubleSlash = "https://status.openai.com//incidents/"
	value := strings.TrimSpace(raw)
	var id string
	switch {
	case strings.HasPrefix(value, doubleSlash):
		id = strings.TrimPrefix(value, doubleSlash)
	case strings.HasPrefix(value, singleSlash):
		id = strings.TrimPrefix(value, singleSlash)
	default:
		return "", errInvalidStatuspageResponse
	}
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "/?#\\\x00\r\n\t ") {
		return "", errInvalidStatuspageResponse
	}
	return id, nil
}

func statusFeedComponentNames(fragment string) ([]string, error) {
	if strings.TrimSpace(fragment) == "" || len(fragment) > 256*1024 {
		return nil, errInvalidStatuspageResponse
	}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), nil)
	if err != nil {
		return nil, errInvalidStatuspageResponse
	}
	components := make([]string, 0)
	seen := make(map[string]struct{})
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "li" {
			value := strings.TrimSpace(statuspageHTMLText(node))
			if marker := strings.LastIndex(value, " ("); marker > 0 && strings.HasSuffix(value, ")") {
				value = strings.TrimSpace(value[:marker])
			}
			key := strings.ToLower(value)
			if value != "" {
				if _, duplicate := seen[key]; !duplicate {
					seen[key] = struct{}{}
					components = append(components, value)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	for _, node := range nodes {
		visit(node)
	}
	if len(components) > 64 {
		return nil, errInvalidStatuspageResponse
	}
	return components, nil
}

func statuspageHTMLText(node *html.Node) string {
	var builder strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return builder.String()
}

func bindOpenAIStatusFeedComponents(incidents []StatuspageIncident, feed openAIStatusFeed) ([]StatuspageIncident, error) {
	result := append([]StatuspageIncident(nil), incidents...)
	for index := range result {
		entry, ok := feed.Entries[result[index].ID]
		if !ok {
			continue
		}
		if strings.TrimSpace(entry.Title) != strings.TrimSpace(result[index].Name) {
			return nil, errInvalidStatuspageResponse
		}
		result[index].Components = make([]StatuspageIncidentComponent, 0, len(entry.Components))
		for _, name := range entry.Components {
			result[index].Components = append(result[index].Components, StatuspageIncidentComponent{Name: name})
		}
	}
	return result, nil
}

type statuspageHistoryProps struct {
	Components      []statuspageHistoryComponent `json:"components"`
	Months          []statuspageHistoryMonth     `json:"months"`
	ComponentFilter []string                     `json:"component_filter"`
	StartTime       string                       `json:"start_time"`
	EndTime         string                       `json:"end_time"`
}

type statuspageHistoryComponent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group bool   `json:"group"`
}

type statuspageHistoryMonth struct {
	Name      string                      `json:"name"`
	Year      int                         `json:"year"`
	Days      int                         `json:"days"`
	Incidents []statuspageHistoryIncident `json:"incidents"`
}

type statuspageHistoryIncident struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Impact    string `json:"impact"`
	Timestamp string `json:"timestamp"`
}

func decodeStatuspageCalendarHistory(
	payload []byte,
	source RadarSourceKey,
	serviceKey ServiceKey,
	expectedComponentIDs []string,
) ([]StatuspageIncident, time.Time, time.Time, error) {
	document, err := html.Parse(bytes.NewReader(payload))
	if err != nil {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	var propsValue string
	matchCount := 0
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			isHistory := false
			candidate := ""
			for _, attribute := range node.Attr {
				switch attribute.Key {
				case "data-react-class":
					isHistory = attribute.Val == "HistoryIndex"
				case "data-react-props":
					candidate = attribute.Val
				}
			}
			if isHistory && candidate != "" {
				matchCount++
				propsValue = candidate
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	if matchCount != 1 || len(propsValue) > 2*1024*1024 {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	var props statuspageHistoryProps
	if !decodeStatuspageJSON([]byte(propsValue), &props) || len(props.Components) == 0 || len(props.Components) > statuspageHistoryMaxIncidents ||
		len(expectedComponentIDs) == 0 || len(props.Months) == 0 || len(props.Months) > statuspageHistoryMaxMonths {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	expectedIDs, ok := normalizedStatuspageIDSet(expectedComponentIDs)
	if !ok || !equalStatuspageIDSets(expectedIDs, props.ComponentFilter) {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	componentNames := make(map[string]string, len(props.Components))
	matchedIDs := make(map[string]struct{}, len(expectedIDs))
	for _, component := range props.Components {
		id := normalizeStatuspageComponentID(component.ID)
		name := strings.TrimSpace(component.Name)
		if id == "" || name == "" {
			return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
		}
		if _, duplicate := componentNames[id]; duplicate {
			return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
		}
		componentNames[id] = name
		if !component.Group && statuspageComponentMatches(source, serviceKey, name) {
			matchedIDs[id] = struct{}{}
		}
	}
	if len(matchedIDs) != len(expectedIDs) {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	components := make([]StatuspageIncidentComponent, 0, len(expectedIDs))
	for id := range expectedIDs {
		name, exists := componentNames[id]
		if !exists {
			return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
		}
		if _, matched := matchedIDs[id]; !matched {
			return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
		}
		components = append(components, StatuspageIncidentComponent{ID: id, Name: name})
	}
	sort.Slice(components, func(left, right int) bool { return components[left].ID < components[right].ID })
	startValue, err := time.Parse(time.RFC3339Nano, props.StartTime)
	if err != nil || startValue.IsZero() {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	endValue, err := time.Parse(time.RFC3339Nano, props.EndTime)
	if err != nil || endValue.IsZero() || !endValue.After(startValue) {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	location := statuspageHistoryLocation(source)
	_, startOffset := startValue.Zone()
	_, endOffset := endValue.Zone()
	_, expectedStartOffset := startValue.In(location).Zone()
	_, expectedEndOffset := endValue.In(location).Zone()
	if startOffset != expectedStartOffset || endOffset != expectedEndOffset || endValue.Sub(startValue) < serviceHealthHistoryDays*24*time.Hour {
		return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	start := startValue.UTC()
	end := endValue.UTC()
	incidents := make([]StatuspageIncident, 0)
	seen := make(map[string]struct{})
	seenMonths := make(map[string]struct{}, len(props.Months))
	for _, month := range props.Months {
		monthNumber, ok := statuspageMonthByName(month.Name)
		if !ok || month.Year < 2020 || month.Year > 2100 || month.Days != daysInStatuspageMonth(month.Year, monthNumber) {
			return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
		}
		monthKey := fmt.Sprintf("%04d-%02d", month.Year, monthNumber)
		if _, duplicate := seenMonths[monthKey]; duplicate {
			return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
		}
		seenMonths[monthKey] = struct{}{}
		for _, incident := range month.Incidents {
			if len(incidents) >= statuspageHistoryMaxIncidents || strings.TrimSpace(incident.Code) == "" || len(incident.Code) > 128 ||
				strings.ContainsAny(incident.Code, "/?#\\\x00\r\n\t ") ||
				strings.TrimSpace(incident.Name) == "" || serviceStatusForIncidentImpact(incident.Impact) == ServiceStatusUnknown {
				return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
			}
			if _, duplicate := seen[incident.Code]; duplicate {
				return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
			}
			createdAt, resolvedAt, err := parseStatuspageHistoryTimestamp(month, incident.Timestamp, location, startValue, endValue)
			if err != nil {
				return nil, time.Time{}, time.Time{}, errInvalidStatuspageResponse
			}
			status := "resolved"
			if resolvedAt == nil {
				// Statuspage renders an active incident with only its start time.
				// The exact upstream state is reconciled with incidents.json after
				// the independently fetched calendar bundles are merged.
				status = "investigating"
			}
			seen[incident.Code] = struct{}{}
			incidents = append(incidents, StatuspageIncident{
				ID: incident.Code, Name: strings.TrimSpace(incident.Name), Status: status, Impact: strings.TrimSpace(incident.Impact),
				CreatedAt: createdAt, ResolvedAt: resolvedAt,
				Components: append([]StatuspageIncidentComponent(nil), components...),
			})
		}
	}
	sort.Slice(incidents, func(left, right int) bool {
		if incidents[left].CreatedAt.Equal(incidents[right].CreatedAt) {
			return incidents[left].ID < incidents[right].ID
		}
		return incidents[left].CreatedAt.After(incidents[right].CreatedAt)
	})
	return incidents, start, end, nil
}

func parseStatuspageHistoryTimestamp(
	month statuspageHistoryMonth,
	raw string,
	location *time.Location,
	coverageStart time.Time,
	coverageEnd time.Time,
) (time.Time, *time.Time, error) {
	raw = strings.TrimSpace(raw)
	matches := statuspageHistoryTimestampPattern.FindStringSubmatch(raw)
	openMatches := statuspageHistoryOpenTimestampPattern.FindStringSubmatch(raw)
	if len(matches) != 8 && len(openMatches) != 5 {
		return time.Time{}, nil, errInvalidStatuspageResponse
	}
	expectedZone := "UTC"
	if _, offset := coverageStart.In(location).Zone(); offset == 8*60*60 {
		expectedZone = "CST"
	}
	zone := ""
	if len(matches) == 8 {
		zone = matches[7]
	} else {
		zone = openMatches[4]
		matches = []string{"", openMatches[1], openMatches[2], openMatches[3], "", "", "", zone}
	}
	if zone != expectedZone {
		return time.Time{}, nil, errInvalidStatuspageResponse
	}
	startMonth, ok := statuspageMonthByAbbreviation(matches[1])
	if !ok {
		return time.Time{}, nil, errInvalidStatuspageResponse
	}
	endMonth := startMonth
	endDay := matches[2]
	if matches[4] != "" {
		endMonth, ok = statuspageMonthByAbbreviation(matches[4])
		if !ok {
			return time.Time{}, nil, errInvalidStatuspageResponse
		}
		endDay = matches[5]
	}
	calendarMonth, ok := statuspageMonthByName(month.Name)
	if !ok {
		return time.Time{}, nil, errInvalidStatuspageResponse
	}
	calendarStart := time.Date(month.Year, calendarMonth, 1, 0, 0, 0, 0, location)
	calendarEnd := calendarStart.AddDate(0, 1, 0)
	type candidate struct{ start, end time.Time }
	candidates := make([]candidate, 0, 1)
	firstYear := coverageStart.In(location).Year() - 1
	lastYear := coverageEnd.In(location).Year() + 1
	for startYear := firstYear; startYear <= lastYear; startYear++ {
		createdAt, err := time.ParseInLocation("2006 Jan 2 15:04", fmt.Sprintf("%d %s %s %s", startYear, matches[1], matches[2], matches[3]), location)
		if err != nil || createdAt.Month() != startMonth {
			continue
		}
		if len(openMatches) == 5 {
			if !createdAt.Before(calendarEnd) || createdAt.Before(calendarStart) || createdAt.Before(coverageStart) || createdAt.After(coverageEnd) {
				continue
			}
			candidates = append(candidates, candidate{start: createdAt})
			continue
		}
		for endYear := startYear; endYear <= startYear+1; endYear++ {
			resolvedAt, err := time.ParseInLocation("2006 Jan 2 15:04", fmt.Sprintf("%d %s %s %s", endYear, endMonth.String()[:3], endDay, matches[6]), location)
			if err != nil || resolvedAt.Month() != endMonth || resolvedAt.Before(createdAt) || resolvedAt.Sub(createdAt) > 180*24*time.Hour {
				continue
			}
			if !createdAt.Before(calendarEnd) || !resolvedAt.After(calendarStart) || createdAt.After(coverageEnd) || resolvedAt.Before(coverageStart) {
				continue
			}
			candidates = append(candidates, candidate{start: createdAt, end: resolvedAt})
		}
	}
	if len(candidates) != 1 {
		return time.Time{}, nil, errInvalidStatuspageResponse
	}
	createdAt := candidates[0].start.UTC()
	if len(openMatches) == 5 {
		return createdAt, nil, nil
	}
	resolvedAt := candidates[0].end.UTC()
	return createdAt, &resolvedAt, nil
}

func statuspageMonthByName(name string) (time.Month, bool) {
	for _, month := range []time.Month{time.January, time.February, time.March, time.April, time.May, time.June, time.July, time.August, time.September, time.October, time.November, time.December} {
		if month.String() == name {
			return month, true
		}
	}
	return 0, false
}

func statuspageMonthByAbbreviation(value string) (time.Month, bool) {
	for _, month := range []time.Month{time.January, time.February, time.March, time.April, time.May, time.June, time.July, time.August, time.September, time.October, time.November, time.December} {
		if month.String()[:3] == value {
			return month, true
		}
	}
	return 0, false
}

func daysInStatuspageMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func normalizedStatuspageIDSet(ids []string) (map[string]struct{}, bool) {
	result := make(map[string]struct{}, len(ids))
	for _, rawID := range ids {
		id := normalizeStatuspageComponentID(rawID)
		if id == "" {
			return nil, false
		}
		if _, duplicate := result[id]; duplicate {
			return nil, false
		}
		result[id] = struct{}{}
	}
	return result, true
}

func equalStatuspageIDSets(expected map[string]struct{}, actual []string) bool {
	actualSet, ok := normalizedStatuspageIDSet(actual)
	if !ok || len(actualSet) != len(expected) {
		return false
	}
	for id := range expected {
		if _, exists := actualSet[id]; !exists {
			return false
		}
	}
	return true
}

func decodeStatuspageCalendarBundle(payload []byte) (statuspageCalendarBundle, error) {
	var wire statuspageCalendarBundleWire
	if !decodeStatuspageJSON(payload, &wire) || len(wire.Incidents) > statuspageHistoryMaxIncidents*4 {
		return statuspageCalendarBundle{}, errInvalidStatuspageResponse
	}
	coverageStart, err := parseStatuspageTimestamp(wire.CoverageStart)
	if err != nil {
		return statuspageCalendarBundle{}, errInvalidStatuspageResponse
	}
	coverageEnd, err := parseStatuspageTimestamp(wire.CoverageEnd)
	if err != nil || !coverageEnd.After(coverageStart) || coverageEnd.Sub(coverageStart) < serviceHealthHistoryDays*24*time.Hour {
		return statuspageCalendarBundle{}, errInvalidStatuspageResponse
	}
	incidents := make([]StatuspageIncident, 0, len(wire.Incidents))
	seen := make(map[string]struct{}, len(wire.Incidents))
	for _, incidentWire := range wire.Incidents {
		incident, err := decodeStatuspageIncident(incidentWire)
		if err != nil || len(incident.Components) == 0 {
			return statuspageCalendarBundle{}, errInvalidStatuspageResponse
		}
		if _, duplicate := seen[incident.ID]; duplicate {
			return statuspageCalendarBundle{}, errInvalidStatuspageResponse
		}
		seen[incident.ID] = struct{}{}
		incidents = append(incidents, incident)
	}
	return statuspageCalendarBundle{
		CoverageStart: coverageStart,
		CoverageEnd:   coverageEnd,
		Incidents:     encodeStatuspageIncidents(incidents),
	}, nil
}

// reconcileOpenStatuspageCalendarIncidents replaces the calendar parser's
// provisional state for active incidents with the precise incidents.json
// state. Statuspage's HistoryIndex only exposes the start minute while an
// incident is open, so the companion JSON endpoint remains authoritative for
// its status and second-precision creation time.
func reconcileOpenStatuspageCalendarIncidents(
	calendar statuspageCalendarBundle,
	current []StatuspageIncident,
) (statuspageCalendarBundle, error) {
	currentByID := make(map[string]StatuspageIncident, len(current))
	for _, incident := range current {
		currentByID[incident.ID] = incident
	}
	for index := range calendar.Incidents {
		wire := &calendar.Incidents[index]
		if wire.ResolvedAt != nil {
			continue
		}
		incident, ok := currentByID[strings.TrimSpace(wire.ID)]
		if !ok || strings.TrimSpace(wire.Name) != incident.Name || strings.TrimSpace(wire.Impact) != incident.Impact {
			return statuspageCalendarBundle{}, errInvalidStatuspageResponse
		}
		calendarCreatedAt, err := parseStatuspageTimestamp(wire.CreatedAt)
		if err != nil || !calendarCreatedAt.Truncate(time.Minute).Equal(incident.CreatedAt.UTC().Truncate(time.Minute)) {
			return statuspageCalendarBundle{}, errInvalidStatuspageResponse
		}
		wire.Status = incident.Status
		wire.CreatedAt = incident.CreatedAt.UTC().Format(time.RFC3339Nano)
		if incident.ResolvedAt != nil {
			resolvedAt := incident.ResolvedAt.UTC().Format(time.RFC3339Nano)
			wire.ResolvedAt = &resolvedAt
		}
	}
	return calendar, nil
}

func mergeStatuspageCalendarIncidents(left, right []StatuspageIncident) ([]StatuspageIncident, error) {
	result := append([]StatuspageIncident(nil), left...)
	byID := make(map[string]int, len(left)+len(right))
	for index, incident := range result {
		byID[incident.ID] = index
	}
	for _, candidate := range right {
		index, duplicate := byID[candidate.ID]
		if !duplicate {
			byID[candidate.ID] = len(result)
			result = append(result, candidate)
			continue
		}
		current := result[index]
		if current.Name != candidate.Name || current.Status != candidate.Status || current.Impact != candidate.Impact ||
			!current.CreatedAt.Equal(candidate.CreatedAt) || !equalStatuspageOptionalTime(current.ResolvedAt, candidate.ResolvedAt) {
			return nil, errInvalidStatuspageResponse
		}
		components := make(map[string]string, len(current.Components)+len(candidate.Components))
		for _, component := range append(current.Components, candidate.Components...) {
			id := normalizeStatuspageComponentID(component.ID)
			name := strings.TrimSpace(component.Name)
			if id == "" || name == "" {
				return nil, errInvalidStatuspageResponse
			}
			if existing, exists := components[id]; exists && existing != name {
				return nil, errInvalidStatuspageResponse
			}
			components[id] = name
		}
		current.Components = make([]StatuspageIncidentComponent, 0, len(components))
		for id, name := range components {
			current.Components = append(current.Components, StatuspageIncidentComponent{ID: id, Name: name})
		}
		sort.Slice(current.Components, func(left, right int) bool { return current.Components[left].ID < current.Components[right].ID })
		result[index] = current
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.After(result[right].CreatedAt)
	})
	return result, nil
}

func equalStatuspageOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func mergeStatuspageIncidents(primary, supplemental []StatuspageIncident) ([]StatuspageIncident, error) {
	result := append([]StatuspageIncident(nil), primary...)
	byID := make(map[string]StatuspageIncident, len(primary)+len(supplemental))
	for _, incident := range primary {
		byID[incident.ID] = incident
	}
	for _, incident := range supplemental {
		if existing, duplicate := byID[incident.ID]; duplicate {
			if existing.Name != incident.Name {
				return nil, errInvalidStatuspageResponse
			}
			continue
		}
		byID[incident.ID] = incident
		result = append(result, incident)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].CreatedAt.After(result[right].CreatedAt) })
	return result, nil
}

func encodeStatuspageIncidents(incidents []StatuspageIncident) []statuspageIncidentWire {
	result := make([]statuspageIncidentWire, 0, len(incidents))
	for _, incident := range incidents {
		wire := statuspageIncidentWire{
			ID: incident.ID, Name: incident.Name, Status: incident.Status, Impact: incident.Impact,
			CreatedAt:  incident.CreatedAt.UTC().Format(time.RFC3339Nano),
			Components: make([]statuspageIncidentComponentWire, 0, len(incident.Components)),
		}
		if incident.ResolvedAt != nil {
			value := incident.ResolvedAt.UTC().Format(time.RFC3339Nano)
			wire.ResolvedAt = &value
		}
		for _, component := range incident.Components {
			wire.Components = append(wire.Components, statuspageIncidentComponentWire{ID: component.ID, Name: component.Name})
		}
		result = append(result, wire)
	}
	return result
}

func utcDayStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func statuspageHistoryLocation(source RadarSourceKey) *time.Location {
	switch source {
	case RadarSourceStatusKimi, RadarSourceStatusMiniMaxChina:
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	default:
		return time.UTC
	}
}

func statuspageDayStart(source RadarSourceKey, value time.Time) time.Time {
	location := statuspageHistoryLocation(source)
	value = value.In(location)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location)
}

// MapStatuspageServiceHealth returns exactly two stable cards for one source.
// Unknown component states are localized to the affected card rather than
// invalidating the full response.
func MapStatuspageServiceHealth(source RadarSourceKey, summary StatuspageSummary) ([]ServiceHealthDTO, error) {
	if err := validateStatuspageSummary(summary); err != nil {
		return nil, errInvalidStatuspageResponse
	}

	_, publicURL, descriptors, ok := statuspageSourceDescriptor(source)
	if !ok {
		return nil, &RadarFetcherConfigError{Field: "statuspage_source"}
	}

	cards := make([]ServiceHealthDTO, 0, 2)
	for _, descriptor := range descriptors {
		matched := matchingStatuspageComponents(source, descriptor.Key, summary.Components)
		status, lastUpdatedAt := aggregateStatuspageComponents(matched)
		cards = append(cards, ServiceHealthDTO{
			ServiceKey:      descriptor.Key,
			Name:            descriptor.Name,
			Status:          status,
			StatusIndicator: statusIndicatorForServiceStatus(status),
			Uptime90d:       nil,
			LastIncident:    latestApplicableStatuspageIncident(source, descriptor.Key, matched, summary.Incidents),
			LastUpdatedAt:   lastUpdatedAt,
			History30d:      statuspageHistoryForService(source, descriptor.Key, matched, summary),
			SourceURL:       publicURL,
			Stale:           false,
		})
	}
	return cards, nil
}

// MergeStatuspageServiceHealth produces the four historical canonical cards
// plus any configured platform cards present in the source groups. Missing
// historical entries become unknown; optional platform entries are omitted so
// the frontend can distinguish "not fetched yet" from an upstream state.
func MergeStatuspageServiceHealth(groups ...[]ServiceHealthDTO) []ServiceHealthDTO {
	byKey := make(map[ServiceKey]ServiceHealthDTO, 8)
	for _, group := range groups {
		for _, card := range group {
			switch card.ServiceKey {
			case ServiceKeyClaudeAPI, ServiceKeyClaudeCode, ServiceKeyCodexWeb, ServiceKeyOpenAIAPI,
				ServiceKeyWindsurf, ServiceKeyDeepSeek, ServiceKeyKimi, ServiceKeyMiniMax:
				if current, exists := byKey[card.ServiceKey]; exists {
					byKey[card.ServiceKey] = mergeStatuspageCards(current, card)
				} else {
					byKey[card.ServiceKey] = cloneServiceHealthDTO(card)
				}
			}
		}
	}

	descriptors := CanonicalRadarServices()
	descriptors = append(descriptors,
		RadarServiceDescriptor{Key: ServiceKeyWindsurf, Name: "Windsurf"},
		RadarServiceDescriptor{Key: ServiceKeyDeepSeek, Name: "DeepSeek"},
		RadarServiceDescriptor{Key: ServiceKeyKimi, Name: "Kimi"},
		RadarServiceDescriptor{Key: ServiceKeyMiniMax, Name: "MiniMax"},
	)
	result := make([]ServiceHealthDTO, 0, len(descriptors))
	for _, descriptor := range descriptors {
		card, ok := byKey[descriptor.Key]
		if !ok {
			if descriptor.Key == ServiceKeyWindsurf || descriptor.Key == ServiceKeyDeepSeek || descriptor.Key == ServiceKeyKimi || descriptor.Key == ServiceKeyMiniMax {
				continue
			}
			card = unknownStatuspageCard(descriptor)
		}
		card.ServiceKey = descriptor.Key
		card.Name = descriptor.Name
		card.Status = canonicalServiceStatus(card.Status)
		card.StatusIndicator = statusIndicatorForServiceStatus(card.Status)
		card.Uptime90d = nil
		if descriptor.Key == ServiceKeyClaudeAPI || descriptor.Key == ServiceKeyClaudeCode || descriptor.Key == ServiceKeyOpenAIAPI || descriptor.Key == ServiceKeyCodexWeb {
			card.SourceURL = statuspagePublicURLForService(descriptor.Key)
		} else if card.SourceURL == "" {
			card.SourceURL = statuspagePublicURLForService(descriptor.Key)
		}
		result = append(result, card)
	}
	return result
}

func mergeStatuspageCards(left, right ServiceHealthDTO) ServiceHealthDTO {
	result := cloneServiceHealthDTO(left)
	if serviceStatusSeverity(right.Status) > serviceStatusSeverity(result.Status) {
		result.Status = right.Status
		result.StatusIndicator = right.StatusIndicator
		result.SourceURL = right.SourceURL
	}
	if result.LastUpdatedAt == nil || right.LastUpdatedAt != nil && right.LastUpdatedAt.After(*result.LastUpdatedAt) {
		if right.LastUpdatedAt != nil {
			value := right.LastUpdatedAt.UTC()
			result.LastUpdatedAt = &value
		}
	}
	if result.LastIncident == nil || right.LastIncident != nil && right.LastIncident.CreatedAt.After(result.LastIncident.CreatedAt) {
		result.LastIncident = cloneServiceHealthDTO(right).LastIncident
	}
	result.Stale = result.Stale || right.Stale
	result.History30d = mergeServiceHealthHistory(result.History30d, right.History30d)
	return result
}

func decodeStatuspageJSON(payload []byte, destination any) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	var trailing json.RawMessage
	return decoder.Decode(&trailing) == io.EOF
}

func parseStatuspageTimestamp(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, errInvalidStatuspageResponse
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || parsed.IsZero() {
		return time.Time{}, errInvalidStatuspageResponse
	}
	return parsed.UTC(), nil
}

func decodeStatuspageComponent(wire statuspageComponentWire) (StatuspageComponent, error) {
	if strings.TrimSpace(wire.ID) == "" || strings.TrimSpace(wire.Name) == "" || strings.TrimSpace(wire.Status) == "" {
		return StatuspageComponent{}, errInvalidStatuspageResponse
	}
	createdAt, err := parseStatuspageTimestamp(wire.CreatedAt)
	if err != nil {
		return StatuspageComponent{}, errInvalidStatuspageResponse
	}
	updatedAt, err := parseStatuspageTimestamp(wire.UpdatedAt)
	if err != nil {
		return StatuspageComponent{}, errInvalidStatuspageResponse
	}
	return StatuspageComponent{
		ID:        strings.TrimSpace(wire.ID),
		Name:      strings.TrimSpace(wire.Name),
		Status:    strings.TrimSpace(wire.Status),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Group:     wire.Group,
	}, nil
}

func decodeStatuspageIncident(wire statuspageIncidentWire) (StatuspageIncident, error) {
	if strings.TrimSpace(wire.ID) == "" || strings.TrimSpace(wire.Name) == "" ||
		strings.TrimSpace(wire.Status) == "" || strings.TrimSpace(wire.Impact) == "" {
		return StatuspageIncident{}, errInvalidStatuspageResponse
	}
	createdAt, err := parseStatuspageTimestamp(wire.CreatedAt)
	if err != nil {
		return StatuspageIncident{}, errInvalidStatuspageResponse
	}
	var resolvedAt *time.Time
	if wire.ResolvedAt != nil {
		parsed, err := parseStatuspageTimestamp(*wire.ResolvedAt)
		if err != nil || parsed.Before(createdAt) {
			return StatuspageIncident{}, errInvalidStatuspageResponse
		}
		resolvedAt = &parsed
	}
	components := make([]StatuspageIncidentComponent, 0, len(wire.Components))
	for _, component := range wire.Components {
		id := strings.TrimSpace(component.ID)
		name := strings.TrimSpace(component.Name)
		if id == "" && name == "" {
			return StatuspageIncident{}, errInvalidStatuspageResponse
		}
		components = append(components, StatuspageIncidentComponent{ID: id, Name: name})
	}
	return StatuspageIncident{
		ID:         strings.TrimSpace(wire.ID),
		Name:       strings.TrimSpace(wire.Name),
		Status:     strings.TrimSpace(wire.Status),
		Impact:     strings.TrimSpace(wire.Impact),
		CreatedAt:  createdAt,
		ResolvedAt: resolvedAt,
		Components: components,
	}, nil
}

func validateStatuspageSummary(summary StatuspageSummary) error {
	if strings.TrimSpace(summary.Page.ID) == "" || strings.TrimSpace(summary.Page.Name) == "" || summary.Page.UpdatedAt.IsZero() {
		return errInvalidStatuspageResponse
	}
	if strings.TrimSpace(summary.Status.Indicator) == "" || strings.TrimSpace(summary.Status.Description) == "" || summary.Components == nil {
		return errInvalidStatuspageResponse
	}
	seenComponentIDs := make(map[string]struct{}, len(summary.Components))
	for _, component := range summary.Components {
		if strings.TrimSpace(component.ID) == "" || strings.TrimSpace(component.Name) == "" || strings.TrimSpace(component.Status) == "" ||
			component.CreatedAt.IsZero() || component.UpdatedAt.IsZero() {
			return errInvalidStatuspageResponse
		}
		normalizedID := normalizeStatuspageComponentID(component.ID)
		if _, duplicate := seenComponentIDs[normalizedID]; duplicate {
			return errInvalidStatuspageResponse
		}
		seenComponentIDs[normalizedID] = struct{}{}
	}
	for _, incident := range summary.Incidents {
		if strings.TrimSpace(incident.ID) == "" || strings.TrimSpace(incident.Name) == "" ||
			strings.TrimSpace(incident.Status) == "" || strings.TrimSpace(incident.Impact) == "" || incident.CreatedAt.IsZero() {
			return errInvalidStatuspageResponse
		}
	}
	return nil
}

func normalizeStatuspageComponentID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func matchingStatuspageComponents(source RadarSourceKey, serviceKey ServiceKey, components []StatuspageComponent) []StatuspageComponent {
	matched := make([]StatuspageComponent, 0)
	for _, component := range components {
		if component.Group {
			continue
		}
		if statuspageComponentMatches(source, serviceKey, component.Name) {
			matched = append(matched, component)
		}
	}
	return matched
}

func statuspageComponentMatches(source RadarSourceKey, serviceKey ServiceKey, rawName string) bool {
	name := strings.ToLower(strings.TrimSpace(rawName))
	switch {
	case source == RadarSourceStatusClaude && serviceKey == ServiceKeyClaudeAPI:
		return matchesStatuspageAlias(name, claudeAPIStatuspageAliases)
	case source == RadarSourceStatusClaude && serviceKey == ServiceKeyClaudeCode:
		return matchesStatuspageAlias(name, claudeCodeStatuspageAliases)
	case source == RadarSourceStatusOpenAI && serviceKey == ServiceKeyCodexWeb:
		return matchesStatuspageAlias(name, codexWebStatuspageAliases)
	case source == RadarSourceStatusOpenAI && serviceKey == ServiceKeyOpenAIAPI:
		return matchesStatuspageAlias(name, openAIAPIStatuspageAliases)
	case source == RadarSourceStatusWindsurf && serviceKey == ServiceKeyWindsurf:
		return matchesStatuspageAlias(name, windsurfStatuspageAliases)
	case source == RadarSourceStatusDeepSeek && serviceKey == ServiceKeyDeepSeek:
		return matchesStatuspageAlias(name, deepSeekStatuspageAliases)
	case source == RadarSourceStatusKimi && serviceKey == ServiceKeyKimi:
		return matchesStatuspageAlias(name, kimiStatuspageAliases)
	case source == RadarSourceStatusMiniMaxGlobal && serviceKey == ServiceKeyMiniMax:
		return matchesStatuspageAlias(name, miniMaxGlobalStatuspageAliases)
	case source == RadarSourceStatusMiniMaxChina && serviceKey == ServiceKeyMiniMax:
		return matchesStatuspageAlias(name, miniMaxChinaStatuspageAliases)
	default:
		return false
	}
}

func matchesStatuspageAlias(name string, aliases map[string]struct{}) bool {
	_, ok := aliases[name]
	return ok
}

func aggregateStatuspageComponents(components []StatuspageComponent) (ServiceStatus, *time.Time) {
	if len(components) == 0 {
		return ServiceStatusUnknown, nil
	}
	status := normalizeStatuspageComponentStatus(components[0].Status)
	latest := components[0].UpdatedAt.UTC()
	for _, component := range components[1:] {
		candidate := normalizeStatuspageComponentStatus(component.Status)
		if serviceStatusSeverity(candidate) > serviceStatusSeverity(status) {
			status = candidate
		}
		if component.UpdatedAt.After(latest) {
			latest = component.UpdatedAt.UTC()
		}
	}
	return status, &latest
}

func normalizeStatuspageComponentStatus(raw string) ServiceStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ServiceStatusOperational):
		return ServiceStatusOperational
	case string(ServiceStatusDegradedPerformance):
		return ServiceStatusDegradedPerformance
	case string(ServiceStatusPartialOutage):
		return ServiceStatusPartialOutage
	case string(ServiceStatusMajorOutage):
		return ServiceStatusMajorOutage
	case string(ServiceStatusUnderMaintenance):
		return ServiceStatusUnderMaintenance
	default:
		return ServiceStatusUnknown
	}
}

func canonicalServiceStatus(status ServiceStatus) ServiceStatus {
	switch status {
	case ServiceStatusOperational,
		ServiceStatusDegradedPerformance,
		ServiceStatusPartialOutage,
		ServiceStatusMajorOutage,
		ServiceStatusUnderMaintenance,
		ServiceStatusUnknown:
		return status
	default:
		return ServiceStatusUnknown
	}
}

func serviceStatusSeverity(status ServiceStatus) int {
	// Unknown outranks operational so uncertainty cannot masquerade as healthy,
	// but a concrete maintenance/degradation/outage signal remains actionable.
	switch status {
	case ServiceStatusOperational:
		return 0
	case ServiceStatusUnknown:
		return 1
	case ServiceStatusUnderMaintenance:
		return 2
	case ServiceStatusDegradedPerformance:
		return 3
	case ServiceStatusPartialOutage:
		return 4
	case ServiceStatusMajorOutage:
		return 5
	default:
		return 1
	}
}

func statusIndicatorForServiceStatus(status ServiceStatus) StatusIndicator {
	switch status {
	case ServiceStatusOperational:
		return StatusIndicatorNone
	case ServiceStatusDegradedPerformance, ServiceStatusUnderMaintenance:
		return StatusIndicatorMinor
	case ServiceStatusPartialOutage:
		return StatusIndicatorMajor
	case ServiceStatusMajorOutage:
		return StatusIndicatorCritical
	default:
		return StatusIndicatorUnknown
	}
}

func latestApplicableStatuspageIncident(
	source RadarSourceKey,
	serviceKey ServiceKey,
	components []StatuspageComponent,
	incidents []StatuspageIncident,
) *RadarIncidentDTO {
	matchedIDs := make(map[string]struct{}, len(components))
	for _, component := range components {
		matchedIDs[normalizeStatuspageComponentID(component.ID)] = struct{}{}
	}

	var latest *StatuspageIncident
	for index := range incidents {
		incident := &incidents[index]
		// An incident without an official component binding cannot safely be
		// attributed to an API/LLM card. It remains an unknown history signal
		// rather than being copied to every product on the status page.
		if len(incident.Components) == 0 || !statuspageIncidentMatches(source, serviceKey, matchedIDs, incident.Components) {
			continue
		}
		if latest == nil || incident.CreatedAt.After(latest.CreatedAt) {
			latest = incident
		}
	}
	if latest == nil {
		return nil
	}
	createdAt := latest.CreatedAt.UTC()
	var resolvedAt *time.Time
	if latest.ResolvedAt != nil {
		value := latest.ResolvedAt.UTC()
		resolvedAt = &value
	}
	return &RadarIncidentDTO{
		Name:       latest.Name,
		Status:     latest.Status,
		Impact:     latest.Impact,
		CreatedAt:  createdAt,
		ResolvedAt: resolvedAt,
	}
}

func statuspageHistoryForService(
	source RadarSourceKey,
	serviceKey ServiceKey,
	components []StatuspageComponent,
	summary StatuspageSummary,
) []ServiceHealthHistoryDayDTO {
	if source == RadarSourceStatusOpenAI && summary.ComponentImpacts != nil {
		return statuspageImpactHistoryForService(source, serviceKey, components, summary)
	}
	endDay := statuspageDayStart(source, summary.Page.UpdatedAt)
	startDay := endDay.AddDate(0, 0, -(serviceHealthHistoryDays - 1))
	coverageStart := endDay.AddDate(0, 0, 1)
	if summary.HistoryCoverageStart != nil {
		coverageStart = statuspageDayStart(source, *summary.HistoryCoverageStart)
		if coverageStart.Before(startDay) {
			coverageStart = startDay
		}
	}
	matchedIDs := make(map[string]struct{}, len(components))
	for _, component := range components {
		matchedIDs[normalizeStatuspageComponentID(component.ID)] = struct{}{}
	}
	historyIncidents := summary.Incidents
	allowAmbiguous := true
	if summary.ComponentHistory != nil {
		historyIncidents = summary.ComponentHistory
		allowAmbiguous = false
	}
	relevant := make([]StatuspageIncident, 0, len(historyIncidents))
	ambiguous := make([]StatuspageIncident, 0)
	for _, incident := range historyIncidents {
		if len(incident.Components) == 0 {
			if allowAmbiguous {
				ambiguous = append(ambiguous, incident)
			}
			continue
		}
		if !statuspageIncidentMatches(source, serviceKey, matchedIDs, incident.Components) {
			continue
		}
		relevant = append(relevant, incident)
	}
	days := make([]ServiceHealthHistoryDayDTO, 0, serviceHealthHistoryDays)
	for offset := 0; offset < serviceHealthHistoryDays; offset++ {
		dayStart := startDay.AddDate(0, 0, offset)
		dayEnd := dayStart.AddDate(0, 0, 1)
		status := ServiceStatusOperational
		if dayStart.Before(coverageStart) {
			status = ServiceStatusUnknown
		}
		for _, incident := range ambiguous {
			incidentEnd := summary.Page.UpdatedAt
			if incident.ResolvedAt != nil {
				incidentEnd = *incident.ResolvedAt
			}
			if incident.CreatedAt.Before(dayEnd) && incidentEnd.After(dayStart) && serviceStatusSeverity(ServiceStatusUnknown) > serviceStatusSeverity(status) {
				status = ServiceStatusUnknown
			}
		}
		incidents := make([]RadarIncidentDTO, 0)
		for _, incident := range relevant {
			incidentEnd := summary.Page.UpdatedAt
			if incident.ResolvedAt != nil {
				incidentEnd = *incident.ResolvedAt
			}
			if !incident.CreatedAt.Before(dayEnd) || !incidentEnd.After(dayStart) {
				continue
			}
			incidentStatus := serviceStatusForIncidentImpact(incident.Impact)
			if serviceStatusSeverity(incidentStatus) > serviceStatusSeverity(status) {
				status = incidentStatus
			}
			createdAt := incident.CreatedAt.UTC()
			var resolvedAt *time.Time
			if incident.ResolvedAt != nil {
				value := incident.ResolvedAt.UTC()
				resolvedAt = &value
			}
			incidents = append(incidents, RadarIncidentDTO{
				Name: incident.Name, Status: incident.Status, Impact: incident.Impact,
				CreatedAt: createdAt, ResolvedAt: resolvedAt,
			})
		}
		sort.Slice(incidents, func(left, right int) bool {
			return incidents[left].CreatedAt.After(incidents[right].CreatedAt)
		})
		days = append(days, ServiceHealthHistoryDayDTO{
			Date: dayStart.Format(time.DateOnly), Status: status, Incidents: incidents,
		})
	}
	return days
}

func statuspageImpactHistoryForService(
	source RadarSourceKey,
	serviceKey ServiceKey,
	components []StatuspageComponent,
	summary StatuspageSummary,
) []ServiceHealthHistoryDayDTO {
	endDay := statuspageDayStart(source, summary.Page.UpdatedAt)
	startDay := endDay.AddDate(0, 0, -(serviceHealthHistoryDays - 1))
	coverageStart := endDay.AddDate(0, 0, 1)
	if summary.HistoryCoverageStart != nil {
		coverageStart = statuspageDayStart(source, *summary.HistoryCoverageStart)
		if coverageStart.Before(startDay) {
			coverageStart = startDay
		}
	}
	matchedIDs := make(map[string]struct{}, len(components))
	for _, component := range components {
		matchedIDs[normalizeStatuspageComponentID(component.ID)] = struct{}{}
	}
	relevant := make([]StatuspageComponentImpact, 0)
	for _, impact := range summary.ComponentImpacts {
		_, matchedCurrentComponent := matchedIDs[normalizeStatuspageComponentID(impact.ComponentID)]
		matchedHistoricalName := statuspageComponentMatches(source, serviceKey, impact.ComponentName)
		matchedOfficialGroup := serviceKey == ServiceKeyOpenAIAPI && containsStatuspageGroup(impact.ComponentGroups, "APIs")
		if matchedCurrentComponent || matchedHistoricalName || matchedOfficialGroup {
			relevant = append(relevant, impact)
		}
	}
	days := make([]ServiceHealthHistoryDayDTO, 0, serviceHealthHistoryDays)
	for offset := 0; offset < serviceHealthHistoryDays; offset++ {
		dayStart := startDay.AddDate(0, 0, offset)
		dayEnd := dayStart.AddDate(0, 0, 1)
		status := ServiceStatusOperational
		if dayStart.Before(coverageStart) {
			status = ServiceStatusUnknown
		}
		incidentsByID := make(map[string]RadarIncidentDTO)
		for _, impact := range relevant {
			impactEnd := summary.Page.UpdatedAt
			if impact.EndAt != nil {
				impactEnd = *impact.EndAt
			}
			if !impact.StartAt.Before(dayEnd) || !impactEnd.After(dayStart) {
				continue
			}
			if serviceStatusSeverity(impact.Status) > serviceStatusSeverity(status) {
				status = impact.Status
			}
			createdAt := impact.StartAt.UTC()
			var resolvedAt *time.Time
			if impact.EndAt != nil {
				value := impact.EndAt.UTC()
				resolvedAt = &value
			}
			candidate := RadarIncidentDTO{
				Name: impact.IncidentName, Status: impact.IncidentStatus, Impact: incidentImpactForServiceStatus(impact.Status),
				CreatedAt: createdAt, ResolvedAt: resolvedAt,
			}
			current, exists := incidentsByID[impact.IncidentID]
			if !exists || serviceStatusSeverity(impact.Status) > serviceStatusSeverity(serviceStatusForIncidentImpact(current.Impact)) {
				incidentsByID[impact.IncidentID] = candidate
			}
		}
		incidents := make([]RadarIncidentDTO, 0, len(incidentsByID))
		for _, incident := range incidentsByID {
			incidents = append(incidents, incident)
		}
		sort.Slice(incidents, func(left, right int) bool { return incidents[left].CreatedAt.After(incidents[right].CreatedAt) })
		days = append(days, ServiceHealthHistoryDayDTO{
			Date: dayStart.Format(time.DateOnly), Status: status, Incidents: incidents,
		})
	}
	return days
}

func containsStatuspageGroup(groups []string, expected string) bool {
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group), expected) {
			return true
		}
	}
	return false
}

func incidentImpactForServiceStatus(status ServiceStatus) string {
	switch status {
	case ServiceStatusMajorOutage:
		return "critical"
	case ServiceStatusPartialOutage:
		return "major"
	case ServiceStatusDegradedPerformance:
		return "minor"
	case ServiceStatusUnderMaintenance:
		return "maintenance"
	default:
		return "none"
	}
}

func serviceStatusForIncidentImpact(impact string) ServiceStatus {
	switch strings.ToLower(strings.TrimSpace(impact)) {
	case "critical":
		return ServiceStatusMajorOutage
	case "major":
		return ServiceStatusPartialOutage
	case "minor":
		return ServiceStatusDegradedPerformance
	case "maintenance":
		return ServiceStatusUnderMaintenance
	case "none":
		return ServiceStatusOperational
	default:
		return ServiceStatusUnknown
	}
}

func mergeServiceHealthHistory(left, right []ServiceHealthHistoryDayDTO) []ServiceHealthHistoryDayDTO {
	byDate := make(map[string]ServiceHealthHistoryDayDTO, len(left)+len(right))
	for _, day := range append(append([]ServiceHealthHistoryDayDTO(nil), left...), right...) {
		current, exists := byDate[day.Date]
		if !exists {
			current = ServiceHealthHistoryDayDTO{Date: day.Date, Status: day.Status, Incidents: []RadarIncidentDTO{}}
		}
		if serviceStatusSeverity(day.Status) > serviceStatusSeverity(current.Status) {
			current.Status = day.Status
		}
		seen := make(map[string]struct{}, len(current.Incidents))
		for _, incident := range current.Incidents {
			seen[incident.Name+"\x00"+incident.CreatedAt.Format(time.RFC3339Nano)] = struct{}{}
		}
		for _, incident := range day.Incidents {
			key := incident.Name + "\x00" + incident.CreatedAt.Format(time.RFC3339Nano)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			current.Incidents = append(current.Incidents, incident)
		}
		byDate[day.Date] = current
	}
	result := make([]ServiceHealthHistoryDayDTO, 0, len(byDate))
	for _, day := range byDate {
		result = append(result, day)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Date < result[right].Date })
	return result
}

func statuspageIncidentMatches(
	source RadarSourceKey,
	serviceKey ServiceKey,
	matchedIDs map[string]struct{},
	components []StatuspageIncidentComponent,
) bool {
	for _, component := range components {
		if component.ID != "" {
			if _, ok := matchedIDs[normalizeStatuspageComponentID(component.ID)]; ok {
				return true
			}
			continue
		}
		if component.Name != "" && statuspageComponentMatches(source, serviceKey, component.Name) {
			return true
		}
	}
	return false
}

func unknownStatuspageCard(descriptor RadarServiceDescriptor) ServiceHealthDTO {
	return ServiceHealthDTO{
		ServiceKey:      descriptor.Key,
		Name:            descriptor.Name,
		Status:          ServiceStatusUnknown,
		StatusIndicator: StatusIndicatorUnknown,
		SourceURL:       statuspagePublicURLForService(descriptor.Key),
	}
}

func statuspagePublicURLForService(serviceKey ServiceKey) string {
	switch serviceKey {
	case ServiceKeyClaudeAPI, ServiceKeyClaudeCode:
		return claudeStatuspagePublicURL
	case ServiceKeyOpenAIAPI, ServiceKeyCodexWeb:
		return openAIStatuspagePublicURL
	case ServiceKeyWindsurf:
		return windsurfStatuspagePublicURL
	case ServiceKeyDeepSeek:
		return deepSeekStatuspagePublicURL
	case ServiceKeyKimi:
		return kimiStatuspagePublicURL
	case ServiceKeyMiniMax:
		return miniMaxChinaStatuspagePublicURL
	default:
		return ""
	}
}

func cloneServiceHealthDTO(input ServiceHealthDTO) ServiceHealthDTO {
	result := input
	result.History30d = make([]ServiceHealthHistoryDayDTO, len(input.History30d))
	for index, day := range input.History30d {
		result.History30d[index] = day
		result.History30d[index].Incidents = make([]RadarIncidentDTO, len(day.Incidents))
		for incidentIndex, incident := range day.Incidents {
			result.History30d[index].Incidents[incidentIndex] = incident
			result.History30d[index].Incidents[incidentIndex].CreatedAt = incident.CreatedAt.UTC()
			if incident.ResolvedAt != nil {
				resolvedAt := incident.ResolvedAt.UTC()
				result.History30d[index].Incidents[incidentIndex].ResolvedAt = &resolvedAt
			}
		}
	}
	if input.Uptime90d != nil {
		value := *input.Uptime90d
		result.Uptime90d = &value
	}
	if input.LastUpdatedAt != nil {
		value := input.LastUpdatedAt.UTC()
		result.LastUpdatedAt = &value
	}
	if input.LastIncident != nil {
		incident := *input.LastIncident
		incident.CreatedAt = incident.CreatedAt.UTC()
		if input.LastIncident.ResolvedAt != nil {
			resolvedAt := input.LastIncident.ResolvedAt.UTC()
			incident.ResolvedAt = &resolvedAt
		}
		result.LastIncident = &incident
	}
	return result
}
