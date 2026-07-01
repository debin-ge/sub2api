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
	paymentprovider "github.com/Wei-Shaw/sub2api/internal/payment/provider"
)

const (
	pendingWiseReconcileLimit = 100
	wiseReconcileWindow       = 72 * time.Hour
	wiseMaxReconcileWindow    = 720 * time.Hour
)

type WiseWebhookReconcileResult struct {
	Matched     bool
	AutoFulfill bool
	OrderID     string
	TradeNo     string
	Reason      string
	Scanned     int
	Fulfilled   int
	Errors      []string
}

type WiseWebhookHandleResult struct {
	DeliveryID       string
	EventType        string
	TestNotification bool
	Duplicate        bool
	Queued           bool
	Reason           string
}

type wiseWebhookEnvelope struct {
	EventType string
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

func (s *PaymentService) HandleWiseWebhookFast(ctx context.Context, rawBody string, headers map[string]string) (*WiseWebhookHandleResult, error) {
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

		envelope := wiseWebhookEnvelopeFromBody(rawBody)
		deliveryID := wiseWebhookHeaderValue(headers, "x-delivery-id")
		if deliveryID == "" {
			return nil, fmt.Errorf("wise webhook delivery id is required")
		}
		testNotification := strings.EqualFold(wiseWebhookHeaderValue(headers, "x-test-notification"), "true")
		result := &WiseWebhookHandleResult{
			DeliveryID:       deliveryID,
			EventType:        envelope.EventType,
			TestNotification: testNotification,
		}

		record, err := s.RecordPaymentWebhookDelivery(ctx, PaymentWebhookDeliveryInput{
			ProviderKey:      payment.TypeWise,
			DeliveryID:       deliveryID,
			EventType:        envelope.EventType,
			TestNotification: testNotification,
			RawBody:          rawBody,
		})
		if err != nil {
			return nil, err
		}

		if testNotification {
			if !record.Inserted {
				result.Duplicate = true
				result.Reason = "duplicate_delivery"
				return result, nil
			}
			result.Reason = "test_notification"
			if err := s.MarkPaymentWebhookDeliveryStatus(ctx, record.ID, PaymentWebhookDeliveryStatusIgnored, ""); err != nil {
				return nil, err
			}
			return result, nil
		}
		if !wiseWebhookEventIsReconcileTrigger(rawBody) {
			if !record.Inserted {
				result.Duplicate = true
				result.Reason = "duplicate_delivery"
				return result, nil
			}
			result.Reason = "event_ignored_unsupported"
			if err := s.MarkPaymentWebhookDeliveryStatus(ctx, record.ID, PaymentWebhookDeliveryStatusIgnored, ""); err != nil {
				return nil, err
			}
			return result, nil
		}

		queued, err := s.TryQueuePaymentWebhookDelivery(ctx, record.ID)
		if err != nil {
			return nil, err
		}
		if !queued {
			result.Duplicate = true
			result.Reason = "duplicate_delivery"
			return result, nil
		}
		result.Queued = true
		result.Reason = "queued"
		go s.runWiseWebhookReconciliation(record.ID)
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no wise webhook provider could verify notification")
}

func (s *PaymentService) runWiseWebhookReconciliation(deliveryRecordID int) {
	ctx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	defer cancel()

	if _, err := s.ReconcilePendingWiseOrders(ctx); err != nil {
		slog.Error("wise webhook reconciliation failed", "deliveryRecordID", deliveryRecordID, "error", err)
		_ = s.MarkPaymentWebhookDeliveryStatus(context.Background(), deliveryRecordID, PaymentWebhookDeliveryStatusFailed, err.Error())
		return
	}
	_ = s.MarkPaymentWebhookDeliveryStatus(context.Background(), deliveryRecordID, PaymentWebhookDeliveryStatusProcessed, "")
}

func (s *PaymentService) ReconcilePendingWiseOrders(ctx context.Context) (*WiseWebhookReconcileResult, error) {
	result := &WiseWebhookReconcileResult{
		Reason: "event_verified_no_pending",
	}
	now := time.Now()
	orders := make([]*dbent.PaymentOrder, 0, pendingWiseReconcileLimit)
	var lastID int64
	for {
		page, nextLastID, hasMore, err := s.queryWiseReconciliationCandidates(ctx, lastID, now)
		if err != nil {
			return nil, err
		}
		if nextLastID == 0 {
			break
		}
		result.Scanned += len(page)
		orders = append(orders, page...)
		lastID = nextLastID
		if !hasMore {
			break
		}
	}

	groups, reconcileErrs := s.groupWiseReconciliationOrders(ctx, orders)
	for _, group := range groups {
		reconcileErrs = append(reconcileErrs, s.reconcileWiseOrderGroup(ctx, group, result)...)
	}

	if result.Fulfilled > 0 {
		result.Reason = "event_verified_reconciled"
	} else if result.Scanned > 0 {
		result.Reason = "event_verified_no_auto_fulfill"
	}
	if len(reconcileErrs) > 0 {
		result.Errors = wiseReconcileErrorStrings(reconcileErrs)
		return result, fmt.Errorf("wise reconciliation failed: %d error(s)", len(reconcileErrs))
	}
	return result, nil
}

func (s *PaymentService) queryWiseReconciliationCandidates(ctx context.Context, afterID int64, now time.Time) ([]*dbent.PaymentOrder, int64, bool, error) {
	predicates := []dbpredicate.PaymentOrder{
		paymentorder.PaymentTypeEQ(payment.TypeWise),
		paymentorder.Or(
			paymentorder.StatusEQ(OrderStatusPending),
			paymentorder.And(
				paymentorder.StatusIn(OrderStatusExpired, OrderStatusCancelled),
				paymentorder.ExpiresAtGTE(now.Add(-wiseMaxReconcileWindow)),
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
		return nil, 0, false, fmt.Errorf("query pending wise orders: %w", err)
	}
	if len(orders) == 0 {
		return nil, 0, false, nil
	}
	lastID := afterID
	candidates := make([]*dbent.PaymentOrder, 0, len(orders))
	for _, order := range orders {
		if order == nil {
			continue
		}
		if order.ID > lastID {
			lastID = order.ID
		}
		if s.wiseOrderInsideReconcileWindow(ctx, order, now) {
			candidates = append(candidates, order)
		}
	}
	return candidates, lastID, len(orders) == pendingWiseReconcileLimit, nil
}

func (s *PaymentService) wiseOrderInsideReconcileWindow(ctx context.Context, order *dbent.PaymentOrder, now time.Time) bool {
	if order == nil {
		return false
	}
	if order.Status == OrderStatusPending {
		return true
	}
	if order.Status != OrderStatusExpired && order.Status != OrderStatusCancelled {
		return false
	}
	return !order.ExpiresAt.Before(now.Add(-s.wiseReconcileWindowForOrder(ctx, order)))
}

func (s *PaymentService) wiseReconcileWindowForOrder(ctx context.Context, order *dbent.PaymentOrder) time.Duration {
	if s == nil || order == nil || strings.TrimSpace(order.PaymentType) != payment.TypeWise {
		return wiseReconcileWindow
	}
	instID, ok := wiseOrderProviderInstanceID(order)
	if !ok || s.loadBalancer == nil {
		return wiseReconcileWindow
	}
	config, err := s.loadBalancer.GetInstanceConfig(ctx, instID)
	if err != nil {
		slog.Warn("wise reconcile window config lookup failed", "orderID", order.ID, "instanceID", instID, "error", err)
		return wiseReconcileWindow
	}
	return wiseReconcileWindowFromConfig(config)
}

func wiseOrderProviderInstanceID(order *dbent.PaymentOrder) (int64, bool) {
	if order == nil {
		return 0, false
	}
	raw := strings.TrimSpace(psStringValue(order.ProviderInstanceID))
	if raw == "" {
		return 0, false
	}
	instID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || instID <= 0 {
		return 0, false
	}
	return instID, true
}

func wiseReconcileWindowFromConfig(config map[string]string) time.Duration {
	raw := strings.TrimSpace(providerConfigFieldValue(config, "reconcileWindowHours"))
	if raw == "" {
		return wiseReconcileWindow
	}
	hours, err := strconv.Atoi(raw)
	if err != nil || hours <= 0 {
		return wiseReconcileWindow
	}
	maxHours := int(wiseMaxReconcileWindow / time.Hour)
	if hours > maxHours {
		return wiseMaxReconcileWindow
	}
	return time.Duration(hours) * time.Hour
}

func wiseWebhookEnvelopeFromBody(rawBody string) wiseWebhookEnvelope {
	var payload struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal([]byte(rawBody), &payload); err != nil {
		return wiseWebhookEnvelope{}
	}
	return wiseWebhookEnvelope{EventType: strings.TrimSpace(payload.EventType)}
}

func wiseWebhookHeaderValue(headers map[string]string, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for key, value := range headers {
		if strings.ToLower(strings.TrimSpace(key)) == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *PaymentService) groupWiseReconciliationOrders(ctx context.Context, orders []*dbent.PaymentOrder) ([]*wiseReconcileOrderGroup, []error) {
	groups := make([]*wiseReconcileOrderGroup, 0)
	errs := make([]error, 0)
	byKey := make(map[string]*wiseReconcileOrderGroup)
	for _, order := range orders {
		if order == nil {
			continue
		}
		key, prov, err := s.wiseReconciliationProviderForOrder(ctx, order)
		if err != nil {
			slog.Warn("wise pending order provider resolution failed", "orderID", order.ID, "outTradeNo", order.OutTradeNo, "error", err)
			errs = append(errs, fmt.Errorf("resolve wise provider for %s: %w", order.OutTradeNo, err))
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
	return groups, errs
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

func (s *PaymentService) reconcileWiseOrderGroup(ctx context.Context, group *wiseReconcileOrderGroup, aggregate *WiseWebhookReconcileResult) []error {
	errs := make([]error, 0)
	if group == nil || group.provider == nil || aggregate == nil {
		return errs
	}
	if batchProvider, ok := group.provider.(wiseBatchOrderQuerier); ok {
		tradeNos := make([]string, 0, len(group.orders))
		for _, order := range group.orders {
			if order == nil {
				continue
			}
			tradeNos = append(tradeNos, order.OutTradeNo)
		}
		responses, err := batchProvider.QueryOrders(wiseOrderGroupQueryContext(ctx, group.orders), tradeNos)
		if err != nil {
			slog.Warn("wise pending order batch reconcile failed", "providerGroup", group.key, "orders", len(group.orders), "error", err)
			return append(errs, fmt.Errorf("query wise order batch %s: %w", group.key, err))
		}
		for _, order := range group.orders {
			if order == nil {
				continue
			}
			orderResult, err := s.handleWiseQueryOrderResponse(ctx, order, responses[strings.TrimSpace(order.OutTradeNo)])
			s.mergeWiseReconcileResult(aggregate, order, orderResult, err)
			if err != nil {
				errs = append(errs, fmt.Errorf("handle wise order %s: %w", order.OutTradeNo, err))
			}
		}
		return errs
	}

	for _, order := range group.orders {
		if order == nil {
			continue
		}
		resp, err := group.provider.QueryOrder(paymentprovider.WithWiseOrderCreatedAt(ctx, order.CreatedAt), order.OutTradeNo)
		if err != nil {
			err = fmt.Errorf("query wise order %s: %w", order.OutTradeNo, err)
		}
		orderResult, handleErr := s.handleWiseQueryOrderResponse(ctx, order, resp)
		if err == nil {
			err = handleErr
		}
		s.mergeWiseReconcileResult(aggregate, order, orderResult, err)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func wiseReconcileErrorStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		out = append(out, err.Error())
	}
	return out
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

	resp, err := prov.QueryOrder(paymentprovider.WithWiseOrderCreatedAt(ctx, order.CreatedAt), order.OutTradeNo)
	if err != nil {
		return nil, fmt.Errorf("query wise order %s: %w", order.OutTradeNo, err)
	}
	return s.handleWiseQueryOrderResponse(ctx, order, resp)
}

func wiseOrderGroupQueryContext(ctx context.Context, orders []*dbent.PaymentOrder) context.Context {
	var earliest time.Time
	for _, order := range orders {
		if order == nil || order.CreatedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || order.CreatedAt.Before(earliest) {
			earliest = order.CreatedAt
		}
	}
	return paymentprovider.WithWiseOrderCreatedAt(ctx, earliest)
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
		decision := strings.TrimSpace(resp.Metadata["reconcile_decision"])
		matched := wiseReconcileDecisionReferencesTransaction(decision)
		slog.Info("wise order not auto-fulfilled during reconciliation",
			"orderID", order.ID,
			"outTradeNo", order.OutTradeNo,
			"tradeNo", resp.TradeNo,
			"status", resp.Status,
			"matched", matched,
			"metadata", resp.Metadata,
		)
		if matched {
			s.writeAuditLog(ctx, order.ID, "PAYMENT_WISE_RECONCILE_MANUAL_REVIEW", payment.TypeWise,
				wiseReconcileAuditDetail(order, resp, reason, "manual_review"))
		} else if decision == "no_match" {
			s.writeAuditLog(ctx, order.ID, "PAYMENT_WISE_RECONCILE_NO_MATCH", payment.TypeWise,
				wiseReconcileAuditDetail(order, resp, reason, "no_match"))
		}
		return &WiseWebhookReconcileResult{
			Matched:     matched,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      reason,
		}, nil
	}
	if !isValidProviderAmount(resp.Amount) {
		s.writeAuditLog(ctx, order.ID, "PAYMENT_INVALID_AMOUNT", payment.TypeWise,
			wiseReconcileAuditDetail(order, resp, "invalid_amount", "manual_review"))
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "invalid_amount",
		}, nil
	}
	if !payment.AmountsEqualByMinorUnit(order.PayAmount, resp.Amount, PaymentOrderCurrency(order)) {
		detail := wiseReconcileAuditDetail(order, resp, "amount_mismatch", "manual_review")
		detail["reconcile_reason"] = "amount_mismatch"
		detail["reconcile_decision"] = "manual_review"
		detail["reconcileDecision"] = "manual_review"
		s.writeAuditLog(ctx, order.ID, "PAYMENT_AMOUNT_MISMATCH", payment.TypeWise, detail)
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "amount_mismatch",
		}, nil
	}
	if err := validateProviderNotificationMetadata(order, payment.TypeWise, resp.Metadata); err != nil {
		detail := wiseReconcileAuditDetail(order, resp, "metadata_mismatch", "manual_review")
		detail["detail"] = err.Error()
		s.writeAuditLog(ctx, order.ID, "PAYMENT_PROVIDER_METADATA_MISMATCH", payment.TypeWise, detail)
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
		detail := wiseReconcileAuditDetail(order, resp, "transaction_id_missing", "manual_review")
		detail["detail"] = "wise transaction id missing"
		s.writeAuditLog(ctx, order.ID, "PAYMENT_PROVIDER_METADATA_MISMATCH", payment.TypeWise, detail)
		return &WiseWebhookReconcileResult{
			Matched:     true,
			AutoFulfill: false,
			OrderID:     order.OutTradeNo,
			TradeNo:     resp.TradeNo,
			Reason:      "transaction_id_missing",
		}, nil
	}
	if err := s.ensureWiseTransactionUnused(ctx, order.ID, wiseTransactionID); err != nil {
		detail := wiseReconcileAuditDetail(order, resp, "transaction_reused", "manual_review")
		detail["detail"] = err.Error()
		detail["wise_transaction_id"] = wiseTransactionID
		s.writeAuditLog(ctx, order.ID, "PAYMENT_TRANSACTION_REUSED", payment.TypeWise, detail)
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

func wiseReconcileDecisionReferencesTransaction(decision string) bool {
	switch strings.TrimSpace(decision) {
	case "manual_review", "rejected", "auto_fulfill":
		return true
	default:
		return false
	}
}

func wiseReconcileAuditDetail(order *dbent.PaymentOrder, resp *payment.QueryOrderResponse, reason, reviewAction string) map[string]any {
	detail := map[string]any{
		"reason":       strings.TrimSpace(reason),
		"reviewAction": strings.TrimSpace(reviewAction),
	}
	if order != nil {
		detail["expected"] = order.PayAmount
		detail["outTradeNo"] = order.OutTradeNo
		detail["orderStatus"] = order.Status
	}
	if resp == nil {
		return detail
	}
	detail["tradeNo"] = resp.TradeNo
	if isValidProviderAmount(resp.Amount) {
		detail["paid"] = resp.Amount
	} else {
		detail["paid"] = fmt.Sprintf("%v", resp.Amount)
	}
	if resp.Status != "" {
		detail["providerStatus"] = resp.Status
	}
	for key, value := range resp.Metadata {
		if strings.TrimSpace(value) == "" {
			continue
		}
		detail[key] = value
		if key == "reconcile_decision" {
			detail["reconcileDecision"] = value
		}
	}
	return detail
}

func wiseWebhookEventIsReconcileTrigger(rawBody string) bool {
	switch wiseWebhookEnvelopeFromBody(rawBody).EventType {
	case "balances#credit":
		return true
	default:
		return false
	}
}
