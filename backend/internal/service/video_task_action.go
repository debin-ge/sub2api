package service

type VideoTaskAction string

const (
	VideoActionNone                   VideoTaskAction = "none"
	VideoActionObserve                VideoTaskAction = "observe"
	VideoActionSettle                 VideoTaskAction = "settle"
	VideoActionRecoverTerminalBilling VideoTaskAction = "recover_terminal_billing"
	VideoActionRecoverHeld            VideoTaskAction = "recover_held"
	VideoActionRecoverSubmitting      VideoTaskAction = "recover_submitting"
	VideoActionQuarantineUnknown      VideoTaskAction = "quarantine_unknown"
	VideoActionDeleteContent          VideoTaskAction = "delete_content"
)

func NextVideoAction(task *VideoTask) VideoTaskAction {
	if task == nil {
		return VideoActionNone
	}
	if task.BillingState == VideoBillingCapturePending || task.BillingState == VideoBillingReleasePending {
		return VideoActionSettle
	}
	if task.BillingState == VideoBillingHeld {
		if IsVideoGenerationTerminal(task.GenerationState) {
			return VideoActionRecoverTerminalBilling
		}
		switch task.GenerationState {
		case VideoGenerationHeld:
			return VideoActionRecoverHeld
		case VideoGenerationSubmitting:
			return VideoActionRecoverSubmitting
		case VideoGenerationSubmissionUnknown:
			return VideoActionQuarantineUnknown
		case VideoGenerationQueued, VideoGenerationInProgress:
			return VideoActionObserve
		}
	}
	if IsVideoBillingTerminal(task.BillingState) && IsVideoGenerationTerminal(task.GenerationState) &&
		(task.DeleteState == VideoDeleteRequested || task.DeleteState == VideoDeleteDeleting || task.DeleteState == VideoDeleteFailed) {
		return VideoActionDeleteContent
	}
	return VideoActionNone
}
