//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func legacyGrokReceiptFixture(t *testing.T, request service.VideoSubmitRequest, accountID int64, rawKey string) *service.GrokVideoTask {
	t.Helper()
	id := uuid.NewString()
	task := &service.GrokVideoTask{PublicID: "grok_video_" + strings.ReplaceAll(uuid.NewString(), "-", ""), UserID: request.APIKey.UserID, APIKeyID: request.APIKey.ID, GroupID: *request.APIKey.GroupID, AccountID: accountID, Operation: request.Operation, RequestHash: "legacy-hash", IdempotencyKey: &rawKey, ProviderTaskID: &id, GenerationState: "queued", SyncStatus: "scheduled", SettlementStatus: "not_due"}
	t.Cleanup(func() {
		_, e := integrationDB.Exec(`DELETE FROM grok_video_task_events WHERE task_id IN(SELECT id FROM grok_video_tasks WHERE public_id=$1)`, task.PublicID)
		require.NoError(t, e)
		_, e = integrationDB.Exec(`DELETE FROM grok_video_tasks WHERE public_id=$1`, task.PublicID)
		require.NoError(t, e)
	})
	return task
}
func TestGrokCrossEngineCreationReceiptIsAtomic(t *testing.T) {
	for _, first := range []string{"legacy", "managed", "concurrent"} {
		t.Run(first, func(t *testing.T) {
			ctx := context.Background()
			svc, _, _, upstream, user, request, _ := nativeGrokFixture(t)
			rawKey := "cross-engine-" + uuid.NewString()
			request.IdempotencyKey = service.GrokManagedIdempotencyKey(request.APIKey, request.Operation, rawKey)
			var accountID int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT account_id FROM account_groups WHERE group_id=$1`, *request.APIKey.GroupID).Scan(&accountID))
			legacy := legacyGrokReceiptFixture(t, request, accountID, rawKey)
			repo := NewGrokVideoTaskRepository(integrationDB)
			var legacyErr, nativeErr error
			oldCreate := func() { _, _, legacyErr = repo.Create(ctx, legacy) }
			newCreate := func() { _, nativeErr = svc.Submit(ctx, request) }
			switch first {
			case "legacy":
				oldCreate()
				newCreate()
			case "managed":
				newCreate()
				oldCreate()
			default:
				var wg sync.WaitGroup
				wg.Add(2)
				start := make(chan struct{})
				go func() { defer wg.Done(); <-start; oldCreate() }()
				go func() { defer wg.Done(); <-start; newCreate() }()
				close(start)
				wg.Wait()
			}
			require.NotEqual(t, legacyErr == nil, nativeErr == nil, "exactly one engine may own a creation")
			if nativeErr == nil {
				require.ErrorIs(t, legacyErr, service.ErrGrokVideoConflict)
				require.Equal(t, int64(1), upstream.created.Load())
				assertVideoBudgetTotals(t, user.ID, 1, 9.2, 0.8)
			} else {
				require.True(t, errors.Is(nativeErr, service.ErrVideoIdempotencyConflict), nativeErr)
				require.NoError(t, legacyErr)
				require.Zero(t, upstream.created.Load())
				assertVideoBudgetTotals(t, user.ID, 0, 10, 0)
			}
			var count int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM grok_video_creation_receipts WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, user.ID, request.Operation, request.IdempotencyKey).Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}
func TestGrokReceiptMigrationBackfillsExistingTasks(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	schema := "grok_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL search_path TO `+schema+`,public`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `CREATE TABLE grok_video_tasks (user_id bigint, api_key_id bigint, operation text, idempotency_key text, public_id text, created_at timestamptz DEFAULT NOW(),account_id bigint,generation_state text,data jsonb);
 CREATE TABLE video_tasks (user_id bigint,operation text,idempotency_key text,public_id text,provider text,source text,created_at timestamptz DEFAULT NOW());`)
	require.NoError(t, err)
	rawKey := "客户:exact-key"
	expected := service.GrokManagedIdempotencyKey(&service.APIKey{ID: 9, UserID: 7}, service.VideoOperationGenerate, rawKey)
	_, err = tx.ExecContext(ctx, `INSERT INTO grok_video_tasks VALUES(7,9,'generate',$1,'grok_video_existing',NOW(),3,'queued','{"private_enc":"opaque"}')`, rawKey)
	require.NoError(t, err)
	script, err := migrations.FS.ReadFile("286_grok_video_creation_receipts.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(script))
	require.NoError(t, err)
	var engine, publicID, private string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT engine,task_public_id FROM grok_video_creation_receipts WHERE user_id=7 AND operation='generate' AND idempotency_key=$1`, expected).Scan(&engine, &publicID))
	require.Equal(t, "legacy", engine)
	require.Equal(t, "grok_video_existing", publicID)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT data->>'private_enc' FROM grok_video_tasks WHERE public_id=$1`, publicID).Scan(&private))
	require.Equal(t, "opaque", private)
	_, err = tx.ExecContext(ctx, string(script))
	require.NoError(t, err, "migration must be replay-safe")
	_, err = tx.ExecContext(ctx, `INSERT INTO video_tasks(user_id,operation,idempotency_key,public_id,provider,source) VALUES(7,'generate',$1,'video_new','grok','managed')`, expected)
	require.True(t, isGrokCreationReceiptConflict(err), fmt.Sprint(err))
}
