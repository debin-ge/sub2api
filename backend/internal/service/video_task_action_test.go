package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNextVideoActionDoesNotLetDeletionSkipFinancialObligations(t *testing.T) {
	for _, deletion := range []string{VideoDeleteRequested, VideoDeleteDeleting, VideoDeleteFailed} {
		for _, test := range []struct {
			generation, billing string
			action              VideoTaskAction
		}{
			{VideoGenerationInProgress, VideoBillingHeld, VideoActionObserve},
			{VideoGenerationQueued, VideoBillingHeld, VideoActionObserve},
			{VideoGenerationCompleted, VideoBillingHeld, VideoActionRecoverTerminalBilling},
			{VideoGenerationCompleted, VideoBillingCapturePending, VideoActionSettle},
			{VideoGenerationFailed, VideoBillingReleasePending, VideoActionSettle},
			{VideoGenerationFailed, VideoBillingManualReview, VideoActionNone},
			{VideoGenerationCompleted, VideoBillingCaptured, VideoActionDeleteContent},
			{VideoGenerationFailed, VideoBillingReleased, VideoActionDeleteContent},
		} {
			task := &VideoTask{GenerationState: test.generation, BillingState: test.billing, DeleteState: deletion}
			require.Equal(t, test.action, NextVideoAction(task))
		}
	}
}
