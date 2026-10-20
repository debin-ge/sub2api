package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Exercise the actual migration constraints and repository SQL in memory.
// PostgreSQL migration/locking validation remains part of the integration suite.
func creationIntentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	raw, err := migrations.FS.ReadFile("284_gemini_cache_creation_intents.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(raw))
	require.NoError(t, err)
	return db
}

func TestGeminiCacheCreationAdmissionIsAtomicAcrossOwnersAndInstances(t *testing.T) {
	for _, shared := range []string{"user", "account"} {
		t.Run(shared, func(t *testing.T) {
			db := creationIntentTestDB(t)
			const parallel = 16
			start := make(chan struct{})
			results := make(chan error, parallel)
			var wg sync.WaitGroup
			for i := 0; i < parallel; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					intent := &service.GeminiCachedContentCreationIntent{PublicID: fmt.Sprintf("intent%d", i),
						UserID: int64(i + 1), APIKeyID: int64(i + 1), AccountID: int64(i + 1), UpstreamModel: "models/synthetic", TTL: time.Hour}
					if shared == "user" {
						intent.UserID = 1
					} else {
						intent.AccountID = 1
					}
					<-start
					repo := &geminiCachedContentRepository{sql: db}
					results <- repo.BeginCreation(context.Background(), intent)
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			accepted, blocked := 0, 0
			for err := range results {
				if err == nil {
					accepted++
				} else if errors.Is(err, service.ErrGeminiCachedContentCreationPending) {
					blocked++
				} else {
					t.Fatalf("unexpected admission failure: %v", err)
				}
			}
			require.Equal(t, 1, accepted)
			require.Equal(t, parallel-1, blocked)
		})
	}
}

func TestGeminiCacheCreationIntentSurvivesRestartAndElapsedTTL(t *testing.T) {
	db := creationIntentTestDB(t)
	ctx := context.Background()
	intent := &service.GeminiCachedContentCreationIntent{PublicID: "first", UserID: 1, APIKeyID: 7,
		AccountID: 9, UpstreamModel: "models/synthetic", TTL: time.Minute}
	repo := &geminiCachedContentRepository{sql: db}
	require.NoError(t, repo.BeginCreation(ctx, intent))
	_, err := db.Exec(`UPDATE gemini_cache_creation_intents SET created_at = '2000-01-01 00:00:00'`)
	require.NoError(t, err)
	restarted := &geminiCachedContentRepository{sql: db}
	intent.PublicID = "second"
	intent.APIKeyID++
	require.ErrorIs(t, restarted.BeginCreation(ctx, intent), service.ErrGeminiCachedContentCreationPending)
	require.NoError(t, restarted.ResolveCreation(ctx, "unrelated", "request_not_sent"))
	require.ErrorIs(t, restarted.BeginCreation(ctx, intent), service.ErrGeminiCachedContentCreationPending)
	require.Error(t, restarted.ResolveCreation(ctx, "first", ""), "resolution evidence cannot be empty")
	require.NoError(t, restarted.ResolveCreation(ctx, "first", "verified final supplier accounting; synthetic operator reference"))
	require.NoError(t, restarted.BeginCreation(ctx, intent))
	// Resolved rows remain audit evidence, and their identity cannot be replayed.
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM gemini_cache_creation_intents`).Scan(&count))
	require.Equal(t, 2, count)
}
