package service

const (
	VideoGenerationPreparing         = "preparing"
	VideoGenerationHeld              = "held"
	VideoGenerationSubmitting        = "submitting"
	VideoGenerationSubmissionUnknown = "submission_unknown"
	VideoGenerationQueued            = "queued"
	VideoGenerationInProgress        = "in_progress"
	VideoGenerationCompleted         = "completed"
	VideoGenerationFailed            = "failed"
	VideoGenerationCancelled         = "cancelled"
	VideoGenerationExpired           = "expired"

	VideoBillingNone           = "none"
	VideoBillingHeld           = "held"
	VideoBillingCapturePending = "capture_pending"
	VideoBillingCaptured       = "captured"
	VideoBillingReleasePending = "release_pending"
	VideoBillingReleased       = "released"
	VideoBillingManualReview   = "manual_review"

	VideoDeleteNone      = "none"
	VideoDeleteRequested = "requested"
	VideoDeleteDeleting  = "deleting"
	VideoDeleteDeleted   = "deleted"
	VideoDeleteFailed    = "delete_failed"
)

var videoGenerationTransitions = map[string]map[string]struct{}{
	VideoGenerationPreparing: {
		VideoGenerationHeld:   {},
		VideoGenerationFailed: {},
	},
	VideoGenerationHeld: {
		VideoGenerationSubmitting: {},
		VideoGenerationFailed:     {},
		VideoGenerationCancelled:  {},
	},
	VideoGenerationSubmitting: {
		VideoGenerationQueued:            {},
		VideoGenerationInProgress:        {},
		VideoGenerationCompleted:         {},
		VideoGenerationSubmissionUnknown: {},
		VideoGenerationFailed:            {},
	},
	VideoGenerationSubmissionUnknown: {
		VideoGenerationQueued:     {},
		VideoGenerationInProgress: {},
		VideoGenerationCompleted:  {},
		VideoGenerationFailed:     {},
		VideoGenerationCancelled:  {},
		VideoGenerationExpired:    {},
	},
	VideoGenerationQueued: {
		VideoGenerationInProgress: {},
		VideoGenerationCompleted:  {},
		VideoGenerationFailed:     {},
		VideoGenerationCancelled:  {},
		VideoGenerationExpired:    {},
	},
	VideoGenerationInProgress: {
		VideoGenerationCompleted: {},
		VideoGenerationFailed:    {},
		VideoGenerationCancelled: {},
		VideoGenerationExpired:   {},
	},
}

var videoBillingTransitions = map[string]map[string]struct{}{
	VideoBillingNone: {
		VideoBillingHeld:         {},
		VideoBillingManualReview: {},
	},
	VideoBillingHeld: {
		VideoBillingCapturePending: {},
		VideoBillingReleasePending: {},
		VideoBillingManualReview:   {},
	},
	VideoBillingCapturePending: {
		VideoBillingCaptured:     {},
		VideoBillingManualReview: {},
	},
	VideoBillingReleasePending: {
		VideoBillingReleased:     {},
		VideoBillingManualReview: {},
	},
	VideoBillingManualReview: {
		VideoBillingHeld:           {},
		VideoBillingCapturePending: {},
		VideoBillingReleasePending: {},
	},
}

var videoDeleteTransitions = map[string]map[string]struct{}{
	VideoDeleteNone: {
		VideoDeleteRequested: {},
	},
	VideoDeleteRequested: {
		VideoDeleteDeleting: {},
		VideoDeleteDeleted:  {},
		VideoDeleteFailed:   {},
	},
	VideoDeleteDeleting: {
		VideoDeleteDeleted: {},
		VideoDeleteFailed:  {},
	},
	VideoDeleteFailed: {
		VideoDeleteDeleting: {},
		VideoDeleteDeleted:  {},
	},
}

func CanTransitionVideoGeneration(from, to string) bool {
	if from == to {
		return true
	}
	_, ok := videoGenerationTransitions[from][to]
	return ok
}

func CanTransitionVideoBilling(from, to string) bool {
	if from == to {
		return true
	}
	_, ok := videoBillingTransitions[from][to]
	return ok
}

func CanTransitionVideoDelete(from, to string) bool {
	if from == to {
		return true
	}
	_, ok := videoDeleteTransitions[from][to]
	return ok
}

func IsVideoGenerationTerminal(state string) bool {
	switch state {
	case VideoGenerationCompleted, VideoGenerationFailed, VideoGenerationCancelled, VideoGenerationExpired:
		return true
	default:
		return false
	}
}

func IsVideoBillingTerminal(state string) bool {
	return state == VideoBillingCaptured || state == VideoBillingReleased
}

func ProjectVideoStatus(task *VideoTask) string {
	if task == nil {
		return VideoGenerationFailed
	}
	switch task.GenerationState {
	case VideoGenerationCompleted:
		if task.BillingState == VideoBillingCaptured {
			return VideoGenerationCompleted
		}
		return VideoGenerationInProgress
	case VideoGenerationFailed, VideoGenerationCancelled, VideoGenerationExpired:
		return VideoGenerationFailed
	case VideoGenerationInProgress:
		return VideoGenerationInProgress
	default:
		return VideoGenerationQueued
	}
}
