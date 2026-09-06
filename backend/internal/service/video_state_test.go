package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoGenerationTransitionsAreMonotonic(t *testing.T) {
	require.True(t, CanTransitionVideoGeneration(VideoGenerationPreparing, VideoGenerationHeld))
	require.True(t, CanTransitionVideoGeneration(VideoGenerationSubmitting, VideoGenerationSubmissionUnknown))
	require.True(t, CanTransitionVideoGeneration(VideoGenerationSubmissionUnknown, VideoGenerationCompleted))
	require.False(t, CanTransitionVideoGeneration(VideoGenerationCompleted, VideoGenerationInProgress))
	require.False(t, CanTransitionVideoGeneration(VideoGenerationFailed, VideoGenerationQueued))
}

func TestVideoBillingCaptureAndReleaseAreMutuallyExclusive(t *testing.T) {
	require.True(t, CanTransitionVideoBilling(VideoBillingHeld, VideoBillingCapturePending))
	require.True(t, CanTransitionVideoBilling(VideoBillingHeld, VideoBillingReleasePending))
	require.False(t, CanTransitionVideoBilling(VideoBillingCaptured, VideoBillingReleasePending))
	require.False(t, CanTransitionVideoBilling(VideoBillingReleased, VideoBillingCapturePending))
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
