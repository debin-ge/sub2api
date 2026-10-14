package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type flovaExecutionStateMatcher struct{}

func (flovaExecutionStateMatcher) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var state service.VideoProviderExecutionState
	return json.Unmarshal([]byte(raw), &state) == nil && state.ResourceID == "res1" &&
		state.ProjectID == "p1" && state.StreamChatID == "s1" && state.CostUncertain &&
		state.SpendingPolicy == service.VideoSpendingBlockedUnverified
}

func TestFlovaControlTransitionPersistsStateAndBlockedActionAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	oldID := "p1:s1|m=seedance-2.5;r=720p;s=4;t=1791373800"
	newID := oldID // resource is private state; provider identity must stay stable
	state := &service.VideoProviderExecutionState{Version: 1, Provider: service.VideoProviderFlova,
		ProjectID: "p1", StreamChatID: "s1", ResourceID: "res1", SpendingPolicy: service.VideoSpendingBlockedUnverified, CostUncertain: true}
	row := videoTaskRow{ID: 1, PublicID: "task", Provider: service.VideoProviderFlova, ProviderTaskID: &oldID,
		GenerationState: service.VideoGenerationInProgress, BillingState: service.VideoBillingHeld,
		DeleteState: service.VideoDeleteNone, Version: 3}
	before, err := json.Marshal(row)
	require.NoError(t, err)
	row.ProviderTaskID, row.ProviderExecutionState, row.Version = &newID, state, 4
	after, err := json.Marshal(row)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT to_jsonb\(vt\).*FOR UPDATE`).WithArgs("task").WillReturnRows(sqlmock.NewRows([]string{"task"}).AddRow(before))
	args := make([]driver.Value, 28)
	for i := range args {
		args[i] = sqlmock.AnyArg()
	}
	args[0], args[21], args[26], args[27] = int64(1), int64(3), newID, flovaExecutionStateMatcher{}
	mock.ExpectExec(`(?s)UPDATE video_tasks.*provider_task_id = COALESCE\(\$27, provider_task_id\).*provider_execution_state = COALESCE\(\$28::jsonb, provider_execution_state\).*WHERE id = \$1 AND version = \$22`).
		WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
	key := strings.Repeat("a", 64)
	mock.ExpectExec(`(?s)INSERT INTO video_provider_blocked_actions.*ON CONFLICT \(task_id, action_key\) DO UPDATE`).
		WithArgs(int64(1), key, "batch_task_limit", service.VideoSpendingBlockedUnverified, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`INSERT INTO video_task_events`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10))
	mock.ExpectQuery(`SELECT to_jsonb\(vt\).*WHERE id = \$1`).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"task"}).AddRow(after))
	mock.ExpectCommit()
	repo := &videoTaskRepository{db: db}
	ctx := service.WithVideoTaskWriteGuard(context.Background(), 1, 3)
	updated, err := repo.TransitionVideoTask(ctx, "task", service.VideoTaskTransition{
		GenerationState: service.VideoGenerationInProgress, ProviderTaskID: &newID, ProviderExecutionState: state,
		BlockedProviderActions: []service.VideoProviderBlockedAction{{ActionKey: key, ActionType: "batch_task_limit", Reason: service.VideoSpendingBlockedUnverified}},
	})
	require.NoError(t, err)
	require.Equal(t, newID, *updated.ProviderTaskID)
	require.Equal(t, "res1", updated.ProviderExecutionState.ResourceID)
	require.Equal(t, int64(4), updated.Version)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFlovaControlStaleVersionCannotWriteStateOrActions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	id := "p1:s1|m=seedance-2.5;r=720p;s=4;t=1791373800"
	raw, err := json.Marshal(videoTaskRow{ID: 1, Provider: service.VideoProviderFlova, ProviderTaskID: &id, Version: 4})
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT to_jsonb\(vt\).*FOR UPDATE`).WithArgs("task").WillReturnRows(sqlmock.NewRows([]string{"task"}).AddRow(raw))
	mock.ExpectRollback()
	repo := &videoTaskRepository{db: db}
	_, err = repo.TransitionVideoTask(service.WithVideoTaskWriteGuard(context.Background(), 1, 3), "task", service.VideoTaskTransition{ProviderTaskID: &id})
	require.ErrorIs(t, err, service.ErrVideoVersionConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}
