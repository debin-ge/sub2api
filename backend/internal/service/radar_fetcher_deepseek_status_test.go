package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecodeDeepSeekStatusPageMapsOfficialAPIComponentAndActiveIncident(t *testing.T) {
	payload := deepSeekStatusHTML(t, `{
      "initialData":{
        "page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[
          {"component_id":"api","name":"API 服务 (API Service)","available_since_seconds":1706745600},
          {"component_id":"chat","name":"网页对话服务 (Web Chat Service)","available_since_seconds":1706745600}
        ]},
        "active_changes":[{
          "change_id":6653444512287,"title":"DeepSeek API 性能下降","status":"identified","start_at_seconds":1782982008,
          "affected_components":[{"component_id":"api","name":"API 服务 (API Service)","status":"degraded"}]
        }]
      },
      "initialDataUpdatedAt":1784165568367
    }`)

	canonical, err := decodeDeepSeekStatusPage(payload)
	require.NoError(t, err)
	summary, err := DecodeStatuspageSummary(canonical)
	require.NoError(t, err)
	require.Equal(t, "DeepSeek Status", summary.Page.Name)
	require.Len(t, summary.Components, 2)
	require.Equal(t, "degraded_performance", summary.Components[0].Status)

	cards, err := MapStatuspageServiceHealth(RadarSourceStatusDeepSeek, summary)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, ServiceKeyDeepSeek, cards[0].ServiceKey)
	require.Equal(t, ServiceStatusDegradedPerformance, cards[0].Status)
	require.NotNil(t, cards[0].LastIncident)
	require.Equal(t, "DeepSeek API 性能下降", cards[0].LastIncident.Name)
}

func TestDecodeDeepSeekStatusPageMapsOfficialThirtyDayHistory(t *testing.T) {
	var current any
	require.NoError(t, json.Unmarshal([]byte(`{
      "initialData":{"page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[
        {"component_id":"api","name":"API 服务 (API Service)","available_since_seconds":1706745600},
        {"component_id":"chat","name":"网页对话服务 (Web Chat Service)","available_since_seconds":1706745600}
      ]},"active_changes":[]},"initialDataUpdatedAt":1784165568367
    }`), &current))
	start := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	history := map[string]any{"initialData": map[string]any{
		"component_impacts": []map[string]any{{
			"component_id": "api", "change_id": int64(99),
			"start_at_seconds": start.Unix(), "end_at_seconds": end.Unix(), "status": "full_outage",
		}},
		"linked_changes": []map[string]any{{"id": int64(99), "type": "incident", "title": "DeepSeek API unavailable"}},
	}}
	payload := deepSeekStatusHTMLNodes(t, current, history)

	summary, err := DecodeDeepSeekStatusPage(payload)
	require.NoError(t, err)
	require.NotNil(t, summary.HistoryCoverageStart)
	cards, err := MapStatuspageServiceHealth(RadarSourceStatusDeepSeek, summary)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Len(t, cards[0].History30d, 30)
	var incidentDay ServiceHealthHistoryDayDTO
	for _, day := range cards[0].History30d {
		if day.Date == "2026-07-12" {
			incidentDay = day
		}
	}
	require.Equal(t, ServiceStatusMajorOutage, incidentDay.Status)
	require.Len(t, incidentDay.Incidents, 1)
	require.Equal(t, "DeepSeek API unavailable", incidentDay.Incidents[0].Name)
}

func TestDecodeDeepSeekStatusPageMapsSplitV4APIComponentsAsOperational(t *testing.T) {
	var current any
	require.NoError(t, json.Unmarshal([]byte(`{
      "initialData":{"page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[
        {"component_id":"v4-pro-api","name":"DeepSeek V4 Pro API服务","available_since_seconds":1706745600},
        {"component_id":"v4-flash-api","name":"DeepSeek V4 Flash API服务","available_since_seconds":1706745600},
        {"component_id":"chat","name":"网页对话服务 (Web Chat Service)","available_since_seconds":1706745600}
      ]},"active_changes":[]},"initialDataUpdatedAt":1784165568367
    }`), &current))
	history := map[string]any{"initialData": map[string]any{
		"component_impacts": []any{},
		"linked_changes":    []any{},
	}}

	summary, err := DecodeDeepSeekStatusPage(deepSeekStatusHTMLNodes(t, current, history))
	require.NoError(t, err)
	cards, err := MapStatuspageServiceHealth(RadarSourceStatusDeepSeek, summary)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, ServiceStatusOperational, cards[0].Status)
	require.Equal(t, StatusIndicatorNone, cards[0].StatusIndicator)
	require.NotNil(t, cards[0].LastUpdatedAt)
	require.Nil(t, cards[0].LastIncident)
	require.Len(t, cards[0].History30d, serviceHealthHistoryDays)
	for _, day := range cards[0].History30d {
		require.Equal(t, ServiceStatusOperational, day.Status)
		require.Empty(t, day.Incidents)
	}
}

func TestDecodeDeepSeekStatusPageRejectsUnmappedHistoricalComponent(t *testing.T) {
	var current any
	require.NoError(t, json.Unmarshal([]byte(`{
      "initialData":{"page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[
        {"component_id":"api","name":"API 服务 (API Service)","available_since_seconds":1706745600}
      ]},"active_changes":[]},"initialDataUpdatedAt":1784165568367
    }`), &current))
	history := map[string]any{"initialData": map[string]any{
		"component_impacts": []map[string]any{{
			"component_id": "removed-api", "change_id": int64(99),
			"start_at_seconds": int64(1783843200), "end_at_seconds": int64(1783846800), "status": "full_outage",
		}},
		"linked_changes": []map[string]any{{"id": int64(99), "type": "incident", "title": "Historical outage"}},
	}}

	_, err := DecodeDeepSeekStatusPage(deepSeekStatusHTMLNodes(t, current, history))
	require.Error(t, err)
}

func TestDecodeDeepSeekStatusPageFindsPropsAfterOtherFlightRecords(t *testing.T) {
	props := `{
      "initialData":{"page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[
        {"component_id":"api","name":"API 服务 (API Service)","available_since_seconds":1706745600}
      ]},"active_changes":[]},"initialDataUpdatedAt":1784165568367
    }`
	var node any
	require.NoError(t, json.Unmarshal([]byte(props), &node))
	relevant, err := json.Marshal([]any{"$", "$component", nil, node})
	require.NoError(t, err)
	frame, err := json.Marshal([]any{1, "1:I[123,[],\"default\"]\n21:" + string(relevant) + "\n22:T20,ignored text record"})
	require.NoError(t, err)
	payload := []byte("<!doctype html><html><body><script>self.__next_f.push(" + string(frame) + ")</script></body></html>")

	summary, err := DecodeDeepSeekStatusPage(payload)
	require.NoError(t, err)
	require.Len(t, summary.Components, 1)
	require.Equal(t, "API 服务 (API Service)", summary.Components[0].Name)
}

func TestDecodeDeepSeekStatusPageRejectsUntrustedIdentityAndUnknownState(t *testing.T) {
	tests := []string{
		`{"initialData":{"page":{"page_id":1,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[{"component_id":"api","name":"API Service","available_since_seconds":1}]},"active_changes":[]},"initialDataUpdatedAt":1784165568367}`,
		`{"initialData":{"page":{"page_id":1,"name":"DeepSeek","custom_domain":"attacker.example","components":[{"component_id":"api","name":"API Service","available_since_seconds":1}]},"active_changes":[]},"initialDataUpdatedAt":1784165568367}`,
		`{"initialData":{"page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[{"component_id":"api","name":"API Service","available_since_seconds":1}]},"active_changes":[{"change_id":2,"title":"bad","status":"open","start_at_seconds":2,"affected_components":[{"component_id":"api","name":"API Service","status":"invented"}]}]},"initialDataUpdatedAt":1784165568367}`,
	}
	for _, input := range tests {
		_, err := decodeDeepSeekStatusPage(deepSeekStatusHTML(t, input))
		require.Error(t, err)
	}
}

func TestDecodeDeepSeekStatusPageRejectsOversizedOrDuplicateState(t *testing.T) {
	components := make([]map[string]any, deepSeekMaxComponents+1)
	for i := range components {
		components[i] = map[string]any{
			"component_id":            "component-" + string(rune(i+1)),
			"name":                    "Component",
			"available_since_seconds": 1,
		}
	}
	activeChanges := make([]map[string]any, deepSeekMaxActiveChanges+1)
	for i := range activeChanges {
		activeChanges[i] = map[string]any{
			"change_id": int64(i + 1), "title": "Change", "status": "open", "start_at_seconds": 1,
			"affected_components": []any{},
		}
	}
	affectedComponents := make([]map[string]any, deepSeekMaxAffectedComponents+1)
	for i := range affectedComponents {
		affectedComponents[i] = map[string]any{
			"component_id": "api", "name": "API Service", "status": "degraded",
		}
	}
	basePage := map[string]any{
		"page_id": deepSeekStatusPageID, "name": "DeepSeek", "custom_domain": "status.deepseek.com",
		"components": []map[string]any{{"component_id": "api", "name": "API Service", "available_since_seconds": 1}},
	}
	tests := []map[string]any{
		{
			"page": map[string]any{
				"page_id": deepSeekStatusPageID, "name": "DeepSeek", "custom_domain": "status.deepseek.com",
				"components": components,
			},
			"active_changes": []any{},
		},
		{"page": basePage, "active_changes": activeChanges},
		{
			"page": basePage,
			"active_changes": []map[string]any{{
				"change_id": 1, "title": "oversized", "status": "open", "start_at_seconds": 1,
				"affected_components": affectedComponents,
			}},
		},
		{
			"page": basePage,
			"active_changes": []map[string]any{
				{"change_id": 2, "title": "first", "status": "open", "start_at_seconds": 2, "affected_components": []any{}},
				{"change_id": 2, "title": "duplicate", "status": "open", "start_at_seconds": 3, "affected_components": []any{}},
			},
		},
	}
	for _, initialData := range tests {
		input, err := json.Marshal(map[string]any{
			"initialData":          initialData,
			"initialDataUpdatedAt": int64(1784165568367),
		})
		require.NoError(t, err)
		_, err = decodeDeepSeekStatusPage(deepSeekStatusHTML(t, string(input)))
		require.Error(t, err)
	}
}

func TestDeepSeekStatusFetcherUsesFixedProviderEndpointAndPublishesCanonicalJSON(t *testing.T) {
	payload := deepSeekStatusHTML(t, `{
      "initialData":{"page":{"page_id":6410630422455,"name":"DeepSeek","custom_domain":"status.deepseek.com","components":[
        {"component_id":"api","name":"API 服务 (API Service)","available_since_seconds":1706745600}
      ]},"active_changes":[]},"initialDataUpdatedAt":1784165568367
    }`)
	var captured *http.Request
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		return &http.Response{StatusCode: http.StatusOK, Body: newRadarTrackingBody(string(payload))}, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.StatuspageIntervalMinutes = 37

	fetcher, err := NewDeepSeekStatusFetcher(cfg, client)
	require.NoError(t, err)
	require.Equal(t, 37*time.Minute, fetcher.Interval())
	canonical, meta, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	require.Equal(t, RadarSourceStatusDeepSeek, fetcher.Source())
	require.Equal(t, "statuspage.flashcat.cloud", captured.URL.Host)
	require.Equal(t, "/deepseek", captured.URL.Path)
	require.Equal(t, "text/html", captured.Header.Get("Accept"))
	require.Nil(t, meta.Error)
	_, err = DecodeStatuspageSummary(canonical)
	require.NoError(t, err)
}

func deepSeekStatusHTML(t *testing.T, props string) []byte {
	t.Helper()
	var node any
	require.NoError(t, json.Unmarshal([]byte(props), &node))
	return deepSeekStatusHTMLNodes(t, node)
}

func deepSeekStatusHTMLNodes(t *testing.T, nodes ...any) []byte {
	t.Helper()
	records := make([]byte, 0)
	for index, node := range nodes {
		segmentBytes, err := json.Marshal([]any{"$", "$component", nil, node})
		require.NoError(t, err)
		if index > 0 {
			records = append(records, '\n')
		}
		records = append(records, strconv.Itoa(21+index)...)
		records = append(records, ':')
		records = append(records, segmentBytes...)
	}
	frame, err := json.Marshal([]any{1, string(records)})
	require.NoError(t, err)
	return []byte("<!doctype html><html><body><script>self.__next_f.push(" + string(frame) + ")</script></body></html>")
}
