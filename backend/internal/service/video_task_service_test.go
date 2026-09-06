package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoTaskRepoStub struct {
	task               *VideoTask
	existing           *VideoTask
	preflightExisting  *VideoTask
	create             VideoCreateTaskParams
	transitions        []VideoTaskTransition
	accepted           *VideoProviderAcceptance
	unknown            int
	sources            map[string]*VideoTask
	eventCreated       *bool
	events             []VideoTaskEvent
	accessCleanupCalls int
	claimByIDCalls     int
	claimedByID        *VideoTask
	dueTasks           []*VideoTask
	listUserID         int64
	listFilter         VideoTaskFilter
	listPage           *VideoTaskPage
	createErrors       map[int64]error
	transitionErr      error
	transitionErrTask  *VideoTask
}

func (r *videoTaskRepoStub) CreateHeldVideoTask(_ context.Context, params VideoCreateTaskParams) (*VideoTask, bool, error) {
	r.create = params
	if err := r.createErrors[params.AccountID]; err != nil {
		return nil, false, err
	}
	if r.existing != nil {
		return r.existing, false, nil
	}
	r.task = &VideoTask{
		ID: 1, PublicID: params.PublicID, UserID: params.Owner.UserID, APIKeyID: &params.Owner.APIKeyID,
		GroupID: params.Owner.GroupID, AccountID: &params.AccountID, AccountOwnerUserID: params.AccountOwnerUserID,
		Provider:  params.Provider,
		Operation: params.Operation, Endpoint: params.Endpoint, RequestedModel: params.RequestedModel,
		PublicModel: params.PublicModel, ChannelModel: params.ChannelModel, UpstreamModel: params.UpstreamModel,
		RequestHash: params.RequestHash, StableClientToken: &params.StableClientToken,
		GenerationState: VideoGenerationHeld, BillingState: VideoBillingHeld, DeleteState: VideoDeleteNone,
		BillingUnit: &params.BillingUnit, EstimatedUnits: &params.EstimatedUnits,
		PriceSnapshot: params.PriceSnapshot, RequestAttributes: params.RequestAttributes,
		HoldID: &params.HoldID, HoldAmount: &params.HoldAmount,
		NextActionAt: params.NextActionAt,
	}
	return r.task, true, nil
}
func (r *videoTaskRepoStub) GetVideoTaskByIdempotency(context.Context, int64, string, string) (*VideoTask, error) {
	if r.preflightExisting != nil {
		return r.preflightExisting, nil
	}
	return nil, ErrVideoTaskNotFound
}
func (r *videoTaskRepoStub) GetVideoTaskForOwner(_ context.Context, userID int64, publicID string) (*VideoTask, error) {
	if task := r.sources[publicID]; task != nil && task.UserID == userID {
		return task, nil
	}
	return nil, ErrVideoTaskNotFound
}
func (r *videoTaskRepoStub) GetVideoTaskByProviderIDForOwner(_ context.Context, userID int64, providerTaskID string) (*VideoTask, error) {
	var matched *VideoTask
	for _, task := range r.sources {
		if task == nil || task.UserID != userID || task.ProviderTaskID == nil || strings.TrimSpace(*task.ProviderTaskID) != strings.TrimSpace(providerTaskID) {
			continue
		}
		if matched != nil {
			return nil, ErrVideoInvalidRequest
		}
		matched = task
	}
	if matched == nil {
		return nil, ErrVideoTaskNotFound
	}
	return matched, nil
}
func (r *videoTaskRepoStub) GetVideoTaskByProxyKeyForOwner(_ context.Context, userID int64, proxyKey string) (*VideoTask, error) {
	for _, task := range r.sources {
		if task != nil && task.UserID == userID && task.ProviderVideoProxyKey != nil && *task.ProviderVideoProxyKey == strings.TrimSpace(proxyKey) {
			return task, nil
		}
	}
	return nil, ErrVideoTaskNotFound
}
func (r *videoTaskRepoStub) GetVideoTaskByProxyKey(_ context.Context, proxyKey string) (*VideoTask, error) {
	for _, task := range r.sources {
		if task != nil && task.ProviderVideoProxyKey != nil && *task.ProviderVideoProxyKey == strings.TrimSpace(proxyKey) {
			return task, nil
		}
	}
	return nil, ErrVideoTaskNotFound
}
func (r *videoTaskRepoStub) GetVideoTaskByPublicID(context.Context, string) (*VideoTask, error) {
	return r.task, nil
}
func (r *videoTaskRepoStub) GetVideoTaskByProviderID(_ context.Context, provider string, accountID int64, providerID string) (*VideoTask, error) {
	for _, task := range append([]*VideoTask{r.task}, videoTestSourceTasks(r.sources)...) {
		if task != nil && task.Provider == provider && task.AccountID != nil && *task.AccountID == accountID && task.ProviderTaskID != nil && *task.ProviderTaskID == providerID {
			return task, nil
		}
	}
	return nil, ErrVideoTaskNotFound
}

func videoTestSourceTasks(sources map[string]*VideoTask) []*VideoTask {
	values := make([]*VideoTask, 0, len(sources))
	for _, task := range sources {
		values = append(values, task)
	}
	return values
}
func (r *videoTaskRepoStub) ListVideoTasksForOwner(_ context.Context, userID int64, filter VideoTaskFilter) (*VideoTaskPage, error) {
	r.listUserID = userID
	r.listFilter = filter
	if r.listPage != nil {
		return r.listPage, nil
	}
	return &VideoTaskPage{}, nil
}
func (r *videoTaskRepoStub) TransitionVideoTask(_ context.Context, _ string, transition VideoTaskTransition) (*VideoTask, error) {
	if r.transitionErr != nil {
		err := r.transitionErr
		r.transitionErr = nil
		if r.transitionErrTask != nil {
			r.task = r.transitionErrTask
		}
		return nil, err
	}
	r.transitions = append(r.transitions, transition)
	if transition.GenerationState != "" {
		r.task.GenerationState = transition.GenerationState
	}
	if transition.BillingState != "" {
		r.task.BillingState = transition.BillingState
	}
	if transition.DeleteState != "" {
		r.task.DeleteState = transition.DeleteState
	}
	if transition.ActualUnits != nil {
		r.task.ActualUnits = transition.ActualUnits
	}
	if transition.ActualCost != nil {
		r.task.ActualCost = transition.ActualCost
	}
	if transition.ProviderFinishedAt != nil {
		r.task.ProviderFinishedAt = transition.ProviderFinishedAt
	}
	if transition.ProviderVideoURLEnc != "" {
		r.task.ProviderVideoURLEnc = &transition.ProviderVideoURLEnc
	}
	if transition.ProviderVideoProxyKey != "" {
		r.task.ProviderVideoProxyKey = &transition.ProviderVideoProxyKey
	}
	r.task.NextActionAt = transition.NextActionAt
	if transition.ErrorKind != "" {
		r.task.LastErrorKind = &transition.ErrorKind
	}
	if transition.ErrorCode != "" {
		r.task.LastErrorCode = &transition.ErrorCode
	}
	if transition.ErrorMessage != "" {
		r.task.LastErrorMessage = &transition.ErrorMessage
	}
	if transition.IncrementPollAttempts {
		r.task.PollAttempts++
	}
	return r.task, nil
}
func (r *videoTaskRepoStub) SaveVideoProviderAccepted(_ context.Context, _ string, acceptance VideoProviderAcceptance) (*VideoTask, error) {
	r.accepted = &acceptance
	r.task.ProviderTaskID = &acceptance.ProviderTaskID
	r.task.GenerationState = acceptance.GenerationState
	if acceptance.BillingState != "" {
		r.task.BillingState = acceptance.BillingState
	}
	r.task.UsageSnapshot = acceptance.UsageSnapshot
	r.task.ResponseMetadata = acceptance.ResponseMetadata
	r.task.ProviderFinishedAt = acceptance.ProviderFinishedAt
	if acceptance.ProviderVideoURLEnc != "" {
		r.task.ProviderVideoURLEnc = &acceptance.ProviderVideoURLEnc
	}
	if acceptance.ProviderVideoProxyKey != "" {
		r.task.ProviderVideoProxyKey = &acceptance.ProviderVideoProxyKey
	}
	r.task.ActualUnits = acceptance.ActualUnits
	r.task.ActualCost = acceptance.ActualCost
	r.task.NextActionAt = acceptance.NextActionAt
	if acceptance.ErrorKind != "" {
		r.task.LastErrorKind = &acceptance.ErrorKind
	}
	if acceptance.ErrorCode != "" {
		r.task.LastErrorCode = &acceptance.ErrorCode
	}
	if acceptance.ErrorMessage != "" {
		r.task.LastErrorMessage = &acceptance.ErrorMessage
	}
	return r.task, nil
}
func (r *videoTaskRepoStub) MarkVideoSubmissionUnknown(_ context.Context, _ string, _ *VideoProviderError, _ time.Time) (*VideoTask, error) {
	r.unknown++
	r.task.GenerationState = VideoGenerationSubmissionUnknown
	return r.task, nil
}
func (r *videoTaskRepoStub) ClaimVideoTask(context.Context, string, string, time.Duration) (*VideoTask, error) {
	r.claimByIDCalls++
	return r.claimedByID, nil
}
func (r *videoTaskRepoStub) ClaimDueVideoTasks(_ context.Context, _ string, limit int, _ time.Duration) ([]*VideoTask, error) {
	if len(r.dueTasks) < limit {
		limit = len(r.dueTasks)
	}
	tasks := r.dueTasks[:limit]
	r.dueTasks = r.dueTasks[limit:]
	return tasks, nil
}
func (r *videoTaskRepoStub) RenewVideoTaskLease(context.Context, VideoTaskLease, time.Duration) (time.Time, error) {
	return time.Now().Add(time.Minute), nil
}
func (r *videoTaskRepoStub) ReleaseVideoTaskLease(context.Context, VideoTaskLease, *time.Time) error {
	return nil
}
func (r *videoTaskRepoStub) ClearExpiredVideoProviderAccess(context.Context, int) (int64, error) {
	r.accessCleanupCalls++
	return 0, nil
}
func (r *videoTaskRepoStub) AppendVideoTaskEvent(_ context.Context, event VideoTaskEvent) (bool, error) {
	r.events = append(r.events, event)
	if r.eventCreated != nil {
		return *r.eventCreated, nil
	}
	return true, nil
}

type videoResourceRepoStub struct {
	resources map[string]*VideoResource
	bySource  map[int64]*VideoResource
	createErr error
}

func (r *videoResourceRepoStub) CreateVideoResource(_ context.Context, params VideoCreateResourceParams) (*VideoResource, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	resource := &VideoResource{ID: 2, PublicID: NewVideoResourceID(), UserID: params.Owner.UserID, AccountID: params.AccountID,
		Provider: params.Provider, SourceTaskID: params.SourceTaskID, ProviderResourceID: params.ProviderResourceID, Status: params.Status}
	if params.SourceTaskID != nil {
		r.bySource[*params.SourceTaskID] = resource
	}
	return resource, nil
}
func (r *videoResourceRepoStub) GetVideoResourceForOwner(_ context.Context, userID int64, publicID string) (*VideoResource, error) {
	resource := r.resources[publicID]
	if resource == nil || resource.UserID != userID || resource.DeletedAt != nil {
		return nil, ErrVideoResourceNotFound
	}
	return resource, nil
}
func (r *videoResourceRepoStub) GetVideoResourceForOwnerIncludingDeleted(_ context.Context, userID int64, publicID string) (*VideoResource, error) {
	resource := r.resources[publicID]
	if resource == nil || resource.UserID != userID {
		return nil, ErrVideoResourceNotFound
	}
	return resource, nil
}
func (r *videoResourceRepoStub) GetVideoResourceBySourceTaskForOwner(_ context.Context, userID int64, sourceTaskID int64) (*VideoResource, error) {
	resource := r.bySource[sourceTaskID]
	if resource == nil || resource.UserID != userID {
		return nil, ErrVideoResourceNotFound
	}
	return resource, nil
}
func (r *videoResourceRepoStub) GetVideoResourceByProviderID(context.Context, string, int64, string) (*VideoResource, error) {
	return nil, ErrVideoResourceNotFound
}
func (r *videoResourceRepoStub) MarkVideoResourceDeleted(context.Context, int64, string) error {
	return nil
}

type videoQueueStub struct {
	enqueued   []string
	requeued   []string
	reserved   []string
	acked      []string
	reserveErr error
}

func (q *videoQueueStub) Enqueue(_ context.Context, id string) (bool, error) {
	q.enqueued = append(q.enqueued, id)
	return true, nil
}
func (q *videoQueueStub) Reserve(context.Context) (string, error) {
	if q.reserveErr != nil {
		return "", q.reserveErr
	}
	if len(q.reserved) == 0 {
		return "", ErrVideoQueueEmpty
	}
	id := q.reserved[0]
	q.reserved = q.reserved[1:]
	return id, nil
}
func (q *videoQueueStub) RequeueAfter(_ context.Context, id string, _ time.Duration) error {
	q.requeued = append(q.requeued, id)
	return nil
}
func (q *videoQueueStub) Ack(_ context.Context, id string) error {
	q.acked = append(q.acked, id)
	return nil
}
func (q *videoQueueStub) MoveDueToReady(context.Context, int) (int, error) { return 0, nil }
func (q *videoQueueStub) RecoverStale(context.Context, time.Duration, int) (int, error) {
	return 0, nil
}

type videoAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

type videoDisclosureAccountRepoStub struct {
	*videoAccountRepoStub
	allowed          bool
	authorizationErr error
}

func (r *videoDisclosureAccountRepoStub) CanScheduleAccountForUser(context.Context, int64, int64) (bool, error) {
	return r.allowed, r.authorizationErr
}

func (r *videoAccountRepoStub) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}
func (r *videoAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, ErrAccountNotFound
}

type videoGroupRepoStub struct {
	GroupRepository
	group *Group
}

func (r *videoGroupRepoStub) GetByID(context.Context, int64) (*Group, error) { return r.group, nil }

type videoUserRateRepoStub struct {
	UserGroupRateRepository
	rate *float64
}

func (r *videoUserRateRepoStub) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	return r.rate, nil
}

type videoProviderStub struct {
	name             string
	result           *ProviderVideoTask
	err              error
	createCalls      int
	createAccountIDs []int64
	getCalls         int
	deleteCalls      int
	characterDeletes int
	deleteErr        error
	contentCalls     int
	contentReq       ProviderContentRequest
	content          *ProviderContent
	contentErr       error
	request          VideoCreateRequest
	editSource       *ProviderTaskRef
	webhookEvent     *ProviderWebhookEvent
	character        *ProviderVideoResource
	characterErr     error
	characterGets    int
	validationErr    error
	validationCalls  int
}

func (p *videoProviderStub) Name() string {
	if p.name != "" {
		return p.name
	}
	return VideoProviderOpenAI
}
func (p *videoProviderStub) Capabilities() VideoCapabilities { return DefaultOpenAIVideoCapabilities() }
func (p *videoProviderStub) SupportsAccount(*Account) bool   { return true }
func (p *videoProviderStub) ValidateSubmission(_ *Account, _ VideoCreateRequest, _ []VideoInput) error {
	p.validationCalls++
	return p.validationErr
}
func (p *videoProviderStub) Create(_ context.Context, account *Account, request VideoCreateRequest, _ []VideoInput) (*ProviderVideoTask, error) {
	p.createCalls++
	if account != nil {
		p.createAccountIDs = append(p.createAccountIDs, account.ID)
	}
	p.request = request
	return p.result, p.err
}
func (p *videoProviderStub) Edit(_ context.Context, _ *Account, request VideoEditRequest, _ []VideoInput) (*ProviderVideoTask, error) {
	p.createCalls++
	p.request = request.VideoCreateRequest
	p.editSource = request.SourceTask
	return p.result, p.err
}
func (p *videoProviderStub) Extend(_ context.Context, _ *Account, request VideoExtendRequest) (*ProviderVideoTask, error) {
	p.createCalls++
	p.request = request.VideoCreateRequest
	return p.result, p.err
}
func (p *videoProviderStub) CreateCharacter(_ context.Context, _ *Account, _ VideoCharacterRequest, _ VideoInput) (*ProviderVideoResource, error) {
	p.createCalls++
	if p.err != nil {
		return nil, p.err
	}
	if p.character != nil {
		return p.character, nil
	}
	return &ProviderVideoResource{ProviderResourceID: "char_upstream", Status: "ready"}, nil
}
func (p *videoProviderStub) GetCharacter(context.Context, *Account, ProviderResourceRef) (*ProviderVideoResource, error) {
	p.characterGets++
	if p.characterErr != nil {
		return nil, p.characterErr
	}
	if p.character != nil {
		return p.character, nil
	}
	return nil, ErrVideoResourceNotFound
}
func (p *videoProviderStub) DeleteCharacter(context.Context, *Account, ProviderResourceRef) error {
	p.characterDeletes++
	return p.deleteErr
}
func (p *videoProviderStub) Get(context.Context, *Account, ProviderTaskRef) (*ProviderVideoTask, error) {
	p.getCalls++
	return p.result, p.err
}
func (p *videoProviderStub) OpenContent(_ context.Context, _ *Account, request ProviderContentRequest) (*ProviderContent, error) {
	p.contentCalls++
	p.contentReq = request
	if p.content != nil || p.contentErr != nil {
		return p.content, p.contentErr
	}
	return &ProviderContent{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("video"))}, nil
}
func (p *videoProviderStub) Delete(context.Context, *Account, ProviderTaskRef) error {
	p.deleteCalls++
	return p.deleteErr
}
func (p *videoProviderStub) VerifyWebhook(context.Context, *Account, ProviderWebhookRequest) (*ProviderWebhookEvent, error) {
	return p.webhookEvent, p.err
}

type closeTrackingVideoContentBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingVideoContentBody) Close() error {
	b.closed = true
	return nil
}

type videoEncryptorStub struct{}

func (videoEncryptorStub) Encrypt(value string) (string, error) { return "enc:" + value, nil }
func (videoEncryptorStub) Decrypt(value string) (string, error) {
	return strings.TrimPrefix(value, "enc:"), nil
}

func newVideoTaskServiceForTest(provider *videoProviderStub, group *Group, resources map[string]*VideoResource) (*VideoTaskService, *videoTaskRepoStub, *videoQueueStub) {
	taskRepo := &videoTaskRepoStub{sources: make(map[string]*VideoTask)}
	for _, resource := range resources {
		source := &VideoTask{ID: int64(100 + len(taskRepo.sources)), PublicID: NewVideoTaskID(), UserID: resource.UserID,
			Provider: resource.Provider, AccountID: &resource.AccountID, ProviderTaskID: &resource.ProviderResourceID,
			Operation: VideoOperationCharacterCreate, GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured, DeleteState: VideoDeleteNone}
		if resource.SourceTaskID != nil {
			source.ID = *resource.SourceTaskID
		} else {
			resource.SourceTaskID = &source.ID
		}
		taskRepo.sources[source.PublicID] = source
	}
	queue := &videoQueueStub{}
	account := Account{
		ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 4,
		Credentials: map[string]any{"api_key": "sk-test"},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
		Enabled: true, CreationEnabled: true, SubmitTimeoutSeconds: 10,
		PollIntervalSeconds: 10, SubmissionUnknownQuarantineMinutes: 60,
	}}}
	service := NewVideoTaskService(
		taskRepo,
		&videoResourceRepoStub{resources: resources, bySource: make(map[int64]*VideoResource)},
		queue,
		&videoAccountRepoStub{accounts: []Account{account}},
		&videoGroupRepoStub{group: group},
		&videoUserRateRepoStub{},
		nil,
		nil,
		NewVideoProviderRegistry(provider),
		NewVideoPricingResolver(nil, nil),
		&videoSettlementRepoStub{},
		videoEncryptorStub{},
		nil,
		cfg,
	)
	service.admission = &videoAdmissionStub{}
	service.settlements.(*videoSettlementRepoStub).onSettle = func(settlement *BalanceSettlementCommand) {
		if settlement.Action == BalanceSettlementRelease && taskRepo.task != nil {
			taskRepo.task.BillingState = VideoBillingReleased
			taskRepo.task.ActualCost = floatPointer(0)
		}
	}
	service.now = func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) }
	return service, taskRepo, queue
}

type videoAdmissionStub struct {
	err           error
	checks        int
	tokens        []string
	invalidations int
	onCheck       func()
}

func (admission *videoAdmissionStub) CheckVideoAdmission(_ context.Context, _ *APIKey, _ *Group, _ string, token string) error {
	admission.checks++
	admission.tokens = append(admission.tokens, token)
	if admission.onCheck != nil {
		admission.onCheck()
	}
	return admission.err
}

func (admission *videoAdmissionStub) InvalidateVideoHold(context.Context, int64) error {
	admission.invalidations++
	return nil
}

func videoGroupForTest(subscription string) *Group {
	unit := VideoBillingUnitSecond
	price := 0.5
	return &Group{
		ID: 7, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: subscription,
		RateMultiplier: 2,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{OpenAIVideoModelSora2}, BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{{ID: 5, PerRequestPrice: &price, BillingUnit: &unit}},
		}},
	}
}

func videoSubmitRequestForTest() VideoSubmitRequest {
	groupID := int64(7)
	return VideoSubmitRequest{
		APIKey:    &APIKey{ID: 3, UserID: 42, GroupID: &groupID},
		Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
		Prompt: "A tracking shot", Seconds: 8, Size: "1280x720", IdempotencyKey: "idem-1",
	}
}

func TestVideoTaskServiceSubmitAccepted(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued,
		RawStatus: "queued", SuggestedPollInterval: 60 * time.Second,
	}}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, VideoGenerationQueued, result.Task.GenerationState)
	require.Equal(t, int64(11), tasks.create.AccountID)
	require.InDelta(t, 8, tasks.create.HoldAmount, 0.000001)
	require.Equal(t, "video_upstream", *result.Task.ProviderTaskID)
	require.Equal(t, 1, provider.createCalls)
	require.NotEmpty(t, queue.enqueued)
	require.NotEmpty(t, tasks.transitions)
	require.NotNil(t, tasks.create.NextActionAt)
	require.Equal(t, svc.now().UTC().Add(15*time.Second), *tasks.create.NextActionAt)
	require.NotNil(t, tasks.transitions[0].NextActionAt)
	require.Equal(t, svc.now().UTC().Add(15*time.Second), *tasks.transitions[0].NextActionAt)
	require.NotNil(t, tasks.accepted)
	require.NotNil(t, tasks.accepted.NextActionAt)
	require.Equal(t, svc.now().UTC().Add(10*time.Second), *tasks.accepted.NextActionAt)
}

func TestVideoTaskServiceIdempotencyReplayBypassesChangedRoutingAndPricing(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	hash, err := videoClientRequestHash(request)
	require.NoError(t, err)
	existing := &VideoTask{
		ID: 23, PublicID: "video_dddddddddddddddddddddddddddddddd", UserID: request.APIKey.UserID,
		Operation: request.Operation, Endpoint: CompositeRouteEndpointVideos, RequestHash: hash,
		GenerationState: VideoGenerationQueued, BillingState: VideoBillingHeld, DeleteState: VideoDeleteNone,
	}
	tasks.preflightExisting = existing
	svc.groups = nil
	svc.accounts = nil
	svc.pricing = nil

	result, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.False(t, result.Created)
	require.Same(t, existing, result.Task)
	require.Zero(t, provider.createCalls)
	require.Empty(t, tasks.create.PublicID)
}

func TestVideoTaskServiceIdempotencyPreflightRejectsDifferentRequest(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	tasks.preflightExisting = &VideoTask{
		ID: 23, PublicID: "video_dddddddddddddddddddddddddddddddd", UserID: request.APIKey.UserID,
		Operation: request.Operation, Endpoint: CompositeRouteEndpointVideos, RequestHash: "different",
	}

	result, err := svc.Submit(context.Background(), request)

	require.ErrorIs(t, err, ErrVideoIdempotencyConflict)
	require.Nil(t, result)
	require.Zero(t, provider.createCalls)
}

func TestVideoClientRequestHashUsesInputContentNotFilenameOrDerivedDimensions(t *testing.T) {
	request := videoSubmitRequestForTest()
	request.Inputs = []VideoInput{{VideoInputManifestEntry: VideoInputManifestEntry{
		Role: VideoInputRoleReferenceImage, FileName: "first.png", MIMEType: "image/png",
		Size: 100, SHA256: "content-hash", Width: 1280, Height: 720,
	}}}
	first, err := videoClientRequestHash(request)
	require.NoError(t, err)

	request.Inputs[0].FileName = "renamed.png"
	request.Inputs[0].Width = 2560
	request.Inputs[0].Height = 1440
	second, err := videoClientRequestHash(request)
	require.NoError(t, err)
	require.Equal(t, first, second)

	request.Inputs[0].SHA256 = "different-content"
	third, err := videoClientRequestHash(request)
	require.NoError(t, err)
	require.NotEqual(t, first, third)
}

func TestVideoTaskServiceAppliesOpenAIDefaultsBeforeHoldAndSubmission(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	request.Model = ""
	request.Seconds = 0
	request.Size = ""

	result, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.Equal(t, OpenAIVideoModelSora2, result.Task.RequestedModel)
	require.Equal(t, 4, provider.request.Seconds)
	require.Equal(t, "720x1280", provider.request.Size)
	require.Equal(t, 4, tasks.create.RequestAttributes["seconds"])
	require.Equal(t, "720x1280", tasks.create.RequestAttributes["size"])
	require.InDelta(t, 4, tasks.create.HoldAmount, 0.000001)
}

func TestVideoTaskServiceUsesModelPriceVideoProfileWhenGroupAndChannelHaveNoPrice(t *testing.T) {
	const model = "doubao-seedance-2.0-mini-480p"
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_seedance", Status: VideoGenerationQueued,
		RawStatus: "queued", SuggestedPollInterval: 10 * time.Second,
	}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.ModelPricing = nil
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	pricing := NewPricingService(&config.Config{}, nil)
	profile := seedanceVideoPricing()
	profile.Defaults.RequestMode = VideoRequestModeBatch
	profile.Defaults.InferenceMode = VideoInferenceModeOffline
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{
		model: {VideoPricing: profile, PricePresenceKnown: true, TokenPricingAbsent: true},
	})
	svc.pricing = NewVideoPricingResolver(nil, pricing)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Credentials["model_mapping"] = map[string]any{model: model}
	request := videoSubmitRequestForTest()
	request.Model = model
	request.Size = "864x480"
	request.Seconds = 5

	result, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, VideoBillingUnitVideoToken, tasks.create.BillingUnit)
	require.Equal(t, VideoPricingSourceCatalog, tasks.create.PriceSnapshot["pricing_source"])
	require.Equal(t, model, tasks.create.PriceSnapshot["billing_model"])
	require.Equal(t, "480p-no-video", tasks.create.PriceSnapshot["rule_key"])
	require.Equal(t, "480p", tasks.create.RequestAttributes["resolution"])
	require.Equal(t, VideoRequestModeStandard, tasks.create.RequestAttributes["request_mode"])
	require.Equal(t, VideoInferenceModeOnline, tasks.create.RequestAttributes["inference_mode"])
	require.Equal(t, 48_600.0, tasks.create.EstimatedUnits)
	require.InDelta(t, 0.0972, tasks.create.HoldAmount, 1e-9, "group multiplier remains applied")
}

func TestVideoTaskServicePricesCompatibleReferenceVideoAsVideoInput(t *testing.T) {
	const model = "doubao-seedance-2.0-mini-480p"
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_seedance_reference", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.ModelPricing = nil
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.SeedCatalogForTest(map[string]*ModelPriceEntry{
		model: {VideoPricing: seedanceVideoPricing(), PricePresenceKnown: true, TokenPricingAbsent: true},
	})
	svc.pricing = NewVideoPricingResolver(nil, pricing)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Credentials["model_mapping"] = map[string]any{model: model}
	request := videoSubmitRequestForTest()
	request.Model = model
	request.Size = "864x480"
	request.Seconds = 5
	request.ReferenceMedia.ReferenceVideos = []string{"https://media.example.com/reference.mp4?auth_key=signed"}

	result, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, "480p-reference-video", tasks.create.PriceSnapshot["rule_key"])
	require.Equal(t, true, tasks.create.RequestAttributes["input_has_video"])
	require.Equal(t, 1, tasks.create.RequestAttributes["reference_video_count"])
	require.InDelta(t, 0.1944, tasks.create.HoldAmount, 1e-9)
	require.NotContains(t, tasks.create.RequestAttributes, "reference_videos")
}

func TestVideoReferenceMediaPreservesLegacyRequestHashWhenEmpty(t *testing.T) {
	request := videoSubmitRequestForTest()
	legacyHash, err := HashVideoRequest(map[string]any{
		"operation": request.Operation, "model": strings.TrimSpace(request.Model),
		"prompt": request.Prompt, "seconds": request.Seconds, "size": strings.TrimSpace(request.Size),
		"quality": strings.TrimSpace(request.Quality), "audio_enabled": request.AudioEnabled,
		"service_tier": strings.TrimSpace(request.ServiceTier), "request_mode": strings.TrimSpace(request.RequestMode),
		"inference_mode": strings.TrimSpace(request.InferenceMode), "input_reference": request.InputReference,
		"inputs": videoInputHashManifest(request.Inputs), "characters": request.CharacterIDs,
		"source_video": strings.TrimSpace(request.SourceVideoID), "provider_options": request.ProviderOptions,
		"callback_url": strings.TrimSpace(request.CallbackURL),
	})
	require.NoError(t, err)

	currentHash, err := videoClientRequestHash(request)
	require.NoError(t, err)
	require.Equal(t, legacyHash, currentHash)

	request.ReferenceMedia.ReferenceVideos = []string{"https://media.example.com/reference.mp4"}
	referenceHash, err := videoClientRequestHash(request)
	require.NoError(t, err)
	require.NotEqual(t, legacyHash, referenceHash)
}

func TestVideoTaskServiceForwardsWhitelistedCustomVideoModel(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued,
		RawStatus: "queued", SuggestedPollInterval: 10 * time.Second,
	}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.ModelPricing[0].Models = []string{"custom-public-model"}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Credentials["model_mapping"] = map[string]any{
		"custom-public-model": "custom-upstream-model",
	}
	request := videoSubmitRequestForTest()
	request.Model = "custom-public-model"

	_, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.Equal(t, "custom-public-model", tasks.create.RequestedModel)
	require.Equal(t, "custom-upstream-model", tasks.create.UpstreamModel)
	require.Equal(t, "custom-upstream-model", provider.request.Model)
	require.Equal(t, "custom-public-model", provider.request.RequestedModel)
}

func TestVideoTaskServiceRejectsCustomVideoModelOutsideAccountWhitelist(t *testing.T) {
	provider := &videoProviderStub{}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.ModelPricing[0].Models = []string{"custom-public-model"}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].Credentials["model_mapping"] = map[string]any{
		"different-model": "different-upstream-model",
	}
	request := videoSubmitRequestForTest()
	request.Model = "custom-public-model"

	_, err := svc.Submit(context.Background(), request)

	require.ErrorIs(t, err, ErrVideoNoAccountAvailable)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceUsesCompositeDecisionWithOriginalClientModel(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued,
		RawStatus: "queued", SuggestedPollInterval: 10 * time.Second,
	}}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.Platform = PlatformComposite
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	request := videoSubmitRequestForTest()
	request.Model = "video-alias"
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched: true, GroupID: group.ID, PublicModel: "video-alias", TargetPlatform: PlatformOpenAI,
		UpstreamModel: OpenAIVideoModelSora2, Endpoint: CompositeRouteEndpointVideos,
	})

	_, err := svc.Submit(ctx, request)

	require.NoError(t, err)
	require.Equal(t, "video-alias", tasks.create.RequestedModel)
	require.Equal(t, OpenAIVideoModelSora2, tasks.create.PublicModel)
}

func TestVideoTaskServiceResolvesParentAndRootForDerivedTask(t *testing.T) {
	providerID := "video_source_upstream"
	accountID := int64(11)
	rootID := int64(5)
	sourceID := "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_edit_upstream", Status: VideoGenerationQueued,
		RawStatus: "queued", SuggestedPollInterval: 10 * time.Second,
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	tasks.sources[sourceID] = &VideoTask{
		ID: 10, PublicID: sourceID, UserID: 42, Provider: VideoProviderOpenAI,
		ProviderTaskID: &providerID, AccountID: &accountID, RootTaskID: &rootID,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured,
		DeleteState: VideoDeleteNone, PublicModel: OpenAIVideoModelSora2,
		UpstreamModel:     OpenAIVideoModelSora2,
		RequestAttributes: map[string]any{"size": "1280x720", "seconds": 8},
	}
	request := videoSubmitRequestForTest()
	request.Operation = VideoOperationEdit
	request.SourceVideoID = sourceID

	resolved, err := svc.resolveSubmission(context.Background(), request, nil)

	require.NoError(t, err)
	require.Equal(t, int64(10), *videoParentTaskID(resolved.sourceTask))
	require.Equal(t, rootID, *videoRootTaskID(resolved.sourceTask))
}

func TestVideoTaskServiceResolvesOwnedProviderVideoIDAndInheritsModel(t *testing.T) {
	providerID := "video_provider_owned"
	accountID := int64(11)
	sourceID := "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_edit_upstream", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	tasks.sources[sourceID] = &VideoTask{
		ID: 10, PublicID: sourceID, UserID: 42, Provider: VideoProviderOpenAI,
		ProviderTaskID: &providerID, AccountID: &accountID,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured,
		DeleteState: VideoDeleteNone, PublicModel: OpenAIVideoModelSora2,
		UpstreamModel:     OpenAIVideoModelSora2,
		RequestAttributes: map[string]any{"size": "1280x720", "seconds": 8},
	}
	request := videoSubmitRequestForTest()
	request.Operation = VideoOperationEdit
	request.SourceVideoID = providerID
	request.Model = ""

	resolved, err := svc.resolveSubmission(context.Background(), request, nil)

	require.NoError(t, err)
	require.Equal(t, OpenAIVideoModelSora2, resolved.requestedModel)
	require.Equal(t, int64(10), *videoParentTaskID(resolved.sourceTask))
	require.NotNil(t, resolved.source)
	require.Equal(t, providerID, resolved.source.ProviderTaskID)
}

func TestVideoTaskServiceCharacterIdempotencyReplayReturnsResource(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	existing := &VideoTask{
		ID: 17, PublicID: "video_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", UserID: 42,
		Operation: VideoOperationCharacterCreate, GenerationState: VideoGenerationCompleted,
		BillingState: VideoBillingCaptured, DeleteState: VideoDeleteNone,
	}
	resource := &VideoResource{
		ID: 19, PublicID: "char_cccccccccccccccccccccccccccccccc", UserID: 42,
		Provider: VideoProviderOpenAI, AccountID: 11, ProviderResourceID: "char_upstream", Status: "ready",
	}
	tasks.existing = existing
	svc.resources.(*videoResourceRepoStub).bySource[existing.ID] = resource
	request := videoSubmitRequestForTest()
	request.Operation = VideoOperationCharacterCreate
	request.Prompt = ""
	request.Inputs = []VideoInput{{VideoInputManifestEntry: VideoInputManifestEntry{
		Role: VideoInputRoleCharacterClip, FileName: "character.mp4", MIMEType: "video/mp4", Size: 4, SHA256: "abcd",
	}}}

	result, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.Same(t, resource, result.Resource)
	require.False(t, result.Created)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceCharacterCreationPersistsResource(t *testing.T) {
	unit := VideoBillingUnitRequest
	price := 0.25
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.ModelPricing[0].Intervals = []PricingInterval{{ID: 8, TierLabel: "character", PerRequestPrice: &price, BillingUnit: &unit}}
	provider := &videoProviderStub{}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, group, nil)
	request := videoSubmitRequestForTest()
	request.Operation = VideoOperationCharacterCreate
	request.Prompt = ""
	request.ProviderOptions = map[string]any{"name": "Mossy"}
	request.Inputs = []VideoInput{{VideoInputManifestEntry: VideoInputManifestEntry{
		Role: VideoInputRoleCharacterClip, FileName: "character.mp4", MIMEType: "video/mp4", Size: 4, SHA256: "abcd",
	}}}

	result, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.NotNil(t, result.Resource)
	require.NotNil(t, tasks.task)
	require.Equal(t, "char_upstream", result.Resource.ProviderResourceID)
	require.Equal(t, 1, provider.createCalls)
	require.NotEmpty(t, queue.enqueued)
}

func TestVideoTaskServiceCharacterReplayRecoversFailedResourcePersistence(t *testing.T) {
	unit := VideoBillingUnitRequest
	price := 0.25
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.ModelPricing[0].Intervals = []PricingInterval{{ID: 8, TierLabel: "character", PerRequestPrice: &price, BillingUnit: &unit}}
	provider := &videoProviderStub{character: &ProviderVideoResource{ProviderResourceID: "char_upstream", Status: "ready"}}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, group, nil)
	resources := svc.resources.(*videoResourceRepoStub)
	resources.createErr = errors.New("database unavailable")
	request := videoSubmitRequestForTest()
	request.Operation = VideoOperationCharacterCreate
	request.Prompt = ""
	request.ProviderOptions = map[string]any{"name": "Mossy"}
	request.Inputs = []VideoInput{{VideoInputManifestEntry: VideoInputManifestEntry{
		Role: VideoInputRoleCharacterClip, FileName: "character.mp4", MIMEType: "video/mp4", Size: 4, SHA256: "abcd",
	}}}

	result, err := svc.Submit(context.Background(), request)
	require.EqualError(t, err, "database unavailable")
	require.Nil(t, result)
	require.NotNil(t, tasks.task)

	resources.createErr = nil
	tasks.preflightExisting = tasks.task
	replayed, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.NotNil(t, replayed.Resource)
	require.Equal(t, 1, provider.characterGets)
	require.NotEmpty(t, queue.enqueued)
}

func TestVideoTaskServiceLegacyUnknownReleaseRequiresReview(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationSubmissionUnknown
	task.BillingState = VideoBillingManualReview
	tasks.task = task

	updated, err := svc.ResolveSubmissionUnknownNotCreated(context.Background(), task.PublicID)

	require.ErrorIs(t, err, ErrVideoReviewRequired)
	require.Nil(t, updated)
	require.Equal(t, VideoGenerationSubmissionUnknown, tasks.task.GenerationState)
	require.Empty(t, queue.enqueued)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceLegacyUnknownBindingRequiresReview(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_confirmed", Status: VideoGenerationQueued,
		RawStatus: "queued", SuggestedPollInterval: 10 * time.Second,
	}}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.ProviderTaskID = nil
	task.GenerationState = VideoGenerationSubmissionUnknown
	task.BillingState = VideoBillingManualReview
	tasks.task = task

	updated, err := svc.ResolveSubmissionUnknownCreated(context.Background(), task.PublicID, "video_confirmed")

	require.ErrorIs(t, err, ErrVideoReviewRequired)
	require.Nil(t, updated)
	require.Equal(t, VideoGenerationSubmissionUnknown, tasks.task.GenerationState)
	require.Zero(t, provider.getCalls)
	require.Zero(t, provider.createCalls)
	require.Empty(t, queue.enqueued)
}

func TestVideoTaskServiceLegacyUnknownCharacterRequiresReview(t *testing.T) {
	provider := &videoProviderStub{character: &ProviderVideoResource{
		ProviderResourceID: "char_confirmed", Status: "ready", Metadata: map[string]any{"name": "Mossy"},
	}}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.Operation = VideoOperationCharacterCreate
	task.ProviderTaskID = nil
	task.GenerationState = VideoGenerationSubmissionUnknown
	task.BillingState = VideoBillingManualReview
	billingUnit := VideoBillingUnitRequest
	task.BillingUnit = &billingUnit
	tasks.task = task

	updated, err := svc.ResolveSubmissionUnknownCreated(context.Background(), task.PublicID, "char_confirmed")

	require.ErrorIs(t, err, ErrVideoReviewRequired)
	require.Nil(t, updated)
	require.Equal(t, VideoGenerationSubmissionUnknown, tasks.task.GenerationState)
	require.Zero(t, provider.characterGets)
	require.Zero(t, provider.getCalls)
	require.Zero(t, provider.createCalls)
	require.Empty(t, queue.enqueued)
	resource := svc.resources.(*videoResourceRepoStub).bySource[task.ID]
	require.Nil(t, resource)
}

func TestVideoTaskServiceSubmitRejectedReleasesHold(t *testing.T) {
	providerErr := &VideoProviderError{Kind: "validation", Code: "bad_prompt", Message: "rejected", Certainty: VideoSubmissionRejected, StatusCode: 400}
	provider := &videoProviderStub{err: providerErr}
	svc, _, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())
	require.ErrorIs(t, err, providerErr)
	require.Equal(t, VideoGenerationFailed, result.Task.GenerationState)
	require.Equal(t, VideoBillingReleased, result.Task.BillingState)
	settlements := svc.settlements.(*videoSettlementRepoStub)
	require.Equal(t, BalanceSettlementRelease, settlements.settlement.Action)
	require.True(t, settlements.acked)
	require.Empty(t, queue.enqueued)
}

func TestVideoTaskServiceSubmitImmediateFailureReturnsDetailsAndReleasesHold(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_failed", Status: VideoGenerationFailed, RawStatus: "failed",
		ErrorCode: "content_policy", ErrorMessage: "video generation was rejected by content policy",
		Usage: map[string]any{"seconds": 3},
	}}
	svc, _, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)

	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.NoError(t, err)
	require.Equal(t, VideoGenerationFailed, result.Task.GenerationState)
	require.Equal(t, VideoBillingReleased, result.Task.BillingState)
	require.Equal(t, "content_policy", *result.Task.LastErrorCode)
	require.Equal(t, "video generation was rejected by content policy", *result.Task.LastErrorMessage)
	settlements := svc.settlements.(*videoSettlementRepoStub)
	require.Equal(t, BalanceSettlementRelease, settlements.settlement.Action)
	require.True(t, settlements.acked)
	require.Empty(t, queue.enqueued)
}

func TestVideoTaskServiceSubmitUnknownKeepsHoldAndDoesNotRetry(t *testing.T) {
	provider := &videoProviderStub{err: &VideoProviderError{Kind: "transport", Code: "timeout", Message: "timeout", Certainty: VideoSubmissionUnknown}}
	svc, _, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())
	require.NoError(t, err)
	require.Equal(t, VideoGenerationSubmissionUnknown, result.Task.GenerationState)
	require.Equal(t, VideoBillingHeld, result.Task.BillingState)
	require.Equal(t, 1, provider.createCalls)
	require.NotEmpty(t, queue.enqueued)
}

func TestVideoTaskServiceSubmitAcceptedErrorKeepsHoldForReconciliation(t *testing.T) {
	provider := &videoProviderStub{err: &VideoProviderError{
		Kind: "transport", Code: "response_lost", Message: "provider accepted before connection closed",
		Certainty: VideoSubmissionAccepted,
	}}
	svc, _, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)

	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.NoError(t, err)
	require.Equal(t, VideoGenerationSubmissionUnknown, result.Task.GenerationState)
	require.Equal(t, VideoBillingHeld, result.Task.BillingState)
	require.NotEmpty(t, queue.enqueued)
}

func TestVideoTaskServiceInvalidAcceptedProviderIDBecomesSubmissionUnknown(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: strings.Repeat("x", 256), Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	svc, _, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)

	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.NoError(t, err)
	require.Equal(t, VideoGenerationSubmissionUnknown, result.Task.GenerationState)
	require.Equal(t, VideoBillingHeld, result.Task.BillingState)
	require.NotEmpty(t, queue.enqueued)
}

func TestVideoTaskServiceSanitizesGenericProviderAcceptance(t *testing.T) {
	invalidProgress := math.NaN()
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: " video_upstream ", Status: "provider-private-state",
		RawStatus: strings.Repeat("x", 65), Progress: &invalidProgress,
		ContentVariants: []string{" video ", "video", "bad/value", strings.Repeat("x", 65), "thumbnail"},
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)

	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.NoError(t, err)
	require.Equal(t, VideoGenerationInProgress, result.Task.GenerationState)
	require.Equal(t, "video_upstream", tasks.accepted.ProviderTaskID)
	require.Equal(t, "unknown", tasks.accepted.ProviderStatus)
	require.Nil(t, tasks.accepted.Progress)
	require.Equal(t, []string{"video", "thumbnail"}, tasks.accepted.ContentVariants)
	require.Empty(t, tasks.accepted.ErrorKind)
	require.Empty(t, tasks.accepted.ErrorCode)
	require.Empty(t, tasks.accepted.ErrorMessage)
}

func TestVideoProviderRegistryRejectsUnsafeProviderName(t *testing.T) {
	unsafe := &videoProviderStub{name: "OpenAI unsafe"}
	registry := NewVideoProviderRegistry(unsafe)

	_, ok := registry.Get(unsafe.name)
	require.False(t, ok)
}

func TestVideoTaskServiceSanitizesRejectedProviderErrorBeforePersistence(t *testing.T) {
	provider := &videoProviderStub{err: &VideoProviderError{
		Kind: strings.Repeat("x", 33), Code: strings.Repeat("x", 129),
		Message: "Bearer provider-secret", Certainty: VideoSubmissionRejected,
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)

	_, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.Error(t, err)
	require.Len(t, tasks.transitions, 2)
	transition := tasks.transitions[1]
	require.Equal(t, "upstream", transition.ErrorKind)
	require.Equal(t, "upstream_error", transition.ErrorCode)
	require.Equal(t, "video provider rejected the submission", transition.ErrorMessage)
	require.NotContains(t, transition.ErrorMessage, "provider-secret")
}

func TestVideoTaskServiceReconcileSanitizesGenericProviderObservation(t *testing.T) {
	task := baseVideoWorkerTask()
	repo := &videoTaskRepoStub{task: task}
	svc := &VideoTaskService{tasks: repo, cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{PollIntervalSeconds: 10}}}, now: time.Now}
	invalidProgress := math.Inf(1)

	_, err := svc.ReconcileProviderObservation(context.Background(), task, &ProviderVideoTask{
		Status: VideoGenerationInProgress, RawStatus: strings.Repeat("x", 65), Progress: &invalidProgress,
		ErrorCode: strings.Repeat("x", 129), ErrorMessage: "Bearer provider-secret",
		ContentVariants: []string{"video", "video", "unsafe/value"},
	}, "provider_polled")

	require.NoError(t, err)
	require.Len(t, repo.transitions, 1)
	transition := repo.transitions[0]
	require.Equal(t, "unknown", transition.ProviderStatus)
	require.Nil(t, transition.Progress)
	require.Equal(t, "upstream_error", transition.ErrorCode)
	require.Equal(t, "video provider task failed", transition.ErrorMessage)
	require.Equal(t, []string{"video"}, transition.ContentVariants)
	require.Equal(t, map[string]any{"raw_status": "unknown"}, transition.EventPayload)
}

func TestVideoTaskServiceReconcileUsesFixedPollInterval(t *testing.T) {
	task := baseVideoWorkerTask()
	task.PollAttempts = 99
	repo := &videoTaskRepoStub{task: task}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	svc := &VideoTaskService{
		tasks: repo,
		cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
			PollIntervalSeconds: 7,
		}}},
		now: func() time.Time { return now },
	}

	updated, err := svc.ReconcileProviderObservation(context.Background(), task, &ProviderVideoTask{
		ProviderTaskID:        *task.ProviderTaskID,
		Status:                VideoGenerationInProgress,
		RawStatus:             "processing",
		SuggestedPollInterval: 120 * time.Second,
	}, "provider_polled")

	require.NoError(t, err)
	require.Equal(t, 100, updated.PollAttempts)
	require.NotNil(t, updated.NextActionAt)
	require.Equal(t, now.Add(7*time.Second), *updated.NextActionAt)
}

func TestVideoTaskServicePreservesProviderFailureAndReleasesHold(t *testing.T) {
	task := baseVideoWorkerTask()
	repo := &videoTaskRepoStub{task: task}
	svc := &VideoTaskService{tasks: repo, cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{PollIntervalSeconds: 10}}}, now: time.Now}

	_, err := svc.ReconcileProviderObservation(context.Background(), task, &ProviderVideoTask{
		Status: VideoGenerationFailed, RawStatus: "failed", ErrorCode: "content_policy",
		ErrorMessage: "video generation was rejected by content policy", Usage: map[string]any{"seconds": 3},
	}, "provider_polled")

	require.NoError(t, err)
	transition := repo.transitions[0]
	require.Equal(t, VideoBillingReleasePending, transition.BillingState)
	require.Equal(t, 0.0, *transition.ActualCost)
	require.Equal(t, "upstream", transition.ErrorKind)
	require.Equal(t, "content_policy", transition.ErrorCode)
	require.Equal(t, "video generation was rejected by content policy", transition.ErrorMessage)
}

func TestVideoTaskServiceReconcileFailedIgnoresProviderUsageAndReleases(t *testing.T) {
	task := baseVideoWorkerTask()
	providerFinishedAt := time.Now().UTC().Add(-time.Minute)
	repo := &videoTaskRepoStub{task: task}
	svc := &VideoTaskService{tasks: repo, cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{PollIntervalSeconds: 10}}}, now: time.Now}

	updated, err := svc.ReconcileProviderObservation(context.Background(), task, &ProviderVideoTask{
		ProviderTaskID: *task.ProviderTaskID, Status: VideoGenerationFailed, RawStatus: "failed",
		Usage: map[string]any{"seconds": 3}, ProviderFinishedAt: &providerFinishedAt,
	}, "provider_polled")

	require.NoError(t, err)
	require.Equal(t, VideoGenerationFailed, updated.GenerationState)
	require.Equal(t, VideoBillingReleasePending, updated.BillingState)
	require.Zero(t, *updated.ActualUnits)
	require.Zero(t, *updated.ActualCost)
	require.Equal(t, providerFinishedAt, *updated.ProviderFinishedAt)
	require.Len(t, repo.transitions, 1)
}

func TestVideoTaskServiceReconcileFailedWithoutUsageReleasesWithoutReview(t *testing.T) {
	task := baseVideoWorkerTask()
	repo := &videoTaskRepoStub{task: task}
	svc := &VideoTaskService{tasks: repo, cfg: &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{PollIntervalSeconds: 10}}}, now: time.Now}

	updated, err := svc.ReconcileProviderObservation(context.Background(), task, &ProviderVideoTask{
		ProviderTaskID: *task.ProviderTaskID, Status: VideoGenerationFailed, RawStatus: "failed",
	}, "provider_polled")

	require.NoError(t, err)
	require.Equal(t, VideoBillingReleasePending, updated.BillingState)
	require.Nil(t, updated.LastErrorCode)
	require.Len(t, repo.transitions, 1)
}

func TestVideoTaskServiceRejectsSubscriptionBeforeProviderCall(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeSubscription), nil)
	_, err := svc.Submit(context.Background(), videoSubmitRequestForTest())
	require.ErrorIs(t, err, ErrVideoSubscriptionUnsupported)
	require.Zero(t, provider.createCalls)
	require.Nil(t, tasks.task)
}

func TestVideoTaskServiceRejectsPrivateCallbackBeforeHoldAndProviderCall(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "never"}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	svc.cfg.Gateway.Video.Callback = config.GatewayVideoCallbackConfig{
		Enabled: true, RetryHours: 24, RequestTimeoutSeconds: 5, SigningSecret: "secret",
	}
	request := videoSubmitRequestForTest()
	request.CallbackURL = "https://127.0.0.1/private"

	_, err := svc.Submit(context.Background(), request)

	require.ErrorIs(t, err, ErrVideoInvalidRequest)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceRejectsCallbackWhenDeliveryIsDisabled(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "never"}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	request.CallbackURL = "https://example.com/callback"

	_, err := svc.Submit(context.Background(), request)

	require.ErrorIs(t, err, ErrVideoCallbacksDisabled)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceRejectsUnsupportedProviderBinaryRoleBeforeHold(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	request.Operation = VideoOperationGenerate
	request.Inputs = []VideoInput{{VideoInputManifestEntry: VideoInputManifestEntry{
		Role: VideoInputRole("depth_map"), MIMEType: "application/octet-stream", Size: 4, SHA256: "abcd",
	}}}

	_, err := svc.Submit(context.Background(), request)

	require.ErrorIs(t, err, ErrVideoInputUnsupported)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceRunsProviderSpecificValidationBeforeHold(t *testing.T) {
	provider := &videoProviderStub{validationErr: ErrVideoInvalidRequest}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)

	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.ErrorIs(t, err, ErrVideoInvalidRequest)
	require.Nil(t, result)
	require.Equal(t, 1, provider.validationCalls)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceResourceDisclosureUsesMostRestrictivePolicy(t *testing.T) {
	provider := &videoProviderStub{}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.VideoDisclosurePolicy = config.VideoDisclosureNone
	resourceID := "char_cccccccccccccccccccccccccccccccc"
	resource := &VideoResource{
		ID: 19, PublicID: resourceID, UserID: 42, GroupID: &group.ID,
		Provider: VideoProviderOpenAI, AccountID: 11, ProviderResourceID: "char_upstream", Status: "ready",
	}
	svc, _, _ := newVideoTaskServiceForTest(provider, group, map[string]*VideoResource{resourceID: resource})
	svc.cfg.Gateway.Video.DisclosurePolicy = config.VideoDisclosureDedicatedCredentials
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].VideoDisclosurePolicy = config.VideoDisclosureDedicatedCredentials

	disclosure, err := svc.ResourceDisclosureForOwner(context.Background(), resource.UserID, resource.PublicID)

	require.NoError(t, err)
	require.Equal(t, config.VideoDisclosureNone, disclosure.Policy)
	require.Empty(t, disclosure.Provider)
	require.Empty(t, disclosure.ProviderResourceID)
}

func TestVideoTaskServiceResourceIdentityDisclosureIsAudited(t *testing.T) {
	provider := &videoProviderStub{}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.VideoDisclosurePolicy = config.VideoDisclosureIdentity
	sourceTaskID := int64(21)
	resourceID := "char_abababababababababababababababab"
	resource := &VideoResource{
		ID: 19, PublicID: resourceID, UserID: 42, GroupID: &group.ID, SourceTaskID: &sourceTaskID,
		Provider: VideoProviderOpenAI, AccountID: 11, ProviderResourceID: "char_upstream", Status: "ready",
	}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, map[string]*VideoResource{resourceID: resource})
	svc.cfg.Gateway.Video.DisclosurePolicy = config.VideoDisclosureIdentity

	disclosure, err := svc.ResourceDisclosureForOwner(context.Background(), resource.UserID, resource.PublicID)

	require.NoError(t, err)
	require.Equal(t, "char_upstream", disclosure.ProviderResourceID)
	require.Len(t, tasks.events, 1)
	require.Equal(t, "provider_resource_identity_disclosed", tasks.events[0].EventType)
	require.Equal(t, &sourceTaskID, tasks.events[0].TaskID)
}

func TestVideoProviderAcceptanceFiltersUncontrolledMetadataAndAccess(t *testing.T) {
	provider := &videoProviderStub{}
	svc, _, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	expiresAt := svc.now().UTC().Add(time.Hour)
	providerTask := &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued,
		Metadata: map[string]any{
			"model": "sora-2", "seconds": "8", "signed_url": "https://example.test/video?token=secret",
			"unknown": map[string]any{"api_key": "sk-provider-secret"},
		},
		Usage:  map[string]any{"video_tokens": 123, "api_key": 456, "nested": map[string]any{"secret": "value"}},
		Access: &ProviderTaskAccess{Kind: "api_key", Scope: "provider_account", Value: "sk-provider-secret", ExpiresAt: &expiresAt},
	}

	acceptance, err := svc.videoProviderAcceptance(providerTask)

	require.NoError(t, err)
	require.Equal(t, map[string]any{"model": "sora-2", "seconds": "8"}, acceptance.ResponseMetadata)
	require.Equal(t, map[string]any{"video_tokens": float64(123)}, acceptance.UsageSnapshot)
	require.Empty(t, acceptance.ProviderAccessEnc)
	raw, marshalErr := json.Marshal(acceptance)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(raw), "sk-provider-secret")
	require.NotContains(t, string(raw), "token=secret")
}

func TestVideoProviderAcceptanceEncryptsCompleteVideoURL(t *testing.T) {
	provider := &videoProviderStub{}
	svc, _, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	providerURL := "https://cdn.example.test/v1/videos/video_upstream/content?token=signed&disposition=inline"

	acceptance, err := svc.videoProviderAcceptance(&ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationCompleted, VideoURL: providerURL,
	})

	require.NoError(t, err)
	require.Equal(t, "enc:"+providerURL, acceptance.ProviderVideoURLEnc)
	expectedProxyKey, err := providerVideoProxyKey(providerURL)
	require.NoError(t, err)
	require.Equal(t, expectedProxyKey, acceptance.ProviderVideoProxyKey)
	require.NotContains(t, acceptance.ResponseMetadata, "video_url")
}

func TestVideoTaskServiceDisclosureDecryptsOnlyScopedExpiringAccessAndAuditsMetadata(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	svc.cfg.Gateway.Video.DisclosurePolicy = config.VideoDisclosureTaskAccess
	expiresAt := svc.now().UTC().Add(time.Hour)
	kind, scope, encrypted, providerID := "signed_url", "video_content", "enc:https://download.example/signed?secret=1", "video_upstream"
	accountID := int64(11)
	task := &VideoTask{
		ID: 31, PublicID: "video_dddddddddddddddddddddddddddddddd", UserID: 42,
		Provider: VideoProviderOpenAI, ProviderTaskID: &providerID, AccountID: &accountID,
		ProviderAccessKind: &kind, ProviderAccessScope: &scope,
		ProviderAccessEnc: &encrypted, ProviderAccessExpires: &expiresAt,
	}
	tasks.sources[task.PublicID] = task

	disclosure, err := svc.DisclosureForOwner(context.Background(), task.UserID, task.PublicID)

	require.NoError(t, err)
	require.Equal(t, providerID, disclosure.ProviderTaskID)
	require.NotNil(t, disclosure.Access)
	require.Equal(t, "https://download.example/signed?secret=1", disclosure.Access.Value)
	require.Len(t, tasks.events, 1)
	raw, err := json.Marshal(tasks.events[0].Payload)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret=1")
	require.Equal(t, "provider_access_disclosed", tasks.events[0].EventType)
}

func TestVideoTaskServiceDisclosureReturnsDedicatedCredentialOnlyToVerifiedOwner(t *testing.T) {
	provider := &videoProviderStub{}
	group := videoGroupForTest(SubscriptionTypeStandard)
	group.VideoDisclosurePolicy = config.VideoDisclosureDedicatedCredentials
	svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
	svc.cfg.Gateway.Video.DisclosurePolicy = config.VideoDisclosureDedicatedCredentials
	ownerID := int64(42)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts[0].VideoOwnerUserID = &ownerID
	accounts.accounts[0].OwnershipMode = AccountOwnershipUserDedicated
	accounts.accounts[0].OwnerUserID = &ownerID
	accounts.accounts[0].IsolationState = AccountIsolationVerified
	accounts.accounts[0].ProviderIdentityVersion = 3
	accounts.accounts[0].IsolationVerifiedVersion = 3
	bindingID := int64(9)
	accounts.accounts[0].ProviderPrincipalBindingID = &bindingID
	accounts.accounts[0].VideoDisclosurePolicy = config.VideoDisclosureDedicatedCredentials
	authorizer := &videoDisclosureAccountRepoStub{videoAccountRepoStub: accounts, allowed: true}
	svc.accounts = authorizer

	providerID := "video_upstream"
	accountID := int64(11)
	apiKeyID := int64(3)
	task := &VideoTask{
		ID: 31, PublicID: "video_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", UserID: ownerID,
		APIKeyID: &apiKeyID, GroupID: &group.ID, AccountID: &accountID,
		AccountOwnerUserID: &ownerID, Provider: VideoProviderOpenAI, ProviderTaskID: &providerID,
		RequestAttributes: map[string]any{"account_identity_version": 3},
	}
	tasks.sources[task.PublicID] = task

	disclosure, err := svc.DisclosureForOwner(context.Background(), ownerID, task.PublicID)

	require.NoError(t, err)
	require.Equal(t, config.VideoDisclosureDedicatedCredentials, disclosure.Policy)
	require.Equal(t, providerID, disclosure.ProviderTaskID)
	require.NotNil(t, disclosure.Access)
	require.Equal(t, "api_key", disclosure.Access.Kind)
	require.Equal(t, "provider_account", disclosure.Access.Scope)
	require.Equal(t, "sk-test", disclosure.Access.Value)
	require.Len(t, tasks.events, 1)
	require.Equal(t, ownerID, tasks.events[0].Payload["user_id"])
	require.Equal(t, &apiKeyID, tasks.events[0].Payload["api_key_id"])
	raw, marshalErr := json.Marshal(tasks.events[0])
	require.NoError(t, marshalErr)
	require.NotContains(t, string(raw), "sk-test")

	for _, failure := range []struct {
		name    string
		allowed bool
		err     error
		missing bool
	}{
		{name: "alias created after verification"},
		{name: "authorization database unavailable", allowed: true, err: errors.New("database unavailable")},
		{name: "authorization repository missing", missing: true},
	} {
		t.Run(failure.name, func(t *testing.T) {
			authorizer.allowed, authorizer.authorizationErr = failure.allowed, failure.err
			svc.accounts = authorizer
			if failure.missing {
				svc.accounts = accounts
			}
			disclosure, err := svc.DisclosureForOwner(context.Background(), ownerID, task.PublicID)
			require.NoError(t, err)
			require.Nil(t, disclosure.Access)
			require.Equal(t, providerID, disclosure.ProviderTaskID)
			require.Equal(t, "provider_identity_disclosed", tasks.events[len(tasks.events)-1].EventType)
		})
	}
}

func TestVideoTaskServiceDisclosureNeverReturnsSharedOrMismatchedCredential(t *testing.T) {
	tests := []struct {
		name             string
		accountOwner     *int64
		taskAccountOwner *int64
	}{
		{name: "shared account has no owner"},
		{name: "current account owner differs", accountOwner: videoInt64Ptr(99), taskAccountOwner: videoInt64Ptr(42)},
		{name: "task owner snapshot differs", accountOwner: videoInt64Ptr(42), taskAccountOwner: videoInt64Ptr(99)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &videoProviderStub{}
			group := videoGroupForTest(SubscriptionTypeStandard)
			group.VideoDisclosurePolicy = config.VideoDisclosureDedicatedCredentials
			svc, tasks, _ := newVideoTaskServiceForTest(provider, group, nil)
			svc.cfg.Gateway.Video.DisclosurePolicy = config.VideoDisclosureDedicatedCredentials
			accounts := svc.accounts.(*videoAccountRepoStub)
			accounts.accounts[0].VideoOwnerUserID = tt.accountOwner
			accounts.accounts[0].VideoDisclosurePolicy = config.VideoDisclosureDedicatedCredentials

			providerID := "video_upstream"
			accountID := int64(11)
			groupID := int64(7)
			task := &VideoTask{
				ID: 32, PublicID: "video_ffffffffffffffffffffffffffffffff", UserID: 42,
				GroupID: &groupID, AccountID: &accountID, AccountOwnerUserID: tt.taskAccountOwner,
				Provider: VideoProviderOpenAI, ProviderTaskID: &providerID,
			}
			tasks.sources[task.PublicID] = task

			disclosure, err := svc.DisclosureForOwner(context.Background(), task.UserID, task.PublicID)

			require.NoError(t, err)
			require.Nil(t, disclosure.Access)
			require.Len(t, tasks.events, 1)
			require.Equal(t, "provider_identity_disclosed", tasks.events[0].EventType)
		})
	}
}

func TestVideoTaskServiceSkipsDedicatedAccountOwnedByAnotherUser(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	otherOwnerID := int64(99)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts = []Account{
		{
			ID: 10, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 1,
			Status: StatusActive, Schedulable: true, Concurrency: 4,
			VideoOwnerUserID: &otherOwnerID, Credentials: map[string]any{"api_key": "sk-other"},
		},
		{
			ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 2,
			Status: StatusActive, Schedulable: true, Concurrency: 4,
			Credentials: map[string]any{"api_key": "sk-shared"},
		},
	}

	_, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.NoError(t, err)
	require.Equal(t, int64(11), tasks.create.AccountID)
	require.Equal(t, 1, provider.createCalls)
}

func TestVideoTaskServiceUsesOwnedAccountForProviderFileReference(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	ownerID := int64(42)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts = []Account{
		{
			ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 1,
			Status: StatusActive, Schedulable: true, Concurrency: 4,
			Credentials: map[string]any{"api_key": "sk-shared"},
		},
		{
			ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 2,
			Status: StatusActive, Schedulable: true, Concurrency: 4, VideoOwnerUserID: &ownerID,
			OwnershipMode: AccountOwnershipUserDedicated, OwnerUserID: &ownerID,
			IsolationState: AccountIsolationVerified, ProviderIdentityVersion: 1, IsolationVerifiedVersion: 1,
			ProviderPrincipalBindingID: videoInt64Ptr(9),
			Credentials:                map[string]any{"api_key": "sk-owned"},
		},
	}
	request := videoSubmitRequestForTest()
	request.InputReference = &ProviderInputReference{FileID: "file_owned_reference"}

	_, err := svc.Submit(context.Background(), request)

	require.NoError(t, err)
	require.Equal(t, int64(12), tasks.create.AccountID)
	require.Equal(t, []int64{12}, provider.createAccountIDs)
}

func TestVideoTaskServiceRejectsProviderFileReferenceWithoutOwnedAccount(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	request := videoSubmitRequestForTest()
	request.InputReference = &ProviderInputReference{FileID: "file_unscoped_reference"}

	result, err := svc.Submit(context.Background(), request)

	require.ErrorIs(t, err, ErrVideoNoAccountAvailable)
	require.Nil(t, result)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceRejectsProviderFileReferenceWithUnverifiedOwner(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	ownerID := int64(42)
	account := &svc.accounts.(*videoAccountRepoStub).accounts[0]
	account.VideoOwnerUserID, account.OwnerUserID = &ownerID, &ownerID
	account.OwnershipMode = AccountOwnershipUserDedicated
	account.IsolationState = AccountIsolationUnverified
	request := videoSubmitRequestForTest()
	request.InputReference = &ProviderInputReference{FileID: "file_unverified_scope"}
	_, err := svc.Submit(context.Background(), request)
	require.ErrorIs(t, err, ErrVideoNoAccountAvailable)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceRetriesAnotherAccountBeforeProviderSubmissionWhenConcurrencyIsFull(t *testing.T) {
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts = []Account{
		{
			ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 1,
			Status: StatusActive, Schedulable: true, Concurrency: 1,
			Credentials: map[string]any{"api_key": "sk-first"},
		},
		{
			ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 2,
			Status: StatusActive, Schedulable: true, Concurrency: 1,
			Credentials: map[string]any{"api_key": "sk-second"},
		},
	}
	tasks.createErrors = map[int64]error{11: ErrVideoAccountConcurrencyLimited}

	result, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, int64(12), tasks.create.AccountID)
	require.Equal(t, []int64{12}, provider.createAccountIDs)
}

func TestVideoTaskServiceRejectsWhenOnlyAnotherUsersDedicatedAccountExists(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	otherOwnerID := int64(99)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts = []Account{{
		ID: 10, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 4,
		VideoOwnerUserID: &otherOwnerID, Credentials: map[string]any{"api_key": "sk-other"},
	}}

	_, err := svc.Submit(context.Background(), videoSubmitRequestForTest())

	require.ErrorIs(t, err, ErrVideoNoAccountAvailable)
	require.Empty(t, tasks.create.PublicID)
	require.Zero(t, provider.createCalls)
}

func videoInt64Ptr(value int64) *int64 { return &value }

func TestVideoTaskServiceResolvesCharacterAccountAffinity(t *testing.T) {
	characterID := "char_0123456789abcdef0123456789abcdef"
	provider := &videoProviderStub{result: &ProviderVideoTask{ProviderTaskID: "video_upstream", Status: VideoGenerationQueued, RawStatus: "queued"}}
	resources := map[string]*VideoResource{
		characterID: {PublicID: characterID, UserID: 42, Provider: VideoProviderOpenAI, AccountID: 11, ProviderResourceID: "char_upstream", Status: "ready"},
	}
	svc, _, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), resources)
	request := videoSubmitRequestForTest()
	request.CharacterIDs = []string{characterID}
	resolved, err := svc.resolveSubmission(context.Background(), request, nil)
	require.NoError(t, err)
	require.Len(t, resolved.providerRequest.Characters, 1)
	require.Equal(t, "char_upstream", resolved.providerRequest.Characters[0].ProviderResourceID)
	require.Equal(t, int64(11), resolved.providerRequest.Characters[0].AccountID)
}

func TestVideoTaskServiceRejectsCharacterReferenceBeforeAccountConcurrency(t *testing.T) {
	characterID := "char_0123456789abcdef0123456789abcdef"
	provider := &videoProviderStub{result: &ProviderVideoTask{
		ProviderTaskID: "video_upstream", Status: VideoGenerationQueued, RawStatus: "queued",
	}}
	resources := map[string]*VideoResource{
		characterID: {
			PublicID: characterID, UserID: 42, Provider: VideoProviderOpenAI,
			AccountID: 11, ProviderResourceID: "char_upstream", Status: "ready",
		},
	}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), resources)
	accounts := svc.accounts.(*videoAccountRepoStub)
	accounts.accounts = append(accounts.accounts, Account{
		ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 2,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-second"},
	})
	tasks.createErrors = map[int64]error{11: ErrVideoAccountConcurrencyLimited}
	request := videoSubmitRequestForTest()
	request.CharacterIDs = []string{characterID}

	result, err := svc.Submit(context.Background(), request)

	require.ErrorIs(t, err, ErrVideoAccountConcurrencyLimited)
	require.Nil(t, result)
	require.Equal(t, int64(11), tasks.create.AccountID)
	require.Zero(t, provider.createCalls)
}

func TestVideoTaskServiceOwnerQueriesPreserveScopeAndFilters(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	tasks.sources[task.PublicID] = task
	tasks.listPage = &VideoTaskPage{Data: []*VideoTask{task}, HasMore: true, After: "next-cursor"}

	owned, err := svc.GetForOwner(context.Background(), task.UserID, task.PublicID)
	require.NoError(t, err)
	require.Same(t, task, owned)
	_, err = svc.GetForOwner(context.Background(), task.UserID+1, task.PublicID)
	require.ErrorIs(t, err, ErrVideoTaskNotFound)

	filter := VideoTaskFilter{Status: VideoGenerationQueued, Model: OpenAIVideoModelSora2, Operation: VideoOperationGenerate, Limit: 5, After: "cursor", Order: "asc"}
	page, err := svc.ListForOwner(context.Background(), task.UserID, filter)
	require.NoError(t, err)
	require.Equal(t, task.UserID, tasks.listUserID)
	require.Equal(t, filter, tasks.listFilter)
	require.Same(t, tasks.listPage, page)
}

func TestVideoTaskServiceGetForOwnerWakesPendingCompletedSettlement(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCapturePending
	tasks.sources[task.PublicID] = task

	result, err := svc.GetForOwner(context.Background(), task.UserID, task.PublicID)

	require.ErrorIs(t, err, ErrVideoSettlementPending)
	require.Nil(t, result)
	require.Equal(t, []string{task.PublicID}, queue.enqueued)
}

func TestVideoTaskServiceGetForOwnerWakesFailedReleaseRecovery(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationFailed
	task.BillingState = VideoBillingReleasePending
	tasks.sources[task.PublicID] = task

	loaded, err := svc.GetForOwner(context.Background(), task.UserID, task.PublicID)

	require.NoError(t, err)
	require.Same(t, task, loaded)
	require.Contains(t, queue.enqueued, task.PublicID)
}

func TestVideoTaskServiceGetForOwnerWakesOnlyDueGenerationPoll(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	now := time.Now().UTC()
	svc.now = func() time.Time { return now }
	task := baseVideoWorkerTask()
	tasks.sources[task.PublicID] = task

	task.NextActionAt = timePointer(now.Add(time.Minute))
	result, err := svc.GetForOwner(context.Background(), task.UserID, task.PublicID)
	require.NoError(t, err)
	require.Same(t, task, result)
	require.Empty(t, queue.enqueued)

	task.NextActionAt = timePointer(now)
	result, err = svc.GetForOwner(context.Background(), task.UserID, task.PublicID)
	require.NoError(t, err)
	require.Same(t, task, result)
	require.Equal(t, []string{task.PublicID}, queue.enqueued)
}

func TestVideoTaskServiceOpenContentRequiresEligibleOwnedTaskAndPreservesAffinity(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	svc.cfg.Gateway.Video.ContentProxy.Enabled = true
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCaptured
	tasks.task = task
	tasks.sources[task.PublicID] = task

	request := ProviderContentRequest{Variant: "thumbnail", Method: http.MethodGet, Range: "bytes=0-99", IfRange: `"etag"`}
	content, err := svc.OpenContentForOwner(context.Background(), task.UserID, task.PublicID, request)
	require.NoError(t, err)
	require.NotNil(t, content)
	require.NoError(t, content.Body.Close())
	require.Equal(t, 1, provider.contentCalls)
	require.Equal(t, request.Variant, provider.contentReq.Variant)
	require.Equal(t, request.Method, provider.contentReq.Method)
	require.Equal(t, request.Range, provider.contentReq.Range)
	require.Equal(t, request.IfRange, provider.contentReq.IfRange)
	require.Equal(t, task.Provider, provider.contentReq.TaskRef.Provider)
	require.Equal(t, *task.AccountID, provider.contentReq.TaskRef.AccountID)
	require.Equal(t, *task.ProviderTaskID, provider.contentReq.TaskRef.ProviderTaskID)

	_, err = svc.OpenContentForOwner(context.Background(), task.UserID+1, task.PublicID, request)
	require.ErrorIs(t, err, ErrVideoTaskNotFound)
	require.Equal(t, 1, provider.contentCalls)
}

func TestVideoTaskServiceOpenContentResolvesRewrittenProviderPathAndOriginalURL(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	svc.cfg.Gateway.Video.ContentProxy.Enabled = true
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCaptured
	providerURL := "https://cdn.example.test/assets/render/final.mp4?token=signed&disposition=inline"
	encrypted := "enc:" + providerURL
	task.ProviderVideoURLEnc = &encrypted
	proxyKey, err := providerVideoProxyKey(providerURL)
	require.NoError(t, err)
	task.ProviderVideoProxyKey = &proxyKey
	tasks.task = task
	tasks.sources[task.PublicID] = task

	content, err := svc.OpenContentForOwner(context.Background(), task.UserID, *task.ProviderTaskID, ProviderContentRequest{
		Method: http.MethodGet, Range: "bytes=0-10",
	})

	require.NoError(t, err)
	require.NotNil(t, content)
	require.NoError(t, content.Body.Close())
	require.Equal(t, providerURL, provider.contentReq.UpstreamURL)
	require.Equal(t, *task.ProviderTaskID, provider.contentReq.TaskRef.ProviderTaskID)

	publicURL, err := svc.VideoURLForOwner(context.Background(), task.UserID, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, providerURL, publicURL)

	byURL, err := svc.GetContentTaskByURLForOwner(context.Background(), task.UserID, "/assets/render/final.mp4?token=signed&disposition=inline")
	require.NoError(t, err)
	require.Same(t, task, byURL)

	publicByURL, err := svc.GetContentTaskByURL(context.Background(), "/assets/render/final.mp4?token=signed&disposition=inline")
	require.NoError(t, err)
	require.Same(t, task, publicByURL)
}

func TestVideoTaskServiceOpenContentRejectsInvalidProviderStatusAndClosesBody(t *testing.T) {
	body := &closeTrackingVideoContentBody{Reader: strings.NewReader("redirect")}
	provider := &videoProviderStub{content: &ProviderContent{StatusCode: http.StatusFound, Body: body}}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	svc.cfg.Gateway.Video.ContentProxy.Enabled = true
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationCompleted
	task.BillingState = VideoBillingCaptured
	tasks.task = task
	tasks.sources[task.PublicID] = task

	content, err := svc.OpenContentForOwner(context.Background(), task.UserID, task.PublicID, ProviderContentRequest{})

	require.Nil(t, content)
	require.Error(t, err)
	require.True(t, body.closed)
}

func TestVideoTaskServiceOpenContentRejectsIneligibleStatesBeforeProvider(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*VideoTask, time.Time)
		want   error
	}{
		{
			name: "completed settlement pending",
			mutate: func(task *VideoTask, _ time.Time) {
				task.GenerationState = VideoGenerationCompleted
				task.BillingState = VideoBillingCapturePending
			},
			want: ErrVideoSettlementPending,
		},
		{
			name: "generation still queued",
			mutate: func(task *VideoTask, _ time.Time) {
				task.GenerationState = VideoGenerationQueued
				task.BillingState = VideoBillingHeld
			},
			want: ErrVideoContentNotReady,
		},
		{
			name: "deleted tombstone",
			mutate: func(task *VideoTask, _ time.Time) {
				task.GenerationState = VideoGenerationCompleted
				task.BillingState = VideoBillingCaptured
				task.DeleteState = VideoDeleteDeleted
			},
			want: ErrVideoTaskNotFound,
		},
		{
			name: "provider content expired",
			mutate: func(task *VideoTask, now time.Time) {
				task.GenerationState = VideoGenerationCompleted
				task.BillingState = VideoBillingCaptured
				task.ContentExpiresAt = timePointer(now.Add(-time.Second))
			},
			want: ErrVideoContentExpired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &videoProviderStub{}
			svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
			svc.cfg.Gateway.Video.ContentProxy.Enabled = true
			task := baseVideoWorkerTask()
			tt.mutate(task, svc.now())
			tasks.task = task
			tasks.sources[task.PublicID] = task

			_, err := svc.OpenContentForOwner(context.Background(), task.UserID, task.PublicID, ProviderContentRequest{Method: http.MethodGet})

			require.ErrorIs(t, err, tt.want)
			require.Zero(t, provider.contentCalls)
		})
	}
}

func TestVideoTaskServiceDeletePreSubmitReleasesHoldWithoutProviderCall(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.ProviderTaskID = nil
	task.GenerationState = VideoGenerationHeld
	task.BillingState = VideoBillingHeld
	tasks.task = task
	tasks.sources[task.PublicID] = task

	deleted, err := svc.DeleteForOwner(context.Background(), task.UserID, task.PublicID)

	require.NoError(t, err)
	require.Equal(t, VideoGenerationCancelled, deleted.GenerationState)
	require.Equal(t, VideoBillingReleasePending, deleted.BillingState)
	require.Equal(t, VideoDeleteDeleted, deleted.DeleteState)
	require.Zero(t, provider.deleteCalls)
	require.NotEmpty(t, queue.enqueued)
	require.Len(t, tasks.transitions, 1)
	require.Equal(t, "delete_pre_submit", tasks.transitions[0].EventType)
}

func TestVideoTaskServiceDeleteGuardsUnknownAndUnsettledCompletion(t *testing.T) {
	tests := []struct {
		name       string
		generation string
		billing    string
		want       error
	}{
		{name: "submitting", generation: VideoGenerationSubmitting, billing: VideoBillingHeld, want: ErrVideoDeleteConflict},
		{name: "submission unknown", generation: VideoGenerationSubmissionUnknown, billing: VideoBillingHeld, want: ErrVideoDeleteConflict},
		{name: "queued", generation: VideoGenerationQueued, billing: VideoBillingHeld, want: ErrVideoDeleteConflict},
		{name: "in progress", generation: VideoGenerationInProgress, billing: VideoBillingHeld, want: ErrVideoDeleteConflict},
		{name: "completed settlement pending", generation: VideoGenerationCompleted, billing: VideoBillingCapturePending, want: ErrVideoSettlementPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &videoProviderStub{}
			svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
			task := baseVideoWorkerTask()
			task.GenerationState = tt.generation
			task.BillingState = tt.billing
			tasks.task = task
			tasks.sources[task.PublicID] = task

			_, err := svc.DeleteForOwner(context.Background(), task.UserID, task.PublicID)

			require.ErrorIs(t, err, tt.want)
			require.Zero(t, provider.deleteCalls)
			require.Empty(t, tasks.transitions)
		})
	}
}

func TestVideoTaskServiceDeleteAcceptedTaskRejectsWithoutCancelContract(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	tasks.task = task
	tasks.sources[task.PublicID] = task

	deleted, err := svc.DeleteForOwner(context.Background(), task.UserID, task.PublicID)

	require.ErrorIs(t, err, ErrVideoDeleteConflict)
	require.Nil(t, deleted)
	require.Zero(t, provider.deleteCalls)
	require.Equal(t, VideoGenerationQueued, task.GenerationState)
	require.Equal(t, VideoBillingHeld, task.BillingState)
	require.Equal(t, VideoDeleteNone, task.DeleteState)
}

func TestVideoTaskServiceDeleteFailedHeldTaskReleasesWithoutBillingReview(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationFailed
	tasks.task = task
	tasks.sources[task.PublicID] = task

	deleted, err := svc.DeleteForOwner(context.Background(), task.UserID, task.PublicID)

	require.NoError(t, err)
	require.NotNil(t, deleted)
	require.Zero(t, provider.deleteCalls)
	require.Equal(t, VideoBillingReleased, task.BillingState)
	require.Equal(t, VideoDeleteRequested, task.DeleteState)
	require.NotEmpty(t, queue.enqueued)
}

func TestVideoTaskServiceDeleteSettledFailedTaskOnlyEnqueuesIntent(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.GenerationState = VideoGenerationFailed
	task.BillingState = VideoBillingReleased
	tasks.task = task
	tasks.sources[task.PublicID] = task

	deleted, err := svc.DeleteForOwner(context.Background(), task.UserID, task.PublicID)

	require.NoError(t, err)
	require.Zero(t, provider.deleteCalls)
	require.Equal(t, VideoDeleteRequested, deleted.DeleteState)
	leaseCtx := WithVideoTaskLease(context.Background(), VideoTaskLease{TaskID: task.ID, Owner: "delete-worker", Epoch: 1, Version: task.Version})
	deleted, err = svc.RetryDeleteTask(leaseCtx, deleted)
	require.NoError(t, err)
	require.Equal(t, 1, provider.deleteCalls)
	require.Equal(t, VideoBillingReleased, deleted.BillingState)
	require.Equal(t, VideoDeleteDeleted, deleted.DeleteState)
}

func TestVideoTaskServiceDeleteFailureIsRetryableWithoutChangingBilling(t *testing.T) {
	deleteErr := errors.New("temporary delete failure")
	provider := &videoProviderStub{deleteErr: deleteErr}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.GenerationState, task.BillingState, task.DeleteState = VideoGenerationCompleted, VideoBillingCaptured, VideoDeleteRequested
	tasks.task = task
	tasks.sources[task.PublicID] = task

	leaseCtx := WithVideoTaskLease(context.Background(), VideoTaskLease{TaskID: task.ID, Owner: "delete-worker", Epoch: 1, Version: task.Version})
	_, err := svc.RetryDeleteTask(leaseCtx, task)

	require.ErrorIs(t, err, deleteErr)
	require.Equal(t, 1, provider.deleteCalls)
	require.Equal(t, VideoGenerationCompleted, task.GenerationState)
	require.Equal(t, VideoBillingCaptured, task.BillingState)
	require.Equal(t, VideoDeleteFailed, task.DeleteState)
	require.NotNil(t, task.NextActionAt)
}

func TestVideoTaskServiceDeleteIsIdempotentAfterTombstone(t *testing.T) {
	provider := &videoProviderStub{}
	svc, tasks, _ := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
	task := baseVideoWorkerTask()
	task.DeleteState = VideoDeleteDeleted
	tasks.task = task
	tasks.sources[task.PublicID] = task

	deleted, err := svc.DeleteForOwner(context.Background(), task.UserID, task.PublicID)

	require.NoError(t, err)
	require.Same(t, task, deleted)
	require.Zero(t, provider.deleteCalls)
	require.Empty(t, tasks.transitions)
}

func TestVideoTaskServiceRepeatedDeleteDoesNotInvalidateRunningDeletion(t *testing.T) {
	for _, state := range []string{VideoDeleteRequested, VideoDeleteDeleting, VideoDeleteFailed} {
		t.Run(state, func(t *testing.T) {
			provider := &videoProviderStub{}
			svc, tasks, queue := newVideoTaskServiceForTest(provider, videoGroupForTest(SubscriptionTypeStandard), nil)
			task := baseVideoWorkerTask()
			task.GenerationState, task.BillingState, task.DeleteState = VideoGenerationCompleted, VideoBillingCaptured, state
			task.Version = 7
			tasks.task, tasks.sources[task.PublicID] = task, task
			returned, err := svc.DeleteForOwner(context.Background(), task.UserID, task.PublicID)
			require.NoError(t, err)
			require.Equal(t, int64(7), returned.Version)
			require.Equal(t, state, returned.DeleteState)
			require.Empty(t, tasks.transitions)
			require.Empty(t, queue.enqueued)
			require.Zero(t, provider.deleteCalls)
		})
	}
}

func TestVideoPriceSnapshotStoresUsageResolutionInsteadOfInternalPricingKey(t *testing.T) {
	snapshot := videoPriceSnapshot(&VideoPriceQuote{
		BillingModel: "doubao-seedance-2.0-mini-480p",
		Attributes: VideoPricingAttributes{
			Model:      "doubao-seedance-2.0-mini-480p",
			Resolution: "resolution-1",
		},
	})

	require.Equal(t, "resolution-1", snapshot["resolution"])
	require.Equal(t, "480p", snapshot["usage_resolution"])
}
