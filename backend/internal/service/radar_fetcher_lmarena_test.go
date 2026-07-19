package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const validLMArenaPayload = `{
  "meta": {
    "fetched_at": "2026-07-10T18:30:00.123456789+08:00",
    "last_updated": "Jul 10, 2026",
    "model_count": 2
  },
  "models": [
    {"rank": 2, "model": "Model B", "vendor": "Vendor B", "score": 1200.5, "ci": 5.25, "votes": 20},
    {"rank": 1, "model": "Model A", "vendor": "Vendor A", "score": 1300, "ci": 7.5, "votes": 10}
  ]
}`

func TestLMArenaFetcherFetchesEveryOfficialPageAndBuildsCanonicalPayload(t *testing.T) {
	const total = 101
	requests := make([]*http.Request, 0, 2)
	bodies := make([]*radarTrackingBody, 0, 2)
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		offset, err := strconv.Atoi(req.URL.Query().Get("offset"))
		require.NoError(t, err)
		body := newRadarTrackingBody(hfLMArenaPage(t, offset, total, hfLMArenaPageOptions{}))
		bodies = append(bodies, body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Revision":   []string{strings.Repeat("a", 40)},
			},
		}, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.LMArenaURL = "https://datasets-server.huggingface.co/filter"
	cfg.Radar.LMArenaIntervalMinutes = 777

	fetcher, err := NewLMArenaFetcher(cfg, client)
	require.NoError(t, err)
	require.Equal(t, RadarSourceLMArena, fetcher.Source())
	require.Equal(t, 777*time.Minute, fetcher.Interval())

	payload, meta, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	require.Len(t, requests, 2, "the model beyond the first 100 rows must be fetched")
	for index, request := range requests {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https", request.URL.Scheme)
		require.Equal(t, "datasets-server.huggingface.co", request.URL.Host)
		require.Equal(t, "/filter", request.URL.EscapedPath())
		require.Len(t, request.URL.Query(), 7)
		require.Equal(t, "lmarena-ai/leaderboard-dataset", request.URL.Query().Get("dataset"))
		require.Equal(t, "text_style_control", request.URL.Query().Get("config"))
		require.Equal(t, "latest", request.URL.Query().Get("split"))
		require.Equal(t, `"category" = 'overall'`, request.URL.Query().Get("where"))
		require.Equal(t, `"rank" ASC`, request.URL.Query().Get("orderby"))
		require.Equal(t, strconv.Itoa(index*100), request.URL.Query().Get("offset"))
		require.Equal(t, "100", request.URL.Query().Get("length"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.Empty(t, request.Header.Get("Authorization"))
	}
	for _, body := range bodies {
		require.True(t, body.isClosed())
	}
	snapshot, err := DecodeLMArena(payload)
	require.NoError(t, err)
	require.Equal(t, total, snapshot.ModelCount)
	require.Len(t, snapshot.Models, total)
	require.Equal(t, "gpt-5.5", snapshot.Models[19].Model)
	require.Equal(t, 20, snapshot.Models[19].Rank)
	require.Equal(t, "openai", snapshot.Models[19].Vendor)
	require.Equal(t, 1474.5, snapshot.Models[19].Score)
	require.Equal(t, 4.5, snapshot.Models[19].CI)
	require.NotNil(t, snapshot.Models[19].Votes)
	require.Equal(t, int64(42020), *snapshot.Models[19].Votes)
	require.NotNil(t, snapshot.LastUpdatedAt)
	require.Equal(t, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), *snapshot.LastUpdatedAt)
	require.Nil(t, meta.Error)
	require.NotNil(t, meta.LastSuccessAt)
	require.Equal(t, meta.LastSuccessAt.UTC(), snapshot.FetchedAt)
}

func TestLMArenaFetcherRejectsInconsistentOrUnsafeOfficialPages(t *testing.T) {
	tests := []struct {
		name      string
		page      func(offset int) string
		revision  func(offset int) string
		wantCalls int
	}{
		{
			name: "revision changes between pages",
			page: func(offset int) string { return hfLMArenaPage(t, offset, 101, hfLMArenaPageOptions{}) },
			revision: func(offset int) string {
				if offset == 0 {
					return strings.Repeat("a", 40)
				}
				return strings.Repeat("b", 40)
			},
			wantCalls: 2,
		},
		{
			name: "total changes between pages",
			page: func(offset int) string {
				total := 101
				if offset != 0 {
					total = 102
				}
				return hfLMArenaPage(t, offset, total, hfLMArenaPageOptions{})
			},
			revision:  func(int) string { return strings.Repeat("a", 40) },
			wantCalls: 2,
		},
		{
			name:      "partial page",
			page:      func(offset int) string { return hfLMArenaPage(t, offset, 1, hfLMArenaPageOptions{partial: true}) },
			revision:  func(int) string { return strings.Repeat("a", 40) },
			wantCalls: 1,
		},
		{
			name:      "truncated cell",
			page:      func(offset int) string { return hfLMArenaPage(t, offset, 1, hfLMArenaPageOptions{truncated: true}) },
			revision:  func(int) string { return strings.Repeat("a", 40) },
			wantCalls: 1,
		},
		{
			name: "fractional votes",
			page: func(offset int) string {
				return hfLMArenaPage(t, offset, 1, hfLMArenaPageOptions{fractionalVotes: true})
			},
			revision:  func(int) string { return strings.Repeat("a", 40) },
			wantCalls: 1,
		},
		{
			name: "schema dtype drift",
			page: func(offset int) string {
				return mutateHFLMArenaPage(t, hfLMArenaPage(t, offset, 1, hfLMArenaPageOptions{}), func(page map[string]any) {
					features := page["features"].([]any)
					features[3].(map[string]any)["type"].(map[string]any)["dtype"] = "string"
				})
			},
			revision:  func(int) string { return strings.Repeat("a", 40) },
			wantCalls: 1,
		},
		{
			name: "short first page",
			page: func(offset int) string {
				return mutateHFLMArenaPage(t, hfLMArenaPage(t, offset, 101, hfLMArenaPageOptions{}), func(page map[string]any) {
					rows := page["rows"].([]any)
					page["rows"] = rows[:len(rows)-1]
				})
			},
			revision:  func(int) string { return strings.Repeat("a", 40) },
			wantCalls: 1,
		},
		{
			name: "duplicate ranks",
			page: func(offset int) string {
				return mutateHFLMArenaPage(t, hfLMArenaPage(t, offset, 2, hfLMArenaPageOptions{}), func(page map[string]any) {
					rows := page["rows"].([]any)
					rows[1].(map[string]any)["row"].(map[string]any)["rank"] = float64(1)
				})
			},
			revision:  func(int) string { return strings.Repeat("a", 40) },
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				offset, err := strconv.Atoi(req.URL.Query().Get("offset"))
				require.NoError(t, err)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       newRadarTrackingBody(tt.page(offset)),
					Header: http.Header{
						"Content-Type": []string{"application/json"},
						"X-Revision":   []string{tt.revision(offset)},
					},
				}, nil
			})
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.LMArenaURL = "https://datasets-server.huggingface.co/filter"
			fetcher, err := NewLMArenaFetcher(cfg, client)
			require.NoError(t, err)

			payload, meta, err := fetcher.Fetch(context.Background())

			require.Error(t, err)
			require.Nil(t, payload)
			require.Equal(t, tt.wantCalls, attempts)
			require.Nil(t, meta.LastSuccessAt)
			requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeInvalidResponse)
		})
	}
}

func TestLMArenaFetcherRejectsUntrustedOfficialResponseMetadata(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		revision    string
	}{
		{name: "missing content type", revision: strings.Repeat("a", 40)},
		{name: "HTML challenge", contentType: "text/html", revision: strings.Repeat("a", 40)},
		{name: "missing revision", contentType: "application/json"},
		{name: "uppercase revision", contentType: "application/json", revision: strings.Repeat("A", 40)},
		{name: "malformed revision", contentType: "application/json", revision: strings.Repeat("g", 40)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       newRadarTrackingBody(hfLMArenaPage(t, 0, 1, hfLMArenaPageOptions{})),
					Header: http.Header{
						"Content-Type": []string{tt.contentType},
						"X-Revision":   []string{tt.revision},
					},
				}, nil
			})
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.LMArenaURL = "https://datasets-server.huggingface.co/filter"
			fetcher, err := NewLMArenaFetcher(cfg, client)
			require.NoError(t, err)

			payload, meta, err := fetcher.Fetch(context.Background())

			require.Error(t, err)
			require.Nil(t, payload)
			require.Nil(t, meta.LastSuccessAt)
			requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeInvalidResponse)
		})
	}
}

func TestLMArenaFetcherCapsTheAggregatePaginatedResponse(t *testing.T) {
	firstPage := hfLMArenaPage(t, 0, 101, hfLMArenaPageOptions{})
	secondPage := hfLMArenaPage(t, 100, 101, hfLMArenaPageOptions{})
	limit := len(firstPage)
	if len(secondPage) > limit {
		limit = len(secondPage)
	}
	limit++ // Each page is allowed independently, but their aggregate is not.
	client := radarDoerFunc(func(req *http.Request) (*http.Response, error) {
		offset, err := strconv.Atoi(req.URL.Query().Get("offset"))
		require.NoError(t, err)
		payload := firstPage
		if offset == 100 {
			payload = secondPage
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       newRadarTrackingBody(payload),
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Revision":   []string{strings.Repeat("a", 40)},
			},
		}, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.LMArenaURL = "https://datasets-server.huggingface.co/filter"
	cfg.Radar.ExternalResponseMaxBytes = int64(limit)
	fetcher, err := NewLMArenaFetcher(cfg, client)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Nil(t, meta.LastSuccessAt)
	requireRadarFetchErrorCode(t, meta, DataSourceErrorCodeInvalidResponse)
}

func TestDecodeAndMapLMArenaCurrentSchema(t *testing.T) {
	snapshot, err := DecodeLMArena([]byte(validLMArenaPayload))
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 10, 10, 30, 0, 123456789, time.UTC), snapshot.FetchedAt)
	require.NotNil(t, snapshot.LastUpdatedAt)
	require.Equal(t, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), *snapshot.LastUpdatedAt)
	require.Equal(t, time.UTC, snapshot.FetchedAt.Location())
	require.Equal(t, time.UTC, snapshot.LastUpdatedAt.Location())
	require.Equal(t, 2, snapshot.ModelCount)
	require.Equal(t, []string{"Model A", "Model B"}, []string{snapshot.Models[0].Model, snapshot.Models[1].Model})
	require.Empty(t, snapshot.Warnings)

	dto, err := MapLMArena(snapshot)
	require.NoError(t, err)
	require.False(t, dto.Stale)
	require.NotNil(t, dto.LastUpdatedAt)
	require.Equal(t, *snapshot.LastUpdatedAt, *dto.LastUpdatedAt)
	require.Equal(t, time.UTC, dto.LastUpdatedAt.Location())
	require.NotNil(t, dto.FetchedAt)
	require.Equal(t, snapshot.FetchedAt, *dto.FetchedAt)
	require.Equal(t, time.UTC, dto.FetchedAt.Location())
	require.NotNil(t, dto.TotalVotes)
	require.Equal(t, int64(30), *dto.TotalVotes)
	require.Len(t, dto.Leaderboard, 2)

	first := dto.Leaderboard[0]
	require.Equal(t, 1, first.Rank)
	require.Equal(t, "Model A", first.Model)
	require.NotNil(t, first.Vendor)
	require.Equal(t, "Vendor A", *first.Vendor)
	require.NotNil(t, first.Elo)
	require.Equal(t, 1300.0, *first.Elo)
	require.NotNil(t, first.CILower)
	require.Equal(t, 1292.5, *first.CILower)
	require.NotNil(t, first.CIUpper)
	require.Equal(t, 1307.5, *first.CIUpper)
	require.NotNil(t, first.Votes)
	require.Equal(t, int64(10), *first.Votes)
}

func TestDecodeAndMapLMArenaAllowsUnknownLastUpdated(t *testing.T) {
	for _, tt := range []struct {
		name             string
		lastUpdatedField string
	}{
		{name: "explicit null", lastUpdatedField: `,"last_updated":null`},
		{name: "omitted", lastUpdatedField: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := []byte(`{
              "meta":{"fetched_at":"2026-07-15T04:32:51.929167+00:00"` + tt.lastUpdatedField + `,"model_count":1},
              "models":[{"rank":1,"model":"Model A","vendor":"Vendor A","score":1300,"ci":7.5,"votes":10}]
            }`)

			snapshot, err := DecodeLMArena(payload)
			require.NoError(t, err)
			require.Nil(t, snapshot.LastUpdatedAt)

			dto, err := MapLMArena(snapshot)
			require.NoError(t, err)
			require.Nil(t, dto.LastUpdatedAt)
			require.NotNil(t, dto.FetchedAt)
			require.Equal(t, snapshot.FetchedAt, *dto.FetchedAt)
			require.Len(t, dto.Leaderboard, 1)
		})
	}
}

func TestDecodeLMArenaReturnsStableSafeWarnings(t *testing.T) {
	payload := []byte(`{
      "meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":4},
      "models":[
        {"rank":3,"model":"Third","vendor":"V","score":3,"ci":0,"votes":3},
        {"rank":1,"model":"First A","vendor":"V","score":1,"ci":0,"votes":1},
        {"rank":1,"model":"First B","vendor":"V","score":2,"ci":0,"votes":2}
      ]
    }`)

	snapshot, err := DecodeLMArena(payload)

	require.NoError(t, err)
	require.Equal(t, []string{"First A", "First B", "Third"}, []string{
		snapshot.Models[0].Model,
		snapshot.Models[1].Model,
		snapshot.Models[2].Model,
	}, "equal ranks must retain upstream order")
	require.Equal(t, []LMArenaWarningCode{
		LMArenaWarningDuplicateRank,
		LMArenaWarningRankGap,
		LMArenaWarningModelCountMismatch,
	}, snapshot.Warnings)
	for _, warning := range snapshot.Warnings {
		require.NotContains(t, string(warning), "First")
		require.NotContains(t, string(warning), "Third")
	}
}

func TestDecodeLMArenaAcceptsEmptyModels(t *testing.T) {
	snapshot, err := DecodeLMArena([]byte(`{
      "meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":0},
      "models":[]
    }`))
	require.NoError(t, err)
	require.NotNil(t, snapshot.Models)
	require.Empty(t, snapshot.Models)
	require.Empty(t, snapshot.Warnings)

	dto, err := MapLMArena(snapshot)
	require.NoError(t, err)
	require.NotNil(t, dto.Leaderboard)
	require.Empty(t, dto.Leaderboard)
	require.NotNil(t, dto.TotalVotes)
	require.Zero(t, *dto.TotalVotes)
}

func TestDecodeLMArenaRejectsMalformedAmbiguousOrUnsafeData(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "malformed JSON", payload: `{"meta":`},
		{name: "trailing JSON", payload: `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":0},"models":[]} {}`},
		{name: "missing meta", payload: `{"models":[]}`},
		{name: "null meta", payload: `{"meta":null,"models":[]}`},
		{name: "bad fetched timestamp", payload: `{"meta":{"fetched_at":"yesterday","last_updated":"Jul 10, 2026","model_count":0},"models":[]}`},
		{name: "zero fetched timestamp", payload: `{"meta":{"fetched_at":"0001-01-01T00:00:00Z","last_updated":"Jul 10, 2026","model_count":0},"models":[]}`},
		{name: "bad updated date", payload: `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"2026-07-10","model_count":0},"models":[]}`},
		{name: "zero updated date", payload: `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jan 1, 0001","model_count":0},"models":[]}`},
		{name: "negative model count", payload: `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":-1},"models":[]}`},
		{name: "missing models", payload: `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":0}}`},
		{name: "null models", payload: `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":0},"models":null}`},
		{name: "rank zero", payload: lmarenaPayloadWithModel(`{"rank":0,"model":"Model","vendor":"V","score":1,"ci":0,"votes":1}`)},
		{name: "missing rank", payload: lmarenaPayloadWithModel(`{"model":"Model","vendor":"V","score":1,"ci":0,"votes":1}`)},
		{name: "blank model", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"  ","vendor":"V","score":1,"ci":0,"votes":1}`)},
		{name: "missing score", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"Model","vendor":"V","ci":0,"votes":1}`)},
		{name: "null score", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"Model","vendor":"V","score":null,"ci":0,"votes":1}`)},
		{name: "score numeric overflow", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"Model","vendor":"V","score":1e309,"ci":0,"votes":1}`)},
		{name: "negative ci", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"Model","vendor":"V","score":1,"ci":-0.1,"votes":1}`)},
		{name: "CI bounds overflow", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"Model","vendor":"V","score":1e308,"ci":1e308,"votes":1}`)},
		{name: "negative votes", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"Model","vendor":"V","score":1,"ci":0,"votes":-1}`)},
		{name: "votes integer overflow", payload: lmarenaPayloadWithModel(`{"rank":1,"model":"Model","vendor":"V","score":1,"ci":0,"votes":9223372036854775808}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, err := DecodeLMArena([]byte(tt.payload))
			require.Error(t, err)
			require.Zero(t, snapshot)
			require.NotContains(t, err.Error(), tt.payload)
			require.NotContains(t, err.Error(), "Model")
		})
	}
}

func TestDecodeAndMapLMArenaNullableVotes(t *testing.T) {
	seven := int64(7)
	maxInt := int64(math.MaxInt64)
	tests := []struct {
		name      string
		models    string
		wantVotes []*int64
		wantTotal *int64
	}{
		{
			name:      "missing",
			models:    `[{"rank":1,"model":"Missing","vendor":"V","score":1,"ci":0}]`,
			wantVotes: []*int64{nil},
			wantTotal: nil,
		},
		{
			name:      "null",
			models:    `[{"rank":1,"model":"Null","vendor":"V","score":1,"ci":0,"votes":null}]`,
			wantVotes: []*int64{nil},
			wantTotal: nil,
		},
		{
			name: "mixed",
			models: `[
				{"rank":1,"model":"Known","vendor":"V","score":1,"ci":0,"votes":7},
				{"rank":2,"model":"Unknown","vendor":"V","score":1,"ci":0}
			]`,
			wantVotes: []*int64{&seven, nil},
			wantTotal: nil,
		},
		{
			name: "mixed does not overflow an unavailable total",
			models: `[
				{"rank":1,"model":"Known","vendor":"V","score":1,"ci":0,"votes":9223372036854775807},
				{"rank":2,"model":"Unknown","vendor":"V","score":1,"ci":0}
			]`,
			wantVotes: []*int64{&maxInt, nil},
			wantTotal: nil,
		},
		{
			name:      "all present",
			models:    `[{"rank":1,"model":"Known","vendor":"V","score":1,"ci":0,"votes":7}]`,
			wantVotes: []*int64{&seven},
			wantTotal: &seven,
		},
		{
			name:      "empty",
			models:    `[]`,
			wantVotes: []*int64{},
			wantTotal: radarInt64Pointer(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":` +
				strconv.Itoa(len(tt.wantVotes)) + `},"models":` + tt.models + `}`
			snapshot, err := DecodeLMArena([]byte(payload))
			require.NoError(t, err)
			dto, err := MapLMArena(snapshot)
			require.NoError(t, err)
			require.Len(t, snapshot.Models, len(tt.wantVotes))
			require.Len(t, dto.Leaderboard, len(tt.wantVotes))
			for index, want := range tt.wantVotes {
				if want == nil {
					require.Nil(t, any(snapshot.Models[index].Votes))
					require.Nil(t, dto.Leaderboard[index].Votes)
					continue
				}
				snapshotVotes, ok := any(snapshot.Models[index].Votes).(*int64)
				require.True(t, ok, "decoded votes must retain nullable representation")
				require.NotNil(t, snapshotVotes)
				require.NotNil(t, dto.Leaderboard[index].Votes)
				require.Equal(t, *want, *snapshotVotes)
				require.Equal(t, *want, *dto.Leaderboard[index].Votes)
				require.NotSame(t, snapshotVotes, dto.Leaderboard[index].Votes)
			}
			if tt.wantTotal == nil {
				require.Nil(t, dto.TotalVotes)
			} else {
				require.NotNil(t, dto.TotalVotes)
				require.Equal(t, *tt.wantTotal, *dto.TotalVotes)
			}
		})
	}
}

func TestDecodeLMArenaRejectsTotalVotesOverflow(t *testing.T) {
	payload := []byte(`{
      "meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":2},
      "models":[
        {"rank":1,"model":"A","vendor":"V","score":1,"ci":0,"votes":9223372036854775807},
        {"rank":2,"model":"B","vendor":"V","score":1,"ci":0,"votes":1}
      ]
    }`)

	snapshot, err := DecodeLMArena(payload)

	require.Error(t, err)
	require.Zero(t, snapshot)
	require.NotContains(t, err.Error(), "9223372036854775807")
}

func TestMapLMArenaSortsWithoutMutatingOrAliasingInput(t *testing.T) {
	snapshot, err := DecodeLMArena([]byte(validLMArenaPayload))
	require.NoError(t, err)
	snapshot.Models[0], snapshot.Models[1] = snapshot.Models[1], snapshot.Models[0]
	original := append([]LMArenaModel(nil), snapshot.Models...)
	dto, err := MapLMArena(snapshot)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, []int{dto.Leaderboard[0].Rank, dto.Leaderboard[1].Rank})
	require.Equal(t, original, snapshot.Models)

	*dto.Leaderboard[0].Elo = math.SmallestNonzeroFloat64
	*dto.Leaderboard[0].Votes = 999
	require.Equal(t, 1300.0, snapshot.Models[1].Score)
	snapshotVotes, ok := any(snapshot.Models[1].Votes).(*int64)
	require.True(t, ok, "decoded votes must retain nullable representation")
	require.NotNil(t, snapshotVotes)
	require.Equal(t, int64(10), *snapshotVotes)
}

func TestNewLMArenaFetcherRejectsInvalidConfigWithoutLeakingValues(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP client must not be called by constructor")
		return nil, nil
	})
	cfg := validRadarFetcherTestConfig()
	cfg.Radar.LMArenaURL = "https://user:very-secret@example.test/#fragment"

	fetcher, err := NewLMArenaFetcher(cfg, client)

	require.Error(t, err)
	require.Nil(t, fetcher)
	var configErr *RadarFetcherConfigError
	require.True(t, errors.As(err, &configErr))
	require.NotContains(t, err.Error(), "very-secret")
}

func TestNewLMArenaFetcherRejectsUntrustedEndpointEvenWithProxy(t *testing.T) {
	client := radarDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP client must not be called by constructor")
		return nil, nil
	})
	endpoints := []string{
		"http://datasets-server.huggingface.co/filter",
		"https://127.0.0.1/filter",
		"https://10.0.0.1/filter",
		"https://[::1]/filter",
		"https://user:secret@datasets-server.huggingface.co/filter",
		"https://unknown.example/filter",
		"https://datasets-server.huggingface.co:8443/filter",
		"https://datasets-server.huggingface.co/filter?dataset=attacker/dataset",
	}
	for _, endpoint := range endpoints {
		for _, proxyURL := range []string{"", "http://127.0.0.1:8080"} {
			cfg := validRadarFetcherTestConfig()
			cfg.Radar.LMArenaURL = endpoint
			cfg.Update.ProxyURL = proxyURL
			fetcher, err := NewLMArenaFetcher(cfg, client)
			require.Nil(t, fetcher)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		}
	}
}

type hfLMArenaPageOptions struct {
	partial         bool
	truncated       bool
	fractionalVotes bool
}

func hfLMArenaPage(t *testing.T, offset, total int, options hfLMArenaPageOptions) string {
	t.Helper()
	pageLength := 100
	if remaining := total - offset; remaining < pageLength {
		pageLength = remaining
	}
	if pageLength < 0 {
		pageLength = 0
	}

	rows := make([]map[string]any, 0, pageLength)
	for index := 0; index < pageLength; index++ {
		rank := offset + index + 1
		model := fmt.Sprintf("model-%d", rank)
		vendor := "vendor"
		rating := 1200.0 + float64(total-rank)
		votes := float64(42000 + rank)
		if rank == 20 {
			model = "gpt-5.5"
			vendor = "openai"
			rating = 1474.5
			votes = 42020
		}
		if options.fractionalVotes && rank == 1 {
			votes = 1.5
		}
		truncatedCells := []any{}
		if options.truncated && rank == 1 {
			truncatedCells = []any{"model_name"}
		}
		rows = append(rows, map[string]any{
			"row_idx": rank - 1,
			"row": map[string]any{
				"model_name":               model,
				"organization":             vendor,
				"license":                  "Proprietary",
				"rating":                   rating,
				"rating_lower":             rating - 4.5,
				"rating_upper":             rating + 4.5,
				"variance":                 1.0,
				"vote_count":               votes,
				"rank":                     rank,
				"category":                 "overall",
				"leaderboard_publish_date": "2026-07-10",
			},
			"truncated_cells": truncatedCells,
		})
	}

	features := []map[string]any{
		hfLMArenaFeature("model_name", "string"),
		hfLMArenaFeature("organization", "string"),
		hfLMArenaFeature("license", "string"),
		hfLMArenaFeature("rating", "float64"),
		hfLMArenaFeature("rating_lower", "float64"),
		hfLMArenaFeature("rating_upper", "float64"),
		hfLMArenaFeature("variance", "float64"),
		hfLMArenaFeature("vote_count", "float64"),
		hfLMArenaFeature("rank", "int64"),
		hfLMArenaFeature("category", "string"),
		hfLMArenaFeature("leaderboard_publish_date", "string"),
	}
	payload, err := json.Marshal(map[string]any{
		"features":          features,
		"rows":              rows,
		"num_rows_total":    total,
		"num_rows_per_page": 100,
		"partial":           options.partial,
	})
	require.NoError(t, err)
	return string(payload)
}

func hfLMArenaFeature(name, dtype string) map[string]any {
	return map[string]any{
		"feature_idx": 0,
		"name":        name,
		"type": map[string]any{
			"dtype": dtype,
			"_type": "Value",
		},
	}
}

func mutateHFLMArenaPage(t *testing.T, payload string, mutate func(map[string]any)) string {
	t.Helper()
	var page map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &page))
	mutate(page)
	result, err := json.Marshal(page)
	require.NoError(t, err)
	return string(result)
}

func lmarenaPayloadWithModel(model string) string {
	return `{"meta":{"fetched_at":"2026-07-10T10:00:00Z","last_updated":"Jul 10, 2026","model_count":1},"models":[` + model + `]}`
}

func radarInt64Pointer(value int64) *int64 {
	return &value
}
