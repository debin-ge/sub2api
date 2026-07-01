//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/paymentwebhookdelivery"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestRecordPaymentWebhookDeliveryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc := &PaymentService{entClient: client}

	input := PaymentWebhookDeliveryInput{
		ProviderKey:      payment.TypeWise,
		DeliveryID:       "delivery-123",
		EventType:        "balances#credit",
		TestNotification: true,
		RawBody:          `{"event_type":"balances#credit"}`,
	}

	first, err := svc.RecordPaymentWebhookDelivery(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.True(t, first.Inserted)
	require.NotZero(t, first.ID)

	second, err := svc.RecordPaymentWebhookDelivery(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.False(t, second.Inserted)
	require.Equal(t, first.ID, second.ID)

	rows, err := client.PaymentWebhookDelivery.Query().
		Where(
			paymentwebhookdelivery.ProviderKeyEQ(payment.TypeWise),
			paymentwebhookdelivery.DeliveryIDEQ("delivery-123"),
		).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, PaymentWebhookDeliveryStatusReceived, rows[0].Status)
	require.Equal(t, input.EventType, rows[0].EventType)
	require.True(t, rows[0].TestNotification)
	require.Nil(t, rows[0].Error)
	require.Nil(t, rows[0].ProcessedAt)

	sum := sha256.Sum256([]byte(input.RawBody))
	require.Equal(t, hex.EncodeToString(sum[:]), rows[0].RawBodyHash)
	require.NotContains(t, rows[0].RawBodyHash, input.RawBody)
}

func TestTryQueuePaymentWebhookDeliveryOnlyTransitionsReceived(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc := &PaymentService{entClient: client}

	record, err := svc.RecordPaymentWebhookDelivery(ctx, PaymentWebhookDeliveryInput{
		ProviderKey: payment.TypeWise,
		DeliveryID:  "delivery-queue",
		EventType:   "balances#credit",
		RawBody:     `{"event_type":"balances#credit"}`,
	})
	require.NoError(t, err)

	queued, err := svc.TryQueuePaymentWebhookDelivery(ctx, record.ID)
	require.NoError(t, err)
	require.True(t, queued)

	queued, err = svc.TryQueuePaymentWebhookDelivery(ctx, record.ID)
	require.NoError(t, err)
	require.False(t, queued)

	row, err := client.PaymentWebhookDelivery.Get(ctx, int64(record.ID))
	require.NoError(t, err)
	require.Equal(t, PaymentWebhookDeliveryStatusQueued, row.Status)
}

func TestMarkPaymentWebhookDeliveryStatusRejectsEmptyAndClearsErrorOnSuccess(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	svc := &PaymentService{entClient: client}

	record, err := svc.RecordPaymentWebhookDelivery(ctx, PaymentWebhookDeliveryInput{
		ProviderKey: payment.TypeWise,
		DeliveryID:  "delivery-status",
		EventType:   "balances#credit",
		RawBody:     `{"event_type":"balances#credit"}`,
	})
	require.NoError(t, err)

	require.NoError(t, svc.MarkPaymentWebhookDeliveryStatus(ctx, record.ID, PaymentWebhookDeliveryStatusFailed, "temporary failure"))
	require.NoError(t, svc.MarkPaymentWebhookDeliveryStatus(ctx, record.ID, "", "ignored"))
	row, err := client.PaymentWebhookDelivery.Get(ctx, int64(record.ID))
	require.NoError(t, err)
	require.Equal(t, PaymentWebhookDeliveryStatusFailed, row.Status)
	require.NotNil(t, row.Error)

	require.NoError(t, svc.MarkPaymentWebhookDeliveryStatus(ctx, record.ID, PaymentWebhookDeliveryStatusProcessed, ""))
	row, err = client.PaymentWebhookDelivery.Get(ctx, int64(record.ID))
	require.NoError(t, err)
	require.Equal(t, PaymentWebhookDeliveryStatusProcessed, row.Status)
	require.Nil(t, row.Error)
}
