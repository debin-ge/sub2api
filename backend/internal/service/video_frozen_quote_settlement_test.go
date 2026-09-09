package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func bytedanceVideoTaskForTest(t *testing.T) *VideoTask {
	t.Helper()
	task := baseVideoWorkerTask()
	task.Provider = VideoProviderByteDance
	task.RequestedModel, task.ChannelModel, task.UpstreamModel =
		ByteDanceVideoModelSeedance10Pro, ByteDanceVideoModelSeedance10Pro, ByteDanceVideoModelSeedance10Pro
	task.EstimatedUnits = floatPointer(8)
	bindVideoExecutionSpecForTest(t, task, 0)
	return task
}

func videoBillingUnitPointer(value string) *string { return &value }

// A ByteDance task used to be rejected by its own frozen specification, which
// sent every completion into the conflict branch regardless of what the
// provider returned. It must now settle on the observed usage like any other
// provider.
func TestByteDanceCompletionSettlesOnUsageWithoutASpecificationConflict(t *testing.T) {
	task := bytedanceVideoTaskForTest(t)

	spec, err := videoFrozenExecutionSpec(task)
	require.NoError(t, err)
	require.NotNil(t, spec)
	require.Equal(t, VideoProviderByteDance, spec.Provider)

	observed := &ProviderVideoTask{
		ProviderTaskID: "cgt-video-1", Status: VideoGenerationCompleted,
		Metadata: map[string]any{"model": ByteDanceVideoModelSeedance10Pro, "size": "1280x720", "seconds": 8},
	}
	require.NoError(t, videoCheckObservedSpecification(task, videoObservedMetadata(task, observed.Metadata)))

	decision := videoTerminalBillingFor(task, VideoGenerationCompleted, observed)
	require.Equal(t, VideoBillingCapturePending, decision.state)
	require.Empty(t, decision.errorCode)
	require.Empty(t, decision.errorKind)
	require.Equal(t, 8.0, *decision.actualUnits)
	// 8 seconds at 0.5 with a 2x customer multiplier, not the frozen hold of 5.
	require.InDelta(t, 8.0, *decision.actualCost, 0.000001)
	require.NotContains(t, videoObservedMetadata(task, observed.Metadata), "execution_spec_conflict")
	require.NotContains(t, task.ResponseMetadata, "execution_spec_conflict")
}

// A fixed request price can fall back to the frozen quote when usage is absent.
func TestCompletedFixedPriceTaskWithoutUsageCapturesTheFrozenQuote(t *testing.T) {
	task := baseVideoWorkerTask()
	task.BillingUnit = videoBillingUnitPointer(VideoBillingUnitRequest)
	task.EstimatedUnits = floatPointer(8)
	delete(task.RequestAttributes, "seconds")
	task.PriceSnapshot = nil

	decision := videoTerminalBillingFor(task, VideoGenerationCompleted, &ProviderVideoTask{Status: VideoGenerationCompleted})

	require.Equal(t, VideoBillingCapturePending, decision.state)
	require.Equal(t, "billing", decision.errorKind)
	require.Equal(t, "usage_missing", decision.errorCode)
	require.NotEmpty(t, decision.errorMessage)
	require.InDelta(t, *task.EstimatedUnits, *decision.actualUnits, 0.000001)
	require.InDelta(t, *task.HoldAmount, *decision.actualCost, 0.000001)
}

func TestCompletedUsageBasedTaskWithoutUsageReleases(t *testing.T) {
	for _, billingUnit := range []string{VideoBillingUnitSecond, VideoBillingUnitVideoToken} {
		t.Run(billingUnit, func(t *testing.T) {
			task := baseVideoWorkerTask()
			task.BillingUnit = videoBillingUnitPointer(billingUnit)
			task.EstimatedUnits = floatPointer(8)
			delete(task.RequestAttributes, "seconds")

			decision := videoTerminalBillingFor(task, VideoGenerationCompleted, &ProviderVideoTask{Status: VideoGenerationCompleted})

			require.Equal(t, VideoBillingReleasePending, decision.state)
			require.Equal(t, "usage_missing", decision.errorCode)
			require.Zero(t, *decision.actualUnits)
			require.Zero(t, *decision.actualCost)
		})
	}
}

// Without a usable frozen quote there is nothing to charge against, so the
// fallback releases rather than inventing an amount.
func TestCompletedTaskWithoutAFrozenQuoteReleasesInstead(t *testing.T) {
	for name, mutate := range map[string]func(task *VideoTask){
		"no_estimate":      func(task *VideoTask) { task.EstimatedUnits = nil },
		"no_hold":          func(task *VideoTask) { task.HoldAmount = nil },
		"negative_hold":    func(task *VideoTask) { task.HoldAmount = floatPointer(-1) },
		"negative_units":   func(task *VideoTask) { task.EstimatedUnits = floatPointer(-1) },
		"not_a_number":     func(task *VideoTask) { task.HoldAmount = floatPointer(math.NaN()) },
		"infinite_measure": func(task *VideoTask) { task.EstimatedUnits = floatPointer(math.Inf(1)) },
	} {
		t.Run(name, func(t *testing.T) {
			task := baseVideoWorkerTask()
			task.EstimatedUnits = floatPointer(8)
			delete(task.RequestAttributes, "seconds")
			mutate(task)

			decision := videoTerminalBillingFor(task, VideoGenerationCompleted, &ProviderVideoTask{Status: VideoGenerationCompleted})

			require.Equal(t, VideoBillingReleasePending, decision.state)
			require.Equal(t, "usage_missing", decision.errorCode)
			require.Zero(t, *decision.actualUnits)
			require.Zero(t, *decision.actualCost)
		})
	}
}

// Nothing was delivered, so every undelivered terminal state releases the whole
// hold and records an explicit zero charge.
func TestUndeliveredTerminalStatesReleaseTheEntireHold(t *testing.T) {
	for _, state := range []string{VideoGenerationFailed, VideoGenerationCancelled, VideoGenerationExpired} {
		t.Run(state, func(t *testing.T) {
			task := baseVideoWorkerTask()
			task.EstimatedUnits = floatPointer(8)

			decision := videoTerminalBillingFor(task, state, &ProviderVideoTask{Status: state, Metadata: map[string]any{"seconds": 8}})

			require.Equal(t, VideoBillingReleasePending, decision.state)
			require.Zero(t, *decision.actualUnits)
			require.Zero(t, *decision.actualCost)
			require.Empty(t, decision.errorCode)
		})
	}
}
