package service

import (
	"context"
	"errors"
	"strings"
)

type VideoIngressRoute struct {
	Decision            CompositeRouteDecision
	ManagedReplay       bool
	ResolveAfterParsing bool
}

func ManagedVideoOperationForPath(path string) (string, bool) {
	if strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	switch path {
	case "/videos":
		return VideoOperationGenerate, true
	case "/videos/edits":
		return VideoOperationEdit, true
	case "/videos/extensions":
		return VideoOperationExtend, true
	case "/videos/characters":
		return VideoOperationCharacterCreate, true
	default:
		return "", false
	}
}

func (s *VideoTaskService) ResolveVideoIngress(ctx context.Context, key *APIKey, operation, model, sourceID, idempotencyKey string) (*VideoIngressRoute, error) {
	if s == nil || s.tasks == nil {
		return nil, ErrBillingServiceUnavailable
	}
	if key == nil || key.ID <= 0 || key.UserID <= 0 || key.Group == nil || key.GroupID == nil ||
		key.Group.Platform != PlatformComposite || key.Group.ID != *key.GroupID {
		return nil, ErrVideoInvalidRequest
	}
	switch operation {
	case VideoOperationGenerate, VideoOperationEdit, VideoOperationExtend, VideoOperationCharacterCreate:
	default:
		return nil, ErrVideoInvalidRequest
	}
	endpoint := videoEndpointForOperation(operation)
	if strings.TrimSpace(idempotencyKey) != "" {
		task, err := s.tasks.GetVideoTaskByIdempotency(ctx, key.UserID, endpoint, strings.TrimSpace(idempotencyKey))
		if err == nil {
			if task == nil || task.UserID != key.UserID || task.Provider != VideoProviderOpenAI {
				return nil, ErrVideoInvalidRequest
			}
			return &VideoIngressRoute{ManagedReplay: true, Decision: CompositeRouteDecision{
				Matched: true, GroupID: *key.GroupID, PublicModel: model, TargetPlatform: task.Provider,
				UpstreamModel: model, Endpoint: endpoint,
			}}, nil
		}
		if !errors.Is(err, ErrVideoTaskNotFound) {
			return nil, err
		}
	}
	if IsValidVideoTaskID(sourceID) && (operation == VideoOperationEdit || operation == VideoOperationExtend) {
		task, err := s.tasks.GetVideoTaskForOwner(ctx, key.UserID, sourceID)
		if err != nil {
			return nil, err
		}
		if task.Provider != VideoProviderOpenAI {
			return nil, ErrVideoProviderUnsupported
		}
		return &VideoIngressRoute{ResolveAfterParsing: true, Decision: CompositeRouteDecision{
			Matched: true, TargetPlatform: PlatformOpenAI,
		}}, nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		var err error
		model, err = s.defaultVideoModel(nil)
		if err != nil {
			return nil, err
		}
	}
	if s.composite == nil {
		return nil, ErrVideoNoAccountAvailable
	}
	decision, err := s.composite.Resolve(ctx, *key.GroupID, model, endpoint)
	if err != nil {
		return nil, err
	}
	if !decision.Matched || (decision.TargetPlatform != PlatformOpenAI && decision.TargetPlatform != PlatformGrok) {
		return nil, ErrVideoNoAccountAvailable
	}
	if operation == VideoOperationCharacterCreate && decision.TargetPlatform != PlatformOpenAI {
		return nil, ErrVideoCapabilityUnsupported
	}
	return &VideoIngressRoute{Decision: decision}, nil
}

func (s *VideoTaskService) lookupVideoSubmissionReplay(ctx context.Context, request VideoSubmitRequest, requestHash string) (*VideoSubmitResult, error) {
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		return nil, nil
	}
	existing, err := s.tasks.GetVideoTaskByIdempotency(ctx, request.APIKey.UserID, videoEndpointForOperation(request.Operation), key)
	if errors.Is(err, ErrVideoTaskNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !videoRequestMatchesTask(request, requestHash, existing) {
		return nil, ErrVideoIdempotencyConflict
	}
	return s.replayedSubmitResult(ctx, request, existing)
}

func (s *VideoTaskService) replayAfterVideoCreationFailure(ctx context.Context, request VideoSubmitRequest, requestHash string, cause error) (*VideoSubmitResult, error) {
	replay, err := s.lookupVideoSubmissionReplay(ctx, request, requestHash)
	if err != nil || replay != nil {
		return replay, err
	}
	return nil, cause
}

func (s *VideoTaskService) defaultVideoModel(source *VideoTask) (string, error) {
	if source != nil {
		if model := firstNonEmptyString(source.RequestedModel, source.PublicModel, source.UpstreamModel); model != "" {
			return model, nil
		}
	}
	if s.providers == nil {
		return "", ErrVideoProviderUnsupported
	}
	provider, ok := s.providers.Get(VideoProviderOpenAI)
	if !ok {
		return "", ErrVideoProviderUnsupported
	}
	model := strings.TrimSpace(provider.Capabilities().DefaultModel)
	if model == "" {
		return "", ErrVideoInvalidRequest
	}
	return model, nil
}

func videoRequestMatchesTask(request VideoSubmitRequest, hash string, task *VideoTask) bool {
	if task == nil {
		return false
	}
	if version, exists := task.RequestAttributes["client_request_contract_version"]; exists {
		value, ok := numericMapValue(map[string]any{"version": version}, "version")
		return ok && value == 2 && task.RequestHash == hash
	}
	if task.RequestedModel != task.PublicModel && strings.TrimSpace(request.Model) != "" && strings.TrimSpace(request.Model) != task.RequestedModel {
		return false
	}
	if task.RequestHash == hash {
		return true
	}
	if strings.TrimSpace(request.Model) != task.RequestedModel || task.PublicModel == "" {
		return false
	}
	legacy := request
	legacy.Model = task.PublicModel
	legacyHash, err := videoClientRequestHash(legacy)
	return err == nil && task.RequestHash == legacyHash
}
