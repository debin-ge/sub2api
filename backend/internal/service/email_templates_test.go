//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func requireUpdatedEmailShell(t *testing.T, body string) {
	t.Helper()

	require.Contains(t, body, "brand-bar")
	require.Contains(t, body, "box-shadow:0 8px 28px")
	require.NotContains(t, body, "linear-gradient(135deg")
	require.NotContains(t, body, "Sub2API")
	require.NotContains(t, body, "%!")
	require.NotContains(t, body, "MISSING")
	require.NotContains(t, body, "EXTRA")
}

func TestVerifyCodeEmailBodyUsesConfiguredSiteNameAndUpdatedShell(t *testing.T) {
	body := (&EmailService{}).buildVerifyCodeEmailBody("123456", "Acme API")

	require.Contains(t, body, "Acme API")
	require.Contains(t, body, "123456")
	requireUpdatedEmailShell(t, body)
}

func TestVerifyCodeEmailBodyDoesNotExposeProductFallback(t *testing.T) {
	body := (&EmailService{}).buildVerifyCodeEmailBody("123456", "Sub2API")

	require.NotContains(t, body, "Sub2API")
	require.Contains(t, body, "123456")
}

func TestPasswordResetEmailBodyUsesConfiguredSiteNameAndUpdatedShell(t *testing.T) {
	body := (&EmailService{}).buildPasswordResetEmailBody("https://example.com/reset?token=abc", "Acme API")

	require.Contains(t, body, "Acme API")
	require.Contains(t, body, `href="https://example.com/reset?token=abc"`)
	requireUpdatedEmailShell(t, body)
}

func TestPasswordResetEmailBodyDoesNotExposeProductFallback(t *testing.T) {
	body := (&EmailService{}).buildPasswordResetEmailBody("https://example.com/reset?token=abc", "Sub2API")

	require.NotContains(t, body, "Sub2API")
	require.Contains(t, body, "https://example.com/reset?token=abc")
}

func TestNotifyVerifyEmailBodyUsesConfiguredSiteNameAndUpdatedShell(t *testing.T) {
	body := buildNotifyVerifyEmailBody("654321", "Acme API")

	require.Contains(t, body, "Acme API")
	require.Contains(t, body, "654321")
	requireUpdatedEmailShell(t, body)
}

func TestNotifyVerifyEmailBodyDoesNotExposeProductFallback(t *testing.T) {
	body := buildNotifyVerifyEmailBody("654321", "Sub2API")

	require.NotContains(t, body, "Sub2API")
	require.Contains(t, body, "654321")
}

func TestBalanceEmailBodiesUseConfiguredSiteNameAndUpdatedShell(t *testing.T) {
	s := &BalanceNotifyService{}

	lowBody := s.buildBalanceLowEmailBody("Alice", 3.14, 10.0, "Acme API", "")
	require.Contains(t, lowBody, "Acme API")
	requireUpdatedEmailShell(t, lowBody)

	quotaBody := s.buildQuotaAlertEmailBody(42, "acc-foo", "openai", "日限额 / Daily", 750.50, 1000, 249.50, "$249.50", "Acme API")
	require.Contains(t, quotaBody, "Acme API")
	requireUpdatedEmailShell(t, quotaBody)
}

func TestBalanceSiteNameFallbackDoesNotExposeProductName(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()

	require.NotEqual(t, "Sub2API", s.getSiteName(t.Context()))
}
