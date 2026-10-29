//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGrokVideoCreationPersistsBeforeWritingResponse(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "persistence failed"}[fail], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := []byte(`{"model":"grok-imagine-video","prompt":"test"}`)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(string(body)))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"request_id":"task"}`))}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			account := &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}}
			persisted := false
			_, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json", func(status int, raw []byte) error {
				require.False(t, c.Writer.Written())
				require.Equal(t, 200, status)
				require.JSONEq(t, `{"request_id":"task"}`, string(raw))
				if fail {
					return errors.New("database unavailable")
				}
				persisted = true
				return nil
			})
			if fail {
				require.Error(t, err)
				require.False(t, c.Writer.Written())
				require.Empty(t, recorder.Body.String())
			} else {
				require.NoError(t, err)
				require.True(t, persisted)
				require.JSONEq(t, `{"request_id":"task"}`, recorder.Body.String())
			}
		})
	}
}
