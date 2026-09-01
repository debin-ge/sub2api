package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const apiKeyEmailVerificationPurpose = "api_key_notification_binding"

var (
	ErrAPIKeyNotificationEmailInvalid = infraerrors.BadRequest("API_KEY_NOTIFICATION_EMAIL_INVALID", "invalid API key notification email")
	ErrAPIKeyEmailVerificationInvalid = infraerrors.BadRequest("API_KEY_EMAIL_VERIFICATION_INVALID", "invalid or expired API key email verification")
	ErrAPIKeyEmailVerificationLimited = infraerrors.TooManyRequests("API_KEY_EMAIL_VERIFICATION_LIMITED", "too many API key email verification requests")
	ErrAPIKeyNotificationUnverified   = infraerrors.BadRequest("API_KEY_NOTIFICATION_EMAIL_UNVERIFIED", "API key notification email must be verified")
	ErrAPIKeyRotationExpiryRequired   = infraerrors.BadRequest("API_KEY_ROTATION_EXPIRY_REQUIRED", "rotate on expiry requires a future expiration time")
)

type APIKeyEmailVerificationProof struct {
	UserID     int64     `json:"user_id"`
	Email      string    `json:"email"`
	Purpose    string    `json:"purpose"`
	VerifiedAt time.Time `json:"verified_at"`
}

type APIKeyEmailVerificationResult struct {
	Token     string    `json:"verification_token"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}

type APIKeyEmailVerificationCache interface {
	GetCode(ctx context.Context, userID int64, email string) (*VerificationCodeData, error)
	SetCode(ctx context.Context, userID int64, email string, data *VerificationCodeData, ttl time.Duration) error
	DeleteCode(ctx context.Context, userID int64, email string) error
	IncrementSendCount(ctx context.Context, userID int64, ttl time.Duration) (int64, error)
	SetProof(ctx context.Context, token string, proof APIKeyEmailVerificationProof, ttl time.Duration) error
	GetProof(ctx context.Context, token string) (*APIKeyEmailVerificationProof, error)
}

type APIKeyEmailVerificationService struct {
	cache        APIKeyEmailVerificationCache
	emailService *EmailService
}

func NewAPIKeyEmailVerificationService(cache APIKeyEmailVerificationCache, emailService *EmailService) *APIKeyEmailVerificationService {
	return &APIKeyEmailVerificationService{cache: cache, emailService: emailService}
}

func NormalizeAPIKeyNotificationEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", nil
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(parsed.Address, email) || len(email) > 320 {
		return "", ErrAPIKeyNotificationEmailInvalid
	}
	return email, nil
}

func (s *APIKeyEmailVerificationService) SendCode(ctx context.Context, userID int64, rawEmail, locale string) error {
	if s == nil || s.cache == nil || s.emailService == nil || userID <= 0 {
		return ErrEmailNotConfigured
	}
	email, err := NormalizeAPIKeyNotificationEmail(rawEmail)
	if err != nil || email == "" {
		return ErrAPIKeyNotificationEmailInvalid
	}
	if existing, getErr := s.cache.GetCode(ctx, userID, email); getErr == nil && existing != nil && time.Since(existing.CreatedAt) < verifyCodeCooldown {
		return ErrVerifyCodeTooFrequent
	}
	count, err := s.cache.IncrementSendCount(ctx, userID, notifyCodeUserRateWindow)
	if err != nil {
		return fmt.Errorf("reserve API key email verification send: %w", err)
	}
	if count > notifyCodeUserRateLimit {
		return ErrAPIKeyEmailVerificationLimited
	}

	code, err := s.emailService.GenerateVerifyCode()
	if err != nil {
		return fmt.Errorf("generate API key email verification code: %w", err)
	}
	if err := s.sendVerificationEmail(ctx, userID, email, code, locale); err != nil {
		return err
	}
	now := time.Now().UTC()
	data := &VerificationCodeData{Code: code, CreatedAt: now, ExpiresAt: now.Add(verifyCodeTTL)}
	if err := s.cache.SetCode(ctx, userID, email, data, verifyCodeTTL); err != nil {
		return fmt.Errorf("save API key email verification code: %w", err)
	}
	return nil
}

func (s *APIKeyEmailVerificationService) VerifyCode(ctx context.Context, userID int64, rawEmail, code string) (*APIKeyEmailVerificationResult, error) {
	if s == nil || s.cache == nil || userID <= 0 {
		return nil, ErrAPIKeyEmailVerificationInvalid
	}
	email, err := NormalizeAPIKeyNotificationEmail(rawEmail)
	if err != nil || email == "" {
		return nil, ErrAPIKeyNotificationEmailInvalid
	}
	data, err := s.cache.GetCode(ctx, userID, email)
	if err != nil || data == nil || time.Now().After(data.ExpiresAt) {
		return nil, ErrAPIKeyEmailVerificationInvalid
	}
	if data.Attempts >= maxVerifyCodeAttempts {
		return nil, ErrVerifyCodeMaxAttempts
	}
	if subtle.ConstantTimeCompare([]byte(data.Code), []byte(strings.TrimSpace(code))) != 1 {
		data.Attempts++
		remaining := time.Until(data.ExpiresAt)
		if remaining > 0 {
			_ = s.cache.SetCode(ctx, userID, email, data, remaining)
		}
		if data.Attempts >= maxVerifyCodeAttempts {
			return nil, ErrVerifyCodeMaxAttempts
		}
		return nil, ErrAPIKeyEmailVerificationInvalid
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate API key email verification token: %w", err)
	}
	token := hex.EncodeToString(bytes)
	verifiedAt := time.Now().UTC()
	proof := APIKeyEmailVerificationProof{UserID: userID, Email: email, Purpose: apiKeyEmailVerificationPurpose, VerifiedAt: verifiedAt}
	if err := s.cache.SetProof(ctx, token, proof, verifyCodeTTL); err != nil {
		return nil, fmt.Errorf("save API key email verification token: %w", err)
	}
	_ = s.cache.DeleteCode(ctx, userID, email)
	return &APIKeyEmailVerificationResult{Token: token, Email: email, ExpiresAt: verifiedAt.Add(verifyCodeTTL)}, nil
}

func (s *APIKeyEmailVerificationService) ValidateProof(ctx context.Context, userID int64, rawEmail, token string) (time.Time, error) {
	if s == nil || s.cache == nil || strings.TrimSpace(token) == "" {
		return time.Time{}, ErrAPIKeyNotificationUnverified
	}
	email, err := NormalizeAPIKeyNotificationEmail(rawEmail)
	if err != nil || email == "" {
		return time.Time{}, ErrAPIKeyNotificationEmailInvalid
	}
	proof, err := s.cache.GetProof(ctx, strings.TrimSpace(token))
	if err != nil || proof == nil || proof.UserID != userID || proof.Purpose != apiKeyEmailVerificationPurpose || !strings.EqualFold(proof.Email, email) {
		return time.Time{}, ErrAPIKeyNotificationUnverified
	}
	return proof.VerifiedAt, nil
}

func (s *APIKeyEmailVerificationService) sendVerificationEmail(ctx context.Context, userID int64, email, code, locale string) error {
	if s.emailService.notificationEmailService != nil {
		err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event: NotificationEmailEventNotificationEmailVerifyCode, Locale: locale,
			RecipientEmail: email, RecipientName: emailRecipientName(email), UserID: userID,
			Variables: map[string]string{"verification_code": code, "expires_in_minutes": strconv.Itoa(int(verifyCodeTTL / time.Minute))},
		})
		if err == nil {
			return nil
		}
		if !shouldFallbackNotificationEmail(err) {
			return err
		}
	}
	siteName := fallbackEmailSiteName
	if s.emailService.settingRepo != nil {
		if value, err := s.emailService.settingRepo.GetValue(ctx, SettingKeySiteName); err == nil && strings.TrimSpace(value) != "" {
			siteName = value
		}
	}
	subject := fmt.Sprintf("[%s] API Key Notification Email Verification", EmailSubjectSiteName(siteName))
	return s.emailService.SendEmail(ctx, email, subject, buildNotifyVerifyEmailBody(code, siteName))
}
