package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type glmSchedulingAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (r glmSchedulingAccountRepoStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, errors.New("account not found")
}

func (r glmSchedulingAccountRepoStub) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform && acc.IsSchedulable() {
			result = append(result, acc)
		}
	}
	return result, nil
}

func TestAccountGLMHelpersUseFixedEndpoints(t *testing.T) {
	acc := &Account{
		Platform: PlatformGLM,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":            "  sk-glm-test \n",
			"base_url_anthropic": "https://proxy.example/anthropic",
			"base_url_openai":    "https://proxy.example/openai",
			"base_url":           "https://proxy.example/base",
		},
		Extra: map[string]any{
			"base_url_anthropic": "https://extra.example/anthropic",
			"base_url_openai":    "https://extra.example/openai",
		},
	}

	if !acc.IsGLM() {
		t.Fatalf("expected IsGLM true")
	}
	if !acc.IsGLMCodingPlan() {
		t.Fatalf("expected IsGLMCodingPlan true")
	}
	if got := acc.GetGLMAPIKey(); got != "sk-glm-test" {
		t.Fatalf("api key = %q", got)
	}
	if got := acc.GetGLMAnthropicBaseURL(); got != "https://open.bigmodel.cn/api/anthropic" {
		t.Fatalf("anthropic base url = %q", got)
	}
	if got := acc.GetGLMOpenAIBaseURL(); got != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Fatalf("openai base url = %q", got)
	}
}

func TestAccountGLMModelMappingAndNormalization(t *testing.T) {
	acc := &Account{
		Platform: PlatformGLM,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-glm-test",
			"model_mapping": map[string]any{
				"custom-model": "GLM-custom",
				"GLM-5.1":      "GLM-overridden",
				"GLM-4.7":      "GLM-overridden",
				"GLM-4.5-air":  "GLM-overridden",
			},
		},
	}

	cases := []struct {
		name  string
		model string
		want  string
	}{
		{name: "sonnet default", model: "claude-sonnet-4-5", want: "GLM-5.1"},
		{name: "opus default", model: "claude-opus-4-5", want: "GLM-5.1"},
		{name: "haiku default", model: "claude-haiku-4-5", want: "GLM-4.5-air"},
		{name: "glm 5.1 lower", model: " glm-5.1 ", want: "GLM-5.1"},
		{name: "glm 4.7 lower", model: "glm-4.7", want: "GLM-4.7"},
		{name: "glm 4.5 air lower", model: "glm-4.5-air", want: "GLM-4.5-air"},
		{name: "glm 5.1 canonical ignores explicit mapping", model: "GLM-5.1", want: "GLM-5.1"},
		{name: "glm 4.7 canonical ignores explicit mapping", model: "GLM-4.7", want: "GLM-4.7"},
		{name: "glm 4.5 air canonical ignores explicit mapping", model: "GLM-4.5-air", want: "GLM-4.5-air"},
		{name: "explicit mapping", model: "custom-model", want: "GLM-custom"},
		{name: "unknown passthrough trimmed", model: "  other-model  ", want: "other-model"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acc.GetGLMMappedModel(tc.model); got != tc.want {
				t.Fatalf("GetGLMMappedModel(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}

func TestAccountGLMInvalidAccountHelpers(t *testing.T) {
	acc := &Account{Platform: PlatformGLM, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "sk-glm-test"}}
	if acc.IsGLMCodingPlan() {
		t.Fatalf("expected OAuth GLM account not to be coding plan")
	}

	other := &Account{Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-minimax"}}
	if other.IsGLM() {
		t.Fatalf("expected non-GLM account IsGLM false")
	}
	if got := other.GetGLMAPIKey(); got != "" {
		t.Fatalf("non-GLM api key = %q", got)
	}
}

func TestGatewayService_SelectAccountForModelWithPlatform_GLMScheduling(t *testing.T) {
	ctx := context.Background()
	repo := glmSchedulingAccountRepoStub{
		accounts: []Account{
			{ID: 1, Platform: PlatformGLM, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "sk-glm"}},
			{ID: 2, Platform: PlatformAnthropic, Priority: 1, Status: StatusActive, Schedulable: true},
		},
	}

	svc := &GatewayService{
		accountRepo: repo,
		cfg:         &config.Config{RunMode: config.RunModeSimple},
	}

	acc, err := svc.selectAccountForModelWithPlatform(ctx, nil, "", "claude-sonnet-4-5", nil, PlatformGLM)
	if err != nil {
		t.Fatalf("selectAccountForModelWithPlatform error = %v", err)
	}
	if acc == nil {
		t.Fatalf("expected account")
	}
	if acc.ID != 1 {
		t.Fatalf("account ID = %d, want 1", acc.ID)
	}
	if acc.Platform != PlatformGLM {
		t.Fatalf("platform = %q, want %q", acc.Platform, PlatformGLM)
	}
}
