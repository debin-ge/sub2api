package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type GrokVideoMigrationCase struct {
	LegacyResolution string  `json:"legacy_resolution,omitempty"`
	Model            string  `json:"model"`
	Operation        string  `json:"operation"`
	Resolution       string  `json:"resolution"`
	Seconds          int     `json:"seconds"`
	SourceSeconds    float64 `json:"source_seconds,omitempty"`
	InputType        string  `json:"input_type,omitempty"`
	GenerateAudio    *bool   `json:"generate_audio,omitempty"`
}
type GrokVideoMigrationPreviewRequest struct {
	APIKeyID             int64                    `json:"api_key_id"`
	AccountID            int64                    `json:"account_id"`
	Cases                []GrokVideoMigrationCase `json:"cases"`
	ProposedModelPricing []ChannelModelPricing    `json:"proposed_model_pricing,omitempty"`
}
type GrokVideoMigrationComparison struct {
	LegacyAfterCost  *float64               `json:"legacy_after_cost,omitempty"`
	LegacySeconds    int                    `json:"legacy_seconds"`
	LegacyResolution string                 `json:"legacy_resolution"`
	Notes            []string               `json:"notes,omitempty"`
	Case             GrokVideoMigrationCase `json:"case"`
	RequestedModel   string                 `json:"requested_model"`
	ChannelModel     string                 `json:"channel_model"`
	UpstreamModel    string                 `json:"upstream_model"`
	LegacyCost       *float64               `json:"legacy_cost,omitempty"`
	ManagedCost      *float64               `json:"managed_cost,omitempty"`
	HoldAmount       *float64               `json:"hold_amount,omitempty"`
	SuggestedRule    *VideoPricingRule      `json:"suggested_rule,omitempty"`
	PricingSource    string                 `json:"pricing_source,omitempty"`
	Error            string                 `json:"error,omitempty"`
}
type GrokVideoMigrationPreview struct {
	GroupID     int64                          `json:"group_id"`
	Ready       bool                           `json:"ready"`
	Warnings    []string                       `json:"warnings"`
	Comparisons []GrokVideoMigrationComparison `json:"comparisons"`
	Fingerprint string                         `json:"fingerprint"`
	GeneratedAt time.Time                      `json:"generated_at"`
}

// PreviewGrokMigration is strictly read-only. It uses the live legacy price
// resolver and the managed resolver on the same owner/account/sample matrix.
// Suggestions are scoped to the sample: applying them remains an explicit
// configuration edit, never an implicit price fallback on paid requests.
func (s *VideoAdminService) PreviewGrokMigration(ctx context.Context, in GrokVideoMigrationPreviewRequest) (*GrokVideoMigrationPreview, error) {
	if s == nil || s.grok == nil || s.grok.keys == nil || s.taskSvc == nil || s.taskSvc.groups == nil || s.taskSvc.pricing == nil || in.APIKeyID <= 0 || in.AccountID <= 0 || len(in.Cases) == 0 || len(in.Cases) > 128 || len(in.ProposedModelPricing) > 128 {
		return nil, ErrVideoInvalidRequest
	}
	key, err := s.grok.keys.GetByID(ctx, in.APIKeyID)
	if err != nil {
		return nil, err
	}
	if key.GroupID == nil {
		return nil, ErrVideoInvalidRequest
	}
	originalGroup, err := s.taskSvc.groups.GetByID(ctx, *key.GroupID)
	if err != nil {
		return nil, err
	}
	groupCopy := *originalGroup
	keyCopy := *key
	key = &keyCopy
	key.Group = &groupCopy
	account, err := s.grok.accounts.GetByID(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformGrok || !s.grok.gateway.openAIAccountMatchesSchedulingGroup(account, key.GroupID) || !canScheduleAccountForUser(ctx, s.grok.accounts, account, key.UserID) {
		return nil, ErrVideoNoAccountAvailable
	}
	report := &GrokVideoMigrationPreview{GroupID: groupCopy.ID, Ready: true, Warnings: []string{}, Comparisons: []GrokVideoMigrationComparison{}, GeneratedAt: time.Now().UTC()}
	if groupCopy.IsSubscriptionType() {
		report.Ready = false
		report.Warnings = append(report.Warnings, "subscription_groups_are_not_supported_by_managed_video")
	}
	if s.grok.cfg != nil && s.grok.cfg.RunMode == config.RunModeSimple {
		report.Ready = false
		report.Warnings = append(report.Warnings, "simple_mode_changes_from_legacy_usage_only_to_managed_balance_holds")
	}
	multiplier, err := s.taskSvc.videoCustomerMultiplier(ctx, key, &groupCopy)
	if err != nil {
		return nil, err
	}
	candidateGroup := groupCopy
	if in.ProposedModelPricing != nil {
		candidateGroup.ModelPricing = append([]ChannelModelPricing(nil), in.ProposedModelPricing...)
	}
	priceCtx := WithResolvedTargetPlatform(ctx, PlatformGrok)
	for index, sample := range in.Cases {
		sample.Operation = normalizeVideoOperation(sample.Operation)
		if sample.Model == "" || len(sample.Model) > 200 || sample.Seconds < 0 || sample.Seconds > 120 || !finiteNonNegative(sample.SourceSeconds) || sample.SourceSeconds > 120 {
			return nil, ErrVideoInvalidRequest
		}
		comparison := GrokVideoMigrationComparison{Case: sample, RequestedModel: sample.Model}
		publicModel := sample.Model
		if groupCopy.Platform == PlatformComposite {
			if s.taskSvc.composite == nil {
				return nil, ErrVideoNoAccountAvailable
			}
			decision, e := s.taskSvc.composite.Resolve(ctx, groupCopy.ID, sample.Model, videoEndpointForOperation(sample.Operation))
			if e != nil {
				return nil, e
			}
			if !decision.Matched || decision.TargetPlatform != PlatformGrok {
				comparison.Error = "route_does_not_target_grok"
				report.Ready = false
				report.Comparisons = append(report.Comparisons, comparison)
				continue
			}
			publicModel = decision.UpstreamModel
		} else if groupCopy.Platform != PlatformGrok {
			return nil, ErrVideoProviderUnsupported
		}
		mapping := ChannelMappingResult{MappedModel: publicModel, BillingModelSource: BillingModelSourceChannelMapped}
		if s.taskSvc.channels != nil {
			mapping = s.taskSvc.channels.ResolveChannelMapping(priceCtx, groupCopy.ID, publicModel)
		}
		channelModel := firstNonEmptyString(mapping.MappedModel, publicModel)
		comparison.ChannelModel = channelModel
		comparison.UpstreamModel = account.GetMappedModel(channelModel)
		if !account.IsModelSupported(channelModel) || comparison.UpstreamModel == "" {
			comparison.Error = "account_model_not_supported"
			report.Ready = false
			report.Comparisons = append(report.Comparisons, comparison)
			continue
		}
		normalized := (&GrokVideoProvider{}).ApplyRequestDefaults(VideoCreateRequest{RequestedModel: channelModel, Model: comparison.UpstreamModel, Operation: sample.Operation, Seconds: sample.Seconds, Resolution: sample.Resolution, AudioEnabled: sample.GenerateAudio})
		if sample.Operation == VideoOperationGenerate {
			sample.Seconds = normalized.Seconds
			sample.Resolution = normalized.Resolution
			sample.GenerateAudio = normalized.AudioEnabled
			comparison.Case = sample
		}
		oldResult := &OpenAIForwardResult{Model: publicModel, BillingModel: comparison.UpstreamModel, VideoCount: 1, VideoResolution: sample.Resolution, VideoDurationSeconds: sample.Seconds}
		if sample.Operation == VideoOperationEdit || sample.Operation == VideoOperationExtend {
			oldResult.VideoResolution = firstNonEmptyString(sample.LegacyResolution, "480p")
			seconds := sample.SourceSeconds
			if sample.Operation == VideoOperationExtend {
				seconds += float64(sample.Seconds)
			}
			oldResult.VideoDurationSeconds = int(seconds)
		}
		comparison.LegacySeconds = NormalizeVideoBillingDurationSecondsOrDefault(oldResult.VideoDurationSeconds)
		comparison.LegacyResolution = NormalizeVideoBillingResolutionOrDefault(oldResult.VideoResolution)
		legacy, oldErr := s.grok.gateway.calculateOpenAIVideoCost(priceCtx, comparison.UpstreamModel, key, oldResult, multiplier)
		if oldErr != nil {
			comparison.Error = "legacy_price_unavailable"
		} else {
			comparison.LegacyCost = &legacy.ActualCost
		}
		afterKey := *key
		afterKey.Group = &candidateGroup
		legacyAfter, afterErr := s.grok.gateway.calculateOpenAIVideoCost(priceCtx, comparison.UpstreamModel, &afterKey, oldResult, multiplier)
		if afterErr != nil {
			if comparison.Error == "" {
				comparison.Error = "legacy_price_after_migration_unavailable"
			}
		} else {
			comparison.LegacyAfterCost = &legacyAfter.ActualCost
			if comparison.LegacyCost != nil && math.Abs(legacyAfter.ActualCost-*comparison.LegacyCost) > 1e-8 {
				comparison.Error = "legacy_pending_task_price_would_change"
			}
		}
		// Infer only a proven linear per-second or constant per-request retail rule.
		if oldErr == nil && sample.Operation == VideoOperationGenerate {
			probe := *oldResult
			probe.VideoDurationSeconds = 1
			one, e1 := s.grok.gateway.calculateOpenAIVideoCost(priceCtx, comparison.UpstreamModel, key, &probe, multiplier)
			probe.VideoDurationSeconds = 15
			fifteen, e15 := s.grok.gateway.calculateOpenAIVideoCost(priceCtx, comparison.UpstreamModel, key, &probe, multiplier)
			if e1 == nil && e15 == nil {
				unit := ""
				if math.Abs(one.TotalCost*15-fifteen.TotalCost) < 1e-8 {
					unit = VideoBillingUnitSecond
				} else if math.Abs(one.TotalCost-fifteen.TotalCost) < 1e-8 {
					unit = VideoBillingUnitRequest
				}
				expected := one.TotalCost
				if unit == VideoBillingUnitSecond {
					expected *= float64(NormalizeVideoBillingDurationSecondsOrDefault(sample.Seconds))
				}
				if math.Abs(expected-legacy.TotalCost) > 1e-8 {
					unit = ""
				}
				if unit != "" {
					comparison.SuggestedRule = &VideoPricingRule{Key: fmt.Sprintf("grok-migration-%d", index+1), BillingUnit: unit, UnitPriceUSD: one.TotalCost, Conditions: VideoPricingConditions{Providers: []string{PlatformGrok}, Operations: []string{sample.Operation}, Resolutions: []string{sample.Resolution}}}
				}
			}
		}
		attrs := VideoPricingAttributes{Provider: PlatformGrok, Model: comparison.UpstreamModel, Operation: sample.Operation, Resolution: sample.Resolution, Seconds: sample.Seconds, MaximumOutputSeconds: sample.Seconds, InputType: firstNonEmptyString(sample.InputType, "text"), AudioEnabled: sample.GenerateAudio, CustomerMultiplier: &multiplier, At: report.GeneratedAt}
		switch sample.Operation {
		case VideoOperationGenerate:
		case VideoOperationEdit:
			if !finitePositive(sample.SourceSeconds) {
				comparison.Error = "verified_source_seconds_required"
			} else {
				attrs.Seconds = int(math.Ceil(sample.SourceSeconds))
				attrs.MaximumOutputSeconds = attrs.Seconds
				attrs.BillableSeconds = sample.SourceSeconds
				attrs.InputHasVideo = true
				attrs.InputVideoSeconds = sample.SourceSeconds
				attrs.InputType = string(VideoInputRoleSourceVideo)
			}
		case VideoOperationExtend:
			if !finitePositive(sample.SourceSeconds) || sample.Seconds <= 0 {
				comparison.Error = "verified_source_seconds_required"
			}
			attrs.InputHasVideo = true
			attrs.InputVideoSeconds = sample.SourceSeconds
			attrs.InputType = string(VideoInputRoleSourceVideo)
		default:
			return nil, ErrVideoInvalidRequest
		}
		quote, newErr := s.taskSvc.pricing.Resolve(priceCtx, VideoPricingResolveRequest{Group: &candidateGroup, Platform: PlatformGrok, Mapping: mapping, RequestedModel: sample.Model, ChannelModel: channelModel, UpstreamModel: comparison.UpstreamModel, Provider: PlatformGrok, Attributes: attrs})
		if newErr != nil {
			if comparison.Error == "" {
				comparison.Error = "managed_price_unavailable"
			}
		} else {
			comparison.ManagedCost = &quote.EstimatedCost
			comparison.HoldAmount = &quote.HoldAmount
			comparison.PricingSource = quote.Source
			if comparison.LegacyCost != nil && math.Abs(*comparison.LegacyCost-quote.EstimatedCost) > 1e-8 && comparison.Error == "" {
				comparison.Error = "customer_price_difference"
				if sample.Operation != VideoOperationGenerate {
					comparison.Notes = append(comparison.Notes, "legacy_uses_integer_clamped_output_and_request_resolution; managed_uses_verified_source_and_billable_delta")
				}
			}
		}
		if comparison.Error != "" {
			report.Ready = false
		}
		report.Comparisons = append(report.Comparisons, comparison)
	}
	report.Fingerprint, err = HashVideoRequest(map[string]any{"group_id": report.GroupID, "api_key_id": in.APIKeyID, "account_id": in.AccountID, "comparisons": report.Comparisons, "warnings": report.Warnings})
	if err != nil {
		return nil, err
	}
	return report, nil
}
