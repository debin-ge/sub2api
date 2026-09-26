package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
)

type VideoSubmitRequest struct {
	APIKey          *APIKey
	Operation       string
	Model           string
	Prompt          string
	Seconds         int
	Size            string
	Quality         string
	AudioEnabled    *bool
	ServiceTier     string
	RequestMode     string
	InferenceMode   string
	InputReference  *ProviderInputReference
	ReferenceMedia  ProviderVideoReferenceMedia
	Inputs          []VideoInput
	CharacterIDs    []string
	SourceVideoID   string
	IdempotencyKey  string
	CallbackURL     string
	ProviderOptions map[string]any
}

type VideoSubmitResult struct {
	Task     *VideoTask
	Resource *VideoResource
	Created  bool
}

type VideoTaskService struct {
	tasks       VideoTaskRepository
	resources   VideoResourceRepository
	queue       VideoTaskQueue
	accounts    AccountRepository
	groups      GroupRepository
	userRates   UserGroupRateRepository
	channels    *ChannelService
	composite   *CompositeRouteResolver
	providers   *VideoProviderRegistry
	pricing     *VideoPricingResolver
	settlements VideoBalanceSettlementRepository
	admission   VideoTaskAdmission
	encryptor   SecretEncryptor
	cfg         *config.Config
	now         func() time.Time
}

func NewVideoTaskService(
	tasks VideoTaskRepository,
	resources VideoResourceRepository,
	queue VideoTaskQueue,
	accounts AccountRepository,
	groups GroupRepository,
	userRates UserGroupRateRepository,
	channels *ChannelService,
	composite *CompositeRouteResolver,
	providers *VideoProviderRegistry,
	pricing *VideoPricingResolver,
	settlements VideoBalanceSettlementRepository,
	encryptor SecretEncryptor,
	billingCache *BillingCacheService,
	cfg *config.Config,
) *VideoTaskService {
	svc := &VideoTaskService{
		tasks: tasks, resources: resources, queue: queue, accounts: accounts,
		groups: groups, userRates: userRates, channels: channels, composite: composite,
		providers: providers, pricing: pricing, settlements: settlements, encryptor: encryptor, cfg: cfg,
		now: time.Now,
	}
	// admission is an interface, so assigning a nil *BillingCacheService straight
	// into it would produce a non-nil interface wrapping a nil pointer: every
	// `s.admission != nil` guard would pass and then call a method on a nil
	// receiver, which answers ErrBillingServiceUnavailable. Assign only a live
	// cache, so the nil checks around the field mean what they read.
	if billingCache != nil {
		svc.admission = billingCache
	}
	return svc
}

type resolvedVideoSubmission struct {
	providerRequest          VideoCreateRequest
	executionSpec            ResolvedVideoExecutionSpec
	executionSpecHash        string
	request                  VideoSubmitRequest
	group                    *Group
	provider                 VideoProvider
	account                  *Account
	endpoint                 string
	requestedModel           string
	publicModel              string
	channelModel             string
	upstreamModel            string
	channelID                *int64
	characters               []ProviderResourceRef
	source                   *ProviderTaskRef
	sourceTask               *VideoTask
	quote                    *VideoPriceQuote
	requestHash              string
	callbackURLEnc           string
	callbackRetryHours       int
	callbackDisclosurePolicy string
}

const videoSubmitRecoveryGrace = 5 * time.Second

const (
	videoSubmissionReconciliationPollInterval  = 10 * time.Second
	videoDefaultSubmissionReconciliationWindow = 30 * time.Minute
)

func (s *VideoTaskService) Submit(ctx context.Context, request VideoSubmitRequest) (result *VideoSubmitResult, returnErr error) {
	startedAt := time.Now()
	providerName := ""
	defer func() {
		observability.DefaultVideoMetrics().RecordSubmission(providerName, request.Operation, videoSubmissionMetricResult(result, returnErr), time.Since(startedAt))
	}()
	if s == nil || s.tasks == nil || s.providers == nil || request.APIKey == nil {
		return nil, ErrVideoInvalidRequest
	}
	if s.cfg == nil || !s.cfg.Gateway.Video.Enabled {
		return nil, ErrVideoDisabled
	}
	request.Operation = normalizeVideoOperation(request.Operation)
	if err := ValidateVideoReleaseSubmission(request); err != nil {
		return nil, err
	}
	if err := validateVideoSubmitRequest(request); err != nil {
		return nil, err
	}
	requestHash, err := videoClientRequestHash(request)
	if err != nil {
		return nil, err
	}
	if replay, replayErr := s.lookupVideoSubmissionReplay(ctx, request, requestHash); replayErr != nil || replay != nil {
		return replay, replayErr
	}
	if !s.cfg.Gateway.Video.CreationEnabled {
		return s.replayAfterVideoCreationFailure(ctx, request, requestHash, ErrVideoCreationDisabled)
	}
	if s.admission == nil {
		return nil, ErrBillingServiceUnavailable
	}
	publicID := NewVideoTaskID()
	operationRef := strings.TrimSpace(request.IdempotencyKey)
	if operationRef == "" {
		operationRef = publicID
	}
	operationToken, err := HashVideoRequest(map[string]any{"user_id": request.APIKey.UserID, "endpoint": videoEndpointForOperation(request.Operation), "operation_ref": operationRef})
	if err != nil {
		return nil, err
	}
	originalRequest := request
	excludedAccounts := make(map[int64]struct{})
	timeout := 180 * time.Second
	if s.cfg.Gateway.Video.SubmitTimeoutSeconds > 0 {
		timeout = time.Duration(s.cfg.Gateway.Video.SubmitTimeoutSeconds) * time.Second
	}
	submitRecoveryAt := s.now().UTC().Add(timeout + videoSubmitRecoveryGrace)
	var resolved *resolvedVideoSubmission
	var task *VideoTask
	for {
		resolved, err = s.resolveSubmission(ctx, originalRequest, excludedAccounts)
		if err != nil {
			if errors.Is(err, ErrVideoNoAccountAvailable) && len(excludedAccounts) > 0 {
				return s.replayAfterVideoCreationFailure(ctx, originalRequest, requestHash, ErrVideoAccountConcurrencyLimited)
			}
			return s.replayAfterVideoCreationFailure(ctx, originalRequest, requestHash, err)
		}
		resolved.requestHash = requestHash
		request = resolved.request
		providerName = resolved.provider.Name()
		if err := s.admission.CheckVideoAdmission(ctx, request.APIKey, resolved.group, resolved.provider.Name(), operationToken); err != nil {
			return s.replayAfterVideoCreationFailure(ctx, originalRequest, requestHash, err)
		}
		owner := VideoOwner{UserID: request.APIKey.UserID, APIKeyID: request.APIKey.ID, GroupID: request.APIKey.GroupID}
		var created bool
		task, created, err = s.tasks.CreateHeldVideoTask(ctx, VideoCreateTaskParams{
			PublicID: publicID, Owner: owner, ChannelID: resolved.channelID,
			AccountID: resolved.account.ID, AccountOwnerUserID: resolved.account.VideoOwnerUserID,
			Provider: resolved.provider.Name(), Operation: request.Operation,
			ParentTaskID: videoParentTaskID(resolved.sourceTask), RootTaskID: videoRootTaskID(resolved.sourceTask),
			Endpoint: resolved.endpoint, RequestedModel: resolved.requestedModel,
			PublicModel: resolved.publicModel, ChannelModel: resolved.channelModel,
			UpstreamModel: resolved.upstreamModel, RequestHash: resolved.requestHash,
			IdempotencyKey: strings.TrimSpace(request.IdempotencyKey), InputManifest: videoInputManifest(request.Inputs),
			RequestAttributes: videoRequestAttributes(request, resolved), StableClientToken: publicID,
			BillingUnit: resolved.quote.BillingUnit, EstimatedUnits: resolved.quote.EstimatedUnits,
			PriceSnapshot: videoPriceSnapshot(resolved.quote), ProviderCostSnapshot: map[string]any{
				"account_rate_multiplier": resolved.account.BillingRateMultiplier(),
			}, Currency: "USD", HoldID: VideoTaskHoldRequestID(publicID), HoldAmount: resolved.quote.HoldAmount,
			CallbackURLEnc: resolved.callbackURLEnc, NextActionAt: &submitRecoveryAt,
			MaxAccountConcurrency: maxVideoAccountConcurrency(resolved.account),
		})
		if err != nil {
			observability.DefaultVideoMetrics().RecordHold("error", resolved.quote.HoldAmount)
		} else if created {
			observability.DefaultVideoMetrics().RecordHold("success", resolved.quote.HoldAmount)
		}
		if errors.Is(err, ErrVideoAccountConcurrencyLimited) && resolved.source == nil && len(resolved.characters) == 0 {
			excludedAccounts[resolved.account.ID] = struct{}{}
			continue
		}
		if err != nil {
			return s.replayAfterVideoCreationFailure(ctx, originalRequest, requestHash, err)
		}
		if !created {
			return s.replayedSubmitResult(ctx, request, task)
		}
		if err := s.admission.InvalidateVideoHold(context.WithoutCancel(ctx), task.UserID); err != nil {
			s.markPreSubmitFailure(context.WithoutCancel(ctx), task, err)
			return nil, err
		}
		break
	}

	task, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: VideoGenerationSubmitting,
		NextActionAt:    &submitRecoveryAt,
		EventType:       "provider_submitting",
		EventPayload:    map[string]any{"account_id": resolved.account.ID},
	})
	if err != nil {
		s.markPreSubmitFailure(context.WithoutCancel(ctx), task, err)
		return nil, err
	}

	submitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	providerTask, providerResource, providerErr := s.submitToProvider(submitCtx, resolved, request, task)
	cancel()
	if providerErr != nil {
		return s.handleSubmissionError(context.WithoutCancel(ctx), task, providerErr)
	}

	if providerResource != nil {
		name, _ := request.ProviderOptions["name"].(string)
		return s.finishCharacterSubmission(context.WithoutCancel(ctx), task, name, providerResource)
	}
	return s.finishTaskSubmission(context.WithoutCancel(ctx), task, resolved, providerTask)
}

func videoSubmissionMetricResult(result *VideoSubmitResult, err error) string {
	if err == nil {
		if result != nil && !result.Created {
			return "replayed"
		}
		return "accepted"
	}
	if errors.Is(err, ErrVideoSubmissionUnknown) {
		return "submission_unknown"
	}
	// Classify exactly as handleSubmissionError does, so the counter is the one
	// signal that an upstream job may exist that this platform will never poll.
	// Errors raised before the provider was called are not provider errors and
	// stay in the generic bucket.
	var providerErr *VideoProviderError
	if errors.As(err, &providerErr) {
		if providerErr.Certainty == VideoSubmissionRejected {
			return "rejected"
		}
		return "submission_unknown"
	}
	return "error"
}

func (s *VideoTaskService) resolveSubmission(ctx context.Context, request VideoSubmitRequest, excludedAccounts map[int64]struct{}) (*resolvedVideoSubmission, error) {
	if videoRequestModeOrDefault(request.RequestMode) != VideoRequestModeStandard || videoInferenceModeOrDefault(request.InferenceMode) != VideoInferenceModeOnline {
		return nil, ErrVideoCapabilityUnsupported
	}
	if request.APIKey.GroupID == nil || *request.APIKey.GroupID <= 0 || s.groups == nil {
		return nil, ErrVideoPricingMissing
	}
	group, err := s.groups.GetByID(ctx, *request.APIKey.GroupID)
	if err != nil || group == nil {
		return nil, ErrVideoPricingMissing
	}
	if group.IsSubscriptionType() {
		return nil, ErrVideoSubscriptionUnsupported
	}
	if group.Status != StatusActive || group.ID != *request.APIKey.GroupID {
		return nil, ErrVideoNoAccountAvailable
	}
	endpoint := videoEndpointForOperation(request.Operation)
	requestedModel := strings.TrimSpace(request.Model)
	publicModel := requestedModel
	characters, forcedAccountID, err := s.resolveCharacters(ctx, request.APIKey.UserID, request.CharacterIDs)
	if err != nil {
		return nil, err
	}
	source, sourceTask, sourceAccountID, err := s.resolveSource(ctx, request.APIKey.UserID, request.Operation, request.SourceVideoID)
	if err != nil {
		return nil, err
	}
	if forcedAccountID != 0 && sourceAccountID != 0 && forcedAccountID != sourceAccountID {
		return nil, ErrVideoCapabilityUnsupported
	}
	if forcedAccountID == 0 {
		forcedAccountID = sourceAccountID
	}
	if requestedModel == "" && sourceTask != nil {
		requestedModel, err = s.defaultVideoModel(sourceTask, sourceTask.Provider)
		if err != nil {
			return nil, err
		}
		publicModel = requestedModel
	}
	if requestedModel == "" && request.Operation == VideoOperationEdit && sourceTask == nil && len(request.Inputs) > 0 {
		return nil, ErrVideoInvalidRequest
	}
	if requestedModel == "" && group.Platform != PlatformComposite {
		requestedModel, err = s.defaultVideoModel(nil, group.Platform)
		if err != nil {
			return nil, err
		}
		publicModel = requestedModel
	}
	targetPlatform := group.Platform
	if targetPlatform == PlatformComposite {
		decision, resolved := CompositeRouteDecisionFromContext(ctx)
		if resolved {
			if requestedModel == "" {
				requestedModel = strings.TrimSpace(decision.PublicModel)
				publicModel = requestedModel
			}
			if !decision.Matched || decision.GroupID != group.ID || decision.Endpoint != endpoint || decision.PublicModel != requestedModel {
				return nil, ErrVideoNoAccountAvailable
			}
		} else if pinnedPlatform, pinned := ResolvedTargetPlatformFromContext(ctx); pinned {
			if sourceTask == nil {
				return nil, ErrVideoNoAccountAvailable
			}
			targetPlatform = pinnedPlatform
			publicModel = firstNonEmptyString(sourceTask.UpstreamModel, sourceTask.PublicModel, requestedModel)
		} else {
			if s.composite == nil {
				return nil, ErrVideoNoAccountAvailable
			}
			decision, err = s.composite.Resolve(ctx, group.ID, requestedModel, endpoint)
			if err != nil {
				return nil, err
			}
			if !decision.Matched {
				return nil, ErrVideoNoAccountAvailable
			}
		}
		if resolved || decision.Matched {
			targetPlatform, publicModel = decision.TargetPlatform, decision.UpstreamModel
		}
	}
	providerName, supported := managedVideoProviderForPlatform(targetPlatform)
	if !supported {
		return nil, ErrVideoNoAccountAvailable
	}
	if requestedModel == "" {
		if request.Operation == VideoOperationEdit && sourceTask == nil && len(request.Inputs) > 0 {
			return nil, ErrVideoInvalidRequest
		}
		requestedModel, err = s.defaultVideoModel(sourceTask, targetPlatform)
		if err != nil {
			return nil, err
		}
		publicModel = requestedModel
	}
	if source != nil && source.Provider != providerName {
		return nil, ErrVideoCapabilityUnsupported
	}
	for i := range characters {
		if characters[i].Provider != providerName {
			return nil, ErrVideoCapabilityUnsupported
		}
	}
	resolvedCtx := WithResolvedTargetPlatform(ctx, targetPlatform)
	channelModel := publicModel
	mapping := ChannelMappingResult{MappedModel: publicModel, BillingModelSource: BillingModelSourceChannelMapped}
	var channelID *int64
	if s.channels != nil {
		mapping = s.channels.ResolveChannelMapping(resolvedCtx, group.ID, publicModel)
		if strings.TrimSpace(mapping.MappedModel) != "" {
			channelModel = mapping.MappedModel
		}
		if mapping.ChannelID > 0 {
			id := mapping.ChannelID
			channelID = &id
		}
	}

	requiresOwnedAccount := request.InputReference != nil && strings.TrimSpace(request.InputReference.FileID) != ""
	account, upstreamModel, provider, err := s.selectVideoAccount(
		ctx, request.APIKey.UserID, group.ID, targetPlatform, providerName, channelModel, forcedAccountID,
		excludedAccounts, requiresOwnedAccount,
	)
	if err != nil {
		return nil, err
	}
	for i := range characters {
		if characters[i].AccountID != account.ID {
			return nil, ErrVideoCapabilityUnsupported
		}
	}
	if source != nil && source.AccountID != account.ID {
		return nil, ErrVideoCapabilityUnsupported
	}
	providerRequest := request.providerRequest(publicModel, upstreamModel, characters)
	providerRequest = ApplyVideoCapabilityDefaults(provider.Capabilities(), providerRequest)
	for _, input := range request.Inputs {
		capabilities := provider.Capabilities()
		if !capabilities.SupportsInputForOperation(request.Operation, input.Role) || !capabilities.SupportsInput(input.Role, input.MIMEType, input.Size) {
			return nil, ErrVideoInputUnsupported
		}
	}
	providerRequest, executionSpec, err := resolveVideoExecutionSpec(providerRequest, account.ID, provider.Name(), sourceTask)
	if err != nil {
		return nil, err
	}
	executionSpecHash, err := HashVideoRequest(executionSpec)
	if err != nil {
		return nil, ErrVideoInvalidRequest
	}
	request.Model = requestedModel
	request.Seconds = providerRequest.Seconds
	request.Size = providerRequest.Size
	if err := ValidateVideoCreateCapabilities(provider.Capabilities(), providerRequest, request.Inputs); err != nil {
		return nil, err
	}
	if validator, ok := provider.(VideoSubmissionValidator); ok {
		if err := validator.ValidateSubmission(account, providerRequest, request.Inputs); err != nil {
			return nil, err
		}
	}

	multiplier, err := s.videoCustomerMultiplier(ctx, request.APIKey, group)
	if err != nil {
		return nil, ErrVideoPricingMissing
	}
	inputType := videoPricingInputType(request)
	attrs := VideoPricingAttributes{
		Provider: provider.Name(), Model: upstreamModel, Operation: request.Operation,
		Size: request.Size, Resolution: executionSpec.Resolution,
		Seconds: request.Seconds, InputType: inputType,
		MaximumOutputSeconds: videoMaximumOutputSeconds(request, providerRequest),
		OutputSpecUnverified: executionSpec.OutputUnverified,
		InputHasVideo:        videoInputHasVideo(request), InputVideoSeconds: trustedVideoInputSeconds(request, sourceTask),
		AudioEnabled: request.AudioEnabled, Quality: request.Quality, ServiceTier: request.ServiceTier,
		RequestMode: videoRequestModeOrDefault(request.RequestMode), InferenceMode: videoInferenceModeOrDefault(request.InferenceMode),
		CustomerMultiplier: &multiplier, At: s.now().UTC(),
	}
	if s.pricing == nil {
		return nil, ErrVideoPricingMissing
	}
	quote, err := s.pricing.Resolve(resolvedCtx, VideoPricingResolveRequest{
		Group: group, Platform: targetPlatform, Mapping: mapping,
		RequestedModel: requestedModel, ChannelModel: channelModel, UpstreamModel: upstreamModel,
		Provider: provider.Name(), Attributes: attrs,
	})
	if err != nil {
		return nil, err
	}
	if err := validateVideoExecutionQuote(executionSpec, quote); err != nil {
		return nil, err
	}
	callbackURLEnc := ""
	callbackRetryHours := s.cfg.Gateway.Video.Callback.RetryHours
	if callbackRetryHours <= 0 {
		callbackRetryHours = 24
	}
	if callbackURL := strings.TrimSpace(request.CallbackURL); callbackURL != "" {
		if callbackRetryHours > 8760 {
			return nil, ErrVideoInvalidRequest
		}
		if s.cfg == nil || !s.cfg.Gateway.Video.Callback.Enabled || strings.TrimSpace(s.cfg.Gateway.Video.Callback.SigningSecret) == "" {
			return nil, ErrVideoCallbacksDisabled
		}
		callbackURL, err = ValidateVideoCallbackURL(ctx, callbackURL)
		if err != nil {
			return nil, ErrVideoInvalidRequest
		}
		if s.encryptor == nil {
			return nil, errors.New("video callback encryptor is not configured")
		}
		callbackURLEnc, err = s.encryptor.Encrypt(callbackURL)
		if err != nil {
			return nil, err
		}
	}
	return &resolvedVideoSubmission{
		providerRequest: providerRequest, executionSpec: executionSpec, executionSpecHash: executionSpecHash,
		callbackRetryHours:       callbackRetryHours,
		callbackDisclosurePolicy: effectiveVideoDisclosurePolicy(s.cfg.Gateway.Video.DisclosurePolicy, group.VideoDisclosurePolicy, account.VideoDisclosurePolicy),
		request:                  request, group: group, provider: provider, account: account, endpoint: endpoint,
		requestedModel: requestedModel, publicModel: publicModel, channelModel: channelModel,
		upstreamModel: upstreamModel, channelID: channelID, characters: characters,
		source: source, sourceTask: sourceTask, quote: quote, callbackURLEnc: callbackURLEnc,
	}, nil
}

func (s *VideoTaskService) replayedSubmitResult(ctx context.Context, request VideoSubmitRequest, task *VideoTask) (*VideoSubmitResult, error) {
	result := &VideoSubmitResult{Task: task, Created: false}
	if request.Operation != VideoOperationCharacterCreate || s.resources == nil {
		return result, nil
	}
	if task.GenerationState != VideoGenerationCompleted {
		return result, nil
	}
	resource, err := s.resources.GetVideoResourceBySourceTaskForOwner(ctx, request.APIKey.UserID, task.ID)
	if errors.Is(err, ErrVideoResourceNotFound) {
		resource, err = s.recoverCharacterResource(ctx, request, task)
	}
	if err != nil {
		return nil, err
	}
	result.Resource = resource
	return result, nil
}

func (s *VideoTaskService) recoverCharacterResource(ctx context.Context, request VideoSubmitRequest, task *VideoTask) (*VideoResource, error) {
	if task == nil || task.GenerationState != VideoGenerationCompleted || task.AccountID == nil ||
		task.ProviderTaskID == nil || s.accounts == nil || s.providers == nil {
		return nil, ErrVideoResourceNotFound
	}
	account, err := s.accounts.GetByID(ctx, *task.AccountID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(task.Provider)
	if !ok {
		return nil, ErrVideoProviderUnsupported
	}
	characterProvider, ok := provider.(VideoCharacterProvider)
	if !ok || !provider.Capabilities().Supports(VideoCapabilityCharacters) {
		return nil, ErrVideoCapabilityUnsupported
	}
	providerResourceID := strings.TrimSpace(*task.ProviderTaskID)
	observed, err := characterProvider.GetCharacter(ctx, account, ProviderResourceRef{
		Provider: task.Provider, AccountID: *task.AccountID, ProviderResourceID: providerResourceID,
	})
	if err != nil {
		return nil, err
	}
	if observed == nil || strings.TrimSpace(observed.ProviderResourceID) != providerResourceID {
		return nil, ErrVideoInvalidRequest
	}
	name, _ := request.ProviderOptions["name"].(string)
	if strings.TrimSpace(name) == "" {
		name, _ = observed.Metadata["name"].(string)
	}
	resource, createErr := s.resources.CreateVideoResource(ctx, VideoCreateResourceParams{
		Owner:    VideoOwner{UserID: task.UserID, APIKeyID: valueOrZero(task.APIKeyID), GroupID: task.GroupID},
		Provider: task.Provider, ChannelID: task.ChannelID, AccountID: *task.AccountID,
		SourceTaskID: &task.ID, ProviderResourceID: providerResourceID,
		Model: task.UpstreamModel, Status: "ready", Metadata: map[string]any{"name": strings.TrimSpace(name)},
		ExpiresAt: observed.ExpiresAt,
	})
	if createErr == nil {
		return resource, nil
	}
	if existing, lookupErr := s.resources.GetVideoResourceBySourceTaskForOwner(ctx, task.UserID, task.ID); lookupErr == nil {
		return existing, nil
	}
	return nil, createErr
}

func managedVideoProviderForPlatform(platform string) (string, bool) {
	switch strings.TrimSpace(platform) {
	case PlatformOpenAI:
		return VideoProviderOpenAI, true
	case PlatformByteDance:
		return VideoProviderByteDance, true
	default:
		return "", false
	}
}

// providerRequestResolution reports the output resolution a provider was asked
// to produce. Ark takes resolution as its own request parameter, so it can only
// be read back from the request — a model identifier does not encode it.
// OpenAI expresses the same thing through size and reports no resolution.
func providerRequestResolution(provider string, request VideoCreateRequest) string {
	if provider != VideoProviderByteDance {
		return ""
	}
	options, err := byteDanceGenerationOptions(request)
	if err != nil {
		return ""
	}
	return options.Resolution
}

func (s *VideoTaskService) selectVideoAccount(ctx context.Context, userID, groupID int64, targetPlatform, providerName, model string, forcedAccountID int64, excludedAccounts map[int64]struct{}, requiresOwnedAccount bool) (*Account, string, VideoProvider, error) {
	if s.accounts == nil {
		return nil, "", nil, ErrVideoNoAccountAvailable
	}
	candidates, err := s.accounts.ListSchedulableByGroupIDAndPlatform(ctx, groupID, targetPlatform)
	if err != nil {
		return nil, "", nil, err
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})
	provider, ok := s.providers.Get(providerName)
	if !ok {
		return nil, "", nil, ErrVideoProviderUnsupported
	}
	for i := range candidates {
		account := &candidates[i]
		if forcedAccountID > 0 && account.ID != forcedAccountID {
			continue
		}
		if _, excluded := excludedAccounts[account.ID]; excluded {
			continue
		}
		if !canScheduleAccountForUser(ctx, s.accounts, account, userID) {
			continue
		}
		if requiresOwnedAccount && (!account.hasVerifiedDedicatedIsolation() || *account.OwnerUserID != userID) {
			continue
		}
		if !account.IsSchedulable() || !provider.SupportsAccount(account) || !account.IsModelSupported(model) {
			continue
		}
		upstreamModel := account.GetMappedModel(model)
		if strings.TrimSpace(upstreamModel) == "" {
			continue
		}
		return account, upstreamModel, provider, nil
	}
	return nil, "", nil, ErrVideoNoAccountAvailable
}

func (s *VideoTaskService) resolveCharacters(ctx context.Context, userID int64, ids []string) ([]ProviderResourceRef, int64, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}
	if s.resources == nil || len(ids) > 2 {
		return nil, 0, ErrVideoCapabilityUnsupported
	}
	refs := make([]ProviderResourceRef, 0, len(ids))
	var accountID int64
	for _, id := range ids {
		resource, err := s.GetCharacterForOwner(ctx, userID, id)
		if err != nil {
			return nil, 0, err
		}
		if resource.Provider != VideoProviderOpenAI {
			return nil, 0, ErrVideoResourceNotFound
		}
		if accountID == 0 {
			accountID = resource.AccountID
		}
		if accountID != resource.AccountID {
			return nil, 0, ErrVideoCapabilityUnsupported
		}
		refs = append(refs, ProviderResourceRef{Provider: resource.Provider, AccountID: resource.AccountID, ProviderResourceID: resource.ProviderResourceID})
	}
	return refs, accountID, nil
}

func (s *VideoTaskService) resolveSource(ctx context.Context, userID int64, operation, sourceID string) (*ProviderTaskRef, *VideoTask, int64, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		if operation == VideoOperationExtend {
			return nil, nil, 0, ErrVideoInvalidRequest
		}
		return nil, nil, 0, nil
	}
	if !validVideoProviderIdentifier(sourceID) {
		return nil, nil, 0, ErrVideoInvalidRequest
	}
	source, err := s.tasks.GetVideoTaskForOwner(ctx, userID, sourceID)
	if errors.Is(err, ErrVideoTaskNotFound) {
		source, err = s.tasks.GetVideoTaskByProviderIDForOwner(ctx, userID, sourceID)
	}
	if err != nil || source == nil || source.ProviderTaskID == nil || source.AccountID == nil {
		return nil, nil, 0, ErrVideoTaskNotFound
	}
	if source.GenerationState != VideoGenerationCompleted || source.BillingState != VideoBillingCaptured || source.DeleteState == VideoDeleteDeleted {
		return nil, nil, 0, ErrVideoCapabilityUnsupported
	}
	ref := &ProviderTaskRef{Provider: source.Provider, AccountID: *source.AccountID, ProviderTaskID: *source.ProviderTaskID}
	return ref, source, *source.AccountID, nil
}

func videoParentTaskID(source *VideoTask) *int64 {
	if source == nil || source.ID <= 0 {
		return nil
	}
	id := source.ID
	return &id
}

func videoRootTaskID(source *VideoTask) *int64 {
	if source == nil || source.ID <= 0 {
		return nil
	}
	if source.RootTaskID != nil && *source.RootTaskID > 0 {
		id := *source.RootTaskID
		return &id
	}
	id := source.ID
	return &id
}

func (s *VideoTaskService) submitToProvider(ctx context.Context, resolved *resolvedVideoSubmission, request VideoSubmitRequest, task *VideoTask) (*ProviderVideoTask, *ProviderVideoResource, error) {
	providerRequest := resolved.providerRequest
	providerRequest.TaskID = task.PublicID
	providerRequest.ClientToken = videoStringValue(task.StableClientToken)
	switch request.Operation {
	case VideoOperationGenerate:
		providerTask, err := resolved.provider.Create(ctx, resolved.account, providerRequest, request.Inputs)
		return providerTask, nil, err
	case VideoOperationEdit:
		editor, ok := resolved.provider.(VideoEditor)
		if !ok {
			return nil, nil, videoCapabilityRejection()
		}
		providerTask, err := editor.Edit(ctx, resolved.account, VideoEditRequest{VideoCreateRequest: providerRequest, SourceTask: resolved.source}, request.Inputs)
		return providerTask, nil, err
	case VideoOperationExtend:
		extender, ok := resolved.provider.(VideoExtender)
		if !ok || resolved.source == nil {
			return nil, nil, videoCapabilityRejection()
		}
		providerTask, err := extender.Extend(ctx, resolved.account, VideoExtendRequest{VideoCreateRequest: providerRequest, SourceTask: *resolved.source})
		return providerTask, nil, err
	case VideoOperationCharacterCreate:
		characterProvider, ok := resolved.provider.(VideoCharacterProvider)
		if !ok || len(request.Inputs) != 1 {
			return nil, nil, videoCapabilityRejection()
		}
		name, _ := request.ProviderOptions["name"].(string)
		resource, err := characterProvider.CreateCharacter(ctx, resolved.account, VideoCharacterRequest{
			TaskID: task.PublicID, ClientToken: task.PublicID, Name: name,
			ProviderOptions: request.ProviderOptions,
		}, request.Inputs[0])
		return nil, resource, err
	default:
		return nil, nil, videoCapabilityRejection()
	}
}

// videoCapabilityRejection reports a capability mismatch that submitToProvider
// detects before it calls the provider. The outcome is certain — nothing was
// submitted — so it has to be classified as a rejection: a bare sentinel here
// would fall through handleSubmissionError's unknown branch and stamp the task
// as an unconfirmed submission, which is the one signal that means an upstream
// job may exist. The sentinel stays in the chain, and the code and status match
// ErrVideoCapabilityUnsupported, so the client-facing error is unchanged.
func videoCapabilityRejection() *VideoProviderError {
	rejection := rejectedVideoProviderError("validation", "video_capability_unsupported",
		"video capability is not supported", http.StatusBadRequest)
	rejection.Cause = ErrVideoCapabilityUnsupported
	return rejection
}

// handleSubmissionError keeps an unobserved upstream outcome in the submitting
// state so the worker can reconcile it by client token. Only a certain provider
// rejection is released immediately; every other error may have happened after
// the provider accepted the request and therefore remains held until the
// deterministic reconciliation window expires.
func (s *VideoTaskService) handleSubmissionError(ctx context.Context, task *VideoTask, providerErr error) (*VideoSubmitResult, error) {
	var typed *VideoProviderError
	event := "provider_rejected"
	if !errors.As(providerErr, &typed) || typed.Certainty != VideoSubmissionRejected {
		if typed == nil {
			typed = unknownVideoProviderError("transport", "submission_unknown", "video provider submission outcome is unknown", providerErr)
		}
		typed = sanitizedVideoProviderError(typed, "transport", "submission_unknown", "video provider submission outcome is unknown")
		event = "provider_submission_reconciling"
	} else {
		typed = sanitizedVideoProviderError(typed, "upstream", "submission_rejected", "video provider rejected the submission")
	}
	now := s.now().UTC()
	if event == "provider_submission_reconciling" {
		next := now.Add(videoSubmissionReconciliationPollInterval)
		updated, err := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
			GenerationState: VideoGenerationSubmitting, BillingState: VideoBillingHeld,
			NextActionAt: &next, ErrorKind: typed.Kind, ErrorCode: "submission_reconciling",
			ErrorMessage: "video provider submission outcome is being reconciled automatically",
			EventType:    "provider_submission_reconciling", SubmissionUnknown: true,
		})
		if err != nil {
			return nil, errors.Join(providerErr, err)
		}
		// The task remains held and is returned as an accepted asynchronous task.
		// The worker owns the automatic reconciliation and eventual settlement.
		return &VideoSubmitResult{Task: updated, Created: true}, nil
	}
	updated, err := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: VideoGenerationFailed, BillingState: VideoBillingReleasePending,
		NextActionAt: &now, ErrorKind: typed.Kind, ErrorCode: typed.Code,
		ErrorMessage: typed.Message, EventType: event,
	})
	if err != nil {
		return nil, errors.Join(providerErr, err)
	}
	settled, settleErr := s.releaseConfirmedVideoFailure(ctx, updated)
	if settleErr != nil {
		s.enqueueBestEffort(ctx, updated.PublicID)
		return &VideoSubmitResult{Task: settled, Created: true}, errors.Join(providerErr, settleErr)
	}
	return &VideoSubmitResult{Task: settled, Created: true}, providerErr
}

func videoSubmissionReconciliationWindow(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Gateway.Video.SubmissionReconciliationMinutes > 0 {
		return time.Duration(cfg.Gateway.Video.SubmissionReconciliationMinutes) * time.Minute
	}
	return videoDefaultSubmissionReconciliationWindow
}

func (s *VideoTaskService) reconcileSubmitting(ctx context.Context, task *VideoTask) (*VideoTask, error) {
	if s == nil || task == nil || task.AccountID == nil || task.ProviderTaskID != nil || s.accounts == nil || s.providers == nil {
		return task, ErrVideoInvalidRequest
	}
	now := s.now().UTC()
	startedAt := task.SubmissionUnknownAt
	if startedAt == nil {
		startedAt = &now
	}
	deadline := startedAt.Add(videoSubmissionReconciliationWindow(s.cfg))
	if !now.Before(deadline) {
		updated, err := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
			GenerationState: VideoGenerationFailed, BillingState: VideoBillingReleasePending,
			NextActionAt: &now, ErrorKind: "submission", ErrorCode: "submission_reconciliation_expired",
			ErrorMessage: "video provider submission could not be reconciled within the automatic reconciliation window",
			EventType:    "submission_reconciliation_expired",
		})
		if err != nil {
			return nil, err
		}
		return s.releaseConfirmedVideoFailure(ctx, updated)
	}

	account, err := s.accounts.GetByID(ctx, *task.AccountID)
	if err != nil {
		return task, err
	}
	provider, ok := s.providers.Get(task.Provider)
	if !ok {
		return task, ErrVideoProviderUnsupported
	}
	searcher, supported := provider.(VideoTaskSearcher)
	if supported && strings.TrimSpace(videoStringValue(task.StableClientToken)) != "" {
		// A lookup failure is deliberately swallowed: it is not proof that the
		// task was never created, so the only safe answer is to keep asking
		// until the deterministic reconciliation deadline above expires and
		// releases the hold in full.
		observed, searchErr := searcher.SearchByClientToken(ctx, account, videoStringValue(task.StableClientToken))
		if searchErr == nil && observed != nil && validVideoProviderIdentifier(observed.ProviderTaskID) {
			acceptance, acceptanceErr := s.videoProviderAcceptance(observed)
			if acceptanceErr != nil {
				return task, acceptanceErr
			}
			s.applyTerminalBillingToAcceptance(task, observed, &acceptance)
			updated, saveErr := s.tasks.SaveVideoProviderAccepted(videoTaskWriteContext(ctx, task), task.PublicID, acceptance)
			if saveErr != nil {
				return nil, saveErr
			}
			if updated.GenerationState == VideoGenerationFailed {
				return s.releaseConfirmedVideoFailure(ctx, updated)
			}
			return updated, nil
		}
	}

	next := now.Add(videoSubmissionReconciliationPollInterval)
	updated, err := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: VideoGenerationSubmitting, BillingState: VideoBillingHeld,
		NextActionAt: &next, ErrorKind: "submission", ErrorCode: "submission_reconciling",
		ErrorMessage: "video provider submission outcome is being reconciled automatically",
		EventType:    "submission_reconciliation_retry", SubmissionUnknown: true,
	})
	if err != nil {
		return nil, err
	}
	return updated, &videoTaskScheduledRetry{cause: errors.New("video submission reconciliation pending"), next: next}
}

func (s *VideoTaskService) finishTaskSubmission(ctx context.Context, task *VideoTask, resolved *resolvedVideoSubmission, providerTask *ProviderVideoTask) (*VideoSubmitResult, error) {
	if providerTask == nil || !validVideoProviderIdentifier(providerTask.ProviderTaskID) {
		return s.handleSubmissionError(ctx, task, unknownVideoProviderError("upstream", "missing_task", "video provider returned no task", nil))
	}
	acceptance, err := s.videoProviderAcceptance(providerTask)
	if err != nil {
		return s.handleSubmissionError(ctx, task, unknownVideoProviderError("upstream", "access_encryption_failed", "video task access could not be secured", err))
	}
	s.applyTerminalBillingToAcceptance(task, providerTask, &acceptance)
	updated, err := s.tasks.SaveVideoProviderAccepted(videoTaskWriteContext(ctx, task), task.PublicID, acceptance)
	if err != nil {
		return nil, err
	}
	if updated.GenerationState == VideoGenerationFailed {
		settled, settleErr := s.releaseConfirmedVideoFailure(ctx, updated)
		if settleErr != nil {
			s.enqueueBestEffort(ctx, updated.PublicID)
			return &VideoSubmitResult{Task: settled, Created: true}, nil
		}
		return &VideoSubmitResult{Task: settled, Created: true}, nil
	}
	s.enqueueBestEffort(ctx, updated.PublicID)
	return &VideoSubmitResult{Task: updated, Created: true}, nil
}

func (s *VideoTaskService) videoProviderAcceptance(providerTask *ProviderVideoTask) (VideoProviderAcceptance, error) {
	if providerTask == nil || !validVideoProviderIdentifier(providerTask.ProviderTaskID) {
		return VideoProviderAcceptance{}, ErrVideoInvalidRequest
	}
	accessKind, accessScope, accessEnc := "", "", ""
	var accessExpires *time.Time
	if providerTask.Access != nil {
		kind := strings.ToLower(strings.TrimSpace(providerTask.Access.Kind))
		scope := strings.ToLower(strings.TrimSpace(providerTask.Access.Scope))
		value := strings.TrimSpace(providerTask.Access.Value)
		if validVideoTaskAccess(kind, scope) && providerTask.Access.ExpiresAt != nil &&
			s.now().UTC().Before(*providerTask.Access.ExpiresAt) && value != "" && len(value) <= 16<<10 {
			if s.encryptor == nil {
				return VideoProviderAcceptance{}, errors.New("video task access encryptor is not configured")
			}
			var err error
			accessEnc, err = s.encryptor.Encrypt(value)
			if err != nil {
				return VideoProviderAcceptance{}, err
			}
			accessKind, accessScope, accessExpires = kind, scope, providerTask.Access.ExpiresAt
		}
	}
	providerVideoURLEnc, err := s.encryptProviderVideoURL(providerTask.VideoURL)
	if err != nil {
		return VideoProviderAcceptance{}, err
	}
	videoProxyKey, err := providerVideoProxyKey(providerTask.VideoURL)
	if err != nil {
		return VideoProviderAcceptance{}, err
	}
	next := s.now().UTC().Add(videoPollInterval(s.cfg, 0))
	errorKind, errorCode, errorMessage := "", boundedVideoProviderCode(providerTask.ErrorCode), boundedVideoProviderMessage(providerTask.ErrorMessage, "video provider task failed")
	if errorCode != "" || errorMessage != "" {
		errorKind = "upstream"
	}
	return VideoProviderAcceptance{
		ProviderTaskID: strings.TrimSpace(providerTask.ProviderTaskID), ProviderStatus: boundedVideoProviderStatus(providerTask.RawStatus),
		ProviderCreatedAt: providerTask.ProviderCreatedAt, ProviderFinishedAt: providerTask.ProviderFinishedAt,
		GenerationState: normalizedVideoProviderGenerationState(providerTask.Status, VideoGenerationInProgress),
		Progress:        boundedVideoProviderProgress(providerTask.Progress), UsageSnapshot: sanitizeVideoProviderUsage(providerTask.Usage),
		ResponseMetadata: sanitizeVideoProviderMetadata(providerTask.Metadata), ContentVariants: sanitizeVideoContentVariants(providerTask.ContentVariants),
		ContentExpiresAt: providerTask.ContentExpiresAt, ProviderAccessKind: accessKind,
		ProviderAccessScope: accessScope, ProviderAccessEnc: accessEnc,
		ProviderAccessExpiresAt: accessExpires, ProviderVideoURLEnc: providerVideoURLEnc,
		ProviderVideoProxyKey: videoProxyKey, NextActionAt: &next,
		ErrorKind: errorKind, ErrorCode: errorCode, ErrorMessage: errorMessage,
	}, nil
}

func (s *VideoTaskService) encryptProviderVideoURL(raw string) (string, error) {
	normalized, err := normalizeProviderVideoURL(raw)
	if err != nil || normalized == "" {
		return "", err
	}
	if s == nil || s.encryptor == nil {
		return "", errors.New("video URL encryptor is not configured")
	}
	return s.encryptor.Encrypt(normalized)
}

func (s *VideoTaskService) finishCharacterSubmission(ctx context.Context, task *VideoTask, name string, providerResource *ProviderVideoResource) (*VideoSubmitResult, error) {
	if task == nil || task.AccountID == nil || providerResource == nil {
		return nil, ErrVideoInvalidRequest
	}
	if !validVideoProviderIdentifier(providerResource.ProviderResourceID) {
		return s.handleSubmissionError(ctx, task, unknownVideoProviderError("upstream", "missing_resource", "video provider returned no character id", nil))
	}
	providerTask := &ProviderVideoTask{
		ProviderTaskID: providerResource.ProviderResourceID, Status: VideoGenerationCompleted,
		RawStatus: providerResource.Status, Metadata: providerResource.Metadata,
		ContentExpiresAt: providerResource.ExpiresAt, ProviderFinishedAt: timePointer(s.now().UTC()), Access: providerResource.Access,
	}
	acceptance, err := s.videoProviderAcceptance(providerTask)
	if err != nil {
		return s.handleSubmissionError(ctx, task, unknownVideoProviderError("upstream", "access_encryption_failed", "video character access could not be secured", err))
	}
	if strings.TrimSpace(name) == "" {
		name, _ = providerResource.Metadata["name"].(string)
	}
	// The character resource is persisted before the task is accepted, so a
	// completed character task can never settle without its local metadata.
	resource, err := s.resources.CreateVideoResource(ctx, VideoCreateResourceParams{
		Owner:    VideoOwner{UserID: task.UserID, APIKeyID: valueOrZero(task.APIKeyID), GroupID: task.GroupID},
		Provider: task.Provider, ChannelID: task.ChannelID, AccountID: *task.AccountID,
		SourceTaskID: &task.ID, ProviderResourceID: providerResource.ProviderResourceID,
		Model: task.UpstreamModel, Status: "ready", Metadata: map[string]any{"name": strings.TrimSpace(name)},
		ExpiresAt: providerResource.ExpiresAt,
	})
	if err != nil {
		now := s.now().UTC()
		failed, transitionErr := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
			GenerationState: VideoGenerationFailed, BillingState: VideoBillingReleasePending,
			NextActionAt: &now, ErrorKind: "persistence", ErrorCode: "resource_persistence_failed",
			ErrorMessage: "character metadata could not be persisted",
			EventType:    "resource_persistence_failed",
		})
		if transitionErr != nil {
			return nil, errors.Join(err, transitionErr)
		}
		if _, settleErr := s.releaseConfirmedVideoFailure(ctx, failed); settleErr != nil {
			s.enqueueBestEffort(ctx, failed.PublicID)
		}
		return nil, err
	}
	s.applyTerminalBillingToAcceptance(task, providerTask, &acceptance)
	updated, err := s.tasks.SaveVideoProviderAccepted(videoTaskWriteContext(ctx, task), task.PublicID, acceptance)
	if err != nil {
		return nil, err
	}
	s.enqueueBestEffort(ctx, updated.PublicID)
	return &VideoSubmitResult{Task: updated, Resource: resource, Created: true}, nil
}

type videoTerminalBillingDecision struct {
	state        string
	actualUnits  *float64
	actualCost   *float64
	errorKind    string
	errorCode    string
	errorMessage string
}

// videoFrozenQuoteBilling settles a completed task at the amount held when the
// task was accepted. It is the fallback whenever provider evidence cannot produce
// a trustworthy usage-based amount, for every billing unit: a delivered video is
// charged the pre-deducted hold rather than nothing. The hold is released instead
// when the frozen quote itself is unusable, so an unpriceable task is never charged.
func videoFrozenQuoteBilling(task *VideoTask, kind, code, message string) videoTerminalBillingDecision {
	decision := videoTerminalBillingDecision{errorKind: kind, errorCode: code, errorMessage: message}
	if task == nil || task.EstimatedUnits == nil || task.HoldAmount == nil ||
		!finiteNonNegative(*task.EstimatedUnits) || !finiteNonNegative(*task.HoldAmount) {
		zero := 0.0
		decision.state, decision.actualUnits, decision.actualCost = VideoBillingReleasePending, &zero, &zero
		return decision
	}
	units, cost := *task.EstimatedUnits, *task.HoldAmount
	decision.state, decision.actualUnits, decision.actualCost = VideoBillingCapturePending, &units, &cost
	return decision
}

func videoTerminalBillingFor(task *VideoTask, target string, providerTask *ProviderVideoTask) videoTerminalBillingDecision {
	if task == nil || !IsVideoGenerationTerminal(target) {
		return videoTerminalBillingDecision{}
	}
	if target != VideoGenerationCompleted {
		// Nothing was delivered, so the hold is released in full and the task
		// records an explicit zero rather than an absent charge.
		zero := 0.0
		return videoTerminalBillingDecision{
			state: VideoBillingReleasePending, actualUnits: &zero, actualCost: &zero,
		}
	}
	metadata := task.ResponseMetadata
	if providerTask != nil {
		metadata = videoObservedMetadata(task, providerTask.Metadata)
	}
	if err := videoCheckObservedSpecification(task, metadata); err != nil {
		// 上游确实交付了视频，只是产出与创建时冻结的执行规格不一致。与 usage_missing
		// 一样按冻结报价结算（预扣多少收多少），而不是把预扣全额退回让这次生成免费；
		// 错误种类/码/文案照旧写进任务与用量日志，运维仍能据此定位这类任务。
		// 冻结报价本身不可用（无 HoldAmount/EstimatedUnits）时 videoFrozenQuoteBilling 仍会释放。
		return videoFrozenQuoteBilling(task, "specification", "execution_spec_conflict",
			"provider output conflicts with the frozen execution specification: "+err.Error())
	}
	candidate := *task
	candidate.GenerationState = target
	units, cost, err := videoActualCost(&candidate, providerTask)
	if err != nil {
		return videoFrozenQuoteBilling(task, "billing", "usage_missing", err.Error())
	}
	return videoTerminalBillingDecision{
		state: VideoBillingCapturePending, actualUnits: &units, actualCost: &cost,
	}
}

func (s *VideoTaskService) applyTerminalBillingToAcceptance(task *VideoTask, providerTask *ProviderVideoTask, acceptance *VideoProviderAcceptance) {
	if s == nil || acceptance == nil {
		return
	}
	acceptance.ResponseMetadata = videoObservedMetadata(task, acceptance.ResponseMetadata)
	if !IsVideoGenerationTerminal(acceptance.GenerationState) {
		return
	}
	decision := videoTerminalBillingFor(task, acceptance.GenerationState, providerTask)
	acceptance.BillingState = decision.state
	acceptance.ActualUnits = decision.actualUnits
	acceptance.ActualCost = decision.actualCost
	if decision.errorKind != "" || decision.errorCode != "" || decision.errorMessage != "" {
		acceptance.ErrorKind = decision.errorKind
		acceptance.ErrorCode = decision.errorCode
		acceptance.ErrorMessage = decision.errorMessage
	}
	now := s.now().UTC()
	acceptance.NextActionAt = &now
}

// prepareTerminalBilling repairs tasks written by older binaries that reached a
// provider terminal state before a billing intent was persisted.
func (s *VideoTaskService) prepareTerminalBilling(ctx context.Context, task *VideoTask, providerTask *ProviderVideoTask) (*VideoTask, error) {
	if task == nil || !IsVideoGenerationTerminal(task.GenerationState) || task.BillingState != VideoBillingHeld {
		return task, nil
	}
	decision := videoTerminalBillingFor(task, task.GenerationState, providerTask)
	if task.GenerationState == VideoGenerationFailed && decision.errorCode == "" {
		decision.errorKind = videoStringValue(task.LastErrorKind)
		decision.errorCode = videoStringValue(task.LastErrorCode)
		decision.errorMessage = videoStringValue(task.LastErrorMessage)
	}
	now := s.now().UTC()
	return s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: task.GenerationState, BillingState: decision.state,
		ActualUnits: decision.actualUnits, ActualCost: decision.actualCost, NextActionAt: &now,
		ErrorKind: decision.errorKind, ErrorCode: decision.errorCode, ErrorMessage: decision.errorMessage,
		EventType: "terminal_billing_recovered",
	})
}

// ReconcileProviderObservation is the single monotonic state path used by both
// polling and provider webhooks.
func (s *VideoTaskService) ReconcileProviderObservation(ctx context.Context, task *VideoTask, observed *ProviderVideoTask, eventType string) (*VideoTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if task == nil || observed == nil || strings.TrimSpace(task.PublicID) == "" {
		return nil, ErrVideoInvalidRequest
	}
	target := normalizedVideoProviderGenerationState(observed.Status, task.GenerationState)
	rawStatus := boundedVideoProviderStatus(observed.RawStatus)
	if !CanTransitionVideoGeneration(task.GenerationState, target) {
		_, _ = s.tasks.AppendVideoTaskEvent(ctx, VideoTaskEvent{
			TaskID: &task.ID, EventType: "provider_observation_ignored", Provider: task.Provider,
			AccountID: task.AccountID, ProviderTaskID: videoStringValue(task.ProviderTaskID),
			FromGenerationState: task.GenerationState, ToGenerationState: target,
			Payload:   map[string]any{"raw_status": rawStatus, "reason": "non_monotonic"},
			EventHash: transitionEventHashForService(task.ID, task.Version, target, rawStatus),
		})
		return task, nil
	}
	next := s.now().UTC().Add(videoPollInterval(s.cfg, 0))
	var nextPointer *time.Time
	if !IsVideoGenerationTerminal(target) {
		nextPointer = &next
	}
	errorKind := ""
	errorCode := boundedVideoProviderCode(observed.ErrorCode)
	errorMessage := boundedVideoProviderMessage(observed.ErrorMessage, "video provider task failed")
	if errorCode != "" || errorMessage != "" {
		errorKind = "upstream"
	}
	providerVideoURLEnc, err := s.encryptProviderVideoURL(observed.VideoURL)
	if err != nil {
		return nil, err
	}
	videoProxyKey, err := providerVideoProxyKey(observed.VideoURL)
	if err != nil {
		return nil, err
	}
	transition := VideoTaskTransition{
		GenerationState: target, ProviderStatus: rawStatus, Progress: boundedVideoProviderProgress(observed.Progress),
		UsageSnapshot: sanitizeVideoProviderUsage(observed.Usage), ResponseMetadata: videoObservedMetadata(task, observed.Metadata),
		ContentVariants: sanitizeVideoContentVariants(observed.ContentVariants), ContentExpiresAt: observed.ContentExpiresAt,
		ProviderVideoURLEnc:   providerVideoURLEnc,
		ProviderVideoProxyKey: videoProxyKey,
		ProviderFinishedAt:    observed.ProviderFinishedAt,
		NextActionAt:          nextPointer, ErrorKind: errorKind, ErrorCode: errorCode,
		ErrorMessage: errorMessage, EventType: eventType,
		EventPayload:          map[string]any{"raw_status": rawStatus},
		IncrementPollAttempts: eventType == "provider_polled",
	}
	if IsVideoGenerationTerminal(target) {
		decision := videoTerminalBillingFor(task, target, observed)
		transition.BillingState = decision.state
		transition.ActualUnits = decision.actualUnits
		transition.ActualCost = decision.actualCost
		if decision.errorKind != "" || decision.errorCode != "" || decision.errorMessage != "" {
			transition.ErrorKind = decision.errorKind
			transition.ErrorCode = decision.errorCode
			transition.ErrorMessage = decision.errorMessage
		}
		now := s.now().UTC()
		transition.NextActionAt = &now
	}
	updated, err := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, transition)
	if err != nil {
		if errors.Is(err, ErrVideoInvalidTransition) || errors.Is(err, ErrVideoVersionConflict) {
			latest, lookupErr := s.tasks.GetVideoTaskByPublicID(ctx, task.PublicID)
			if lookupErr == nil && videoObservationSuperseded(latest, target) {
				_, _ = s.tasks.AppendVideoTaskEvent(ctx, VideoTaskEvent{
					TaskID: &latest.ID, EventType: "provider_observation_ignored", Provider: latest.Provider,
					AccountID: latest.AccountID, ProviderTaskID: videoStringValue(latest.ProviderTaskID),
					FromGenerationState: latest.GenerationState, ToGenerationState: target,
					Payload:   map[string]any{"raw_status": rawStatus, "reason": "concurrent_state_advanced"},
					EventHash: transitionEventHashForService(latest.ID, latest.Version, target, rawStatus, "concurrent"),
				})
				return latest, nil
			}
		}
		return nil, err
	}
	observability.DefaultVideoMetrics().RecordState(task.Provider, target)
	if target == VideoGenerationFailed && s.settlements != nil {
		settled, settleErr := s.releaseConfirmedVideoFailure(ctx, updated)
		if settleErr != nil {
			s.enqueueBestEffort(context.WithoutCancel(ctx), updated.PublicID)
			return settled, settleErr
		}
		updated = settled
	}
	return updated, nil
}

func videoObservationSuperseded(latest *VideoTask, target string) bool {
	if latest == nil {
		return false
	}
	if !CanTransitionVideoGeneration(latest.GenerationState, target) {
		return true
	}
	return latest.GenerationState == target && IsVideoBillingTerminal(latest.BillingState)
}

// RefreshProviderTask performs a read-only upstream Get followed by the shared
// monotonic reconciliation path. It never calls Create and is therefore safe
// for explicit administrative recovery.
func (s *VideoTaskService) RefreshProviderTask(ctx context.Context, task *VideoTask) (*VideoTask, error) {
	if s == nil || task == nil || task.AccountID == nil || task.ProviderTaskID == nil ||
		s.accounts == nil || s.providers == nil {
		return nil, ErrVideoInvalidRequest
	}
	if task.GenerationState != VideoGenerationQueued && task.GenerationState != VideoGenerationInProgress {
		return nil, ErrVideoInvalidTransition
	}
	account, err := s.accounts.GetByID(ctx, *task.AccountID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(task.Provider)
	if !ok {
		return nil, ErrVideoProviderUnsupported
	}
	observed, err := provider.Get(ctx, account, ProviderTaskRef{
		Provider: task.Provider, AccountID: *task.AccountID, ProviderTaskID: *task.ProviderTaskID,
	})
	if err != nil {
		observability.DefaultVideoMetrics().RecordProviderGet(task.Provider, "admin", "error")
		return nil, err
	}
	observability.DefaultVideoMetrics().RecordProviderGet(task.Provider, "admin", "success")
	return s.ReconcileProviderObservation(ctx, task, observed, "admin_provider_get")
}

func transitionEventHashForService(taskID, version int64, values ...string) string {
	payload := map[string]any{"task_id": taskID, "version": version, "values": values}
	hash, _ := HashVideoRequest(payload)
	return hash
}

func (s *VideoTaskService) markPreSubmitFailure(ctx context.Context, task *VideoTask, cause error) {
	if task == nil {
		return
	}
	now := s.now().UTC()
	updated, err := s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: VideoGenerationFailed, BillingState: VideoBillingReleasePending,
		NextActionAt: &now, ErrorKind: "internal", ErrorCode: "pre_submit_failed",
		ErrorMessage: boundedProviderMessage(cause.Error(), ""), EventType: "pre_submit_failed",
	})
	if err == nil {
		if _, releaseErr := s.releaseConfirmedVideoFailure(ctx, updated); releaseErr == nil {
			return
		}
	}
	s.enqueueBestEffort(ctx, task.PublicID)
}

func (s *VideoTaskService) releaseConfirmedVideoFailure(ctx context.Context, task *VideoTask) (*VideoTask, error) {
	if task == nil || task.GenerationState != VideoGenerationFailed || task.BillingState != VideoBillingReleasePending {
		return task, nil
	}
	if s == nil || s.settlements == nil || s.tasks == nil || task.APIKeyID == nil || task.HoldAmount == nil {
		return task, errors.New("video failure hold release is not configured")
	}
	settlement := &BalanceSettlementCommand{
		TaskID: task.ID, Action: BalanceSettlementRelease,
		Hold: BalanceHoldCommand{
			RequestID: VideoTaskReleaseRequestID(task.PublicID), APIKeyID: *task.APIKeyID,
			RequestPayloadHash: task.RequestHash, UserID: task.UserID,
			Scope: BalanceHoldScopeVideoTask, RefID: task.PublicID,
			HoldAmount: *task.HoldAmount, ActualAmount: 0,
		},
	}
	result, err := s.settlements.SettleVideoBalance(videoTaskWriteContext(ctx, task), settlement, nil)
	if err != nil {
		return task, err
	}
	latest, lookupErr := s.tasks.GetVideoTaskByPublicID(context.WithoutCancel(ctx), task.PublicID)
	if lookupErr != nil {
		latest = task
	}
	if result == nil || result.OutboxReceipt == nil {
		return latest, errors.Join(lookupErr, errors.New("video failure hold release receipt is missing"))
	}
	if s.admission != nil {
		if err := s.admission.InvalidateVideoHold(context.WithoutCancel(ctx), task.UserID); err != nil {
			return latest, errors.Join(lookupErr, err)
		}
	}
	if err := s.settlements.AcknowledgeVideoBalanceSettlement(
		context.WithoutCancel(ctx), result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID,
	); err != nil {
		return latest, errors.Join(lookupErr, err)
	}
	return latest, lookupErr
}

func (s *VideoTaskService) enqueueBestEffort(ctx context.Context, publicID string) {
	if s.queue != nil {
		_, _ = s.queue.Enqueue(ctx, publicID)
	}
}

func (s *VideoTaskService) GetForOwner(ctx context.Context, userID int64, publicID string) (*VideoTask, error) {
	task, err := s.tasks.GetVideoTaskForOwner(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	if task.GenerationState == VideoGenerationCompleted && task.BillingState != VideoBillingCaptured {
		if task.BillingState == VideoBillingCapturePending || task.BillingState == VideoBillingReleasePending {
			s.enqueueBestEffort(ctx, task.PublicID)
		}
		return nil, ErrVideoSettlementPending
	}
	if task.GenerationState == VideoGenerationFailed && task.BillingState == VideoBillingReleasePending {
		s.enqueueBestEffort(context.WithoutCancel(ctx), task.PublicID)
	}
	if (task.GenerationState == VideoGenerationQueued || task.GenerationState == VideoGenerationInProgress) &&
		(task.NextActionAt == nil || !task.NextActionAt.After(s.now().UTC())) {
		s.enqueueBestEffort(context.WithoutCancel(ctx), task.PublicID)
	}
	return task, nil
}

func (s *VideoTaskService) ListForOwner(ctx context.Context, userID int64, filter VideoTaskFilter) (*VideoTaskPage, error) {
	return s.tasks.ListVideoTasksForOwner(ctx, userID, filter)
}

func (s *VideoTaskService) GetCharacterForOwner(ctx context.Context, userID int64, publicID string) (*VideoResource, error) {
	if s == nil || s.resources == nil {
		return nil, ErrVideoResourceNotFound
	}
	resource, err := s.resources.GetVideoResourceForOwner(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	if err := s.requireSettledCharacter(ctx, userID, resource); err != nil {
		return nil, err
	}
	return resource, nil
}

func (s *VideoTaskService) ResourceDisclosureForOwner(ctx context.Context, userID int64, publicID string) (*VideoResourceDisclosure, error) {
	if s == nil || s.resources == nil || s.cfg == nil {
		return nil, ErrVideoResourceNotFound
	}
	resource, err := s.GetCharacterForOwner(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	policy, _ := s.videoDisclosurePolicyForScope(ctx, resource.GroupID, &resource.AccountID)
	disclosure := &VideoResourceDisclosure{Policy: policy}
	if policy != config.VideoDisclosureNone {
		disclosure.Provider = resource.Provider
		disclosure.ProviderResourceID = resource.ProviderResourceID
		if err := s.auditVideoResourceDisclosure(ctx, resource, policy); err != nil {
			return nil, err
		}
	}
	return disclosure, nil
}

func (s *VideoTaskService) DisclosureForOwner(ctx context.Context, userID int64, publicID string) (*VideoTaskDisclosure, error) {
	if s == nil || s.tasks == nil || s.cfg == nil {
		return nil, ErrVideoTaskNotFound
	}
	task, err := s.tasks.GetVideoTaskForOwner(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	if task.Operation == VideoOperationCharacterCreate &&
		(task.GenerationState != VideoGenerationCompleted || task.BillingState != VideoBillingCaptured ||
			task.DeleteState != VideoDeleteNone || task.DeletedAt != nil) {
		return &VideoTaskDisclosure{Policy: config.VideoDisclosureNone}, nil
	}
	policy, account := s.videoDisclosurePolicy(ctx, task)
	disclosure := &VideoTaskDisclosure{Policy: policy}
	if policy == config.VideoDisclosureNone {
		return disclosure, nil
	}
	disclosure.Provider = task.Provider
	if task.ProviderTaskID != nil {
		disclosure.ProviderTaskID = strings.TrimSpace(*task.ProviderTaskID)
	}
	if policy == config.VideoDisclosureDedicatedCredentials {
		// Verification can predate a conflicting credential alias. Recheck the
		// database authorization before disclosing a provider-wide credential.
		authorizer, ok := s.accounts.(AccountSchedulingAuthorizationRepository)
		allowed := false
		if ok && account != nil {
			var err error
			allowed, err = authorizer.CanScheduleAccountForUser(ctx, account.ID, userID)
			allowed = err == nil && allowed
		}
		if access := dedicatedVideoCredentialForOwner(task, account, userID); allowed && access != nil {
			disclosure.Access = access
			if err := s.auditVideoAccessDisclosure(ctx, task, policy, disclosure.ProviderTaskID, access); err != nil {
				return nil, err
			}
			return disclosure, nil
		}
	}
	if videoDisclosureRank(policy) < videoDisclosureRank(config.VideoDisclosureTaskAccess) ||
		task.ProviderAccessEnc == nil || strings.TrimSpace(*task.ProviderAccessEnc) == "" {
		if err := s.auditVideoAccessDisclosure(ctx, task, policy, disclosure.ProviderTaskID, nil); err != nil {
			return nil, err
		}
		return disclosure, nil
	}
	if task.ProviderAccessExpires == nil || !s.now().UTC().Before(*task.ProviderAccessExpires) {
		if err := s.auditVideoAccessDisclosure(ctx, task, policy, disclosure.ProviderTaskID, nil); err != nil {
			return nil, err
		}
		return disclosure, nil
	}
	if s.encryptor == nil || task.ProviderAccessKind == nil || task.ProviderAccessScope == nil {
		return nil, errors.New("video task access decryptor is not configured")
	}
	kind := strings.ToLower(strings.TrimSpace(*task.ProviderAccessKind))
	scope := strings.ToLower(strings.TrimSpace(*task.ProviderAccessScope))
	if !validVideoTaskAccess(kind, scope) {
		return nil, errors.New("video task access scope is not safe to disclose")
	}
	value, err := s.encryptor.Decrypt(*task.ProviderAccessEnc)
	if err != nil {
		return nil, errors.New("video task access could not be decrypted")
	}
	disclosure.Access = &ProviderTaskAccess{
		Kind:      kind,
		Value:     value,
		Scope:     scope,
		ExpiresAt: task.ProviderAccessExpires,
	}
	if err := s.auditVideoAccessDisclosure(ctx, task, policy, disclosure.ProviderTaskID, disclosure.Access); err != nil {
		return nil, err
	}
	return disclosure, nil
}

func (s *VideoTaskService) videoDisclosurePolicy(ctx context.Context, task *VideoTask) (string, *Account) {
	if task == nil {
		return config.VideoDisclosureNone, nil
	}
	return s.videoDisclosurePolicyForScope(ctx, task.GroupID, task.AccountID)
}

func (s *VideoTaskService) videoDisclosurePolicyForScope(ctx context.Context, groupID, accountID *int64) (string, *Account) {
	globalPolicy := strings.TrimSpace(s.cfg.Gateway.Video.DisclosurePolicy)
	groupPolicy := ""
	if groupID != nil {
		if s.groups == nil {
			return config.VideoDisclosureNone, nil
		}
		group, err := s.groups.GetByID(ctx, *groupID)
		if err != nil || group == nil {
			return config.VideoDisclosureNone, nil
		}
		groupPolicy = group.VideoDisclosurePolicy
	}
	accountPolicy := ""
	var account *Account
	if accountID != nil {
		if s.accounts == nil {
			return config.VideoDisclosureNone, nil
		}
		loaded, err := s.accounts.GetByID(ctx, *accountID)
		if err != nil || loaded == nil {
			return config.VideoDisclosureNone, nil
		}
		account = loaded
		accountPolicy = account.VideoDisclosurePolicy
	}
	return effectiveVideoDisclosurePolicy(globalPolicy, groupPolicy, accountPolicy), account
}

func dedicatedVideoCredentialForOwner(task *VideoTask, account *Account, userID int64) *ProviderTaskAccess {
	if task == nil || account == nil || task.AccountID == nil || *task.AccountID != account.ID ||
		task.UserID != userID || userID <= 0 ||
		task.AccountOwnerUserID == nil || *task.AccountOwnerUserID != userID ||
		account.VideoOwnerUserID == nil || *account.VideoOwnerUserID != userID ||
		!account.hasVerifiedDedicatedIsolation() || *account.OwnerUserID != userID ||
		account.VideoDisclosurePolicy != config.VideoDisclosureDedicatedCredentials ||
		account.Type != AccountTypeAPIKey {
		return nil
	}
	encodedVersion, err := json.Marshal(task.RequestAttributes["account_identity_version"])
	if err != nil {
		return nil
	}
	version, err := strconv.ParseInt(string(encodedVersion), 10, 64)
	if err != nil || version != account.ProviderIdentityVersion {
		return nil
	}
	value := strings.TrimSpace(account.GetCredential("api_key"))
	if value == "" {
		return nil
	}
	return &ProviderTaskAccess{Kind: "api_key", Value: value, Scope: "provider_account"}
}

func (s *VideoTaskService) auditVideoAccessDisclosure(ctx context.Context, task *VideoTask, policy, providerTaskID string, access *ProviderTaskAccess) error {
	if task == nil {
		return nil
	}
	kind, scope, eventType := "identity", "provider_task", "provider_identity_disclosed"
	if access != nil {
		kind = strings.ToLower(strings.TrimSpace(access.Kind))
		scope = strings.ToLower(strings.TrimSpace(access.Scope))
		eventType = "provider_access_disclosed"
	}
	eventHash, _ := HashVideoRequest(map[string]any{
		"task_id": task.ID, "user_id": task.UserID, "api_key_id": task.APIKeyID,
		"policy": policy, "kind": kind, "scope": scope,
		"disclosed_at": s.now().UTC().UnixNano(),
	})
	if _, err := s.tasks.AppendVideoTaskEvent(ctx, VideoTaskEvent{
		TaskID: &task.ID, EventType: eventType, Provider: task.Provider,
		AccountID: task.AccountID, ProviderTaskID: providerTaskID,
		Payload: map[string]any{
			"user_id": task.UserID, "api_key_id": task.APIKeyID,
			"policy": policy, "kind": kind, "scope": scope,
		}, EventHash: eventHash,
	}); err != nil {
		return err
	}
	observability.DefaultVideoMetrics().RecordAccessDisclosure(kind, policy)
	return nil
}

func (s *VideoTaskService) auditVideoResourceDisclosure(ctx context.Context, resource *VideoResource, policy string) error {
	if resource == nil || s.tasks == nil {
		return nil
	}
	eventHash, _ := HashVideoRequest(map[string]any{
		"resource_id": resource.ID, "user_id": resource.UserID, "api_key_id": resource.APIKeyID,
		"policy": policy, "disclosed_at": s.now().UTC().UnixNano(),
	})
	if _, err := s.tasks.AppendVideoTaskEvent(ctx, VideoTaskEvent{
		TaskID: resource.SourceTaskID, EventType: "provider_resource_identity_disclosed",
		Provider: resource.Provider, AccountID: &resource.AccountID,
		Payload: map[string]any{
			"user_id": resource.UserID, "api_key_id": resource.APIKeyID,
			"policy": policy, "kind": "identity", "scope": "provider_resource",
			"resource_id": resource.PublicID,
		}, EventHash: eventHash,
	}); err != nil {
		return err
	}
	observability.DefaultVideoMetrics().RecordAccessDisclosure("identity", policy)
	return nil
}

func validVideoTaskAccess(kind, scope string) bool {
	switch kind {
	case "signed_url", "token":
	default:
		return false
	}
	switch scope {
	case "video_content", "video_task", "content":
		return true
	default:
		return false
	}
}

func (s *VideoTaskService) OpenContentForOwner(ctx context.Context, userID int64, publicID string, request ProviderContentRequest) (*ProviderContent, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.Video.Enabled || !s.cfg.Gateway.Video.ContentProxy.Enabled {
		return nil, ErrVideoDisabled
	}
	task, err := s.GetContentTaskForOwner(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	return s.OpenContentForTask(ctx, task, request)
}

func (s *VideoTaskService) OpenContentForTask(ctx context.Context, task *VideoTask, request ProviderContentRequest) (*ProviderContent, error) {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.Video.Enabled || !s.cfg.Gateway.Video.ContentProxy.Enabled {
		return nil, ErrVideoDisabled
	}
	if task == nil {
		return nil, ErrVideoTaskNotFound
	}
	if task.GenerationState != VideoGenerationCompleted || task.BillingState != VideoBillingCaptured {
		if task.GenerationState == VideoGenerationCompleted {
			return nil, ErrVideoSettlementPending
		}
		return nil, ErrVideoContentNotReady
	}
	if task.DeleteState == VideoDeleteDeleted || task.DeletedAt != nil {
		return nil, ErrVideoTaskNotFound
	}
	if task.ContentExpiresAt != nil && !s.now().UTC().Before(*task.ContentExpiresAt) {
		return nil, ErrVideoContentExpired
	}
	if task.AccountID == nil || task.ProviderTaskID == nil || s.accounts == nil {
		return nil, ErrVideoContentNotReady
	}
	account, err := s.accounts.GetByID(ctx, *task.AccountID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(task.Provider)
	if !ok {
		return nil, ErrVideoProviderUnsupported
	}
	request.TaskRef = ProviderTaskRef{Provider: task.Provider, AccountID: *task.AccountID, ProviderTaskID: *task.ProviderTaskID}
	request.UpstreamURL, err = s.decryptProviderVideoURL(task)
	if err != nil {
		return nil, err
	}
	content, err := provider.OpenContent(ctx, account, request)
	if err != nil {
		return nil, videoContentOpenError(err)
	}
	if content == nil || content.Body == nil {
		return nil, unknownVideoProviderError("upstream", "invalid_content_response", "video provider returned no content body", nil)
	}
	if (content.StatusCode < http.StatusOK || content.StatusCode >= http.StatusMultipleChoices) && content.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		_ = content.Body.Close()
		return nil, &VideoProviderError{
			Kind: "upstream", Code: "invalid_content_status", Message: "video provider returned an invalid content status",
			Retryable: content.StatusCode >= http.StatusInternalServerError, Certainty: VideoSubmissionAccepted,
		}
	}
	return content, nil
}

// videoContentOpenError 把"签名链接已经失效"翻译成一个稳定的错误码。
//
// 取回内容时我们不带任何上游凭据——授权整个写在 URL 的签名里（bytedance_video_provider.go
// 的 OpenContent 注释说得很明白）。因此对象存储回 401/403 只有一种解释：那条链接过期或被
// 撤销了，而不是"这个用户没有权限"。原样透出去，用户看到的是上游那句英文
// "The request signature expired"；换成 video_content_expired，前端才有一个可翻译、
// 可判断的码，而不是去猜一段上游文案。
func videoContentOpenError(err error) error {
	var providerErr *VideoProviderError
	if errors.As(err, &providerErr) &&
		(providerErr.StatusCode == http.StatusUnauthorized || providerErr.StatusCode == http.StatusForbidden) {
		return ErrVideoContentExpired
	}
	return err
}

func (s *VideoTaskService) GetContentTaskForOwner(ctx context.Context, userID int64, reference string) (*VideoTask, error) {
	if s == nil || s.tasks == nil {
		return nil, ErrVideoTaskNotFound
	}
	reference = strings.TrimSpace(reference)
	task, err := s.tasks.GetVideoTaskForOwner(ctx, userID, reference)
	if err == nil || !errors.Is(err, ErrVideoTaskNotFound) {
		return task, err
	}
	return s.tasks.GetVideoTaskByProviderIDForOwner(ctx, userID, reference)
}

func (s *VideoTaskService) GetContentTaskByURLForOwner(ctx context.Context, userID int64, requestURI string) (*VideoTask, error) {
	if s == nil || s.tasks == nil {
		return nil, ErrVideoTaskNotFound
	}
	key, err := videoRequestURIProxyKey(requestURI)
	if err != nil {
		return nil, ErrVideoTaskNotFound
	}
	lookup, ok := s.tasks.(VideoTaskProxyLookupRepository)
	if !ok {
		return nil, ErrVideoTaskNotFound
	}
	return lookup.GetVideoTaskByProxyKeyForOwner(ctx, userID, key)
}

// GetContentTaskByPublicID 按 public_id 加载任务且不校验属主，仅供签名内容链接使用：
// 链接上的 HMAC 签名已经证明持有者是从属主的任务详情里拿到它的，签名校验由 handler 完成。
func (s *VideoTaskService) GetContentTaskByPublicID(ctx context.Context, publicID string) (*VideoTask, error) {
	if s == nil || s.tasks == nil {
		return nil, ErrVideoTaskNotFound
	}
	publicID = strings.TrimSpace(publicID)
	if !IsValidVideoTaskID(publicID) {
		return nil, ErrVideoTaskNotFound
	}
	return s.tasks.GetVideoTaskByPublicID(ctx, publicID)
}

func (s *VideoTaskService) VideoURLForOwner(ctx context.Context, userID int64, publicID string) (string, error) {
	if s == nil || s.tasks == nil {
		return "", ErrVideoTaskNotFound
	}
	task, err := s.tasks.GetVideoTaskForOwner(ctx, userID, strings.TrimSpace(publicID))
	if err != nil {
		return "", err
	}
	return s.decryptProviderVideoURL(task)
}

func (s *VideoTaskService) decryptProviderVideoURL(task *VideoTask) (string, error) {
	if task == nil || task.ProviderVideoURLEnc == nil || strings.TrimSpace(*task.ProviderVideoURLEnc) == "" {
		return "", nil
	}
	if s == nil || s.encryptor == nil {
		return "", errors.New("video URL decryptor is not configured")
	}
	decrypted, err := s.encryptor.Decrypt(*task.ProviderVideoURLEnc)
	if err != nil {
		return "", errors.New("video URL could not be decrypted")
	}
	return normalizeProviderVideoURL(decrypted)
}

func providerVideoProxyKey(raw string) (string, error) {
	normalized, err := normalizeProviderVideoURL(raw)
	if err != nil || normalized == "" {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	return hashVideoProxyRequestTarget(parsed.RequestURI())
}

func videoRequestURIProxyKey(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return "", ErrVideoInvalidRequest
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "", ErrVideoInvalidRequest
	}
	return hashVideoProxyRequestTarget(parsed.RequestURI())
}

func hashVideoProxyRequestTarget(target string) (string, error) {
	if target == "" {
		target = "/"
	}
	return HashVideoRequest(map[string]any{"provider_video_request_target": target})
}

func (s *VideoTaskService) DeleteForOwner(ctx context.Context, userID int64, publicID string) (*VideoTask, error) {
	task, err := s.tasks.GetVideoTaskForOwner(ctx, userID, publicID)
	if err != nil {
		return nil, err
	}
	if task.GenerationState == VideoGenerationSubmitting {
		return nil, ErrVideoDeleteConflict
	}
	if task.DeleteState == VideoDeleteDeleted {
		return task, nil
	}
	if task.GenerationState == VideoGenerationQueued || task.GenerationState == VideoGenerationInProgress {
		return nil, ErrVideoDeleteConflict
	}
	if task.GenerationState == VideoGenerationHeld && task.BillingState == VideoBillingHeld {
		if task.ProviderTaskID != nil || task.SubmittedAt != nil || task.SubmitAttempts != 0 {
			return nil, ErrVideoDeleteConflict
		}
		now := s.now().UTC()
		task, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
			GenerationState: VideoGenerationCancelled, BillingState: VideoBillingReleasePending,
			DeleteState: VideoDeleteDeleted, NextActionAt: &now, EventType: "delete_pre_submit",
		})
		if errors.Is(err, ErrVideoInvalidTransition) {
			return nil, ErrVideoDeleteConflict
		}
		if err == nil {
			s.enqueueBestEffort(ctx, task.PublicID)
		}
		return task, err
	}
	if IsVideoGenerationTerminal(task.GenerationState) && task.BillingState == VideoBillingHeld {
		task, err = s.prepareTerminalBilling(ctx, task, &ProviderVideoTask{
			ProviderTaskID: videoStringValue(task.ProviderTaskID), Status: task.GenerationState,
			Usage: task.UsageSnapshot, Metadata: task.ResponseMetadata,
		})
		if err != nil {
			return nil, err
		}
		if task.GenerationState == VideoGenerationFailed && task.BillingState == VideoBillingReleasePending && s.settlements != nil {
			settled, releaseErr := s.releaseConfirmedVideoFailure(ctx, task)
			task = settled
			if releaseErr != nil {
				s.enqueueBestEffort(context.WithoutCancel(ctx), task.PublicID)
				return nil, ErrVideoSettlementPending
			}
		}
	}
	if IsVideoGenerationTerminal(task.GenerationState) && !IsVideoBillingTerminal(task.BillingState) {
		if task.BillingState == VideoBillingCapturePending || task.BillingState == VideoBillingReleasePending {
			s.enqueueBestEffort(ctx, task.PublicID)
		}
		return nil, ErrVideoSettlementPending
	}
	if task.DeleteState == VideoDeleteRequested || task.DeleteState == VideoDeleteDeleting || task.DeleteState == VideoDeleteFailed {
		return task, nil
	}
	task, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		DeleteState: VideoDeleteRequested, NextActionAt: timePointer(s.now().UTC()), EventType: "delete_requested",
	})
	if err != nil {
		return nil, err
	}
	s.enqueueBestEffort(ctx, task.PublicID)
	return task, nil
}

func (s *VideoTaskService) RetryDeleteTask(ctx context.Context, task *VideoTask) (*VideoTask, error) {
	if s == nil || s.tasks == nil || task == nil {
		return nil, ErrVideoInvalidRequest
	}
	if task.DeleteState == VideoDeleteDeleted {
		return task, nil
	}
	lease, owned := VideoTaskLeaseFromContext(ctx)
	if !owned || lease.TaskID != task.ID || lease.Epoch <= 0 {
		return nil, ErrVideoLeaseLost
	}
	if !IsVideoGenerationTerminal(task.GenerationState) {
		return nil, ErrVideoDeleteConflict
	}
	if IsVideoGenerationTerminal(task.GenerationState) && !IsVideoBillingTerminal(task.BillingState) {
		return nil, ErrVideoSettlementPending
	}
	var err error
	if task.ProviderTaskID == nil || task.AccountID == nil {
		billing := task.BillingState
		if billing == VideoBillingHeld {
			billing = VideoBillingReleasePending
		}
		var next *time.Time
		if billing == VideoBillingCapturePending || billing == VideoBillingReleasePending {
			next = timePointer(s.now().UTC())
		}
		generation := task.GenerationState
		if !IsVideoGenerationTerminal(generation) {
			generation = VideoGenerationCancelled
		}
		task, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
			GenerationState: generation, BillingState: billing,
			DeleteState: VideoDeleteDeleted, NextActionAt: next, EventType: "delete_local_only",
		})
		if err == nil {
			s.enqueueBestEffort(ctx, task.PublicID)
		}
		return task, err
	}
	account, err := s.accounts.GetByID(ctx, *task.AccountID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.providers.Get(task.Provider)
	if !ok {
		return nil, ErrVideoProviderUnsupported
	}
	if task.DeleteState != VideoDeleteDeleting {
		task, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
			DeleteState: VideoDeleteDeleting, NextActionAt: timePointer(s.now().UTC()), EventType: "provider_delete_started",
		})
		if err != nil {
			return nil, err
		}
	}
	ref := ProviderTaskRef{Provider: task.Provider, AccountID: *task.AccountID, ProviderTaskID: *task.ProviderTaskID}
	requestCtx, requestCancel := context.WithTimeout(ctx, videoWorkerRequestTimeout(s.cfg))
	if task.Operation == VideoOperationCharacterCreate {
		if characters, supported := provider.(VideoCharacterProvider); supported {
			err = characters.DeleteCharacter(requestCtx, account, ProviderResourceRef{
				Provider: ref.Provider, AccountID: ref.AccountID, ProviderResourceID: ref.ProviderTaskID,
			})
		} else {
			err = ErrVideoCapabilityUnsupported
		}
	} else {
		err = provider.Delete(requestCtx, account, ref)
	}
	requestCancel()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		next := s.now().UTC().Add(videoWorkerRetryDelay(task.PollAttempts + 1))
		_, _ = s.tasks.TransitionVideoTask(videoTaskWriteContext(context.WithoutCancel(ctx), task), task.PublicID, VideoTaskTransition{
			DeleteState: VideoDeleteFailed, ErrorKind: "upstream", ErrorCode: "delete_failed",
			ErrorMessage: boundedProviderMessage(err.Error(), ""), NextActionAt: &next, EventType: "provider_delete_failed",
		})
		return nil, err
	}
	billing := task.BillingState
	generation := task.GenerationState
	if !IsVideoGenerationTerminal(generation) {
		generation = VideoGenerationCancelled
		if billing == VideoBillingHeld {
			billing = VideoBillingReleasePending
		}
	} else if generation != VideoGenerationCompleted && billing == VideoBillingHeld {
		billing = VideoBillingReleasePending
	}
	var next *time.Time
	if billing == VideoBillingCapturePending || billing == VideoBillingReleasePending {
		next = timePointer(s.now().UTC())
	}
	task, err = s.tasks.TransitionVideoTask(videoTaskWriteContext(ctx, task), task.PublicID, VideoTaskTransition{
		GenerationState: generation, BillingState: billing,
		DeleteState: VideoDeleteDeleted, NextActionAt: next, EventType: "provider_deleted",
	})
	if err == nil {
		s.enqueueBestEffort(ctx, task.PublicID)
	}
	return task, err
}

func (s *VideoTaskService) DeleteCharacterForOwner(ctx context.Context, userID int64, publicID string) error {
	if s == nil || s.resources == nil || s.tasks == nil {
		return ErrVideoResourceNotFound
	}
	resource, err := s.resources.GetVideoResourceForOwnerIncludingDeleted(ctx, userID, publicID)
	if err != nil {
		return err
	}
	if resource.DeletedAt != nil || resource.Status == "deleted" {
		return nil
	}
	task, err := s.characterSourceTask(ctx, userID, resource)
	if err != nil {
		return err
	}
	if task.DeleteState == VideoDeleteDeleted {
		return ErrVideoInvalidTransition
	}
	if _, err := s.DeleteForOwner(ctx, userID, task.PublicID); err != nil {
		return err
	}
	return ErrVideoDeletePending
}

func (request VideoSubmitRequest) providerRequest(publicModel, upstreamModel string, characters []ProviderResourceRef) VideoCreateRequest {
	return VideoCreateRequest{
		Operation: request.Operation, Model: upstreamModel, RequestedModel: publicModel,
		Prompt: request.Prompt, Seconds: request.Seconds, Size: request.Size,
		Quality: request.Quality, AudioEnabled: request.AudioEnabled,
		ServiceTier: request.ServiceTier, Characters: characters,
		InputReference: request.InputReference, ReferenceMedia: request.ReferenceMedia,
		ProviderOptions: request.ProviderOptions,
	}
}

func normalizeVideoOperation(operation string) string {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "", VideoOperationGenerate:
		return VideoOperationGenerate
	case VideoOperationEdit:
		return VideoOperationEdit
	case VideoOperationExtend:
		return VideoOperationExtend
	case VideoOperationCharacterCreate:
		return VideoOperationCharacterCreate
	default:
		return strings.ToLower(strings.TrimSpace(operation))
	}
}

func validateVideoSubmitRequest(request VideoSubmitRequest) error {
	if request.APIKey == nil || request.APIKey.ID <= 0 || request.APIKey.UserID <= 0 {
		return ErrVideoInvalidRequest
	}
	if request.Operation != VideoOperationCharacterCreate && strings.TrimSpace(request.Prompt) == "" {
		return ErrVideoInvalidRequest
	}
	switch request.Operation {
	case VideoOperationGenerate:
	case VideoOperationEdit:
		if strings.TrimSpace(request.SourceVideoID) == "" && len(request.Inputs) != 1 {
			return ErrVideoInvalidRequest
		}
		if strings.TrimSpace(request.SourceVideoID) != "" && len(request.Inputs) != 0 {
			return ErrVideoInvalidRequest
		}
	case VideoOperationExtend:
		if strings.TrimSpace(request.SourceVideoID) == "" || len(request.Inputs) != 0 || len(request.CharacterIDs) != 0 || request.InputReference != nil {
			return ErrVideoInvalidRequest
		}
	case VideoOperationCharacterCreate:
		if len(request.Inputs) != 1 || request.Inputs[0].Role != VideoInputRoleCharacterClip {
			return ErrVideoInvalidRequest
		}
	default:
		return ErrVideoCapabilityUnsupported
	}
	return nil
}

func videoEndpointForOperation(operation string) string {
	switch operation {
	case VideoOperationEdit:
		return CompositeRouteEndpointVideoEdits
	case VideoOperationExtend:
		return CompositeRouteEndpointVideoExtensions
	case VideoOperationCharacterCreate:
		return CompositeRouteEndpointVideoCharacters
	default:
		return CompositeRouteEndpointVideos
	}
}

func findVideoGroupPricing(group *Group, platform, model string) *ChannelModelPricing {
	if group == nil {
		return nil
	}
	for i := range group.ModelPricing {
		pricing := &group.ModelPricing[i]
		if strings.TrimSpace(pricing.Platform) != "" && !strings.EqualFold(strings.TrimSpace(pricing.Platform), strings.TrimSpace(platform)) {
			continue
		}
		for _, candidate := range pricing.Models {
			if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(model)) {
				copy := pricing.Clone()
				return &copy
			}
		}
	}
	return nil
}

func (s *VideoTaskService) videoCustomerMultiplier(ctx context.Context, apiKey *APIKey, group *Group) (float64, error) {
	multiplier := group.RateMultiplier
	if s.userRates != nil {
		value, err := s.userRates.GetByUserAndGroup(ctx, apiKey.UserID, group.ID)
		if err != nil {
			return 0, err
		}
		if value != nil {
			multiplier = *value
		}
	}
	apiKey.Group = group
	multiplier = resolveVideoRateMultiplier(apiKey, multiplier)
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return 0, ErrVideoPricingInvalid
	}
	return multiplier, nil
}

func videoPricingInputType(request VideoSubmitRequest) string {
	if len(request.Inputs) > 0 {
		return string(request.Inputs[0].Role)
	}
	if request.InputReference != nil {
		return string(VideoInputRoleReferenceImage)
	}
	if request.ReferenceMedia.HasVideo() {
		return string(VideoInputRoleReferenceVideo)
	}
	if request.ReferenceMedia.HasImage() {
		return string(VideoInputRoleReferenceImage)
	}
	if len(request.CharacterIDs) > 0 {
		return "character"
	}
	if request.SourceVideoID != "" {
		return string(VideoInputRoleSourceVideo)
	}
	return "text"
}

func videoMaximumOutputSeconds(request VideoSubmitRequest, providerRequest VideoCreateRequest) int {
	if request.Seconds > 0 {
		return request.Seconds
	}
	if providerRequest.Seconds > 0 {
		return providerRequest.Seconds
	}
	return 0
}

func videoRequestModeOrDefault(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return VideoRequestModeStandard
	}
	return value
}

func videoInferenceModeOrDefault(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return VideoInferenceModeOnline
	}
	return value
}

func videoInputHasVideo(request VideoSubmitRequest) bool {
	if strings.TrimSpace(request.SourceVideoID) != "" || request.ReferenceMedia.HasVideo() {
		return true
	}
	for _, input := range request.Inputs {
		if input.Role == VideoInputRoleSourceVideo || input.Role == VideoInputRoleCharacterClip ||
			strings.HasPrefix(strings.ToLower(strings.TrimSpace(input.MIMEType)), "video/") {
			return true
		}
	}
	return false
}

func trustedVideoInputSeconds(request VideoSubmitRequest, source *VideoTask) float64 {
	if !videoInputHasVideo(request) || source == nil {
		return 0
	}
	if value, ok := numericMapValue(source.ResponseMetadata, "seconds"); ok && finitePositive(value) {
		return value
	}
	if value, ok := numericMapValue(source.RequestAttributes, "seconds"); ok && finitePositive(value) {
		return value
	}
	return 0
}

func videoInputManifest(inputs []VideoInput) []VideoInputManifestEntry {
	manifest := make([]VideoInputManifestEntry, len(inputs))
	for i := range inputs {
		manifest[i] = inputs[i].VideoInputManifestEntry
	}
	return manifest
}

func videoClientRequestHash(request VideoSubmitRequest) (string, error) {
	contract := map[string]any{
		"operation": request.Operation, "model": strings.TrimSpace(request.Model),
		"prompt": request.Prompt, "seconds": request.Seconds, "size": strings.TrimSpace(request.Size),
		"quality": strings.TrimSpace(request.Quality), "audio_enabled": request.AudioEnabled,
		"service_tier": strings.TrimSpace(request.ServiceTier), "request_mode": strings.TrimSpace(request.RequestMode),
		"inference_mode": strings.TrimSpace(request.InferenceMode), "input_reference": request.InputReference,
		"inputs": videoInputHashManifest(request.Inputs), "characters": request.CharacterIDs,
		"source_video": strings.TrimSpace(request.SourceVideoID), "provider_options": request.ProviderOptions,
		"callback_url": strings.TrimSpace(request.CallbackURL),
	}
	if !request.ReferenceMedia.Empty() {
		contract["reference_media"] = request.ReferenceMedia
	}
	return HashVideoRequest(contract)
}

type videoInputHashEntry struct {
	Role     VideoInputRole `json:"role"`
	MIMEType string         `json:"mime_type"`
	Size     int64          `json:"size"`
	SHA256   string         `json:"sha256"`
}

func videoInputHashManifest(inputs []VideoInput) []videoInputHashEntry {
	manifest := make([]videoInputHashEntry, len(inputs))
	for i := range inputs {
		manifest[i] = videoInputHashEntry{
			Role: inputs[i].Role, MIMEType: inputs[i].MIMEType,
			Size: inputs[i].Size, SHA256: inputs[i].SHA256,
		}
	}
	return manifest
}

// videoPromptAttributeMaxRunes 限制随任务留存的提示词长度。request_attributes 是一块
// 与任务同生命周期的 JSONB，一条没有上限的提示词会把它撑成行外存储，且这里只为回放与
// 排障服务，两千字足够。
const videoPromptAttributeMaxRunes = 2000

func videoRequestAttributes(request VideoSubmitRequest, resolved *resolvedVideoSubmission) map[string]any {
	attrs := resolved.quote.Attributes
	attributes := map[string]any{
		"client_request_contract_version": 2,
		"account_identity_version":        resolved.account.ProviderIdentityVersion,
		"requires_verified_isolation":     request.InputReference != nil && strings.TrimSpace(request.InputReference.FileID) != "",
		"execution_spec":                  resolved.executionSpec,
		"execution_spec_version":          resolved.executionSpec.Version,
		"execution_spec_hash":             resolved.executionSpecHash,
		"source_output_seconds":           resolved.executionSpec.SourceSeconds,
		"extension_depth":                 resolved.executionSpec.ExtensionDepth,
		"total_seconds":                   resolved.executionSpec.TotalSeconds,
		"callback_contract_version":       1,
		"callback_retry_hours":            resolved.callbackRetryHours,
		"callback_disclosure_policy":      resolved.callbackDisclosurePolicy,
		"seconds":                         request.Seconds, "size": request.Size, "raw_size": request.Size,
		"resolution": attrs.Resolution, "quality": request.Quality, "audio_enabled": attrs.AudioEnabled,
		"service_tier": request.ServiceTier, "input_type": videoPricingInputType(request),
		"input_has_video": attrs.InputHasVideo, "input_video_seconds": attrs.InputVideoSeconds,
		"request_mode": attrs.RequestMode, "inference_mode": attrs.InferenceMode,
		"billing_model":   resolved.quote.BillingModel,
		"character_ids":   append([]string(nil), request.CharacterIDs...),
		"source_video_id": request.SourceVideoID,
		"reference_image_count": len(request.ReferenceMedia.ReferenceImages) + videoOptionalReferenceCount(
			request.ReferenceMedia.ImageURL, request.ReferenceMedia.FirstImageURL, request.ReferenceMedia.LastImageURL,
		),
		"reference_video_count": len(request.ReferenceMedia.ReferenceVideos),
		"reference_audio_count": len(request.ReferenceMedia.ReferenceAudios),
		"provider":              resolved.provider.Name(),
	}
	// 提示词跟着任务一起留存：它不参与任何上游请求或哈希，只是让任务事后还说得清
	// "当初要的是什么"。空提示词（character_create）不写键，省得下游把空串当成
	// "存过但为空"。
	if prompt := trimRunes(strings.TrimSpace(request.Prompt), videoPromptAttributeMaxRunes); prompt != "" {
		attributes["prompt"] = prompt
	}
	return attributes
}

func videoOptionalReferenceCount(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func videoPriceSnapshot(quote *VideoPriceQuote) map[string]any {
	snapshot := map[string]any{
		"billing_contract_version": 2, "failure_billing_policy": "release",
		"quota_time_contract_version": videoQuotaTimeContractVersion, "quota_time_zone": videoQuotaTimeZone(),
		"rule_id": quote.RuleID, "billing_unit": quote.BillingUnit,
		"rule_key": quote.RuleKey, "pricing_source": quote.Source,
		"pricing_platform": quote.PricingPlatform, "billing_model": quote.BillingModel,
		"config_version": quote.ConfigVersion, "config_hash": quote.ConfigHash,
		"conditions": append(json.RawMessage(nil), quote.Conditions...),
		"unit_price": quote.UnitPrice, "estimated_units": quote.EstimatedUnits,
		"maximum_units": quote.MaximumUnits, "customer_multiplier": quote.CustomerMultiplier,
		"estimated_cost": quote.EstimatedCost, "hold_amount": quote.HoldAmount,
		"priority": quote.Priority, "specificity": quote.Specificity,
		"operation": quote.Attributes.Operation, "raw_size": quote.Attributes.Size,
		"resolution": quote.Attributes.Resolution, "seconds": quote.Attributes.Seconds,
		"input_type": quote.Attributes.InputType, "input_has_video": quote.Attributes.InputHasVideo,
		"input_video_seconds": quote.Attributes.InputVideoSeconds, "generate_audio": quote.Attributes.AudioEnabled,
		"request_mode": quote.Attributes.RequestMode, "inference_mode": quote.Attributes.InferenceMode,
		"service_tier": quote.Attributes.ServiceTier, "quality": quote.Attributes.Quality,
	}
	usageResolution := canonicalVideoUsageResolution(quote.Attributes.Size)
	if usageResolution == "" {
		usageResolution = canonicalVideoUsageResolution(quote.Attributes.Resolution)
	}
	if usageResolution == "" {
		usageResolution = videoUsageResolutionFromModels(quote.Attributes.Model, quote.BillingModel)
	}
	if usageResolution != "" {
		snapshot["usage_resolution"] = usageResolution
	}
	if quote.Estimator != nil {
		snapshot["estimator_name"] = quote.EstimatorName
		snapshot["estimator"] = *quote.Estimator
	}
	return snapshot
}

func videoActualCost(task *VideoTask, providerTask *ProviderVideoTask) (float64, float64, error) {
	if task == nil || task.BillingUnit == nil || task.PriceSnapshot == nil {
		return 0, 0, errors.New("video price snapshot is missing")
	}
	unitPrice, ok := numericMapValue(task.PriceSnapshot, "unit_price")
	if !ok {
		return 0, 0, errors.New("video unit price is missing")
	}
	multiplier, ok := numericMapValue(task.PriceSnapshot, "customer_multiplier")
	if !ok {
		return 0, 0, errors.New("video customer multiplier is missing")
	}
	if !finiteNonNegative(unitPrice) || !finiteNonNegative(multiplier) {
		return 0, 0, errors.New("video price snapshot is invalid")
	}
	units := 1.0
	switch *task.BillingUnit {
	case VideoBillingUnitRequest:
	case VideoBillingUnitSecond:
		units, ok = numericMapValue(task.RequestAttributes, "seconds")
		if providerTask != nil {
			if value, providerOK := numericMapValue(providerTask.Metadata, "seconds"); providerOK {
				units, ok = value, true
				if task.Operation == VideoOperationExtend {
					sourceSeconds, sourceKnown := numericMapValue(task.RequestAttributes, "source_output_seconds")
					if !sourceKnown || !finitePositive(sourceSeconds) {
						return 0, 0, errors.New("video extension source duration is missing")
					}
					units -= sourceSeconds
				}
			}
		}
	case VideoBillingUnitVideoToken:
		if providerTask == nil {
			ok = false
		} else {
			var conflict bool
			units, ok, conflict = canonicalVideoTokenUsage(providerTask.Usage)
			if conflict {
				return 0, 0, errors.New("video actual usage aliases conflict")
			}
		}
	default:
		ok = false
	}
	if !ok || units <= 0 || math.IsNaN(units) || math.IsInf(units, 0) {
		return 0, 0, errors.New("video actual usage is missing")
	}
	cost := unitPrice * units * multiplier
	if !finiteNonNegative(cost) {
		return 0, 0, errors.New("video actual cost is invalid")
	}
	return units, QuantizeUsageBillingAmount(cost), nil
}

func numericMapValue(values map[string]any, key string) (float64, bool) {
	if values == nil {
		return 0, false
	}
	switch value := values[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func maxVideoAccountConcurrency(account *Account) int {
	if account != nil && account.Concurrency > 0 {
		return account.Concurrency
	}
	return 1
}

func videoStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timePointer(value time.Time) *time.Time { return &value }
