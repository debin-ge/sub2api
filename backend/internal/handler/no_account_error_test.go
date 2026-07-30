//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type fakeDiagnoser struct {
	calls []fakeDiagnoseCall
	resp  service.ModelAvailabilityDiagnosis
}

type fakeDiagnoseCall struct {
	GroupID  *int64
	Model    string
	Platform string
}

func (f *fakeDiagnoser) DiagnoseModelAvailabilityForPlatform(
	_ context.Context,
	groupID *int64,
	model, platform string,
) service.ModelAvailabilityDiagnosis {
	f.calls = append(f.calls, fakeDiagnoseCall{
		GroupID:  groupID,
		Model:    model,
		Platform: platform,
	})
	return f.resp
}

func ptrInt64(v int64) *int64 { return &v }

// newTestGinContextWithRequest wraps the bare newTestGinContext helper
// (defined in openai_gateway_cyber_test.go) by additionally attaching a stub
// *http.Request so the classifier can extract c.Request.Context().
func newTestGinContextWithRequest() *gin.Context {
	c := newTestGinContext()
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	return c
}

func TestClassifyNoAccountError_NilDiagnoser_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, nil, apiKey, nil, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_NilAPIKey_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}

	cls := classifyNoAccountErrorFromGin(c, fd, nil, nil, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
	require.Empty(t, fd.calls, "diagnoser must not be consulted when apiKey missing")
}

func TestClassifyNoAccountError_NilGroupID_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: nil}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, nil, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
	require.Empty(t, fd.calls, "diagnoser must not be consulted when group not bound")
}

func TestClassifyNoAccountError_EmptyModel_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, nil, "   ", "", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
	require.Empty(t, fd.calls)
}

func TestClassifyNoAccountError_ModelNotSupported_Returns404(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(42)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, nil, "gpt-5.1-codex-mini", "gpt-5.1-codex-mini", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "gpt-5.1-codex-mini", "message must surface the requested model")

	require.Len(t, fd.calls, 1)
	require.Equal(t, "gpt-5.1-codex-mini", fd.calls[0].Model)
	require.Equal(t, service.PlatformOpenAI, fd.calls[0].Platform)
	require.NotNil(t, fd.calls[0].GroupID)
	require.Equal(t, int64(42), *fd.calls[0].GroupID)
}

func TestClassifyOpenAICompatibleNoAccountError_GrokUsesGrokPlatform(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	groupID := int64(43)
	apiKey := &service.APIKey{
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformGrok,
		},
	}

	cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, fd, apiKey, nil, "grok-4.5", "grok-4.5")

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Len(t, fd.calls, 1)
	require.Equal(t, service.PlatformGrok, fd.calls[0].Platform)

	logErr := openAICompatibleSelectionErrorForLog(
		fmt.Errorf("no available OpenAI accounts supporting model: grok-4.5"),
		service.PlatformGrok,
	)
	require.EqualError(t, logErr, "no available Grok accounts supporting model: grok-4.5")
}

func TestClassifyNoAccountError_HasModelSupport_KeepsRoutingMessageGenerationToCaller(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, nil, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status, "model exists somewhere — caller stays on 503")
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_ModelSupportedOnlyByRateLimitedAccount_Returns503(t *testing.T) {
	c := newTestGinContextWithRequest()
	// The diagnoser's configured-state lookup still sees the model-supporting
	// account even though normal scheduling has excluded it during cooldown.
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, nil, "claude-opus-4-8", "claude-opus-4-8", service.PlatformAnthropic)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound, "temporary account cooldown must remain retryable")
}

func TestClassifyNoAccountError_NoAccountsInPool_Stays503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: false, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, nil, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status, "empty pool is a service-availability issue, not a model issue")
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_DisplayModelOverridesRoutingForMessage(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, nil, "gpt-5", "claude-3-fancy", service.PlatformOpenAI)

	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "claude-3-fancy", "user-facing message must reference the model the user asked for, not the post-mapping routing model")
	require.Len(t, fd.calls, 1)
	require.Equal(t, "gpt-5", fd.calls[0].Model, "diagnosis must run against the routing model (post group dispatch mapping)")
}

func TestClassifyNoAccountError_PricingUnavailable_Returns404(t *testing.T) {
	c := newTestGinContextWithRequest()
	// 池子完全健康：有账号、账号也支持该模型。旧口径下这会被判成 503「稍后重试」。
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}
	selErr := fmt.Errorf("%w for billing_kind=token upstream_model=%q", service.ErrModelPricingUnavailable, "kimi-k3")

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, selErr, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status, "定价缺失要改配置才能修，重试无意义")
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound, "调用方据此跳过 markOpsRoutingCapacityLimited*")
	require.True(t, cls.PricingUnavailable)
	require.Contains(t, cls.Message, "gpt-5")
	require.NotContains(t, cls.Message, "kimi-k3", "上游模型链是运维信息，不能进响应体")
	require.Empty(t, fd.calls, "错误已自述原因，不必再查账号池")
}

// 价格门禁与账号池状态无关：池子空、未绑定分组、模型名为空这些会让通用分支直接回落
// 503 的前置条件，都不该把定价缺失也拖回 503。
func TestClassifyNoAccountError_PricingUnavailable_IgnoresPoolPreconditions(t *testing.T) {
	selErr := fmt.Errorf("%w: no price", service.ErrModelPricingUnavailable)
	cases := []struct {
		name   string
		diag   service.ModelAvailabilityDiagnoser
		apiKey *service.APIKey
	}{
		{name: "nil diagnoser", apiKey: &service.APIKey{GroupID: ptrInt64(7)}},
		{name: "nil api key", diag: &fakeDiagnoser{}},
		{name: "no group bound", diag: &fakeDiagnoser{}, apiKey: &service.APIKey{}},
		{name: "empty pool", diag: &fakeDiagnoser{}, apiKey: &service.APIKey{GroupID: ptrInt64(7)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cls := classifyNoAccountErrorFromGin(
				newTestGinContextWithRequest(), tc.diag, tc.apiKey, selErr, "gpt-5", "gpt-5", service.PlatformOpenAI)
			require.Equal(t, http.StatusNotFound, cls.Status)
			require.True(t, cls.PricingUnavailable)
		})
	}
}

func TestClassifyNoAccountError_NonPricingErrorStillDiagnosesPool(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(
		c, fd, apiKey, service.ErrNoAvailableAccounts, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.PricingUnavailable)
	require.Len(t, fd.calls, 1, "非定价原因仍要靠账号池诊断区分 404/503")
}

func TestClassifyOpenAICompatibleNoAccountError_PricingUnavailable_Returns404(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	groupID := int64(43)
	apiKey := &service.APIKey{
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformGrok},
	}
	selErr := fmt.Errorf("%w: no price", service.ErrModelPricingUnavailable)

	cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, fd, apiKey, selErr, "grok-4.5", "grok-4.5")

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.True(t, cls.PricingUnavailable)
	require.Empty(t, fd.calls)
}

func TestClassifyNoAccountError_FromGin_NilContextStillSafe(t *testing.T) {
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(nil, fd, apiKey, nil, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status, "even with a nil gin context the classifier must still run and yield a coherent response")
	require.True(t, cls.ModelNotFound)
}

func TestClassifyGroupAccessSelectionError_PreservesTypedContract(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantReason string
		wantType   string
	}{
		{
			name:       "exclusive",
			err:        infraerrors.Forbidden(string(service.GroupAccessDenyGroupNotAllowed), "exclusive access required"),
			wantStatus: http.StatusForbidden,
			wantReason: string(service.GroupAccessDenyGroupNotAllowed),
			wantType:   "permission_error",
		},
		{
			name:       "vip",
			err:        infraerrors.Forbidden(string(service.GroupAccessDenyVIPOnly), "VIP access required"),
			wantStatus: http.StatusForbidden,
			wantReason: string(service.GroupAccessDenyVIPOnly),
			wantType:   "permission_error",
		},
		{
			name:       "profile missing",
			err:        infraerrors.InternalServer(string(service.GroupAccessDenyProfileMissing), "profile missing"),
			wantStatus: http.StatusInternalServerError,
			wantReason: string(service.GroupAccessDenyProfileMissing),
			wantType:   "api_error",
		},
		{
			name:       "invalid fallback",
			err:        infraerrors.ServiceUnavailable(string(service.GroupAccessDenyFallbackInvalidConfig), "invalid fallback"),
			wantStatus: http.StatusServiceUnavailable,
			wantReason: string(service.GroupAccessDenyFallbackInvalidConfig),
			wantType:   "api_error",
		},
		{
			name:       "inactive group keeps legacy contract",
			err:        infraerrors.BadRequest(string(service.GroupAccessDenyGroupInactive), "group inactive"),
			wantStatus: http.StatusBadRequest,
			wantReason: string(service.GroupAccessDenyGroupInactive),
			wantType:   "invalid_request_error",
		},
		{
			name:       "subscription required keeps legacy contract",
			err:        infraerrors.BadRequest(string(service.GroupAccessDenySubscriptionRequired), "subscription required"),
			wantStatus: http.StatusBadRequest,
			wantReason: string(service.GroupAccessDenySubscriptionRequired),
			wantType:   "invalid_request_error",
		},
		{
			name:       "write-time subscription reason is normalized for runtime",
			err:        infraerrors.BadRequest(string(service.GroupAccessDenyFallbackSubscription), "subscription forbidden"),
			wantStatus: http.StatusServiceUnavailable,
			wantReason: string(service.GroupAccessDenyFallbackInvalidConfig),
			wantType:   "api_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := classifyGroupAccessSelectionError(fmt.Errorf("selection failed: %w", tt.err))
			require.True(t, ok)
			require.Equal(t, tt.wantStatus, got.Status)
			require.Equal(t, tt.wantReason, got.Reason)
			require.Equal(t, tt.wantType, got.OpenAIType)
			require.NotEmpty(t, got.Message)
		})
	}

	_, ok := classifyGroupAccessSelectionError(fmt.Errorf("%w: model", service.ErrNoAvailableAccounts))
	require.False(t, ok, "ordinary capacity errors must continue to the no-account classifier")
}

func TestHandleOpenAICompatibleGroupAccessSelectionErrorWritesStableCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	handled := handleOpenAICompatibleGroupAccessSelectionError(
		c,
		infraerrors.Forbidden(string(service.GroupAccessDenyVIPOnly), "VIP access required"),
		false,
	)

	require.True(t, handled)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.JSONEq(t, `{
		"type": "error",
		"error": {
			"type": "permission_error",
			"code": "GROUP_VIP_ONLY",
			"message": "VIP access required"
		}
	}`, recorder.Body.String())
}

func TestForkGatewaysClassifyEverySelectionFailureExit(t *testing.T) {
	for filename, expected := range map[string]int{
		"windsurf_gateway_handler.go": 3,
		"glm_gateway_handler.go":      3,
		"deepseek_gateway_handler.go": 3,
		"kimi_gateway_handler.go":     3,
		"minimax_gateway_handler.go":  3,
		"opencode_gateway_handler.go": 2,
	} {
		t.Run(filename, func(t *testing.T) {
			source, err := os.ReadFile(filename)
			require.NoError(t, err)
			require.Equal(
				t,
				expected,
				strings.Count(
					string(source),
					"handleOpenAICompatibleGroupAccessSelectionError(c, err,",
				),
			)
		})
	}
}
