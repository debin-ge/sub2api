package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderwebhooksubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
)

const (
	wiseWebhookSubscriptionStatusActive  = "active"
	wiseWebhookSubscriptionStatusFailed  = "failed"
	wiseWebhookSubscriptionStatusDeleted = "deleted"
	wiseWebhookSubscriptionStatusUnknown = "unknown"

	wiseWebhookSubscriptionTrigger         = "balances#credit"
	wiseWebhookSubscriptionDeliveryVersion = "4.0.0"
	wiseWebhookSubscriptionPath            = "/api/v1/payment/webhook/wise"
	wiseWebhookSubscriptionUnknownURL      = "unknown"
)

type wiseProfileSubscriptionClient interface {
	CreateProfileSubscription(ctx context.Context, req provider.WiseProfileSubscriptionRequest) (*provider.WiseProfileSubscriptionResponse, error)
	DeleteProfileSubscription(ctx context.Context, profileID string, subscriptionID string) error
}

func (s *PaymentConfigService) ensureWiseWebhookSubscription(ctx context.Context, inst *dbent.PaymentProviderInstance, config map[string]string) error {
	return s.ensureWiseWebhookSubscriptionWithPersistentFailure(ctx, inst, config, false)
}

type wiseWebhookSubscriptionEnsureOptions struct {
	ForceRemoteCreate bool
	PersistFailure    bool
	PersistSuccess    bool
}

type wiseWebhookSubscriptionEnsureResult struct {
	ExternalID     string
	DeliveryURL    string
	RemoteCreated  bool
	ReusedExisting bool
}

func (s *PaymentConfigService) ensureWiseWebhookSubscriptionWithOptions(ctx context.Context, inst *dbent.PaymentProviderInstance, config map[string]string, options wiseWebhookSubscriptionEnsureOptions) (*wiseWebhookSubscriptionEnsureResult, error) {
	if s == nil || s.entClient == nil || inst == nil || inst.ProviderKey != payment.TypeWise || !inst.Enabled {
		return nil, nil
	}

	baseURL := ""
	if s.webhookBaseURLResolver != nil {
		baseURL = strings.TrimRight(strings.TrimSpace(s.webhookBaseURLResolver(ctx)), "/")
	}
	if baseURL == "" {
		errText := "wise webhook base URL is required; configure api_base_url/frontend_url or API_BASE_URL/FRONTEND_URL/APP_URL"
		if options.PersistFailure {
			if err := s.persistWiseWebhookSubscriptionFailure(ctx, inst, wiseWebhookSubscriptionUnknownURL, errText, config); err != nil {
				return nil, fmt.Errorf("wise webhook subscription failed: %s: %w", errText, err)
			}
		}
		return nil, fmt.Errorf("wise webhook subscription failed: %s", errText)
	}
	deliveryURL := baseURL + wiseWebhookSubscriptionPath

	if _, err := provider.NewWise("subscription-validate", config); err != nil {
		errText := sanitizeWiseWebhookSubscriptionError(err, config)
		if options.PersistFailure {
			if persistErr := s.persistWiseWebhookSubscriptionFailure(ctx, inst, deliveryURL, errText, config); persistErr != nil {
				return nil, fmt.Errorf("wise webhook subscription failed: %s: %w", errText, persistErr)
			}
		}
		return nil, fmt.Errorf("wise webhook subscription failed: %s", errText)
	}

	if !options.ForceRemoteCreate {
		reusable, err := s.hasReusableWiseWebhookSubscription(ctx, inst.ID, deliveryURL)
		if err != nil {
			return nil, fmt.Errorf("wise webhook subscription lookup: %w", err)
		}
		if reusable {
			return &wiseWebhookSubscriptionEnsureResult{
				DeliveryURL:    deliveryURL,
				ReusedExisting: true,
			}, nil
		}
	}

	req := provider.WiseProfileSubscriptionRequest{
		ProfileID:       providerConfigFieldValue(config, "profileId"),
		Name:            fmt.Sprintf("Sub2API Wise balance credit - %d", inst.ID),
		TriggerOn:       wiseWebhookSubscriptionTrigger,
		DeliveryVersion: wiseWebhookSubscriptionDeliveryVersion,
		DeliveryURL:     deliveryURL,
	}
	clientFactory := s.wiseSubscriptionClientFactory
	if clientFactory == nil {
		clientFactory = defaultWiseSubscriptionClientFactory
	}
	client := clientFactory(config)
	if client == nil {
		errText := "wise subscription client is required"
		if options.PersistFailure {
			if err := s.persistWiseWebhookSubscriptionFailure(ctx, inst, deliveryURL, errText, config); err != nil {
				return nil, fmt.Errorf("wise webhook subscription failed: %s: %w", errText, err)
			}
		}
		return nil, fmt.Errorf("wise webhook subscription failed: %s", errText)
	}
	resp, err := client.CreateProfileSubscription(ctx, req)
	if err != nil {
		errText := sanitizeWiseWebhookSubscriptionError(err, config)
		if options.PersistFailure {
			if persistErr := s.persistWiseWebhookSubscriptionFailure(ctx, inst, deliveryURL, errText, config); persistErr != nil {
				return nil, fmt.Errorf("wise webhook subscription failed: %s: %w", errText, persistErr)
			}
		}
		return nil, fmt.Errorf("wise webhook subscription failed: %s", errText)
	}

	externalID := ""
	if resp != nil {
		externalID = resp.ID
	}
	result := &wiseWebhookSubscriptionEnsureResult{
		ExternalID:    externalID,
		DeliveryURL:   deliveryURL,
		RemoteCreated: true,
	}
	if options.PersistSuccess {
		if err := s.upsertWiseWebhookSubscription(ctx, inst, externalID, deliveryURL, wiseWebhookSubscriptionStatusActive, ""); err != nil {
			return nil, fmt.Errorf("wise webhook subscription persist: %w", err)
		}
	}
	return result, nil
}

func (s *PaymentConfigService) ensureWiseWebhookSubscriptionWithPersistentFailure(ctx context.Context, inst *dbent.PaymentProviderInstance, config map[string]string, forceRemoteCreate bool) error {
	_, err := s.ensureWiseWebhookSubscriptionWithOptions(ctx, inst, config, wiseWebhookSubscriptionEnsureOptions{
		ForceRemoteCreate: forceRemoteCreate,
		PersistFailure:    true,
		PersistSuccess:    true,
	})
	return err
}

func (s *PaymentConfigService) ensureWiseWebhookSubscriptionDryRun(ctx context.Context, inst *dbent.PaymentProviderInstance, config map[string]string) (*wiseWebhookSubscriptionEnsureResult, error) {
	return s.ensureWiseWebhookSubscriptionWithOptions(ctx, inst, config, wiseWebhookSubscriptionEnsureOptions{
		ForceRemoteCreate: true,
		PersistFailure:    false,
		PersistSuccess:    false,
	})
}

func (s *PaymentConfigService) hasReusableWiseWebhookSubscription(ctx context.Context, providerInstanceID int64, deliveryURL string) (bool, error) {
	_, err := s.entClient.PaymentProviderWebhookSubscription.Query().
		Where(
			paymentproviderwebhooksubscription.ProviderInstanceIDEQ(providerInstanceID),
			paymentproviderwebhooksubscription.ProviderKeyEQ(payment.TypeWise),
			paymentproviderwebhooksubscription.TriggerOnEQ(wiseWebhookSubscriptionTrigger),
			paymentproviderwebhooksubscription.DeliveryURLEQ(deliveryURL),
			paymentproviderwebhooksubscription.StatusEQ(wiseWebhookSubscriptionStatusActive),
			paymentproviderwebhooksubscription.ExternalSubscriptionIDNEQ(""),
		).
		First(ctx)
	if dbent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *PaymentConfigService) invalidateWiseWebhookSubscription(ctx context.Context, providerInstanceID int64) error {
	if s == nil || s.entClient == nil || providerInstanceID <= 0 {
		return nil
	}
	_, err := s.entClient.PaymentProviderWebhookSubscription.Update().
		Where(
			paymentproviderwebhooksubscription.ProviderInstanceIDEQ(providerInstanceID),
			paymentproviderwebhooksubscription.ProviderKeyEQ(payment.TypeWise),
			paymentproviderwebhooksubscription.TriggerOnEQ(wiseWebhookSubscriptionTrigger),
		).
		SetStatus(wiseWebhookSubscriptionStatusUnknown).
		SetExternalSubscriptionID("").
		ClearLastError().
		Save(ctx)
	return err
}

func (s *PaymentConfigService) deleteWiseWebhookSubscription(ctx context.Context, inst *dbent.PaymentProviderInstance, config map[string]string) error {
	if s == nil || s.entClient == nil || inst == nil || inst.ProviderKey != payment.TypeWise {
		return nil
	}
	rows, err := s.entClient.PaymentProviderWebhookSubscription.Query().
		Where(
			paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID),
			paymentproviderwebhooksubscription.ProviderKeyEQ(payment.TypeWise),
			paymentproviderwebhooksubscription.TriggerOnEQ(wiseWebhookSubscriptionTrigger),
			paymentproviderwebhooksubscription.ExternalSubscriptionIDNEQ(""),
			paymentproviderwebhooksubscription.StatusNEQ(wiseWebhookSubscriptionStatusDeleted),
		).
		All(ctx)
	if err != nil {
		return fmt.Errorf("wise webhook subscription delete lookup: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	profileID := providerConfigFieldValue(config, "profileId")
	if strings.TrimSpace(profileID) == "" {
		return fmt.Errorf("wise webhook subscription delete failed: missing profileId")
	}
	clientFactory := s.wiseSubscriptionClientFactory
	if clientFactory == nil {
		clientFactory = defaultWiseSubscriptionClientFactory
	}
	remoteClient := clientFactory(config)
	if remoteClient == nil {
		return fmt.Errorf("wise webhook subscription delete failed: subscription client is required")
	}

	for _, row := range rows {
		externalID := strings.TrimSpace(row.ExternalSubscriptionID)
		if externalID == "" {
			continue
		}
		if err := remoteClient.DeleteProfileSubscription(ctx, profileID, externalID); err != nil {
			errText := sanitizeWiseWebhookSubscriptionError(err, config)
			_ = s.entClient.PaymentProviderWebhookSubscription.UpdateOneID(row.ID).
				SetStatus(wiseWebhookSubscriptionStatusFailed).
				SetLastError(errText).
				SetSyncedAt(time.Now()).
				Exec(ctx)
			return fmt.Errorf("wise webhook subscription delete failed: %s", errText)
		}
		if err := s.entClient.PaymentProviderWebhookSubscription.UpdateOneID(row.ID).
			SetStatus(wiseWebhookSubscriptionStatusDeleted).
			ClearLastError().
			SetSyncedAt(time.Now()).
			Exec(ctx); err != nil {
			return fmt.Errorf("wise webhook subscription mark deleted: %w", err)
		}
	}
	return nil
}

func (s *PaymentConfigService) deleteWiseWebhookSubscriptionIDs(ctx context.Context, inst *dbent.PaymentProviderInstance, config map[string]string, externalIDs []string) error {
	if s == nil || inst == nil || inst.ProviderKey != payment.TypeWise || len(externalIDs) == 0 {
		return nil
	}
	profileID := providerConfigFieldValue(config, "profileId")
	if strings.TrimSpace(profileID) == "" {
		return fmt.Errorf("wise webhook subscription delete failed: missing profileId")
	}
	clientFactory := s.wiseSubscriptionClientFactory
	if clientFactory == nil {
		clientFactory = defaultWiseSubscriptionClientFactory
	}
	remoteClient := clientFactory(config)
	if remoteClient == nil {
		return fmt.Errorf("wise webhook subscription delete failed: subscription client is required")
	}

	seen := make(map[string]struct{}, len(externalIDs))
	for _, externalID := range externalIDs {
		externalID = strings.TrimSpace(externalID)
		if externalID == "" {
			continue
		}
		if _, ok := seen[externalID]; ok {
			continue
		}
		seen[externalID] = struct{}{}
		if err := remoteClient.DeleteProfileSubscription(ctx, profileID, externalID); err != nil {
			errText := sanitizeWiseWebhookSubscriptionError(err, config)
			return fmt.Errorf("wise webhook subscription delete failed: %s", errText)
		}
	}
	return nil
}

func (s *PaymentConfigService) wiseWebhookSubscriptionExternalIDs(ctx context.Context, providerInstanceID int64) ([]string, error) {
	if s == nil || s.entClient == nil || providerInstanceID <= 0 {
		return nil, nil
	}
	rows, err := s.entClient.PaymentProviderWebhookSubscription.Query().
		Where(
			paymentproviderwebhooksubscription.ProviderInstanceIDEQ(providerInstanceID),
			paymentproviderwebhooksubscription.ProviderKeyEQ(payment.TypeWise),
			paymentproviderwebhooksubscription.TriggerOnEQ(wiseWebhookSubscriptionTrigger),
			paymentproviderwebhooksubscription.ExternalSubscriptionIDNEQ(""),
			paymentproviderwebhooksubscription.StatusNEQ(wiseWebhookSubscriptionStatusDeleted),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if externalID := strings.TrimSpace(row.ExternalSubscriptionID); externalID != "" {
			ids = append(ids, externalID)
		}
	}
	return ids, nil
}

func (s *PaymentConfigService) persistWiseWebhookSubscriptionFailure(ctx context.Context, inst *dbent.PaymentProviderInstance, deliveryURL, errText string, config map[string]string) error {
	errText = sanitizeWiseWebhookSubscriptionText(errText, config)
	if strings.TrimSpace(deliveryURL) == "" {
		deliveryURL = wiseWebhookSubscriptionUnknownURL
	}
	return s.upsertWiseWebhookSubscription(ctx, inst, "", deliveryURL, wiseWebhookSubscriptionStatusFailed, errText)
}

func (s *PaymentConfigService) upsertWiseWebhookSubscription(ctx context.Context, inst *dbent.PaymentProviderInstance, externalID, deliveryURL, status, lastError string) error {
	if s == nil || s.entClient == nil || inst == nil {
		return nil
	}
	return upsertWiseWebhookSubscriptionWithClient(ctx, s.entClient, inst, externalID, deliveryURL, status, lastError)
}

func upsertWiseWebhookSubscriptionWithClient(ctx context.Context, client *dbent.Client, inst *dbent.PaymentProviderInstance, externalID, deliveryURL, status, lastError string) error {
	if client == nil || inst == nil {
		return nil
	}
	if strings.TrimSpace(status) == "" {
		status = wiseWebhookSubscriptionStatusUnknown
	}
	if strings.TrimSpace(deliveryURL) == "" {
		deliveryURL = wiseWebhookSubscriptionUnknownURL
	}

	now := time.Now()
	existingRows, err := client.PaymentProviderWebhookSubscription.Query().
		Where(
			paymentproviderwebhooksubscription.ProviderInstanceIDEQ(inst.ID),
			paymentproviderwebhooksubscription.TriggerOnEQ(wiseWebhookSubscriptionTrigger),
		).
		Order(paymentproviderwebhooksubscription.ByID()).
		All(ctx)
	if err != nil {
		return err
	}
	if len(existingRows) == 0 {
		create := client.PaymentProviderWebhookSubscription.Create().
			SetProviderInstanceID(inst.ID).
			SetProviderKey(payment.TypeWise).
			SetExternalSubscriptionID(externalID).
			SetTriggerOn(wiseWebhookSubscriptionTrigger).
			SetDeliveryVersion(wiseWebhookSubscriptionDeliveryVersion).
			SetDeliveryURL(deliveryURL).
			SetStatus(status).
			SetSyncedAt(now)
		if strings.TrimSpace(lastError) != "" {
			create.SetLastError(lastError)
		}
		return create.Exec(ctx)
	}

	existing := existingRows[0]
	for _, row := range existingRows {
		if row.DeliveryURL == deliveryURL {
			existing = row
			break
		}
	}
	for _, row := range existingRows {
		if row.ID == existing.ID {
			continue
		}
		if err := client.PaymentProviderWebhookSubscription.DeleteOneID(row.ID).Exec(ctx); err != nil {
			return err
		}
	}

	update := client.PaymentProviderWebhookSubscription.UpdateOneID(existing.ID).
		SetProviderKey(payment.TypeWise).
		SetExternalSubscriptionID(externalID).
		SetTriggerOn(wiseWebhookSubscriptionTrigger).
		SetDeliveryVersion(wiseWebhookSubscriptionDeliveryVersion).
		SetDeliveryURL(deliveryURL).
		SetStatus(status).
		SetSyncedAt(now)
	if strings.TrimSpace(lastError) != "" {
		update.SetLastError(lastError)
	} else {
		update.ClearLastError()
	}
	return update.Exec(ctx)
}

func sanitizeWiseWebhookSubscriptionError(err error, config map[string]string) string {
	if err == nil {
		return ""
	}
	return sanitizeWiseWebhookSubscriptionText(err.Error(), config)
}

func sanitizeWiseWebhookSubscriptionText(text string, config map[string]string) string {
	apiToken := strings.TrimSpace(providerConfigFieldValue(config, "apiToken"))
	if apiToken == "" {
		return text
	}
	return strings.ReplaceAll(text, apiToken, "[REDACTED]")
}
