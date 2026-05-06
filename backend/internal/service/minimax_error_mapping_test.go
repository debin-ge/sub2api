package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestMiniMaxUpstreamStatusErrorMapping(t *testing.T) {
	cases := []struct {
		status       int
		clientStatus int
		errorType    string
		retryable    bool
	}{
		{status: http.StatusUnauthorized, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusForbidden, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusTooManyRequests, clientStatus: http.StatusTooManyRequests, errorType: "rate_limit_error", retryable: true},
		{status: http.StatusInternalServerError, clientStatus: http.StatusBadGateway, errorType: "server_error", retryable: true},
		{status: 529, clientStatus: http.StatusBadGateway, errorType: "overloaded_error", retryable: true},
		{status: http.StatusBadRequest, clientStatus: http.StatusBadRequest, errorType: "invalid_request_error", retryable: false},
	}

	for _, tc := range cases {
		got := MapMiniMaxUpstreamStatus(tc.status)
		if got.ClientStatus != tc.clientStatus || got.ErrorType != tc.errorType || got.Retryable != tc.retryable {
			t.Fatalf("MapMiniMaxUpstreamStatus(%d) = %+v", tc.status, got)
		}
	}
}

func TestMiniMaxForwardMessagesRetryableUpstreamErrorReturnsFailoverBeforeWriting(t *testing.T) {
	tests := []int{http.StatusTooManyRequests, http.StatusInternalServerError, 529}

	for _, status := range tests {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header: http.Header{
						"Content-Type": {"application/json"},
						"X-Request-Id": {"upstream-error"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"try later"}}`)),
				}, nil
			})}
			cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
			svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
			c, rec := newMiniMaxGatewayTestContext()

			_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(false), "req-retryable")
			if err == nil {
				t.Fatalf("expected failover error")
			}
			var failoverErr *UpstreamFailoverError
			if !errors.As(err, &failoverErr) {
				t.Fatalf("error type = %T %v", err, err)
			}
			if failoverErr.StatusCode != status {
				t.Fatalf("failover status = %d", failoverErr.StatusCode)
			}
			if got := strings.TrimSpace(string(failoverErr.ResponseBody)); !strings.Contains(got, "try later") {
				t.Fatalf("failover body = %q", got)
			}
			if got := failoverErr.ResponseHeaders.Get("X-Request-Id"); got != "upstream-error" {
				t.Fatalf("failover headers x-request-id = %q", got)
			}
			if rec.Body.String() != "" {
				t.Fatalf("response should not be written before failover, got %q", rec.Body.String())
			}
			if cache.rollbackCalls != 1 || cache.rollbackRequestID != "req-retryable" {
				t.Fatalf("rollback call = calls %d requestID %q", cache.rollbackCalls, cache.rollbackRequestID)
			}
		})
	}
}

func TestMiniMaxForwardMessagesAuthUpstreamErrorReturnsMappedErrorBeforeWriting(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad api key"}}`)),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, rec := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(false), "req-auth")
	if err == nil {
		t.Fatalf("expected upstream auth error")
	}
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("error type = %T %v", err, err)
	}
	if failoverErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("failover status = %d", failoverErr.StatusCode)
	}
	if rec.Body.String() != "" {
		t.Fatalf("response should not be written before handler mapping, got %q", rec.Body.String())
	}
	if cache.rollbackCalls != 1 || cache.rollbackRequestID != "req-auth" {
		t.Fatalf("rollback call = calls %d requestID %q", cache.rollbackCalls, cache.rollbackRequestID)
	}
}

type miniMaxDegradeAccountRepo struct {
	sessionWindowMockRepo

	rateLimitedID int64
	resetAt       time.Time

	overloadedID  int64
	overloadUntil time.Time

	errorID  int64
	errorMsg string
}

func (r *miniMaxDegradeAccountRepo) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	r.rateLimitedID = id
	r.resetAt = resetAt
	return nil
}

func (r *miniMaxDegradeAccountRepo) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	r.overloadedID = id
	r.overloadUntil = until
	return nil
}

func (r *miniMaxDegradeAccountRepo) SetError(ctx context.Context, id int64, errorMsg string) error {
	r.errorID = id
	r.errorMsg = errorMsg
	return nil
}

func TestMiniMaxGatewayServiceHandleMiniMaxUpstreamErrorPersistsDegradation(t *testing.T) {
	repo := &miniMaxDegradeAccountRepo{}
	cfg := &config.Config{RateLimit: config.RateLimitConfig{OverloadCooldownMinutes: 7}}
	gateway := &GatewayService{
		accountRepo:      repo,
		rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil),
		cfg:              cfg,
	}
	account := &Account{ID: 101, Platform: PlatformMiniMax, Type: AccountTypeAPIKey}

	beforeRateLimit := time.Now()
	gateway.HandleMiniMaxUpstreamError(context.Background(), account, &UpstreamFailoverError{
		StatusCode:   http.StatusTooManyRequests,
		ResponseBody: []byte(`{"error":{"message":"rate limited"}}`),
	})
	if repo.rateLimitedID != account.ID {
		t.Fatalf("rate limited account id = %d", repo.rateLimitedID)
	}
	if !repo.resetAt.After(beforeRateLimit) {
		t.Fatalf("rate limit reset should be in the future, got %s", repo.resetAt)
	}

	beforeOverload := time.Now()
	gateway.HandleMiniMaxUpstreamError(context.Background(), account, &UpstreamFailoverError{StatusCode: http.StatusInternalServerError})
	if repo.overloadedID != account.ID {
		t.Fatalf("overloaded account id = %d", repo.overloadedID)
	}
	if !repo.overloadUntil.After(beforeOverload.Add(6 * time.Minute)) {
		t.Fatalf("overload until should use configured cooldown, got %s", repo.overloadUntil)
	}
}
