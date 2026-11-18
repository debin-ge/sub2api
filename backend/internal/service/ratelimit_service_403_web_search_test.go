//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 火山方舟真实返回的联网搜索未开通 403。
const arkWebSearchAccessDenied403Body = `{"type":"error","request_id":"02179490373522561e61b6be4ea80d80d16a59ce3c2929cfabfe6",` +
	`"error":{"code":"AccessDenied","message":"Access denied for web search. Please go to the Volcano Ark Console to verify the activation status. Request id: 02179490373522561e61b6be4ea80d80d16a59ce3c2929cfabfe6","param":"","type":"Forbidden"}}`

const arkWebSearchFreeQuota403Body = `{"error":{"code":"QuotaExceeded.DoubaoSearchFreeQuotaExceeded",` +
	`"message":"Your free web search quota has been exhausted. Please subscribe to continue. Request ID: 0217","param":"","type":"Forbidden"}}`

// 方舟通用资源无权限同样是 AccessDenied，但属于账号级问题，不能被豁免。
const arkGenericAccessDenied403Body = `{"error":{"code":"AccessDenied","message":"The request failed because you do not have access to the requested resource.","param":"","type":"Forbidden"}}`

func TestIsUpstreamWebSearchFeature403(t *testing.T) {
	require.True(t, isUpstreamWebSearchFeature403([]byte(arkWebSearchAccessDenied403Body)))
	require.True(t, isUpstreamWebSearchFeature403([]byte(arkWebSearchFreeQuota403Body)))
	require.False(t, isUpstreamWebSearchFeature403([]byte(arkGenericAccessDenied403Body)))
	require.False(t, isUpstreamWebSearchFeature403([]byte(`{"error":{"code":"Forbidden","message":"Access denied for web search"}}`)))
	require.False(t, isUpstreamWebSearchFeature403(nil))
	require.False(t, isUpstreamWebSearchFeature403([]byte(openAI403HTMLBody)))
}

func TestHandleUpstreamError_WebSearchFeature403DoesNotPenalizeAccount(t *testing.T) {
	for _, body := range []string{arkWebSearchAccessDenied403Body, arkWebSearchFreeQuota403Body} {
		for _, platform := range []string{PlatformDeepSeek, PlatformKimi, PlatformOpenAI, PlatformAnthropic} {
			t.Run(platform, func(t *testing.T) {
				repo := &rateLimitAccountRepoStub{}
				counter := &countingOpenAI403CounterCache{
					openAI403CounterCacheStub: openAI403CounterCacheStub{counts: []int64{openAI403DisableThreshold}},
				}
				blocker := &runtimeBlockRecorder{}
				svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
				svc.SetOpenAI403CounterCache(counter)
				svc.SetAccountRuntimeBlocker(blocker)
				account := &Account{ID: 801, Platform: platform, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "sk-test"}}

				for i := 0; i < openAI403DisableThreshold+2; i++ {
					shouldDisable := svc.HandleUpstreamError(
						context.Background(), account, http.StatusForbidden, http.Header{}, []byte(body),
					)
					require.False(t, shouldDisable, "联网搜索功能级 403 不得判定账号应下线")
				}
				require.Equal(t, 0, counter.increments, "联网搜索功能级 403 不得递增账号 403 计数")
				require.Equal(t, 0, repo.setErrorCalls, "联网搜索功能级 403 不得永久禁用账号")
				require.Equal(t, 0, repo.tempCalls, "联网搜索功能级 403 不得临时停调账号")
				require.Empty(t, blocker.accounts, "联网搜索功能级 403 不得触发调度阻断")
			})
		}
	}
}

func TestHandleUpstreamError_ArkGenericAccessDeniedStillCounts(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &countingOpenAI403CounterCache{openAI403CounterCacheStub: openAI403CounterCacheStub{counts: []int64{1}}}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc.SetOpenAI403CounterCache(counter)
	svc.SetAccountRuntimeBlocker(&runtimeBlockRecorder{})
	account := &Account{ID: 802, Platform: PlatformDeepSeek, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"}}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(), account, http.StatusForbidden, http.Header{}, []byte(arkGenericAccessDenied403Body),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, counter.increments, "通用 AccessDenied 仍按账号级 403 计数")
	require.Equal(t, 1, repo.tempCalls)
}

// 只免处罚、不免换号：功能级 403 仍须触发 failover，让开通了搜索组件的账号接手。
func TestShouldFailoverOpenAIUpstreamResponse_WebSearchFeature403StillFailsOver(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 803, Platform: PlatformDeepSeek, Type: AccountTypeAPIKey}
	require.True(t, svc.shouldFailoverOpenAIUpstreamResponse(
		account, http.StatusForbidden, "Access denied for web search", []byte(arkWebSearchAccessDenied403Body),
	))
}
