//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderwebhooksubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

type wiseSubscriptionCreatorStub struct {
	req   provider.WiseProfileSubscriptionRequest
	calls int
	err   error

	deleteErr error
	deleted   []string
}

func (s *wiseSubscriptionCreatorStub) CreateProfileSubscription(_ context.Context, req provider.WiseProfileSubscriptionRequest) (*provider.WiseProfileSubscriptionResponse, error) {
	s.calls++
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return &provider.WiseProfileSubscriptionResponse{
		ID:        "subscription-123",
		TriggerOn: req.TriggerOn,
		Delivery: struct {
			Version string `json:"version"`
			URL     string `json:"url"`
		}{
			Version: req.DeliveryVersion,
			URL:     req.DeliveryURL,
		},
	}, nil
}

func (s *wiseSubscriptionCreatorStub) DeleteProfileSubscription(_ context.Context, profileID string, subscriptionID string) error {
	s.deleted = append(s.deleted, profileID+":"+subscriptionID)
	return s.deleteErr
}

func TestEnsureWiseWebhookSubscriptionCreatesRequestAndPersistsActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := &PaymentConfigService{
		entClient: client,
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return creator
		},
		webhookBaseURLResolver: func(context.Context) string {
			return "https://api.example.com/"
		},
	}
	config := validWiseProviderConfigForConfigService(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.ensureWiseWebhookSubscription(ctx, inst, config))

	require.Equal(t, "profile-123", creator.req.ProfileID)
	require.Equal(t, "Sub2API Wise balance credit - 1", creator.req.Name)
	require.Equal(t, "balances#credit", creator.req.TriggerOn)
	require.Equal(t, "4.0.0", creator.req.DeliveryVersion)
	require.Equal(t, "https://api.example.com/api/v1/payment/webhook/wise", creator.req.DeliveryURL)

	sub, err := client.PaymentProviderWebhookSubscription.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(inst.ID), sub.ProviderInstanceID)
	require.Equal(t, payment.TypeWise, sub.ProviderKey)
	require.Equal(t, "subscription-123", sub.ExternalSubscriptionID)
	require.Equal(t, "balances#credit", sub.TriggerOn)
	require.Equal(t, "4.0.0", sub.DeliveryVersion)
	require.Equal(t, "https://api.example.com/api/v1/payment/webhook/wise", sub.DeliveryURL)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, sub.Status)
	require.Nil(t, sub.LastError)
	require.NotNil(t, sub.SyncedAt)
}

func TestDeleteWiseWebhookSubscriptionDeletesRemoteAndMarksDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := &PaymentConfigService{
		entClient: client,
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return creator
		},
	}
	config := validWiseProviderConfigForConfigService(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	createActiveWiseWebhookSubscriptionForTest(t, ctx, client, inst.ID, "subscription-delete-123")

	err = svc.deleteWiseWebhookSubscription(ctx, inst, config)

	require.NoError(t, err)
	require.Equal(t, []string{"profile-123:subscription-delete-123"}, creator.deleted)
	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusDeleted, sub.Status)
	require.Equal(t, "subscription-delete-123", sub.ExternalSubscriptionID)
	require.Nil(t, sub.LastError)
	require.NotNil(t, sub.SyncedAt)
}

func TestEnsureWiseWebhookSubscriptionDryRunCreatesRemoteButDoesNotPersist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := &PaymentConfigService{
		entClient: client,
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return creator
		},
		webhookBaseURLResolver: func(context.Context) string {
			return "https://api.example.com"
		},
	}
	config := validWiseProviderConfigForConfigService(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.upsertWiseWebhookSubscription(
		ctx,
		inst,
		"existing-subscription-id",
		"https://api.example.com/api/v1/payment/webhook/wise",
		wiseWebhookSubscriptionStatusActive,
		"",
	))

	result, err := svc.ensureWiseWebhookSubscriptionDryRun(ctx, inst, config)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.RemoteCreated)
	require.Equal(t, "subscription-123", result.ExternalID)
	require.Equal(t, "https://api.example.com/api/v1/payment/webhook/wise", result.DeliveryURL)

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, sub.Status)
	require.Equal(t, "existing-subscription-id", sub.ExternalSubscriptionID)
	require.Nil(t, sub.LastError)
}

func TestEnsureWiseWebhookSubscriptionDeliveryURLChangeUpdatesSameRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	baseURL := "https://api-one.example.com"
	svc := &PaymentConfigService{
		entClient: client,
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return creator
		},
		webhookBaseURLResolver: func(context.Context) string {
			return baseURL
		},
	}
	config := validWiseProviderConfigForConfigService(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.ensureWiseWebhookSubscription(ctx, inst, config))
	first, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "https://api-one.example.com/api/v1/payment/webhook/wise", first.DeliveryURL)

	baseURL = "https://api-two.example.com"
	require.NoError(t, svc.ensureWiseWebhookSubscription(ctx, inst, config))

	count, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	updated, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, first.ID, updated.ID)
	require.Equal(t, "https://api-two.example.com/api/v1/payment/webhook/wise", updated.DeliveryURL)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, updated.Status)
}

func TestUpdateProviderInstanceEnabledWiseAPITokenRotationReusesActiveSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, creator.calls)

	err = client.PaymentProviderWebhookSubscription.Update().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		SetExternalSubscriptionID("existing-subscription-id").
		Exec(ctx)
	require.NoError(t, err)
	creator.err = errors.New("remote should not be called")

	updated, err := svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"apiToken": "rotated-token"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, creator.calls)

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "existing-subscription-id", sub.ExternalSubscriptionID)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, sub.Status)
}

func TestEnsureWiseWebhookSubscriptionFailurePersistsFailedAndMasksToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{err: errors.New("remote rejected token-test")}
	svc := &PaymentConfigService{
		entClient: client,
		wiseSubscriptionClientFactory: func(map[string]string) wiseProfileSubscriptionClient {
			return creator
		},
		webhookBaseURLResolver: func(context.Context) string {
			return "https://api.example.com"
		},
	}
	config := validWiseProviderConfigForConfigService(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	err = svc.ensureWiseWebhookSubscription(ctx, inst, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wise webhook subscription failed")
	require.NotContains(t, err.Error(), "token-test")

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusFailed, sub.Status)
	require.NotNil(t, sub.LastError)
	require.Contains(t, *sub.LastError, "[REDACTED]")
	require.NotContains(t, *sub.LastError, "token-test")
	require.NotNil(t, sub.SyncedAt)
}

func TestEnsureWiseWebhookSubscriptionNilResolverPersistsFailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{entClient: client}
	config := validWiseProviderConfigForConfigService(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWise).
		SetName("wise").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeWise).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	err = svc.ensureWiseWebhookSubscription(ctx, inst, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wise webhook subscription failed")
	require.Contains(t, err.Error(), "wise webhook base URL is required")
	require.Contains(t, err.Error(), "API_BASE_URL")

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusFailed, sub.Status)
	require.NotNil(t, sub.LastError)
	require.Contains(t, *sub.LastError, "wise webhook base URL is required")
	require.Contains(t, *sub.LastError, "API_BASE_URL")
}

func TestCreateProviderInstanceEnabledWiseEnsuresWebhookSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	require.NotNil(t, inst)

	require.Equal(t, "profile-123", creator.req.ProfileID)
	require.Equal(t, "Sub2API Wise balance credit - 1", creator.req.Name)

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, sub.Status)
	require.Equal(t, "https://api.example.com/api/v1/payment/webhook/wise", sub.DeliveryURL)
}

func TestUpdateProviderInstanceDisabledWiseEnableFailureLeavesProviderDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{err: errors.New("remote rejected token-test")}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        false,
	})
	require.NoError(t, err)

	updated, err := svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Enabled: boolPtrValue(true),
	})
	require.Error(t, err)
	require.Nil(t, updated)
	require.NotContains(t, err.Error(), "token-test")

	saved, err := client.PaymentProviderInstance.Get(ctx, inst.ID)
	require.NoError(t, err)
	require.False(t, saved.Enabled)

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusFailed, sub.Status)
}

func createActiveWiseWebhookSubscriptionForTest(t *testing.T, ctx context.Context, client *dbent.Client, instID int64, externalID string) {
	t.Helper()

	_, err := client.PaymentProviderWebhookSubscription.Create().
		SetProviderInstanceID(instID).
		SetProviderKey(payment.TypeWise).
		SetExternalSubscriptionID(externalID).
		SetTriggerOn(wiseWebhookSubscriptionTrigger).
		SetDeliveryVersion(wiseWebhookSubscriptionDeliveryVersion).
		SetDeliveryURL("https://api.example.com/api/v1/payment/webhook/wise").
		SetStatus(wiseWebhookSubscriptionStatusActive).
		SetSyncedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)
}

func TestUpdateProviderInstanceEnabledWiseProfileChangeSubscriptionFailureKeepsExistingConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}
	originalConfig := validWiseProviderConfigForConfigService(t)

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         originalConfig,
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, creator.calls)
	err = client.PaymentProviderWebhookSubscription.Update().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		SetExternalSubscriptionID("existing-subscription-id").
		Exec(ctx)
	require.NoError(t, err)
	creator.err = errors.New("remote rejected profile change token-test")

	updated, err := svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"profileId": "profile-updated"},
	})
	require.Error(t, err)
	require.Nil(t, updated)
	require.NotContains(t, err.Error(), "token-test")
	require.Equal(t, 2, creator.calls)

	saved, err := client.PaymentProviderInstance.Get(ctx, inst.ID)
	require.NoError(t, err)
	require.True(t, saved.Enabled)
	savedConfig, err := svc.decryptConfig(saved.Config)
	require.NoError(t, err)
	require.Equal(t, originalConfig["profileId"], savedConfig["profileId"])

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, sub.Status)
	require.Equal(t, "existing-subscription-id", sub.ExternalSubscriptionID)
	require.Nil(t, sub.LastError)
}

func TestUpdateProviderInstanceEnabledWiseProfileChangeDeletesOldRemoteSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, creator.calls)
	err = client.PaymentProviderWebhookSubscription.Update().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		SetExternalSubscriptionID("old-subscription-id").
		Exec(ctx)
	require.NoError(t, err)

	updated, err := svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"profileId": "profile-updated"},
	})

	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.Equal(t, 2, creator.calls)
	require.Equal(t, "profile-updated", creator.req.ProfileID)
	require.Equal(t, []string{"profile-123:old-subscription-id"}, creator.deleted)
	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, sub.Status)
	require.Equal(t, "subscription-123", sub.ExternalSubscriptionID)
}

func TestUpdateProviderInstanceDisabledWiseRemoteTargetChangeKeepsDeletedSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, creator.calls)

	err = client.PaymentProviderWebhookSubscription.Update().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		SetExternalSubscriptionID("old-subscription-id").
		Exec(ctx)
	require.NoError(t, err)

	updated, err := svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Enabled: boolPtrValue(false),
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled)

	updated, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"profileId": "profile-updated"},
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled)

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusDeleted, sub.Status)
	require.Equal(t, "old-subscription-id", sub.ExternalSubscriptionID)
	require.Equal(t, []string{"profile-123:old-subscription-id"}, creator.deleted)

	updated, err = svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Enabled: boolPtrValue(true),
	})
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.Equal(t, 2, creator.calls)
	require.Equal(t, "profile-updated", creator.req.ProfileID)

	sub, err = client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusActive, sub.Status)
	require.Equal(t, "subscription-123", sub.ExternalSubscriptionID)
}

func TestUpdateProviderInstanceEnabledWiseUnrelatedFieldsSkipsSubscriptionEnsure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
		SortOrder:      1,
		Limits:         "{}",
		RefundEnabled:  true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, creator.calls)

	name := "wise renamed"
	sortOrder := 2
	limits := `{"single_min":1}`
	refundEnabled := false
	updated, err := svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
		Name:          &name,
		SortOrder:     &sortOrder,
		Limits:        &limits,
		RefundEnabled: &refundEnabled,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, creator.calls)
}

func TestUpdateProviderInstanceEnabledWiseLocalConfigChangesSkipSubscriptionEnsure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config map[string]string
	}{
		{
			name:   "clear webhook public key override",
			config: map[string]string{"webhookPublicKey": ""},
		},
		{
			name:   "change currency",
			config: map[string]string{"currency": "EUR"},
		},
		{
			name:   "change quick pay base url",
			config: map[string]string{"quickPayBaseUrl": "https://wise.com/pay/business/updated-account"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			creator := &wiseSubscriptionCreatorStub{}
			svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
			svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
				return creator
			}
			svc.webhookBaseURLResolver = func(context.Context) string {
				return "https://api.example.com"
			}

			inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
				ProviderKey:    payment.TypeWise,
				Name:           "wise",
				Config:         validWiseProviderConfigForConfigService(t),
				SupportedTypes: []string{payment.TypeWise},
				Enabled:        true,
			})
			require.NoError(t, err)
			require.Equal(t, 1, creator.calls)

			err = client.PaymentProviderWebhookSubscription.Update().
				Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
				SetExternalSubscriptionID("existing-subscription-id").
				Exec(ctx)
			require.NoError(t, err)

			updated, err := svc.UpdateProviderInstance(ctx, inst.ID, UpdateProviderInstanceRequest{
				Config: tc.config,
			})
			require.NoError(t, err)
			require.NotNil(t, updated)
			require.Equal(t, 1, creator.calls)

			sub, err := client.PaymentProviderWebhookSubscription.Query().
				Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID)).
				Only(ctx)
			require.NoError(t, err)
			require.Equal(t, "existing-subscription-id", sub.ExternalSubscriptionID)
		})
	}
}

func TestCreateProviderInstanceEnabledWiseDisablesProviderWhenSubscriptionFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	creator := &wiseSubscriptionCreatorStub{err: errors.New("remote rejected token-test")}
	svc := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	svc.wiseSubscriptionClientFactory = func(map[string]string) wiseProfileSubscriptionClient {
		return creator
	}
	svc.webhookBaseURLResolver = func(context.Context) string {
		return "https://api.example.com"
	}

	inst, err := svc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{
		ProviderKey:    payment.TypeWise,
		Name:           "wise",
		Config:         validWiseProviderConfigForConfigService(t),
		SupportedTypes: []string{payment.TypeWise},
		Enabled:        true,
	})
	require.Error(t, err)
	require.Nil(t, inst)
	require.NotContains(t, err.Error(), "token-test")

	saved, err := client.PaymentProviderInstance.Query().Only(ctx)
	require.NoError(t, err)
	require.False(t, saved.Enabled)

	sub, err := client.PaymentProviderWebhookSubscription.Query().
		Where(paymentproviderwebhooksubscription.ProviderInstanceIDEQ(saved.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, wiseWebhookSubscriptionStatusFailed, sub.Status)
}
