package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestVideoProviderCostMigrationRepairsInterruptedConcurrentIndexes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	for _, name := range videoProviderCostIndexNames {
		mock.ExpectQuery("(?s)SELECT EXISTS.*AND NOT i.indisvalid").WithArgs(name).
			WillReturnRows(sqlmock.NewRows([]string{"invalid"}).AddRow(true))
		mock.ExpectExec(regexp.QuoteMeta("DROP INDEX CONCURRENTLY IF EXISTS " + name)).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	require.NoError(t, prepareNonTransactionalMigration(context.Background(), db, videoProviderCostIndexesMigration))
	require.NoError(t, mock.ExpectationsWereMet())
	raw, err := migrations.FS.ReadFile(videoProviderCostIndexesMigration)
	require.NoError(t, err)
	nontx, err := validateMigrationExecutionMode(videoProviderCostIndexesMigration, string(raw))
	require.NoError(t, err)
	require.True(t, nontx)
}

func TestVideoProviderCostExposureSharedLimits(t *testing.T) {
	for _, tc := range []struct {
		user, account int
		limited       bool
	}{{2, 19, false}, {3, 1, true}, {0, 20, true}} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT\s+\(SELECT COUNT`).WithArgs(int64(42), int64(11)).WillReturnRows(sqlmock.NewRows([]string{"user", "account"}).AddRow(tc.user, tc.account))
		mock.ExpectRollback()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		err = checkVideoProviderCostExposureTx(context.Background(), tx, 42, 11)
		if tc.limited {
			require.ErrorIs(t, err, service.ErrVideoProviderCostLimited)
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
		db.Close()
	}
}

func TestVideoProviderCostRecordRetainsDeletedAccountAndPropagatesWriteFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	failed := errors.New("ledger unavailable")
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT a.id FROM accounts a JOIN video_tasks vt ON vt.account_id = a.id`).
		WithArgs(int64(7), int64(42), int64(3), "video-test").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO video_provider_costs`).WithArgs(int64(7), service.BalanceSettlementRelease).WillReturnError(failed)
	mock.ExpectRollback()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, lockVideoProviderCostAccountTx(context.Background(), tx, 7, 42, 3, "video-test"))
	require.ErrorIs(t, recordUncertainVideoProviderCostTx(context.Background(), tx, 7, service.BalanceSettlementRelease), failed)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoProviderCostResolutionCASAndIdempotency(t *testing.T) {
	amount := 1.25
	in := service.VideoProviderCostResolution{ExpectedVersion: 3, State: "resolved_cost_recorded", Amount: &amount,
		Currency: "USD", Evidence: "Invoice INV-test line 9 confirms the supplier cost.", ActorID: 99, IdempotencyKey: "resolve-7"}
	for _, scenario := range []string{"resolve", "stale", "replay", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			row := map[string]any{"id": 7, "version": 3, "state": "unresolved", "provider_task_id": "private-provider-handle", "stable_client_token": "private-client-token", "account_identity_version": 2}
			if scenario == "stale" {
				row["version"] = 4
			}
			if scenario == "replay" || scenario == "conflict" {
				row["version"], row["state"], row["amount"], row["currency"] = 4, in.State, amount, in.Currency
				row["evidence"], row["resolved_by"], row["resolution_key"] = in.Evidence, in.ActorID, in.IdempotencyKey
				if scenario == "conflict" {
					row["amount"] = 2.0
				}
			}
			body, err := json.Marshal(row)
			require.NoError(t, err)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT to_jsonb\(c\).*FOR UPDATE`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"json"}).AddRow(body))
			if scenario == "resolve" {
				row["version"], row["state"], row["amount"], row["currency"] = 4, in.State, amount, in.Currency
				row["evidence"], row["resolved_by"], row["resolution_key"] = in.Evidence, in.ActorID, in.IdempotencyKey
				body, _ = json.Marshal(row)
				mock.ExpectQuery(`UPDATE video_provider_costs c SET state`).WithArgs(int64(7), in.ExpectedVersion, in.State, amount, in.Currency, in.Evidence, in.ActorID, in.IdempotencyKey).
					WillReturnRows(sqlmock.NewRows([]string{"json"}).AddRow(body))
			}
			if scenario == "stale" || scenario == "conflict" {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			result, err := (&videoTaskRepository{db: db}).ResolveVideoProviderCost(context.Background(), 7, in)
			if scenario == "stale" || scenario == "conflict" {
				require.ErrorIs(t, err, service.ErrVideoProviderCostConflict)
			} else {
				require.NoError(t, err)
				require.Equal(t, in.State, result.State)
				require.Equal(t, "private-provider-handle", result.ProviderTaskID)
				public, err := json.Marshal(result)
				require.NoError(t, err)
				require.NotContains(t, string(public), "private-provider-handle")
				require.NotContains(t, string(public), "private-client-token")
			}
			require.NoError(t, mock.ExpectationsWereMet(), "the resolution transaction may only update the supplier ledger")
		})
	}
}

func TestVideoProviderCostObservationCannotOverwriteAdminResolution(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	item := &service.VideoProviderCost{ID: 7, Version: 3}
	mock.ExpectExec(`UPDATE video_provider_costs SET observation_status`).WithArgs(int64(7), int64(3), "old-worker", "completed", "", nil, "provider-task").
		WillReturnResult(sqlmock.NewResult(0, 0))
	err = (&videoTaskRepository{db: db}).RecordVideoProviderCostObservation(context.Background(), item, "old-worker", "provider-task", "completed", "", nil)
	require.ErrorIs(t, err, service.ErrVideoProviderCostConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}
