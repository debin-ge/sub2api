package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestServiceWindsurfUsesOpenAICompatibleBearerProbe(t *testing.T) {
	c, rec := newCodingPlanTestContext()
	upstream := &codingPlanHTTPUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)),
		},
	}
	account := &Account{
		ID:          22,
		Platform:    PlatformWindsurf,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-windsurf"},
	}
	svc := &AccountTestService{
		accountRepo:  &codingPlanAccountTestRepo{account: account},
		httpUpstream: upstream,
	}

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.NoError(t, err)
	require.NotNil(t, upstream.req)
	require.Equal(t, "https://tik.frontech.dev:3003/v1/chat/completions", upstream.req.URL.String())
	require.Equal(t, "Bearer sk-windsurf", upstream.req.Header.Get("Authorization"))
	require.Empty(t, upstream.req.Header.Get("x-api-key"))
	require.Equal(t, "claude-sonnet-4.6", gjson.GetBytes(upstream.body, "model").String())
	require.False(t, gjson.GetBytes(upstream.body, "stream").Bool())
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
}
