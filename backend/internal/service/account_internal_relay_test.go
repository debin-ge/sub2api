//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
	"github.com/stretchr/testify/require"
)

func TestAccountIsInternalRelay(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "nil", account: nil},
		{name: "missing extra", account: &Account{}},
		{name: "missing key", account: &Account{Extra: map[string]any{}}},
		{name: "false", account: &Account{Extra: map[string]any{InternalRelayExtraKey: false}}},
		{name: "malformed", account: &Account{Extra: map[string]any{InternalRelayExtraKey: "true"}}},
		{name: "true", account: &Account{Extra: map[string]any{InternalRelayExtraKey: true}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.IsInternalRelay())
		})
	}
}

func TestNormalizeInternalRelayCreateExtra(t *testing.T) {
	validCredentials := map[string]any{"base_url": "http://127.0.0.1:8080", "api_key": "secret"}

	t.Run("eligible API key account is enabled and preserves unknown extra", func(t *testing.T) {
		extra, err := normalizeInternalRelayCreateExtra(
			PlatformOpenAI,
			AccountTypeAPIKey,
			validCredentials,
			map[string]any{InternalRelayExtraKey: true, "future_key": "keep"},
		)

		require.NoError(t, err)
		require.Equal(t, true, extra[InternalRelayExtraKey])
		require.Equal(t, "keep", extra["future_key"])
	})

	for _, platform := range []string{
		PlatformAnthropic,
		PlatformOpenAI,
		PlatformGemini,
		PlatformGrok,
		PlatformWindsurf,
		PlatformOpenCode,
	} {
		t.Run("supports "+platform+" API key", func(t *testing.T) {
			extra, err := normalizeInternalRelayCreateExtra(
				platform,
				AccountTypeAPIKey,
				validCredentials,
				map[string]any{InternalRelayExtraKey: true},
			)

			require.NoError(t, err)
			require.Equal(t, true, extra[InternalRelayExtraKey])
		})
	}

	for _, platform := range []string{PlatformMiniMax, PlatformGLM, PlatformKimi, PlatformDeepSeek} {
		t.Run("supports dual endpoints for "+platform, func(t *testing.T) {
			extra, err := normalizeInternalRelayCreateExtra(
				platform,
				AccountTypeAPIKey,
				map[string]any{
					"api_key":            "secret",
					"base_url_anthropic": "http://127.0.0.1:8080",
					"base_url_openai":    "http://localhost:8080/v1",
				},
				map[string]any{InternalRelayExtraKey: true},
			)

			require.NoError(t, err)
			require.Equal(t, true, extra[InternalRelayExtraKey])
		})
	}

	t.Run("false is removed instead of persisted", func(t *testing.T) {
		extra, err := normalizeInternalRelayCreateExtra(
			PlatformOpenAI,
			AccountTypeAPIKey,
			validCredentials,
			map[string]any{InternalRelayExtraKey: false, "future_key": "keep"},
		)

		require.NoError(t, err)
		require.NotContains(t, extra, InternalRelayExtraKey)
		require.Equal(t, "keep", extra["future_key"])
	})

	for _, tc := range []struct {
		name        string
		platform    string
		accountType string
		baseURL     string
	}{
		{name: "OAuth is unsupported", platform: PlatformOpenAI, accountType: AccountTypeOAuth, baseURL: "http://127.0.0.1:8080"},
		{name: "non-loopback URL is rejected", platform: PlatformOpenAI, accountType: AccountTypeAPIKey, baseURL: "https://api.openai.com"},
		{name: "URL without HTTP scheme is rejected", platform: PlatformOpenAI, accountType: AccountTypeAPIKey, baseURL: "127.0.0.1:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeInternalRelayCreateExtra(
				tc.platform,
				tc.accountType,
				map[string]any{"base_url": tc.baseURL},
				map[string]any{InternalRelayExtraKey: true},
			)

			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
		})
	}

	t.Run("dual endpoint platform rejects one external endpoint", func(t *testing.T) {
		_, err := normalizeInternalRelayCreateExtra(
			PlatformGLM,
			AccountTypeAPIKey,
			map[string]any{
				"base_url_anthropic": "http://127.0.0.1:8080",
				"base_url_openai":    "https://open.bigmodel.cn/api/coding/paas/v4",
			},
			map[string]any{InternalRelayExtraKey: true},
		)

		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	})

	t.Run("malformed flag is rejected", func(t *testing.T) {
		_, err := normalizeInternalRelayCreateExtra(
			PlatformOpenAI,
			AccountTypeAPIKey,
			validCredentials,
			map[string]any{InternalRelayExtraKey: "true"},
		)

		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	})
}

func TestNormalizeInternalRelayUpdateExtra(t *testing.T) {
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "http://localhost:8080", "api_key": "stored"},
		Extra:       map[string]any{InternalRelayExtraKey: true, "future_key": "keep"},
	}

	t.Run("explicit false removes flag and preserves unknown extra", func(t *testing.T) {
		input := &UpdateAccountInput{Extra: map[string]any{InternalRelayExtraKey: false, "future_key": "keep"}}
		normalized, err := normalizeInternalRelayUpdateExtra(account, input, input.Extra)

		require.NoError(t, err)
		require.NotContains(t, normalized, InternalRelayExtraKey)
		require.Equal(t, "keep", normalized["future_key"])
	})

	t.Run("omitted flag remains enabled", func(t *testing.T) {
		input := &UpdateAccountInput{Extra: map[string]any{"future_key": "keep"}}
		normalized, err := normalizeInternalRelayUpdateExtra(account, input, input.Extra)

		require.NoError(t, err)
		require.Equal(t, true, normalized[InternalRelayExtraKey])
		require.Equal(t, "keep", normalized["future_key"])
	})

	t.Run("enabled account cannot be changed to non-loopback URL", func(t *testing.T) {
		_, err := normalizeInternalRelayUpdateExtra(account, &UpdateAccountInput{
			Credentials: map[string]any{"base_url": "https://api.openai.com"},
		}, nil)

		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	})

	t.Run("enabled account cannot be changed away from API key", func(t *testing.T) {
		_, err := normalizeInternalRelayUpdateExtra(account, &UpdateAccountInput{Type: AccountTypeOAuth}, nil)

		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	})
}

func TestApplyInternalRelayHeaderWithSecret(t *testing.T) {
	const secret = "test-jwt-secret-32-bytes-long-value"
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "http://[::1]:8080", "api_key": "stored"},
		Extra:       map[string]any{InternalRelayExtraKey: true},
	}
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "outer-request")
	headers := make(http.Header)
	headers.Set(internalrelay.HeaderName, "forged-value")

	applyInternalRelayHeaderWithSecret(ctx, secret, account, headers)

	marker := headers.Get(internalrelay.HeaderName)
	require.NotEmpty(t, marker)
	require.NotEqual(t, "forged-value", marker)
	metadata, err := internalrelay.NewSigner(secret).Verify(marker, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(42), metadata.AccountID)
	require.Equal(t, "client:outer-request", metadata.ParentRequestID)

	t.Run("disabled account removes caller supplied marker", func(t *testing.T) {
		disabled := *account
		disabled.Extra = nil
		disabledHeaders := make(http.Header)
		disabledHeaders.Set(internalrelay.HeaderName, "forged-value")

		applyInternalRelayHeaderWithSecret(ctx, secret, &disabled, disabledHeaders)

		require.Empty(t, disabledHeaders.Get(internalrelay.HeaderName))
	})

	t.Run("missing client request ID does not create marker", func(t *testing.T) {
		missingIDHeaders := make(http.Header)
		missingIDHeaders.Set(internalrelay.HeaderName, "forged-value")

		applyInternalRelayHeaderWithSecret(context.Background(), secret, account, missingIDHeaders)

		require.Empty(t, missingIDHeaders.Get(internalrelay.HeaderName))
	})
}

func TestApplyInternalRelayHeaderFromContextSupportsNonOpenAIAPIKey(t *testing.T) {
	const secret = "test-jwt-secret-32-bytes-long-value"
	account := &Account{
		ID:          84,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "http://localhost:8080", "api_key": "stored"},
		Extra:       map[string]any{InternalRelayExtraKey: true},
	}
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "outer-anthropic")
	ctx = context.WithValue(ctx, ctxkey.InternalRelaySigner, internalrelay.NewSigner(secret))
	headers := make(http.Header)

	applyInternalRelayHeaderFromContext(ctx, account, headers)

	metadata, err := internalrelay.NewSigner(secret).Verify(headers.Get(internalrelay.HeaderName), time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(84), metadata.AccountID)
	require.Equal(t, "client:outer-anthropic", metadata.ParentRequestID)
}

func TestValidateInternalRelayOrUpstreamBaseURLAllowsValidatedLoopback(t *testing.T) {
	account := &Account{
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "http://127.0.0.1:8080/"},
		Extra:       map[string]any{InternalRelayExtraKey: true},
	}
	fallbackCalled := false

	normalized, err := validateInternalRelayOrUpstreamBaseURL(account, "http://127.0.0.1:8080/", func(string) (string, error) {
		fallbackCalled = true
		return "", errors.New("operator policy rejected loopback")
	})

	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8080", normalized)
	require.False(t, fallbackCalled)
}
