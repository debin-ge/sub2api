package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
)

const validAAModelsPayload = `{"data":[{"slug":"claude-sonnet-4","name":"Claude Sonnet 4","creator":"Anthropic","released_at":"2026-05-22","intelligence_index":94.2,"coding_index":89.5,"agentic_index":91.8,"price_input_per_1m":3,"price_output_per_1m":15,"last_updated_at":"2026-07-10T06:00:00Z"}]}`

type radarDoerFunc func(*http.Request) (*http.Response, error)

func (f radarDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type radarRoundTripFunc func(*http.Request) (*http.Response, error)

func (f radarRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type radarTrackingBody struct {
	reader io.Reader

	mu     sync.Mutex
	closed bool
}

type radarFailingBody struct {
	secret string
	closed bool
}

func (b *radarFailingBody) Read([]byte) (int, error) {
	return 0, errors.New(b.secret)
}

func (b *radarFailingBody) Close() error {
	b.closed = true
	return nil
}

func newRadarTrackingBody(value string) *radarTrackingBody {
	return &radarTrackingBody{reader: strings.NewReader(value)}
}

func (b *radarTrackingBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *radarTrackingBody) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	return nil
}

func (b *radarTrackingBody) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func validRadarFetcherTestConfig() *config.Config {
	return &config.Config{Radar: config.RadarConfig{
		Enabled:                                            true,
		QuotaAggregatorIntervalMin:                         15,
		QuotaHistoryRetentionDays:                          7,
		SampleSizeWarnBelow:                                3,
		PublicMinBucketAccounts:                            2,
		InferMinUtilization:                                5,
		InferMaxStdevRatio:                                 0.3,
		ArtificialAnalysisAPIKey:                           "test-aa-api-key",
		ArtificialAnalysisModelSlugs:                       []string{"claude-sonnet-4"},
		ExternalRequestTimeoutSeconds:                      10,
		ExternalResponseMaxBytes:                           1024 * 1024,
		ArtificialAnalysisModelsIntervalMinutes:            360,
		ArtificialAnalysisPerformanceIntervalMinutes:       1440,
		LMArenaIntervalMinutes:                             1440,
		StatuspageIntervalMinutes:                          30,
		SourceHardRetentionDays:                            7,
		QuotaStaleThresholdMinutes:                         30,
		HealthStaleThresholdMinutes:                        60,
		ArtificialAnalysisModelsStaleThresholdMinutes:      720,
		ArtificialAnalysisPerformanceStaleThresholdMinutes: 2880,
		LMArenaStaleThresholdMinutes:                       2880,
		LMArenaURL:                                         "https://datasets-server.huggingface.co/filter",
	}}
}

func radarHTTPClientTestOptions(cfg *config.Config, validateResolvedIP bool) httpclient.Options {
	timeout := time.Duration(cfg.Radar.ExternalRequestTimeoutSeconds) * time.Second
	return httpclient.Options{
		ProxyURL:              cfg.Update.ProxyURL,
		Timeout:               timeout,
		ResponseHeaderTimeout: timeout,
		ValidateResolvedIP:    validateResolvedIP,
	}
}

func requireRadarFetchErrorCode(t *testing.T, meta SourceFetchMeta, want DataSourceErrorCode) {
	t.Helper()
	require.NotNil(t, meta.Error)
	require.Equal(t, want, *meta.Error)
	require.False(t, meta.LastAttemptAt.IsZero())
	require.Equal(t, time.UTC, meta.LastAttemptAt.Location())
	require.Nil(t, meta.LastSuccessAt)
}

func setRadarFetcherSleep(t *testing.T, fetcher RadarFetcher, sleep RadarSleepFunc) {
	t.Helper()
	implementation, ok := fetcher.(*radarHTTPFetcher)
	require.True(t, ok)
	implementation.sleep = sleep
}

func TestRadarHTTPFetcherDoesNotRetryClientFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantCode   DataSourceErrorCode
		secretBody string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantCode: DataSourceErrorCodeUnauthorized, secretBody: "sensitive-auth-body-token"},
		{name: "forbidden", status: http.StatusForbidden, wantCode: DataSourceErrorCodeUnauthorized, secretBody: "sensitive-forbidden-body-token"},
		{name: "rate limited", status: http.StatusTooManyRequests, wantCode: DataSourceErrorCodeRateLimited, secretBody: "sensitive-rate-limit-body-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			body := newRadarTrackingBody(tt.secretBody)
			client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{
					StatusCode:    tt.status,
					Body:          body,
					ContentLength: int64(len(tt.secretBody)),
				}, nil
			})
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.ArtificialAnalysisAPIKey = "api-key-must-never-leak"
			fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
			require.NoError(t, err)

			payload, meta, err := fetcher.Fetch(context.Background())

			require.Error(t, err)
			require.Nil(t, payload)
			require.Equal(t, 1, attempts)
			require.True(t, body.isClosed())
			require.NotNil(t, meta.HTTPStatus)
			require.Equal(t, tt.status, *meta.HTTPStatus)
			requireRadarFetchErrorCode(t, meta, tt.wantCode)
			require.NotContains(t, err.Error(), tt.secretBody)
			require.NotContains(t, err.Error(), cfg.Radar.ArtificialAnalysisAPIKey)
		})
	}
}

func TestRadarHTTPFetcherRetriesServerFailuresThenSucceeds(t *testing.T) {
	var (
		attempts int
		bodies   []*radarTrackingBody
	)
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		status := http.StatusInternalServerError
		value := "upstream failure"
		if attempts == 3 {
			status = http.StatusOK
			value = validAAModelsPayload
		}
		body := newRadarTrackingBody(value)
		bodies = append(bodies, body)
		return &http.Response{StatusCode: status, Body: body, ContentLength: int64(len(value))}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)
	var backoffs []time.Duration
	setRadarFetcherSleep(t, fetcher, func(_ context.Context, duration time.Duration) error {
		backoffs = append(backoffs, duration)
		return nil
	})

	payload, meta, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	require.JSONEq(t, validAAModelsPayload, string(payload))
	require.Equal(t, 3, attempts)
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second}, backoffs)
	require.Len(t, bodies, 3)
	for _, body := range bodies {
		require.True(t, body.isClosed())
	}
	require.Nil(t, meta.Error)
	require.NotNil(t, meta.LastSuccessAt)
	require.Equal(t, meta.LastAttemptAt, *meta.LastSuccessAt)
	require.NotNil(t, meta.HTTPStatus)
	require.Equal(t, http.StatusOK, *meta.HTTPStatus)
}

func TestRadarHTTPFetcherExhaustsServerFailuresWithSafeUpstreamCode(t *testing.T) {
	var bodies []*radarTrackingBody
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		body := newRadarTrackingBody("secret upstream diagnostic")
		bodies = append(bodies, body)
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)
	var backoffs []time.Duration
	setRadarFetcherSleep(t, fetcher, func(_ context.Context, duration time.Duration) error {
		backoffs = append(backoffs, duration)
		return nil
	})

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Len(t, bodies, 3)
	for _, body := range bodies {
		require.True(t, body.isClosed())
	}
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second}, backoffs)
	require.NotNil(t, meta.HTTPStatus)
	require.Equal(t, http.StatusServiceUnavailable, *meta.HTTPStatus)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeUpstreamError)
	require.NotContains(t, err.Error(), "secret upstream diagnostic")
}

func TestRadarHTTPFetcherDoesNotRetryOtherHTTP4xx(t *testing.T) {
	attempts := 0
	body := newRadarTrackingBody("not found")
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusNotFound, Body: body}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Equal(t, 1, attempts)
	require.True(t, body.isClosed())
	require.NotNil(t, meta.HTTPStatus)
	require.Equal(t, http.StatusNotFound, *meta.HTTPStatus)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeUpstreamError)
}

func TestRadarHTTPFetcherRetriesNetworkErrorsThreeTimesAndSanitizesError(t *testing.T) {
	attempts := 0
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, fmt.Errorf("dial failed with secret api-key-must-never-leak and token=query-secret")
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ArtificialAnalysisAPIKey = "api-key-must-never-leak"
	fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
	require.NoError(t, err)
	var backoffs []time.Duration
	setRadarFetcherSleep(t, fetcher, func(_ context.Context, duration time.Duration) error {
		backoffs = append(backoffs, duration)
		return nil
	})

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Equal(t, 3, attempts)
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second}, backoffs)
	require.Nil(t, meta.HTTPStatus)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeNetworkError)
	require.NotContains(t, err.Error(), cfg.Radar.ArtificialAnalysisAPIKey)
	require.NotContains(t, err.Error(), "query-secret")
}

func TestRadarHTTPFetcherRetriesResponseReadNetworkErrorsAndClosesBodies(t *testing.T) {
	const secret = "secret-read-failure-detail"
	var bodies []*radarFailingBody
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		body := &radarFailingBody{secret: secret}
		bodies = append(bodies, body)
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)
	var backoffs []time.Duration
	setRadarFetcherSleep(t, fetcher, func(_ context.Context, duration time.Duration) error {
		backoffs = append(backoffs, duration)
		return nil
	})

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Len(t, bodies, 3)
	for _, body := range bodies {
		require.True(t, body.closed)
	}
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second}, backoffs)
	require.Nil(t, meta.HTTPStatus)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeNetworkError)
	require.NotContains(t, err.Error(), secret)
}

func TestRadarHTTPFetcherContextCancellationStopsRetryBackoff(t *testing.T) {
	attempts := 0
	body := newRadarTrackingBody("temporary failure")
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusBadGateway, Body: body}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	setRadarFetcherSleep(t, fetcher, func(ctx context.Context, _ time.Duration) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})

	payload, meta, err := fetcher.Fetch(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, payload)
	require.Equal(t, 1, attempts)
	require.True(t, body.isClosed())
	require.Nil(t, meta.HTTPStatus)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeNetworkError)
}

func TestRadarHTTPFetcherContextCancellationDuringRequest(t *testing.T) {
	attempts := 0
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	payload, meta, err := fetcher.Fetch(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, payload)
	require.Equal(t, 1, attempts)
	require.Nil(t, meta.HTTPStatus)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeNetworkError)
}

func TestRadarHTTPFetcherRejectsOversizeBodiesRegardlessOfContentLength(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
	}{
		{name: "unknown content length", contentLength: -1},
		{name: "misleading content length", contentLength: 1},
		{name: "declared content length", contentLength: 4096},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := newRadarTrackingBody(strings.Repeat("x", 65))
			client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: tt.contentLength}, nil
			})
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.ExternalResponseMaxBytes = 64
			fetcher, err := NewArtificialAnalysisModelsFetcher(cfg, client)
			require.NoError(t, err)

			payload, meta, err := fetcher.Fetch(context.Background())

			require.Error(t, err)
			require.Nil(t, payload)
			require.True(t, body.isClosed())
			require.NotNil(t, meta.HTTPStatus)
			require.Equal(t, http.StatusOK, *meta.HTTPStatus)
			requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeInvalidResponse)
		})
	}
}

func TestRadarHTTPFetcherDoesNotRetryInvalidJSON(t *testing.T) {
	attempts := 0
	body := newRadarTrackingBody(`{"data":`)
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	fetcher, err := NewArtificialAnalysisModelsFetcher(validRadarFetcherTestConfig(), client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Equal(t, 1, attempts)
	require.True(t, body.isClosed())
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeInvalidResponse)
}

func TestRadarSleepWithContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, radarSleepWithContext(ctx, time.Hour), context.Canceled)
}

func TestNewRadarHTTPClientRejectsBadProxyWithoutExposingIt(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Update.ProxyURL = "http://proxy-user:proxy-secret@%"

	client, err := NewRadarHTTPClient(cfg)

	require.Error(t, err)
	require.Nil(t, client)
	var configErr *RadarFetcherConfigError
	require.True(t, errors.As(err, &configErr))
	require.NotContains(t, err.Error(), "proxy-secret")
}

func TestNewRadarHTTPClientUsesConfiguredTimeout(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ExternalRequestTimeoutSeconds = 17

	client, err := NewRadarHTTPClient(cfg)

	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, 17*time.Second, client.Timeout)
}

func TestNewRadarHTTPClientRejectsRedirectsWithoutForwardingAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		location string
	}{
		{name: "cross origin", location: "https://attacker.example/collect"},
		{name: "HTTPS downgrade", location: "http://origin.example/insecure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.ExternalRequestTimeoutSeconds = 23
			radarClient, err := NewRadarHTTPClient(cfg)
			require.NoError(t, err)
			require.NotNil(t, radarClient.CheckRedirect)

			const apiKey = "redirect-secret-api-key"
			calls := 0
			initialKey := ""
			forwardedKey := ""
			clientUnderTest := *radarClient
			clientUnderTest.Transport = radarRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					forwardedKey = req.Header.Get("x-api-key")
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{}`)),
						Request:    req,
					}, nil
				}
				initialKey = req.Header.Get("x-api-key")
				header := make(http.Header)
				header.Set("Location", tt.location)
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     header,
					Body:       io.NopCloser(strings.NewReader("redirect")),
					Request:    req,
				}, nil
			})
			req, err := http.NewRequest(http.MethodGet, "https://origin.example/start", nil)
			require.NoError(t, err)
			req.Header.Set("x-api-key", apiKey)

			resp, err := clientUnderTest.Do(req)

			require.NoError(t, err)
			require.Equal(t, http.StatusFound, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, 1, calls)
			require.Equal(t, apiKey, initialKey)
			require.Empty(t, forwardedKey)
		})
	}
}

func TestNewRadarHTTPClientDoesNotMutateSharedPooledClient(t *testing.T) {
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.ExternalRequestTimeoutSeconds = 31
	options := radarHTTPClientTestOptions(cfg, true)
	pooledClient, err := httpclient.GetClient(options)
	require.NoError(t, err)
	require.Nil(t, pooledClient.CheckRedirect)
	pooledTransport := pooledClient.Transport

	radarClient, err := NewRadarHTTPClient(cfg)

	require.NoError(t, err)
	require.NotSame(t, pooledClient, radarClient)
	require.Same(t, pooledTransport, radarClient.Transport)
	require.NotNil(t, radarClient.CheckRedirect)
	require.Nil(t, pooledClient.CheckRedirect)
	radarClient.Timeout = time.Nanosecond
	require.Equal(t, 31*time.Second, pooledClient.Timeout)
	pooledAgain, err := httpclient.GetClient(options)
	require.NoError(t, err)
	require.Same(t, pooledClient, pooledAgain)
	require.Nil(t, pooledAgain.CheckRedirect)
	require.Same(t, pooledTransport, pooledAgain.Transport)
}

func TestNewRadarHTTPClientEnablesResolvedIPValidationOnlyForDirectConnections(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		cfg := validRadarFetcherTestConfig()
		cfg.Radar.ExternalRequestTimeoutSeconds = 37
		radarClient, err := NewRadarHTTPClient(cfg)
		require.NoError(t, err)

		pooledClient, err := httpclient.GetClient(radarHTTPClientTestOptions(cfg, true))
		require.NoError(t, err)
		require.Same(t, pooledClient.Transport, radarClient.Transport)
		unvalidatedClient, err := httpclient.GetClient(radarHTTPClientTestOptions(cfg, false))
		require.NoError(t, err)
		require.NotSame(t, unvalidatedClient.Transport, radarClient.Transport,
			"direct Radar traffic must retain resolved-IP validation")
	})

	proxies := []struct {
		name     string
		proxyURL string
		timeout  int
	}{
		{name: "HTTP proxy", proxyURL: "http://127.0.0.1:1", timeout: 41},
		{name: "SOCKS5H proxy", proxyURL: "socks5h://proxy-user:proxy-password@127.0.0.1:1", timeout: 43},
	}
	for _, tt := range proxies {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validRadarFetcherTestConfig()
			cfg.Update.ProxyURL = tt.proxyURL
			cfg.Radar.ExternalRequestTimeoutSeconds = tt.timeout
			radarClient, err := NewRadarHTTPClient(cfg)
			require.NoError(t, err)

			pooledClient, err := httpclient.GetClient(radarHTTPClientTestOptions(cfg, false))
			require.NoError(t, err)
			require.Same(t, pooledClient.Transport, radarClient.Transport)
			validatedClient, err := httpclient.GetClient(radarHTTPClientTestOptions(cfg, true))
			require.NoError(t, err)
			require.NotSame(t, validatedClient.Transport, radarClient.Transport,
				"proxy traffic must not perform local target DNS pre-validation")
		})
	}
}
