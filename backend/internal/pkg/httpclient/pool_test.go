package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidatedTransport_CacheHostValidation(t *testing.T) {
	var validateCalls int32
	validate := func(_ context.Context, host string) error {
		atomic.AddInt32(&validateCalls, 1)
		require.Equal(t, "api.openai.com", host)
		return nil
	}

	var baseCalls int32
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&baseCalls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})

	now := time.Unix(1730000000, 0)
	transport := newValidatedTransport(base)
	transport.now = func() time.Time { return now }
	transport.validateResolvedIP = validate

	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)
	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	require.Equal(t, int32(1), atomic.LoadInt32(&validateCalls))
	require.Equal(t, int32(2), atomic.LoadInt32(&baseCalls))
}

func TestValidatedTransport_ExpiredCacheTriggersRevalidation(t *testing.T) {
	var validateCalls int32
	validate := func(context.Context, string) error {
		atomic.AddInt32(&validateCalls, 1)
		return nil
	}

	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})

	now := time.Unix(1730001000, 0)
	transport := newValidatedTransport(base)
	transport.now = func() time.Time { return now }
	transport.validateResolvedIP = validate

	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	now = now.Add(validatedHostTTL + time.Second)
	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	require.Equal(t, int32(2), atomic.LoadInt32(&validateCalls))
}

func TestValidatedTransport_ValidationErrorStopsRoundTrip(t *testing.T) {
	expectedErr := errors.New("dns rebinding rejected")
	validate := func(context.Context, string) error {
		return expectedErr
	}

	var baseCalls int32
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&baseCalls, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})

	transport := newValidatedTransport(base)
	transport.validateResolvedIP = validate
	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, int32(0), atomic.LoadInt32(&baseCalls))
}

func TestValidatedTransport_RequestCancellationStopsDNSAndBaseRoundTrip(t *testing.T) {
	validationStarted := make(chan struct{})
	validate := func(ctx context.Context, _ string) error {
		close(validationStarted)
		<-ctx.Done()
		return ctx.Err()
	}

	var baseCalls int32
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&baseCalls, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	transport := newValidatedTransport(base)
	transport.validateResolvedIP = validate
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		_, roundTripErr := transport.RoundTrip(req)
		result <- roundTripErr
	}()

	<-validationStarted
	cancel()

	require.ErrorIs(t, <-result, context.Canceled)
	require.Equal(t, int32(0), atomic.LoadInt32(&baseCalls))
}
