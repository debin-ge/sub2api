package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitorrequesttemplate"
)

func TestChannelMonitorDomesticProvidersAreSupported(t *testing.T) {
	for _, provider := range []string{
		MonitorProviderMiniMax,
		MonitorProviderGLM,
		MonitorProviderKimi,
		MonitorProviderDeepSeek,
		MonitorProviderWindsurf,
	} {
		t.Run(provider, func(t *testing.T) {
			if err := validateProvider(provider); err != nil {
				t.Fatalf("validateProvider(%q) error = %v", provider, err)
			}
		})
	}
}

func TestChannelMonitorDomesticProviderAdapters(t *testing.T) {
	cases := []struct {
		provider string
		model    string
		path     string
		textPath string
		authKey  string
	}{
		{provider: MonitorProviderMiniMax, model: "MiniMax-M2.7", path: "/anthropic/v1/messages", textPath: "content.0.text", authKey: "Authorization"},
		{provider: MonitorProviderGLM, model: "GLM-5.1", path: "/api/anthropic/v1/messages", textPath: "content.0.text", authKey: "Authorization"},
		{provider: MonitorProviderKimi, model: "kimi-for-coding", path: "/coding/v1/messages", textPath: "content.0.text", authKey: "Authorization"},
		{provider: MonitorProviderDeepSeek, model: "deepseek-v4-flash", path: "/chat/completions", textPath: "choices.0.message.content", authKey: "Authorization"},
		{provider: MonitorProviderWindsurf, model: "claude-sonnet-4.6", path: "/v1/chat/completions", textPath: "choices.0.message.content", authKey: "Authorization"},
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			adapter, ok := providerAdapters[tc.provider]
			if !ok {
				t.Fatalf("adapter for %q not found", tc.provider)
			}
			if got := adapter.buildPath(tc.model); got != tc.path {
				t.Fatalf("path = %q, want %q", got, tc.path)
			}
			if adapter.textPath != tc.textPath {
				t.Fatalf("textPath = %q, want %q", adapter.textPath, tc.textPath)
			}
			headers := adapter.buildHeaders("sk-test")
			if got := headers[tc.authKey]; got != "Bearer sk-test" {
				t.Fatalf("%s = %q, want Bearer sk-test", tc.authKey, got)
			}

			bodyBytes, err := adapter.buildBody(tc.model, "what is 1+1?")
			if err != nil {
				t.Fatalf("buildBody error = %v", err)
			}
			var body map[string]any
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}
			if body["model"] != tc.model {
				t.Fatalf("body model = %v, want %q; body=%s", body["model"], tc.model, string(bodyBytes))
			}
			if _, ok := body["messages"]; !ok {
				t.Fatalf("body missing messages: %s", string(bodyBytes))
			}
		})
	}
}

func TestChannelMonitorDomesticProviderMergeDenyList(t *testing.T) {
	for _, provider := range []string{MonitorProviderMiniMax, MonitorProviderGLM, MonitorProviderKimi, MonitorProviderDeepSeek, MonitorProviderWindsurf} {
		t.Run(provider, func(t *testing.T) {
			deny := bodyMergeKeyDenyList[provider]
			if !deny["model"] || !deny["messages"] {
				t.Fatalf("provider %q deny list should protect model and messages: %#v", provider, deny)
			}
		})
	}
}

func TestChannelMonitorTemplateDomesticProvidersAreValid(t *testing.T) {
	for _, provider := range []string{MonitorProviderMiniMax, MonitorProviderGLM, MonitorProviderKimi, MonitorProviderDeepSeek, MonitorProviderWindsurf} {
		t.Run(provider, func(t *testing.T) {
			err := validateTemplateCreateParams(ChannelMonitorRequestTemplateCreateParams{
				Name:             "domestic",
				Provider:         provider,
				BodyOverrideMode: MonitorBodyOverrideModeOff,
			})
			if err != nil {
				t.Fatalf("validateTemplateCreateParams(%q) error = %v", provider, err)
			}
		})
	}
}

func TestChannelMonitorEntDomesticProviderValidators(t *testing.T) {
	for _, provider := range []string{MonitorProviderMiniMax, MonitorProviderGLM, MonitorProviderKimi, MonitorProviderDeepSeek, MonitorProviderWindsurf} {
		t.Run(provider, func(t *testing.T) {
			if err := channelmonitor.ProviderValidator(channelmonitor.Provider(provider)); err != nil {
				t.Fatalf("channelmonitor provider validator error = %v", err)
			}
			if err := channelmonitorrequesttemplate.ProviderValidator(channelmonitorrequesttemplate.Provider(provider)); err != nil {
				t.Fatalf("channelmonitorrequesttemplate provider validator error = %v", err)
			}
		})
	}
}
