package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type GrokVideoTaskService struct {
	repo      GrokVideoTaskRepository
	gateway   *OpenAIGatewayService
	keys      *APIKeyService
	users     UserRepository
	accounts  AccountRepository
	encryptor SecretEncryptor
	billing   UsageBillingRepository
	post      *UsageBillingPostEffectsService
	cfg       *config.Config
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewGrokVideoTaskService(repo GrokVideoTaskRepository, gateway *OpenAIGatewayService, keys *APIKeyService, users UserRepository, accounts AccountRepository, encryptor SecretEncryptor, billing UsageBillingRepository, post *UsageBillingPostEffectsService, cfg *config.Config) *GrokVideoTaskService {
	return &GrokVideoTaskService{repo: repo, gateway: gateway, keys: keys, users: users, accounts: accounts, encryptor: encryptor, billing: billing, post: post, cfg: cfg}
}
func (s *GrokVideoTaskService) Enabled() bool {
	return s != nil && s.cfg != nil && s.cfg.GrokVideoTracking.Enabled
}
func (s *GrokVideoTaskService) seal(v any) (string, error) {
	if s.encryptor == nil {
		return "", errors.New("grok video encryption unavailable")
	}
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	return s.encryptor.Encrypt(string(b))
}
func (s *GrokVideoTaskService) open(v string, out any) error {
	if s.encryptor == nil {
		return errors.New("grok video encryption unavailable")
	}
	b, e := s.encryptor.Decrypt(v)
	if e != nil {
		return e
	}
	return json.Unmarshal([]byte(b), out)
}
func grokOperation(e GrokMediaEndpoint) string {
	switch e {
	case GrokMediaEndpointVideosEdits:
		return VideoOperationEdit
	case GrokMediaEndpointVideosExtensions:
		return VideoOperationExtend
	default:
		return VideoOperationGenerate
	}
}
func grokRequestHash(body []byte) string {
	var v any
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.UseNumber()
	if d.Decode(&v) == nil {
		if b, e := json.Marshal(v); e == nil {
			body = b
		}
	}
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}
func (s *GrokVideoTaskService) Replay(ctx context.Context, key *APIKey, e GrokMediaEndpoint, idem string, body []byte) (*GrokVideoTask, error) {
	if idem == "" {
		return nil, nil
	}
	if len(idem) > 255 {
		return nil, ErrGrokVideoConflict
	}
	t, err := s.repo.FindIdempotent(ctx, key.UserID, key.ID, grokOperation(e), idem)
	if errors.Is(err, ErrGrokVideoNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if key.GroupID == nil || t.GroupID != *key.GroupID {
		return nil, ErrGrokVideoConflict
	}
	if t.RequestHash != grokRequestHash(body) {
		// Older composite requests were recorded after model rewriting. Reconstruct
		// only the model that was actually submitted, and only when the frozen
		// original alias matches the caller; all other request bytes remain hashed.
		var attribution grokVideoPrivate
		if s.open(t.PrivateEnc, &attribution) != nil || attribution.OriginalModel == "" || gjson.GetBytes(body, "model").String() != attribution.OriginalModel {
			return nil, ErrGrokVideoConflict
		}
		normalized, err := sjson.SetBytes(body, "model", t.RequestedModel)
		if err != nil || t.RequestHash != grokRequestHash(normalized) {
			return nil, ErrGrokVideoConflict
		}
	}
	if t.CreateResponseEnc == "" {
		return nil, ErrGrokVideoBusy
	}
	return t, nil
}
func (s *GrokVideoTaskService) Begin(ctx context.Context, key *APIKey, account *Account, sub *UserSubscription, e GrokMediaEndpoint, idem, path string, body []byte, fields ChannelUsageFields, contentTypes ...string) (*GrokVideoTask, bool, error) {
	if key == nil || key.GroupID == nil || account == nil {
		return nil, false, ErrGrokVideoConflict
	}
	contentType := "application/json"
	if len(contentTypes) > 0 {
		contentType = contentTypes[0]
	}
	info := ParseGrokMediaRequest(contentType, body)
	private, err := s.seal(grokVideoPrivate{Group: key.Group, Subscription: sub, QuotaPlatform: QuotaPlatform(ctx, key), ChannelUsageFields: fields})
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	due := now.Add(60 * time.Second)
	t := &GrokVideoTask{PublicID: "grok_video_" + strings.ReplaceAll(uuid.NewString(), "-", ""), UserID: key.UserID, APIKeyID: key.ID, GroupID: *key.GroupID, AccountID: account.ID, Operation: grokOperation(e), Endpoint: path, RequestHash: grokRequestHash(body), RequestedModel: info.Model, UpstreamModel: account.GetMappedModel(info.Model), Prompt: info.Prompt, Resolution: info.Resolution, Duration: info.DurationSeconds, AspectRatio: info.AspectRatio, InputCount: len(info.InputImageURLs) + len(info.Uploads), PrivateEnc: private, GenerationState: "submitting", SyncStatus: "scheduled", SettlementStatus: "not_due", NextActionAt: &due, CreatedAt: now}
	if gjson.ValidBytes(body) {
		add := func(value gjson.Result, role VideoInputRole) {
			if !value.Exists() {
				return
			}
			sum := sha256.Sum256([]byte(value.Raw))
			t.InputManifest = append(t.InputManifest, VideoInputManifestEntry{Role: role, SHA256: hex.EncodeToString(sum[:])})
		}
		add(gjson.GetBytes(body, "image"), VideoInputRoleFirstFrame)
		add(gjson.GetBytes(body, "last_frame"), VideoInputRoleLastFrame)
		add(gjson.GetBytes(body, "video"), VideoInputRoleSourceVideo)
		for _, item := range gjson.GetBytes(body, "reference_images").Array() {
			add(item, VideoInputRoleTaggedReferenceImage)
		}
		for _, item := range gjson.GetBytes(body, "reference_audios").Array() {
			add(item, VideoInputRoleReferenceAudio)
		}
		for _, item := range gjson.GetBytes(body, "keyframes").Array() {
			add(item, VideoInputRoleReferenceImage)
		}
	} else {
		for _, url := range info.InputImageURLs {
			sum := sha256.Sum256([]byte(url))
			t.InputManifest = append(t.InputManifest, VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, SHA256: hex.EncodeToString(sum[:]), MIMEType: "image/*"})
		}
		for _, upload := range info.Uploads {
			sum := sha256.Sum256(upload.Data)
			t.InputManifest = append(t.InputManifest, VideoInputManifestEntry{Role: VideoInputRoleReferenceImage, SHA256: hex.EncodeToString(sum[:]), MIMEType: upload.ContentType, Size: int64(len(upload.Data))})
		}
	}
	t.InputCount = len(t.InputManifest)
	if v := gjson.GetBytes(body, "generate_audio"); v.Type == gjson.True || v.Type == gjson.False {
		b := v.Bool()
		t.GenerateAudio = &b
	}
	if !s.Enabled() {
		t.Prompt = ""
	}
	if idem != "" {
		t.IdempotencyKey = &idem
	}
	saved, created, err := s.repo.Create(ctx, t)
	if err != nil {
		observability.GrokVideoEvent("create_failed")
	} else if created {
		observability.GrokVideoEvent("created")
	}
	return saved, created, err
}
func (s *GrokVideoTaskService) CreateResponse(t *GrokVideoTask) ([]byte, int, error) {
	var b json.RawMessage
	err := s.open(t.CreateResponseEnc, &b)
	return b, t.CreateStatus, err
}
func (s *GrokVideoTaskService) Accepted(ctx context.Context, t *GrokVideoTask, status int, body []byte) error {
	id := extractGrokMediaVideoRequestID(body)
	if id == "" {
		_ = s.SubmissionFailed(ctx, t, true, "missing_upstream_task_id")
		return errors.New("missing upstream task id")
	}
	enc, err := s.seal(json.RawMessage(body))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	next := now.Add(5 * time.Second)
	t.ProviderTaskID = &id
	t.CreateResponseEnc = enc
	t.CreateStatus = status
	t.GenerationState = "queued"
	t.SyncStatus = "scheduled"
	t.SubmittedAt = &now
	t.NextActionAt = &next
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for {
		err = s.repo.Save(saveCtx, t, "submitted")
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrGrokVideoBusy) {
			saved, e := s.repo.Get(saveCtx, t.PublicID)
			if e == nil && saved.ProviderTaskID != nil && *saved.ProviderTaskID == id {
				*t = *saved
				return nil
			}
			if e == nil && saved.ProviderTaskID == nil && saved.LeaseOwner == nil && (saved.GenerationState == "submitting" || saved.GenerationState == "submission_unknown") {
				t.Version = saved.Version
				t.LeaseEpoch = saved.LeaseEpoch
			}
		}
		select {
		case <-saveCtx.Done():
			observability.GrokVideoEvent("persist_failed")
			slog.Error("grok_video.acceptance_persistence_failed", "task_id", t.PublicID, "provider_task_id", id)
			return err
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func (s *GrokVideoTaskService) SubmissionFailed(ctx context.Context, t *GrokVideoTask, unknown bool, code string) error {
	t.GenerationState = "failed"
	t.SyncStatus = "finished"
	t.SettlementStatus = "not_required"
	if unknown {
		observability.GrokVideoEvent("submission_unknown")
		t.GenerationState = "submission_unknown"
		t.SyncStatus = "paused"
		t.SettlementStatus = "not_due"
	}
	t.LastErrorCode = code
	t.NextActionAt = nil
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.repo.Save(saveCtx, t, "submission_"+t.GenerationState)
}
func (s *GrokVideoTaskService) Resolve(ctx context.Context, key *APIKey, id string) (*GrokVideoTask, error) {
	if key == nil || key.GroupID == nil {
		return nil, ErrGrokVideoNotFound
	}
	t, err := s.repo.FindOwned(ctx, key.UserID, key.ID, *key.GroupID, id)
	if !errors.Is(err, ErrGrokVideoNotFound) || !s.Enabled() {
		return t, err
	}
	accountID, err := s.gateway.ResolveGrokMediaVideoRequestAccount(ctx, key.GroupID, id, key.UserID, key.ID)
	if errors.Is(err, ErrStickySessionNotFound) || err == nil && accountID <= 0 {
		return nil, ErrGrokVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	pending, err := s.gateway.LoadGrokVideoPendingBilling(ctx, id, key.UserID, key.ID)
	if err != nil {
		return nil, err
	}
	private, err := s.seal(grokVideoPrivate{Group: key.Group, QuotaPlatform: QuotaPlatform(ctx, key)})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t = &GrokVideoTask{PublicID: "grok_video_" + strings.ReplaceAll(uuid.NewString(), "-", ""), UserID: key.UserID, APIKeyID: key.ID, GroupID: *key.GroupID, AccountID: accountID, ProviderTaskID: &id, Operation: VideoOperationGenerate, Endpoint: "/v1/videos/generations", RequestHash: grokRequestHash([]byte(id)), PrivateEnc: private, ImportedFromLegacy: true, BillingSnapshotMissing: true, GenerationState: "queued", SyncStatus: "scheduled", SettlementStatus: "not_due", NextActionAt: &now}
	if pending != nil {
		t.RequestedModel = pending.Model
		t.UpstreamModel = pending.UpstreamModel
		t.Resolution = pending.VideoResolution
		t.Duration = pending.VideoDurationSeconds
		t.BillingSnapshotMissing = t.Resolution == "" || t.RequestedModel == "" || (key.Group != nil && key.Group.IsSubscriptionType())
		if start, e := time.Parse(time.RFC3339Nano, pending.CreatedAt); e == nil {
			t.SubmittedAt = &start
		}
	}
	t, _, err = s.repo.Create(ctx, t)
	if err != nil {
		observability.GrokVideoEvent("import_failed")
	} else {
		observability.GrokVideoEvent("imported")
	}
	return t, err
}
func (s *GrokVideoTaskService) Account(ctx context.Context, t *GrokVideoTask) (*Account, error) {
	a, err := s.accounts.GetByID(ctx, t.AccountID)
	if err != nil {
		return nil, ErrGrokVideoUnavailable
	}
	group := t.GroupID
	if a == nil || a.ID != t.AccountID || a.Platform != PlatformGrok || a.IsPermanentlyUnschedulable() || !s.gateway.openAIAccountMatchesSchedulingGroup(a, &group) || !canScheduleAccountForUser(ctx, s.accounts, a, t.UserID) {
		return nil, ErrGrokVideoUnavailable
	}
	return a, nil
}
func (s *GrokVideoTaskService) CachedResponse(t *GrokVideoTask) ([]byte, error) {
	if t.ResponseEnc != "" {
		var body json.RawMessage
		if err := s.open(t.ResponseEnc, &body); err != nil {
			return nil, err
		}
		return body, nil
	}
	return json.Marshal(map[string]any{"status": "pending", "progress": t.Progress})
}
func (s *GrokVideoTaskService) Observe(ctx context.Context, t *GrokVideoTask, force bool) (*GrokVideoTask, error) {
	if !force && t.SyncStatus == "retrying" && t.NextActionAt != nil && time.Now().Before(*t.NextActionAt) {
		status := 503
		if t.LastErrorCode == "upstream_429" {
			status = 429
		}
		return t, &GrokVideoHTTPError{Status: status, RetryAfter: time.Until(*t.NextActionAt)}
	}
	if !force && t.LastPolledAt != nil && time.Since(*t.LastPolledAt) < 3*time.Second {
		return t, nil
	}
	if (t.GenerationState == "completed" && t.SettlementStatus == "settled") || t.GenerationState == "failed" || t.GenerationState == "expired" {
		return t, nil
	}
	claimed, err := s.repo.Claim(ctx, t.PublicID, uuid.NewString(), true)
	if errors.Is(err, ErrGrokVideoBusy) {
		return s.repo.Get(ctx, t.PublicID)
	}
	if err != nil {
		return nil, err
	}
	err = s.processClaim(ctx, claimed, true)
	// A billing failure must not hide a successfully generated video.
	if err != nil && claimed.GenerationState == "completed" && claimed.ResponseEnc != "" && (claimed.SettlementStatus == "blocked" || claimed.SettlementStatus == "retrying") {
		return claimed, nil
	}
	return claimed, err
}
func (s *GrokVideoTaskService) processClaim(ctx context.Context, t *GrokVideoTask, settle bool) (retErr error) {
	before := t.GenerationState + ":" + t.SyncStatus + ":" + t.SettlementStatus + ":" + t.LastErrorCode
	defer func() {
		event := ""
		if before != t.GenerationState+":"+t.SyncStatus+":"+t.SettlementStatus+":"+t.LastErrorCode {
			event = "observation"
		}
		t.LeaseOwner = nil
		t.LeaseExpiresAt = nil
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.repo.Save(saveCtx, t, event); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	if t.ProviderTaskID == nil {
		t.GenerationState = "submission_unknown"
		t.SyncStatus = "paused"
		t.NextActionAt = nil
		t.LastErrorCode = "submission_unconfirmed"
		return nil
	}
	if !IsVideoGenerationTerminal(t.GenerationState) {
		deadline := t.CreatedAt.Add(24 * time.Hour)
		if t.ObservationDeadline != nil {
			deadline = *t.ObservationDeadline
		}
		if time.Now().After(deadline) {
			t.SyncStatus = "paused"
			t.NextActionAt = nil
			t.LastErrorCode = "observation_deadline_exceeded"
			observability.GrokVideoEvent("sync_paused")
			return nil
		}

		account, err := s.Account(ctx, t)
		var response *GrokVideoHTTPResponse
		if err == nil {
			reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			response, err = s.gateway.FetchGrokVideoStatus(reqCtx, account, *t.ProviderTaskID)
			cancel()
		}
		if err != nil {
			s.retry(t, err)
			return err
		}
		observability.GrokVideoEvent("observed")
		observability.GrokVideoObservationAge(t.CreatedAt)
		raw := response.Body
		now := time.Now().UTC()
		status := gjson.GetBytes(raw, "status").String()
		oldState := t.GenerationState
		state := ""
		switch status {
		case "pending":
			state = "queued"
			if gjson.GetBytes(raw, "progress").Float() > 0 {
				state = "in_progress"
			}
		case "done":
			if IsGrokVideoStatusBillable(raw) {
				state = "completed"
			}
		case "failed":
			state = "failed"
		case "expired":
			state = "expired"
		}
		if state == "" {
			err = errors.New("invalid upstream video status")
			s.retry(t, err)
			return err
		}
		enc, e := s.seal(json.RawMessage(raw))
		if e != nil {
			s.retry(t, e)
			return e
		}
		t.ResponseEnc = enc
		t.ProviderStatus = status
		t.GenerationState = state
		t.LastPolledAt = &now
		t.LastErrorCode = ""
		t.PollAttempts = 0
		t.Progress = math.Max(t.Progress, math.Min(100, math.Max(0, gjson.GetBytes(raw, "progress").Float())))
		if state == "completed" {
			t.Progress = 100
			t.FinishedAt = &now
			t.ActualDuration = int(gjson.GetBytes(raw, "video.duration").Int())
			t.SettlementStatus = "pending"
			if v := gjson.GetBytes(raw, "usage.cost_in_usd_ticks"); v.Type == gjson.Number {
				t.UpstreamCostTicks = json.Number(v.Raw)
			}
		}
		if state == "failed" || state == "expired" {
			t.FinishedAt = &now
			t.SettlementStatus = "not_required"
		}
		if IsVideoGenerationTerminal(state) {
			t.SyncStatus = "finished"
			t.NextActionAt = nil
			if state == "completed" {
				t.NextActionAt = &now
			}
		} else {
			t.SyncStatus = "scheduled"
			due := now.Add(5*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond)
			t.NextActionAt = &due
		}
		// Completion must be durable before any billing side effect.
		event := ""
		if oldState != state {
			event = "status_" + state
		}
		if e = s.repo.Save(ctx, t, event); e != nil {
			return e
		}
		before = t.GenerationState + ":" + t.SyncStatus + ":" + t.SettlementStatus + ":" + t.LastErrorCode
	}
	if settle && t.GenerationState == "completed" && t.SettlementStatus != "settled" && t.SettlementStatus != "blocked" {
		return s.settle(ctx, t)
	}
	return nil
}
func (s *GrokVideoTaskService) retry(t *GrokVideoTask, err error) {
	t.PollAttempts++
	t.SyncStatus = "retrying"
	t.LastErrorCode = "upstream_unavailable"
	if errors.Is(err, ErrGrokVideoUnavailable) {
		t.LastErrorCode = "account_unavailable"
	}
	delay := time.Duration(min(60, 5*(1<<min(t.PollAttempts-1, 4)))) * time.Second
	var upstream *GrokVideoHTTPError
	if errors.As(err, &upstream) {
		t.LastErrorCode = fmt.Sprintf("upstream_%d", upstream.Status)
		if upstream.RetryAfter > delay {
			delay = upstream.RetryAfter
		}
	}
	due := time.Now().Add(delay)
	t.NextActionAt = &due
	deadline := t.CreatedAt.Add(24 * time.Hour)
	if t.ObservationDeadline != nil {
		deadline = *t.ObservationDeadline
	}
	if time.Now().After(deadline) {
		t.SyncStatus = "paused"
		t.NextActionAt = nil
		observability.GrokVideoEvent("sync_paused")
		slog.Warn("grok_video.sync_paused", "task_id", t.PublicID)
	}
}
func (s *GrokVideoTaskService) settle(ctx context.Context, t *GrokVideoTask) error {
	id := StableGrokVideoBillingRequestID(*t.ProviderTaskID)
	log, err := s.repo.RecordedUsage(ctx, id, t.APIKeyID)
	if err != nil {
		return err
	}
	if log != nil && log.BillingState == BillingStateSettled {
		cost := log.ActualCost
		t.ActualCost = &cost
		t.UsageLogID = &log.ID
		observability.GrokVideoEvent("settled")
		observability.GrokVideoSettlementDelay(t.FinishedAt)
		t.SettlementStatus = "settled"
		now := time.Now().UTC()
		t.SettledAt = &now
		t.NextActionAt = nil
		return nil
	}
	var prepared *PreparedGrokVideoBilling
	if t.SnapshotEnc != "" {
		if err = s.open(t.SnapshotEnc, &prepared); err != nil {
			return err
		}
	} else {
		prepared, err = s.repo.ExistingBilling(ctx, id, t.APIKeyID)
		if err != nil {
			return err
		}
		if prepared == nil {
			if t.BillingSnapshotMissing {
				observability.GrokVideoEvent("settlement_blocked")
				t.SettlementStatus = "blocked"
				t.LastErrorCode = "billing_snapshot_missing"
				t.NextActionAt = nil
				return nil
			}
			prepared, err = s.prepareBilling(ctx, t)
			if err != nil {
				observability.GrokVideoEvent("settlement_blocked")
				t.SettlementStatus = "blocked"
				t.LastErrorCode = "billing_preparation_failed"
				t.NextActionAt = nil
				return err
			}
		}
		t.SnapshotEnc, err = s.seal(prepared)
		if err != nil {
			return err
		}
		if err = s.repo.Save(ctx, t, "settlement_prepared"); err != nil {
			return err
		}
	}
	if prepared.Simple {
		err = s.repo.RecordSimpleUsage(ctx, prepared.UsageLog)
	} else {
		repo, ok := s.billing.(DurableUsageBillingRepository)
		if !ok {
			return ErrDurableUsageBillingRequired
		}
		var result *UsageBillingApplyResult
		result, err = repo.ApplyAndRecord(ctx, prepared.Command, prepared.UsageLog)
		if err == nil && (result == nil || !result.UsageLogRecorded) {
			err = ErrDurableUsageBillingRequired
		}
		if err == nil && result.OutboxReceipt != nil {
			if e := s.post.Finalize(ctx, prepared.Command, result); e == nil {
				_ = repo.AcknowledgeUsageBillingOutbox(ctx, result.OutboxReceipt.WorkerID, result.OutboxReceipt.ID)
			}
		}
	}
	if err != nil {
		observability.GrokVideoEvent("settlement_failed")
		t.SettlementStatus = "retrying"
		t.LastErrorCode = "settlement_failed"
		due := time.Now().Add(10 * time.Second)
		t.NextActionAt = &due
		return err
	}
	// Reconcile from the durable log on the next pass, including simple mode.
	t.SettlementStatus = "pending"
	due := time.Now()
	t.NextActionAt = &due
	return nil
}
func (s *GrokVideoTaskService) prepareBilling(ctx context.Context, t *GrokVideoTask) (*PreparedGrokVideoBilling, error) {
	var p grokVideoPrivate
	if err := s.open(t.PrivateEnc, &p); err != nil {
		return nil, err
	}
	key, err := s.keys.GetByID(ctx, t.APIKeyID)
	if err != nil {
		return nil, err
	}
	keyCopy := *key
	key = &keyCopy
	key.Group = p.Group
	key.GroupID = &t.GroupID
	user, err := s.users.GetByID(ctx, t.UserID)
	if err != nil {
		return nil, err
	}
	key.User = user
	account, err := s.accounts.GetByID(ctx, t.AccountID)
	if err != nil {
		return nil, err
	}
	duration := t.ActualDuration
	if duration <= 0 {
		duration = t.Duration
	}
	result := &OpenAIForwardResult{RequestID: StableGrokVideoBillingRequestID(*t.ProviderTaskID), ResponseID: *t.ProviderTaskID, Model: t.RequestedModel, BillingModel: firstNonEmpty(t.UpstreamModel, t.RequestedModel), UpstreamModel: t.UpstreamModel, VideoCount: 1, VideoResolution: t.Resolution, VideoDurationSeconds: duration, Duration: time.Since(t.CreatedAt)}
	var prepared *PreparedGrokVideoBilling
	err = s.gateway.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user, Account: account, Subscription: p.Subscription, InboundEndpoint: t.Endpoint, UpstreamEndpoint: "/v1/videos/generations", QuotaPlatform: p.QuotaPlatform, BillingKind: BillingKindVideo, APIKeyService: s.keys, RequestPayloadHash: HashUsageRequestPayload([]byte(*t.ProviderTaskID)), ChannelUsageFields: p.ChannelUsageFields, PrepareOnly: func(v *PreparedGrokVideoBilling) error { prepared = v; return nil }})
	if err == nil && prepared != nil && (prepared.UsageLog == nil || prepared.UsageLog.BillingState != BillingStateSettled) {
		err = errors.New("grok video pricing unavailable")
	}
	if err == nil && prepared == nil {
		err = ErrDurableUsageBillingRequired
	}
	return prepared, err
}
func (s *GrokVideoTaskService) Start() {
	if s == nil || s.cfg == nil || !s.cfg.GrokVideoTracking.WorkerEnabled {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ids, err := s.repo.Due(ctx, 4)
				if err != nil {
					slog.Warn("grok_video.worker_read_failed")
					continue
				}
				var wg sync.WaitGroup
				for _, id := range ids {
					wg.Add(1)
					go func(id string) {
						defer wg.Done()
						t, e := s.repo.Claim(ctx, id, uuid.NewString(), false)
						if e != nil {
							return
						}
						taskCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
						defer cancel()
						if e = s.processClaim(taskCtx, t, true); e != nil {
							slog.Warn("grok_video.worker_task_failed", "task_id", id)
						}
					}(id)
				}
				wg.Wait()
			}
		}
	}()
}
func (s *GrokVideoTaskService) Stop() {
	if s != nil && s.cancel != nil {
		s.cancel()
		<-s.done
	}
}
func (s *GrokVideoTaskService) AdminAction(ctx context.Context, id, action string) (*GrokVideoTask, error) {
	t, err := s.repo.Claim(ctx, id, uuid.NewString(), true)
	if err != nil {
		return nil, err
	}
	switch action {
	case "get", "resume":
		if t.ProviderTaskID == nil || IsVideoGenerationTerminal(t.GenerationState) {
			t.LeaseOwner = nil
			t.LeaseExpiresAt = nil
			_ = s.repo.Save(ctx, t, "")
			return nil, ErrVideoInvalidTransition
		}
		t.SyncStatus = "scheduled"
		deadline := time.Now().Add(24 * time.Hour)
		t.ObservationDeadline = &deadline
	case "settlement":
		if t.SnapshotEnc == "" || t.SettlementStatus == "settled" {
			t.LeaseOwner = nil
			t.LeaseExpiresAt = nil
			_ = s.repo.Save(ctx, t, "")
			return nil, ErrVideoInvalidTransition
		}
		t.SettlementStatus = "pending"
	default:
		t.LeaseOwner = nil
		t.LeaseExpiresAt = nil
		_ = s.repo.Save(ctx, t, "")
		return nil, ErrVideoInvalidRequest
	}
	due := time.Now()
	t.NextActionAt = &due
	if err = s.repo.Save(ctx, t, "admin_"+action); err != nil {
		return nil, err
	}
	return t, s.processClaim(ctx, t, true)
}

// FetchGrokVideoStatus performs no HTTP handler writes or customer billing.
type GrokVideoHTTPResponse struct {
	Body   []byte
	Status int
	Header http.Header
}
type GrokVideoHTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *GrokVideoHTTPError) Error() string {
	return fmt.Sprintf("grok video upstream status %d", e.Status)
}
