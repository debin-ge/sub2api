package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type radarLMArenaCatalogStub struct {
	models map[string][]string
	err    error
	calls  int
}

func (s *radarLMArenaCatalogStub) ListPublicPassive(context.Context) (map[string][]string, error) {
	s.calls++
	return s.models, s.err
}

type radarLMArenaFetcherStub struct {
	payload []byte
	meta    SourceFetchMeta
	err     error
}

func (s *radarLMArenaFetcherStub) Source() RadarSourceKey  { return RadarSourceLMArena }
func (s *radarLMArenaFetcherStub) Interval() time.Duration { return 24 * time.Hour }
func (s *radarLMArenaFetcherStub) Fetch(context.Context) ([]byte, SourceFetchMeta, error) {
	return append([]byte(nil), s.payload...), s.meta, s.err
}

func TestCatalogMatchedLMArenaFetcherPublishesOnlyBackendIntersection(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	inner := &radarLMArenaFetcherStub{
		payload: []byte(`{
          "meta":{"fetched_at":"2026-07-16T00:00:00Z","last_updated":"Jul 10, 2026","model_count":4},
          "models":[
            {"rank":1,"model":"unmatched","vendor":"V","score":1500,"ci":1,"votes":1},
            {"rank":2,"model":"claude-opus-4-6","vendor":"Anthropic","score":1490,"ci":2,"votes":20},
            {"rank":3,"model":"gpt-5.5","vendor":"OpenAI","score":1480,"ci":3,"votes":30},
            {"rank":4,"model":"gpt-5.5-high","vendor":"OpenAI","score":1470,"ci":4,"votes":40}
          ]
        }`),
		meta: SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now},
	}
	catalog := &radarLMArenaCatalogStub{models: map[string][]string{
		PlatformAnthropic: {"claude-opus-4.6"},
		PlatformOpenAI:    {"gpt-5.5"},
	}}
	fetcher, err := newCatalogMatchedLMArenaFetcher(inner, catalog)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.NoError(t, err)
	require.Equal(t, inner.meta, meta)
	require.Equal(t, 1, catalog.calls)
	snapshot, err := DecodeLMArena(payload)
	require.NoError(t, err)
	dto, err := MapLMArena(snapshot)
	require.NoError(t, err)
	require.Equal(t, []string{"claude-opus-4.6", "gpt-5.5"}, []string{
		dto.Leaderboard[0].Model,
		dto.Leaderboard[1].Model,
	})
	require.Equal(t, []int{2, 3}, []int{dto.Leaderboard[0].Rank, dto.Leaderboard[1].Rank})
	require.NotNil(t, dto.TotalVotes)
	require.Equal(t, int64(50), *dto.TotalVotes)
}

func TestCatalogMatchedLMArenaFetcherFailsClosedWithoutReplacingPayload(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	inner := &radarLMArenaFetcherStub{
		payload: []byte(validLMArenaPayload),
		meta:    SourceFetchMeta{LastAttemptAt: now, LastSuccessAt: &now},
	}
	catalog := &radarLMArenaCatalogStub{err: errors.New("database details must not escape")}
	fetcher, err := newCatalogMatchedLMArenaFetcher(inner, catalog)
	require.NoError(t, err)

	payload, meta, err := fetcher.Fetch(context.Background())

	require.Error(t, err)
	require.Nil(t, payload)
	require.Nil(t, meta.LastSuccessAt)
	require.NotNil(t, meta.Error)
	require.Equal(t, DataSourceErrorCodeUpstreamError, *meta.Error)
	require.NotContains(t, err.Error(), "database details")
}

func TestNewCatalogMatchedLMArenaFetcherRejectsMissingDependencies(t *testing.T) {
	_, err := newCatalogMatchedLMArenaFetcher(nil, &radarLMArenaCatalogStub{})
	require.Error(t, err)
	_, err = newCatalogMatchedLMArenaFetcher(&radarLMArenaFetcherStub{}, nil)
	require.Error(t, err)
}
