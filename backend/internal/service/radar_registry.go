package service

import "sort"

const (
	claudeStatuspageAPIURL                 = "https://status.claude.com/api/v2/summary.json"
	openAIStatuspageAPIURL                 = "https://status.openai.com/api/v2/summary.json"
	windsurfStatuspageAPIURL               = "https://status.windsurf.com/api/v2/summary.json"
	kimiStatuspageAPIURL                   = "https://status.moonshot.cn/api/v2/summary.json"
	miniMaxGlobalStatuspageAPIURL          = "https://status.minimax.io/api/v2/summary.json"
	miniMaxChinaStatuspageAPIURL           = "https://status.minimaxi.com/api/v2/summary.json"
	claudeStatuspageIncidentsURL           = "https://status.claude.com/api/v2/incidents.json"
	openAIStatuspageIncidentsURL           = "https://status.openai.com/api/v2/incidents.json"
	windsurfStatuspageIncidentsURL         = "https://status.windsurf.com/api/v2/incidents.json"
	kimiStatuspageIncidentsURL             = "https://status.moonshot.cn/api/v2/incidents.json"
	miniMaxGlobalIncidentsURL              = "https://status.minimax.io/api/v2/incidents.json"
	miniMaxChinaIncidentsURL               = "https://status.minimaxi.com/api/v2/incidents.json"
	openAIStatuspageFeedURL                = "https://status.openai.com/feed.atom"
	openAIStatusSummaryURL                 = "https://status.openai.com/proxy/openai-1"
	openAIComponentImpactsURL              = "https://status.openai.com/proxy/openai-1/component_impacts"
	claudeAPIHistoryURL                    = "https://status.claude.com/history?filter=k8w3r06qmzrp"
	claudeCodeHistoryURL                   = "https://status.claude.com/history?filter=yyzkbfz2thpt"
	windsurfHistoryURL                     = "https://status.windsurf.com/history?filter=8q19cygxvshj,r5wf1ykd7y1m"
	kimiHistoryURL                         = "https://status.moonshot.cn/history?filter=8psr5dfdld0s,8rkd3yj051gl,lk7q3z0fcylp,p1j9ttb7jwhp,rf64wcbxt3r2,wmn9wzv84k1v,x0zsqgy57b75,z2zfp65lvb2z"
	miniMaxGlobalLLMHistoryURL             = "https://status.minimax.io/history?filter=pr0d8qr59svt"
	miniMaxChinaLLMHistoryURL              = "https://status.minimaxi.com/history?filter=vwp8mgy34fck"
	claudeStatuspagePublicURL              = "https://status.claude.com"
	openAIStatuspagePublicURL              = "https://status.openai.com"
	windsurfStatuspagePublicURL            = "https://status.windsurf.com"
	deepSeekStatuspagePublicURL            = "https://status.deepseek.com"
	kimiStatuspagePublicURL                = "https://status.moonshot.cn"
	miniMaxGlobalStatuspagePublicURL       = "https://status.minimax.io"
	miniMaxChinaStatuspagePublicURL        = "https://status.minimaxi.com"
	miniMaxChinaLLMComponentID             = "vwp8mgy34fck"
	miniMaxChinaLLMComponentName           = "大语言模型LLM"
	deepSeekStatusDataURL                  = "https://statuspage.flashcat.cloud/deepseek"
	deepSeekStatusPageID             int64 = 6410630422455
	deepSeekStatusPageName                 = "DeepSeek"
	deepSeekStatusCustomDomain             = "status.deepseek.com"
)

const (
	serviceHealthHistoryDays          = 30
	serviceHealthUptimeWindowDays     = 90
	serviceHealthRecentIncidentDays   = 7
	serviceHealthIncidentPreviewLimit = 3
)

const (
	radarBenchmarkMaxSelectedModels = 10
	radarBenchmarkDefaultModelCount = 6
	radarBenchmarkScoreMin          = 0
	radarBenchmarkScoreMax          = 100
	radarBenchmarkScoreStep         = 25
	radarLMArenaCatalogLimit        = 10
)

var radarBenchmarkMetricKeys = []string{
	"intelligence_index",
	"coding_index",
	"agentic_index",
}

// radarSourceRegistration is the single source of truth for public Radar
// source identity, health-platform metadata, and pinned external contracts.
type radarSourceRegistration struct {
	Source        RadarSourceKey
	Name          string
	PublicURL     string
	Platform      string
	PlatformOrder int
	ScheduleOrder int
}

type statuspageCalendarSpec struct {
	serviceKey   ServiceKey
	endpoint     string
	componentIDs []string
}

type radarStatuspageContract struct {
	SummaryEndpoint          string
	IncidentsEndpoint        string
	PublicURL                string
	FeedEndpoint             string
	StatusSummaryEndpoint    string
	ComponentImpactsEndpoint string
	CalendarSpecs            []statuspageCalendarSpec
	ServiceAliases           map[ServiceKey]map[string]struct{}
}

func radarAliases(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func radarStatuspageContractFor(source RadarSourceKey) (radarStatuspageContract, bool) {
	switch source {
	case RadarSourceStatusClaude:
		return radarStatuspageContract{
			SummaryEndpoint:   claudeStatuspageAPIURL,
			IncidentsEndpoint: claudeStatuspageIncidentsURL,
			PublicURL:         claudeStatuspagePublicURL,
			CalendarSpecs: []statuspageCalendarSpec{
				{serviceKey: ServiceKeyClaudeAPI, endpoint: claudeAPIHistoryURL, componentIDs: []string{"k8w3r06qmzrp"}},
				{serviceKey: ServiceKeyClaudeCode, endpoint: claudeCodeHistoryURL, componentIDs: []string{"yyzkbfz2thpt"}},
			},
			ServiceAliases: map[ServiceKey]map[string]struct{}{
				ServiceKeyClaudeAPI:  radarAliases("claude api", "claude api (api.anthropic.com)"),
				ServiceKeyClaudeCode: radarAliases("claude code"),
			},
		}, true
	case RadarSourceStatusOpenAI:
		return radarStatuspageContract{
			SummaryEndpoint:          openAIStatuspageAPIURL,
			IncidentsEndpoint:        openAIStatuspageIncidentsURL,
			PublicURL:                openAIStatuspagePublicURL,
			FeedEndpoint:             openAIStatuspageFeedURL,
			StatusSummaryEndpoint:    openAIStatusSummaryURL,
			ComponentImpactsEndpoint: openAIComponentImpactsURL,
			ServiceAliases: map[ServiceKey]map[string]struct{}{
				ServiceKeyCodexWeb: radarAliases("codex web", "chatgpt codex", "codex in chatgpt desktop"),
				ServiceKeyOpenAIAPI: radarAliases(
					"api", "apis", "openai api", "codex api", "responses", "responses api",
					"batch", "batch api", "audio", "audio api", "embeddings", "embeddings api",
					"moderations", "moderations api", "files", "files api", "fine-tuning",
					"fine-tuning api", "fine tuning", "fine tuning api", "chat completions",
					"chat completions api", "completions", "completions api", "assistants",
					"assistants api", "images", "images api", "image generation", "realtime",
					"realtime api", "uploads", "uploads api", "compliance api", "ads api",
				),
			},
		}, true
	case RadarSourceStatusWindsurf:
		return radarStatuspageContract{
			SummaryEndpoint:   windsurfStatuspageAPIURL,
			IncidentsEndpoint: windsurfStatuspageIncidentsURL,
			PublicURL:         windsurfStatuspagePublicURL,
			CalendarSpecs: []statuspageCalendarSpec{{
				serviceKey: ServiceKeyWindsurf, endpoint: windsurfHistoryURL,
				componentIDs: []string{"8q19cygxvshj", "r5wf1ykd7y1m"},
			}},
			ServiceAliases: map[ServiceKey]map[string]struct{}{
				ServiceKeyWindsurf: radarAliases("cascade", "windsurf tab"),
			},
		}, true
	case RadarSourceStatusDeepSeek:
		return radarStatuspageContract{
			PublicURL: deepSeekStatuspagePublicURL,
			ServiceAliases: map[ServiceKey]map[string]struct{}{
				ServiceKeyDeepSeek: radarAliases(
					"api service", "api 服务 (api service)", "deepseek v4 pro api服务", "deepseek v4 flash api服务",
				),
			},
		}, true
	case RadarSourceStatusKimi:
		return radarStatuspageContract{
			SummaryEndpoint:   kimiStatuspageAPIURL,
			IncidentsEndpoint: kimiStatuspageIncidentsURL,
			PublicURL:         kimiStatuspagePublicURL,
			CalendarSpecs: []statuspageCalendarSpec{{
				serviceKey: ServiceKeyKimi, endpoint: kimiHistoryURL,
				componentIDs: []string{"8psr5dfdld0s", "8rkd3yj051gl", "lk7q3z0fcylp", "p1j9ttb7jwhp", "rf64wcbxt3r2", "wmn9wzv84k1v", "x0zsqgy57b75", "z2zfp65lvb2z"},
			}},
			ServiceAliases: map[ServiceKey]map[string]struct{}{
				ServiceKeyKimi: radarAliases("open api", "api service", "model", "vision model", "thinking model", "text model", "research model", "k2 model"),
			},
		}, true
	case RadarSourceStatusMiniMaxGlobal:
		return radarStatuspageContract{
			SummaryEndpoint:   miniMaxGlobalStatuspageAPIURL,
			IncidentsEndpoint: miniMaxGlobalIncidentsURL,
			PublicURL:         miniMaxGlobalStatuspagePublicURL,
			CalendarSpecs: []statuspageCalendarSpec{{
				serviceKey: ServiceKeyMiniMax, endpoint: miniMaxGlobalLLMHistoryURL, componentIDs: []string{"pr0d8qr59svt"},
			}},
			ServiceAliases: map[ServiceKey]map[string]struct{}{
				ServiceKeyMiniMax: radarAliases("large language models (llm)"),
			},
		}, true
	case RadarSourceStatusMiniMaxChina:
		return radarStatuspageContract{
			SummaryEndpoint:   miniMaxChinaStatuspageAPIURL,
			IncidentsEndpoint: miniMaxChinaIncidentsURL,
			PublicURL:         miniMaxChinaStatuspagePublicURL,
			CalendarSpecs: []statuspageCalendarSpec{{
				serviceKey: ServiceKeyMiniMax, endpoint: miniMaxChinaLLMHistoryURL, componentIDs: []string{miniMaxChinaLLMComponentID},
			}},
			ServiceAliases: map[ServiceKey]map[string]struct{}{
				ServiceKeyMiniMax: radarAliases("大语言模型llm"),
			},
		}, true
	default:
		return radarStatuspageContract{}, false
	}
}

func radarSourceRegistrations() []radarSourceRegistration {
	return []radarSourceRegistration{
		{
			Source:    RadarSourceAA,
			Name:      "Artificial Analysis",
			PublicURL: radarServiceArtificialAnalysisPublicURL,
		},
		{
			Source:    RadarSourceLMArena,
			Name:      "LMArena",
			PublicURL: radarServiceLMArenaPublicURL,
		},
		{
			Source:        RadarSourceStatusClaude,
			Name:          "Claude Status",
			PublicURL:     claudeStatuspagePublicURL,
			Platform:      PlatformAnthropic,
			PlatformOrder: 0,
			ScheduleOrder: 0,
		},
		{
			Source:        RadarSourceStatusOpenAI,
			Name:          "OpenAI Status",
			PublicURL:     openAIStatuspagePublicURL,
			Platform:      PlatformOpenAI,
			PlatformOrder: 4,
			ScheduleOrder: 1,
		},
		{
			Source:        RadarSourceStatusWindsurf,
			Name:          "Windsurf Status",
			PublicURL:     windsurfStatuspagePublicURL,
			Platform:      "windsurf",
			PlatformOrder: 5,
			ScheduleOrder: 2,
		},
		{
			Source:        RadarSourceStatusDeepSeek,
			Name:          "DeepSeek Status",
			PublicURL:     deepSeekStatuspagePublicURL,
			Platform:      "deepseek",
			PlatformOrder: 1,
			ScheduleOrder: 5,
		},
		{
			Source:        RadarSourceStatusKimi,
			Name:          "Kimi Status",
			PublicURL:     kimiStatuspagePublicURL,
			Platform:      "kimi",
			PlatformOrder: 2,
			ScheduleOrder: 3,
		},
		{
			Source:        RadarSourceStatusMiniMaxChina,
			Name:          "MiniMax China Status",
			PublicURL:     miniMaxChinaStatuspagePublicURL,
			Platform:      "minimax",
			PlatformOrder: 3,
			ScheduleOrder: 4,
		},
	}
}

func radarSourceRegistrationFor(source RadarSourceKey) (radarSourceRegistration, bool) {
	if source == RadarSourceStatusMiniMaxGlobal {
		source = RadarSourceStatusMiniMaxChina
	}
	for _, registration := range radarSourceRegistrations() {
		if registration.Source == source {
			return registration, true
		}
	}
	return radarSourceRegistration{}, false
}

func statuspageRadarSources() []RadarSourceKey {
	registrations := make([]radarSourceRegistration, 0, 6)
	for _, registration := range radarSourceRegistrations() {
		if registration.Platform != "" {
			registrations = append(registrations, registration)
		}
	}
	sort.Slice(registrations, func(left, right int) bool {
		return registrations[left].ScheduleOrder < registrations[right].ScheduleOrder
	})
	result := make([]RadarSourceKey, len(registrations))
	for index := range registrations {
		result[index] = registrations[index].Source
	}
	return result
}

type radarServiceRegistration struct {
	Descriptor    RadarServiceDescriptor
	Source        RadarSourceKey
	AlwaysPresent bool
}

func radarServiceRegistrations() []radarServiceRegistration {
	return []radarServiceRegistration{
		{
			Descriptor:    RadarServiceDescriptor{Key: ServiceKeyClaudeAPI, Name: "Claude API"},
			Source:        RadarSourceStatusClaude,
			AlwaysPresent: true,
		},
		{
			Descriptor:    RadarServiceDescriptor{Key: ServiceKeyClaudeCode, Name: "Claude Code"},
			Source:        RadarSourceStatusClaude,
			AlwaysPresent: true,
		},
		{
			Descriptor:    RadarServiceDescriptor{Key: ServiceKeyCodexWeb, Name: "Codex Web"},
			Source:        RadarSourceStatusOpenAI,
			AlwaysPresent: true,
		},
		{
			Descriptor:    RadarServiceDescriptor{Key: ServiceKeyOpenAIAPI, Name: "OpenAI API"},
			Source:        RadarSourceStatusOpenAI,
			AlwaysPresent: true,
		},
		{
			Descriptor: RadarServiceDescriptor{Key: ServiceKeyWindsurf, Name: "Windsurf"},
			Source:     RadarSourceStatusWindsurf,
		},
		{
			Descriptor: RadarServiceDescriptor{Key: ServiceKeyDeepSeek, Name: "DeepSeek"},
			Source:     RadarSourceStatusDeepSeek,
		},
		{
			Descriptor: RadarServiceDescriptor{Key: ServiceKeyKimi, Name: "Kimi"},
			Source:     RadarSourceStatusKimi,
		},
		{
			Descriptor: RadarServiceDescriptor{Key: ServiceKeyMiniMax, Name: "MiniMax"},
			Source:     RadarSourceStatusMiniMaxChina,
		},
	}
}

func radarAllServiceDescriptors() []RadarServiceDescriptor {
	registrations := radarServiceRegistrations()
	result := make([]RadarServiceDescriptor, 0, len(registrations))
	for _, registration := range registrations {
		result = append(result, registration.Descriptor)
	}
	return result
}

func radarServiceDescriptorsForSource(source RadarSourceKey) []RadarServiceDescriptor {
	if source == RadarSourceStatusMiniMaxGlobal {
		source = RadarSourceStatusMiniMaxChina
	}
	result := make([]RadarServiceDescriptor, 0, 2)
	for _, registration := range radarServiceRegistrations() {
		if registration.Source == source {
			result = append(result, registration.Descriptor)
		}
	}
	return result
}

func radarServiceRegistrationForKey(serviceKey ServiceKey) (radarServiceRegistration, bool) {
	for _, registration := range radarServiceRegistrations() {
		if registration.Descriptor.Key == serviceKey {
			return registration, true
		}
	}
	return radarServiceRegistration{}, false
}

func radarServicePlatform(serviceKey ServiceKey) (string, int, bool) {
	service, ok := radarServiceRegistrationForKey(serviceKey)
	if !ok {
		return "", 0, false
	}
	source, ok := radarSourceRegistrationFor(service.Source)
	if !ok || source.Platform == "" {
		return "", 0, false
	}
	return source.Platform, source.PlatformOrder, true
}
