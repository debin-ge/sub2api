package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestCompatibleGatewayDefaultClientsUseSixtySecondHeaderTimeout(t *testing.T) {
	tests := []struct {
		name   string
		client *http.Client
	}{
		{name: "minimax", client: NewMiniMaxGatewayService(nil, nil, nil).httpClient},
		{name: "glm", client: NewGLMGatewayService(nil, nil).httpClient},
		{name: "kimi", client: NewKimiGatewayService(nil, nil).httpClient},
		{name: "deepseek", client: NewDeepSeekGatewayService(nil, nil).httpClient},
		{name: "windsurf", client: NewWindsurfGatewayService(nil, nil).httpClient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.client == nil {
				t.Fatalf("expected default http client")
			}
			if tt.client.Timeout != 0 {
				t.Fatalf("default http client Timeout = %v, want 0 so streaming body reads are not capped", tt.client.Timeout)
			}
			transport, ok := tt.client.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport type = %T, want *http.Transport", tt.client.Transport)
			}
			if transport.ResponseHeaderTimeout != 60*time.Second {
				t.Fatalf("ResponseHeaderTimeout = %v, want 60s", transport.ResponseHeaderTimeout)
			}
		})
	}
}

func TestCompatibleGatewayProvidersUseConfiguredTimeout(t *testing.T) {
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			CompatibleUpstreamTimeoutSeconds: 75,
		},
	}

	tests := []struct {
		name   string
		client *http.Client
	}{
		{name: "minimax", client: ProvideMiniMaxGatewayService(nil, cfg).httpClient},
		{name: "glm", client: ProvideGLMGatewayService(cfg).httpClient},
		{name: "kimi", client: ProvideKimiGatewayService(cfg).httpClient},
		{name: "deepseek", client: ProvideDeepSeekGatewayService(cfg).httpClient},
		{name: "windsurf", client: ProvideWindsurfGatewayService(cfg).httpClient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, ok := tt.client.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport type = %T, want *http.Transport", tt.client.Transport)
			}
			if transport.ResponseHeaderTimeout != 75*time.Second {
				t.Fatalf("ResponseHeaderTimeout = %v, want 75s", transport.ResponseHeaderTimeout)
			}
		})
	}
}
