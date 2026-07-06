package payment

import (
	"errors"
	"testing"
)

func TestProviderSentinelErrorsSupportErrorsIs(t *testing.T) {
	if !errors.Is(ErrManualRefundRequired, ErrManualRefundRequired) {
		t.Fatal("ErrManualRefundRequired must be comparable through errors.Is")
	}
	if !errors.Is(ErrCancelNotSupported, ErrCancelNotSupported) {
		t.Fatal("ErrCancelNotSupported must be comparable through errors.Is")
	}
}
