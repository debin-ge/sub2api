package service

import (
	"context"
	"errors"
	"net/http"
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

func TestStatuspageFetchersUseFixedHTTPSEndpointsAndConfiguredInterval(t *testing.T) {
	tests := []struct {
		name     string
		source   RadarSourceKey
		wantHost string
	}{
		{name: "Claude", source: RadarSourceStatusClaude, wantHost: "status.claude.com"},
		{name: "OpenAI", source: RadarSourceStatusOpenAI, wantHost: "status.openai.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured *http.Request
			body := newRadarTrackingBody(validStatuspagePayload)
			client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
				captured = req.Clone(req.Context())
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			})
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.StatuspageIntervalMinutes = 47

			fetcher, err := NewStatuspageFetcher(cfg, tt.source, client)
			require.NoError(t, err)
			require.Equal(t, tt.source, fetcher.Source())
			require.Equal(t, 47*time.Minute, fetcher.Interval())

			payload, meta, err := fetcher.Fetch(context.Background())

			require.NoError(t, err)
			require.JSONEq(t, validStatuspagePayload, string(payload))
			require.True(t, body.isClosed())
			require.NotNil(t, captured)
			require.Equal(t, http.MethodGet, captured.Method)
			require.Equal(t, "https", captured.URL.Scheme)
			require.Equal(t, tt.wantHost, captured.URL.Host)
			require.Equal(t, "/api/v2/summary.json", captured.URL.EscapedPath())
			require.Empty(t, captured.URL.RawQuery)
			require.Empty(t, captured.Header.Get("x-api-key"))
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
	attempts := 0
	body := newRadarTrackingBody(payloadJSON)
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	fetcher, err := NewStatuspageFetcher(validRadarFetcherTestConfig(), RadarSourceStatusClaude, client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Equal(t, 1, attempts)
	require.True(t, body.isClosed())
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

func TestMapStatuspageIncidentUsesRealPageLevelShapeAndKeepsPointersIsolated(t *testing.T) {
	summary, err := DecodeStatuspageSummary([]byte(`{
      "page":{"id":"claude","name":"Claude Status","url":"https://payload.example","updated_at":"2026-07-10T10:30:00Z"},
      "status":{"indicator":"minor","description":"Incident"},
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
	require.Equal(t, "Source-wide page incident", cards[0].LastIncident.Name, "latest safely applicable incident should win")
	require.NotNil(t, cards[1].LastIncident)
	require.Equal(t, "Source-wide page incident", cards[1].LastIncident.Name)
	require.NotContains(t, cards[0].LastIncident.Name, "Unrelated")
	require.NotSame(t, cards[0].LastIncident, cards[1].LastIncident)
	require.NotNil(t, cards[0].LastIncident.ResolvedAt)
	require.NotNil(t, cards[1].LastIncident.ResolvedAt)
	require.NotSame(t, cards[0].LastIncident.ResolvedAt, cards[1].LastIncident.ResolvedAt)

	pageLevel := &summary.Incidents[len(summary.Incidents)-1]
	require.NotNil(t, pageLevel.ResolvedAt)
	require.NotSame(t, pageLevel.ResolvedAt, cards[0].LastIncident.ResolvedAt)
	originalName := pageLevel.Name
	originalResolvedAt := *pageLevel.ResolvedAt
	secondCardResolvedAt := *cards[1].LastIncident.ResolvedAt
	cards[0].LastIncident.Name = "mutated output"
	*cards[0].LastIncident.ResolvedAt = cards[0].LastIncident.ResolvedAt.Add(time.Hour)
	require.Equal(t, originalName, pageLevel.Name)
	require.Equal(t, originalResolvedAt, *pageLevel.ResolvedAt)
	require.Equal(t, "Source-wide page incident", cards[1].LastIncident.Name)
	require.Equal(t, secondCardResolvedAt, *cards[1].LastIncident.ResolvedAt)
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
