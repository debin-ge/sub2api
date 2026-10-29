//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newGrokTaskFixture(t *testing.T) (service.GrokVideoTaskRepository, *service.GrokVideoTask) {
	t.Helper()
	r := NewGrokVideoTaskRepository(integrationDB)
	now := time.Now().UTC()
	key := uuid.NewString()
	task := &service.GrokVideoTask{PublicID: "grok_video_" + fmt.Sprintf("%032x", time.Now().UnixNano()), UserID: 991, APIKeyID: 992, GroupID: 993, AccountID: 994, Operation: "generate", RequestHash: "test-hash", IdempotencyKey: &key, GenerationState: "submitting", SyncStatus: "scheduled", SettlementStatus: "not_due", NextActionAt: &now, Prompt: "prompt", PrivateEnc: "ENCRYPTED-PRIVATE", SnapshotEnc: "ENCRYPTED-BILLING", ResponseEnc: "ENCRYPTED-URL", RequestedModel: "grok-imagine-video-1.5", Resolution: "720p", Duration: 8}
	saved, created, err := r.Create(context.Background(), task)
	require.NoError(t, err)
	require.True(t, created)
	t.Cleanup(func() {
		_, e := integrationDB.Exec(`DELETE FROM grok_video_task_events WHERE task_id=$1`, saved.ID)
		require.NoError(t, e)
		_, e = integrationDB.Exec(`DELETE FROM grok_video_tasks WHERE id=$1`, saved.ID)
		require.NoError(t, e)
	})
	return r, saved
}
func TestGrokVideoPersistenceAndIsolation(t *testing.T) {
	ctx := context.Background()
	r, task := newGrokTaskFixture(t)
	id := uuid.NewString()
	task.ProviderTaskID = &id
	task.GenerationState = "queued"
	require.NoError(t, r.Save(ctx, task, "submitted"))
	got, err := r.FindOwned(ctx, task.UserID, task.APIKeyID, task.GroupID, id)
	require.NoError(t, err)
	require.Equal(t, task.PublicID, got.PublicID)
	for _, owner := range [][3]int64{{0, task.APIKeyID, task.GroupID}, {task.UserID, 0, task.GroupID}, {task.UserID, task.APIKeyID, 0}} {
		_, err = r.FindOwned(ctx, owner[0], owner[1], owner[2], id)
		require.ErrorIs(t, err, service.ErrGrokVideoNotFound)
	}
	copy := *task
	copy.PublicID = "unused"
	same, created, err := r.Create(ctx, &copy)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, task.ID, same.ID)
	copy.RequestHash = "different"
	_, _, err = r.Create(ctx, &copy)
	require.ErrorIs(t, err, service.ErrGrokVideoConflict)
}
func TestGrokVideoLeaseFencing(t *testing.T) {
	ctx := context.Background()
	r, task := newGrokTaskFixture(t)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Claim(ctx, task.PublicID, uuid.NewString(), true)
			if err == nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, winners.Load())
	stale, err := r.Get(ctx, task.PublicID)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE grok_video_tasks SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	fresh, err := r.Claim(ctx, task.PublicID, "new-worker", true)
	require.NoError(t, err)
	stale.GenerationState = "failed"
	require.ErrorIs(t, r.Save(ctx, stale, "stale"), service.ErrGrokVideoBusy)
	fresh.GenerationState = "completed"
	fresh.LeaseOwner = nil
	fresh.LeaseExpiresAt = nil
	require.NoError(t, r.Save(ctx, fresh, "completed"))
	got, err := r.Get(ctx, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, "completed", got.GenerationState)
}
func TestGrokVideoAdminProjection(t *testing.T) {
	ctx := context.Background()
	r, task := newGrokTaskFixture(t)
	id := uuid.NewString()
	task.ProviderTaskID = &id
	task.GenerationState = "in_progress"
	task.Progress = 25
	require.NoError(t, r.Save(ctx, task, "progress"))
	admin := NewVideoAdminRepository(integrationDB)
	page, err := admin.ListVideoTasksAdmin(ctx, service.VideoAdminTaskFilter{Source: "grok_relay", Query: task.PublicID})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Tasks, 1)
	row := page.Tasks[0]
	require.Equal(t, "grok", row.Provider)
	require.Equal(t, "grok_relay", row.Source)
	require.Equal(t, "not_due", row.SettlementStatus)
	require.Contains(t, row.AllowedActions, "get")
	require.Nil(t, row.ProviderAccessEnc)
	require.Nil(t, row.ProviderVideoURLEnc)
	require.Empty(t, row.PriceSnapshot)
	details, err := admin.GetVideoTaskAdmin(ctx, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, task.PublicID, details.PublicID)
	events, err := admin.ListVideoTaskEventsAdmin(ctx, task.PublicID, 1, 20)
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	managed, err := admin.ListVideoTasksAdmin(ctx, service.VideoAdminTaskFilter{Source: "managed", Query: task.PublicID})
	require.NoError(t, err)
	require.Zero(t, managed.Total)
	billing, err := admin.ListVideoTasksAdmin(ctx, service.VideoAdminTaskFilter{BillingState: "none", Query: task.PublicID})
	require.NoError(t, err)
	require.Zero(t, billing.Total)
}

type grokIntegrationCipher struct{}

func (grokIntegrationCipher) Encrypt(value string) (string, error) { return "sealed:" + value, nil }
func (grokIntegrationCipher) Decrypt(value string) (string, error) {
	return strings.TrimPrefix(value, "sealed:"), nil
}

func TestGrokVideoConcurrentSettlementAndCrashReconciliation(t *testing.T) {
	ctx := context.Background()
	_, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	repo := NewGrokVideoTaskRepository(integrationDB)
	id := uuid.NewString()
	stable := service.StableGrokVideoBillingRequestID(id)
	cmd := &service.UsageBillingCommand{RequestID: stable, APIKeyID: key.ID, UserID: user.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, Model: "grok-imagine-video", MediaType: "video", BalanceCost: 1.25, APIKeyQuotaCost: 1.25, ActualCost: 1.25, TotalCost: 1.25}
	log := &service.UsageLog{RequestID: stable, APIKeyID: key.ID, UserID: user.ID, AccountID: account.ID, Model: "grok-imagine-video", ActualCost: 1.25, TotalCost: 1.25, CreatedAt: time.Now()}
	prepared := service.PreparedGrokVideoBilling{Command: cmd, UsageLog: log}
	raw, err := json.Marshal(prepared)
	require.NoError(t, err)
	cipher := grokIntegrationCipher{}
	enc, err := cipher.Encrypt(string(raw))
	require.NoError(t, err)
	task := &service.GrokVideoTask{PublicID: "grok_video_" + strings.ReplaceAll(uuid.NewString(), "-", ""), UserID: user.ID, APIKeyID: key.ID, GroupID: 1, AccountID: account.ID, Operation: "generate", RequestHash: "hash", ProviderTaskID: &id, GenerationState: "completed", SyncStatus: "finished", SettlementStatus: "pending", SnapshotEnc: enc}
	task, _, err = repo.Create(ctx, task)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, e := integrationDB.Exec(`DELETE FROM grok_video_task_events WHERE task_id=$1`, task.ID)
		require.NoError(t, e)
		_, e = integrationDB.Exec(`DELETE FROM grok_video_tasks WHERE id=$1`, task.ID)
		require.NoError(t, e)
	})
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	svc := service.NewGrokVideoTaskService(repo, nil, nil, nil, nil, cipher, billing, nil, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := svc.Observe(ctx, task, true); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 1e-8)
	// Simulate a crash after the durable charge, before marking the task settled.
	_, err = integrationDB.Exec(`UPDATE grok_video_tasks SET settlement_status='pending',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1`, task.ID)
	require.NoError(t, err)
	current, err := repo.Get(ctx, task.PublicID)
	require.NoError(t, err)
	_, err = svc.Observe(ctx, current, true)
	require.NoError(t, err)
	current, err = repo.Get(ctx, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, "settled", current.SettlementStatus)
	require.InDelta(t, 1.25, *current.ActualCost, 1e-8)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, stable, key.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 1e-8)
}
