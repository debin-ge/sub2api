package service

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const validStatuspagePayload = `{
  "page":{"id":"page","name":"Status","url":"https://untrusted-payload.example","updated_at":"2026-07-10T10:30:00+08:00"},
  "status":{"indicator":"none","description":"All Systems Operational"},
  "components":[
    {"id":"component","name":"Claude API","status":"operational","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:00:00Z","group":false}
  ],
  "incidents":[]
}`

const validStatuspageIncidentsPayload = `{
  "page":{"id":"page","name":"Status","url":"https://untrusted-payload.example","updated_at":"2026-07-10T10:30:00+08:00"},
  "incidents":[]
}`

type statuspageFixtureComponent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func statuspageFixtureComponents(source RadarSourceKey) []statuspageFixtureComponent {
	switch source {
	case RadarSourceStatusClaude:
		return []statuspageFixtureComponent{{ID: "k8w3r06qmzrp", Name: "Claude API (api.anthropic.com)"}, {ID: "yyzkbfz2thpt", Name: "Claude Code"}}
	case RadarSourceStatusOpenAI:
		return []statuspageFixtureComponent{{ID: "responses", Name: "Responses"}}
	case RadarSourceStatusWindsurf:
		return []statuspageFixtureComponent{{ID: "8q19cygxvshj", Name: "Windsurf Tab"}, {ID: "r5wf1ykd7y1m", Name: "Cascade"}}
	case RadarSourceStatusKimi:
		return []statuspageFixtureComponent{
			{ID: "8psr5dfdld0s", Name: "Open API"}, {ID: "8rkd3yj051gl", Name: "Vision Model"},
			{ID: "lk7q3z0fcylp", Name: "Thinking Model"}, {ID: "p1j9ttb7jwhp", Name: "Text Model"},
			{ID: "rf64wcbxt3r2", Name: "API Service"}, {ID: "wmn9wzv84k1v", Name: "Research Model"},
			{ID: "x0zsqgy57b75", Name: "Model"}, {ID: "z2zfp65lvb2z", Name: "K2 Model"},
		}
	case RadarSourceStatusMiniMaxGlobal:
		return []statuspageFixtureComponent{{ID: "pr0d8qr59svt", Name: "Large Language Models (LLM)"}}
	case RadarSourceStatusMiniMaxChina:
		return []statuspageFixtureComponent{{ID: miniMaxChinaLLMComponentID, Name: miniMaxChinaLLMComponentName}}
	default:
		return nil
	}
}

func statuspageSummaryFixture(t *testing.T, source RadarSourceKey) string {
	t.Helper()
	components := statuspageFixtureComponents(source)
	wires := make([]map[string]any, 0, len(components))
	for _, component := range components {
		wires = append(wires, map[string]any{
			"id": component.ID, "name": component.Name, "status": "operational", "group": false,
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-07-10T10:00:00Z",
		})
	}
	payload, err := json.Marshal(map[string]any{
		"page":       map[string]any{"id": "page", "name": "Status", "url": "https://payload.example", "updated_at": "2026-07-10T10:30:00Z"},
		"status":     map[string]any{"indicator": "none", "description": "All Systems Operational"},
		"components": wires,
	})
	require.NoError(t, err)
	return string(payload)
}

func statuspageCalendarFixture(t *testing.T, source RadarSourceKey, filter string, julyIncidents ...map[string]any) string {
	t.Helper()
	names := make(map[string]string)
	for _, component := range statuspageFixtureComponents(source) {
		names[component.ID] = component.Name
	}
	ids := strings.Split(filter, ",")
	components := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		name, ok := names[id]
		require.True(t, ok, "unknown fixture component %s", id)
		components = append(components, map[string]any{"id": id, "name": name, "group": false})
	}
	start := "2026-06-01T00:00:00Z"
	end := "2026-07-31T23:59:59Z"
	if source == RadarSourceStatusKimi || source == RadarSourceStatusMiniMaxChina {
		start = "2026-06-01T00:00:00+08:00"
		end = "2026-07-31T23:59:59+08:00"
	}
	props, err := json.Marshal(map[string]any{
		"components": components,
		"months": []map[string]any{
			{"name": "July", "year": 2026, "days": 31, "incidents": julyIncidents},
			{"name": "June", "year": 2026, "days": 30, "incidents": []any{}},
		},
		"component_filter": ids, "start_time": start, "end_time": end,
	})
	require.NoError(t, err)
	return `<div data-react-class="HistoryIndex" data-react-props="` + html.EscapeString(string(props)) + `"></div>`
}

func TestStatuspageFetchersUseFixedHTTPSEndpointsAndConfiguredInterval(t *testing.T) {
	tests := []struct {
		name     string
		source   RadarSourceKey
		wantHost string
	}{
		{name: "Claude", source: RadarSourceStatusClaude, wantHost: "status.claude.com"},
		{name: "OpenAI", source: RadarSourceStatusOpenAI, wantHost: "status.openai.com"},
		{name: "Windsurf", source: RadarSourceStatusWindsurf, wantHost: "status.windsurf.com"},
		{name: "Kimi", source: RadarSourceStatusKimi, wantHost: "status.moonshot.cn"},
		{name: "MiniMax global", source: RadarSourceStatusMiniMaxGlobal, wantHost: "status.minimax.io"},
		{name: "MiniMax China", source: RadarSourceStatusMiniMaxChina, wantHost: "status.minimaxi.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedRequests := 3
			switch tt.source {
			case RadarSourceStatusClaude:
				expectedRequests = 4
			case RadarSourceStatusOpenAI:
				expectedRequests = 5
			}
			captured := make(chan *http.Request, expectedRequests)
			client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
				captured <- req.Clone(req.Context())
				payload := statuspageSummaryFixture(t, tt.source)
				switch {
				case req.URL.String() == openAIStatuspageFeedURL:
					payload = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><id>https://status.openai.com/</id><title>OpenAI status</title><updated>2026-07-10T10:30:00Z</updated><generator>incident.io</generator></feed>`
				case strings.HasPrefix(req.URL.String(), openAIComponentImpactsURL):
					payload = `{"component_impacts":[],"incident_links":[],"component_uptimes":[]}`
				case req.URL.String() == openAIStatusSummaryURL:
					payload = `{"summary":{"components":[{"id":"responses","name":"Responses"}],"structure":{"items":[{"group":{"id":"apis","name":"APIs","components":[{"component_id":"responses","name":"Responses"}]}}]}}}`
				case req.URL.EscapedPath() == "/history":
					payload = statuspageCalendarFixture(t, tt.source, req.URL.Query().Get("filter"))
				default:
					if req.URL.EscapedPath() == "/api/v2/incidents.json" {
						payload = validStatuspageIncidentsPayload
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload)}, nil
			})
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.StatuspageIntervalMinutes = 47

			fetcher, err := NewStatuspageFetcher(cfg, tt.source, client)
			require.NoError(t, err)
			require.Equal(t, tt.source, fetcher.Source())
			require.Equal(t, 47*time.Minute, fetcher.Interval())

			payload, meta, err := fetcher.Fetch(context.Background())

			require.NoError(t, err)
			summary, decodeErr := DecodeStatuspageSummary(payload)
			require.NoError(t, decodeErr)
			require.NotNil(t, summary.HistoryCoverageStart)
			endpoints := make(map[string]struct{}, expectedRequests)
			for range expectedRequests {
				request := <-captured
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "https", request.URL.Scheme)
				require.Equal(t, tt.wantHost, request.URL.Host)
				require.Empty(t, request.Header.Get("x-api-key"))
				endpoints[request.URL.String()] = struct{}{}
			}
			require.Contains(t, endpoints, "https://"+tt.wantHost+"/api/v2/summary.json")
			require.Contains(t, endpoints, "https://"+tt.wantHost+"/api/v2/incidents.json")
			if tt.source == RadarSourceStatusOpenAI {
				require.Contains(t, endpoints, openAIStatuspageFeedURL)
				require.Contains(t, endpoints, openAIStatusSummaryURL)
				foundTimeline := false
				for endpoint := range endpoints {
					if strings.HasPrefix(endpoint, openAIComponentImpactsURL+"?") {
						foundTimeline = true
						requestURL, parseErr := url.Parse(endpoint)
						require.NoError(t, parseErr)
						start, startErr := time.Parse(time.RFC3339Nano, requestURL.Query().Get("start_at"))
						end, endErr := time.Parse(time.RFC3339Nano, requestURL.Query().Get("end_at"))
						require.NoError(t, startErr)
						require.NoError(t, endErr)
						require.Equal(t, serviceHealthHistoryDays, int(end.Sub(start).Hours()/24))
					}
				}
				require.True(t, foundTimeline)
			}
			if tt.source != RadarSourceStatusOpenAI {
				for _, spec := range statuspageCalendarSpecs(tt.source) {
					require.Contains(t, endpoints, spec.endpoint)
				}
			}
			require.Nil(t, meta.Error)
		})
	}
}

func TestStatuspageFetcherRejectsZeroTimestampWithoutRecordingSuccess(t *testing.T) {
	payloadJSON := `{
      "page":{"id":"page","name":"Status","url":"https://payload.example","updated_at":"0001-01-01T00:00:00Z"},
      "status":{"indicator":"none","description":"OK"},
      "components":[]
    }`
	var attempts atomic.Int32
	client := radarDoerFunc(func(request *http.Request) (*http.Response, error) {
		attempts.Add(1)
		payload := payloadJSON
		if request.URL.EscapedPath() == "/api/v2/incidents.json" {
			payload = validStatuspageIncidentsPayload
		}
		return &http.Response{StatusCode: http.StatusOK, Body: newRadarTrackingBody(payload)}, nil
	})
	fetcher, err := NewStatuspageFetcher(validRadarFetcherTestConfig(), RadarSourceStatusClaude, client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.GreaterOrEqual(t, attempts.Load(), int32(2))
	require.LessOrEqual(t, attempts.Load(), int32(4))
	require.Nil(t, meta.LastSuccessAt)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeInvalidResponse)
}

func TestDecodeAndMapStatuspageSummariesCanonicalOrderAndSafeMatching(t *testing.T) {
	claude, err := DecodeStatuspageSummary([]byte(`{
      "page":{"id":"claude","name":"Claude Status","url":"https://payload.example/claude","updated_at":"2026-07-10T10:30:00Z"},
      "status":{"indicator":"critical","description":"payload summary is not authoritative"},
      "components":[
        {"id":"claude-code-lure","name":"Claude Code API","status":"major_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:50:00Z"},
        {"id":"claude-api","name":"  cLaUdE aPi (api.anthropic.com)  ","status":"degraded_performance","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:10:00+02:00"},
        {"id":"third-party","name":"Third-party Claude API","status":"major_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:45:00Z"},
        {"id":"claude-code","name":" CLAUDE CODE ","status":"under_maintenance","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:20:00Z"}
      ],
      "incidents":[]
    }`))
	require.NoError(t, err)

	openAI, err := DecodeStatuspageSummary([]byte(`{
      "page":{"id":"openai","name":"OpenAI Status","url":"https://payload.example/openai","updated_at":"2026-07-10T11:30:00Z"},
      "status":{"indicator":"none","description":"All Systems Operational"},
      "components":[
        {"id":"conversations","name":"Conversations","status":"major_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:59:00Z"},
        {"id":"codex-lure","name":"My Codex API proxy","status":"major_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:58:00Z"},
        {"id":"responses","name":"Responses","status":"operational","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:10:00Z"},
        {"id":"batch","name":"Batch API","status":"degraded_performance","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:20:00Z"},
        {"id":"codex-api","name":"Codex API","status":"major_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:30:00Z"},
        {"id":"embeddings","name":"Embeddings","status":"new_upstream_state","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T13:40:00+02:00"},
        {"id":"codex-web","name":" Codex in ChatGPT Desktop ","status":"partial_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:15:00Z"},
        {"id":"login","name":"Login","status":"major_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:57:00Z"},
        {"id":"gpts","name":"GPTs","status":"major_outage","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T11:56:00Z"}
      ]
    }`))
	require.NoError(t, err, "OpenAI currently omits incidents and that must remain valid")
	require.NotNil(t, openAI.Incidents)
	require.Empty(t, openAI.Incidents)

	claudeCards, err := MapStatuspageServiceHealth(RadarSourceStatusClaude, claude)
	require.NoError(t, err)
	openAICards, err := MapStatuspageServiceHealth(RadarSourceStatusOpenAI, openAI)
	require.NoError(t, err)
	require.Len(t, claudeCards, 2)
	require.Len(t, openAICards, 2)

	cards := MergeStatuspageServiceHealth(claudeCards, openAICards)
	require.Equal(t, []ServiceKey{
		ServiceKeyClaudeAPI,
		ServiceKeyClaudeCode,
		ServiceKeyCodexWeb,
		ServiceKeyOpenAIAPI,
	}, []ServiceKey{cards[0].ServiceKey, cards[1].ServiceKey, cards[2].ServiceKey, cards[3].ServiceKey})

	requireServiceHealthCard(t, cards[0], "Claude API", ServiceStatusDegradedPerformance, StatusIndicatorMinor, "https://status.claude.com")
	require.Equal(t, time.Date(2026, 7, 10, 8, 10, 0, 0, time.UTC), *cards[0].LastUpdatedAt)
	requireServiceHealthCard(t, cards[1], "Claude Code", ServiceStatusUnderMaintenance, StatusIndicatorMinor, "https://status.claude.com")
	require.Equal(t, time.Date(2026, 7, 10, 10, 20, 0, 0, time.UTC), *cards[1].LastUpdatedAt)
	requireServiceHealthCard(t, cards[2], "Codex Web", ServiceStatusPartialOutage, StatusIndicatorMajor, "https://status.openai.com")
	require.Equal(t, time.Date(2026, 7, 10, 11, 15, 0, 0, time.UTC), *cards[2].LastUpdatedAt)
	requireServiceHealthCard(t, cards[3], "OpenAI API", ServiceStatusMajorOutage, StatusIndicatorCritical, "https://status.openai.com")
	require.Equal(t, time.Date(2026, 7, 10, 11, 40, 0, 0, time.UTC), *cards[3].LastUpdatedAt, "latest matched component wins even when its status is unknown")
}

func TestMapStatuspageServiceHealthBuildsThirtyDayHistoryWithoutInventingCoverage(t *testing.T) {
	updatedAt := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	coverageStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	resolvedAt := time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC)
	summary := StatuspageSummary{
		Page:   StatuspagePage{ID: "claude", Name: "Claude", UpdatedAt: updatedAt},
		Status: StatuspageOverallStatus{Indicator: "none", Description: "Operational"},
		Components: []StatuspageComponent{{
			ID: "api", Name: "Claude API", Status: "operational",
			CreatedAt: updatedAt.AddDate(-1, 0, 0), UpdatedAt: updatedAt,
		}},
		Incidents: []StatuspageIncident{
			{
				ID: "minor", Name: "Elevated latency", Status: "resolved", Impact: "minor",
				CreatedAt: time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC), ResolvedAt: radarHistoryTime(time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)),
				Components: []StatuspageIncidentComponent{{ID: "api", Name: "Claude API"}},
			},
			{
				ID: "critical", Name: "API outage", Status: "resolved", Impact: "critical",
				CreatedAt: time.Date(2026, time.July, 12, 23, 0, 0, 0, time.UTC), ResolvedAt: &resolvedAt,
				Components: []StatuspageIncidentComponent{{ID: "api", Name: "Claude API"}},
			},
		},
		HistoryCoverageStart: &coverageStart,
	}

	cards, err := MapStatuspageServiceHealth(RadarSourceStatusClaude, summary)
	require.NoError(t, err)
	require.Len(t, cards[0].History30d, serviceHealthHistoryDays)
	require.Equal(t, serviceHealthHistoryDays, cards[0].HistoryDays)
	require.Equal(t, serviceHealthUptimeWindowDays, cards[0].UptimeWindowDays)
	require.Equal(t, serviceHealthRecentIncidentDays, cards[0].RecentIncidentDays)
	require.Equal(t, serviceHealthIncidentPreviewLimit, cards[0].IncidentPreviewLimit)
	require.Equal(t, "2026-06-17", cards[0].History30d[0].Date)
	require.Equal(t, ServiceStatusUnknown, cards[0].History30d[0].Status)
	require.NotNil(t, cards[0].History30d[0].Incidents)
	byDate := make(map[string]ServiceHealthHistoryDayDTO, len(cards[0].History30d))
	for _, day := range cards[0].History30d {
		byDate[day.Date] = day
	}
	require.Equal(t, ServiceStatusOperational, byDate["2026-07-11"].Status)
	require.Equal(t, ServiceStatusMajorOutage, byDate["2026-07-12"].Status)
	require.Equal(t, ServiceStatusMajorOutage, byDate["2026-07-13"].Status)
	require.Len(t, byDate["2026-07-12"].Incidents, 2)
}

func TestMergeOpenAIHistoryUsesOfficialFeedComponentsAndLatestFeedDate(t *testing.T) {
	summaryPayload := []byte(`{
      "page":{"id":"openai","name":"OpenAI","url":"https://status.openai.com/","updated_at":"2026-07-09T19:25:56Z"},
      "status":{"indicator":"none","description":"All Systems Operational"},
      "components":[
        {"id":"responses","name":"Responses","status":"operational","created_at":"2025-03-13T18:31:32Z","updated_at":"2026-07-09T19:25:56Z"},
        {"id":"conversations","name":"Conversations","status":"operational","created_at":"2025-02-25T01:31:15Z","updated_at":"2026-07-09T19:25:56Z"}
      ]
    }`)
	incidentsPayload := []byte(`{
      "page":{"id":"openai","name":"OpenAI","url":"https://status.openai.com/","updated_at":"2026-07-15T00:39:32Z"},
      "incidents":[
        {"id":"api-incident","name":"Responses API errors","status":"resolved","impact":"major","created_at":"2026-07-14T10:00:00Z","resolved_at":"2026-07-14T10:15:00Z"},
        {"id":"chatgpt-incident","name":"ChatGPT conversation errors","status":"resolved","impact":"critical","created_at":"2026-07-14T11:00:00Z","resolved_at":"2026-07-14T11:15:00Z"},
        {"id":"unscoped-july-9","name":"Users are experiencing elevated errors when selecting models","status":"resolved","impact":"none","created_at":"2026-07-09T19:44:14Z","resolved_at":"2026-07-09T20:42:57Z"},
        {"id":"unscoped-july-10","name":"OpenAI website and Help Center content may be unavailable","status":"resolved","impact":"none","created_at":"2026-07-10T21:13:30Z","resolved_at":"2026-07-10T23:07:39Z"}
      ]
    }`)
	feedPayload := []byte(`<?xml version="1.0" encoding="utf-8"?>
      <feed xmlns="http://www.w3.org/2005/Atom">
        <id>https://status.openai.com/</id><title>OpenAI status</title><updated>2026-07-16T04:06:23.122Z</updated><generator>incident.io</generator>
        <entry><title>Responses API errors</title><id>https://status.openai.com//incidents/api-incident</id><updated>2026-07-14T10:15:00Z</updated><content type="html"><![CDATA[<b>Affected components</b><ul><li>Responses (Operational)</li></ul>]]></content></entry>
        <entry><title>ChatGPT conversation errors</title><id>https://status.openai.com//incidents/chatgpt-incident</id><updated>2026-07-14T11:15:00Z</updated><content type="html"><![CDATA[<b>Affected components</b><ul><li>Conversations (Operational)</li></ul>]]></content></entry>
		<entry><title>Users are experiencing elevated errors when selecting models</title><id>https://status.openai.com//incidents/unscoped-july-9</id><updated>2026-07-09T20:42:57Z</updated><content type="html"><![CDATA[<b>Status: Resolved</b>]]></content></entry>
		<entry><title>OpenAI website and Help Center content may be unavailable</title><id>https://status.openai.com//incidents/unscoped-july-10</id><updated>2026-07-10T23:07:39Z</updated><content type="html"><![CDATA[<b>Status: Resolved</b>]]></content></entry>
      </feed>`)
	componentImpactsPayload := []byte(`{
	  "radar_coverage_start":"2026-06-17T00:00:00Z",
	  "radar_coverage_end":"2026-07-17T00:00:00Z",
      "component_impacts":[
        {"id":"impact-api","component_id":"responses","status_page_incident_id":"api-incident","start_at":"2026-07-14T10:00:00Z","end_at":"2026-07-14T10:15:00Z","status":"partial_outage"},
        {"id":"impact-historical-sora","component_id":"historical-sora","status_page_incident_id":"sora-incident","start_at":"2026-07-11T06:35:19Z","end_at":"2026-07-11T07:03:51Z","status":"full_outage"}
      ],
      "incident_links":[
        {"id":"api-incident","name":"Responses API errors","status":"resolved","published_at":"2026-07-14T10:00:00Z"},
        {"id":"sora-incident","name":"Elevated Errors for Sora API","status":"resolved","published_at":"2026-07-11T06:35:19Z"},
        {"id":"unscoped-july-9","name":"Users are experiencing elevated errors when selecting models","status":"resolved","published_at":"2026-07-09T19:44:14Z"},
        {"id":"unscoped-july-10","name":"OpenAI website and Help Center content may be unavailable","status":"resolved","published_at":"2026-07-10T21:13:30Z"}
      ]
    }`)
	catalogPayload := []byte(`{
      "summary":{
        "components":[
          {"id":"responses","name":"Responses"},
          {"id":"historical-sora","name":"Sora"}
        ],
        "structure":{"items":[
          {"group":{"id":"apis","name":"APIs","components":[
            {"component_id":"responses","name":"Responses"},
            {"component_id":"historical-sora","name":"Sora"}
          ]}}
        ]}
      }
    }`)

	merged, err := mergeStatuspageHistoryPayloads(RadarSourceStatusOpenAI, summaryPayload, incidentsPayload, feedPayload, componentImpactsPayload, catalogPayload, nil)
	require.NoError(t, err)
	summary, err := DecodeStatuspageSummary(merged)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.July, 16, 23, 59, 59, 999999999, time.UTC), summary.Page.UpdatedAt)

	cards, err := MapStatuspageServiceHealth(RadarSourceStatusOpenAI, summary)
	require.NoError(t, err)
	apiHistory := historyDayByDate(t, cards[1].History30d, "2026-07-14")
	require.Equal(t, ServiceStatusPartialOutage, apiHistory.Status)
	require.Len(t, apiHistory.Incidents, 1)
	require.Equal(t, "Responses API errors", apiHistory.Incidents[0].Name)
	require.NotContains(t, apiHistory.Incidents[0].Name, "ChatGPT")
	historicalAPIHistory := historyDayByDate(t, cards[1].History30d, "2026-07-11")
	require.Equal(t, ServiceStatusMajorOutage, historicalAPIHistory.Status)
	require.Len(t, historicalAPIHistory.Incidents, 1)
	require.Equal(t, "Elevated Errors for Sora API", historicalAPIHistory.Incidents[0].Name)
	require.Equal(t, ServiceStatusOperational, historyDayByDate(t, cards[0].History30d, "2026-07-11").Status)
	for _, date := range []string{"2026-07-09", "2026-07-10"} {
		for _, card := range cards {
			day := historyDayByDate(t, card.History30d, date)
			require.Equal(t, ServiceStatusOperational, day.Status, date)
			require.Empty(t, day.Incidents, date)
		}
	}
}

func TestMergeMiniMaxChinaHistoryUsesFilteredOfficialCalendarBeyondIncidentLimit(t *testing.T) {
	summaryPayload := []byte(`{
      "page":{"id":"minimax-cn","name":"MiniMax","url":"https://status.minimaxi.com","updated_at":"2026-07-16T11:31:46+08:00"},
      "status":{"indicator":"none","description":"All Systems Operational"},
      "components":[
        {"id":"vwp8mgy34fck","name":"大语言模型LLM","status":"operational","created_at":"2026-04-21T14:19:10+08:00","updated_at":"2026-07-15T02:02:45+08:00"},
        {"id":"speech","name":"语音合成","status":"operational","created_at":"2026-04-21T14:19:24+08:00","updated_at":"2026-07-15T17:34:47+08:00"}
      ]
    }`)
	incidentsPayload := []byte(`{
      "page":{"id":"minimax-cn","name":"MiniMax","url":"https://status.minimaxi.com","updated_at":"2026-07-16T11:31:46+08:00"},
      "incidents":[
        {"id":"recent-speech","name":"语音合成 错误率升高","status":"resolved","impact":"major","created_at":"2026-07-15T17:33:47+08:00","resolved_at":"2026-07-15T17:34:47+08:00","components":[{"id":"speech","name":"语音合成"}]}
      ]
    }`)
	props := `{
      "page_status":{"page":{"name":"MiniMax"}},
      "components":[{"id":"vwp8mgy34fck","name":"大语言模型LLM"},{"id":"speech","name":"语音合成"}],
      "months":[
        {"name":"July","year":2026,"starts_on":3,"days":31,"incidents":[
          {"code":"cn-july-14-a","name":"大语言模型 LLM 错误率升高","message":"该问题已解决。","impact":"major","timestamp":"Jul <var data-var='date'>14</var>, <var data-var='time'>14:31</var> - <var data-var='time'>14:32</var> CST"},
          {"code":"cn-july-14-b","name":"大语言模型 LLM 错误率升高","message":"该问题已解决。","impact":"major","timestamp":"Jul <var data-var='date'>14</var>, <var data-var='time'>16:02</var> - <var data-var='time'>16:04</var> CST"},
          {"code":"cn-july-15-a","name":"大语言模型 LLM 错误率升高","message":"该问题已解决。","impact":"major","timestamp":"Jul <var data-var='date'>15</var>, <var data-var='time'>00:34</var> - <var data-var='time'>00:36</var> CST"},
          {"code":"cn-july-15-b","name":"大语言模型 LLM 错误率升高","message":"该问题已解决。","impact":"major","timestamp":"Jul <var data-var='date'>15</var>, <var data-var='time'>02:00</var> - <var data-var='time'>02:02</var> CST"}
        ]},
        {"name":"June","year":2026,"starts_on":0,"days":30,"incidents":[
          {"code":"cn-june-24","name":"大语言模型 LLM 错误率升高","message":"该问题已解决。","impact":"major","timestamp":"Jun <var data-var='date'>24</var>, <var data-var='time'>08:04</var> - <var data-var='time'>08:08</var> CST"}
        ]}
      ],
      "show_component_filter":false,"show_uptime_calendar":true,"component_filter":["vwp8mgy34fck"],
      "start_time":"2026-05-01T00:00:00+08:00","end_time":"2026-07-31T23:59:59+08:00"
    }`
	historyPayload := []byte(`<html><body><div data-react-class="HistoryIndex" data-react-props="` + html.EscapeString(props) + `"></div></body></html>`)
	historyIncidents, coverageStart, coverageEnd, err := decodeStatuspageCalendarHistory(
		historyPayload, RadarSourceStatusMiniMaxChina, ServiceKeyMiniMax, []string{miniMaxChinaLLMComponentID},
	)
	require.NoError(t, err)
	calendarPayload, err := json.Marshal(statuspageCalendarBundleWire{
		CoverageStart: coverageStart.Format(time.RFC3339Nano), CoverageEnd: coverageEnd.Format(time.RFC3339Nano),
		Incidents: encodeStatuspageIncidents(historyIncidents),
	})
	require.NoError(t, err)

	merged, err := mergeStatuspageHistoryPayloads(RadarSourceStatusMiniMaxChina, summaryPayload, incidentsPayload, nil, nil, nil, calendarPayload)
	require.NoError(t, err)
	summary, err := DecodeStatuspageSummary(merged)
	require.NoError(t, err)
	cards, err := MapStatuspageServiceHealth(RadarSourceStatusMiniMaxChina, summary)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	oldDay := historyDayByDate(t, cards[0].History30d, "2026-06-24")
	require.Equal(t, ServiceStatusPartialOutage, oldDay.Status)
	require.Len(t, oldDay.Incidents, 1)
	require.Equal(t, time.Date(2026, time.June, 24, 0, 4, 0, 0, time.UTC), oldDay.Incidents[0].CreatedAt)
	july14 := historyDayByDate(t, cards[0].History30d, "2026-07-14")
	require.Len(t, july14.Incidents, 2, "only the two China-site LLM incidents must be present")
	require.Equal(t, ServiceStatusPartialOutage, july14.Status)
	july15 := historyDayByDate(t, cards[0].History30d, "2026-07-15")
	require.Len(t, july15.Incidents, 2, "China-site midnight incidents must stay on the official local calendar date")
	require.Equal(t, ServiceStatusPartialOutage, july15.Status)
}

func TestKimiCalendarHistoryUsesFilteredTruthInsteadOfUnscopedIncidents(t *testing.T) {
	summaryPayload := []byte(strings.Replace(
		statuspageSummaryFixture(t, RadarSourceStatusKimi),
		"2026-07-10T10:30:00Z", "2026-07-19T10:30:00+08:00", 1,
	))
	incidentsPayload := []byte(`{
      "page":{"id":"page","name":"Status","url":"https://payload.example","updated_at":"2026-07-19T10:30:00+08:00"},
      "incidents":[
        {"id":"unscoped-agentic","name":"Agentic 模型错误报警","status":"resolved","impact":"major","created_at":"2026-07-12T11:29:16+08:00","resolved_at":"2026-07-12T13:05:26+08:00","components":[]}
      ]
    }`)
	spec := statuspageCalendarSpecs(RadarSourceStatusKimi)[0]
	historyPayload := []byte(statuspageCalendarFixture(t, RadarSourceStatusKimi, strings.Join(spec.componentIDs, ","), map[string]any{
		"code": "kimi-api-july-13", "name": "API Service 错误率升高", "impact": "major",
		"timestamp": "Jul <var data-var='date'>13</var>, <var data-var='time'>09:11</var> - <var data-var='time'>09:35</var> CST",
	}))
	historyIncidents, coverageStart, coverageEnd, err := decodeStatuspageCalendarHistory(
		historyPayload, RadarSourceStatusKimi, ServiceKeyKimi, spec.componentIDs,
	)
	require.NoError(t, err)
	calendarPayload, err := json.Marshal(statuspageCalendarBundleWire{
		CoverageStart: coverageStart.Format(time.RFC3339Nano), CoverageEnd: coverageEnd.Format(time.RFC3339Nano),
		Incidents: encodeStatuspageIncidents(historyIncidents),
	})
	require.NoError(t, err)

	merged, err := mergeStatuspageHistoryPayloads(
		RadarSourceStatusKimi, summaryPayload, incidentsPayload, nil, nil, nil, calendarPayload,
	)
	require.NoError(t, err)
	summary, err := DecodeStatuspageSummary(merged)
	require.NoError(t, err)
	cards, err := MapStatuspageServiceHealth(RadarSourceStatusKimi, summary)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	july12 := historyDayByDate(t, cards[0].History30d, "2026-07-12")
	require.Equal(t, ServiceStatusOperational, july12.Status)
	require.Empty(t, july12.Incidents)
	july13 := historyDayByDate(t, cards[0].History30d, "2026-07-13")
	require.Equal(t, ServiceStatusPartialOutage, july13.Status)
	require.Len(t, july13.Incidents, 1)
	require.Equal(t, "API Service 错误率升高", july13.Incidents[0].Name)
}

func TestStatuspageCalendarHistoryParsesCrossDayAndCrossMonthIncidents(t *testing.T) {
	payload := []byte(statuspageCalendarFixture(t, RadarSourceStatusClaude, "k8w3r06qmzrp",
		map[string]any{
			"code": "cross-month", "name": "Long-running API incident", "impact": "critical",
			"timestamp": "Jun <var data-var='date'>13</var>, <var data-var='time'>00:50</var> - Jul <var data-var='date'>1</var>, <var data-var='time'>19:26</var> UTC",
		},
		map[string]any{
			"code": "cross-day", "name": "Overnight API incident", "impact": "minor",
			"timestamp": "Jul <var data-var='date'>3</var>, <var data-var='time'>23:42</var> - Jul <var data-var='date'>4</var>, <var data-var='time'>00:20</var> UTC",
		},
	))

	incidents, _, _, err := decodeStatuspageCalendarHistory(
		payload, RadarSourceStatusClaude, ServiceKeyClaudeAPI, []string{"k8w3r06qmzrp"},
	)
	require.NoError(t, err)
	require.Len(t, incidents, 2)
	require.Equal(t, time.Date(2026, time.July, 3, 23, 42, 0, 0, time.UTC), incidents[0].CreatedAt)
	require.Equal(t, time.Date(2026, time.July, 4, 0, 20, 0, 0, time.UTC), *incidents[0].ResolvedAt)
	require.Equal(t, time.Date(2026, time.June, 13, 0, 50, 0, 0, time.UTC), incidents[1].CreatedAt)
	require.Equal(t, time.Date(2026, time.July, 1, 19, 26, 0, 0, time.UTC), *incidents[1].ResolvedAt)
}

func TestStatuspageCalendarHistoryParsesOpenIncident(t *testing.T) {
	payload := []byte(statuspageCalendarFixture(t, RadarSourceStatusClaude, "k8w3r06qmzrp",
		map[string]any{
			"code": "open-incident", "name": "Elevated errors for Claude", "impact": "minor",
			"timestamp": "Jul <var data-var='date'>20</var>, <var data-var='time'>14:41</var> UTC",
		},
	))

	incidents, _, _, err := decodeStatuspageCalendarHistory(
		payload, RadarSourceStatusClaude, ServiceKeyClaudeAPI, []string{"k8w3r06qmzrp"},
	)
	require.NoError(t, err)
	require.Len(t, incidents, 1)
	require.Equal(t, "investigating", incidents[0].Status)
	require.Equal(t, time.Date(2026, time.July, 20, 14, 41, 0, 0, time.UTC), incidents[0].CreatedAt)
	require.Nil(t, incidents[0].ResolvedAt)
}

func TestMergeStatuspageHistoryReconcilesOpenCalendarIncident(t *testing.T) {
	summaryPayload := []byte(strings.Replace(
		statuspageSummaryFixture(t, RadarSourceStatusClaude),
		"2026-07-10T10:30:00Z", "2026-07-20T15:00:00Z", 1,
	))
	incidentsPayload := []byte(`{
      "page":{"id":"page","name":"Status","url":"https://payload.example","updated_at":"2026-07-20T15:00:00Z"},
      "incidents":[
        {"id":"open-incident","name":"Elevated errors for Claude","status":"identified","impact":"minor","created_at":"2026-07-20T14:41:12.517Z","resolved_at":null,"components":[{"id":"k8w3r06qmzrp","name":"Claude API (api.anthropic.com)"}]}
      ]
    }`)
	historyPayload := []byte(statuspageCalendarFixture(t, RadarSourceStatusClaude, "k8w3r06qmzrp", map[string]any{
		"code": "open-incident", "name": "Elevated errors for Claude", "impact": "minor",
		"timestamp": "Jul <var data-var='date'>20</var>, <var data-var='time'>14:41</var> UTC",
	}))
	historyIncidents, coverageStart, coverageEnd, err := decodeStatuspageCalendarHistory(
		historyPayload, RadarSourceStatusClaude, ServiceKeyClaudeAPI, []string{"k8w3r06qmzrp"},
	)
	require.NoError(t, err)
	calendarPayload, err := json.Marshal(statuspageCalendarBundleWire{
		CoverageStart: coverageStart.Format(time.RFC3339Nano), CoverageEnd: coverageEnd.Format(time.RFC3339Nano),
		Incidents: encodeStatuspageIncidents(historyIncidents),
	})
	require.NoError(t, err)

	merged, err := mergeStatuspageHistoryPayloads(
		RadarSourceStatusClaude, summaryPayload, incidentsPayload, nil, nil, nil, calendarPayload,
	)
	require.NoError(t, err)
	summary, err := DecodeStatuspageSummary(merged)
	require.NoError(t, err)
	require.Len(t, summary.ComponentHistory, 1)
	require.Equal(t, "identified", summary.ComponentHistory[0].Status)
	require.Equal(t, time.Date(2026, time.July, 20, 14, 41, 12, 517000000, time.UTC), summary.ComponentHistory[0].CreatedAt)
	require.Nil(t, summary.ComponentHistory[0].ResolvedAt)

	cards, err := MapStatuspageServiceHealth(RadarSourceStatusClaude, summary)
	require.NoError(t, err)
	apiDay := historyDayByDate(t, cards[0].History30d, "2026-07-20")
	require.Equal(t, ServiceStatusDegradedPerformance, apiDay.Status)
	require.Len(t, apiDay.Incidents, 1)
	require.Equal(t, "identified", apiDay.Incidents[0].Status)
}

func TestDecodeStatuspageCalendarHistoryRejectsWrongOrMixedComponentFilter(t *testing.T) {
	for _, filter := range []string{`null`, `[]`, `["speech"]`, `["vwp8mgy34fck","speech"]`} {
		props := `{"months":[],"component_filter":` + filter + `,"start_time":"2026-05-01T00:00:00+08:00","end_time":"2026-07-31T23:59:59+08:00"}`
		payload := []byte(`<div data-react-class="HistoryIndex" data-react-props="` + html.EscapeString(props) + `"></div>`)
		_, _, _, err := decodeStatuspageCalendarHistory(
			payload, RadarSourceStatusMiniMaxChina, ServiceKeyMiniMax, []string{miniMaxChinaLLMComponentID},
		)
		require.Error(t, err, filter)
	}
}

func historyDayByDate(t *testing.T, days []ServiceHealthHistoryDayDTO, date string) ServiceHealthHistoryDayDTO {
	t.Helper()
	for _, day := range days {
		if day.Date == date {
			return day
		}
	}
	t.Fatalf("history day %s not found", date)
	return ServiceHealthHistoryDayDTO{}
}

func radarHistoryTime(value time.Time) *time.Time { return &value }

func TestStatuspageComponentNameVariantsAndLures(t *testing.T) {
	claudeAPINames := []string{"Claude API", " claude api ", "Claude API (api.anthropic.com)"}
	for _, name := range claudeAPINames {
		t.Run("Claude API "+name, func(t *testing.T) {
			cards := mapStatuspageFixture(t, RadarSourceStatusClaude, statuspageComponentsJSON(
				statuspageComponentJSON("component", name, "operational", "2026-07-10T10:00:00Z"),
			))
			require.Equal(t, ServiceStatusOperational, cards[0].Status)
		})
	}

	codexWebNames := []string{"Codex Web", "ChatGPT Codex", "Codex in ChatGPT Desktop"}
	for _, name := range codexWebNames {
		t.Run("Codex Web "+name, func(t *testing.T) {
			cards := mapStatuspageFixture(t, RadarSourceStatusOpenAI, statuspageComponentsJSON(
				statuspageComponentJSON("component", name, "operational", "2026-07-10T10:00:00Z"),
			))
			require.Equal(t, ServiceStatusOperational, cards[0].Status)
		})
	}

	apiNames := []string{
		"Codex API", "Responses", "Responses API", "Batch", "Audio", "Embeddings",
		"Moderations", "Files", "Fine-tuning", "Chat Completions", "Realtime API",
		"Compliance API", "Ads API",
	}
	for _, name := range apiNames {
		t.Run("OpenAI API "+name, func(t *testing.T) {
			cards := mapStatuspageFixture(t, RadarSourceStatusOpenAI, statuspageComponentsJSON(
				statuspageComponentJSON("component", name, "operational", "2026-07-10T10:00:00Z"),
			))
			require.Equal(t, ServiceStatusOperational, cards[1].Status)
		})
	}

	lures := []string{
		"Third-party Claude API", "Claude API Dashboard", "Claude API (third-party proxy)", "Claude Code API",
		"My Codex API proxy", "Codex Website Monitor", "Responses (unrelated)", "Compliance API (unrelated)",
		"Conversations", "Login", "GPTs",
	}
	for _, name := range lures {
		t.Run("lure "+name, func(t *testing.T) {
			source := RadarSourceStatusOpenAI
			if name == "Third-party Claude API" || name == "Claude API Dashboard" ||
				name == "Claude API (third-party proxy)" || name == "Claude Code API" {
				source = RadarSourceStatusClaude
			}
			cards := mapStatuspageFixture(t, source, statuspageComponentsJSON(
				statuspageComponentJSON("component", name, "major_outage", "2026-07-10T10:00:00Z"),
			))
			for _, card := range cards {
				require.Equal(t, ServiceStatusUnknown, card.Status)
			}
		})
	}
}

func TestMapStatuspageOpenAIAPICurrentComplianceAndAdsAliasesAggregateFailures(t *testing.T) {
	tests := []struct {
		name       string
		components string
		want       ServiceStatus
		indicator  StatusIndicator
	}{
		{
			name: "Compliance API alone",
			components: statuspageComponentsJSON(
				statuspageComponentJSON("compliance", " Compliance API ", "partial_outage", "2026-07-10T10:00:00Z"),
			),
			want:      ServiceStatusPartialOutage,
			indicator: StatusIndicatorMajor,
		},
		{
			name: "Ads API alone",
			components: statuspageComponentsJSON(
				statuspageComponentJSON("ads", "ads api", "major_outage", "2026-07-10T10:00:00Z"),
			),
			want:      ServiceStatusMajorOutage,
			indicator: StatusIndicatorCritical,
		},
		{
			name: "worst of both",
			components: statuspageComponentsJSON(
				statuspageComponentJSON("compliance", "Compliance API", "partial_outage", "2026-07-10T10:00:00Z"),
				statuspageComponentJSON("ads", "Ads API", "degraded_performance", "2026-07-10T10:01:00Z"),
			),
			want:      ServiceStatusPartialOutage,
			indicator: StatusIndicatorMajor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cards := mapStatuspageFixture(t, RadarSourceStatusOpenAI, tt.components)
			require.Equal(t, tt.want, cards[1].Status)
			require.Equal(t, tt.indicator, cards[1].StatusIndicator)
		})
	}
}

func TestMapStatuspageOpenAIImageGenerationLiveComponentAffectsAPIWorstStatus(t *testing.T) {
	summary, err := DecodeStatuspageSummary([]byte(`{
      "page":{"id":"openai","name":"OpenAI Status","url":"https://status.openai.com","updated_at":"2026-07-10T10:30:00Z"},
      "status":{"indicator":"critical","description":"Major System Outage"},
      "components":[
        {
          "id":"responses",
          "name":"Responses",
          "status":"operational",
          "created_at":"2025-01-01T00:00:00Z",
          "updated_at":"2026-07-10T10:00:00Z",
          "position":1,
          "description":null,
          "showcase":true,
          "group":false,
          "only_show_if_degraded":false
        },
        {
          "id":"image-generation",
          "name":"Image Generation",
          "status":"major_outage",
          "created_at":"2025-01-01T00:00:00Z",
          "updated_at":"2026-07-10T10:20:00Z",
          "position":2,
          "description":null,
          "showcase":true,
          "group":false,
          "only_show_if_degraded":false
        }
      ]
    }`))
	require.NoError(t, err)

	cards, err := MapStatuspageServiceHealth(RadarSourceStatusOpenAI, summary)

	require.NoError(t, err)
	require.Equal(t, ServiceStatusMajorOutage, cards[1].Status)
	require.Equal(t, StatusIndicatorCritical, cards[1].StatusIndicator)
	require.Equal(t, time.Date(2026, 7, 10, 10, 20, 0, 0, time.UTC), *cards[1].LastUpdatedAt)
}

func TestMapStatuspageUnknownStatusAndMissingComponentsAreUnknown(t *testing.T) {
	unknownCards := mapStatuspageFixture(t, RadarSourceStatusClaude, statuspageComponentsJSON(
		statuspageComponentJSON("api", "Claude API", "future_status", "2026-07-10T10:00:00Z"),
		statuspageComponentJSON("api-region", "Claude API (region)", "operational", "2026-07-10T10:01:00Z"),
	))
	require.Equal(t, ServiceStatusUnknown, unknownCards[0].Status)
	require.Equal(t, StatusIndicatorUnknown, unknownCards[0].StatusIndicator)
	require.Equal(t, ServiceStatusUnknown, unknownCards[1].Status)
	require.Nil(t, unknownCards[1].LastUpdatedAt)

	emptyCards := mapStatuspageFixture(t, RadarSourceStatusOpenAI, `[]`)
	require.Len(t, emptyCards, 2)
	for _, card := range emptyCards {
		require.Equal(t, ServiceStatusUnknown, card.Status)
		require.Equal(t, StatusIndicatorUnknown, card.StatusIndicator)
		require.Nil(t, card.LastUpdatedAt)
		require.Nil(t, card.Uptime90d)
		require.Nil(t, card.LastIncident)
	}
}

func TestMergeStatuspageServiceHealthAlwaysReturnsFourCanonicalSafeCards(t *testing.T) {
	uptime := 99.9
	cards := MergeStatuspageServiceHealth([]ServiceHealthDTO{{
		ServiceKey:      ServiceKeyClaudeAPI,
		Name:            "payload-controlled name",
		Status:          ServiceStatusOperational,
		StatusIndicator: StatusIndicatorCritical,
		Uptime90d:       &uptime,
		SourceURL:       "https://payload.example",
	}})

	require.Len(t, cards, 4)
	require.Equal(t, []ServiceKey{
		ServiceKeyClaudeAPI,
		ServiceKeyClaudeCode,
		ServiceKeyCodexWeb,
		ServiceKeyOpenAIAPI,
	}, []ServiceKey{cards[0].ServiceKey, cards[1].ServiceKey, cards[2].ServiceKey, cards[3].ServiceKey})
	require.Equal(t, "Claude API", cards[0].Name)
	require.Equal(t, ServiceStatusOperational, cards[0].Status)
	require.Equal(t, StatusIndicatorNone, cards[0].StatusIndicator)
	require.Nil(t, cards[0].Uptime90d)
	require.Equal(t, claudeStatuspagePublicURL, cards[0].SourceURL)
	for _, missing := range cards[1:] {
		require.Equal(t, ServiceStatusUnknown, missing.Status)
		require.Equal(t, StatusIndicatorUnknown, missing.StatusIndicator)
		require.Nil(t, missing.LastUpdatedAt)
	}
	for _, card := range cards {
		require.Equal(t, serviceHealthHistoryDays, card.HistoryDays)
		require.Equal(t, serviceHealthUptimeWindowDays, card.UptimeWindowDays)
		require.Equal(t, serviceHealthRecentIncidentDays, card.RecentIncidentDays)
		require.Equal(t, serviceHealthIncidentPreviewLimit, card.IncidentPreviewLimit)
	}
}

func TestMapStatuspageOfficialPlatformComponentsAndMergeMiniMaxRegions(t *testing.T) {
	tests := []struct {
		source    RadarSourceKey
		component string
		wantKey   ServiceKey
	}{
		{source: RadarSourceStatusWindsurf, component: "Cascade", wantKey: ServiceKeyWindsurf},
		{source: RadarSourceStatusKimi, component: "Open API", wantKey: ServiceKeyKimi},
		{source: RadarSourceStatusMiniMaxGlobal, component: "Large Language Models (LLM)", wantKey: ServiceKeyMiniMax},
		{source: RadarSourceStatusMiniMaxChina, component: "大语言模型LLM", wantKey: ServiceKeyMiniMax},
	}
	groups := make([][]ServiceHealthDTO, 0, len(tests))
	for index, test := range tests {
		status := "operational"
		if test.source == RadarSourceStatusMiniMaxChina {
			status = "partial_outage"
		}
		cards := mapStatuspageFixture(t, test.source, statuspageComponentsJSON(
			statuspageComponentJSON("component-"+strconv.Itoa(index), test.component, status, "2026-07-10T10:00:00Z"),
		))
		require.Len(t, cards, 1)
		require.Equal(t, test.wantKey, cards[0].ServiceKey)
		groups = append(groups, cards)
	}

	merged := MergeStatuspageServiceHealth(groups...)
	require.Len(t, merged, 7)
	require.Equal(t, []ServiceKey{ServiceKeyWindsurf, ServiceKeyKimi, ServiceKeyMiniMax}, []ServiceKey{
		merged[4].ServiceKey, merged[5].ServiceKey, merged[6].ServiceKey,
	})
	require.Equal(t, ServiceStatusPartialOutage, merged[6].Status)
}

func TestMapStatuspageOpenAIAPIAggregatesWorstKnownStatus(t *testing.T) {
	components := statuspageComponentsJSON(
		statuspageComponentJSON("responses", "Responses", "operational", "2026-07-10T10:00:00Z"),
		statuspageComponentJSON("batch", "Batch", "under_maintenance", "2026-07-10T10:01:00Z"),
		statuspageComponentJSON("audio", "Audio", "degraded_performance", "2026-07-10T10:02:00Z"),
		statuspageComponentJSON("files", "Files", "partial_outage", "2026-07-10T10:03:00Z"),
		statuspageComponentJSON("moderations", "Moderations", "major_outage", "2026-07-10T10:04:00Z"),
	)

	cards := mapStatuspageFixture(t, RadarSourceStatusOpenAI, components)

	require.Equal(t, ServiceStatusMajorOutage, cards[1].Status)
	require.Equal(t, StatusIndicatorCritical, cards[1].StatusIndicator)
	require.Equal(t, time.Date(2026, 7, 10, 10, 4, 0, 0, time.UTC), *cards[1].LastUpdatedAt)
}

func TestMapStatuspageIncidentDoesNotAttributeUnscopedPageIncidentToEveryService(t *testing.T) {
	summary, err := DecodeStatuspageSummary([]byte(`{
      "page":{"id":"claude","name":"Claude Status","url":"https://payload.example","updated_at":"2026-07-10T10:30:00Z"},
      "status":{"indicator":"minor","description":"Incident"},
      "radar_history_coverage_start":"2026-06-11T00:00:00Z",
      "components":[
        {"id":"api","name":"Claude API","status":"degraded_performance","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:00:00Z"},
        {"id":"code","name":"Claude Code","status":"operational","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:00:00Z"}
      ],
      "incidents":[
        {"id":"linked","name":"Linked API incident","status":"resolved","impact":"minor","created_at":"2026-07-09T10:00:00Z","resolved_at":"2026-07-09T11:00:00Z","components":[{"id":"api","name":"Claude API"}]},
        {"id":"unrelated","name":"Unrelated newest incident","status":"resolved","impact":"critical","created_at":"2026-07-10T12:00:00Z","components":[{"id":"other","name":"Other"}]},
        {"id":"conflicting-id","name":"Conflicting ID incident","status":"resolved","impact":"critical","created_at":"2026-07-10T11:30:00Z","components":[{"id":"other","name":"Claude API"}]},
        {"id":"page-level","name":"Source-wide page incident","status":"resolved","impact":"major","created_at":"2026-07-10T11:00:00Z","resolved_at":"2026-07-10T11:15:00Z","components":[]}
      ]
    }`))
	require.NoError(t, err)

	cards, err := MapStatuspageServiceHealth(RadarSourceStatusClaude, summary)

	require.NoError(t, err)
	require.NotNil(t, cards[0].LastIncident)
	require.Equal(t, "Linked API incident", cards[0].LastIncident.Name)
	require.Nil(t, cards[1].LastIncident)
	require.NotContains(t, cards[0].LastIncident.Name, "Unrelated")
	require.NotNil(t, cards[0].LastIncident.ResolvedAt)
	require.Equal(t, ServiceStatusUnknown, historyDayByDate(t, cards[0].History30d, "2026-07-10").Status)
	require.Equal(t, ServiceStatusUnknown, historyDayByDate(t, cards[1].History30d, "2026-07-10").Status)

	linked := &summary.Incidents[0]
	require.NotNil(t, linked.ResolvedAt)
	require.NotSame(t, linked.ResolvedAt, cards[0].LastIncident.ResolvedAt)
	originalName := linked.Name
	originalResolvedAt := *linked.ResolvedAt
	cards[0].LastIncident.Name = "mutated output"
	*cards[0].LastIncident.ResolvedAt = cards[0].LastIncident.ResolvedAt.Add(time.Hour)
	require.Equal(t, originalName, linked.Name)
	require.Equal(t, originalResolvedAt, *linked.ResolvedAt)
}

func TestDecodeStatuspageStrictJSONArraysAndTimes(t *testing.T) {
	accepted := []struct {
		name      string
		incidents string
	}{
		{name: "missing incidents", incidents: ""},
		{name: "null incidents", incidents: `,"incidents":null`},
		{name: "empty incidents", incidents: `,"incidents":[]`},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{
              "page":{"id":"page","name":"Status","url":"https://payload.example","updated_at":"2026-07-10T10:30:00.123456789+08:00"},
              "status":{"indicator":"none","description":"OK"},
              "components":[]` + tt.incidents + `
            }`
			summary, err := DecodeStatuspageSummary([]byte(payload))
			require.NoError(t, err)
			require.NotNil(t, summary.Components)
			require.NotNil(t, summary.Incidents)
			require.Equal(t, time.Date(2026, 7, 10, 2, 30, 0, 123456789, time.UTC), summary.Page.UpdatedAt)
		})
	}

	rejected := []struct {
		name    string
		payload string
	}{
		{name: "malformed", payload: `{"page":`},
		{name: "trailing JSON", payload: validStatuspagePayload + ` {}`},
		{name: "missing page", payload: `{"status":{"indicator":"none","description":"OK"},"components":[]}`},
		{name: "null page", payload: `{"page":null,"status":{"indicator":"none","description":"OK"},"components":[]}`},
		{name: "missing status", payload: `{"page":{"id":"p","name":"P","url":"https://payload.example","updated_at":"2026-07-10T10:00:00Z"},"components":[]}`},
		{name: "bad page time", payload: `{"page":{"id":"p","name":"P","url":"https://payload.example","updated_at":"today"},"status":{"indicator":"none","description":"OK"},"components":[]}`},
		{name: "zero page time", payload: `{"page":{"id":"p","name":"P","url":"https://payload.example","updated_at":"0001-01-01T00:00:00Z"},"status":{"indicator":"none","description":"OK"},"components":[]}`},
		{name: "missing components", payload: `{"page":{"id":"p","name":"P","url":"https://payload.example","updated_at":"2026-07-10T10:00:00Z"},"status":{"indicator":"none","description":"OK"}}`},
		{name: "null components", payload: `{"page":{"id":"p","name":"P","url":"https://payload.example","updated_at":"2026-07-10T10:00:00Z"},"status":{"indicator":"none","description":"OK"},"components":null}`},
		{name: "duplicate normalized component ID", payload: statuspagePayloadWithComponents(`[
          {"id":" DUPLICATE ","name":"Claude API","status":"operational","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:00:00Z"},
          {"id":"duplicate","name":"Claude Code","status":"operational","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-07-10T10:00:00Z"}
        ]`)},
		{name: "bad component created time", payload: statuspagePayloadWithComponents(`[ {"id":"c","name":"Claude API","status":"operational","created_at":"bad","updated_at":"2026-07-10T10:00:00Z"} ]`)},
		{name: "zero component created time", payload: statuspagePayloadWithComponents(`[ {"id":"c","name":"Claude API","status":"operational","created_at":"0001-01-01T00:00:00Z","updated_at":"2026-07-10T10:00:00Z"} ]`)},
		{name: "bad component updated time", payload: statuspagePayloadWithComponents(`[ {"id":"c","name":"Claude API","status":"operational","created_at":"2026-01-01T00:00:00Z","updated_at":"bad"} ]`)},
		{name: "zero component updated time", payload: statuspagePayloadWithComponents(`[ {"id":"c","name":"Claude API","status":"operational","created_at":"2026-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z"} ]`)},
		{name: "bad incident created time", payload: statuspagePayloadWithComponents(`[]`)[:len(statuspagePayloadWithComponents(`[]`))-1] + `,"incidents":[{"id":"i","name":"Incident","status":"resolved","impact":"minor","created_at":"bad"}]}`},
		{name: "zero incident created time", payload: statuspagePayloadWithComponents(`[]`)[:len(statuspagePayloadWithComponents(`[]`))-1] + `,"incidents":[{"id":"i","name":"Incident","status":"resolved","impact":"minor","created_at":"0001-01-01T00:00:00Z"}]}`},
		{name: "bad incident resolved time", payload: statuspagePayloadWithComponents(`[]`)[:len(statuspagePayloadWithComponents(`[]`))-1] + `,"incidents":[{"id":"i","name":"Incident","status":"resolved","impact":"minor","created_at":"2026-07-10T10:00:00Z","resolved_at":"bad"}]}`},
		{name: "zero incident resolved time", payload: statuspagePayloadWithComponents(`[]`)[:len(statuspagePayloadWithComponents(`[]`))-1] + `,"incidents":[{"id":"i","name":"Incident","status":"resolved","impact":"minor","created_at":"2026-07-10T10:00:00Z","resolved_at":"0001-01-01T00:00:00Z"}]}`},
		{name: "incident resolved before creation without backdated timeline", payload: statuspagePayloadWithComponents(`[]`)[:len(statuspagePayloadWithComponents(`[]`))-1] + `,"incidents":[{"id":"i","name":"Incident","status":"resolved","impact":"minor","created_at":"2026-07-10T10:00:00Z","resolved_at":"2026-07-10T09:00:00Z"}]}`},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			summary, err := DecodeStatuspageSummary([]byte(tt.payload))
			require.Error(t, err)
			require.Zero(t, summary)
			require.NotContains(t, err.Error(), tt.payload)
		})
	}
}

func TestDecodeStatuspageIncidentUsesBackdatedPublicTimeline(t *testing.T) {
	payload := []byte(`{
      "page":{"id":"openai","name":"OpenAI","url":"https://status.openai.com/","updated_at":"2026-07-27T20:52:18Z"},
      "incidents":[{
        "id":"incident","name":"Backdated incident","status":"resolved","impact":"minor",
        "created_at":"2026-07-27T18:06:39Z","resolved_at":"2026-07-27T17:30:00Z",
        "incident_updates":[
          {"status":"resolved","display_at":"2026-07-27T17:30:00Z"},
          {"status":"investigating","display_at":"2026-07-27T15:30:00Z"}
        ]
      }]
    }`)

	_, incidents, err := decodeStatuspageIncidents(payload)

	require.NoError(t, err)
	require.Len(t, incidents, 1)
	require.Equal(t, time.Date(2026, time.July, 27, 15, 30, 0, 0, time.UTC), incidents[0].CreatedAt)
	require.Equal(t, time.Date(2026, time.July, 27, 17, 30, 0, 0, time.UTC), *incidents[0].ResolvedAt)
}

func TestStatuspageConstructorsRejectUnsupportedSourceAndInvalidConfig(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP client must not be called by constructor")
		return nil, nil
	})

	fetcher, err := NewStatuspageFetcher(validRadarFetcherTestConfig(), RadarSourceAA, client)
	require.Error(t, err)
	require.Nil(t, fetcher)
	var configErr *RadarFetcherConfigError
	require.True(t, errors.As(err, &configErr))
	require.Equal(t, "statuspage_source", configErr.Field)

	cfg := validRadarFetcherTestConfig()
	cfg.Radar.StatuspageIntervalMinutes = 0
	fetcher, err = NewStatuspageFetcher(cfg, RadarSourceStatusClaude, client)
	require.Error(t, err)
	require.Nil(t, fetcher)
	require.True(t, errors.As(err, &configErr))
}

func requireServiceHealthCard(
	t *testing.T,
	card ServiceHealthDTO,
	name string,
	status ServiceStatus,
	indicator StatusIndicator,
	sourceURL string,
) {
	t.Helper()
	require.Equal(t, name, card.Name)
	require.Equal(t, status, card.Status)
	require.Equal(t, indicator, card.StatusIndicator)
	require.Equal(t, sourceURL, card.SourceURL)
	require.Nil(t, card.Uptime90d)
	require.False(t, card.Stale)
}

func mapStatuspageFixture(t *testing.T, source RadarSourceKey, components string) []ServiceHealthDTO {
	t.Helper()
	summary, err := DecodeStatuspageSummary([]byte(statuspagePayloadWithComponents(components)))
	require.NoError(t, err)
	cards, err := MapStatuspageServiceHealth(source, summary)
	require.NoError(t, err)
	return cards
}

func statuspagePayloadWithComponents(components string) string {
	return `{"page":{"id":"page","name":"Status","url":"https://payload.example","updated_at":"2026-07-10T10:30:00Z"},"status":{"indicator":"none","description":"OK"},"components":` + components + `}`
}

func statuspageComponentsJSON(components ...string) string {
	result := "["
	for index, component := range components {
		if index > 0 {
			result += ","
		}
		result += component
	}
	return result + "]"
}

func statuspageComponentJSON(id, name, status, updatedAt string) string {
	return `{"id":"` + id + `","name":"` + name + `","status":"` + status + `","created_at":"2026-01-01T00:00:00Z","updated_at":"` + updatedAt + `"}`
}
