package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type radarHandlerCall struct {
	method string
	bucket string
	days   int
}

type fakeRadarPublicService struct {
	mu sync.Mutex

	calls []radarHandlerCall

	health         []service.ServiceHealthDTO
	quotaLatest    *service.QuotaRadarLatestDTO
	quotaTrend     *service.QuotaTrendDTO
	latest         *service.DegradationLatestDTO
	lmarena        *service.LMArenaDTO
	sources        []service.DataSourceMetaDTO
	healthErr      error
	quotaLatestErr error
	quotaTrendErr  error
	latestErr      error
	lmarenaErr     error
	sourcesErr     error
}

func (f *fakeRadarPublicService) record(call radarHandlerCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeRadarPublicService) snapshotCalls() []radarHandlerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]radarHandlerCall(nil), f.calls...)
}

func (f *fakeRadarPublicService) GetServiceHealth(context.Context) ([]service.ServiceHealthDTO, error) {
	f.record(radarHandlerCall{method: "service-health"})
	return f.health, f.healthErr
}

func (f *fakeRadarPublicService) GetQuotaBucketsLatest(context.Context) (*service.QuotaRadarLatestDTO, error) {
	f.record(radarHandlerCall{method: "quota-buckets/latest"})
	return f.quotaLatest, f.quotaLatestErr
}

func (f *fakeRadarPublicService) GetQuotaBucketsTrend(_ context.Context, bucket string, days int) (*service.QuotaTrendDTO, error) {
	f.record(radarHandlerCall{method: "quota-buckets/trend", bucket: bucket, days: days})
	return f.quotaTrend, f.quotaTrendErr
}

func (f *fakeRadarPublicService) GetDegradationLatest(context.Context) (*service.DegradationLatestDTO, error) {
	f.record(radarHandlerCall{method: "degradation/latest"})
	return f.latest, f.latestErr
}

func (f *fakeRadarPublicService) GetLMArena(context.Context) (*service.LMArenaDTO, error) {
	f.record(radarHandlerCall{method: "lmarena"})
	return f.lmarena, f.lmarenaErr
}

func (f *fakeRadarPublicService) GetDataSources(context.Context) ([]service.DataSourceMetaDTO, error) {
	f.record(radarHandlerCall{method: "sources"})
	return f.sources, f.sourcesErr
}

func newRadarHandlerTestConfig(slugs ...string) *config.Config {
	return &config.Config{}
}

func newRadarHandlerForTest(t *testing.T, radarService service.RadarPublicService, slugs ...string) *RadarHandler {
	t.Helper()
	h, err := NewRadarHandler(newRadarHandlerTestConfig(slugs...), radarService)
	require.NoError(t, err)
	return h
}

func performRadarHandlerRequest(handler gin.HandlerFunc, rawQuery string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/radar", handler)
	req := httptest.NewRequest(http.MethodGet, "/radar", nil)
	req.URL.RawQuery = strings.TrimPrefix(rawQuery, "?")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestNewRadarHandlerRejectsNilDependencies(t *testing.T) {
	var typedNil *fakeRadarPublicService

	tests := []struct {
		name         string
		cfg          *config.Config
		radarService service.RadarPublicService
	}{
		{name: "nil config", cfg: nil, radarService: &fakeRadarPublicService{}},
		{name: "nil service", cfg: newRadarHandlerTestConfig("model-a"), radarService: nil},
		{name: "typed nil service", cfg: newRadarHandlerTestConfig("model-a"), radarService: typedNil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := NewRadarHandler(tt.cfg, tt.radarService)
			require.Error(t, err)
			require.Nil(t, h)
			require.NotContains(t, err.Error(), "redis-secret")
		})
	}
}

func TestRadarHandlerSuccessUsesEnvelopeAndExactCacheControl(t *testing.T) {
	fake := &fakeRadarPublicService{
		health:      make([]service.ServiceHealthDTO, 0),
		quotaLatest: &service.QuotaRadarLatestDTO{Buckets: make([]service.BucketSnapshotDTO, 0), SampleSizeWarnBelow: 3, Stale: true},
		quotaTrend:  &service.QuotaTrendDTO{BucketKey: "anthropic/pro", Days: 7, DataPoints: make([]service.QuotaTrendPointDTO, 0), Stale: true},
		latest:      &service.DegradationLatestDTO{Models: make([]service.DegradationModelDTO, 0), LMArenaTop5: make([]service.LMArenaEntryDTO, 0), SourcesLastUpdated: map[string]*time.Time{}},
		lmarena:     &service.LMArenaDTO{Leaderboard: make([]service.LMArenaEntryDTO, 0)},
		sources:     make([]service.DataSourceMetaDTO, 0),
	}
	h := newRadarHandlerForTest(t, fake, "model-a")

	tests := []struct {
		name         string
		handler      gin.HandlerFunc
		query        string
		cacheControl string
	}{
		{name: "service health", handler: h.GetServiceHealth, cacheControl: "public, max-age=300"},
		{name: "quota latest", handler: h.GetQuotaBucketsLatest, cacheControl: "public, max-age=300"},
		{name: "quota trend", handler: h.GetQuotaBucketsTrend, query: "?bucket=anthropic%2Fpro", cacheControl: "public, max-age=600"},
		{name: "degradation latest", handler: h.GetDegradationLatest, cacheControl: "public, max-age=300"},
		{name: "lmarena", handler: h.GetLMArena, cacheControl: "public, max-age=300"},
		{name: "sources", handler: h.GetDataSources, cacheControl: "public, max-age=600"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := performRadarHandlerRequest(tt.handler, tt.query)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tt.cacheControl, w.Header().Get("Cache-Control"))
			var envelope struct {
				Code    int             `json:"code"`
				Message string          `json:"message"`
				Data    json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.Equal(t, 0, envelope.Code)
			require.Equal(t, "success", envelope.Message)
			require.NotEmpty(t, envelope.Data)
		})
	}

	require.Contains(t, fake.snapshotCalls(), radarHandlerCall{
		method: "quota-buckets/trend",
		bucket: "anthropic/pro",
		days:   7,
	})
}

func TestRadarHandlerDegradationCacheControlDistinguishesTransientEmptyFallback(t *testing.T) {
	tests := []struct {
		name         string
		latest       *service.DegradationLatestDTO
		cacheControl string
	}{
		{
			name: "empty stale fallback is not cached",
			latest: &service.DegradationLatestDTO{
				Models:          make([]service.DegradationModelDTO, 0),
				AvailableModels: make([]service.DegradationModelDTO, 0),
				Stale:           true,
			},
			cacheControl: "no-store",
		},
		{
			name: "stale last known good snapshot keeps short cache",
			latest: &service.DegradationLatestDTO{
				Models:          []service.DegradationModelDTO{{Slug: "model-a"}},
				AvailableModels: []service.DegradationModelDTO{{Slug: "model-a"}},
				Stale:           true,
			},
			cacheControl: radarDegradationCacheControl,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRadarPublicService{latest: tt.latest}
			h := newRadarHandlerForTest(t, fake, "model-a")

			w := performRadarHandlerRequest(h.GetDegradationLatest, "")

			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tt.cacheControl, w.Header().Get("Cache-Control"))
		})
	}
}

func TestRadarHandlerPreservesDTOEmptyAndNullShapes(t *testing.T) {
	fake := &fakeRadarPublicService{latest: &service.DegradationLatestDTO{
		Models:             make([]service.DegradationModelDTO, 0),
		LMArenaTop5:        nil,
		SourcesLastUpdated: nil,
	}}
	h := newRadarHandlerForTest(t, fake, "model-a")

	w := performRadarHandlerRequest(h.GetDegradationLatest, "")

	require.Equal(t, http.StatusOK, w.Code)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.JSONEq(t, `[]`, string(envelope.Data["models"]))
	require.JSONEq(t, `null`, string(envelope.Data["lmarena_top5"]))
	require.JSONEq(t, `null`, string(envelope.Data["sources_last_updated"]))
}

func TestRadarHandlerQuotaLatestRejectsAllQueryParameters(t *testing.T) {
	for _, query := range []string{"?extra=value", "?bucket=anthropic%2Fpro", "?extra=%zz", "?extra=value&extra=value"} {
		fake := &fakeRadarPublicService{}
		h := newRadarHandlerForTest(t, fake, "model-a")

		w := performRadarHandlerRequest(h.GetQuotaBucketsLatest, query)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.Empty(t, fake.snapshotCalls())
		require.JSONEq(t, `{"code":400,"message":"invalid radar query"}`, w.Body.String())
	}
}

func TestRadarHandlerQuotaTrendValidatesBeforeCallingService(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "missing bucket", query: "?days=7"},
		{name: "empty bucket", query: "?bucket="},
		{name: "blank bucket", query: "?bucket=%20"},
		{name: "uppercase platform", query: "?bucket=Anthropic%2Fpro"},
		{name: "uppercase tier", query: "?bucket=anthropic%2FPRO"},
		{name: "unknown platform", query: "?bucket=google%2Fpro"},
		{name: "path traversal", query: "?bucket=anthropic%2Fpro%2F..%2F..%2Fradar%3Alock"},
		{name: "repeated bucket", query: "?bucket=anthropic%2Fpro&bucket=anthropic%2Fpro"},
		{name: "empty days", query: "?bucket=anthropic%2Fpro&days="},
		{name: "zero days", query: "?bucket=anthropic%2Fpro&days=0"},
		{name: "days above maximum", query: "?bucket=anthropic%2Fpro&days=8"},
		{name: "negative days", query: "?bucket=anthropic%2Fpro&days=-1"},
		{name: "non decimal days", query: "?bucket=anthropic%2Fpro&days=1.0"},
		{name: "explicit plus days", query: "?bucket=anthropic%2Fpro&days=%2B1"},
		{name: "space padded days", query: "?bucket=anthropic%2Fpro&days=%201%20"},
		{name: "overflow days", query: "?bucket=anthropic%2Fpro&days=999999999999999999999"},
		{name: "repeated days", query: "?bucket=anthropic%2Fpro&days=7&days=7"},
		{name: "malformed escape", query: "?bucket=anthropic%2Fpro&days=%zz"},
		{name: "unknown parameter", query: "?bucket=anthropic%2Fpro&extra=value"},
		{name: "blank parameter name", query: "?bucket=anthropic%2Fpro&=value"},
		{name: "bare semicolon", query: "?bucket=anthropic%2Fpro;days=7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRadarPublicService{}
			h := newRadarHandlerForTest(t, fake, "model-a")

			w := performRadarHandlerRequest(h.GetQuotaBucketsTrend, tt.query)

			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Empty(t, fake.snapshotCalls(), "invalid input must be rejected before the service call")
			require.JSONEq(t, `{"code":400,"message":"invalid radar query"}`, w.Body.String())
			require.NotContains(t, w.Body.String(), "anthropic")
			require.NotContains(t, w.Body.String(), "radar:lock")
		})
	}
}

func TestRadarHandlerQuotaTrendAcceptsEncodedSlashAndDayBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name     string
		query    string
		bucket   string
		wantDays int
	}{
		{name: "encoded slash default", query: "?bucket=anthropic%2Fmax_20x", bucket: "anthropic/max_20x", wantDays: 7},
		{name: "literal slash minimum", query: "?bucket=openai/pro&days=1", bucket: "openai/pro", wantDays: 1},
		{name: "maximum", query: "?bucket=antigravity/ultra&days=7", bucket: "antigravity/ultra", wantDays: 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRadarPublicService{quotaTrend: &service.QuotaTrendDTO{DataPoints: make([]service.QuotaTrendPointDTO, 0)}}
			h := newRadarHandlerForTest(t, fake, "model-a")

			w := performRadarHandlerRequest(h.GetQuotaBucketsTrend, tt.query)

			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, []radarHandlerCall{{
				method: "quota-buckets/trend",
				bucket: tt.bucket,
				days:   tt.wantDays,
			}}, fake.snapshotCalls())
		})
	}
}

func TestRadarHandlerQuotaDTOEmptyAndNullJSONShapes(t *testing.T) {
	fake := &fakeRadarPublicService{
		quotaLatest: &service.QuotaRadarLatestDTO{Buckets: make([]service.BucketSnapshotDTO, 0), LastAggregatedAt: nil, Stale: true},
		quotaTrend:  &service.QuotaTrendDTO{BucketKey: "anthropic/pro", Days: 7, DataPoints: make([]service.QuotaTrendPointDTO, 0), Stale: true},
	}
	h := newRadarHandlerForTest(t, fake, "model-a")

	latest := performRadarHandlerRequest(h.GetQuotaBucketsLatest, "")
	require.Equal(t, http.StatusOK, latest.Code)
	var latestEnvelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(latest.Body.Bytes(), &latestEnvelope))
	require.JSONEq(t, `[]`, string(latestEnvelope.Data["buckets"]))
	require.JSONEq(t, `null`, string(latestEnvelope.Data["last_aggregated_at"]))

	trend := performRadarHandlerRequest(h.GetQuotaBucketsTrend, "?bucket=anthropic%2Fpro")
	require.Equal(t, http.StatusOK, trend.Code)
	var trendEnvelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(trend.Body.Bytes(), &trendEnvelope))
	require.JSONEq(t, `[]`, string(trendEnvelope.Data["data_points"]))
}

func TestRadarHandlerRejectsQueriesOnParameterlessEndpoints(t *testing.T) {
	endpoints := []struct {
		name    string
		handler func(*RadarHandler) gin.HandlerFunc
	}{
		{"service-health", func(h *RadarHandler) gin.HandlerFunc { return h.GetServiceHealth }},
		{"quota-latest", func(h *RadarHandler) gin.HandlerFunc { return h.GetQuotaBucketsLatest }},
		{"degradation-latest", func(h *RadarHandler) gin.HandlerFunc { return h.GetDegradationLatest }},
		{"lmarena", func(h *RadarHandler) gin.HandlerFunc { return h.GetLMArena }},
		{"sources", func(h *RadarHandler) gin.HandlerFunc { return h.GetDataSources }},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			fake := &fakeRadarPublicService{}
			h := newRadarHandlerForTest(t, fake, "model-a")
			w := performRadarHandlerRequest(endpoint.handler(h), "?unknown=1")
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Empty(t, fake.snapshotCalls())
			require.JSONEq(t, `{"code":400,"message":"invalid radar query"}`, w.Body.String())
		})
	}
}

func TestRadarHandlerMapsServiceErrorsWithoutExposingDetails(t *testing.T) {
	const sensitiveDetail = "redis key radar:aa_perf:model-a api_key=top-secret query=model-a"

	tests := []struct {
		name    string
		setErr  func(*fakeRadarPublicService, error)
		handler func(*RadarHandler) gin.HandlerFunc
		query   string
	}{
		{name: "service health", setErr: func(f *fakeRadarPublicService, err error) { f.healthErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetServiceHealth }},
		{name: "quota latest", setErr: func(f *fakeRadarPublicService, err error) { f.quotaLatestErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetQuotaBucketsLatest }},
		{name: "quota trend", setErr: func(f *fakeRadarPublicService, err error) { f.quotaTrendErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetQuotaBucketsTrend }, query: "?bucket=anthropic%2Fpro"},
		{name: "degradation latest", setErr: func(f *fakeRadarPublicService, err error) { f.latestErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetDegradationLatest }},
		{name: "lmarena", setErr: func(f *fakeRadarPublicService, err error) { f.lmarenaErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetLMArena }},
		{name: "sources", setErr: func(f *fakeRadarPublicService, err error) { f.sourcesErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetDataSources }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRadarPublicService{}
			tt.setErr(fake, errors.New(sensitiveDetail))
			h := newRadarHandlerForTest(t, fake, "model-a")

			w := performRadarHandlerRequest(tt.handler(h), tt.query)

			require.Equal(t, http.StatusInternalServerError, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.JSONEq(t, `{"code":500,"message":"internal error"}`, w.Body.String())
			require.NotContains(t, w.Body.String(), "top-secret")
			require.NotContains(t, w.Body.String(), "radar:aa_perf")
			require.NotContains(t, w.Body.String(), "model-a")
		})
	}
}

func TestRadarHandlerMapsClassifiedAndContextErrors(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{name: "invalid service query", err: service.ErrInvalidRadarQuery, wantStatus: http.StatusBadRequest, wantMessage: "invalid radar query"},
		{name: "wrapped invalid service query", err: errors.Join(errors.New("internal detail"), service.ErrInvalidRadarQuery), wantStatus: http.StatusBadRequest, wantMessage: "invalid radar query"},
		{name: "client canceled", err: context.Canceled, wantStatus: 499, wantMessage: "context canceled"},
		{name: "deadline exceeded", err: context.DeadlineExceeded, wantStatus: http.StatusServiceUnavailable, wantMessage: "Service temporarily unavailable, please retry later"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRadarPublicService{healthErr: tt.err}
			h := newRadarHandlerForTest(t, fake, "model-a")

			w := performRadarHandlerRequest(h.GetServiceHealth, "")

			require.Equal(t, tt.wantStatus, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.JSONEq(t, `{"code":`+strconv.Itoa(tt.wantStatus)+`,"message":"`+tt.wantMessage+`"}`, w.Body.String())
		})
	}
}

func TestRadarHandlerMapsRadarUnavailableAcrossEndpoints(t *testing.T) {
	const sensitiveDetail = "redis key radar:aa_perf:model-a api_key=top-secret"

	endpoints := []struct {
		name    string
		setErr  func(*fakeRadarPublicService, error)
		handler func(*RadarHandler) gin.HandlerFunc
		query   string
	}{
		{name: "service health", setErr: func(f *fakeRadarPublicService, err error) { f.healthErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetServiceHealth }},
		{name: "quota latest", setErr: func(f *fakeRadarPublicService, err error) { f.quotaLatestErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetQuotaBucketsLatest }},
		{name: "quota trend", setErr: func(f *fakeRadarPublicService, err error) { f.quotaTrendErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetQuotaBucketsTrend }, query: "?bucket=anthropic%2Fpro"},
		{name: "degradation latest", setErr: func(f *fakeRadarPublicService, err error) { f.latestErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetDegradationLatest }},
		{name: "lmarena", setErr: func(f *fakeRadarPublicService, err error) { f.lmarenaErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetLMArena }},
		{name: "sources", setErr: func(f *fakeRadarPublicService, err error) { f.sourcesErr = err }, handler: func(h *RadarHandler) gin.HandlerFunc { return h.GetDataSources }},
	}
	errCases := []struct {
		name string
		err  error
	}{
		{name: "direct", err: service.ErrRadarUnavailable},
		{name: "wrapped", err: fmt.Errorf("%s: %w", sensitiveDetail, service.ErrRadarUnavailable)},
	}

	for _, endpoint := range endpoints {
		for _, errCase := range errCases {
			t.Run(endpoint.name+"/"+errCase.name, func(t *testing.T) {
				fake := &fakeRadarPublicService{}
				endpoint.setErr(fake, errCase.err)
				h := newRadarHandlerForTest(t, fake, "model-a")

				w := performRadarHandlerRequest(endpoint.handler(h), endpoint.query)

				require.Equal(t, http.StatusServiceUnavailable, w.Code)
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.JSONEq(t, `{"code":503,"message":"Service temporarily unavailable, please retry later"}`, w.Body.String())
				require.NotContains(t, w.Body.String(), "top-secret")
				require.NotContains(t, w.Body.String(), "radar:aa_perf")
				require.NotContains(t, w.Body.String(), "model-a")
			})
		}
	}
}

func TestRadarHandlerErrorBodyNeverEchoesMalformedQuery(t *testing.T) {
	fake := &fakeRadarPublicService{}
	h := newRadarHandlerForTest(t, fake, "model-a")

	w := performRadarHandlerRequest(h.GetDegradationLatest, "?model=redis%3Asecret-key&days=secret-api-key")

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.False(t, strings.Contains(w.Body.String(), "redis"))
	require.False(t, strings.Contains(w.Body.String(), "secret"))
}
