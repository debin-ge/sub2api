package service

import (
	"math"
	"strings"
)

// Source media has been downloaded once, frozen in encrypted spool and probed
// before this function runs. SourceSeconds is distinct from the extension delta.
func resolveGrokVideoExecutionSpec(r VideoCreateRequest, accountID int64, source *VideoTask) (VideoCreateRequest, ResolvedVideoExecutionSpec, error) {
	spec := ResolvedVideoExecutionSpec{Version: 2, Provider: VideoProviderGrok, AccountID: accountID, Operation: r.Operation, Model: r.Model, Resolution: r.Resolution, Ratio: r.Ratio, Seconds: r.Seconds, DurationSemantics: "output"}
	if r.Operation == VideoOperationGenerate {
		return r, spec, nil
	}
	if r.Operation != VideoOperationEdit && r.Operation != VideoOperationExtend {
		return r, spec, ErrVideoCapabilityUnsupported
	}
	if r.Grok == nil || r.Grok.Source == nil || !r.Grok.Source.verified {
		return r, spec, ErrVideoSourceSpecUnavailable
	}
	input := r.Grok.Source
	if !finitePositive(input.Seconds) || input.Width <= 0 || input.Height <= 0 {
		return r, spec, ErrVideoSourceSpecUnavailable
	}
	if r.Size != "" || r.Ratio != "" || r.AudioEnabled != nil || r.SmartDuration || r.Frames != 0 {
		return r, spec, ErrVideoSourceSpecConflict
	}
	// The native derived endpoints retain source geometry, capped at 720p.
	shortSide := input.Width
	if input.Height < shortSide {
		shortSide = input.Height
	}
	resolution := "480p"
	if shortSide > 480 {
		resolution = "720p"
	}
	if r.Resolution != "" && !strings.EqualFold(r.Resolution, resolution) {
		return r, spec, ErrVideoSourceSpecConflict
	}
	r.Resolution = resolution
	spec.Resolution = resolution
	spec.SourceSeconds = input.Seconds
	if source != nil {
		if source.Provider != VideoProviderGrok || source.AccountID == nil || *source.AccountID != accountID {
			return r, spec, ErrVideoSourceSpecUnavailable
		}
		spec.SourceTaskID, spec.SourceVersion = source.ID, source.Version
	}
	if r.Operation == VideoOperationEdit {
		if r.Seconds != 0 || input.Seconds > 8.7 {
			return r, spec, ErrVideoSourceSpecConflict
		}
		r.Seconds = int(math.Ceil(input.Seconds))
		spec.Seconds = r.Seconds
		spec.BillableSeconds = input.Seconds
		return r, spec, nil
	}
	if input.Seconds < 2 || input.Seconds > 15 || r.Seconds < 2 || r.Seconds > 10 {
		return r, spec, ErrVideoSourceSpecConflict
	}
	spec.Seconds = r.Seconds
	spec.BillableSeconds = float64(r.Seconds)
	spec.TotalSeconds = input.Seconds + float64(r.Seconds)
	spec.DurationSemantics = "extension_segment"
	if source != nil {
		depth, err := trustedVideoExtensionDepth(source, input.Seconds)
		if err != nil {
			return r, spec, err
		}
		if depth >= videoMaximumExtensionDepth {
			return r, spec, ErrVideoExtensionLimitExceeded
		}
		spec.ExtensionDepth = depth + 1
	}
	return r, spec, nil
}

func grokVideoRequestHashFields(options *GrokVideoOptions) map[string]any {
	values := map[string]any{"voice_ids": options.VoiceIDs}
	frames := make([]map[string]any, 0, len(options.Keyframes))
	for _, frame := range options.Keyframes {
		item := map[string]any{"timestamp_s": frame.TimestampSeconds, "url": frame.Image.URL}
		if frame.Image.Input != nil {
			item["input"] = videoInputHashManifest([]VideoInput{*frame.Image.Input})
		}
		frames = append(frames, item)
	}
	values["keyframes"] = frames
	if options.Source != nil {
		src := options.Source
		values["source"] = map[string]any{"input": videoInputHashManifest([]VideoInput{src.Input}), "seconds": src.Seconds, "width": src.Width, "height": src.Height}
	}
	return values
}
