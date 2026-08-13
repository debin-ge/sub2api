//go:build unit

package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSessionIDUsageLog(sessionID *string) *service.UsageLog {
	return &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    "req-session-id",
		Model:        "claude-3",
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.0,
		ActualCost:   1.0,
		SessionID:    sessionID,
		CreatedAt:    time.Now().UTC(),
	}
}

// Offsets from the end of the insert arg slice. New columns are appended to the
// tail (so no existing $N placeholder has to be renumbered), which means these
// offsets shift by one each time — that's the point: the compiler can't catch a
// forgotten call site, these constants make the test do it.
const (
	usageLogArgOffsetSessionID  = 3 // …, session_id, created_at, billing_state
	usageLogArgOffsetCreatedAt  = 2
	usageLogArgOffsetBillingSta = 1
)

func usageLogInsertArgFromEnd(args []any, offsetFromEnd int) any {
	return args[len(args)-offsetFromEnd]
}

// TestPrepareUsageLogInsert_SessionIDArgWiring pins the session_id column to the
// arg slice / arg-type table so the INSERT column lists stay in sync.
func TestPrepareUsageLogInsert_SessionIDArgWiring(t *testing.T) {
	require.Len(t, usageLogInsertArgTypes, 60, "arg-type table must include session_id and billing_state")

	sessionID := "sess-persisted-123"
	prepared := prepareUsageLogInsert(newSessionIDUsageLog(&sessionID))

	require.Len(t, prepared.args, len(usageLogInsertArgTypes),
		"prepared args must match the arg-type table length")

	sessionArg := usageLogInsertArgFromEnd(prepared.args, usageLogArgOffsetSessionID)
	ns, ok := sessionArg.(sql.NullString)
	require.True(t, ok, "session_id arg should be a sql.NullString, got %T", sessionArg)
	require.True(t, ns.Valid)
	require.Equal(t, sessionID, ns.String)

	require.Equal(t, "text", usageLogInsertArgTypes[len(usageLogInsertArgTypes)-usageLogArgOffsetSessionID],
		"session_id arg type must be text")
}

// TestPrepareUsageLogInsert_SessionIDNullWhenAbsent proves an absent session id is
// persisted as SQL NULL rather than an empty string.
func TestPrepareUsageLogInsert_SessionIDNullWhenAbsent(t *testing.T) {
	prepared := prepareUsageLogInsert(newSessionIDUsageLog(nil))
	sessionArg := usageLogInsertArgFromEnd(prepared.args, usageLogArgOffsetSessionID)
	ns, ok := sessionArg.(sql.NullString)
	require.True(t, ok, "session_id arg should be a sql.NullString, got %T", sessionArg)
	require.False(t, ns.Valid, "absent session id must be NULL, not empty string")

	empty := ""
	preparedEmpty := prepareUsageLogInsert(newSessionIDUsageLog(&empty))
	nsEmpty := usageLogInsertArgFromEnd(preparedEmpty.args, usageLogArgOffsetSessionID).(sql.NullString)
	require.False(t, nsEmpty.Valid, "empty session id must also be NULL")
}

// TestPrepareUsageLogInsert_BillingStateArgWiring pins billing_state to the insert
// path. Every INSERT column list here uses AnyArg-style fixtures elsewhere, so a
// dropped arg would otherwise silently persist every row as "已结算" — exactly the
// "$0 看起来像免费" failure this column exists to prevent.
func TestPrepareUsageLogInsert_BillingStateArgWiring(t *testing.T) {
	log := newSessionIDUsageLog(nil)
	log.BillingState = service.BillingStatePricingUnavailable

	prepared := prepareUsageLogInsert(log)

	require.Equal(t, "smallint", usageLogInsertArgTypes[len(usageLogInsertArgTypes)-usageLogArgOffsetBillingSta],
		"billing_state arg type must be smallint")
	require.IsType(t, time.Time{}, usageLogInsertArgFromEnd(prepared.args, usageLogArgOffsetCreatedAt),
		"created_at must stay immediately before billing_state")
	require.Equal(t, service.BillingStatePricingUnavailable,
		usageLogInsertArgFromEnd(prepared.args, usageLogArgOffsetBillingSta),
		"billing_state must reach the insert args verbatim")

	settled := prepareUsageLogInsert(newSessionIDUsageLog(nil))
	require.Equal(t, service.BillingStateSettled,
		usageLogInsertArgFromEnd(settled.args, usageLogArgOffsetBillingSta),
		"zero value must persist as settled so historical rows stay compatible")
}

// TestUsageLogQueries_IncludeBillingState guards that every generated INSERT path and
// the SELECT column list carry billing_state — a marker only written on one path is
// worse than no marker, because the gap looks like healthy traffic.
func TestUsageLogQueries_IncludeBillingState(t *testing.T) {
	require.Contains(t, usageLogSelectColumns, "billing_state",
		"SELECT column list must include billing_state")

	log := newSessionIDUsageLog(nil)
	prepared := prepareUsageLogInsert(log)
	key := usageLogBatchKey(log.RequestID, log.APIKeyID)

	batchQuery, _ := buildUsageLogBatchInsertQuery([]string{key},
		map[string]usageLogInsertPrepared{key: prepared})
	require.GreaterOrEqual(t, strings.Count(batchQuery, "billing_state"), 3,
		"batch insert needs billing_state in the CTE def, the INSERT column list and the SELECT")

	bestEffortQuery, _ := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	require.Contains(t, bestEffortQuery, "billing_state")
}

// TestUsageLogInsertQueries_IncludeSessionID guards that every generated INSERT path
// and the SELECT column list reference session_id.
func TestUsageLogInsertQueries_IncludeSessionID(t *testing.T) {
	require.Contains(t, usageLogSelectColumns, "session_id",
		"SELECT column list must include session_id")

	sessionID := "sess-in-query"
	log := newSessionIDUsageLog(&sessionID)
	prepared := prepareUsageLogInsert(log)
	key := usageLogBatchKey(log.RequestID, log.APIKeyID)

	batchQuery, batchArgs := buildUsageLogBatchInsertQuery([]string{key},
		map[string]usageLogInsertPrepared{key: prepared})
	require.Contains(t, batchQuery, "session_id")
	// Two column references (INSERT column list + SELECT ... FROM input) plus the CTE def.
	require.GreaterOrEqual(t, strings.Count(batchQuery, "session_id"), 3)
	require.Len(t, batchArgs, len(prepared.args)+1,
		"batch args include the synthetic input_index before usage-log values")

	bestEffortQuery, bestEffortArgs := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	require.Contains(t, bestEffortQuery, "session_id")
	require.Len(t, bestEffortArgs, len(prepared.args))
}
