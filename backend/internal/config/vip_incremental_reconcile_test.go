package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVIPIncrementalReconcileDefaultsAreSafe(t *testing.T) {
	resetViperWithJWTSecret(t)

	cfg, err := Load()

	require.NoError(t, err)
	require.Equal(t, 2*time.Minute, cfg.PaymentFulfillmentDBTxTimeout)
	require.False(t, cfg.VIPReconcileEnabled)
	require.Equal(t, time.Minute, cfg.VIPReconcileInterval)
	require.Equal(t, 45*time.Second, cfg.VIPReconcileRunTimeout)
	require.Equal(t, 5*time.Second, cfg.VIPReconcileSafetyDelay)
	require.Equal(t, 200, cfg.VIPReconcileBatchSize)
	require.Equal(t, 200*time.Millisecond, cfg.VIPReconcileBatchPause)
	require.Equal(t, 50, cfg.VIPReconcileMaxBatches)
	require.Equal(t, 5*time.Minute, cfg.VIPReconcileOverlap)
	require.Equal(t, time.Minute, cfg.VIPReconcileOverlapMargin)
	require.Equal(t, 30*time.Minute, cfg.VIPReconcileJobRunTimeout)
	require.GreaterOrEqual(
		t,
		cfg.VIPReconcileOverlap,
		cfg.PaymentFulfillmentDBTxTimeout+cfg.VIPReconcileOverlapMargin,
	)
}

func TestLoadVIPIncrementalReconcileFromEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("PAYMENT_FULFILLMENT_DB_TX_TIMEOUT", "90s")
	t.Setenv("VIP_RECONCILE_ENABLED", "false")
	t.Setenv("VIP_RECONCILE_INTERVAL", "2m")
	t.Setenv("VIP_RECONCILE_RUN_TIMEOUT", "30s")
	t.Setenv("VIP_RECONCILE_SAFETY_DELAY", "7s")
	t.Setenv("VIP_RECONCILE_BATCH_SIZE", "321")
	t.Setenv("VIP_RECONCILE_BATCH_PAUSE", "350ms")
	t.Setenv("VIP_RECONCILE_MAX_BATCHES_PER_RUN", "17")
	t.Setenv("VIP_RECONCILE_OVERLAP", "3m")
	t.Setenv("VIP_RECONCILE_OVERLAP_MARGIN", "15s")
	t.Setenv("VIP_RECONCILE_JOB_RUN_TIMEOUT", "20m")

	cfg, err := Load()

	require.NoError(t, err)
	require.Equal(t, 90*time.Second, cfg.PaymentFulfillmentDBTxTimeout)
	require.False(t, cfg.VIPReconcileEnabled)
	require.Equal(t, 2*time.Minute, cfg.VIPReconcileInterval)
	require.Equal(t, 30*time.Second, cfg.VIPReconcileRunTimeout)
	require.Equal(t, 7*time.Second, cfg.VIPReconcileSafetyDelay)
	require.Equal(t, 321, cfg.VIPReconcileBatchSize)
	require.Equal(t, 350*time.Millisecond, cfg.VIPReconcileBatchPause)
	require.Equal(t, 17, cfg.VIPReconcileMaxBatches)
	require.Equal(t, 3*time.Minute, cfg.VIPReconcileOverlap)
	require.Equal(t, 15*time.Second, cfg.VIPReconcileOverlapMargin)
	require.Equal(t, 20*time.Minute, cfg.VIPReconcileJobRunTimeout)
}

func TestVIPIncrementalReconcileRejectsUnsafeOverlap(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("PAYMENT_FULFILLMENT_DB_TX_TIMEOUT", "2m")
	t.Setenv("VIP_RECONCILE_OVERLAP_MARGIN", "1m")
	t.Setenv("VIP_RECONCILE_OVERLAP", "179s")

	_, err := Load()

	require.ErrorContains(
		t,
		err,
		"vip_reconcile_overlap must be >= payment_fulfillment_db_tx_timeout + vip_reconcile_overlap_margin",
	)
}

func TestVIPIncrementalReconcilePreservesExplicitZeroSafetyValues(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("VIP_RECONCILE_SAFETY_DELAY", "0s")
	t.Setenv("VIP_RECONCILE_OVERLAP_MARGIN", "0s")

	cfg, err := Load()

	require.NoError(t, err)
	require.Zero(t, cfg.VIPReconcileSafetyDelay)
	require.Zero(t, cfg.VIPReconcileOverlapMargin)
}

func TestVIPIncrementalReconcileRejectsExplicitZeroTransactionTimeout(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("PAYMENT_FULFILLMENT_DB_TX_TIMEOUT", "0s")

	_, err := Load()

	require.ErrorContains(
		t,
		err,
		"payment_fulfillment_db_tx_timeout must be positive",
	)
}
