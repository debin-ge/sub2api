package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type videoIngressRouteRepo struct {
	compositeRouteRepoStub
	err   error
	reads int
}

func (repo *videoIngressRouteRepo) ListByGroup(ctx context.Context, groupID int64, includeDisabled bool) ([]CompositeModelRoute, error) {
	repo.reads++
	if repo.err != nil {
		return nil, repo.err
	}
	return repo.compositeRouteRepoStub.ListByGroup(ctx, groupID, includeDisabled)
}

func TestVideoCompositeReplayKeepsOriginalHashAndSkipsChangedRoutesAndAdmission(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video-upstream", Status: VideoGenerationQueued}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformComposite
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	routes := &videoIngressRouteRepo{compositeRouteRepoStub: compositeRouteRepoStub{routes: []CompositeModelRoute{
		{GroupID: group.ID, PublicModel: "public-video", UpstreamModel: OpenAIVideoModelSora2, TargetPlatform: PlatformOpenAI, MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointVideos, Enabled: true},
	}}}
	svc.composite = NewCompositeRouteResolver(routes)
	request := videoSubmitRequestForTest()
	request.Model, request.APIKey.Group = "public-video", group
	decision, err := svc.ResolveVideoIngress(context.Background(), request.APIKey, request.Operation, request.Model, "", request.IdempotencyKey)
	require.NoError(t, err)
	created, err := svc.Submit(WithCompositeRouteDecision(context.Background(), decision.Decision), request)
	require.NoError(t, err)
	expectedHash, err := videoClientRequestHash(request)
	require.NoError(t, err)
	require.Equal(t, expectedHash, tasks.create.RequestHash)
	require.Equal(t, "public-video", tasks.create.RequestedModel)
	require.Equal(t, OpenAIVideoModelSora2, tasks.create.PublicModel)
	require.Equal(t, 1, routes.reads)
	tasks.preflightExisting = created.Task
	routes.err = errors.New("current route configuration unavailable")
	svc.cfg.Gateway.Video.CreationEnabled = false
	admission := svc.admission.(*videoAdmissionStub)
	admission.err = ErrVideoInsufficientBalance
	decision, err = svc.ResolveVideoIngress(context.Background(), request.APIKey, request.Operation, request.Model, "", request.IdempotencyKey)
	require.NoError(t, err)
	require.True(t, decision.ManagedReplay)
	replayed, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.False(t, replayed.Created)
	require.Equal(t, created.Task.ID, replayed.Task.ID)
	require.Equal(t, 1, provider.createCalls)
	require.Equal(t, 1, routes.reads)
	require.Equal(t, 1, admission.checks)
	request.Prompt = "different prompt"
	_, err = svc.Submit(context.Background(), request)
	require.ErrorIs(t, err, ErrVideoIdempotencyConflict)
	require.Equal(t, 1, provider.createCalls)
}

func TestVideoCompositeDefaultsAndSourceModelAreResolvedBeforeRouting(t *testing.T) {
	for _, operation := range []string{VideoOperationGenerate, VideoOperationEdit, VideoOperationExtend} {
		t.Run(operation, func(t *testing.T) {
			provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video-derived", Status: VideoGenerationQueued}}
			group := videoGroupForTest(SubscriptionTypeStandard)
			group.Platform = PlatformComposite
			svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
			svc.composite = NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{
				{GroupID: group.ID, PublicModel: "public-source-video", TargetPlatform: PlatformOpenAI, UpstreamModel: OpenAIVideoModelSora2, Endpoint: videoEndpointForOperation(operation), MatchType: CompositeRouteMatchExact, Enabled: true},
			}})
			request := videoSubmitRequestForTest()
			request.Model, request.Operation, request.APIKey.Group = "", operation, group
			if operation != VideoOperationGenerate {
				sourceID, upstreamID, accountID := NewVideoTaskID(), "upstream-source", int64(11)
				tasks.sources[sourceID] = &VideoTask{ID: 9, PublicID: sourceID, UserID: 42, Provider: VideoProviderOpenAI,
					ProviderTaskID: &upstreamID, AccountID: &accountID,
					GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured,
					PublicModel: OpenAIVideoModelSora2, UpstreamModel: OpenAIVideoModelSora2,
					RequestedModel:    "public-source-video",
					RequestAttributes: map[string]any{"size": "1280x720", "seconds": 8}}
				request.SourceVideoID = sourceID
			}
			route, err := svc.ResolveVideoIngress(context.Background(), request.APIKey, request.Operation, request.Model, request.SourceVideoID, request.IdempotencyKey)
			require.NoError(t, err)
			ctx := context.Background()
			if !route.ResolveAfterParsing {
				ctx = WithCompositeRouteDecision(ctx, route.Decision)
			}
			_, err = svc.Submit(ctx, request)
			require.NoError(t, err)
			require.Equal(t, OpenAIVideoModelSora2, tasks.create.UpstreamModel)
			require.Equal(t, 1, provider.createCalls)
		})
	}
}

func TestVideoCompositeManagedSourceNeverDispatchesToGrok(t *testing.T) {
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformComposite
	svc, tasks, _ := newVideoTaskServiceForTest(&videoProviderStub{}, group, nil)
	request := videoSubmitRequestForTest()
	request.APIKey.Group = group
	source := &VideoTask{PublicID: NewVideoTaskID(), UserID: 42, Provider: VideoProviderOpenAI}
	tasks.sources[source.PublicID] = source
	route, err := svc.ResolveVideoIngress(context.Background(), request.APIKey, VideoOperationEdit, "grok-imagine-video", source.PublicID, "")
	require.NoError(t, err)
	require.True(t, route.ResolveAfterParsing)
	require.Equal(t, PlatformOpenAI, route.Decision.TargetPlatform)
	source.UserID = 99
	_, err = svc.ResolveVideoIngress(context.Background(), request.APIKey, VideoOperationEdit, "", source.PublicID, "")
	require.ErrorIs(t, err, ErrVideoTaskNotFound)
}

func TestVideoCompositeFrozenMappingSupportsLegacyReplayWithoutCurrentConfiguration(t *testing.T) {
	request := videoSubmitRequestForTest()
	request.Model = "public-alias"
	legacy := request
	legacy.Model = OpenAIVideoModelSora2
	legacyHash, err := videoClientRequestHash(legacy)
	require.NoError(t, err)
	currentHash, err := videoClientRequestHash(request)
	require.NoError(t, err)
	task := &VideoTask{RequestedModel: request.Model, PublicModel: legacy.Model, RequestHash: legacyHash}
	require.True(t, videoRequestMatchesTask(request, currentHash, task))
	request.Seconds++
	changedHash, err := videoClientRequestHash(request)
	require.NoError(t, err)
	require.False(t, videoRequestMatchesTask(request, changedHash, task))
	request.Seconds--
	task.RequestAttributes = map[string]any{"client_request_contract_version": 2}
	require.False(t, videoRequestMatchesTask(request, currentHash, task))
	task.RequestHash = currentHash
	require.True(t, videoRequestMatchesTask(request, currentHash, task))
}

func TestVideoCompositeRejectsMismatchedDecisionScope(t *testing.T) {
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformComposite
	svc, _, _ := newVideoTaskServiceForTest(&videoProviderStub{}, group, nil)
	request := videoSubmitRequestForTest()
	request.Model = "public-alias"
	for _, mismatch := range []string{"group", "endpoint", "model"} {
		decision := CompositeRouteDecision{Matched: true, GroupID: group.ID, PublicModel: request.Model,
			UpstreamModel: OpenAIVideoModelSora2, TargetPlatform: PlatformOpenAI, Endpoint: CompositeRouteEndpointVideos}
		switch mismatch {
		case "group":
			decision.GroupID++
		case "endpoint":
			decision.Endpoint = CompositeRouteEndpointVideoEdits
		case "model":
			decision.PublicModel = "different"
		}
		_, err := svc.Submit(WithCompositeRouteDecision(context.Background(), decision), request)
		require.ErrorIs(t, err, ErrVideoNoAccountAvailable)
	}
}

func TestVideoReplayFindsConcurrentCommitAfterAdmissionFailure(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video-original", Status: VideoGenerationQueued}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	original, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	admission := svc.admission.(*videoAdmissionStub)
	admission.err = ErrAPIKeyQuotaExhausted
	admission.onCheck = func() { tasks.preflightExisting = original.Task }
	replayed, err := svc.Submit(context.Background(), request)
	require.NoError(t, err)
	require.False(t, replayed.Created)
	require.Equal(t, original.Task.ID, replayed.Task.ID)
	require.Equal(t, 1, provider.createCalls)
	require.Len(t, admission.tokens, 2)
	require.Equal(t, admission.tokens[0], admission.tokens[1])
}
