package service

import "testing"

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelsListConfig(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after models_list_config was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV15AuthSnapshotWithoutReasoningEffortPolicy(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-reasoning-mappings", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 15},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v15 auth snapshot to be rejected after reasoning effort policy was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsCurrentAuthSnapshotWithMissingVIPFields(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}
	autoMode := VIPModeAuto
	paymentRequired := VIPAccessStatePaymentRequired

	t.Run("missing user is_vip", func(t *testing.T) {
		apiKey, ok, err := svc.applyAuthCacheEntry("k-v19-missing-user-vip", &APIKeyAuthCacheEntry{
			Snapshot: &APIKeyAuthSnapshot{
				Version:  apiKeyAuthSnapshotVersion,
				APIKeyID: 1,
				UserID:   2,
				User:     APIKeyAuthUserSnapshot{ID: 2},
			},
		})
		if err != nil {
			t.Fatalf("expected incomplete snapshot to be ignored without error, got %v", err)
		}
		if ok || apiKey != nil {
			t.Fatalf("expected incomplete v19 user snapshot to miss, got ok=%v apiKey=%#v", ok, apiKey)
		}
	})

	t.Run("missing user vip mode", func(t *testing.T) {
		apiKey, ok, err := svc.applyAuthCacheEntry("k-v19-missing-user-vip-mode", &APIKeyAuthCacheEntry{
			Snapshot: &APIKeyAuthSnapshot{
				Version:  apiKeyAuthSnapshotVersion,
				APIKeyID: 1,
				UserID:   2,
				User: APIKeyAuthUserSnapshot{
					ID:             2,
					IsVIP:          authSnapshotBool(false),
					VIPAccessState: &paymentRequired,
				},
			},
		})
		if err != nil {
			t.Fatalf("expected incomplete snapshot to be ignored without error, got %v", err)
		}
		if ok || apiKey != nil {
			t.Fatalf("expected incomplete v19 user snapshot to miss, got ok=%v apiKey=%#v", ok, apiKey)
		}
	})

	t.Run("missing group vip_only", func(t *testing.T) {
		apiKey, ok, err := svc.applyAuthCacheEntry("k-v19-missing-group-vip", &APIKeyAuthCacheEntry{
			Snapshot: &APIKeyAuthSnapshot{
				Version:  apiKeyAuthSnapshotVersion,
				APIKeyID: 1,
				UserID:   2,
				GroupID:  &groupID,
				User: APIKeyAuthUserSnapshot{
					ID:             2,
					IsVIP:          authSnapshotBool(false),
					VIPMode:        &autoMode,
					VIPAccessState: &paymentRequired,
				},
				Group: &APIKeyAuthGroupSnapshot{ID: groupID},
			},
		})
		if err != nil {
			t.Fatalf("expected incomplete snapshot to be ignored without error, got %v", err)
		}
		if ok || apiKey != nil {
			t.Fatalf("expected incomplete v19 group snapshot to miss, got ok=%v apiKey=%#v", ok, apiKey)
		}
	})
}

func TestAPIKeyService_CurrentAuthSnapshotRoundTripPreservesVIPAuthorization(t *testing.T) {
	groupID := int64(9)
	forceOff := false
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(t.Context(), &APIKey{
		ID:      1,
		UserID:  2,
		GroupID: &groupID,
		Status:  StatusActive,
		User: &User{
			ID:     2,
			Status: StatusActive,
			VIPEntitlementSnapshot: VIPEntitlementSnapshot{
				IsVIP:          false,
				ManualOverride: &forceOff,
			},
		},
		Group: &Group{
			ID:               groupID,
			Status:           StatusActive,
			SubscriptionType: SubscriptionTypeStandard,
			VIPOnly:          true,
		},
	})

	if snapshot == nil ||
		snapshot.User.IsVIP == nil ||
		snapshot.User.VIPMode == nil ||
		snapshot.User.VIPAccessState == nil ||
		snapshot.Group == nil ||
		snapshot.Group.VIPOnly == nil {
		t.Fatalf("expected complete v19 VIP authorization snapshot, got %#v", snapshot)
	}
	apiKey, ok, err := svc.applyAuthCacheEntry("k-v19", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	if err != nil || !ok {
		t.Fatalf("expected complete v19 snapshot to apply, ok=%v err=%v", ok, err)
	}
	if apiKey == nil ||
		apiKey.User == nil ||
		apiKey.User.IsVIP ||
		apiKey.User.ManualOverride == nil ||
		*apiKey.User.ManualOverride ||
		apiKey.User.AccessState() != VIPAccessStateRestricted ||
		apiKey.Group == nil ||
		!apiKey.Group.VIPOnly {
		t.Fatalf("expected VIP fields after roundtrip, got %#v", apiKey)
	}
}
