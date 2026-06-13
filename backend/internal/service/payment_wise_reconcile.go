package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	dbpredicate "github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const (
	pendingWiseReconcileLimit = 100
	wiseReconcileWindow       = 72 * time.Hour
)

type WiseWebhookReconcileResult struct {
	Matched     bool
	AutoFulfill bool
	OrderID     string
	TradeNo     string
	Reason      string
	Scanned     int
	Fulfilled   int
}

func (s *PaymentService) HandleWiseWebhook(ctx context.Context, rawBody string, headers map[string]string) (*WiseWebhookReconcileResult, error) {
	providers, err := s.getEnabledWebhookProvidersByKey(ctx, payment.TypeWise)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, prov := range providers {
		if prov == nil {
			continue
		}
		if _, err := prov.VerifyNotification(ctx, rawBody, headers); err != nil {
			lastErr = err
			continue
		}
		if !wiseWebhookEventIsReconcileTrigger(rawBody) {
			return &WiseWebhookReconcileResult{Reason: "event_ignored_unsupported"}, nil
		}
		return s.ReconcilePendingWiseOrders(ctx)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no wise webhook provider could verify notification")
}

func (s *PaymentService) ReconcilePendingWiseOrders(ctx context.Context) (*WiseWebhookReconcileResult, error) {
	result := &WiseWebhookReconcileResult{
		Reason: "event_verified_no_pending",
	}
	now := time.Now()
	var lastID int64
	for {
		orders, err := s.queryWiseReconciliationCandidates(ctx, lastID, now)
		if err != nil {
			return nil, err
		}
		if len(orders) == 0 {
			break
		}
		result.Scanned += len(orders)
		for _, order := range orders {
			if order == nil {
				continue
			}
			if order.ID > lastID {
				lastID = order.ID
			}
			orderResult, err := s.ReconcileWiseOrderByOutTradeNo(ctx, order.OutTradeNo)
			if err != nil {
				slog.Warn("wise pending order reconcile failed", "orderID", order.ID, "outTradeNo", order.OutTradeNo, "error", err)
				continue
			}
			if orderResult == nil {
				continue
			}
			if orderResult.Matched {
				result.Matched = true
			}
			if orderResult.AutoFulfill {
				result.AutoFulfill = true
				result.Fulfilled++
				if result.OrderID == "" {
					result.OrderID = orderResult.OrderID
					result.TradeNo = orderResult.TradeNo
				}
			}
		}
		if len(orders) < pendingWiseReconcileLimit {
			break
		}
	}

	if result.Fulfilled > 0 {
		result.Reason = "event_verified_reconciled"
	} else if result.Scanned > 0 {
		result.Reason = "event_verified_no_auto_fulfill"
	}
	return result, nil
}

func (s *PaymentService) queryWiseReconciliationCandidates(ctx context.Context, afterID int64, now time.Time) ([]*dbent.PaymentOrder, error) {
	predicates := []dbpredicate.PaymentOrder{
		paymentorder.PaymentTypeEQ(payment.TypeWise),
		paymentorder.Or(
			paymentorder.StatusEQ(OrderStatusPending),
			paymentorder.And(
				paymentorder.StatusEQ(OrderStatusExpired),
				paymentorder.ExpiresAtGTE(wiseReconcileCutoff(now)),
			),
		),
	}
	if afterID > 0 {
		predicates = append(predicates, paymentorder.IDGT(afterID))
	}
	orders, err := s.entClient.PaymentOrder.Query().
		Where(predicates...).
		Order(dbent.Asc(paymentorder.FieldID)).
		Limit(pendingWiseReconcileLimit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query pending wise orders: %w", err)
	}
	return orders, nil
}

func wiseReconcileCutoff(now time.Time) time.Time {
	return now.Add(-wiseReconcileWindow)
}

func (s *PaymentService) ReconcileWiseOrderByOutTradeNo(ctx context.Context, outTradeNo string) (*WiseWebhookReconcileResult, error) {
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return nil, fmt.Errorf("out_trade_no is required")
	}

	order, err := s.entClient.PaymentOrder.Query().Where(paymentorder.OutTradeNo(outTradeNo)).Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: out_trade_no=%s", ErrOrderNotFound, outTradeNo)
		}
		return nil, fmt.Errorf("lookup wise order by out_trade_no %s: %w", outTradeNo, err)
	}

	prov, err := s.getOrderProvider(ctx, order)
	if err != nil {
		return nil, err
	}

	resp, err := prov.QueryOrder(ctx, order.OutTradeNo)
	if err != nil {
		return nil, fmt.Errorf("query wise order %s: %w", order.OutTradeNo, err)
	}
	if resp == nil {
		return &WiseWebhookReconcileResult{
			Matched:     false,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			Reason:      "empty_provider_response",
		}, nil
	}

	if resp.Status != payment.ProviderStatusPaid {
		reason := strings.TrimSpace(resp.Metadata["reconcile_reason"])
		if reason == "" {
			reason = "provider_status_" + strings.TrimSpace(resp.Status)
		}
		if reason == "provider_status_" {
			reason = "provider_status_not_paid"
		}
		matched := strings.TrimSpace(resp.Metadata["reconcile_decision"]) != "no_match"
		slog.Info("wise order not auto-fulfilled during reconciliation",
			"orderID", order.ID,
			"outTradeNo", order.OutTradeNo,
			"tradeNo", resp.TradeNo,
			"status", resp.Status,
			"matched", matched,
			"metadata", resp.Metadata,
		)
		return &WiseWebhookReconcileResult{
			Matched:     matched,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      reason,
		}, nil
	}
	if !isValidProviderAmount(resp.Amount) {
		s.writeAuditLog(ctx, order.ID, "PAYMENT_INVALID_AMOUNT", payment.TypeWise, map[string]any{
			"expected": order.PayAmount,
			"paid":     resp.Amount,
			"tradeNo":  resp.TradeNo,
		})
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "invalid_amount",
		}, nil
	}
	if !payment.AmountsEqualByMinorUnit(order.PayAmount, resp.Amount, PaymentOrderCurrency(order)) {
		s.writeAuditLog(ctx, order.ID, "PAYMENT_AMOUNT_MISMATCH", payment.TypeWise, map[string]any{
			"expected": order.PayAmount,
			"paid":     resp.Amount,
			"tradeNo":  resp.TradeNo,
		})
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "amount_mismatch",
		}, nil
	}
	if err := validateProviderNotificationMetadata(order, payment.TypeWise, resp.Metadata); err != nil {
		s.writeAuditLog(ctx, order.ID, "PAYMENT_PROVIDER_METADATA_MISMATCH", payment.TypeWise, map[string]any{
			"detail":  err.Error(),
			"tradeNo": resp.TradeNo,
		})
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "metadata_mismatch",
		}, nil
	}

	notification := &payment.PaymentNotification{
		TradeNo:  resp.TradeNo,
		OrderID:  order.OutTradeNo,
		Amount:   resp.Amount,
		Status:   payment.NotificationStatusSuccess,
		Metadata: resp.Metadata,
	}
	if err := s.HandlePaymentNotification(ctx, notification, payment.TypeWise); err != nil {
		return nil, err
	}
	return &WiseWebhookReconcileResult{
		Matched:     true,
		AutoFulfill: true,
		OrderID:     order.OutTradeNo,
		TradeNo:     resp.TradeNo,
		Reason:      "auto_fulfilled",
		Fulfilled:   1,
	}, nil
}

func wiseWebhookEventIsReconcileTrigger(rawBody string) bool {
	var event struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return false
	}
	switch strings.TrimSpace(event.EventType) {
	case "balances#credit", "balances#update", "account-details-payment#state-change":
		return true
	default:
		return false
	}
}
