package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	claudeStatuspageAPIURL    = "https://status.claude.com/api/v2/summary.json"
	openAIStatuspageAPIURL    = "https://status.openai.com/api/v2/summary.json"
	claudeStatuspagePublicURL = "https://status.claude.com"
	openAIStatuspagePublicURL = "https://status.openai.com"
)

var errInvalidStatuspageResponse = errors.New("invalid Statuspage response")

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
)

// StatuspageSummary is the validated subset of a Statuspage v2 summary used
// by Radar. All timestamps are normalized to UTC.
type StatuspageSummary struct {
	Page       StatuspagePage
	Status     StatuspageOverallStatus
	Components []StatuspageComponent
	Incidents  []StatuspageIncident
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
	Page       *statuspagePageWire        `json:"page"`
	Status     *statuspageOverallWire     `json:"status"`
	Components *[]statuspageComponentWire `json:"components"`
	Incidents  []statuspageIncidentWire   `json:"incidents"`
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

// NewStatuspageFetcher constructs one of the two allowlisted Statuspage
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

	endpoint := ""
	switch source {
	case RadarSourceStatusClaude:
		endpoint = claudeStatuspageAPIURL
	case RadarSourceStatusOpenAI:
		endpoint = openAIStatuspageAPIURL
	default:
		return nil, &RadarFetcherConfigError{Field: "statuspage_source"}
	}

	return newRadarHTTPFetcher(radarHTTPFetcherOptions{
		source:           source,
		interval:         time.Duration(cfg.Radar.StatuspageIntervalMinutes) * time.Minute,
		client:           client,
		endpoint:         endpoint,
		maxResponseBytes: cfg.Radar.ExternalResponseMaxBytes,
		validate: func(payload []byte) error {
			_, err := DecodeStatuspageSummary(payload)
			return err
		},
	})
}

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
	for _, incidentWire := range wire.Incidents {
		incident, err := decodeStatuspageIncident(incidentWire)
		if err != nil {
			return StatuspageSummary{}, errInvalidStatuspageResponse
		}
		incidents = append(incidents, incident)
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
		Components: components,
		Incidents:  incidents,
	}, nil
}

// MapStatuspageServiceHealth returns exactly two stable cards for one source.
// Unknown component states are localized to the affected card rather than
// invalidating the full response.
func MapStatuspageServiceHealth(source RadarSourceKey, summary StatuspageSummary) ([]ServiceHealthDTO, error) {
	if err := validateStatuspageSummary(summary); err != nil {
		return nil, errInvalidStatuspageResponse
	}

	var descriptors []RadarServiceDescriptor
	var publicURL string
	switch source {
	case RadarSourceStatusClaude:
		descriptors = CanonicalRadarServices()[:2]
		publicURL = claudeStatuspagePublicURL
	case RadarSourceStatusOpenAI:
		descriptors = CanonicalRadarServices()[2:]
		publicURL = openAIStatuspagePublicURL
	default:
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
			SourceURL:       publicURL,
			Stale:           false,
		})
	}
	return cards, nil
}

// MergeStatuspageServiceHealth produces the four canonical cards in stable
// display order. Missing or malformed group entries become unknown, never
// operational.
func MergeStatuspageServiceHealth(groups ...[]ServiceHealthDTO) []ServiceHealthDTO {
	byKey := make(map[ServiceKey]ServiceHealthDTO, 4)
	for _, group := range groups {
		for _, card := range group {
			switch card.ServiceKey {
			case ServiceKeyClaudeAPI, ServiceKeyClaudeCode, ServiceKeyCodexWeb, ServiceKeyOpenAIAPI:
				byKey[card.ServiceKey] = cloneServiceHealthDTO(card)
			}
		}
	}

	descriptors := CanonicalRadarServices()
	result := make([]ServiceHealthDTO, 0, len(descriptors))
	for _, descriptor := range descriptors {
		card, ok := byKey[descriptor.Key]
		if !ok {
			card = unknownStatuspageCard(descriptor)
		}
		card.ServiceKey = descriptor.Key
		card.Name = descriptor.Name
		card.Status = canonicalServiceStatus(card.Status)
		card.StatusIndicator = statusIndicatorForServiceStatus(card.Status)
		card.Uptime90d = nil
		if descriptor.Key == ServiceKeyClaudeAPI || descriptor.Key == ServiceKeyClaudeCode {
			card.SourceURL = claudeStatuspagePublicURL
		} else {
			card.SourceURL = openAIStatuspagePublicURL
		}
		result = append(result, card)
	}
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
		if err != nil {
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
		// Statuspage represents page-level incidents with an empty components
		// array. Scoped incidents still require an exact safe association.
		if len(incident.Components) > 0 && !statuspageIncidentMatches(source, serviceKey, matchedIDs, incident.Components) {
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
	if serviceKey == ServiceKeyClaudeAPI || serviceKey == ServiceKeyClaudeCode {
		return claudeStatuspagePublicURL
	}
	return openAIStatuspagePublicURL
}

func cloneServiceHealthDTO(input ServiceHealthDTO) ServiceHealthDTO {
	result := input
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
