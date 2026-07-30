package service

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProvidePaymentServiceWiresCompletionTransactionTimeout(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	svc := ProvidePaymentService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		db,
		&config.Config{PaymentFulfillmentDBTxTimeout: 2 * time.Minute},
	)

	require.Same(t, db, svc.paymentFulfillmentDB)
	require.Equal(t, 2*time.Minute, svc.paymentFulfillmentDBTxTimeout)
}

func TestPaymentFulfillmentCompletionUsesTransactionLocalTimeout(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	leaseVersion := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	completedAt := leaseVersion.Add(time.Second)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('statement_timeout', \$1, true\)`).
		WithArgs("120000ms").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)UPDATE payment_orders.*RETURNING completed_at`).
		WithArgs(
			OrderStatusCompleted,
			int64(42),
			OrderStatusRecharging,
			leaseVersion,
		).
		WillReturnRows(
			sqlmock.NewRows([]string{"completed_at"}).AddRow(completedAt),
		)
	mock.ExpectCommit()

	svc := &PaymentService{}
	svc.SetPaymentFulfillmentDBTransaction(db, 2*time.Minute)
	gotCompletedAt, updated, err := svc.markPaymentOrderCompletedAtDatabaseTime(
		context.Background(),
		42,
		leaseVersion,
	)

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, completedAt, gotCompletedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPaymentFulfillmentCompletionRollsBackWhenSettingTimeoutFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('statement_timeout', \$1, true\)`).
		WithArgs("45000ms").
		WillReturnError(errors.New("statement timeout unavailable"))
	mock.ExpectRollback()

	svc := &PaymentService{}
	svc.SetPaymentFulfillmentDBTransaction(db, 45*time.Second)
	_, updated, err := svc.markPaymentOrderCompletedAtDatabaseTime(
		context.Background(),
		42,
		time.Now(),
	)

	require.False(t, updated)
	require.ErrorContains(t, err, "set payment fulfillment statement timeout")
	require.NoError(t, mock.ExpectationsWereMet())
}
