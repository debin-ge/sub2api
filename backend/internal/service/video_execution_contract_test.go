package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoExecutionConflictMarkersFailClosed(t *testing.T) {
	for _, candidate := range []struct {
		name     string
		value    any
		conflict bool
	}{
		{name: "absent"},
		{name: "zero", value: 0},
		{name: "string_zero", value: "0.0"},
		{name: "positive", value: 1, conflict: true},
		{name: "negative", value: -1, conflict: true},
		{name: "invalid", value: "invalid", conflict: true},
		{name: "nan", value: "NaN", conflict: true},
		{name: "boolean", value: false, conflict: true},
		{name: "object", value: map[string]any{}, conflict: true},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			for _, key := range []string{"execution_spec_conflict", "specification_invalid"} {
				values := map[string]any{key: candidate.value}
				err := videoCheckObservedSpecification(&VideoTask{}, values)
				clean := sanitizeVideoProviderMetadata(values)
				if candidate.conflict {
					require.ErrorIs(t, err, ErrVideoSourceSpecConflict)
					require.Equal(t, float64(1), clean[key])
				} else {
					require.NoError(t, err)
					require.NotContains(t, clean, key)
				}
			}
		})
	}
}

func bindVideoExecutionSpecForTest(t *testing.T, task *VideoTask, sourceSeconds float64) {
	t.Helper()
	if task.Operation == "" {
		task.Operation = VideoOperationGenerate
	}
	spec := ResolvedVideoExecutionSpec{
		Version: 2, Provider: task.Provider, AccountID: valueOrZero(task.AccountID), Operation: task.Operation,
		Model: task.UpstreamModel, Size: "1280x720", Seconds: 8, DurationSemantics: "output",
	}
	if task.Operation == VideoOperationExtend {
		spec.DurationSemantics, spec.SourceSeconds = "extension_segment", sourceSeconds
		task.RequestAttributes["source_output_seconds"] = sourceSeconds
	}
	fingerprint, err := HashVideoRequest(spec)
	require.NoError(t, err)
	task.RequestAttributes["execution_spec"], task.RequestAttributes["execution_spec_hash"] = spec, fingerprint
	task.RequestAttributes["execution_spec_version"] = 2
}

func TestVideoExecutionContractDetectsReturnedConflicts(t *testing.T) {
	for _, test := range []struct {
		name     string
		metadata map[string]any
	}{
		{"model", map[string]any{"model": OpenAIVideoModelSora2Pro}},
		{"size", map[string]any{"size": "1920x1080"}},
		{"seconds", map[string]any{"seconds": 20}},
		{"invalid_seconds", map[string]any{"seconds": "NaN"}},
		{"negative_seconds", map[string]any{"seconds": -1}},
		{"invalid_size", map[string]any{"size": map[string]any{"url": "https://example.test/secret"}}},
		{"invalid_model", map[string]any{"model": "https://example.test/key"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := baseVideoWorkerTask()
			bindVideoExecutionSpecForTest(t, task, 0)
			observed := &ProviderVideoTask{Status: VideoGenerationCompleted, Metadata: test.metadata, Usage: map[string]any{"seconds": 8}}
			decision := videoTerminalBillingFor(task, VideoGenerationCompleted, observed)
			require.Equal(t, VideoBillingManualReview, decision.state)
			require.Equal(t, "execution_spec_conflict", decision.errorCode)
			require.Nil(t, decision.actualCost)
			clean := videoObservedMetadata(task, observed.Metadata)
			require.Equal(t, float64(1), clean["execution_spec_conflict"])
			encoded, err := json.Marshal(clean)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "example.test")
		})
	}
}

func TestVideoExecutionContractSurvivesSerializationAndRejectsTampering(t *testing.T) {
	task := baseVideoWorkerTask()
	bindVideoExecutionSpecForTest(t, task, 0)
	encoded, err := json.Marshal(task)
	require.NoError(t, err)
	var restored VideoTask
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.NoError(t, videoCheckObservedSpecification(&restored, map[string]any{"model": task.UpstreamModel, "size": "1280X720", "seconds": "8"}))
	for _, corruption := range []string{"hash", "version", "identity", "missing_spec"} {
		t.Run(corruption, func(t *testing.T) {
			var mutated VideoTask
			require.NoError(t, json.Unmarshal(encoded, &mutated))
			switch corruption {
			case "hash":
				mutated.RequestAttributes["execution_spec_hash"] = "changed"
			case "version":
				mutated.RequestAttributes["execution_spec_version"] = 3
			case "identity":
				accountID := int64(99)
				mutated.AccountID = &accountID
			case "missing_spec":
				delete(mutated.RequestAttributes, "execution_spec")
			}
			require.ErrorIs(t, videoCheckObservedSpecification(&mutated, nil), ErrVideoSourceSpecUnavailable)
		})
	}
	legacy := baseVideoWorkerTask()
	require.NoError(t, videoCheckObservedSpecification(legacy, map[string]any{"model": "legacy-model"}))
	require.ErrorIs(t, videoCheckObservedSpecification(legacy, map[string]any{"specification_invalid": 1}), ErrVideoSourceSpecConflict)
	legacy.Operation = VideoOperationGenerate
	legacy.RequestAttributes["execution_spec"] = ResolvedVideoExecutionSpec{Version: 1, Provider: legacy.Provider,
		AccountID: *legacy.AccountID, Model: legacy.UpstreamModel, Operation: legacy.Operation, Size: "1280x720", Seconds: 8}
	require.ErrorIs(t, videoCheckObservedSpecification(legacy, map[string]any{"model": OpenAIVideoModelSora2Pro}), ErrVideoSourceSpecConflict)
}

func TestVideoExecutionContractKeepsPollingButNeverForgetsConflict(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_bad_spec", Status: VideoGenerationQueued,
		Metadata: map[string]any{"model": OpenAIVideoModelSora2Pro, "size": "1280x720", "seconds": "8"}}}
	svc, _, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	created, err := svc.Submit(context.Background(), videoSubmitRequestForTest())
	require.NoError(t, err)
	require.Equal(t, VideoBillingHeld, created.Task.BillingState)
	require.NotNil(t, created.Task.NextActionAt)
	require.Equal(t, VideoActionObserve, NextVideoAction(created.Task))
	observed := &ProviderVideoTask{ProviderTaskID: "video_bad_spec", Status: VideoGenerationCompleted,
		Metadata: map[string]any{"model": OpenAIVideoModelSora2, "size": "1280x720", "seconds": "8"}}
	completed, err := svc.ReconcileProviderObservation(context.Background(), created.Task, observed, "provider_polled")
	require.NoError(t, err)
	require.Equal(t, VideoBillingManualReview, completed.BillingState)
	require.Equal(t, float64(1), completed.ResponseMetadata["execution_spec_conflict"])
}

func TestVideoExecutionContractExtensionUsesCombinedOutputNotBillableSegment(t *testing.T) {
	task := baseVideoWorkerTask()
	task.Operation = VideoOperationExtend
	bindVideoExecutionSpecForTest(t, task, 20)
	valid := &ProviderVideoTask{Metadata: map[string]any{"seconds": 28, "size": "1280x720"}}
	decision := videoTerminalBillingFor(task, VideoGenerationCompleted, valid)
	require.Equal(t, VideoBillingCapturePending, decision.state)
	require.Equal(t, 8.0, *decision.actualUnits)
	invalid := videoTerminalBillingFor(task, VideoGenerationCompleted, &ProviderVideoTask{Metadata: map[string]any{"seconds": 8}})
	require.Equal(t, VideoBillingManualReview, invalid.state)
}

func TestVideoExecutionContractEnforcesExtensionDepthAndTotalDuration(t *testing.T) {
	accountID := int64(11)
	base := &VideoTask{
		ID: 10, Version: 3, Operation: VideoOperationGenerate, Provider: VideoProviderOpenAI,
		AccountID: &accountID, UpstreamModel: OpenAIVideoModelSora2,
		ResponseMetadata:  map[string]any{"size": "1280x720", "seconds": 100},
		RequestAttributes: map[string]any{},
	}
	request := VideoCreateRequest{Operation: VideoOperationExtend, Model: OpenAIVideoModelSora2, Prompt: "Continue", Seconds: 20}
	_, spec, err := resolveVideoExecutionSpec(request, accountID, VideoProviderOpenAI, base)
	require.NoError(t, err)
	require.Equal(t, 1, spec.ExtensionDepth)
	require.Equal(t, float64(120), spec.TotalSeconds)

	base.ResponseMetadata["seconds"] = 101
	_, _, err = resolveVideoExecutionSpec(request, accountID, VideoProviderOpenAI, base)
	require.ErrorIs(t, err, ErrVideoExtensionLimitExceeded)

	previous := extensionSourceForTest(t, accountID, 6, 48)
	request.Seconds = 4
	_, _, err = resolveVideoExecutionSpec(request, accountID, VideoProviderOpenAI, previous)
	require.ErrorIs(t, err, ErrVideoExtensionLimitExceeded)

	legacy := *previous
	legacy.RequestAttributes = map[string]any{}
	_, _, err = resolveVideoExecutionSpec(request, accountID, VideoProviderOpenAI, &legacy)
	require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)

	previous = extensionSourceForTest(t, accountID, 5, 100)
	request.Seconds = 20
	_, spec, err = resolveVideoExecutionSpec(request, accountID, VideoProviderOpenAI, previous)
	require.NoError(t, err)
	require.Equal(t, 6, spec.ExtensionDepth)
	require.Equal(t, float64(120), spec.TotalSeconds)
}

func extensionSourceForTest(t *testing.T, accountID int64, depth int, totalSeconds float64) *VideoTask {
	t.Helper()
	task := &VideoTask{
		ID: 20, Version: 7, Operation: VideoOperationExtend, Provider: VideoProviderOpenAI,
		AccountID: &accountID, UpstreamModel: OpenAIVideoModelSora2,
		ResponseMetadata:  map[string]any{"size": "1280x720", "seconds": totalSeconds},
		RequestAttributes: map[string]any{},
	}
	spec := ResolvedVideoExecutionSpec{
		Version: 2, Provider: task.Provider, AccountID: accountID, Operation: task.Operation,
		Model: task.UpstreamModel, Size: "1280x720", Seconds: 8,
		DurationSemantics: "extension_segment", SourceSeconds: totalSeconds - 8,
		ExtensionDepth: depth, TotalSeconds: totalSeconds,
	}
	hash, err := HashVideoRequest(spec)
	require.NoError(t, err)
	task.RequestAttributes["execution_spec"] = spec
	task.RequestAttributes["execution_spec_hash"] = hash
	task.RequestAttributes["execution_spec_version"] = 2
	return task
}

func TestVideoExecutionContractNormalizesSizeBeforePricingAndSubmission(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_canonical", Status: VideoGenerationCompleted,
		Metadata: map[string]any{"model": OpenAIVideoModelSora2, "size": "1280x720", "seconds": "8"}}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	request.Size = "1280X720"
	result, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "1280x720", provider.request.Size)
	require.Equal(t, "1280x720", tasks.create.RequestAttributes["size"])
	require.Equal(t, VideoBillingCapturePending, result.Task.BillingState)
}

func TestVideoExecutionContractCannotBuyOnlineExecutionAtOfflinePrices(t *testing.T) {
	for _, mode := range []string{"batch", "offline"} {
		t.Run(mode, func(t *testing.T) {
			provider := &videoProviderStub{}
			svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
			request := videoSubmitRequestForTest()
			if mode == "batch" {
				request.RequestMode = mode
			} else {
				request.InferenceMode = mode
			}
			_, err := svc.Submit(context.Background(), request)
			require.ErrorIs(t, err, ErrVideoCapabilityUnsupported)
			require.Nil(t, tasks.task)
			require.Zero(t, provider.createCalls)
			require.Zero(t, svc.admission.(*videoAdmissionStub).checks)
			hash, err := videoClientRequestHash(request)
			require.NoError(t, err)
			tasks.preflightExisting = &VideoTask{PublicID: NewVideoTaskID(), RequestHash: hash, RequestAttributes: map[string]any{"client_request_contract_version": 2}}
			result, err := svc.Submit(context.Background(), request)
			require.NoError(t, err)
			require.False(t, result.Created)
		})
	}
}

func TestVideoExecutionContractUnknownUploadOnlyMatchesFlatPrices(t *testing.T) {
	attrs := VideoPricingAttributes{Provider: "openai", Operation: VideoOperationEdit,
		InputType: string(VideoInputRoleSourceVideo), InputHasVideo: true, OutputSpecUnverified: true,
		Size: "864x480", Resolution: "480p", Seconds: 4, MaximumOutputSeconds: 4,
		AudioEnabled: videoBool(false), RequestMode: VideoRequestModeStandard, InferenceMode: VideoInferenceModeOnline}
	profile := seedanceVideoPricing()
	profile.Rules = []VideoPricingRule{
		{Key: "cheap_guessed_size", BillingUnit: VideoBillingUnitRequest, UnitPriceUSD: 1, Priority: 100,
			Conditions: VideoPricingConditions{Resolutions: []string{"480p"}}},
		{Key: "flat_edit", BillingUnit: VideoBillingUnitRequest, UnitPriceUSD: 9,
			Conditions: VideoPricingConditions{Operations: []string{VideoOperationEdit}, InputHasVideo: videoBool(true)}},
	}
	quote, err := ResolveVideoPricingConfig(profile, attrs)
	require.NoError(t, err)
	require.Equal(t, "flat_edit", quote.RuleKey)
	require.Equal(t, 9.0, quote.HoldAmount)
	require.Empty(t, quote.Attributes.Resolution)
	require.Zero(t, quote.Attributes.Seconds)
	require.Nil(t, quote.Attributes.AudioEnabled)
	channel := &ChannelModelPricing{BillingMode: BillingModeVideo, Intervals: []PricingInterval{
		{ID: 1, PerRequestPrice: videoPrice(1), BillingUnit: videoUnit(VideoBillingUnitRequest), Priority: 100, Conditions: json.RawMessage(`{"seconds":[4]}`)},
		{ID: 2, PerRequestPrice: videoPrice(1), BillingUnit: videoUnit(VideoBillingUnitSecond), Priority: 99},
		{ID: 3, PerRequestPrice: videoPrice(9), BillingUnit: videoUnit(VideoBillingUnitRequest)},
	}}
	quote, err = ResolveVideoPrice(channel, attrs)
	require.NoError(t, err)
	require.Equal(t, int64(3), quote.RuleID)
	require.Equal(t, 1.0, quote.MaximumUnits)
	channel.Intervals = channel.Intervals[:2]
	_, err = ResolveVideoPrice(channel, attrs)
	require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)
}

func TestVideoExecutionContractUploadedEditAdmission(t *testing.T) {
	for _, field := range []string{"valid_flat", "model", "seconds", "size", "quality", "audio", "metered"} {
		t.Run(field, func(t *testing.T) {
			provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_upload", Status: VideoGenerationQueued}}
			group := videoGroupForTest(SubscriptionTypeStandard)
			group.ModelPricing[0].Intervals[0].BillingUnit = videoUnit(VideoBillingUnitRequest)
			svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
			request := videoSubmitRequestForTest()
			request.Operation, request.Seconds, request.Size = VideoOperationEdit, 0, ""
			request.Inputs = []VideoInput{{VideoInputManifestEntry: VideoInputManifestEntry{
				Role: VideoInputRoleSourceVideo, MIMEType: "video/mp4", FileName: "source.mp4", SHA256: "abcd", Size: 4,
			}, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("clip")), nil }}}
			switch field {
			case "model":
				request.Model = ""
			case "seconds":
				request.Seconds = 4
			case "size":
				request.Size = "1280x720"
			case "quality":
				request.Quality = "low"
			case "audio":
				request.AudioEnabled = videoBool(false)
			case "metered":
				group.ModelPricing[0].Intervals[0].BillingUnit = videoUnit(VideoBillingUnitSecond)
			}
			resolved, err := svc.resolveSubmission(context.Background(), request, nil)
			if field == "valid_flat" {
				require.NoError(t, err)
				require.Equal(t, 1.0, resolved.quote.HoldAmount)
				require.Zero(t, resolved.providerRequest.Seconds)
				require.Empty(t, resolved.providerRequest.Size)
				require.NotEmpty(t, resolved.executionSpecHash)
				return
			}
			require.Error(t, err)
			require.Nil(t, tasks.task)
			require.Zero(t, provider.createCalls)
			require.Zero(t, svc.admission.(*videoAdmissionStub).checks)
		})
	}
}

type videoSpecRecoveryStub struct {
	*videoSettlementRepoStub
	command *UsageBillingCommand
}

func (repo *videoSpecRecoveryStub) ResumeVideoBalanceSettlement(context.Context, *VideoTask) (*UsageBillingApplyResult, *UsageBillingCommand, bool, error) {
	return &UsageBillingApplyResult{Applied: true, UsageLogRecorded: true,
		OutboxReceipt: &UsageBillingOutboxReceipt{ID: 9, WorkerID: "frozen-intent"}}, repo.command, true, nil
}

func TestVideoExecutionContractSettlementGuardPreservesDurableIntent(t *testing.T) {
	task := baseVideoWorkerTask()
	task.GenerationState, task.BillingState, task.ActualCost = VideoGenerationCompleted, VideoBillingCapturePending, floatPointer(8)
	bindVideoExecutionSpecForTest(t, task, 0)
	task.ResponseMetadata = map[string]any{"model": OpenAIVideoModelSora2Pro}
	worker, tasks, settlements, _ := newVideoWorkerForTest(task, nil)
	require.NoError(t, worker.settle(context.Background(), task))
	require.Equal(t, VideoBillingManualReview, tasks.task.BillingState)
	require.Nil(t, settlements.settlement)
	task.BillingState = VideoBillingCapturePending
	frozen := &UsageBillingCommand{RequestID: VideoTaskCaptureRequestID(task.PublicID), ActualCost: 7}
	worker.settlements = &videoSpecRecoveryStub{videoSettlementRepoStub: settlements, command: frozen}
	worker.finalize = func(_ context.Context, command *UsageBillingCommand, _ *UsageBillingApplyResult) error {
		require.Same(t, frozen, command)
		return nil
	}
	require.NoError(t, worker.settle(context.Background(), task))
	require.True(t, settlements.acked)
	require.Nil(t, settlements.settlement)
}
