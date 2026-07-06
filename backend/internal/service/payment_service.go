package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/google/uuid"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// --- Order Status Constants ---

const (
	OrderStatusPending           = payment.OrderStatusPending
	OrderStatusPaid              = payment.OrderStatusPaid
	OrderStatusRecharging        = payment.OrderStatusRecharging
	OrderStatusCompleted         = payment.OrderStatusCompleted
	OrderStatusExpired           = payment.OrderStatusExpired
	OrderStatusCancelled         = payment.OrderStatusCancelled
	OrderStatusFailed            = payment.OrderStatusFailed
	OrderStatusRefundRequested   = payment.OrderStatusRefundRequested
	OrderStatusRefunding         = payment.OrderStatusRefunding
	OrderStatusPartiallyRefunded = payment.OrderStatusPartiallyRefunded
	OrderStatusRefunded          = payment.OrderStatusRefunded
	OrderStatusRefundFailed      = payment.OrderStatusRefundFailed
)

const (
	// defaultMaxPendingOrders and defaultOrderTimeoutMin are defined in
	// payment_config_service.go alongside other payment configuration defaults.
	paymentGraceMinutes = 5

	defaultPageSize    = 20
	maxPageSize        = 100
	topUsersLimit      = 10
	amountToleranceCNY = 0.01

	defaultOrderIDPrefix   = "Sub2API"
	legacyOrderIDPrefix    = "sub2_"
	maxOrderIDPrefixLength = 24
)

const paymentResumeSigningKeyEnv = "PAYMENT_RESUME_SIGNING_KEY"

// --- Types ---

// generateOutTradeNo creates a unique external order ID for payment providers.
// Format: <site-prefix>20250409aB3kX9mQ (prefix + date + 8-char random)
func generateOutTradeNo(prefix string) string {
	date := time.Now().Format("20060102")
	rnd := generateRandomString(8)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = defaultOrderIDPrefix
	}
	return prefix + date + rnd
}

func orderIDPrefixFromSiteName(siteName string) string {
	siteName = strings.TrimSpace(siteName)
	var b strings.Builder
	b.Grow(min(len(siteName), maxOrderIDPrefixLength))
	for _, ch := range siteName {
		if b.Len() >= maxOrderIDPrefixLength {
			break
		}
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		default:
			if initial, ok := chinesePinyinInitial(ch); ok {
				b.WriteByte(initial)
			}
		}
	}
	prefix := b.String()
	if prefix == "" {
		return defaultOrderIDPrefix
	}
	if len(prefix) > maxOrderIDPrefixLength {
		return prefix[:maxOrderIDPrefixLength]
	}
	return prefix
}

func chinesePinyinInitial(ch rune) (byte, bool) {
	if ch < '\u4e00' || ch > '\u9fff' {
		return 0, false
	}
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(string(ch))
	if err != nil || len(encoded) < 2 {
		return 0, false
	}
	code := int(encoded[0])<<8 + int(encoded[1])
	if code < 0xB0A1 || code > 0xF7FE {
		return 0, false
	}
	switch {
	case code < 0xB0C5:
		return 'A', true
	case code < 0xB2C1:
		return 'B', true
	case code < 0xB4EE:
		return 'C', true
	case code < 0xB6EA:
		return 'D', true
	case code < 0xB7A2:
		return 'E', true
	case code < 0xB8C1:
		return 'F', true
	case code < 0xB9FE:
		return 'G', true
	case code < 0xBBF7:
		return 'H', true
	case code < 0xBFA6:
		return 'J', true
	case code < 0xC0AC:
		return 'K', true
	case code < 0xC2E8:
		return 'L', true
	case code < 0xC4C3:
		return 'M', true
	case code < 0xC5B6:
		return 'N', true
	case code < 0xC5BE:
		return 'O', true
	case code < 0xC6DA:
		return 'P', true
	case code < 0xC8BB:
		return 'Q', true
	case code < 0xC8F6:
		return 'R', true
	case code < 0xCBFA:
		return 'S', true
	case code < 0xCDDA:
		return 'T', true
	case code < 0xCEF4:
		return 'W', true
	case code < 0xD1B9:
		return 'X', true
	case code < 0xD4D1:
		return 'Y', true
	default:
		return 'Z', true
	}
}

func generateRandomString(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rand.IntN(len(charset))]
	}
	return string(b)
}

type CreateOrderRequest struct {
	UserID          int64
	Amount          float64
	PaymentType     string
	OpenID          string
	ClientIP        string
	IsMobile        bool
	IsWeChatBrowser bool
	SrcHost         string
	SrcURL          string
	ReturnURL       string
	PaymentSource   string
	OrderType       string
	PlanID          int64
	Locale          string
}

type CreateOrderResponse struct {
	OrderID      int64                           `json:"order_id"`
	Amount       float64                         `json:"amount"`
	PayAmount    float64                         `json:"pay_amount"`
	FeeRate      float64                         `json:"fee_rate"`
	Status       string                          `json:"status"`
	ResultType   payment.CreatePaymentResultType `json:"result_type,omitempty"`
	PaymentType  string                          `json:"payment_type"`
	OutTradeNo   string                          `json:"out_trade_no,omitempty"`
	PayURL       string                          `json:"pay_url,omitempty"`
	QRCode       string                          `json:"qr_code,omitempty"`
	ClientSecret string                          `json:"client_secret,omitempty"`
	IntentID     string                          `json:"intent_id,omitempty"`
	Currency     string                          `json:"currency,omitempty"`
	CountryCode  string                          `json:"country_code,omitempty"`
	PaymentEnv   string                          `json:"payment_env,omitempty"`
	OAuth        *payment.WechatOAuthInfo        `json:"oauth,omitempty"`
	JSAPI        *payment.WechatJSAPIPayload     `json:"jsapi,omitempty"`
	JSAPIPayload *payment.WechatJSAPIPayload     `json:"jsapi_payload,omitempty"`
	ExpiresAt    time.Time                       `json:"expires_at"`
	PaymentMode  string                          `json:"payment_mode,omitempty"`
	ResumeToken  string                          `json:"resume_token,omitempty"`
}

type OrderListParams struct {
	Page        int
	PageSize    int
	Status      string
	OrderType   string
	PaymentType string
	Keyword     string
}

type RefundPlan struct {
	OrderID         int64
	Order           *dbent.PaymentOrder
	RefundAmount    float64
	GatewayAmount   float64
	Reason          string
	Force           bool
	DeductBalance   bool
	DeductionType   string
	BalanceToDeduct float64
	SubDaysToDeduct int
	SubscriptionID  int64
}

type RefundResult struct {
	Success         bool    `json:"success"`
	Warning         string  `json:"warning,omitempty"`
	RequireForce    bool    `json:"require_force,omitempty"`
	ManualRequired  bool    `json:"manual_required,omitempty"`
	ManualAction    string  `json:"manual_action,omitempty"`
	BalanceDeducted float64 `json:"balance_deducted,omitempty"`
	SubDaysDeducted int     `json:"subscription_days_deducted,omitempty"`
}

type DashboardStats struct {
	TodayAmount   float64 `json:"today_amount"`
	TotalAmount   float64 `json:"total_amount"`
	TodayCount    int     `json:"today_count"`
	TotalCount    int     `json:"total_count"`
	AvgAmount     float64 `json:"avg_amount"`
	PendingOrders int     `json:"pending_orders"`

	DailySeries    []DailyStats        `json:"daily_series"`
	PaymentMethods []PaymentMethodStat `json:"payment_methods"`
	TopUsers       []TopUserStat       `json:"top_users"`
}

type DailyStats struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
	Count  int     `json:"count"`
}

type PaymentMethodStat struct {
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
	Count  int     `json:"count"`
}

type TopUserStat struct {
	UserID int64   `json:"user_id"`
	Email  string  `json:"email"`
	Amount float64 `json:"amount"`
}

// --- Service ---

type PaymentService struct {
	providerMu               sync.Mutex
	providersLoaded          bool
	entClient                *dbent.Client
	registry                 *payment.Registry
	loadBalancer             payment.LoadBalancer
	redeemService            *RedeemService
	subscriptionSvc          *SubscriptionService
	configService            *PaymentConfigService
	settingRepo              SettingRepository
	userRepo                 UserRepository
	groupRepo                GroupRepository
	resumeService            *PaymentResumeService
	affiliateService         *AffiliateService
	notificationEmailService *NotificationEmailService
	wiseReconcileLockCache   LeaderLockCache
	wiseReconcileDB          *sql.DB
	wiseReconcileInstanceID  string
	wiseReconcileCoordinator *wiseReconcileCoordinator
}

func NewPaymentService(entClient *dbent.Client, registry *payment.Registry, loadBalancer payment.LoadBalancer, redeemService *RedeemService, subscriptionSvc *SubscriptionService, configService *PaymentConfigService, userRepo UserRepository, groupRepo GroupRepository, affiliateService *AffiliateService) *PaymentService {
	var settingRepo SettingRepository
	if configService != nil {
		settingRepo = configService.settingRepo
	}
	svc := &PaymentService{
		entClient:                entClient,
		registry:                 registry,
		loadBalancer:             newVisibleMethodLoadBalancer(loadBalancer, configService),
		redeemService:            redeemService,
		subscriptionSvc:          subscriptionSvc,
		configService:            configService,
		settingRepo:              settingRepo,
		userRepo:                 userRepo,
		groupRepo:                groupRepo,
		affiliateService:         affiliateService,
		wiseReconcileInstanceID:  uuid.NewString(),
		wiseReconcileCoordinator: newWiseReconcileCoordinator(wiseReconcileDedupWindow),
	}
	svc.resumeService = psNewPaymentResumeService(configService)
	return svc
}

func (s *PaymentService) orderIDPrefix(ctx context.Context) string {
	siteName := ""
	if s != nil && s.settingRepo != nil {
		if value, err := s.settingRepo.GetValue(ctx, SettingKeySiteName); err == nil {
			siteName = value
		}
	}
	return orderIDPrefixFromSiteName(siteName)
}

func (s *PaymentService) SetNotificationEmailService(notificationEmailService *NotificationEmailService) {
	s.notificationEmailService = notificationEmailService
}

// --- Provider Registry ---

// EnsureProviders lazily initializes the provider registry on first call.
func (s *PaymentService) EnsureProviders(ctx context.Context) {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()
	if !s.providersLoaded {
		s.loadProviders(ctx)
		s.providersLoaded = true
	}
}

// RefreshProviders clears and re-registers all providers from the database.
func (s *PaymentService) RefreshProviders(ctx context.Context) {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()
	s.registry.Clear()
	s.loadProviders(ctx)
	s.providersLoaded = true
}

func (s *PaymentService) loadProviders(ctx context.Context) {
	instances, err := s.entClient.PaymentProviderInstance.Query().
		Where(paymentproviderinstance.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		slog.Error("[PaymentService] failed to query provider instances", "error", err)
		return
	}
	for _, inst := range instances {
		cfg, err := s.loadBalancer.GetInstanceConfig(ctx, int64(inst.ID))
		if err != nil {
			slog.Warn("[PaymentService] failed to decrypt config for instance", "instanceID", inst.ID, "error", err)
			continue
		}
		if inst.PaymentMode != "" {
			cfg["paymentMode"] = inst.PaymentMode
		}
		instID := fmt.Sprintf("%d", inst.ID)
		p, err := provider.CreateProvider(inst.ProviderKey, instID, cfg)
		if err != nil {
			slog.Warn("[PaymentService] failed to create provider for instance", "instanceID", inst.ID, "key", inst.ProviderKey, "error", err)
			continue
		}
		s.registry.Register(p)
	}
}

// --- Helpers ---

func psIsRefundStatus(s string) bool {
	switch s {
	case OrderStatusRefundRequested, OrderStatusRefunding, OrderStatusPartiallyRefunded, OrderStatusRefunded, OrderStatusRefundFailed:
		return true
	}
	return false
}

func psErrMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func psNilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *PaymentService) paymentResume() *PaymentResumeService {
	if s.resumeService != nil {
		return s.resumeService
	}
	return psNewPaymentResumeService(s.configService)
}

func NewLegacyAwarePaymentResumeService(legacyKey []byte) *PaymentResumeService {
	return newLegacyAwarePaymentResumeService(legacyKey)
}

func psNewPaymentResumeService(configService *PaymentConfigService) *PaymentResumeService {
	return newLegacyAwarePaymentResumeService(psResumeLegacyVerificationKey(configService))
}

func newLegacyAwarePaymentResumeService(legacyKey []byte) *PaymentResumeService {
	signingKey, verifyFallbacks := resolvePaymentResumeSigningKeys(legacyKey)
	return NewPaymentResumeService(signingKey, verifyFallbacks...)
}

func psResumeLegacyVerificationKey(configService *PaymentConfigService) []byte {
	if configService == nil {
		return nil
	}
	return configService.encryptionKey
}

func resolvePaymentResumeSigningKeys(legacyKey []byte) ([]byte, [][]byte) {
	signingKey := parsePaymentResumeSigningKey(os.Getenv(paymentResumeSigningKeyEnv))
	if len(signingKey) == 0 {
		if len(legacyKey) == 0 {
			return nil, nil
		}
		return legacyKey, nil
	}
	if len(legacyKey) == 0 || bytes.Equal(legacyKey, signingKey) {
		return signingKey, nil
	}
	return signingKey, [][]byte{legacyKey}
}

func parsePaymentResumeSigningKey(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) >= 64 && len(raw)%2 == 0 {
		if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) > 0 {
			return decoded
		}
	}
	return []byte(raw)
}

func psSliceContains(sl []string, s string) bool {
	for _, v := range sl {
		if v == s {
			return true
		}
	}
	return false
}

// Subscription validity period unit constants.
const (
	validityUnitWeek  = "week"
	validityUnitMonth = "month"
)

func psComputeValidityDays(days int, unit string) int {
	switch unit {
	case validityUnitWeek:
		return days * 7
	case validityUnitMonth:
		return days * 30
	default:
		return days
	}
}

func psStartOfDayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func applyPagination(pageSize, page int) (size, pg int) {
	size = pageSize
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	pg = page
	if pg < 1 {
		pg = 1
	}
	return size, pg
}
