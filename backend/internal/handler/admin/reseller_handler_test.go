package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type resellerBalanceClientStub struct {
	request service.ResellerBalanceRequest
	result  *service.ResellerBalanceResult
	err     error
}

func (s *resellerBalanceClientStub) Fetch(ctx context.Context, in service.ResellerBalanceRequest) (*service.ResellerBalanceResult, error) {
	s.request = in
	return s.result, s.err
}

func TestResellerHandlerUpstreamBalanceDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := newResellerHandler(&config.Config{}, &resellerBalanceClientStub{})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reseller/upstream-balance", nil)

	h.GetUpstreamBalance(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"status":"disabled"`)
	require.Contains(t, w.Body.String(), `"enabled":false`)
	require.NotContains(t, w.Body.String(), "sk-parent")
}

func TestResellerHandlerUpstreamBalanceConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	client := &resellerBalanceClientStub{result: &service.ResellerBalanceResult{
		Enabled:          true,
		Configured:       true,
		UpstreamEndpoint: "https://parent.example.com",
		Status:           service.ResellerBalanceStatusOK,
		Balance:          12.34,
		UserID:           42,
	}}
	h := newResellerHandler(&config.Config{
		Reseller: config.ResellerConfig{
			Enabled:          true,
			UpstreamEndpoint: "https://parent.example.com",
			UpstreamAPIKey:   "sk-parent",
		},
	}, client)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reseller/upstream-balance", nil)

	h.GetUpstreamBalance(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "https://parent.example.com", client.request.Endpoint)
	require.Equal(t, "sk-parent", client.request.APIKey)
	require.Contains(t, w.Body.String(), `"status":"ok"`)
	require.Contains(t, w.Body.String(), `"balance":12.34`)
	require.Contains(t, w.Body.String(), `"upstream_endpoint":"https://parent.example.com"`)
	require.NotContains(t, w.Body.String(), "sk-parent")
}

func TestResellerHandlerUpstreamBalanceClientError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := newResellerHandler(&config.Config{
		Reseller: config.ResellerConfig{
			Enabled:          true,
			UpstreamEndpoint: "https://parent.example.com",
			UpstreamAPIKey:   "sk-parent",
		},
	}, &resellerBalanceClientStub{err: errors.New("boom")})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reseller/upstream-balance", nil)

	h.GetUpstreamBalance(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), "sk-parent")
}
