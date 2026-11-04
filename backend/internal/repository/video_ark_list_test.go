package repository

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestArkVideoListSQLProjectionMatchesPublicStatus(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	states := []string{"preparing", "held", "submitting", "queued", "in_progress", "completed", "failed", "cancelled", "expired", "legacy_unknown"}
	billings := []string{"none", "held", "capture_pending", "captured", "release_pending", "released"}
	for _, generation := range states {
		for _, billing := range billings {
			var got string
			err := db.QueryRow("SELECT "+arkVideoStatusSQL+" FROM (SELECT ? AS generation_state, ? AS billing_state)", generation, billing).Scan(&got)
			require.NoError(t, err)
			require.Equal(t, service.ProjectArkVideoStatus(&service.VideoTask{GenerationState: generation, BillingState: billing}), got, "%s/%s", generation, billing)
		}
	}
	// The expression indexes must cover the very same expression and provider
	// predicate used by the query, or rare/nonmatching statuses can still scan.
	raw, err := migrations.FS.ReadFile("279_ark_video_list_indexes_notx.sql")
	require.NoError(t, err)
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	require.Contains(t, normalize(string(raw)), normalize(arkVideoStatusSQL))
	require.Contains(t, normalize(string(raw)), arkVideoProviderSQL)
}

func TestArkVideoListFiltersBeforeSingleBoundedQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &videoTaskRepository{db: db}
	filter := service.VideoTaskFilter{ArkOnly: true, ArkStatus: "running", Operation: "generate", Model: "model'quoted", Limit: 1, Offset: 49, Order: "desc"}
	query := `SELECT to_jsonb(vt) FROM video_tasks vt WHERE user_id = $1 AND source = 'managed' AND provider <> 'grok' AND ` + arkVideoProviderSQL + ` AND operation = 'generate' AND (` + arkVideoStatusSQL + `) = $2 AND public_model = $3 ORDER BY created_at DESC, id DESC LIMIT $4 OFFSET $5`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(int64(42), "running", filter.Model, 2, 49).
		WillReturnRows(sqlmock.NewRows([]string{"task"}).AddRow(`{"id":2,"public_id":"video_a","user_id":42}`).AddRow(`{"id":1,"public_id":"video_b","user_id":42}`))
	page, err := repo.ListVideoTasksForOwner(context.Background(), 42, filter)
	require.NoError(t, err)
	require.Len(t, page.Data, 1)
	require.True(t, page.HasMore)
	require.Equal(t, int64(42), page.Data[0].UserID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestArkVideoListLimiterSharedAcrossInstancesAndFailsClosed(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	queues := []*videoTaskQueue{{rdb: client}, {rdb: client}}
	var accepted atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 150)
	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := queues[i%2].AllowVideoTaskList(context.Background(), 42)
			if err != nil {
				errs <- err
			}
			if ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, service.ArkVideoListRequestsPerMinute, accepted.Load())
	allowed, err := queues[0].AllowVideoTaskList(context.Background(), 43)
	require.NoError(t, err)
	require.True(t, allowed, "other users have separate limits")
	server.FastForward(time.Minute)
	allowed, err = queues[1].AllowVideoTaskList(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, allowed)
	server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowed, err = queues[0].AllowVideoTaskList(ctx, 42)
	require.Error(t, err)
	require.False(t, allowed)
}

func TestArkVideoListRejectsUnboundedAndMixedFilters(t *testing.T) {
	base := service.VideoTaskFilter{ArkOnly: true, Operation: "generate", Limit: 100, Order: "desc"}
	for i, mutate := range []func(*service.VideoTaskFilter){
		func(f *service.VideoTaskFilter) { f.Offset = 4901 },
		func(f *service.VideoTaskFilter) { f.Offset = -1 },
		func(f *service.VideoTaskFilter) { f.After = "cursor" },
		func(f *service.VideoTaskFilter) { f.Status = "completed" },
		func(f *service.VideoTaskFilter) { f.ArkStatus = "completed" },
		func(f *service.VideoTaskFilter) { f.Operation = "edit" },
		func(f *service.VideoTaskFilter) { f.Limit = 101 },
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			filter := base
			mutate(&filter)
			_, err := (&videoTaskRepository{}).ListVideoTasksForOwner(context.Background(), 42, filter)
			require.ErrorIs(t, err, service.ErrVideoInvalidRequest)
		})
	}
}

func TestArkVideoListMigrationRepairsInterruptedConcurrentIndexes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for i, name := range arkVideoListIndexNames {
		// Alternate invalid and valid indexes: only invalid artifacts should be
		// removed, before IF NOT EXISTS retries the concurrent creation.
		invalid := i%2 == 0
		mock.ExpectQuery("(?s)SELECT EXISTS.*AND NOT i.indisvalid").WithArgs(name).
			WillReturnRows(sqlmock.NewRows([]string{"invalid"}).AddRow(invalid))
		if invalid {
			mock.ExpectExec(regexp.QuoteMeta("DROP INDEX CONCURRENTLY IF EXISTS " + name)).WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	require.NoError(t, prepareNonTransactionalMigration(context.Background(), db, arkVideoListIndexesMigration))
	require.NoError(t, mock.ExpectationsWereMet())
	raw, err := migrations.FS.ReadFile(arkVideoListIndexesMigration)
	require.NoError(t, err)
	nontx, err := validateMigrationExecutionMode(arkVideoListIndexesMigration, string(raw))
	require.NoError(t, err)
	require.True(t, nontx)
	for _, name := range arkVideoListIndexNames {
		require.Contains(t, string(raw), name)
	}
}
