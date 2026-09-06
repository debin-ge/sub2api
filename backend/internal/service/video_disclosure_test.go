package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestEffectiveVideoDisclosurePolicyUsesMostRestrictiveScope(t *testing.T) {
	tests := []struct {
		name                   string
		global, group, account string
		want                   string
	}{
		{
			name:   "empty scoped policies inherit global",
			global: config.VideoDisclosureDedicatedCredentials,
			want:   config.VideoDisclosureDedicatedCredentials,
		},
		{
			name:    "group restricts account and global",
			global:  config.VideoDisclosureDedicatedCredentials,
			group:   config.VideoDisclosureIdentity,
			account: config.VideoDisclosureTaskAccess,
			want:    config.VideoDisclosureIdentity,
		},
		{
			name:    "account can disable disclosure",
			global:  config.VideoDisclosureDedicatedCredentials,
			group:   config.VideoDisclosureTaskAccess,
			account: config.VideoDisclosureNone,
			want:    config.VideoDisclosureNone,
		},
		{
			name:    "invalid scoped policy fails closed",
			global:  config.VideoDisclosureTaskAccess,
			account: "future-policy",
			want:    config.VideoDisclosureNone,
		},
		{
			name:   "invalid global policy fails closed",
			global: "",
			want:   config.VideoDisclosureNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, effectiveVideoDisclosurePolicy(tt.global, tt.group, tt.account))
		})
	}
}

func TestValidateVideoAccountDisclosureRequiresOwnedAPIKeyForDedicatedCredentials(t *testing.T) {
	ownerID := int64(42)
	tests := []struct {
		name    string
		account Account
		wantErr error
	}{
		{
			name:    "owned API key is valid",
			account: Account{Type: AccountTypeAPIKey, VideoOwnerUserID: &ownerID, VideoDisclosurePolicy: config.VideoDisclosureDedicatedCredentials},
		},
		{
			name:    "owner is required",
			account: Account{Type: AccountTypeAPIKey, VideoDisclosurePolicy: config.VideoDisclosureDedicatedCredentials},
			wantErr: errVideoDedicatedOwnerRequired,
		},
		{
			name:    "API key account is required",
			account: Account{Type: AccountTypeOAuth, VideoOwnerUserID: &ownerID, VideoDisclosurePolicy: config.VideoDisclosureDedicatedCredentials},
			wantErr: errVideoDedicatedAPIKeyRequired,
		},
		{
			name:    "invalid policy is rejected",
			account: Account{Type: AccountTypeAPIKey, VideoOwnerUserID: &ownerID, VideoDisclosurePolicy: "all"},
			wantErr: errVideoDisclosurePolicyInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVideoAccountDisclosure(&tt.account)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, 400, infraerrors.Code(err))
		})
	}
}
