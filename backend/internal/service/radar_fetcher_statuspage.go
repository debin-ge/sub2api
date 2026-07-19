package service

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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

var miniMaxHistoryTimestampPattern = regexp.MustCompile(`^([A-Z][a-z]{2}) <var data-var='date'>([0-9]{1,2})</var>, <var data-var='time'>([0-9]{2}:[0-9]{2})</var> - <var data-var='time'>([0-9]{2}:[0-9]{2})</var> CST$`)

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

type statuspageSummaryWire struct {
	Page                      *statuspagePageWire        `json:"page"`
	Status                    *statuspageOverallWire     `json:"status"`
	Components                *[]statuspageComponentWire `json:"components"`
	Incidents                 []statuspageIncidentWire   `json:"incidents"`
	RadarHistoryCoverageStart *string                    `json:"radar_history_coverage_start,omitempty"`
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

type statuspageHistoryFetcher struct {
	source    RadarSourceKey
	interval  time.Duration
	summary   RadarFetcher
	incidents RadarFetcher
	auxiliary RadarFetcher
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
	case RadarSourceStatusMiniMaxChina:
		auxiliary, err = newRadarHTTPFetcher(radarHTTPFetcherOptions{
			source: source, interval: interval, client: client, endpoint: miniMaxChinaLLMHistoryURL,
			maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
			validate: func(payload []byte) error {
				_, _, err := decodeMiniMaxChinaHistory(payload)
				return err
			},
		})
	}
	if err != nil {
		return nil, err
	}
	return &statuspageHistoryFetcher{
		source: source, interval: interval, summary: summary, incidents: incidents, auxiliary: auxiliary,
	}, nil
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
	merged, err := mergeStatuspageHistoryPayloads(f.source, summary.payload, incidents.payload, auxiliary.payload)
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

func mergeStatuspageHistoryPayloads(source RadarSourceKey, summaryPayload, incidentsPayload, auxiliaryPayload []byte) ([]byte, error) {
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
	switch source {
	case RadarSourceStatusOpenAI:
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
		coverageLimit = openAIIncidentHistoryLimit
	case RadarSourceStatusMiniMaxChina:
		historyIncidents, historyStart, historyErr := decodeMiniMaxChinaHistory(auxiliaryPayload)
		if historyErr != nil {
			return nil, errInvalidStatuspageResponse
		}
		incidents, err = mergeStatuspageIncidents(incidents, historyIncidents)
		if err != nil {
			return nil, errInvalidStatuspageResponse
		}
		explicitCoverageStart = &historyStart
	case RadarSourceStatusClaude, RadarSourceStatusWindsurf, RadarSourceStatusKimi, RadarSourceStatusMiniMaxGlobal:
		if len(auxiliaryPayload) != 0 {
			return nil, errInvalidStatuspageResponse
		}
	default:
		return nil, &RadarFetcherConfigError{Field: "statuspage_source"}
	}
	summaryWire.Incidents = encodeStatuspageIncidents(incidents)
	summaryWire.Page.UpdatedAt = effectiveUpdatedAt.Format(time.RFC3339Nano)
	windowStart := statuspageDayStart(source, effectiveUpdatedAt).AddDate(0, 0, -(serviceHealthHistoryDays - 1))
	coverageStart := windowStart
	if explicitCoverageStart != nil {
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

type miniMaxHistoryProps struct {
	Components      []miniMaxHistoryComponent `json:"components"`
	Months          []miniMaxHistoryMonth     `json:"months"`
	ComponentFilter []string                  `json:"component_filter"`
	StartTime       string                    `json:"start_time"`
	EndTime         string                    `json:"end_time"`
}

type miniMaxHistoryComponent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type miniMaxHistoryMonth struct {
	Name      string                   `json:"name"`
	Year      int                      `json:"year"`
	Days      int                      `json:"days"`
	Incidents []miniMaxHistoryIncident `json:"incidents"`
}

type miniMaxHistoryIncident struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Impact    string `json:"impact"`
	Timestamp string `json:"timestamp"`
}

func decodeMiniMaxChinaHistory(payload []byte) ([]StatuspageIncident, time.Time, error) {
	document, err := html.Parse(bytes.NewReader(payload))
	if err != nil {
		return nil, time.Time{}, errInvalidStatuspageResponse
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
		return nil, time.Time{}, errInvalidStatuspageResponse
	}
	var props miniMaxHistoryProps
	if !decodeStatuspageJSON([]byte(propsValue), &props) || len(props.Components) == 0 || len(props.Components) > 64 || len(props.ComponentFilter) != 1 ||
		props.ComponentFilter[0] != miniMaxChinaLLMComponentID || len(props.Months) == 0 || len(props.Months) > statuspageHistoryMaxMonths {
		return nil, time.Time{}, errInvalidStatuspageResponse
	}
	foundComponent := false
	for _, component := range props.Components {
		if component.ID == miniMaxChinaLLMComponentID && component.Name == miniMaxChinaLLMComponentName {
			foundComponent = true
		}
	}
	if !foundComponent {
		return nil, time.Time{}, errInvalidStatuspageResponse
	}
	startValue, err := time.Parse(time.RFC3339Nano, props.StartTime)
	if err != nil || startValue.IsZero() {
		return nil, time.Time{}, errInvalidStatuspageResponse
	}
	endValue, err := time.Parse(time.RFC3339Nano, props.EndTime)
	if err != nil || endValue.IsZero() || !endValue.After(startValue) {
		return nil, time.Time{}, errInvalidStatuspageResponse
	}
	_, startOffset := startValue.Zone()
	_, endOffset := endValue.Zone()
	if startOffset != 8*60*60 || endOffset != 8*60*60 {
		return nil, time.Time{}, errInvalidStatuspageResponse
	}
	start := startValue.UTC()
	end := endValue.UTC()
	incidents := make([]StatuspageIncident, 0)
	seen := make(map[string]struct{})
	for _, month := range props.Months {
		if month.Year < 2020 || month.Year > 2100 || month.Days < 28 || month.Days > 31 {
			return nil, time.Time{}, errInvalidStatuspageResponse
		}
		for _, incident := range month.Incidents {
			if len(incidents) >= statuspageHistoryMaxIncidents || strings.TrimSpace(incident.Code) == "" || len(incident.Code) > 128 ||
				strings.ContainsAny(incident.Code, "/?#\\\x00\r\n\t ") ||
				strings.TrimSpace(incident.Name) == "" || serviceStatusForIncidentImpact(incident.Impact) == ServiceStatusUnknown {
				return nil, time.Time{}, errInvalidStatuspageResponse
			}
			if _, duplicate := seen[incident.Code]; duplicate {
				return nil, time.Time{}, errInvalidStatuspageResponse
			}
			createdAt, resolvedAt, err := parseMiniMaxHistoryTimestamp(month, incident.Timestamp)
			if err != nil || createdAt.Before(start) || resolvedAt.After(end) {
				return nil, time.Time{}, errInvalidStatuspageResponse
			}
			seen[incident.Code] = struct{}{}
			incidents = append(incidents, StatuspageIncident{
				ID: incident.Code, Name: strings.TrimSpace(incident.Name), Status: "resolved", Impact: strings.TrimSpace(incident.Impact),
				CreatedAt: createdAt, ResolvedAt: &resolvedAt,
				Components: []StatuspageIncidentComponent{{ID: miniMaxChinaLLMComponentID, Name: miniMaxChinaLLMComponentName}},
			})
		}
	}
	return incidents, start, nil
}

func parseMiniMaxHistoryTimestamp(month miniMaxHistoryMonth, raw string) (time.Time, time.Time, error) {
	matches := miniMaxHistoryTimestampPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(matches) != 5 || !strings.EqualFold(matches[1], miniMaxMonthAbbreviation(month.Name)) {
		return time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	location := time.FixedZone("CST", 8*60*60)
	createdAt, err := time.ParseInLocation("Jan 2 2006 15:04", fmt.Sprintf("%s %s %d %s", matches[1], matches[2], month.Year, matches[3]), location)
	if err != nil || createdAt.Month().String() != month.Name {
		return time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	resolvedAt, err := time.ParseInLocation("Jan 2 2006 15:04", fmt.Sprintf("%s %s %d %s", matches[1], matches[2], month.Year, matches[4]), location)
	if err != nil || resolvedAt.Before(createdAt) {
		return time.Time{}, time.Time{}, errInvalidStatuspageResponse
	}
	return createdAt.UTC(), resolvedAt.UTC(), nil
}

func miniMaxMonthAbbreviation(name string) string {
	for _, month := range []time.Month{time.January, time.February, time.March, time.April, time.May, time.June, time.July, time.August, time.September, time.October, time.November, time.December} {
		if month.String() == name {
			return month.String()[:3]
		}
	}
	return ""
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
	relevant := make([]StatuspageIncident, 0, len(summary.Incidents))
	ambiguous := make([]StatuspageIncident, 0)
	for _, incident := range summary.Incidents {
		if len(incident.Components) == 0 {
			ambiguous = append(ambiguous, incident)
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
