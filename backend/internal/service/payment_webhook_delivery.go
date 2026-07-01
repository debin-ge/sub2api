package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentwebhookdelivery"
)

const (
	PaymentWebhookDeliveryStatusReceived  = "received"
	PaymentWebhookDeliveryStatusIgnored   = "ignored"
	PaymentWebhookDeliveryStatusQueued    = "queued"
	PaymentWebhookDeliveryStatusProcessed = "processed"
	PaymentWebhookDeliveryStatusFailed    = "failed"
)

type PaymentWebhookDeliveryInput struct {
	ProviderKey      string
	DeliveryID       string
	EventType        string
	TestNotification bool
	RawBody          string
}

type PaymentWebhookDeliveryRecordResult struct {
	Inserted bool
	ID       int
}

func (s *PaymentService) RecordPaymentWebhookDelivery(ctx context.Context, input PaymentWebhookDeliveryInput) (*PaymentWebhookDeliveryRecordResult, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("payment webhook delivery ent client is nil")
	}
	providerKey := strings.TrimSpace(input.ProviderKey)
	deliveryID := strings.TrimSpace(input.DeliveryID)
	if providerKey == "" {
		return nil, fmt.Errorf("payment webhook delivery provider key is required")
	}
	if deliveryID == "" {
		return nil, fmt.Errorf("payment webhook delivery id is required")
	}

	sum := sha256.Sum256([]byte(input.RawBody))
	row, err := s.entClient.PaymentWebhookDelivery.Create().
		SetProviderKey(providerKey).
		SetDeliveryID(deliveryID).
		SetEventType(strings.TrimSpace(input.EventType)).
		SetTestNotification(input.TestNotification).
		SetStatus(PaymentWebhookDeliveryStatusReceived).
		SetRawBodyHash(hex.EncodeToString(sum[:])).
		Save(ctx)
	if err == nil {
		return &PaymentWebhookDeliveryRecordResult{Inserted: true, ID: int(row.ID)}, nil
	}
	if !dbent.IsConstraintError(err) {
		return nil, err
	}

	existing, queryErr := s.entClient.PaymentWebhookDelivery.Query().
		Where(
			paymentwebhookdelivery.ProviderKeyEQ(providerKey),
			paymentwebhookdelivery.DeliveryIDEQ(deliveryID),
		).
		Only(ctx)
	if queryErr != nil {
		return nil, queryErr
	}
	return &PaymentWebhookDeliveryRecordResult{Inserted: false, ID: int(existing.ID)}, nil
}

func (s *PaymentService) TryQueuePaymentWebhookDelivery(ctx context.Context, id int) (bool, error) {
	if s == nil || s.entClient == nil || id <= 0 {
		return false, nil
	}
	count, err := s.entClient.PaymentWebhookDelivery.Update().
		Where(
			paymentwebhookdelivery.IDEQ(int64(id)),
			paymentwebhookdelivery.StatusEQ(PaymentWebhookDeliveryStatusReceived),
		).
		SetStatus(PaymentWebhookDeliveryStatusQueued).
		ClearError().
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func (s *PaymentService) MarkPaymentWebhookDeliveryStatus(ctx context.Context, id int, status string, errText string) error {
	if s == nil || s.entClient == nil || id <= 0 {
		return nil
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return nil
	}
	update := s.entClient.PaymentWebhookDelivery.UpdateOneID(int64(id)).
		SetStatus(status).
		SetUpdatedAt(time.Now())
	if strings.TrimSpace(errText) != "" {
		update.SetError(errText)
	} else if status != PaymentWebhookDeliveryStatusFailed {
		update.ClearError()
	}
	switch status {
	case PaymentWebhookDeliveryStatusProcessed, PaymentWebhookDeliveryStatusFailed, PaymentWebhookDeliveryStatusIgnored:
		update.SetProcessedAt(time.Now())
	}
	return update.Exec(ctx)
}
