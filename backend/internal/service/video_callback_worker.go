package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/google/uuid"
)

const videoCallbackMaxResponseBytes = int64(4 << 10)

var ErrVideoCallbackLeaseLost = errors.New("video callback lease is no longer owned by worker")

type videoCallbackHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type videoCallbackIPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type VideoCallbackWorker struct {
	repository VideoCallbackRepository
	tasks      *VideoTaskService
	encryptor  SecretEncryptor
	cfg        *config.Config
	client     videoCallbackHTTPDoer
	resolver   videoCallbackIPResolver
	now        func() time.Time
	workerID   string
}

func NewVideoCallbackWorker(repository VideoCallbackRepository, encryptor SecretEncryptor, tasks *VideoTaskService, cfg *config.Config) *VideoCallbackWorker {
	resolver := net.DefaultResolver
	return &VideoCallbackWorker{
		repository: repository,
		tasks:      tasks,
		encryptor:  encryptor,
		cfg:        cfg,
		resolver:   resolver,
		client:     newVideoCallbackHTTPClient(resolver, callbackRequestTimeout(cfg)),
		now:        time.Now,
		workerID:   "video-callback-" + uuid.NewString(),
	}
}

func (w *VideoCallbackWorker) ProcessBatch(ctx context.Context, limit int) error {
	if w == nil || w.repository == nil || w.encryptor == nil {
		return errors.New("video callback worker is not configured")
	}
	if w.cfg == nil || !w.cfg.Gateway.Video.Enabled || !w.cfg.Gateway.Video.Callback.Enabled {
		return nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 32
	}
	if err := w.materializeCallbacks(ctx, limit); err != nil {
		return err
	}
	var failures []error
	for index := 0; index < limit; index++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		// A unique claim token also fences late writes from an earlier attempt
		// in the same worker process. Never lease work that is still waiting.
		owner := w.workerID + ":" + uuid.NewString()
		deliveries, err := w.repository.ClaimVideoCallbacks(ctx, owner, 1, w.leaseDuration())
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if len(deliveries) == 0 {
			break
		}
		if len(deliveries) != 1 || deliveries[0] == nil {
			return errors.Join(append(failures, ErrVideoCallbackLeaseLost)...)
		}
		delivery := deliveries[0]
		if err := w.deliverClaim(ctx, delivery, owner); err != nil {
			failures = append(failures, err)
			slog.Error("video callback delivery failed", "delivery_id", delivery.ID, "attempt", delivery.Attempts+1, "error", err)
		}
	}
	return errors.Join(failures...)
}

func (w *VideoCallbackWorker) leaseDuration() time.Duration {
	if w.cfg != nil && w.cfg.Gateway.Video.LeaseSeconds > 0 {
		return time.Duration(w.cfg.Gateway.Video.LeaseSeconds) * time.Second
	}
	return 90 * time.Second
}

func (w *VideoCallbackWorker) renewLease(ctx context.Context, delivery *VideoCallbackDelivery, owner string) error {
	timeout := min(w.leaseDuration()/3, 5*time.Second)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return w.repository.RenewVideoCallbackLease(ctx, delivery.ID, owner, w.leaseDuration())
}

func (w *VideoCallbackWorker) deliverClaim(ctx context.Context, delivery *VideoCallbackDelivery, owner string) error {
	if delivery.ID <= 0 || delivery.LeaseOwner == nil || *delivery.LeaseOwner != owner ||
		delivery.LeaseExpiresAt == nil || !w.now().Before(*delivery.LeaseExpiresAt) {
		return ErrVideoCallbackLeaseLost
	}
	if err := w.renewLease(ctx, delivery, owner); err != nil {
		return err
	}
	workCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	heartbeatCtx, stop := context.WithCancel(workCtx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(min(w.leaseDuration()/3, 20*time.Second))
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := w.renewLease(heartbeatCtx, delivery, owner); err != nil {
					if heartbeatCtx.Err() == nil {
						cancel(errors.Join(ErrVideoCallbackLeaseLost, err))
					}
					return
				}
			}
		}
	}()
	defer func() { stop(); <-done }()
	err := w.deliver(workCtx, delivery, owner)
	if err != nil {
		return errors.Join(err, context.Cause(workCtx))
	}
	// A successful fenced completion is authoritative even if a concurrent
	// heartbeat observes the lease we just released.
	return nil
}

func (w *VideoCallbackWorker) deliver(ctx context.Context, delivery *VideoCallbackDelivery, owner string) (returnErr error) {
	startedAt := time.Now()
	now := w.now().UTC()
	deliveryDelay := now.Sub(delivery.CreatedAt)
	metricResult := "error"
	defer func() {
		if returnErr != nil && metricResult == "delivered" {
			metricResult = "error"
		}
		observability.DefaultVideoMetrics().RecordCallback(metricResult, time.Since(startedAt), deliveryDelay)
	}()
	if !delivery.ExpiresAt.After(now) {
		metricResult = "quarantined"
		return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback retry window expired")
	}
	if w.tasks != nil {
		if data, ok := delivery.Payload["data"].(map[string]any); ok && data["provider"] != nil {
			if w.tasks.tasks == nil {
				metricResult = "quarantined"
				return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback task identity could not be verified")
			}
			publicID, _ := data["id"].(string)
			task, err := w.tasks.tasks.GetVideoTaskByPublicID(ctx, publicID)
			if err != nil || task == nil || task.ID != delivery.TaskID {
				metricResult = "quarantined"
				return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback task identity could not be verified")
			}
			policy, _ := w.tasks.videoDisclosurePolicy(ctx, task)
			if videoDisclosureRank(policy) < videoDisclosureRank(config.VideoDisclosureIdentity) {
				metricResult = "quarantined"
				return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback disclosure policy changed")
			}
		}
	}
	target, err := w.encryptor.Decrypt(delivery.TargetURLEnc)
	if err != nil {
		metricResult = "quarantined"
		return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback target could not be decrypted")
	}
	target, err = validateVideoCallbackURLWithResolver(ctx, target, w.resolver)
	if err != nil {
		metricResult = "quarantined"
		return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback target failed security validation")
	}
	body, err := json.Marshal(delivery.Payload)
	if err != nil {
		metricResult = "quarantined"
		return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback payload could not be encoded")
	}
	secret := strings.TrimSpace(w.cfg.Gateway.Video.Callback.SigningSecret)
	if secret == "" {
		metricResult = "quarantined"
		return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback signing secret is not configured")
	}
	timeout := callbackRequestTimeout(w.cfg)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		metricResult = "quarantined"
		return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, "callback request could not be created")
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "sub2api-video-callback/1")
	request.Header.Set("X-Sub2API-Event-ID", delivery.EventID)
	request.Header.Set("X-Sub2API-Timestamp", timestamp)
	request.Header.Set("X-Sub2API-Content-SHA256", bodyHashHex)
	request.Header.Set("X-Sub2API-Signature", signVideoCallback(secret, timestamp, delivery.EventID, bodyHashHex))

	if err := w.renewLease(requestCtx, delivery, owner); err != nil {
		return err
	}
	response, err := w.client.Do(request)
	if err != nil {
		metricResult = "retry"
		quarantined, retryErr := w.retry(ctx, delivery, owner, 0, "callback transport failed")
		if quarantined {
			metricResult = "quarantined"
		}
		return retryErr
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, videoCallbackMaxResponseBytes))
	closeErr := response.Body.Close()
	if closeErr != nil {
		metricResult = "retry"
		quarantined, retryErr := w.retry(ctx, delivery, owner, response.StatusCode, "callback response close failed")
		if quarantined {
			metricResult = "quarantined"
		}
		return retryErr
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		metricResult = "delivered"
		return w.repository.MarkVideoCallbackDelivered(ctx, delivery.ID, owner, response.StatusCode)
	}
	if response.StatusCode >= 300 && response.StatusCode < 500 && response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests {
		metricResult = "quarantined"
		return w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, fmt.Sprintf("callback returned permanent status %d", response.StatusCode))
	}
	metricResult = "retry"
	quarantined, retryErr := w.retry(ctx, delivery, owner, response.StatusCode, fmt.Sprintf("callback returned retryable status %d", response.StatusCode))
	if quarantined {
		metricResult = "quarantined"
	}
	return retryErr
}

func (w *VideoCallbackWorker) retry(ctx context.Context, delivery *VideoCallbackDelivery, owner string, statusCode int, message string) (bool, error) {
	next := w.now().UTC().Add(videoCallbackRetryDelay(delivery.Attempts + 1))
	if !delivery.ExpiresAt.After(next) {
		return true, w.repository.QuarantineVideoCallback(ctx, delivery.ID, owner, message)
	}
	return false, w.repository.RetryVideoCallback(ctx, delivery.ID, owner, next, statusCode, message)
}

func signVideoCallback(secret, timestamp, eventID, bodyHash string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, "v1."+timestamp+"."+eventID+"."+bodyHash)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func ValidateVideoCallbackURL(ctx context.Context, raw string) (string, error) {
	return validateVideoCallbackURLWithResolver(ctx, raw, net.DefaultResolver)
}

func validateVideoCallbackURLWithResolver(ctx context.Context, raw string, resolver videoCallbackIPResolver) (string, error) {
	validated, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{})
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(validated)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("callback URL contains unsupported components")
	}
	if resolver == nil {
		return "", errors.New("callback DNS resolver is not configured")
	}
	if _, err := resolvePublicVideoCallbackIPs(ctx, resolver, parsed.Hostname()); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func newVideoCallbackHTTPClient(resolver videoCallbackIPResolver, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   2,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolvePublicVideoCallbackIPs(ctx, resolver, host)
		if err != nil {
			return nil, err
		}
		var failures []error
		for _, address := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			failures = append(failures, dialErr)
		}
		return nil, errors.Join(failures...)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func resolvePublicVideoCallbackIPs(ctx context.Context, resolver videoCallbackIPResolver, host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if host == "" {
		return nil, errors.New("callback host is empty")
	}
	if parsed, err := netip.ParseAddr(host); err == nil {
		parsed = parsed.Unmap()
		if !isPublicVideoCallbackIP(parsed) {
			return nil, errors.New("callback host resolves to a blocked address")
		}
		return []netip.Addr{parsed}, nil
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve callback host: %w", err)
	}
	public := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !isPublicVideoCallbackIP(address) {
			return nil, errors.New("callback host resolves to a blocked address")
		}
		public = append(public, address)
	}
	if len(public) == 0 {
		return nil, errors.New("callback host has no public address")
	}
	return public, nil
}

func isPublicVideoCallbackIP(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	for _, prefix := range videoCallbackBlockedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var videoCallbackBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func callbackRequestTimeout(cfg *config.Config) time.Duration {
	seconds := 15
	if cfg != nil && cfg.Gateway.Video.Callback.RequestTimeoutSeconds > 0 {
		seconds = cfg.Gateway.Video.Callback.RequestTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func videoCallbackRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		attempt = 10
	}
	delay := time.Second * time.Duration(1<<(attempt-1))
	if delay > 30*time.Minute {
		return 30 * time.Minute
	}
	return delay
}

func BuildVideoCallbackDelivery(task *VideoTask, cfg *config.Config, now time.Time, disclosurePolicy string) (*VideoCallbackDelivery, bool, error) {
	if task == nil || task.ID <= 0 || task.CallbackURLEnc == nil || strings.TrimSpace(*task.CallbackURLEnc) == "" {
		return nil, false, nil
	}
	if cfg == nil || !cfg.Gateway.Video.Callback.Enabled || !IsVideoBillingTerminal(task.BillingState) {
		return nil, false, nil
	}
	status := ProjectVideoStatus(task)
	eventType := ""
	switch status {
	case VideoGenerationCompleted:
		eventType = "video.completed"
	case VideoGenerationFailed:
		eventType = "video.failed"
	default:
		return nil, false, nil
	}
	fingerprint, err := HashVideoRequest(map[string]any{
		"task_id": task.ID, "event_type": eventType,
		"generation_state": task.GenerationState, "billing_state": task.BillingState,
	})
	if err != nil {
		return nil, false, err
	}
	eventID := "video_evt_" + fingerprint[:32]
	createdAt := now.UTC()
	if task.SettledAt != nil {
		createdAt = task.SettledAt.UTC()
	}
	data := map[string]any{
		"id": task.PublicID, "object": "video", "status": status,
		"created_at": task.CreatedAt.Unix(),
	}
	if videoDisclosureRank(disclosurePolicy) >= videoDisclosureRank(config.VideoDisclosureIdentity) {
		data["provider"] = task.Provider
	}
	if task.FinishedAt != nil {
		data["completed_at"] = task.FinishedAt.Unix()
	}
	if status == VideoGenerationFailed {
		data["error"] = map[string]any{
			"code":    firstNonEmptyString(videoStringValue(task.LastErrorCode), "video_generation_failed"),
			"message": firstNonEmptyString(videoStringValue(task.LastErrorMessage), "video generation failed"),
		}
	}
	retryHours := cfg.Gateway.Video.Callback.RetryHours
	if frozen, ok := numericMapValue(task.RequestAttributes, "callback_retry_hours"); ok && frozen > 0 && frozen <= 8760 {
		retryHours = int(frozen)
	}
	if retryHours <= 0 {
		retryHours = 24
	}
	return &VideoCallbackDelivery{
		TaskID: task.ID, EventID: eventID, EventType: eventType, EventFingerprint: fingerprint,
		Payload: map[string]any{
			"id": eventID, "object": "event", "created_at": createdAt.Unix(),
			"type": eventType, "data": data,
		},
		TargetURLEnc: *task.CallbackURLEnc, Status: "pending",
		NextAttemptAt: createdAt, ExpiresAt: createdAt.Add(time.Duration(retryHours) * time.Hour), CreatedAt: createdAt,
	}, true, nil
}

type VideoCallbackRuntime struct {
	worker *VideoCallbackWorker
	cfg    *config.Config
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

func NewVideoCallbackRuntime(worker *VideoCallbackWorker, cfg *config.Config) *VideoCallbackRuntime {
	return &VideoCallbackRuntime{worker: worker, cfg: cfg}
}

func (r *VideoCallbackRuntime) Start() {
	if !VideoCallbacksAvailable() || r == nil || r.worker == nil || r.cfg == nil || !r.cfg.Gateway.Video.Enabled || !r.cfg.Gateway.Video.Callback.Enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel, r.done = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		batchSize := 32
		if r.cfg.Gateway.Video.WorkerBatchSize > 0 {
			batchSize = r.cfg.Gateway.Video.WorkerBatchSize
		}
		for {
			if err := r.worker.ProcessBatch(ctx, batchSize); err != nil && ctx.Err() == nil {
				slog.Error("video callback worker batch failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (r *VideoCallbackRuntime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func ProvideVideoCallbackRuntime(worker *VideoCallbackWorker, cfg *config.Config) *VideoCallbackRuntime {
	runtime := NewVideoCallbackRuntime(worker, cfg)
	runtime.Start()
	return runtime
}
