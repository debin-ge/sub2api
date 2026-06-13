//go:build unit

package provider

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestCreateProviderWise(t *testing.T) {
	t.Parallel()

	prov, err := CreateProvider(payment.TypeWise, "1", map[string]string{
		"quickPayBaseUrl":    "https://wise.com/pay/business/account",
		"apiBase":            "https://api.wise.com",
		"apiToken":           "token-123",
		"profileId":          "profile-123",
		"balanceId":          "balance-123",
		"currency":           "USD",
		"webhookPublicKey":   testWisePublicKeyPEM,
		"settlementStrategy": "exact_only",
	})
	require.NoError(t, err)
	require.Equal(t, payment.TypeWise, prov.ProviderKey())
	require.Equal(t, []payment.PaymentType{payment.TypeWise}, prov.SupportedTypes())
}

const testWisePublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwj/6fR4W9HutC0Dh9CDk
gOmqZp3esJwprRXb1p6BV9kPfLQOELutQKiqSgNZW5eSKrYpR4xZJg1Aht2nDHNH
RceCKJxaj0QyJzUtMuD1qH9OGqHMWwctQJaMYhzGzS8xZpblNf3WFYsD+iHqJq1E
0U1t6fXB+QwBfQkM3RI/cDyJiH2+trTcTHFIAxchkw+Q2U8BfV02+U5tNNfrjxpt
xHVEH3qPXUgoSQP6sE8MT2mSOjrU8bS7dK7gxsVDjLMsMyHIyASenxcrS5U1XyXm
8Xx+uJtD3Z3RCRlDa67S0twTP+w6S0s2BguMQ8s0F+BAFfZf4d1FiaNmN7uwqblP
NQIDAQAB
-----END PUBLIC KEY-----`
