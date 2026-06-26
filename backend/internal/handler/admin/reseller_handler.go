package admin

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type resellerBalanceFetcher interface {
	Fetch(ctx context.Context, in service.ResellerBalanceRequest) (*service.ResellerBalanceResult, error)
}

type ResellerHandler struct {
	cfg            *config.Config
	settingService *service.SettingService
	client         resellerBalanceFetcher
}

func NewResellerHandler(cfg *config.Config, settingService *service.SettingService, client *service.ResellerBalanceClient) *ResellerHandler {
	return newResellerHandler(cfg, settingService, client)
}

func newResellerHandler(cfg *config.Config, settingService *service.SettingService, client resellerBalanceFetcher) *ResellerHandler {
	return &ResellerHandler{cfg: cfg, settingService: settingService, client: client}
}

// GetUpstreamBalance returns the parent-site account balance visible to this reseller site.
// GET /api/v1/admin/reseller/upstream-balance
func (h *ResellerHandler) GetUpstreamBalance(c *gin.Context) {
	cfg, err := h.resellerConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if !cfg.Enabled {
		response.Success(c, gin.H{
			"enabled":           false,
			"configured":        false,
			"upstream_endpoint": "",
			"balance":           0,
			"status":            service.ResellerBalanceStatusDisabled,
		})
		return
	}

	endpoint := cfg.UpstreamEndpoint
	apiKey := cfg.UpstreamAPIKey
	if endpoint == "" || apiKey == "" || h.client == nil {
		response.Success(c, gin.H{
			"enabled":           true,
			"configured":        false,
			"upstream_endpoint": endpoint,
			"balance":           0,
			"status":            service.ResellerBalanceStatusNotConfigured,
		})
		return
	}

	result, err := h.client.Fetch(c.Request.Context(), service.ResellerBalanceRequest{
		Endpoint: endpoint,
		APIKey:   apiKey,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to query upstream balance")
		return
	}
	response.Success(c, result)
}

func (h *ResellerHandler) resellerConfig(ctx context.Context) (config.ResellerConfig, error) {
	if h.settingService != nil {
		settings, err := h.settingService.GetAllSettings(ctx)
		if err != nil {
			return config.ResellerConfig{}, err
		}
		return config.ResellerConfig{
			Enabled:          settings.ResellerEnabled,
			UpstreamEndpoint: settings.ResellerUpstreamEndpoint,
			UpstreamAPIKey:   settings.ResellerUpstreamAPIKey,
		}, nil
	}
	if h.cfg == nil {
		return config.ResellerConfig{}, nil
	}
	return h.cfg.Reseller, nil
}
