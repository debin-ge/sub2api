package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
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

type wiseBatchOrderQuerier interface {
	QueryOrders(ctx context.Context, tradeNos []string) (map[string]*payment.QueryOrderResponse, error)
}

type wiseReconcileOrderGroup struct {
	key      string
	provider payment.Provider
	orders   []*dbent.PaymentOrder
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
	orders := make([]*dbent.PaymentOrder, 0, pendingWiseReconcileLimit)
	var lastID int64
	for {
		page, err := s.queryWiseReconciliationCandidates(ctx, lastID, now)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		result.Scanned += len(page)
		orders = append(orders, page...)
		for _, order := range page {
			if order == nil {
				continue
			}
			if order.ID > lastID {
				lastID = order.ID
			}
		}
		if len(page) < pendingWiseReconcileLimit {
			break
		}
	}

	groups := s.groupWiseReconciliationOrders(ctx, orders)
	for _, group := range groups {
		s.reconcileWiseOrderGroup(ctx, group, result)
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

func (s *PaymentService) groupWiseReconciliationOrders(ctx context.Context, orders []*dbent.PaymentOrder) []*wiseReconcileOrderGroup {
	groups := make([]*wiseReconcileOrderGroup, 0)
	byKey := make(map[string]*wiseReconcileOrderGroup)
	for _, order := range orders {
		if order == nil {
			continue
		}
		key, prov, err := s.wiseReconciliationProviderForOrder(ctx, order)
		if err != nil {
			slog.Warn("wise pending order provider resolution failed", "orderID", order.ID, "outTradeNo", order.OutTradeNo, "error", err)
			continue
		}
		group := byKey[key]
		if group == nil {
			group = &wiseReconcileOrderGroup{
				key:      key,
				provider: prov,
			}
			byKey[key] = group
			groups = append(groups, group)
		}
		group.orders = append(group.orders, order)
	}
	return groups
}

func (s *PaymentService) wiseReconciliationProviderForOrder(ctx context.Context, order *dbent.PaymentOrder) (string, payment.Provider, error) {
	inst, err := s.getOrderProviderInstance(ctx, order)
	if err != nil {
		return "", nil, fmt.Errorf("load order provider instance: %w", err)
	}
	if inst != nil {
		prov, err := s.createProviderFromInstance(ctx, inst)
		if err != nil {
			return "", nil, err
		}
		return "instance:" + strconv.FormatInt(int64(inst.ID), 10), prov, nil
	}
	if !paymentOrderAllowsRegistryFallback(order) {
		return "", nil, fmt.Errorf("order %d provider instance is unresolved", order.ID)
	}
	providerKey := paymentOrderFallbackProviderKey(s.registry, order)
	if providerKey == "" {
		return "", nil, fmt.Errorf("order %d provider fallback key is missing", order.ID)
	}
	if !s.webhookRegistryFallbackAllowed(ctx, providerKey) {
		return "", nil, fmt.Errorf("order %d provider fallback is ambiguous for %s", order.ID, providerKey)
	}
	s.EnsureProviders(ctx)
	prov, err := s.registry.GetProvider(order.PaymentType)
	if err != nil {
		return "", nil, err
	}
	return "registry:" + providerKey + ":" + strings.TrimSpace(order.PaymentType), prov, nil
}

func (s *PaymentService) reconcileWiseOrderGroup(ctx context.Context, group *wiseReconcileOrderGroup, aggregate *WiseWebhookReconcileResult) {
	if group == nil || group.provider == nil || aggregate == nil {
		return
	}
	if batchProvider, ok := group.provider.(wiseBatchOrderQuerier); ok {
		tradeNos := make([]string, 0, len(group.orders))
		for _, order := range group.orders {
			if order == nil {
				continue
			}
			tradeNos = append(tradeNos, order.OutTradeNo)
		}
		responses, err := batchProvider.QueryOrders(ctx, tradeNos)
		if err != nil {
			slog.Warn("wise pending order batch reconcile failed", "providerGroup", group.key, "orders", len(group.orders), "error", err)
			return
		}
		for _, order := range group.orders {
			if order == nil {
				continue
			}
			orderResult, err := s.handleWiseQueryOrderResponse(ctx, order, responses[strings.TrimSpace(order.OutTradeNo)])
			s.mergeWiseReconcileResult(aggregate, order, orderResult, err)
		}
		return
	}

	for _, order := range group.orders {
		if order == nil {
			continue
		}
		resp, err := group.provider.QueryOrder(ctx, order.OutTradeNo)
		if err != nil {
			err = fmt.Errorf("query wise order %s: %w", order.OutTradeNo, err)
		}
		orderResult, handleErr := s.handleWiseQueryOrderResponse(ctx, order, resp)
		if err == nil {
			err = handleErr
		}
		s.mergeWiseReconcileResult(aggregate, order, orderResult, err)
	}
}

func (s *PaymentService) mergeWiseReconcileResult(aggregate *WiseWebhookReconcileResult, order *dbent.PaymentOrder, orderResult *WiseWebhookReconcileResult, err error) {
	if err != nil {
		orderID := int64(0)
		outTradeNo := ""
		if order != nil {
			orderID = order.ID
			outTradeNo = order.OutTradeNo
		}
		slog.Warn("wise pending order reconcile failed", "orderID", orderID, "outTradeNo", outTradeNo, "error", err)
		return
	}
	if aggregate == nil || orderResult == nil {
		return
	}
	if orderResult.Matched {
		aggregate.Matched = true
	}
	if orderResult.AutoFulfill {
		aggregate.AutoFulfill = true
		aggregate.Fulfilled++
		if aggregate.OrderID == "" {
			aggregate.OrderID = orderResult.OrderID
			aggregate.TradeNo = orderResult.TradeNo
		}
	}
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
	return s.handleWiseQueryOrderResponse(ctx, order, resp)
}

func (s *PaymentService) handleWiseQueryOrderResponse(ctx context.Context, order *dbent.PaymentOrder, resp *payment.QueryOrderResponse) (*WiseWebhookReconcileResult, error) {
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
	wiseTransactionID := wiseTransactionIDFromMetadata(resp.Metadata)
	if wiseTransactionID == "" {
		s.writeAuditLog(ctx, order.ID, "PAYMENT_PROVIDER_METADATA_MISMATCH", payment.TypeWise, map[string]any{
			"detail":  "wise transaction id missing",
			"tradeNo": resp.TradeNo,
		})
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "transaction_id_missing",
		}, nil
	}
	if err := s.ensureWiseTransactionUnused(ctx, order.ID, wiseTransactionID); err != nil {
		s.writeAuditLog(ctx, order.ID, "PAYMENT_TRANSACTION_REUSED", payment.TypeWise, map[string]any{
			"detail":              err.Error(),
			"tradeNo":             resp.TradeNo,
			"wise_transaction_id": wiseTransactionID,
		})
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "transaction_reused",
		}, nil
	}

	notification := &payment.PaymentNotification{
		TradeNo:  wiseTransactionID,
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
		TradeNo:     wiseTransactionID,
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
