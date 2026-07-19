package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/net/html"
)

const (
	deepSeekStatusDataURL               = "https://statuspage.flashcat.cloud/deepseek"
	deepSeekStatusPageID          int64 = 6410630422455
	deepSeekMaxComponents               = 128
	deepSeekMaxActiveChanges            = 256
	deepSeekMaxAffectedComponents       = 128
)

var errInvalidDeepSeekStatusResponse = errors.New("invalid DeepSeek status response")

type deepSeekStatusFetcher struct {
	inner RadarFetcher
}

type deepSeekFlightProps struct {
	InitialData struct {
		Page struct {
			PageID       int64  `json:"page_id"`
			Name         string `json:"name"`
			CustomDomain string `json:"custom_domain"`
			Components   []struct {
				ComponentID           string `json:"component_id"`
				Name                  string `json:"name"`
				AvailableSinceSeconds int64  `json:"available_since_seconds"`
			} `json:"components"`
		} `json:"page"`
		ActiveChanges []struct {
			ChangeID           int64  `json:"change_id"`
			Title              string `json:"title"`
			Status             string `json:"status"`
			StartAtSeconds     int64  `json:"start_at_seconds"`
			CloseAtSeconds     int64  `json:"close_at_seconds"`
			AffectedComponents []struct {
				ComponentID string `json:"component_id"`
				Name        string `json:"name"`
				Status      string `json:"status"`
			} `json:"affected_components"`
		} `json:"active_changes"`
	} `json:"initialData"`
	InitialDataUpdatedAt int64 `json:"initialDataUpdatedAt"`
}

type deepSeekHistoryProps struct {
	InitialData struct {
		ComponentImpacts []struct {
			ComponentID    string `json:"component_id"`
			ChangeID       int64  `json:"change_id"`
			StartAtSeconds int64  `json:"start_at_seconds"`
			EndAtSeconds   int64  `json:"end_at_seconds"`
			Status         string `json:"status"`
		} `json:"component_impacts"`
		LinkedChanges []struct {
			ID    int64  `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"linked_changes"`
	} `json:"initialData"`
}

// NewDeepSeekStatusFetcher reads the official DeepSeek FlashDuty status page
// through its canonical provider host and converts the server-rendered state
// into the same validated Statuspage schema used by the other health sources.
func NewDeepSeekStatusFetcher(cfg *config.Config, client RadarHTTPDoer) (RadarFetcher, error) {
	if cfg == nil {
		return nil, &RadarFetcherConfigError{Field: "config"}
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, &RadarFetcherConfigError{Field: "radar"}
	}
	if isNilRadarHTTPDoer(client) {
		return nil, &RadarFetcherConfigError{Field: "http_client"}
	}
	inner, err := newRadarHTTPFetcher(radarHTTPFetcherOptions{
		source:           RadarSourceStatusDeepSeek,
		interval:         time.Duration(cfg.Radar.StatuspageIntervalMinutes) * time.Minute,
		client:           client,
		endpoint:         deepSeekStatusDataURL,
		headers:          http.Header{"Accept": []string{"text/html"}},
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		validate: func(payload []byte) error {
			_, err := decodeDeepSeekStatusPage(payload)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	return &deepSeekStatusFetcher{inner: inner}, nil
}

func (f *deepSeekStatusFetcher) Source() RadarSourceKey { return RadarSourceStatusDeepSeek }

func (f *deepSeekStatusFetcher) Interval() time.Duration { return f.inner.Interval() }

func (f *deepSeekStatusFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	payload, meta, err := f.inner.Fetch(ctx)
	if err != nil {
		return payload, meta, err
	}
	canonical, err := decodeDeepSeekStatusPage(payload)
	if err != nil {
		return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
	}
	return canonical, meta, nil
}

func decodeDeepSeekStatusPage(payload []byte) ([]byte, error) {
	props, err := extractDeepSeekFlightProps(payload)
	if err != nil {
		return nil, errInvalidDeepSeekStatusResponse
	}
	if props.InitialData.Page.PageID != deepSeekStatusPageID || strings.TrimSpace(props.InitialData.Page.Name) != "DeepSeek" ||
		strings.TrimSpace(props.InitialData.Page.CustomDomain) != "status.deepseek.com" ||
		props.InitialDataUpdatedAt <= 0 || len(props.InitialData.Page.Components) == 0 ||
		len(props.InitialData.Page.Components) > deepSeekMaxComponents ||
		len(props.InitialData.ActiveChanges) > deepSeekMaxActiveChanges {
		return nil, errInvalidDeepSeekStatusResponse
	}
	updatedAt := time.UnixMilli(props.InitialDataUpdatedAt).UTC()
	if updatedAt.IsZero() {
		return nil, errInvalidDeepSeekStatusResponse
	}
	historyProps, hasHistory := extractDeepSeekHistoryProps(payload)

	componentStatus := make(map[string]ServiceStatus, len(props.InitialData.Page.Components))
	for _, component := range props.InitialData.Page.Components {
		id := strings.TrimSpace(component.ComponentID)
		if id == "" || strings.TrimSpace(component.Name) == "" || component.AvailableSinceSeconds <= 0 {
			return nil, errInvalidDeepSeekStatusResponse
		}
		if _, duplicate := componentStatus[id]; duplicate {
			return nil, errInvalidDeepSeekStatusResponse
		}
		componentStatus[id] = ServiceStatusOperational
	}
	changeIDs := make(map[int64]struct{}, len(props.InitialData.ActiveChanges))
	for _, change := range props.InitialData.ActiveChanges {
		if change.ChangeID <= 0 || strings.TrimSpace(change.Title) == "" || strings.TrimSpace(change.Status) == "" || change.StartAtSeconds <= 0 {
			return nil, errInvalidDeepSeekStatusResponse
		}
		if len(change.AffectedComponents) > deepSeekMaxAffectedComponents {
			return nil, errInvalidDeepSeekStatusResponse
		}
		if _, duplicate := changeIDs[change.ChangeID]; duplicate {
			return nil, errInvalidDeepSeekStatusResponse
		}
		changeIDs[change.ChangeID] = struct{}{}
		for _, affected := range change.AffectedComponents {
			current, ok := componentStatus[strings.TrimSpace(affected.ComponentID)]
			if !ok {
				return nil, errInvalidDeepSeekStatusResponse
			}
			status, ok := normalizeDeepSeekComponentStatus(affected.Status)
			if !ok {
				return nil, errInvalidDeepSeekStatusResponse
			}
			if serviceStatusSeverity(status) > serviceStatusSeverity(current) {
				componentStatus[strings.TrimSpace(affected.ComponentID)] = status
			}
		}
	}

	type componentWire struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		Group     bool   `json:"group"`
	}
	type incidentComponentWire struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type incidentWire struct {
		ID         string                  `json:"id"`
		Name       string                  `json:"name"`
		Status     string                  `json:"status"`
		Impact     string                  `json:"impact"`
		CreatedAt  string                  `json:"created_at"`
		ResolvedAt *string                 `json:"resolved_at"`
		Components []incidentComponentWire `json:"components"`
	}
	components := make([]componentWire, 0, len(props.InitialData.Page.Components))
	for _, component := range props.InitialData.Page.Components {
		id := strings.TrimSpace(component.ComponentID)
		components = append(components, componentWire{
			ID: id, Name: strings.TrimSpace(component.Name), Status: string(componentStatus[id]),
			CreatedAt: time.Unix(component.AvailableSinceSeconds, 0).UTC().Format(time.RFC3339Nano),
			UpdatedAt: updatedAt.Format(time.RFC3339Nano), Group: false,
		})
	}
	incidents := make([]incidentWire, 0, len(props.InitialData.ActiveChanges))
	for _, change := range props.InitialData.ActiveChanges {
		matched := make([]incidentComponentWire, 0, len(change.AffectedComponents))
		worst := ServiceStatusOperational
		for _, affected := range change.AffectedComponents {
			id := strings.TrimSpace(affected.ComponentID)
			status := componentStatus[id]
			if serviceStatusSeverity(status) > serviceStatusSeverity(worst) {
				worst = status
			}
			matched = append(matched, incidentComponentWire{ID: id, Name: strings.TrimSpace(affected.Name)})
		}
		var resolvedAt *string
		if change.CloseAtSeconds > 0 {
			value := time.Unix(change.CloseAtSeconds, 0).UTC().Format(time.RFC3339Nano)
			resolvedAt = &value
		}
		incidents = append(incidents, incidentWire{
			ID:   strconv.FormatInt(change.ChangeID, 10),
			Name: strings.TrimSpace(change.Title), Status: strings.TrimSpace(change.Status), Impact: deepSeekIncidentImpact(worst),
			CreatedAt: time.Unix(change.StartAtSeconds, 0).UTC().Format(time.RFC3339Nano), ResolvedAt: resolvedAt, Components: matched,
		})
	}
	var historyCoverageStart *string
	if hasHistory {
		if len(historyProps.InitialData.ComponentImpacts) > 1024 || len(historyProps.InitialData.LinkedChanges) > 512 {
			return nil, errInvalidDeepSeekStatusResponse
		}
		linkedTitles := make(map[int64]string, len(historyProps.InitialData.LinkedChanges))
		for _, change := range historyProps.InitialData.LinkedChanges {
			if change.ID <= 0 || strings.TrimSpace(change.Type) != "incident" || strings.TrimSpace(change.Title) == "" {
				return nil, errInvalidDeepSeekStatusResponse
			}
			if _, duplicate := linkedTitles[change.ID]; duplicate {
				return nil, errInvalidDeepSeekStatusResponse
			}
			linkedTitles[change.ID] = strings.TrimSpace(change.Title)
		}
		type groupedImpact struct {
			start      int64
			end        int64
			worst      ServiceStatus
			components map[string]string
		}
		grouped := make(map[int64]*groupedImpact)
		for _, impact := range historyProps.InitialData.ComponentImpacts {
			componentID := strings.TrimSpace(impact.ComponentID)
			if componentID == "" || impact.ChangeID <= 0 || impact.StartAtSeconds <= 0 || impact.EndAtSeconds < impact.StartAtSeconds {
				return nil, errInvalidDeepSeekStatusResponse
			}
			status, ok := normalizeDeepSeekComponentStatus(impact.Status)
			if !ok {
				return nil, errInvalidDeepSeekStatusResponse
			}
			componentName := ""
			for _, component := range props.InitialData.Page.Components {
				if strings.TrimSpace(component.ComponentID) == componentID {
					componentName = strings.TrimSpace(component.Name)
					break
				}
			}
			if componentName == "" {
				continue
			}
			if _, ok := linkedTitles[impact.ChangeID]; !ok {
				return nil, errInvalidDeepSeekStatusResponse
			}
			item := grouped[impact.ChangeID]
			if item == nil {
				item = &groupedImpact{start: impact.StartAtSeconds, end: impact.EndAtSeconds, worst: status, components: make(map[string]string)}
				grouped[impact.ChangeID] = item
			}
			if impact.StartAtSeconds < item.start {
				item.start = impact.StartAtSeconds
			}
			if impact.EndAtSeconds > item.end {
				item.end = impact.EndAtSeconds
			}
			if serviceStatusSeverity(status) > serviceStatusSeverity(item.worst) {
				item.worst = status
			}
			item.components[componentID] = componentName
		}
		for changeID, impact := range grouped {
			if _, active := changeIDs[changeID]; active {
				continue
			}
			matched := make([]incidentComponentWire, 0, len(impact.components))
			for id, name := range impact.components {
				matched = append(matched, incidentComponentWire{ID: id, Name: name})
			}
			sort.Slice(matched, func(left, right int) bool { return matched[left].ID < matched[right].ID })
			resolved := time.Unix(impact.end, 0).UTC().Format(time.RFC3339Nano)
			incidents = append(incidents, incidentWire{
				ID: strconv.FormatInt(changeID, 10), Name: linkedTitles[changeID], Status: "resolved",
				Impact:     deepSeekIncidentImpact(impact.worst),
				CreatedAt:  time.Unix(impact.start, 0).UTC().Format(time.RFC3339Nano),
				ResolvedAt: &resolved, Components: matched,
			})
		}
		sort.Slice(incidents, func(left, right int) bool {
			if incidents[left].CreatedAt == incidents[right].CreatedAt {
				return incidents[left].ID < incidents[right].ID
			}
			return incidents[left].CreatedAt > incidents[right].CreatedAt
		})
		coverage := utcDayStart(updatedAt).AddDate(0, 0, -(serviceHealthHistoryDays - 1)).Format(time.RFC3339)
		historyCoverageStart = &coverage
	}

	overall := ServiceStatusOperational
	for _, status := range componentStatus {
		if serviceStatusSeverity(status) > serviceStatusSeverity(overall) {
			overall = status
		}
	}
	envelope := struct {
		Page struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			URL       string `json:"url"`
			UpdatedAt string `json:"updated_at"`
		} `json:"page"`
		Status struct {
			Indicator   string `json:"indicator"`
			Description string `json:"description"`
		} `json:"status"`
		Components                []componentWire `json:"components"`
		Incidents                 []incidentWire  `json:"incidents"`
		RadarHistoryCoverageStart *string         `json:"radar_history_coverage_start,omitempty"`
	}{}
	envelope.Page.ID = "deepseek"
	envelope.Page.Name = "DeepSeek Status"
	envelope.Page.URL = deepSeekStatuspagePublicURL
	envelope.Page.UpdatedAt = updatedAt.Format(time.RFC3339Nano)
	envelope.Status.Indicator = string(statusIndicatorForServiceStatus(overall))
	envelope.Status.Description = string(overall)
	envelope.Components = components
	envelope.Incidents = incidents
	envelope.RadarHistoryCoverageStart = historyCoverageStart
	canonical, err := json.Marshal(envelope)
	if err != nil {
		return nil, errInvalidDeepSeekStatusResponse
	}
	if _, err := DecodeStatuspageSummary(canonical); err != nil {
		return nil, errInvalidDeepSeekStatusResponse
	}
	return canonical, nil
}

// DecodeDeepSeekStatusPage validates the official server-rendered DeepSeek
// status payload and returns the shared normalized status summary.
func DecodeDeepSeekStatusPage(payload []byte) (StatuspageSummary, error) {
	canonical, err := decodeDeepSeekStatusPage(payload)
	if err != nil {
		return StatuspageSummary{}, errInvalidDeepSeekStatusResponse
	}
	summary, err := DecodeStatuspageSummary(canonical)
	if err != nil {
		return StatuspageSummary{}, errInvalidDeepSeekStatusResponse
	}
	return summary, nil
}

func extractDeepSeekFlightProps(payload []byte) (deepSeekFlightProps, error) {
	tokenizer := html.NewTokenizer(bytes.NewReader(payload))
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return deepSeekFlightProps{}, errInvalidDeepSeekStatusResponse
		case html.StartTagToken:
			token := tokenizer.Token()
			if token.Data != "script" {
				continue
			}
			if tokenizer.Next() != html.TextToken {
				continue
			}
			if props, ok := decodeDeepSeekFlightScript(string(tokenizer.Text())); ok {
				return props, nil
			}
		}
	}
}

func extractDeepSeekHistoryProps(payload []byte) (deepSeekHistoryProps, bool) {
	tokenizer := html.NewTokenizer(bytes.NewReader(payload))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return deepSeekHistoryProps{}, false
		case html.StartTagToken:
			token := tokenizer.Token()
			if token.Data != "script" || tokenizer.Next() != html.TextToken {
				continue
			}
			if props, ok := decodeDeepSeekFlightHistoryScript(string(tokenizer.Text())); ok {
				return props, true
			}
		}
	}
}

func decodeDeepSeekFlightHistoryScript(script string) (deepSeekHistoryProps, bool) {
	const prefix = "self.__next_f.push("
	index := strings.Index(script, prefix)
	if index < 0 {
		return deepSeekHistoryProps{}, false
	}
	decoder := json.NewDecoder(strings.NewReader(script[index+len(prefix):]))
	var frame []json.RawMessage
	if err := decoder.Decode(&frame); err != nil || len(frame) != 2 {
		return deepSeekHistoryProps{}, false
	}
	var segment string
	if err := json.Unmarshal(frame[1], &segment); err != nil {
		return deepSeekHistoryProps{}, false
	}
	for _, record := range strings.Split(segment, "\n") {
		colon := strings.IndexByte(record, ':')
		if colon <= 0 || colon == len(record)-1 {
			continue
		}
		var node any
		if err := json.Unmarshal([]byte(record[colon+1:]), &node); err != nil {
			continue
		}
		if props, ok := findDeepSeekHistoryProps(node); ok {
			return props, true
		}
	}
	return deepSeekHistoryProps{}, false
}

func findDeepSeekHistoryProps(node any) (deepSeekHistoryProps, bool) {
	switch value := node.(type) {
	case map[string]any:
		if initial, ok := value["initialData"].(map[string]any); ok {
			_, hasImpacts := initial["component_impacts"]
			_, hasChanges := initial["linked_changes"]
			if hasImpacts && hasChanges {
				encoded, err := json.Marshal(value)
				if err == nil {
					var props deepSeekHistoryProps
					if json.Unmarshal(encoded, &props) == nil {
						return props, true
					}
				}
			}
		}
		for _, child := range value {
			if props, ok := findDeepSeekHistoryProps(child); ok {
				return props, true
			}
		}
	case []any:
		for _, child := range value {
			if props, ok := findDeepSeekHistoryProps(child); ok {
				return props, true
			}
		}
	}
	return deepSeekHistoryProps{}, false
}

func decodeDeepSeekFlightScript(script string) (deepSeekFlightProps, bool) {
	const prefix = "self.__next_f.push("
	index := strings.Index(script, prefix)
	if index < 0 {
		return deepSeekFlightProps{}, false
	}
	decoder := json.NewDecoder(strings.NewReader(script[index+len(prefix):]))
	var frame []json.RawMessage
	if err := decoder.Decode(&frame); err != nil || len(frame) != 2 {
		return deepSeekFlightProps{}, false
	}
	var segment string
	if err := json.Unmarshal(frame[1], &segment); err != nil {
		return deepSeekFlightProps{}, false
	}
	// A Next.js Flight frame can contain many newline-delimited records. The
	// production page currently places the status props after module and text
	// records, so parsing only the first record silently misses the live data.
	for _, record := range strings.Split(segment, "\n") {
		colon := strings.IndexByte(record, ':')
		if colon <= 0 || colon == len(record)-1 {
			continue
		}
		var node any
		if err := json.Unmarshal([]byte(record[colon+1:]), &node); err != nil {
			continue
		}
		if props, ok := findDeepSeekFlightProps(node); ok {
			return props, true
		}
	}
	return deepSeekFlightProps{}, false
}

func findDeepSeekFlightProps(node any) (deepSeekFlightProps, bool) {
	switch value := node.(type) {
	case map[string]any:
		if _, hasInitial := value["initialData"]; hasInitial {
			encoded, err := json.Marshal(value)
			if err == nil {
				var props deepSeekFlightProps
				if json.Unmarshal(encoded, &props) == nil && props.InitialData.Page.PageID > 0 {
					return props, true
				}
			}
		}
		for _, child := range value {
			if props, ok := findDeepSeekFlightProps(child); ok {
				return props, true
			}
		}
	case []any:
		for _, child := range value {
			if props, ok := findDeepSeekFlightProps(child); ok {
				return props, true
			}
		}
	}
	return deepSeekFlightProps{}, false
}

func normalizeDeepSeekComponentStatus(raw string) (ServiceStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "operational":
		return ServiceStatusOperational, true
	case "degraded":
		return ServiceStatusDegradedPerformance, true
	case "partial_outage":
		return ServiceStatusPartialOutage, true
	case "full_outage":
		return ServiceStatusMajorOutage, true
	case "maintenance":
		return ServiceStatusUnderMaintenance, true
	default:
		return ServiceStatusUnknown, false
	}
}

func deepSeekIncidentImpact(status ServiceStatus) string {
	switch status {
	case ServiceStatusMajorOutage:
		return "critical"
	case ServiceStatusPartialOutage:
		return "major"
	default:
		return "minor"
	}
}

var _ RadarFetcher = (*deepSeekStatusFetcher)(nil)
