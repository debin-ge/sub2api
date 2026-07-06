package payment

import "errors"

var (
	ErrManualRefundRequired = errors.New("manual refund required")
	ErrCancelNotSupported   = errors.New("cancel payment not supported")
)
