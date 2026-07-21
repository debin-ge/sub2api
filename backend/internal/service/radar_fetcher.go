package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
)

const (
	radarFetchMaxAttempts = 3
)

var radarFetchBackoffs = [...]time.Duration{time.Second, 2 * time.Second}

// RadarHTTPDoer is the subset of http.Client used by external radar fetchers.
// A narrow interface keeps fetchers deterministic in tests while allowing a
// shared *http.Client in production.
type RadarHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RadarFetcher fetches and validates one external Radar source. Implementations
// return the original validated payload and never persist it.
type RadarFetcher interface {
	Source() RadarSourceKey
	Interval() time.Duration
	Fetch(ctx context.Context) (payload []byte, meta SourceFetchMeta, err error)
}

// RadarSleepFunc waits for a retry backoff. Production uses
// radarSleepWithContext; tests may inject an immediate recorder.
type RadarSleepFunc func(ctx context.Context, duration time.Duration) error

// RadarFetcherConfigError identifies invalid fetcher configuration without
// including the configured value, which may contain credentials.
type RadarFetcherConfigError struct {
	Field string
}

func (e *RadarFetcherConfigError) Error() string {
	if e == nil || e.Field == "" {
		return "invalid radar fetcher configuration"
	}
	return "invalid radar fetcher configuration: " + e.Field
}

// RadarFetchError is a sanitized external-source failure. Its string form
// contains only the safe error classification. Context cancellation and
// deadline sentinels remain available through errors.Is.
type RadarFetchError struct {
	Code  DataSourceErrorCode
	cause error
}

func (e *RadarFetchError) Error() string {
	if e == nil {
		return "radar fetch failed"
	}
	return "radar fetch failed: " + string(e.Code)
}

func (e *RadarFetchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type radarResponseValidator func(payload []byte) error

type radarResponseReadError struct {
	cause error
}

func (e *radarResponseReadError) Error() string {
	return "failed to read radar response body"
}

func (e *radarResponseReadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type radarHTTPFetcher struct {
	source           RadarSourceKey
	interval         time.Duration
	client           RadarHTTPDoer
	endpoint         string
	headers          http.Header
	maxResponseBytes int64
	validate         radarResponseValidator
	sleep            RadarSleepFunc
	now              func() time.Time
}

type radarHTTPFetcherOptions struct {
	source           RadarSourceKey
	interval         time.Duration
	client           RadarHTTPDoer
	endpoint         string
	headers          http.Header
	maxResponseBytes int64
	validate         radarResponseValidator
	sleep            RadarSleepFunc
	now              func() time.Time
}

func newRadarHTTPFetcher(options radarHTTPFetcherOptions) (*radarHTTPFetcher, error) {
	if options.source == "" {
		return nil, &RadarFetcherConfigError{Field: "source"}
	}
	if options.interval <= 0 {
		return nil, &RadarFetcherConfigError{Field: "interval"}
	}
	if isNilRadarHTTPDoer(options.client) {
		return nil, &RadarFetcherConfigError{Field: "http_client"}
	}
	parsedEndpoint, err := url.Parse(options.endpoint)
	if err != nil || (parsedEndpoint.Scheme != "http" && parsedEndpoint.Scheme != "https") || parsedEndpoint.Host == "" {
		return nil, &RadarFetcherConfigError{Field: "endpoint"}
	}
	if options.maxResponseBytes <= 0 {
		return nil, &RadarFetcherConfigError{Field: "radar.external_response_max_bytes"}
	}
	if options.validate == nil {
		return nil, &RadarFetcherConfigError{Field: "response_validator"}
	}
	if options.sleep == nil {
		options.sleep = radarSleepWithContext
	}
	if options.now == nil {
		options.now = time.Now
	}

	return &radarHTTPFetcher{
		source:           options.source,
		interval:         options.interval,
		client:           options.client,
		endpoint:         options.endpoint,
		headers:          options.headers.Clone(),
		maxResponseBytes: options.maxResponseBytes,
		validate:         options.validate,
		sleep:            options.sleep,
		now:              options.now,
	}, nil
}

func isNilRadarHTTPDoer(client RadarHTTPDoer) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (f *radarHTTPFetcher) Source() RadarSourceKey {
	return f.source
}

func (f *radarHTTPFetcher) Interval() time.Duration {
	return f.interval
}

func (f *radarHTTPFetcher) Fetch(ctx context.Context) ([]byte, SourceFetchMeta, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var meta SourceFetchMeta
	var lastContextCause error
	for attempt := 0; attempt < radarFetchMaxAttempts; attempt++ {
		attemptedAt := f.now().UTC()
		meta = SourceFetchMeta{LastAttemptAt: attemptedAt}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.endpoint, nil)
		if err != nil {
			return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
		}
		req.Header = f.headers.Clone()

		resp, requestErr := f.client.Do(req)
		if requestErr != nil {
			closeRadarResponseBody(resp)
			meta.HTTPStatus = nil
			lastContextCause = radarContextCause(ctx, requestErr)
			if cause := lastContextCause; cause != nil {
				if ctx.Err() != nil || errors.Is(cause, context.Canceled) {
					return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, cause)
				}
				lastContextCause = cause
			}
			if attempt == radarFetchMaxAttempts-1 {
				return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, lastContextCause)
			}
			if sleepErr := f.sleep(ctx, radarFetchBackoffs[attempt]); sleepErr != nil {
				meta.HTTPStatus = nil
				return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, radarContextCause(ctx, sleepErr))
			}
			continue
		}
		if resp == nil {
			meta.HTTPStatus = nil
			lastContextCause = nil
			if attempt == radarFetchMaxAttempts-1 {
				return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, nil)
			}
			if sleepErr := f.sleep(ctx, radarFetchBackoffs[attempt]); sleepErr != nil {
				return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, radarContextCause(ctx, sleepErr))
			}
			continue
		}

		status := resp.StatusCode
		meta.HTTPStatus = &status
		payload, bodyErr := readAndCloseRadarResponse(resp, f.maxResponseBytes)
		if bodyErr != nil && ctx.Err() != nil {
			meta.HTTPStatus = nil
			return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, ctx.Err())
		}

		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			code := radarHTTPStatusErrorCode(status)
			if status >= http.StatusInternalServerError && status <= 599 && attempt < radarFetchMaxAttempts-1 {
				if sleepErr := f.sleep(ctx, radarFetchBackoffs[attempt]); sleepErr != nil {
					meta.HTTPStatus = nil
					return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, radarContextCause(ctx, sleepErr))
				}
				continue
			}
			return radarFetchFailure(meta, code, nil)
		}

		if bodyErr != nil {
			var readErr *radarResponseReadError
			if errors.As(bodyErr, &readErr) {
				meta.HTTPStatus = nil
				lastContextCause = radarContextCause(ctx, readErr)
				if lastContextCause != nil && (ctx.Err() != nil || errors.Is(lastContextCause, context.Canceled)) {
					return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, lastContextCause)
				}
				if attempt < radarFetchMaxAttempts-1 {
					if sleepErr := f.sleep(ctx, radarFetchBackoffs[attempt]); sleepErr != nil {
						return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, radarContextCause(ctx, sleepErr))
					}
					continue
				}
				return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, lastContextCause)
			}
			return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
		}
		if err := f.validate(payload); err != nil {
			return radarFetchFailure(meta, DataSourceErrorCodeInvalidResponse, nil)
		}

		meta.LastSuccessAt = &attemptedAt
		return payload, meta, nil
	}

	return radarFetchFailure(meta, DataSourceErrorCodeNetworkError, lastContextCause)
}

func readAndCloseRadarResponse(resp *http.Response, maxBytes int64) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("missing response body")
	}
	if resp.ContentLength > maxBytes {
		_ = resp.Body.Close()
		return nil, errors.New("response body exceeds configured limit")
	}

	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, &radarResponseReadError{cause: readErr}
	}
	if int64(len(payload)) > maxBytes {
		return nil, errors.New("response body exceeds configured limit")
	}
	return payload, nil
}

func closeRadarResponseBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func radarHTTPStatusErrorCode(status int) DataSourceErrorCode {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return DataSourceErrorCodeUnauthorized
	case http.StatusTooManyRequests:
		return DataSourceErrorCodeRateLimited
	default:
		return DataSourceErrorCodeUpstreamError
	}
}

func radarFetchFailure(meta SourceFetchMeta, code DataSourceErrorCode, cause error) ([]byte, SourceFetchMeta, error) {
	meta.LastSuccessAt = nil
	meta.Error = &code
	return nil, meta, &RadarFetchError{Code: code, cause: canonicalRadarContextCause(cause)}
}

func radarContextCause(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return canonicalRadarContextCause(err)
}

func canonicalRadarContextCause(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func radarSleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewRadarHTTPClient returns a Radar-specific client backed by the shared
// production transport. Invalid proxy configuration is returned without
// falling back to a direct connection or exposing the proxy URL.
func NewRadarHTTPClient(cfg *config.Config) (*http.Client, error) {
	if cfg == nil {
		return nil, &RadarFetcherConfigError{Field: "config"}
	}
	if err := cfg.Radar.Validate(); err != nil {
		return nil, &RadarFetcherConfigError{Field: "radar"}
	}

	timeout := time.Duration(cfg.Radar.ExternalRequestTimeoutSeconds) * time.Second
	validateResolvedIP := strings.TrimSpace(cfg.Update.ProxyURL) == ""
	pooledClient, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:              cfg.Update.ProxyURL,
		Timeout:               timeout,
		ResponseHeaderTimeout: timeout,
		ValidateResolvedIP:    validateResolvedIP,
	})
	if err != nil {
		return nil, &RadarFetcherConfigError{Field: "update.proxy_url"}
	}

	radarClient := *pooledClient
	radarClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &radarClient, nil
}

var _ RadarFetcher = (*radarHTTPFetcher)(nil)
