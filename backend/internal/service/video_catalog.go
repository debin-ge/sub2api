package service

import (
	"fmt"
	"slices"
	"strings"
)

const (
	OpenAIVideoModelSora2    = "sora-2"
	OpenAIVideoModelSora2Pro = "sora-2-pro"
)

// DefaultOpenAIVideoCapabilities is the built-in safe snapshot. Runtime
// catalog overrides can replace it after validation without changing the
// provider adapter or task state machine.
func DefaultOpenAIVideoCapabilities() VideoCapabilities {
	operations := map[VideoCapability]bool{
		VideoCapabilityCreate:             true,
		VideoCapabilityInputReference:     true,
		VideoCapabilityCharacters:         true,
		VideoCapabilityUploadedVideoEdits: true,
		VideoCapabilityEdits:              true,
		VideoCapabilityExtensions:         true,
		VideoCapabilityWebhook:            true,
	}
	inputMIMEs := map[VideoInputRole]map[string]bool{
		VideoInputRoleReferenceImage: {
			"image/jpeg": true,
			"image/png":  true,
			"image/webp": true,
		},
		VideoInputRoleSourceVideo: {
			"video/mp4": true,
		},
		VideoInputRoleCharacterClip: {
			"video/mp4": true,
		},
	}
	return VideoCapabilities{
		DefaultModel: OpenAIVideoModelSora2,
		DefaultSeconds: map[string]int{
			OpenAIVideoModelSora2:    4,
			OpenAIVideoModelSora2Pro: 4,
		},
		DefaultSizes: map[string]string{
			OpenAIVideoModelSora2:    "720x1280",
			OpenAIVideoModelSora2Pro: "720x1280",
		},
		Operations: operations,
		InputRolesByOperation: map[string]map[VideoInputRole]bool{
			VideoOperationGenerate:        {VideoInputRoleReferenceImage: true},
			VideoOperationEdit:            {VideoInputRoleSourceVideo: true},
			VideoOperationCharacterCreate: {VideoInputRoleCharacterClip: true},
		},
		InputMIMETypes: inputMIMEs,
		MaxInputBytes: map[VideoInputRole]int64{
			VideoInputRoleReferenceImage: MaxContentModerationImageBytes,
			VideoInputRoleSourceVideo:    100 << 20,
			VideoInputRoleCharacterClip:  100 << 20,
		},
		MaxInputsByOperation: map[string]int{
			VideoOperationGenerate:        1,
			VideoOperationEdit:            1,
			VideoOperationCharacterCreate: 1,
		},
		ContentVariants: map[string]bool{"video": true, "thumbnail": true, "spritesheet": true},
		SupportedModels: map[string]bool{
			OpenAIVideoModelSora2:    true,
			OpenAIVideoModelSora2Pro: true,
			"sora-2-2025-10-06":      true,
			"sora-2-2025-12-08":      true,
			"sora-2-pro-2025-10-06":  true,
		},
		SupportedSeconds: map[string][]int{
			OpenAIVideoModelSora2:    {4, 8, 12, 16, 20},
			OpenAIVideoModelSora2Pro: {4, 8, 12, 16, 20},
		},
		SupportedSizes: map[string][]string{
			OpenAIVideoModelSora2: {
				"720x1280", "1280x720", "1024x1792", "1792x1024",
			},
			OpenAIVideoModelSora2Pro: {
				"720x1280", "1280x720", "1024x1792", "1792x1024",
				"1080x1920", "1920x1080",
			},
		},
	}
}

func ValidateVideoCreateCapabilities(caps VideoCapabilities, request VideoCreateRequest, inputs []VideoInput) error {
	operation := VideoCapabilityCreate
	switch request.Operation {
	case "", VideoOperationGenerate:
	case VideoOperationEdit:
		operation = VideoCapabilityEdits
	case VideoOperationExtend:
		operation = VideoCapabilityExtensions
	case VideoOperationCharacterCreate:
		operation = VideoCapabilityCharacters
	default:
		return fmt.Errorf("%w: operation %q", ErrVideoCapabilityUnsupported, request.Operation)
	}
	if !caps.Supports(operation) {
		return fmt.Errorf("%w: operation %q", ErrVideoCapabilityUnsupported, request.Operation)
	}
	if request.Operation == VideoOperationEdit && len(inputs) > 0 && !caps.Supports(VideoCapabilityUploadedVideoEdits) {
		return fmt.Errorf("%w: uploaded video edits", ErrVideoCapabilityUnsupported)
	}
	operationName := normalizeVideoOperation(request.Operation)
	if maximum := caps.MaxInputsByOperation[operationName]; maximum > 0 && len(inputs) > maximum {
		return fmt.Errorf("%w: operation %q accepts at most %d binary input(s)", ErrVideoInputUnsupported, operationName, maximum)
	}
	if request.InputReference != nil {
		hasFile := strings.TrimSpace(request.InputReference.FileID) != ""
		hasURL := strings.TrimSpace(request.InputReference.ImageURL) != ""
		if hasFile == hasURL || !caps.Supports(VideoCapabilityInputReference) {
			return fmt.Errorf("%w: input_reference", ErrVideoInputUnsupported)
		}
		if len(inputs) > 0 && !caps.AllowReferenceAndFile {
			return fmt.Errorf("%w: input_reference cannot be combined with uploaded binary input", ErrVideoInputUnsupported)
		}
	}
	model := strings.ToLower(strings.TrimSpace(request.RequestedModel))
	if model == "" {
		model = strings.ToLower(strings.TrimSpace(request.Model))
	}
	if model == "" {
		return fmt.Errorf("%w: model %q", ErrVideoCapabilityUnsupported, model)
	}
	// SupportedModels describes models whose request constraints are known to
	// the built-in catalog. Account model_mapping remains the routing whitelist;
	// custom compatible upstream models must not be rejected merely because
	// they are absent from this snapshot.
	knownModel := caps.SupportedModels[model]
	if allowed, ok := caps.SupportedSeconds[canonicalOpenAIVideoModel(model)]; operationName != VideoOperationEdit && knownModel && ok && request.Seconds > 0 && !slices.Contains(allowed, request.Seconds) {
		return fmt.Errorf("%w: seconds %d", ErrVideoCapabilityUnsupported, request.Seconds)
	}
	if allowed, ok := caps.SupportedSizes[canonicalOpenAIVideoModel(model)]; knownModel && ok && strings.TrimSpace(request.Size) != "" && !slices.Contains(allowed, strings.ToLower(strings.TrimSpace(request.Size))) {
		return fmt.Errorf("%w: size %q", ErrVideoCapabilityUnsupported, request.Size)
	}
	for _, input := range inputs {
		if !caps.SupportsInputForOperation(request.Operation, input.Role) {
			return fmt.Errorf("%w: role=%s operation=%s", ErrVideoInputUnsupported, input.Role, request.Operation)
		}
		if !caps.SupportsInput(input.Role, input.MIMEType, input.Size) {
			return fmt.Errorf("%w: role=%s mime=%s", ErrVideoInputUnsupported, input.Role, input.MIMEType)
		}
		if input.Role == VideoInputRoleReferenceImage && !caps.Supports(VideoCapabilityInputReference) {
			return fmt.Errorf("%w: input_reference", ErrVideoCapabilityUnsupported)
		}
		if input.Role == VideoInputRoleReferenceImage {
			_, width, height, ok := parseVideoDimensions(request.Size)
			if !ok || input.Width <= 0 || input.Height <= 0 || int64(input.Width) != width || int64(input.Height) != height {
				return fmt.Errorf("%w: input_reference dimensions must match size %q", ErrVideoInputUnsupported, request.Size)
			}
		}
	}
	return nil
}

// ApplyVideoCapabilityDefaults makes provider defaults explicit before price
// resolution and request hashing. Derived operations keep their own semantics
// and therefore do not inherit create-only duration or size defaults.
func ApplyVideoCapabilityDefaults(caps VideoCapabilities, request VideoCreateRequest) VideoCreateRequest {
	if strings.TrimSpace(request.Model) == "" {
		request.Model = strings.TrimSpace(caps.DefaultModel)
	}
	if strings.TrimSpace(request.RequestedModel) == "" {
		request.RequestedModel = request.Model
	}
	if normalizeVideoOperation(request.Operation) != VideoOperationGenerate {
		return request
	}
	model := canonicalOpenAIVideoModel(firstNonEmptyString(request.RequestedModel, request.Model))
	if request.Seconds <= 0 {
		request.Seconds = caps.DefaultSeconds[model]
	}
	if strings.TrimSpace(request.Size) == "" {
		request.Size = strings.TrimSpace(caps.DefaultSizes[model])
	}
	return request
}

func canonicalOpenAIVideoModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, OpenAIVideoModelSora2Pro):
		return OpenAIVideoModelSora2Pro
	case strings.HasPrefix(model, OpenAIVideoModelSora2):
		return OpenAIVideoModelSora2
	default:
		return model
	}
}
