package service

import (
	"encoding/json"
)

// Release policy is separate from provider capabilities: an adapter supporting
// an operation does not make it available in this release.
func ValidateVideoReleaseOperation(operation string) error {
	switch normalizeVideoOperation(operation) {
	case VideoOperationGenerate, VideoOperationEdit, VideoOperationExtend, VideoOperationCharacterCreate:
		return nil
	default:
		return ErrVideoOperationDisabled
	}
}

func ValidateVideoReleaseField(name string) error {
	_ = name
	return nil
}

func ValidateVideoReleaseJSON(operation string, body []byte) error {
	if err := ValidateVideoReleaseOperation(operation); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return ErrVideoInvalidRequest
	}
	for name := range fields {
		if err := ValidateVideoReleaseField(name); err != nil {
			return err
		}
	}
	return nil
}

func ValidateVideoReleaseSubmission(request VideoSubmitRequest) error {
	return ValidateVideoReleaseOperation(request.Operation)
}

func VideoCallbacksAvailable() bool { return true }
