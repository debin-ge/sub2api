package service

const (
	VideoGenerationPreparing  = "preparing"
	VideoGenerationHeld       = "held"
	VideoGenerationSubmitting = "submitting"
	VideoGenerationQueued     = "queued"
	VideoGenerationInProgress = "in_progress"
	VideoGenerationCompleted  = "completed"
	VideoGenerationFailed     = "failed"
	VideoGenerationCancelled  = "cancelled"
	VideoGenerationExpired    = "expired"

	VideoBillingNone           = "none"
	VideoBillingHeld           = "held"
	VideoBillingCapturePending = "capture_pending"
	VideoBillingCaptured       = "captured"
	VideoBillingReleasePending = "release_pending"
	VideoBillingReleased       = "released"

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
		VideoGenerationQueued:     {},
		VideoGenerationInProgress: {},
		VideoGenerationCompleted:  {},
		VideoGenerationFailed:     {},
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
		VideoBillingHeld: {},
	},
	VideoBillingHeld: {
		VideoBillingCapturePending: {},
		VideoBillingReleasePending: {},
	},
	VideoBillingCapturePending: {
		VideoBillingCaptured: {},
		// A capture intent that cannot be executed has to be downgraded to a
		// release, otherwise the task keeps its hold forever: it is neither
		// charged nor refunded, and the worker re-claims it on every tick.
		VideoBillingReleasePending: {},
	},
	VideoBillingReleasePending: {
		VideoBillingReleased: {},
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
