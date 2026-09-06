package service

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
)

type ResolvedVideoExecutionSpec struct {
	Version           int     `json:"version"`
	Provider          string  `json:"provider"`
	AccountID         int64   `json:"account_id"`
	Operation         string  `json:"operation"`
	Model             string  `json:"model"`
	Size              string  `json:"size"`
	Seconds           int     `json:"seconds"`
	DurationSemantics string  `json:"duration_semantics"`
	SourceTaskID      int64   `json:"source_task_id,omitempty"`
	SourceVersion     int64   `json:"source_version,omitempty"`
	SourceSeconds     float64 `json:"source_seconds,omitempty"`
	ExtensionDepth    int     `json:"extension_depth,omitempty"`
	TotalSeconds      float64 `json:"total_seconds,omitempty"`
	OutputUnverified  bool    `json:"output_unverified,omitempty"`
}

const (
	videoMaximumExtensionDepth = 6
	videoMaximumTotalSeconds   = 120
)

func resolveVideoExecutionSpec(request VideoCreateRequest, accountID int64, provider string, source *VideoTask) (VideoCreateRequest, ResolvedVideoExecutionSpec, error) {
	if normalized, _, _, valid := parseVideoDimensions(request.Size); valid {
		request.Size = normalized
	}
	spec := ResolvedVideoExecutionSpec{
		Version: 2, Provider: provider, AccountID: accountID, Operation: request.Operation,
		Model: request.Model, Size: request.Size, Seconds: request.Seconds, DurationSemantics: "output",
	}
	if source == nil {
		if request.Operation == VideoOperationEdit {
			if request.Seconds != 0 || request.Size != "" || request.Quality != "" || request.AudioEnabled != nil ||
				request.ServiceTier != "" || request.InputReference != nil || request.ParentTask != nil || len(request.Characters) != 0 || len(request.ProviderOptions) != 0 {
				return request, spec, ErrVideoSourceSpecConflict
			}
			spec.OutputUnverified = true
			spec.DurationSemantics = "uploaded_output_unverified"
		}
		return request, spec, nil
	}
	model := strings.TrimSpace(source.UpstreamModel)
	if model == "" || source.AccountID == nil || *source.AccountID != accountID || source.Provider != provider {
		return request, spec, ErrVideoSourceSpecUnavailable
	}
	if !strings.EqualFold(model, strings.TrimSpace(request.Model)) {
		return request, spec, ErrVideoSourceSpecConflict
	}
	size := videoSourceOutputSize(source)
	seconds := videoSourceOutputSeconds(source)
	if size == "" || !finitePositive(seconds) {
		return request, spec, ErrVideoSourceSpecUnavailable
	}
	if strings.TrimSpace(request.Size) != "" {
		normalized, _, _, valid := parseVideoDimensions(request.Size)
		if !valid || normalized != size {
			return request, spec, ErrVideoSourceSpecConflict
		}
	}
	if request.Quality != "" || request.AudioEnabled != nil || request.ServiceTier != "" || request.InputReference != nil || len(request.Characters) != 0 || len(request.ProviderOptions) != 0 {
		return request, spec, ErrVideoSourceSpecConflict
	}
	switch request.Operation {
	case VideoOperationEdit:
		if seconds != math.Trunc(seconds) || seconds > float64(math.MaxInt32) {
			return request, spec, ErrVideoSourceSpecUnavailable
		}
		if request.Seconds != 0 && request.Seconds != int(seconds) {
			return request, spec, ErrVideoSourceSpecConflict
		}
		request.Seconds = int(seconds)
	case VideoOperationExtend:
		if !validVideoExtensionSeconds(request.Seconds) {
			return request, spec, ErrVideoInvalidRequest
		}
		depth, err := trustedVideoExtensionDepth(source, seconds)
		if err != nil {
			return request, spec, err
		}
		totalSeconds := seconds + float64(request.Seconds)
		if depth >= videoMaximumExtensionDepth || totalSeconds > videoMaximumTotalSeconds {
			return request, spec, ErrVideoExtensionLimitExceeded
		}
		spec.DurationSemantics = "extension_segment"
		spec.ExtensionDepth = depth + 1
		spec.TotalSeconds = totalSeconds
	default:
		return request, spec, ErrVideoInvalidRequest
	}
	request.Model, request.Size = model, size
	spec.Model, spec.Size, spec.Seconds = model, size, request.Seconds
	spec.SourceTaskID, spec.SourceVersion, spec.SourceSeconds = source.ID, source.Version, seconds
	return request, spec, nil
}

func videoFrozenExecutionSpec(task *VideoTask) (*ResolvedVideoExecutionSpec, error) {
	if task == nil || task.Operation == VideoOperationCharacterCreate {
		return nil, nil
	}
	version, _ := numericMapValue(task.RequestAttributes, "execution_spec_version")
	raw, exists := task.RequestAttributes["execution_spec"]
	if !exists && version == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, ErrVideoSourceSpecUnavailable
	}
	var spec ResolvedVideoExecutionSpec
	if json.Unmarshal(encoded, &spec) != nil || (spec.Version != 1 && spec.Version != 2) {
		return nil, ErrVideoSourceSpecUnavailable
	}
	if spec.Provider != task.Provider || spec.AccountID != valueOrZero(task.AccountID) ||
		spec.Model != task.UpstreamModel || spec.Operation != task.Operation || spec.Model == "" {
		return nil, ErrVideoSourceSpecUnavailable
	}
	if spec.Version == 1 && version == 0 {
		if spec.Operation == VideoOperationEdit && spec.SourceTaskID == 0 {
			conditions, err := json.Marshal(task.PriceSnapshot["conditions"])
			if err != nil || validateVideoExecutionQuote(ResolvedVideoExecutionSpec{OutputUnverified: true}, &VideoPriceQuote{
				BillingUnit: videoStringValue(task.BillingUnit), EstimatedUnits: 1, MaximumUnits: 1, Conditions: conditions,
			}) != nil {
				return nil, ErrVideoSourceSpecUnavailable
			}
			spec.Size, spec.Seconds, spec.OutputUnverified = "", 0, true
		}
		return &spec, nil
	}
	if version != 2 || spec.Version != 2 {
		return nil, ErrVideoSourceSpecUnavailable
	}
	fingerprint, err := HashVideoRequest(spec)
	if err != nil || fingerprint != task.RequestAttributes["execution_spec_hash"] {
		return nil, ErrVideoSourceSpecUnavailable
	}
	return &spec, nil
}

func videoSpecificationConflictMarker(values map[string]any, key string) bool {
	if value, exists := values[key]; !exists || value == nil {
		return false
	}
	marker, valid := numericMapValue(values, key)
	return !valid || marker != 0
}

func videoCheckObservedSpecification(task *VideoTask, metadata map[string]any) error {
	if task == nil {
		return nil
	}
	spec, err := videoFrozenExecutionSpec(task)
	if err != nil {
		return err
	}
	for _, values := range []map[string]any{task.ResponseMetadata, metadata} {
		if videoSpecificationConflictMarker(values, "execution_spec_conflict") {
			return ErrVideoSourceSpecConflict
		}
		if videoSpecificationConflictMarker(values, "specification_invalid") {
			return ErrVideoSourceSpecConflict
		}
	}
	if spec == nil {
		return nil
	}
	if value, exists := metadata["model"]; exists && value != nil && value != "" {
		model, ok := value.(string)
		if !ok || strings.TrimSpace(model) != spec.Model {
			return ErrVideoSourceSpecConflict
		}
	}
	if value, exists := metadata["size"]; exists && value != nil && value != "" {
		size, ok := value.(string)
		normalized, _, _, valid := parseVideoDimensions(size)
		expectedSize := spec.Size
		if expected, _, _, known := parseVideoDimensions(expectedSize); known {
			expectedSize = expected
		}
		if !ok || !valid || (expectedSize != "" && normalized != expectedSize) {
			return ErrVideoSourceSpecConflict
		}
	}
	if value, exists := metadata["seconds"]; exists {
		seconds, valid := numericMapValue(map[string]any{"seconds": value}, "seconds")
		if !valid || !finitePositive(seconds) {
			return ErrVideoSourceSpecConflict
		}
		expected := float64(spec.Seconds)
		if spec.DurationSemantics == "extension_segment" {
			expected = spec.TotalSeconds
			if !finitePositive(expected) {
				expected = float64(spec.Seconds) + spec.SourceSeconds
			}
		}
		if !spec.OutputUnverified && expected > 0 && math.Abs(seconds-expected) > 0.000001 {
			return ErrVideoSourceSpecConflict
		}
	}
	return nil
}

func validVideoExtensionSeconds(seconds int) bool {
	switch seconds {
	case 4, 8, 12, 16, 20:
		return true
	default:
		return false
	}
}

func trustedVideoExtensionDepth(source *VideoTask, observedSeconds float64) (int, error) {
	if source == nil || !finitePositive(observedSeconds) {
		return 0, ErrVideoSourceSpecUnavailable
	}
	if source.Operation != VideoOperationExtend {
		return 0, nil
	}
	spec, err := videoFrozenExecutionSpec(source)
	if err != nil || spec == nil || spec.Operation != VideoOperationExtend || spec.ExtensionDepth <= 0 ||
		spec.ExtensionDepth > videoMaximumExtensionDepth || !finitePositive(spec.TotalSeconds) ||
		math.Abs(spec.TotalSeconds-observedSeconds) > 0.000001 {
		return 0, ErrVideoSourceSpecUnavailable
	}
	return spec.ExtensionDepth, nil
}

func videoObservedMetadata(task *VideoTask, metadata map[string]any) map[string]any {
	clean := sanitizeVideoProviderMetadata(metadata)
	if err := videoCheckObservedSpecification(task, clean); err != nil {
		clean["execution_spec_conflict"] = float64(1)
	}
	return clean
}

func validateVideoExecutionQuote(spec ResolvedVideoExecutionSpec, quote *VideoPriceQuote) error {
	if quote == nil {
		return ErrVideoPricingMissing
	}
	if !spec.OutputUnverified {
		return nil
	}
	conditions, _, err := decodeVideoPricingConditions(quote.Conditions)
	if err != nil || quote.BillingUnit != VideoBillingUnitRequest || conditions.needsVerifiedOutput() ||
		quote.EstimatedUnits != 1 || quote.MaximumUnits != 1 {
		return errors.Join(ErrVideoSourceSpecUnavailable, err)
	}
	return nil
}

func videoSourceOutputSize(task *VideoTask) string {
	for _, attributes := range []map[string]any{task.ResponseMetadata, task.RequestAttributes} {
		if raw, ok := attributes["size"].(string); ok && strings.TrimSpace(raw) != "" {
			normalized, _, _, valid := parseVideoDimensions(raw)
			if !valid {
				return ""
			}
			return normalized
		}
	}
	return ""
}

func videoSourceOutputSeconds(task *VideoTask) float64 {
	if value, exists := task.ResponseMetadata["seconds"]; exists {
		seconds, valid := numericMapValue(map[string]any{"seconds": value}, "seconds")
		if valid && finitePositive(seconds) {
			return seconds
		}
		return 0
	}
	if task.Operation == VideoOperationExtend {
		return 0
	}
	seconds, valid := numericMapValue(task.RequestAttributes, "seconds")
	if valid && finitePositive(seconds) {
		return seconds
	}
	return 0
}
