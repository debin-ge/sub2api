//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func newUsageBillingOutboxTestPayload(t *testing.T) (*service.UsageBillingCommand, *service.UsageLog, []byte, []byte) {
	t.Helper()
	createdAt := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	cmd := &service.UsageBillingCommand{
		RequestID:          "req-durable-1",
		APIKeyID:           7,
		UserID:             42,
		AccountID:          11,
		AccountType:        service.AccountTypeAPIKey,
		Model:              "priced-model",
		InputTokens:        10,
		OutputTokens:       2,
		BalanceCost:        1.25,
		ActualCost:         1.25,
		TotalCost:          1.25,
		OccurredAt:         createdAt,
		RequestPayloadHash: "payload-hash",
	}
	usageLog := &service.UsageLog{
		UserID:       42,
		APIKeyID:     7,
		AccountID:    11,
		RequestID:    cmd.RequestID,
		Model:        cmd.Model,
		InputTokens:  10,
		OutputTokens: 2,
		InputCost:    1,
		OutputCost:   0.25,
		TotalCost:    1.25,
		ActualCost:   1.25,
		CreatedAt:    createdAt,
	}
	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)
	return cmd, usageLog, commandJSON, usageLogJSON
}

func expectClaimedOutboxRow(
	mock sqlmock.Sqlmock,
	cmd *service.UsageBillingCommand,
	commandJSON []byte,
	usageLogJSON []byte,
	createdAt time.Time,
) {
	mock.ExpectQuery(`(?s)SELECT attempts, created_at, request_id, api_key_id, request_fingerprint,.*FROM usage_billing_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{
			"attempts", "created_at", "request_id", "api_key_id", "request_fingerprint",
			"payload_version", "stage", "command_payload", "usage_log_payload", "result_payload",
		}).AddRow(
			0, createdAt, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint,
			usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
			commandJSON, usageLogJSON, nil,
		))
}

func expectUsageBillingEnqueueFingerprintChecks(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup\s+WHERE`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup_archive\s+WHERE`).
		WillReturnError(sql.ErrNoRows)
}

func expectNoExistingUsageLog(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`(?s)SELECT to_jsonb\(ul\).*FROM usage_logs AS ul`).
		WillReturnError(sql.ErrNoRows)
}

func expectExistingUsageLog(t *testing.T, mock sqlmock.Sqlmock, usageLog *service.UsageLog) {
	t.Helper()
	payload, err := json.Marshal(usageLogToPayloadV1(usageLog))
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)SELECT to_jsonb\(ul\).*FROM usage_logs AS ul`).
		WillReturnRows(sqlmock.NewRows([]string{"to_jsonb"}).AddRow(payload))
}

func expectNewUsageBillingClaim(mock sqlmock.Sqlmock, cmd *service.UsageBillingCommand) {
	mock.ExpectQuery(`(?s)INSERT INTO usage_billing_dedup`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
	mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).
		WillReturnError(sql.ErrNoRows)
}

func TestUsageBillingApplyAndRecord_BillingFailureLeavesDurableIntent(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	createdAt := usageLog.CreatedAt

	// Phase one is committed independently: after this point the billing intent
	// survives both the caller returning and a process restart.
	mock.ExpectBegin()
	expectUsageBillingEnqueueFingerprintChecks(mock)
	mock.ExpectQuery(`(?s)INSERT INTO usage_billing_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "attempts", "created_at", "payload_version", "stage",
			"command_payload", "usage_log_payload", "result_payload",
		}).AddRow(
			9, 0, createdAt, usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
			commandJSON, usageLogJSON, nil,
		))
	mock.ExpectCommit()

	// Phase two fails inside the billing transaction.
	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, createdAt)
	expectNoExistingUsageLog(mock)
	expectNewUsageBillingClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WillReturnError(errors.New("injected balance write failure"))
	mock.ExpectRollback()

	// The claim is released for the background worker; no DELETE is issued.
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET attempts = attempts \+ 1`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := &usageBillingRepository{db: db}
	result, err := repo.ApplyAndRecord(ctx, cmd, usageLog)
	require.ErrorContains(t, err, "injected balance write failure")
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingOutboxComplete_UsageLogFailureRollsBackDeduction(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	createdAt := usageLog.CreatedAt
	event := service.UsageBillingOutboxEvent{ID: 9, Command: cmd, UsageLog: usageLog}

	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, createdAt)
	expectNoExistingUsageLog(mock)
	expectNewUsageBillingClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(8.75))
	mock.ExpectQuery(`(?s)INSERT INTO usage_logs`).
		WillReturnError(errors.New("injected usage log failure"))
	mock.ExpectRollback()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(ctx, "worker-1", event)
	require.ErrorContains(t, err, "injected usage log failure")
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingOutboxComplete_LogConflictIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	createdAt := usageLog.CreatedAt
	event := service.UsageBillingOutboxEvent{ID: 9, Command: cmd, UsageLog: usageLog}

	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, createdAt)
	expectExistingUsageLog(t, mock, usageLog)
	mock.ExpectQuery(`(?s)INSERT INTO usage_billing_dedup`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup`).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))

	// A previous ambiguous commit already inserted the log. The retry observes
	// both idempotency keys and simply acknowledges the durable intent.
	mock.ExpectQuery(`(?s)INSERT INTO usage_logs`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT id, created_at FROM usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(55, createdAt))
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET stage = \$3`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(ctx, "worker-1", event)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingOutboxClaim_UsesLeaseAndSkipLocked(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	mock.ExpectBegin()
	// A v1 worker must not claim a future-version payload during a rolling
	// upgrade and quarantine data that only the newer worker understands.
	mock.ExpectQuery(`(?s)payload_version = \$4.*FOR UPDATE SKIP LOCKED.*UPDATE usage_billing_outbox`).
		WithArgs("worker-a", 5, int64(120), usageBillingOutboxPayloadVersion).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "attempts", "created_at", "request_id", "api_key_id",
			"request_fingerprint", "payload_version", "stage",
			"command_payload", "usage_log_payload", "result_payload",
		}).AddRow(
			9, 3, usageLog.CreatedAt, cmd.RequestID, cmd.APIKeyID,
			cmd.RequestFingerprint, usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
			commandJSON, usageLogJSON, nil,
		))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	events, err := repo.ClaimUsageBillingOutbox(ctx, "worker-a", 5, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(9), events[0].ID)
	require.Equal(t, 3, events[0].Attempts)
	require.Equal(t, cmd.RequestFingerprint, events[0].Command.RequestFingerprint)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBoundedUsageBillingRepositoryError_IsPostgresSafeUTF8(t *testing.T) {
	message := strings.Repeat("界", 341) + "\xff\x00tail"

	got := boundedUsageBillingRepositoryError(errors.New(message))

	require.LessOrEqual(t, len(got), 1024)
	require.True(t, utf8.ValidString(got))
	require.NotContains(t, got, "\x00")
	require.Equal(t, "界", string([]rune(got)[len([]rune(got))-1]))
}

func TestUsageBillingOutboxComplete_LegacyCompatibleLogBackfillsDedupWithoutCharging(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	event := service.UsageBillingOutboxEvent{ID: 19, Command: cmd, UsageLog: usageLog}

	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, usageLog.CreatedAt)
	expectExistingUsageLog(t, mock, usageLog)
	expectNewUsageBillingClaim(mock, cmd)
	// The legacy log proves this request was already finalized. In particular,
	// there must be no users balance UPDATE even though the dedup row is absent.
	mock.ExpectQuery(`(?s)INSERT INTO usage_logs`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT id, created_at FROM usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(88, usageLog.CreatedAt))
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET stage = \$3`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(ctx, "worker-legacy", event)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.NotNil(t, result.OutboxReceipt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingOutboxComplete_LegacyDedupWithoutLogPersistsProjectionRepairAcrossRestart(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	event := service.UsageBillingOutboxEvent{ID: 20, Command: cmd, UsageLog: usageLog}

	// The pre-outbox process committed its billing dedup/effects, but crashed
	// before the usage log and cache post-effects. Recovery must not charge
	// again, and it must retain a durable projection-repair marker.
	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, usageLog.CreatedAt)
	expectNoExistingUsageLog(mock)
	mock.ExpectQuery(`(?s)INSERT INTO usage_billing_dedup`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup`).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
	mock.ExpectQuery(`(?s)INSERT INTO usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(89, usageLog.CreatedAt))

	expectedPayload, err := json.Marshal(usageBillingResultToPayloadV1(
		&service.UsageBillingApplyResult{
			Applied:                  false,
			UsageLogRecorded:         true,
			ProjectionRepairRequired: true,
		},
	))
	require.NoError(t, err)
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET stage = \$3`).
		WithArgs(
			event.ID,
			"worker-legacy-crash",
			usageBillingOutboxStageEffects,
			string(expectedPayload),
			usageBillingOutboxStageBilling,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(ctx, "worker-legacy-crash", event)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.True(t, result.ProjectionRepairRequired)

	// A restarted worker reads stage 1 from PostgreSQL. The repair marker must
	// survive result_payload serialization so only idempotent projections run.
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT attempts, created_at, request_id, api_key_id, request_fingerprint,.*FROM usage_billing_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{
			"attempts", "created_at", "request_id", "api_key_id", "request_fingerprint",
			"payload_version", "stage", "command_payload", "usage_log_payload", "result_payload",
		}).AddRow(
			1, usageLog.CreatedAt, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint,
			usageBillingOutboxPayloadVersion, usageBillingOutboxStageEffects,
			commandJSON, usageLogJSON, expectedPayload,
		))
	mock.ExpectCommit()

	replayed, err := repo.CompleteUsageBillingOutbox(
		ctx,
		"worker-after-restart",
		service.UsageBillingOutboxEvent{ID: event.ID, Stage: usageBillingOutboxStageEffects},
	)
	require.NoError(t, err)
	require.False(t, replayed.Applied)
	require.True(t, replayed.UsageLogRecorded)
	require.True(t, replayed.ProjectionRepairRequired)
	require.Equal(t, event.ID, replayed.OutboxReceipt.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingOutboxComplete_LegacyConflictingLogIsPermanentConflict(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	conflicting := *usageLog
	conflicting.TotalCost = usageLog.TotalCost + 9
	event := service.UsageBillingOutboxEvent{ID: 20, Command: cmd, UsageLog: usageLog}

	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, usageLog.CreatedAt)
	expectExistingUsageLog(t, mock, &conflicting)
	mock.ExpectRollback()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(ctx, "worker-conflict", event)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogPayloadsBillingEquivalent_UsesPostgresNumericPrecision(t *testing.T) {
	intended := usageLogPayloadV1{
		RequestID:             "req-numeric-rounding",
		APIKeyID:              7,
		RateMultiplier:        1.2345 * 1.2345,
		AccountRateMultiplier: float64Ptr(1.00004),
		InputCost:             0.12345678904,
		AccountStatsCost:      float64Ptr(0.98765432104),
	}
	// Values read back from usage_logs have already been coerced to
	// NUMERIC(10,4) or NUMERIC(20,10).
	existing := intended
	existing.RateMultiplier = 1.5240
	existing.AccountRateMultiplier = float64Ptr(1.0000)
	existing.InputCost = 0.1234567890
	existing.AccountStatsCost = float64Ptr(0.9876543210)

	require.True(t, usageLogPayloadsBillingEquivalent(existing, intended))

	differentRate := existing
	differentRate.RateMultiplier = 1.5241
	require.False(t, usageLogPayloadsBillingEquivalent(differentRate, intended))

	differentCost := existing
	differentCost.InputCost = 0.1234567891
	require.False(t, usageLogPayloadsBillingEquivalent(differentCost, intended))
}

func TestUsageLogPayloadsBillingEquivalent_NormalizesLegacyRequestType(t *testing.T) {
	intended := usageLogPayloadV1{
		RequestID:   "req-legacy-request-type",
		APIKeyID:    7,
		RequestType: service.RequestTypeStream,
		Stream:      true,
	}
	existing := intended
	existing.RequestType = service.RequestTypeUnknown

	require.True(t, usageLogPayloadsBillingEquivalent(existing, intended))

	conflicting := existing
	conflicting.Stream = false
	conflicting.OpenAIWSMode = true
	require.False(t, usageLogPayloadsBillingEquivalent(conflicting, intended))
}

func TestUsageBillingOutboxComplete_EffectsStageReplaysReceiptWithoutCharging(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	storedResult := &service.UsageBillingApplyResult{
		Applied:          true,
		UsageLogRecorded: true,
		NewBalance:       float64Ptr(8.75),
	}
	resultJSON, err := json.Marshal(usageBillingResultToPayloadV1(storedResult))
	require.NoError(t, err)
	event := service.UsageBillingOutboxEvent{ID: 21, Stage: usageBillingOutboxStageEffects}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT attempts, created_at, request_id, api_key_id, request_fingerprint,.*FROM usage_billing_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{
			"attempts", "created_at", "request_id", "api_key_id", "request_fingerprint",
			"payload_version", "stage", "command_payload", "usage_log_payload", "result_payload",
		}).AddRow(
			1, usageLog.CreatedAt, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint,
			usageBillingOutboxPayloadVersion, usageBillingOutboxStageEffects,
			commandJSON, usageLogJSON, resultJSON,
		))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(ctx, "worker-replay", event)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.NotNil(t, result.NewBalance)
	require.InDelta(t, 8.75, *result.NewBalance, 1e-9)
	require.Equal(t, int64(21), result.OutboxReceipt.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func float64Ptr(value float64) *float64 {
	return &value
}

func TestUsageBillingApplyAndRecord_ExistingDifferentDedupRejectsBeforeEnqueue(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup\s+WHERE`).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		))
	mock.ExpectRollback()

	repo := &usageBillingRepository{db: db}
	result, err := repo.ApplyAndRecord(ctx, cmd, usageLog)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingOutboxClaim_QuarantinesPoisonAndReturnsValidRows(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, commandJSON, usageLogJSON := newUsageBillingOutboxTestPayload(t)
	var identityMismatchPayload usageBillingCommandPayloadV1
	require.NoError(t, json.Unmarshal(commandJSON, &identityMismatchPayload))
	identityMismatchPayload.RequestID = cmd.RequestID + "-tampered"
	identityMismatchJSON, err := json.Marshal(identityMismatchPayload)
	require.NoError(t, err)
	validCmd := *cmd
	validCmd.RequestID = cmd.RequestID + "-valid"
	validCmd.RequestFingerprint = ""
	validCmd.Normalize()
	validLog := *usageLog
	validLog.RequestID = validCmd.RequestID
	validCommandJSON, validUsageLogJSON, err := marshalUsageBillingOutboxPayload(&validCmd, &validLog)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FOR UPDATE SKIP LOCKED.*UPDATE usage_billing_outbox`).
		WithArgs("worker-poison", 5, int64(120), usageBillingOutboxPayloadVersion).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "attempts", "created_at", "request_id", "api_key_id",
			"request_fingerprint", "payload_version", "stage",
			"command_payload", "usage_log_payload", "result_payload",
		}).
			AddRow(
				30, 0, usageLog.CreatedAt, cmd.RequestID, cmd.APIKeyID,
				cmd.RequestFingerprint, usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
				[]byte(`{"broken"`), usageLogJSON, nil,
			).
			AddRow(
				31, 0, validLog.CreatedAt, validCmd.RequestID, validCmd.APIKeyID,
				validCmd.RequestFingerprint, usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
				identityMismatchJSON, usageLogJSON, nil,
			).
			AddRow(
				32, 0, validLog.CreatedAt, validCmd.RequestID, validCmd.APIKeyID,
				validCmd.RequestFingerprint, usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
				validCommandJSON, validUsageLogJSON, nil,
			))
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET terminal_at = NOW\(\)`).
		WithArgs(int64(30), "worker-poison", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET terminal_at = NOW\(\)`).
		WithArgs(int64(31), "worker-poison", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	events, err := repo.ClaimUsageBillingOutbox(ctx, "worker-poison", 5, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(32), events[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestValidateUsageBillingOutboxPayload_ZeroActualCostCannotCarryPositiveAllocation(t *testing.T) {
	t.Run("balance billing", func(t *testing.T) {
		cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
		cmd.ActualCost = 0
		cmd.TotalCost = 0
		cmd.BalanceCost = 1
		usageLog.ActualCost = 0
		usageLog.TotalCost = 0

		err := validateUsageBillingOutboxPayload(cmd, usageLog)

		require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
		require.ErrorContains(t, err, "invalid balance billing allocation")
	})

	t.Run("subscription billing", func(t *testing.T) {
		cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
		subscriptionID := int64(91)
		groupID := int64(12)
		cmd.ActualCost = 0
		cmd.TotalCost = 0
		cmd.BalanceCost = 0
		cmd.SubscriptionCost = 1
		cmd.SubscriptionID = &subscriptionID
		cmd.GroupID = &groupID
		cmd.BillingType = service.BillingTypeSubscription
		cmd.IsSubscriptionBilling = true
		usageLog.ActualCost = 0
		usageLog.TotalCost = 0
		usageLog.SubscriptionID = &subscriptionID
		usageLog.GroupID = &groupID
		usageLog.BillingType = service.BillingTypeSubscription

		err := validateUsageBillingOutboxPayload(cmd, usageLog)

		require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
		require.ErrorContains(t, err, "invalid subscription billing allocation")
	})
}

func TestUsageBillingOutboxComplete_LongNonBillingMetadataIsSanitizedAndStillCharged(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
	longASCII := strings.Repeat("x", 800)
	longUTF8 := strings.Repeat("界", 300)
	usageLog.UserAgent = &longUTF8
	usageLog.ServiceTier = &longASCII
	usageLog.ReasoningEffort = &longASCII
	usageLog.InboundEndpoint = &longASCII
	usageLog.UpstreamEndpoint = &longASCII
	usageLog.SessionID = &longUTF8
	usageLog.IPAddress = &longASCII
	usageLog.ModelMappingChain = &longUTF8

	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)
	decoded, err := decodeUsageBillingOutboxEvent(
		49,
		0,
		usageLog.CreatedAt,
		cmd.RequestID,
		cmd.APIKeyID,
		cmd.RequestFingerprint,
		usageBillingOutboxPayloadVersion,
		usageBillingOutboxStageBilling,
		commandJSON,
		usageLogJSON,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, decoded.UsageLog.UserAgent)
	require.LessOrEqual(t, len(*decoded.UsageLog.UserAgent), 512)
	require.True(t, utf8.ValidString(*decoded.UsageLog.UserAgent))
	require.LessOrEqual(t, len(*decoded.UsageLog.ServiceTier), 16)
	require.LessOrEqual(t, len(*decoded.UsageLog.ReasoningEffort), 20)
	require.LessOrEqual(t, len(*decoded.UsageLog.InboundEndpoint), 128)
	require.LessOrEqual(t, len(*decoded.UsageLog.UpstreamEndpoint), 128)
	require.LessOrEqual(t, len(*decoded.UsageLog.SessionID), 255)
	require.LessOrEqual(t, len(*decoded.UsageLog.IPAddress), 45)
	require.LessOrEqual(t, len(*decoded.UsageLog.ModelMappingChain), 500)

	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, usageLog.CreatedAt)
	expectNoExistingUsageLog(mock)
	expectNewUsageBillingClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(8.75))
	mock.ExpectQuery(`(?s)INSERT INTO usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(99, usageLog.CreatedAt))
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET stage = \$3`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(
		ctx,
		"worker-long-metadata",
		service.UsageBillingOutboxEvent{ID: 49},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingOutboxComplete_PaddedModelUsesCanonicalValueAndStillCharges(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
	paddedModel := strings.Repeat(" ", 120) + "priced-model" + strings.Repeat(" ", 120)
	require.Greater(t, len(paddedModel), 100)
	cmd.Model = paddedModel
	usageLog.Model = paddedModel
	usageLog.RequestedModel = "  priced-model  "

	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)
	require.Equal(t, "priced-model", cmd.Model)
	require.Equal(t, "priced-model", usageLog.Model)
	require.Equal(t, "priced-model", usageLog.RequestedModel)

	decoded, err := decodeUsageBillingOutboxEvent(
		50,
		0,
		usageLog.CreatedAt,
		cmd.RequestID,
		cmd.APIKeyID,
		cmd.RequestFingerprint,
		usageBillingOutboxPayloadVersion,
		usageBillingOutboxStageBilling,
		commandJSON,
		usageLogJSON,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, "priced-model", decoded.Command.Model)
	require.Equal(t, "priced-model", decoded.UsageLog.Model)
	require.NoError(t, validateUsageBillingOutboxPayload(decoded.Command, decoded.UsageLog))

	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, usageLog.CreatedAt)
	expectNoExistingUsageLog(mock)
	expectNewUsageBillingClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(8.75))
	insertArgs := make([]driver.Value, len(usageLogInsertArgTypes))
	for i := range insertArgs {
		insertArgs[i] = sqlmock.AnyArg()
	}
	// usage_logs.model is the fifth insert argument. Assert that the exact
	// canonical value—not the >100-byte attacker-controlled input—is written.
	insertArgs[4] = "priced-model"
	mock.ExpectQuery(`(?s)INSERT INTO usage_logs`).
		WithArgs(insertArgs...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(100, usageLog.CreatedAt))
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET stage = \$3`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	result, err := repo.CompleteUsageBillingOutbox(
		ctx,
		"worker-padded-model",
		service.UsageBillingOutboxEvent{ID: 50},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.True(t, result.UsageLogRecorded)
	require.Equal(t, int64(50), result.OutboxReceipt.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarshalUsageBillingOutboxPayload_BoundsLongIdentitiesWithoutCollisions(t *testing.T) {
	cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
	longRequestID := strings.Repeat("请求", 200)
	longModelA := strings.Repeat("模型", 40) + "-a"
	longModelB := strings.Repeat("模型", 40) + "-b"
	cmd.RequestID = longRequestID
	usageLog.RequestID = longRequestID
	cmd.Model = longModelA
	usageLog.Model = longModelA

	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)
	require.NotEmpty(t, cmd.RequestID)
	require.LessOrEqual(t, len(cmd.RequestID), 255)
	require.True(t, utf8.ValidString(cmd.RequestID))
	require.Equal(t, cmd.RequestID, usageLog.RequestID)
	require.Contains(t, cmd.RequestID, "#sha256:")
	require.LessOrEqual(t, len(cmd.Model), 100)
	require.True(t, utf8.ValidString(cmd.Model))
	require.Equal(t, cmd.Model, usageLog.Model)
	require.Contains(t, cmd.Model, "#sha256:")

	decoded, err := decodeUsageBillingOutboxEvent(
		51,
		0,
		usageLog.CreatedAt,
		cmd.RequestID,
		cmd.APIKeyID,
		cmd.RequestFingerprint,
		usageBillingOutboxPayloadVersion,
		usageBillingOutboxStageBilling,
		commandJSON,
		usageLogJSON,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, cmd.RequestID, decoded.Command.RequestID)
	require.Equal(t, cmd.RequestID, decoded.UsageLog.RequestID)
	require.Equal(t, cmd.Model, decoded.Command.Model)
	require.Equal(t, cmd.Model, decoded.UsageLog.Model)
	require.NoError(t, validateUsageBillingOutboxPayload(decoded.Command, decoded.UsageLog))

	require.NotEqual(
		t,
		canonicalizeUsageBillingIdentity(longModelA, 100),
		canonicalizeUsageBillingIdentity(longModelB, 100),
	)
	invalidRequestIDA := "ws-response-" + string([]byte{0xff}) + "-tail"
	invalidRequestIDB := "ws-response-" + string([]byte{0xfe}) + "-tail"
	canonicalInvalidA := canonicalizeUsageBillingIdentity(invalidRequestIDA, 255)
	canonicalInvalidB := canonicalizeUsageBillingIdentity(invalidRequestIDB, 255)
	require.True(t, utf8.ValidString(canonicalInvalidA))
	require.True(t, utf8.ValidString(canonicalInvalidB))
	require.Contains(t, canonicalInvalidA, "#sha256:")
	require.Contains(t, canonicalInvalidB, "#sha256:")
	require.NotEqual(t, canonicalInvalidA, canonicalInvalidB)
}

func TestMarshalUsageBillingOutboxPayload_RemovesPostgresNULBeforeStageZero(t *testing.T) {
	cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)

	rawRequestID := "request-\x00-a"
	rawModel := "priced-\x00-model"
	cmd.RequestID = rawRequestID
	usageLog.RequestID = rawRequestID
	cmd.Model = rawModel
	usageLog.Model = rawModel
	usageLog.RequestedModel = rawModel

	cmd.RequestPayloadHash = "payload\x00hash"
	cmd.AccountType = "api\x00key"
	cmd.ServiceTier = "pri\x00ority"
	cmd.ReasoningEffort = "med\x00ium"
	cmd.MediaType = "im\x00age"
	cmd.Platform = "op\x00enai"

	upstreamModel := "upstream\x00model"
	mappingChain := "alias\x00->mapped"
	billingTier := "tier\x00one"
	billingMode := "to\x00ken"
	serviceTier := "pri\x00ority"
	reasoningEffort := "med\x00ium"
	inboundEndpoint := "/v1/\x00responses"
	upstreamEndpoint := "/v1/\x00responses"
	userAgent := "client\x00agent"
	ipAddress := "127.0.0.\x001"
	sessionID := "session\x00id"
	imageSize := "1K\x00"
	imageInputSize := "1024\x001024"
	imageOutputSize := "1536\x001024"
	imageSizeSource := "req\x00uest"
	mediaType := "im\x00age"
	videoResolution := "720\x00p"
	usageLog.UpstreamModel = &upstreamModel
	usageLog.ModelMappingChain = &mappingChain
	usageLog.BillingTier = &billingTier
	usageLog.BillingMode = &billingMode
	usageLog.ServiceTier = &serviceTier
	usageLog.ReasoningEffort = &reasoningEffort
	usageLog.InboundEndpoint = &inboundEndpoint
	usageLog.UpstreamEndpoint = &upstreamEndpoint
	usageLog.UserAgent = &userAgent
	usageLog.IPAddress = &ipAddress
	usageLog.SessionID = &sessionID
	usageLog.ImageSize = &imageSize
	usageLog.ImageInputSize = &imageInputSize
	usageLog.ImageOutputSize = &imageOutputSize
	usageLog.ImageSizeSource = &imageSizeSource
	usageLog.MediaType = &mediaType
	usageLog.VideoResolution = &videoResolution
	usageLog.ImageSizeBreakdown = map[string]int{"1K\x00": 1}

	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)
	require.NotContains(t, string(commandJSON), `\u0000`)
	require.NotContains(t, string(usageLogJSON), `\u0000`)
	require.NotContains(t, cmd.RequestID, "\x00")
	require.NotContains(t, cmd.Model, "\x00")
	require.Equal(t, cmd.RequestID, usageLog.RequestID)
	require.Equal(t, cmd.Model, usageLog.Model)
	require.Contains(t, cmd.RequestID, "#sha256:")
	require.Contains(t, cmd.Model, "#sha256:")
	require.True(t, utf8.ValidString(cmd.RequestID))
	require.True(t, utf8.ValidString(cmd.Model))
	require.LessOrEqual(t, len(cmd.RequestID), 255)
	require.LessOrEqual(t, len(cmd.Model), 100)

	// Replacing U+0000 with the visible repair rune must not collide with the
	// original U+0000-bearing identity.
	require.NotEqual(
		t,
		canonicalizeUsageBillingIdentity(rawModel, 100),
		canonicalizeUsageBillingIdentity(strings.ReplaceAll(rawModel, "\x00", "\uFFFD"), 100),
	)
	require.NotEqual(
		t,
		canonicalizeUsageBillingIdentity("priced-\x00-model", 100),
		canonicalizeUsageBillingIdentity("priced-\x00model", 100),
	)

	decoded, err := decodeUsageBillingOutboxEvent(
		53,
		0,
		usageLog.CreatedAt,
		cmd.RequestID,
		cmd.APIKeyID,
		cmd.RequestFingerprint,
		usageBillingOutboxPayloadVersion,
		usageBillingOutboxStageBilling,
		commandJSON,
		usageLogJSON,
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, validateUsageBillingOutboxEvent(decoded))
	require.NotContains(t, decoded.Command.RequestPayloadHash, "\x00")
	require.NotContains(t, decoded.Command.ServiceTier, "\x00")
	require.NotContains(t, decoded.UsageLog.RequestedModel, "\x00")
	require.NotContains(t, *decoded.UsageLog.UpstreamModel, "\x00")
	require.Len(t, decoded.UsageLog.ImageSizeBreakdown, 1)
	for key := range decoded.UsageLog.ImageSizeBreakdown {
		require.NotContains(t, key, "\x00")
		require.Contains(t, key, "#sha256:")
	}

	// Exercise the complete stage-zero enqueue/decode transaction with the
	// PostgreSQL-safe payload bytes.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	expectUsageBillingEnqueueFingerprintChecks(mock)
	mock.ExpectQuery(`(?s)INSERT INTO usage_billing_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "attempts", "created_at", "payload_version", "stage",
			"command_payload", "usage_log_payload", "result_payload",
		}).AddRow(
			53, 0, usageLog.CreatedAt, usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
			commandJSON, usageLogJSON, nil,
		))
	mock.ExpectCommit()

	repo := &usageBillingRepository{db: db}
	event, err := repo.enqueueAndClaimUsageBillingOutbox(
		context.Background(),
		"nul-safe-worker",
		cmd,
		commandJSON,
		usageLogJSON,
	)
	require.NoError(t, err)
	require.Equal(t, int64(53), event.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestValidateUsageBillingOutboxPayload_RejectsPostgresNumericOverflow(t *testing.T) {
	tooLargeInteger := int(usageBillingPostgresIntegerMax + 1)
	tooLargeDuration := tooLargeInteger
	tooLargeAccountRate := usageBillingNumeric10Scale4Max + 0.0001
	tooLargeAccountStatsCost := usageBillingNumeric20Scale10UpperBound

	tests := []struct {
		name      string
		mutate    func(*service.UsageBillingCommand, *service.UsageLog)
		wantField string
	}{
		{
			name: "token count exceeds integer",
			mutate: func(cmd *service.UsageBillingCommand, log *service.UsageLog) {
				cmd.InputTokens = tooLargeInteger
				log.InputTokens = tooLargeInteger
			},
			wantField: "command.input_tokens",
		},
		{
			name: "duration exceeds integer",
			mutate: func(_ *service.UsageBillingCommand, log *service.UsageLog) {
				log.DurationMs = &tooLargeDuration
			},
			wantField: "usage_log.duration_ms",
		},
		{
			name: "cost exceeds numeric 20 10",
			mutate: func(cmd *service.UsageBillingCommand, log *service.UsageLog) {
				cmd.ActualCost = usageBillingNumeric20Scale10UpperBound
				cmd.TotalCost = usageBillingNumeric20Scale10UpperBound
				cmd.BalanceCost = usageBillingNumeric20Scale10UpperBound
				log.ActualCost = usageBillingNumeric20Scale10UpperBound
				log.TotalCost = usageBillingNumeric20Scale10UpperBound
			},
			wantField: "command.actual_cost",
		},
		{
			name: "rate multiplier exceeds numeric 10 4",
			mutate: func(_ *service.UsageBillingCommand, log *service.UsageLog) {
				log.AccountRateMultiplier = &tooLargeAccountRate
			},
			wantField: "usage_log.account_rate_multiplier",
		},
		{
			name: "account stats cost exceeds numeric 20 10",
			mutate: func(_ *service.UsageBillingCommand, log *service.UsageLog) {
				log.AccountStatsCost = &tooLargeAccountStatsCost
			},
			wantField: "usage_log.account_stats_cost",
		},
		{
			name: "platform snapshot is non finite",
			mutate: func(cmd *service.UsageBillingCommand, _ *service.UsageLog) {
				cmd.PlatformQuotaSnapshot = &service.UsageBillingPlatformQuotaSnapshot{
					MonthlyUsageUSD: math.Inf(1),
				}
			},
			wantField: "command.platform_quota_snapshot.monthly_usage_usd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
			tt.mutate(cmd, usageLog)

			err := validateUsageBillingOutboxPayload(cmd, usageLog)

			require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestUsageBillingApplyAndRecord_NonFiniteNumericLeavesQuarantinedDurableIntent(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cmd, usageLog, _, _ := newUsageBillingOutboxTestPayload(t)
	cmd.ActualCost = math.NaN()
	cmd.TotalCost = math.NaN()
	cmd.BalanceCost = math.NaN()
	usageLog.ActualCost = math.NaN()
	usageLog.TotalCost = math.NaN()
	usageLog.InputCost = math.NaN()
	commandJSON, usageLogJSON, err := marshalUsageBillingOutboxPayload(cmd, usageLog)
	require.NoError(t, err)
	require.True(t, json.Valid(commandJSON))
	require.True(t, json.Valid(usageLogJSON))
	require.NotContains(t, string(usageLogJSON), `"input_cost":NaN`)

	decoded, err := decodeUsageBillingOutboxEvent(
		52,
		0,
		usageLog.CreatedAt,
		cmd.RequestID,
		cmd.APIKeyID,
		cmd.RequestFingerprint,
		usageBillingOutboxPayloadVersion,
		usageBillingOutboxStageBilling,
		commandJSON,
		usageLogJSON,
		nil,
	)
	require.NoError(t, err)
	require.Contains(t, decoded.PayloadValidationError, "command.actual_cost=NaN (non_finite)")
	require.Contains(t, decoded.PayloadValidationError, "usage_log.input_cost=NaN (non_finite)")
	require.Zero(t, decoded.Command.ActualCost)
	require.Zero(t, decoded.UsageLog.InputCost)
	require.ErrorIs(t, validateUsageBillingOutboxEvent(decoded), service.ErrUsageBillingPayloadInvalid)

	// Stage 0 must commit successfully even though the payload cannot ever be
	// charged. Complete then rejects it before dedup/balance/log writes, and
	// ApplyAndRecord marks the durable row terminal for operator diagnosis.
	mock.ExpectBegin()
	expectUsageBillingEnqueueFingerprintChecks(mock)
	mock.ExpectQuery(`(?s)INSERT INTO usage_billing_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "attempts", "created_at", "payload_version", "stage",
			"command_payload", "usage_log_payload", "result_payload",
		}).AddRow(
			52, 0, usageLog.CreatedAt, usageBillingOutboxPayloadVersion, usageBillingOutboxStageBilling,
			commandJSON, usageLogJSON, nil,
		))
	mock.ExpectCommit()

	mock.ExpectBegin()
	expectClaimedOutboxRow(mock, cmd, commandJSON, usageLogJSON, usageLog.CreatedAt)
	mock.ExpectRollback()
	mock.ExpectExec(`(?s)UPDATE usage_billing_outbox\s+SET terminal_at = NOW\(\)`).
		WithArgs(int64(52), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := &usageBillingRepository{db: db}
	result, err := repo.ApplyAndRecord(ctx, cmd, usageLog)
	require.Nil(t, result)
	require.ErrorIs(t, err, service.ErrUsageBillingPayloadInvalid)
	require.ErrorContains(t, err, "usage_log.input_cost=NaN (non_finite)")
	require.NoError(t, mock.ExpectationsWereMet())
}
