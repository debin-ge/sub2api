package handler

import (
	"encoding/json"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestSanitizePaymentOrderForResponseIncludesProviderKey(t *testing.T) {
	t.Parallel()

	providerKey := payment.TypeStripe
	result := sanitizePaymentOrderForResponse(&dbent.PaymentOrder{
		ID:          42,
		PaymentType: payment.TypeWxpay,
		ProviderKey: &providerKey,
	})
	if result == nil {
		t.Fatal("sanitizePaymentOrderForResponse returned nil")
	}
	if result.ProviderKey == nil || *result.ProviderKey != payment.TypeStripe {
		t.Fatalf("provider_key = %v, want %q", result.ProviderKey, payment.TypeStripe)
	}

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal PaymentOrderResult: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal PaymentOrderResult: %v", err)
	}
	if got := payload["provider_key"]; got != payment.TypeStripe {
		t.Fatalf("JSON provider_key = %v, want %q", got, payment.TypeStripe)
	}
}

func TestSanitizePaymentOrderForResponseOmitsMissingProviderKey(t *testing.T) {
	t.Parallel()

	result := sanitizePaymentOrderForResponse(&dbent.PaymentOrder{ID: 42})
	if result == nil {
		t.Fatal("sanitizePaymentOrderForResponse returned nil")
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal PaymentOrderResult: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal PaymentOrderResult: %v", err)
	}
	if _, ok := payload["provider_key"]; ok {
		t.Fatalf("JSON unexpectedly contains provider_key: %s", body)
	}
}

func TestSignedPublicOrderResultIncludesProviderKeyButLegacyResultDoesNot(t *testing.T) {
	t.Parallel()

	providerKey := payment.TypeStripe
	order := &dbent.PaymentOrder{
		ID:          42,
		OutTradeNo:  "sub2_stripe_wxpay_42",
		PaymentType: payment.TypeWxpay,
		ProviderKey: &providerKey,
	}

	signedBody, err := json.Marshal(buildPublicOrderResult(order))
	if err != nil {
		t.Fatalf("marshal PublicOrderResult: %v", err)
	}
	var signedPayload map[string]any
	if err := json.Unmarshal(signedBody, &signedPayload); err != nil {
		t.Fatalf("unmarshal PublicOrderResult: %v", err)
	}
	if got := signedPayload["provider_key"]; got != payment.TypeStripe {
		t.Fatalf("signed JSON provider_key = %v, want %q", got, payment.TypeStripe)
	}

	legacyBody, err := json.Marshal(buildPublicOrderVerifyResult(order))
	if err != nil {
		t.Fatalf("marshal PublicOrderVerifyResult: %v", err)
	}
	var legacyPayload map[string]any
	if err := json.Unmarshal(legacyBody, &legacyPayload); err != nil {
		t.Fatalf("unmarshal PublicOrderVerifyResult: %v", err)
	}
	if _, ok := legacyPayload["provider_key"]; ok {
		t.Fatalf("legacy anonymous JSON unexpectedly contains provider_key: %s", legacyBody)
	}
}
