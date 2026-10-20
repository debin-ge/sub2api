//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Simulate a write that succeeded but whose Redis reply was lost. Cleanup must
// cover that unknown write as well as earlier successful reservation dimensions.
type failingFixedChargeCache struct {
	fixedChargeCacheStub
	failBalance bool
	failScope   string
}

func (s *failingFixedChargeCache) ReserveInflightBalance(ctx context.Context, userID int64, requestID string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	allowed, pending, err := s.fixedChargeCacheStub.ReserveInflightBalance(ctx, userID, requestID, amount, balance, ttl)
	if s.failBalance {
		return false, 0, errors.New("reservation reply lost")
	}
	return allowed, pending, err
}

func (s *failingFixedChargeCache) ReserveInflightScope(ctx context.Context, scope, requestID string, amount float64, ttl time.Duration) (float64, error) {
	pending, err := s.fixedChargeCacheStub.ReserveInflightScope(ctx, scope, requestID, amount, ttl)
	if s.failScope == scope {
		return 0, errors.New("scope reply lost")
	}
	return pending, err
}

func TestReserveFixedChargeFailsClosedAndRollsBackAllDimensions(t *testing.T) {
	for _, failure := range []string{"balance", "apikey", "subscription", "subscription_then_apikey", "capability_missing", "scope_capability_missing"} {
		t.Run(failure, func(t *testing.T) {
			cache := &failingFixedChargeCache{fixedChargeCacheStub: fixedChargeCacheStub{balance: 100,
				sub: &SubscriptionCacheData{Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}}}
			key := &APIKey{ID: 7, Quota: 100}
			var group *Group
			var subscription *UserSubscription
			if failure == "subscription" || failure == "subscription_then_apikey" || failure == "scope_capability_missing" {
				limit := 100.0
				group = &Group{ID: 42, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
				subscription = &UserSubscription{ID: 5}
			}
			switch failure {
			case "balance":
				cache.failBalance = true
			case "apikey", "subscription_then_apikey":
				cache.failScope = inflightAPIKeyScope(key.ID)
			case "subscription":
				cache.failScope = inflightSubscriptionScope(subscription.ID)
			}
			var billingCache BillingCache = cache
			if failure == "capability_missing" || failure == "scope_capability_missing" {
				billingCache = struct{ BillingCache }{cache} // intentionally hides the reservation interfaces
			}
			cfg := &config.Config{}
			cfg.Billing.InflightReservation.Enabled = true
			svc := NewBillingCacheService(billingCache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(svc.Stop)
			reservation, err := svc.ReserveFixedCharge(context.Background(), &User{ID: 3}, key, group, subscription, 10)
			require.ErrorIs(t, err, ErrBillingServiceUnavailable)
			require.Nil(t, reservation)
			for scope, holds := range cache.reserved {
				require.Empty(t, holds, "all attempted reservations must be released: %s", scope)
			}
		})
	}
}

func TestEstimatedReservationStillAllowsOptionalCacheFailure(t *testing.T) {
	cache := &failingFixedChargeCache{fixedChargeCacheStub: fixedChargeCacheStub{balance: 100}, failBalance: true}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)
	reservation, err := svc.ReserveInflight(context.Background(), &User{ID: 3}, nil, nil, 10)
	require.NoError(t, err, "ordinary estimates keep their existing optional reservation policy")
	require.Nil(t, reservation)
}
