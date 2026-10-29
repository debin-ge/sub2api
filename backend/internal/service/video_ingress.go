package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
)

type VideoIngressRoute struct {
	Decision            CompositeRouteDecision
	ManagedReplay       bool
	ResolveAfterParsing bool
}

// ArkVideoTasksPathSuffix 是 Ark 视频任务协议的路径（可带 /api/v3、/v3、/v1 或无前缀）。
const ArkVideoTasksPathSuffix = "/contents/generations/tasks"

// ArkVideoModelsPathSuffix 是 sub2api 扩展的 Ark 维度能力目录路径。
const ArkVideoModelsPathSuffix = "/contents/generations/models"

// trimArkVideoPathPrefix 去掉 Ark 路径的版本前缀；非 Ark 路径原样返回。
func trimArkVideoPathPrefix(path string) string {
	for _, prefix := range []string{"/api/v3", "/v3", "/v1"} {
		if strings.HasPrefix(path, prefix+"/") {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

// IsArkVideoTasksPath 报告路径是否命中 Ark 任务协议（创建 / 查询 / 删除 / 列表 / 内容）。
func IsArkVideoTasksPath(path string) bool {
	trimmed := trimArkVideoPathPrefix(strings.TrimRight(path, "/"))
	return trimmed == ArkVideoTasksPathSuffix || strings.HasPrefix(trimmed, ArkVideoTasksPathSuffix+"/")
}

// IsArkVideoModelsPath 报告路径是否命中 Ark 维度能力目录。
func IsArkVideoModelsPath(path string) bool {
	return trimArkVideoPathPrefix(strings.TrimRight(path, "/")) == ArkVideoModelsPathSuffix
}

// IsArkVideoPath 报告路径是否属于 Ark 形态的视频入口（任务或目录）。
func IsArkVideoPath(path string) bool { return IsArkVideoTasksPath(path) || IsArkVideoModelsPath(path) }

func ManagedVideoOperationForPath(path string) (string, bool) {
	if trimArkVideoPathPrefix(strings.TrimRight(path, "/")) == ArkVideoTasksPathSuffix {
		return VideoOperationGenerate, true
	}
	if strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	switch path {
	case "/videos", "/videos/generations":
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
			if task == nil || task.UserID != key.UserID {
				return nil, ErrVideoInvalidRequest
			}
			if _, supported := managedVideoProviderForPlatform(task.Provider); !supported {
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
		// Grok's key-scoped receipt wins before evaluating a newly changed composite
		// route. Otherwise the same client key could create a second paid task on a
		// different provider after cutover or a routing edit.
		grokKey := GrokManagedIdempotencyKey(key, operation, strings.TrimSpace(idempotencyKey))
		task, grokErr := s.tasks.GetVideoTaskByIdempotency(ctx, key.UserID, endpoint, grokKey)
		if grokErr == nil {
			if task == nil || task.Provider != VideoProviderGrok || task.APIKeyID == nil || *task.APIKeyID != key.ID || task.GroupID == nil || *task.GroupID != *key.GroupID {
				return nil, ErrVideoIdempotencyConflict
			}
			return &VideoIngressRoute{ManagedReplay: true, Decision: CompositeRouteDecision{Matched: true, GroupID: *key.GroupID, PublicModel: model, TargetPlatform: PlatformGrok, UpstreamModel: model, Endpoint: endpoint}}, nil
		}
		if !errors.Is(grokErr, ErrVideoTaskNotFound) {
			return nil, grokErr
		}
	}
	if !IsValidVideoTaskID(sourceID) && (operation == VideoOperationEdit || operation == VideoOperationExtend) {
		if id, parseErr := uuid.Parse(sourceID); parseErr == nil {
			internal := "video_" + strings.ReplaceAll(id.String(), "-", "")
			candidate, lookupErr := s.tasks.GetVideoTaskForOwner(ctx, key.UserID, internal)
			if lookupErr == nil && candidate != nil && candidate.Provider == VideoProviderGrok {
				sourceID = internal
			} else if lookupErr != nil && !errors.Is(lookupErr, ErrVideoTaskNotFound) {
				return nil, lookupErr
			}
		}
	}
	if IsValidVideoTaskID(sourceID) && (operation == VideoOperationEdit || operation == VideoOperationExtend) {
		task, err := s.tasks.GetVideoTaskForOwner(ctx, key.UserID, sourceID)
		if err != nil {
			return nil, err
		}
		if task.Provider == VideoProviderGrok && (task.APIKeyID == nil || *task.APIKeyID != key.ID || task.GroupID == nil || *task.GroupID != *key.GroupID) {
			return nil, ErrVideoTaskNotFound
		}
		if _, supported := managedVideoProviderForPlatform(task.Provider); !supported {
			return nil, ErrVideoProviderUnsupported
		}
		return &VideoIngressRoute{ResolveAfterParsing: true, Decision: CompositeRouteDecision{
			Matched: true, TargetPlatform: task.Provider,
		}}, nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		var err error
		model, err = s.defaultVideoModel(nil, PlatformOpenAI)
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
	if !decision.Matched || (!IsManagedVideoPlatform(decision.TargetPlatform) && decision.TargetPlatform != PlatformGrok) {
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

func (s *VideoTaskService) defaultVideoModel(source *VideoTask, targetPlatform string) (string, error) {
	if source != nil {
		if model := firstNonEmptyString(source.RequestedModel, source.PublicModel, source.UpstreamModel); model != "" {
			return model, nil
		}
	}
	if s.providers == nil {
		return "", ErrVideoProviderUnsupported
	}
	providerName, supported := managedVideoProviderForPlatform(targetPlatform)
	if !supported {
		return "", ErrVideoProviderUnsupported
	}
	provider, ok := s.providers.Get(providerName)
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
		if !ok {
			return false
		}
		switch value {
		case videoClientRequestContractVersion:
			return task.RequestHash == hash
		case 2:
			// 升级前落库的任务存的是契约 2 的哈希，必须按旧字段集重算才能认出合法重放。
			if task.RequestHash == hash {
				return true
			}
			legacyHash, err := videoClientRequestHashV2(request)
			return err == nil && task.RequestHash == legacyHash
		default:
			return false
		}
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
