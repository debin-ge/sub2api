package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoGenerationTransitionsAreMonotonic(t *testing.T) {
	require.True(t, CanTransitionVideoGeneration(VideoGenerationPreparing, VideoGenerationHeld))
	require.True(t, CanTransitionVideoGeneration(VideoGenerationSubmitting, VideoGenerationQueued))
	require.True(t, CanTransitionVideoGeneration(VideoGenerationSubmitting, VideoGenerationFailed))
	require.False(t, CanTransitionVideoGeneration(VideoGenerationCompleted, VideoGenerationInProgress))
	require.False(t, CanTransitionVideoGeneration(VideoGenerationFailed, VideoGenerationQueued))
}

func TestVideoBillingCaptureAndReleaseAreMutuallyExclusive(t *testing.T) {
	require.True(t, CanTransitionVideoBilling(VideoBillingHeld, VideoBillingCapturePending))
	require.True(t, CanTransitionVideoBilling(VideoBillingHeld, VideoBillingReleasePending))
	require.False(t, CanTransitionVideoBilling(VideoBillingCaptured, VideoBillingReleasePending))
	require.False(t, CanTransitionVideoBilling(VideoBillingReleased, VideoBillingCapturePending))
}

// A capture intent that turns out to be unexecutable must be able to fall back
// to a release. Without this edge the worker's settlement rewrite is rejected by
// the repository and the task keeps its hold forever, neither charged nor
// refunded, while the claim predicate re-queues it on every tick.
func TestVideoBillingCapturePendingCanDowngradeToRelease(t *testing.T) {
	require.True(t, CanTransitionVideoBilling(VideoBillingCapturePending, VideoBillingReleasePending))
	// The downgrade is one-way: a release intent is the last chance to avoid
	// charging, so it must never be promoted back into a capture.
	require.False(t, CanTransitionVideoBilling(VideoBillingReleasePending, VideoBillingCapturePending))
}

func TestProjectVideoStatusWaitsForCapture(t *testing.T) {
	task := &VideoTask{GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCapturePending}
	require.Equal(t, VideoGenerationInProgress, ProjectVideoStatus(task))
	task.BillingState = VideoBillingCaptured
	require.Equal(t, VideoGenerationCompleted, ProjectVideoStatus(task))
}

func TestVideoIDs(t *testing.T) {
	require.True(t, IsValidVideoTaskID(NewVideoTaskID()))
	require.True(t, IsValidVideoResourceID(NewVideoResourceID()))
	require.False(t, IsValidVideoTaskID("video_provider_supplied"))
	require.False(t, IsValidVideoTaskID("video_!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"))
	require.False(t, IsValidVideoResourceID("char_ABCDEF0123456789abcdef0123456789"))
}
