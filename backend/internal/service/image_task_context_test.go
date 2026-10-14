package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestImageTaskDeadlineSurvivesUpstreamDetachment(t *testing.T) {
	deadline := time.Now().Add(-time.Second)
	ctx := WithImageTaskExecutionDeadline(context.Background(), deadline)
	detached, cancel := detachUpstreamContext(ctx)
	defer cancel()
	got, ok := detached.Deadline()
	require.True(t, ok)
	require.Equal(t, deadline, got)
	require.ErrorIs(t, detached.Err(), context.DeadlineExceeded)

	// Ordinary synchronous calls retain their existing cancellation policy;
	// this change must not turn a disconnected client into unpaid upstream work.
	ordinary, ordinaryCancel := context.WithDeadline(context.Background(), deadline)
	defer ordinaryCancel()
	ordinaryDetached, release := detachUpstreamContext(ordinary)
	defer release()
	_, ok = ordinaryDetached.Deadline()
	require.False(t, ok)
	require.NoError(t, ordinaryDetached.Err())
}

type imageTaskDeadlineUpstream struct {
	inspect func(*http.Request)
}

func (s imageTaskDeadlineUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.inspect(req)
	return nil, errors.New("mock transport stops after inspecting request context")
}

func (s imageTaskDeadlineUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxy, accountID, concurrency)
}

func TestImageTaskDeadlineReachesActualImageGatewayRequests(t *testing.T) {
	for _, route := range []string{"openai_apikey", "openai_oauth_direct", "openai_oauth_responses", "grok"} {
		t.Run(route, func(t *testing.T) {
			deadline := time.Now().Add(time.Minute)
			// Cancellation is separate from the absolute lifetime. The HTTP
			// transport must see a live request bounded by the original deadline.
			parent, cancel := context.WithCancel(context.Background())
			cancel()
			ctx := WithImageTaskExecutionDeadline(parent, deadline)
			called := false
			upstream := imageTaskDeadlineUpstream{inspect: func(req *http.Request) {
				called = true
				got, ok := req.Context().Deadline()
				require.True(t, ok, "the real forwarding path must not erase task lifetime")
				require.Equal(t, deadline, got)
				require.NoError(t, req.Context().Err(), "client cancellation stays detached")
			}}
			svc := newOpenAIImagesTestService(upstream)
			body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			account := newOpenAIImagesAPIKeyAccount()
			if route == "grok" {
				body = []byte(`{"model":"grok-imagine-image","prompt":"draw a cat"}`)
				account.Platform = PlatformGrok
				_, err := svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointImagesGenerations, "", body, "application/json")
				require.Error(t, err)
			} else {
				if route != "openai_apikey" {
					account.Type = AccountTypeOAuth
					account.Credentials = map[string]any{"access_token": "mock-token"}
				}
				if route == "openai_oauth_responses" {
					ctx = withOpenAIImagesForceResponses(ctx)
				}
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				_, err = svc.ForwardImages(ctx, c, account, body, parsed, "")
				require.Error(t, err)
			}
			require.True(t, called, "exercise actual gateway request construction with only the network replaced")
		})
	}
}
