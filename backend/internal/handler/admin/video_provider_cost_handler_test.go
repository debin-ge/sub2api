package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoProviderCostHandlerStub struct {
	videoAdminHandlerStub
	resolutions []service.VideoProviderCostResolution
	listCalls   int
}

func (s *videoProviderCostHandlerStub) ResolveProviderCost(_ context.Context, _ int64, in service.VideoProviderCostResolution) (*service.VideoProviderCost, error) {
	s.resolutions = append(s.resolutions, in)
	return &service.VideoProviderCost{ID: 7, State: in.State, Amount: in.Amount, Currency: in.Currency}, nil
}
func (s *videoProviderCostHandlerStub) ListProviderCosts(context.Context, string, int64, int) (*service.VideoProviderCostPage, error) {
	s.listCalls++
	return &service.VideoProviderCostPage{Items: []*service.VideoProviderCost{{ID: 7, State: "unresolved", ProviderTaskID: "private-provider-handle"}}}, nil
}

func TestVideoProviderCostAdminResolutionValidatesBeforeController(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := `{"expected_version":3,"state":"resolved_cost_recorded","amount":1.25,"currency":"USD","evidence":"Supplier invoice INV-test line 9 confirms this cost."}`
	for _, tc := range []struct {
		name, body, key string
		actor           int64
		want            int
	}{
		{"valid", valid, "resolve-7", 99, http.StatusOK},
		{"missing actor", valid, "resolve-7", 0, http.StatusBadRequest},
		{"missing idempotency", valid, "", 99, http.StatusBadRequest},
		{"missing version", strings.Replace(valid, `"expected_version":3,`, "", 1), "resolve-7", 99, http.StatusBadRequest},
		{"negative amount", strings.Replace(valid, `1.25`, `-1`, 1), "resolve-7", 99, http.StatusBadRequest},
		{"missing amount", strings.Replace(valid, `"amount":1.25,`, "", 1), "resolve-7", 99, http.StatusBadRequest},
		{"unknown currency", strings.Replace(valid, `"USD"`, `"UNKNOWN"`, 1), "resolve-7", 99, http.StatusBadRequest},
		{"customer billing forbidden", strings.Replace(valid, `"resolved_cost_recorded"`, `"capture"`, 1), "resolve-7", 99, http.StatusBadRequest},
		{"duplicate amount", strings.Replace(valid, `"amount":1.25`, `"amount":1.25,"amount":0`, 1), "resolve-7", 99, http.StatusBadRequest},
		{"spoofed actor", strings.Replace(valid, `{"expected_version"`, `{"actor_id":1,"expected_version"`, 1), "resolve-7", 99, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &videoProviderCostHandlerStub{}
			h := newVideoHandler(stub)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/provider-costs/7/resolve", strings.NewReader(tc.body))
			c.Request.Header.Set("Idempotency-Key", tc.key)
			c.Params = gin.Params{{Key: "id", Value: "7"}}
			if tc.actor > 0 {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: tc.actor})
			}
			h.ResolveProviderCost(c)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			if tc.want == http.StatusOK {
				require.Len(t, stub.resolutions, 1)
				require.Equal(t, tc.actor, stub.resolutions[0].ActorID)
				require.Equal(t, tc.key, stub.resolutions[0].IdempotencyKey)
			} else {
				require.Empty(t, stub.resolutions)
			}
		})
	}
}

func TestVideoProviderCostAdminListBoundsAndRedaction(t *testing.T) {
	for _, query := range []string{"?limit=101", "?after=-1", "?limit=bad", "?state=unknown", ""} {
		stub := &videoProviderCostHandlerStub{}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/provider-costs"+query, nil)
		newVideoHandler(stub).ListProviderCosts(c)
		if query == "" {
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, 1, stub.listCalls)
			require.NotContains(t, w.Body.String(), "private-provider-handle")
		} else {
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Zero(t, stub.listCalls)
		}
	}
}
