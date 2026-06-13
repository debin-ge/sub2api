package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const wiseSettlementStrategyExactOnly = "exact_only"

type paymentOrderProviderSnapshot struct {
	SchemaVersion      int
	ProviderInstanceID string
	ProviderKey        string
	PaymentMode        string
	MerchantAppID      string
	MerchantID         string
	BalanceID          string
	Currency           string
	SettlementStrategy string
}

func psOrderProviderSnapshot(order *dbent.PaymentOrder) *paymentOrderProviderSnapshot {
	if order == nil || len(order.ProviderSnapshot) == 0 {
		return nil
	}

	snapshot := &paymentOrderProviderSnapshot{
		SchemaVersion:      psSnapshotIntValue(order.ProviderSnapshot["schema_version"]),
		ProviderInstanceID: psSnapshotStringValue(order.ProviderSnapshot["provider_instance_id"]),
		ProviderKey:        psSnapshotStringValue(order.ProviderSnapshot["provider_key"]),
		PaymentMode:        psSnapshotStringValue(order.ProviderSnapshot["payment_mode"]),
		MerchantAppID:      psSnapshotStringValue(order.ProviderSnapshot["merchant_app_id"]),
		MerchantID:         psSnapshotStringValue(order.ProviderSnapshot["merchant_id"]),
		BalanceID:          psSnapshotStringValue(order.ProviderSnapshot["balance_id"]),
		Currency:           psSnapshotStringValue(order.ProviderSnapshot["currency"]),
		SettlementStrategy: psSnapshotStringValue(order.ProviderSnapshot["settlement_strategy"]),
	}
	if snapshot.SchemaVersion == 0 &&
		snapshot.ProviderInstanceID == "" &&
		snapshot.ProviderKey == "" &&
		snapshot.PaymentMode == "" &&
		snapshot.MerchantAppID == "" &&
		snapshot.MerchantID == "" &&
		snapshot.BalanceID == "" &&
		snapshot.Currency == "" &&
		snapshot.SettlementStrategy == "" {
		return nil
	}
	return snapshot
}

func psSnapshotStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func psSnapshotIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return n
		}
	}
	return 0
}

func (s *PaymentService) resolveSnapshotOrderProviderInstance(ctx context.Context, order *dbent.PaymentOrder, snapshot *paymentOrderProviderSnapshot) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || order == nil || snapshot == nil {
		return nil, nil
	}

	snapshotInstanceID := strings.TrimSpace(snapshot.ProviderInstanceID)
	columnInstanceID := strings.TrimSpace(psStringValue(order.ProviderInstanceID))
	if snapshotInstanceID == "" {
		snapshotInstanceID = columnInstanceID
	}
	if snapshotInstanceID == "" {
		return nil, fmt.Errorf("order %d provider snapshot is missing provider_instance_id", order.ID)
	}
	if columnInstanceID != "" && snapshot.ProviderInstanceID != "" && !strings.EqualFold(columnInstanceID, snapshot.ProviderInstanceID) {
		return nil, fmt.Errorf("order %d provider snapshot instance mismatch: snapshot=%s order=%s", order.ID, snapshot.ProviderInstanceID, columnInstanceID)
	}

	instID, err := strconv.ParseInt(snapshotInstanceID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d provider snapshot instance id is invalid: %s", order.ID, snapshotInstanceID)
	}

	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d provider snapshot instance %s is missing", order.ID, snapshotInstanceID)
		}
		return nil, err
	}

	if snapshot.ProviderKey != "" && !strings.EqualFold(strings.TrimSpace(inst.ProviderKey), snapshot.ProviderKey) {
		return nil, fmt.Errorf("order %d provider snapshot key mismatch: snapshot=%s instance=%s", order.ID, snapshot.ProviderKey, inst.ProviderKey)
	}

	return inst, nil
}

func expectedNotificationProviderKeyForOrder(registry *payment.Registry, order *dbent.PaymentOrder, instanceProviderKey string) string {
	if order == nil {
		return strings.TrimSpace(instanceProviderKey)
	}

	orderProviderKey := psStringValue(order.ProviderKey)
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil && snapshot.ProviderKey != "" {
		orderProviderKey = snapshot.ProviderKey
	}

	return expectedNotificationProviderKey(registry, order.PaymentType, orderProviderKey, instanceProviderKey)
}

func validateProviderSnapshotMetadata(order *dbent.PaymentOrder, providerKey string, metadata map[string]string) error {
	if order == nil {
		return nil
	}

	snapshot := psOrderProviderSnapshot(order)
	if snapshot == nil {
		return nil
	}

	normalizedProviderKey := strings.TrimSpace(providerKey)
	if len(metadata) == 0 {
		if payment.GetBasePaymentType(normalizedProviderKey) == payment.TypeWise {
			return fmt.Errorf("wise notification metadata missing")
		}
		return nil
	}

	switch normalizedProviderKey {
	case payment.TypeWxpay:
		if expected := strings.TrimSpace(snapshot.MerchantAppID); expected != "" {
			actual := strings.TrimSpace(metadata["appid"])
			if actual == "" {
				return fmt.Errorf("wxpay notification missing appid")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("wxpay appid mismatch: expected %s, got %s", expected, actual)
			}
		}
		if expected := strings.TrimSpace(snapshot.MerchantID); expected != "" {
			actual := strings.TrimSpace(metadata["mchid"])
			if actual == "" {
				return fmt.Errorf("wxpay notification missing mchid")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("wxpay mchid mismatch: expected %s, got %s", expected, actual)
			}
		}
		if expected := strings.TrimSpace(snapshot.Currency); expected != "" {
			actual := strings.ToUpper(strings.TrimSpace(metadata["currency"]))
			if actual == "" {
				return fmt.Errorf("wxpay notification missing currency")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("wxpay currency mismatch: expected %s, got %s", expected, actual)
			}
		}
		if actual := strings.TrimSpace(metadata["trade_state"]); actual != "" && !strings.EqualFold(actual, "SUCCESS") {
			return fmt.Errorf("wxpay trade_state mismatch: expected SUCCESS, got %s", actual)
		}
	case payment.TypeAlipay:
		if expected := strings.TrimSpace(snapshot.MerchantAppID); expected != "" {
			actual := strings.TrimSpace(metadata["app_id"])
			if actual == "" {
				return fmt.Errorf("alipay app_id missing")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("alipay app_id mismatch: expected %s, got %s", expected, actual)
			}
		}
	case payment.TypeEasyPay:
		if expected := strings.TrimSpace(snapshot.MerchantID); expected != "" {
			actual := strings.TrimSpace(metadata["pid"])
			if actual == "" {
				return fmt.Errorf("easypay pid missing")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("easypay pid mismatch: expected %s, got %s", expected, actual)
			}
		}
	case payment.TypeStripe:
		if expected := strings.TrimSpace(snapshot.Currency); expected != "" {
			actual := strings.ToUpper(strings.TrimSpace(metadata["currency"]))
			if actual == "" {
				return fmt.Errorf("stripe notification missing currency")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("stripe currency mismatch: expected %s, got %s", expected, actual)
			}
		}
	case payment.TypeAirwallex:
		if expected := strings.TrimSpace(snapshot.MerchantID); expected != "" {
			actual := strings.TrimSpace(metadata["account_id"])
			if actual == "" {
				return fmt.Errorf("airwallex account_id missing")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("airwallex account_id mismatch: expected %s, got %s", expected, actual)
			}
		}
		if expected := strings.TrimSpace(snapshot.Currency); expected != "" {
			actual := strings.ToUpper(strings.TrimSpace(metadata["currency"]))
			if actual == "" {
				return fmt.Errorf("airwallex notification missing currency")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("airwallex currency mismatch: expected %s, got %s", expected, actual)
			}
		}
		if actual := strings.TrimSpace(metadata["status"]); actual != "" && !strings.EqualFold(actual, "SUCCEEDED") {
			return fmt.Errorf("airwallex status mismatch: expected SUCCEEDED, got %s", actual)
		}
	case payment.TypeWise:
		if expected := strings.TrimSpace(snapshot.MerchantID); expected != "" {
			actual := strings.TrimSpace(metadata["profile_id"])
			if actual == "" {
				actual = strings.TrimSpace(metadata["merchant_id"])
			}
			if actual == "" {
				return fmt.Errorf("wise profile_id missing")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("wise profile_id mismatch: expected %s, got %s", expected, actual)
			}
		}
		if expected := strings.TrimSpace(snapshot.BalanceID); expected != "" {
			actual := strings.TrimSpace(metadata["balance_id"])
			if actual == "" {
				return fmt.Errorf("wise balance_id missing")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("wise balance_id mismatch: expected %s, got %s", expected, actual)
			}
		}
		if expected := strings.TrimSpace(snapshot.Currency); expected != "" {
			actual := strings.ToUpper(strings.TrimSpace(metadata["currency"]))
			if actual == "" {
				return fmt.Errorf("wise notification missing currency")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("wise currency mismatch: expected %s, got %s", expected, actual)
			}
		}
		if expected := strings.TrimSpace(snapshot.SettlementStrategy); expected != "" {
			actual := strings.TrimSpace(metadata["settlement_strategy"])
			if actual == "" {
				return fmt.Errorf("wise settlement_strategy missing")
			}
			if !strings.EqualFold(expected, actual) {
				return fmt.Errorf("wise settlement_strategy mismatch: expected %s, got %s", expected, actual)
			}
		}
	}

	return nil
}

func providerMerchantIdentityMetadata(prov payment.Provider) map[string]string {
	if prov == nil {
		return nil
	}
	reporter, ok := prov.(payment.MerchantIdentityProvider)
	if !ok {
		return nil
	}
	return reporter.MerchantIdentityMetadata()
}
