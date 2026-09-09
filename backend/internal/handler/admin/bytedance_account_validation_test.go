package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestValidateByteDanceAccountRequests(t *testing.T) {
	valid := CreateAccountRequest{
		Platform: service.PlatformByteDance,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "secret",
		},
	}
	require.NoError(t, validateCreateAccountRequest(valid))

	withURL := valid
	withURL.Credentials = map[string]any{"api_key": "secret", "base_url": "https://video.example/v1"}
	require.NoError(t, validateCreateAccountRequest(withURL))

	invalidURL := valid
	invalidURL.Credentials = map[string]any{"api_key": "secret", "base_url": "://bad"}
	require.ErrorContains(t, validateCreateAccountRequest(invalidURL), "base_url is invalid")

	oauth := valid
	oauth.Type = service.AccountTypeOAuth
	require.ErrorContains(t, validateCreateAccountRequest(oauth), "must be apikey")

	missingKey := valid
	missingKey.Credentials = map[string]any{}
	require.ErrorContains(t, validateCreateAccountRequest(missingKey), "api_key is required")

	account := &service.Account{Platform: service.PlatformByteDance, Type: service.AccountTypeAPIKey, Credentials: valid.Credentials}
	require.NoError(t, validateUpdateAccountRequest(account, UpdateAccountRequest{Credentials: map[string]any{"base_url": "https://private.example/v1"}}))
	require.ErrorContains(t, validateUpdateAccountRequest(account, UpdateAccountRequest{Credentials: map[string]any{"base_url": "bad"}}), "base_url is invalid")
}
