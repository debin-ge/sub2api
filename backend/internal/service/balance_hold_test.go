package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchImageBalanceHoldFingerprintRemainsByteCompatible(t *testing.T) {
	cmd := &BatchImageBalanceHoldCommand{
		RequestID:          BatchImageCaptureRequestID("imgbatch_golden"),
		APIKeyID:           7,
		UserID:             42,
		BatchID:            "imgbatch_golden",
		HoldAmount:         1.25,
		ActualAmount:       0.75,
		RequestPayloadHash: "payloadhash",
	}
	cmd.Normalize()

	require.Equal(t, "batch_image_hold:imgbatch_golden", BatchImageHoldRequestID(cmd.BatchID))
	require.Equal(t, "batch_image_capture:imgbatch_golden", cmd.RequestID)
	require.Equal(t, "batch_image_release:imgbatch_golden", BatchImageReleaseRequestID(cmd.BatchID))
	require.Equal(t, "bab31b90ca6f5d3e2401e27a03408bfab6ad85475292634f061ddb0e2db3eacb", cmd.RequestFingerprint)
}

func TestBalanceHoldCommandValidationAndVideoRequestIDs(t *testing.T) {
	cmd := &BalanceHoldCommand{
		RequestID:    VideoTaskHoldRequestID("video_abc"),
		APIKeyID:     7,
		UserID:       42,
		Scope:        BalanceHoldScopeVideoTask,
		RefID:        " video_abc ",
		HoldAmount:   1.25,
		ActualAmount: 0,
	}
	cmd.Normalize()

	require.NoError(t, cmd.Validate())
	require.Equal(t, "video_abc", cmd.RefID)
	require.NotEmpty(t, cmd.RequestFingerprint)
	require.Equal(t, "video_task_hold:video_abc", cmd.RequestID)
	require.Equal(t, "video_task_capture:video_abc", VideoTaskCaptureRequestID(cmd.RefID))
	require.Equal(t, "video_task_release:video_abc", VideoTaskReleaseRequestID(cmd.RefID))
	require.Equal(t, cmd.RequestID, BalanceHoldReserveRequestID(cmd.Scope, cmd.RefID))
}

func TestBalanceHoldCommandRejectsUnknownScopeAndInvalidAmounts(t *testing.T) {
	base := BalanceHoldCommand{
		RequestID:  "hold:test",
		APIKeyID:   7,
		UserID:     42,
		Scope:      BalanceHoldScopeVideoTask,
		RefID:      "video_test",
		HoldAmount: 1,
	}
	base.Normalize()
	require.NoError(t, base.Validate())

	invalidScope := base
	invalidScope.Scope = "unknown"
	require.ErrorIs(t, invalidScope.Validate(), ErrBalanceHoldScopeInvalid)

	invalidAmount := base
	invalidAmount.HoldAmount = -1
	require.ErrorIs(t, invalidAmount.Validate(), ErrBalanceHoldAmountInvalid)
}
