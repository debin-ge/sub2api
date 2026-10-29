package service

// Grok's wire protocol is deliberately confined to this adapter. It performs no
// customer billing, task persistence, scheduling, or writes to a Gin response.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const VideoProviderGrok = "grok"
const grokManagedVideoMaxResponse = 1 << 20

// GrokVideoOptions contains protocol-specific references that have no Ark
// equivalent. URLs and binary material remain submission-scoped, never task
// metadata. Source is set only after verifying and freezing the source bytes.
type GrokVideoOptions struct {
	ClientRequestHash string
	Keyframes         []GrokVideoKeyframe
	VoiceIDs          []string
	Source            *GrokVerifiedVideoSource
}
type GrokVideoKeyframe struct {
	Image            VideoContentItem
	TimestampSeconds float64
}
type GrokVerifiedVideoSource struct {
	verified bool
	Input    VideoInput
	Seconds  float64
	Width    int
	Height   int
}

type grokVideoTokenSource interface {
	GetAccessToken(context.Context, *Account) (string, error)
}
type GrokVideoProvider struct {
	catalog  *VideoCapabilityCatalog
	upstream HTTPUpstream
	tokens   grokVideoTokenSource
	cfg      *config.Config
	content  *ByteDanceVideoProvider
}

func NewGrokVideoProvider(upstream HTTPUpstream, tokens *GrokTokenProvider, cfg *config.Config) *GrokVideoProvider {
	return &GrokVideoProvider{upstream: upstream, tokens: tokens, cfg: cfg, content: NewByteDanceVideoProvider(upstream, nil)}
}
func (*GrokVideoProvider) Name() string { return VideoProviderGrok }
func (p *GrokVideoProvider) Capabilities() VideoCapabilities {
	if p != nil && p.catalog != nil {
		if caps, ok := p.catalog.Capabilities(VideoProviderGrok); ok {
			return caps
		}
	}
	return DefaultGrokVideoCapabilities()
}
func (p *GrokVideoProvider) SupportsAccount(a *Account) bool {
	if p == nil || a == nil || a.Platform != PlatformGrok {
		return false
	}
	if eligible, _ := a.GrokMediaGenerationEligibility(); !eligible {
		return false
	}
	if a.Type == AccountTypeAPIKey {
		return strings.TrimSpace(a.GetCredential("api_key")) != ""
	}
	return a.IsGrokOAuth() && p.tokens != nil
}
func DefaultGrokVideoCapabilities() VideoCapabilities {
	c := VideoCapabilities{DefaultModel: "grok-imagine-video-1.5", SupportedModels: map[string]bool{}, DefaultSeconds: map[string]int{}, SupportedSeconds: map[string][]int{}, SupportedResolutions: map[string][]string{}, DefaultResolutions: map[string]string{}, SupportedRatios: map[string][]string{}, DefaultRatios: map[string]string{},
		Operations:      map[VideoCapability]bool{VideoCapabilityCreate: true, VideoCapabilityInputReference: true, VideoCapabilityEdits: true, VideoCapabilityUploadedVideoEdits: true, VideoCapabilityExtensions: true},
		ContentVariants: map[string]bool{"video": true}, InputRolesByOperation: map[string]map[VideoInputRole]bool{}, InputMIMETypes: map[VideoInputRole]map[string]bool{}, MaxInputBytes: map[VideoInputRole]int64{}, MaxInputsByOperation: map[string]int{VideoOperationGenerate: 20, VideoOperationEdit: 1, VideoOperationExtend: 1}}
	for _, model := range []string{"grok-imagine-video", "grok-imagine-video-1.5", "grok-imagine-video-1.5-lite", "grok-imagine-video-1.5-preview", "grok-imagine-video-1.5-2026-05-30"} {
		c.SupportedModels[model] = true
		c.DefaultSeconds[model] = 8
		c.DefaultResolutions[model] = "480p"
		c.DefaultRatios[model] = "16:9"
		c.SupportedResolutions[model] = []string{"480p", "720p"}
		if model != "grok-imagine-video" {
			c.SupportedResolutions[model] = append(c.SupportedResolutions[model], "1080p")
		}
		for i := 1; i <= 15; i++ {
			c.SupportedSeconds[model] = append(c.SupportedSeconds[model], i)
		}
		c.SupportedRatios[model] = []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "21:9", "5:2"}
	}
	c.InputRolesByOperation[VideoOperationGenerate] = map[VideoInputRole]bool{VideoInputRoleFirstFrame: true, VideoInputRoleLastFrame: true, VideoInputRoleTaggedReferenceImage: true, VideoInputRoleReferenceImage: true}
	for role := range c.InputRolesByOperation[VideoOperationGenerate] {
		c.InputMIMETypes[role] = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
		c.MaxInputBytes[role] = MaxContentModerationImageBytes
	}
	for _, op := range []string{VideoOperationEdit, VideoOperationExtend} {
		c.InputRolesByOperation[op] = map[VideoInputRole]bool{VideoInputRoleSourceVideo: true}
	}
	c.InputMIMETypes[VideoInputRoleSourceVideo] = map[string]bool{"video/mp4": true}
	c.MaxInputBytes[VideoInputRoleSourceVideo] = 100 << 20
	return c
}
func grokManagedModelFamily(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "grok-imagine-video-1.5-preview" || model == "grok-imagine-video-1.5-2026-05-30" {
		return "grok-imagine-video-1.5"
	}
	return model
}
func grokCapabilityModel(r VideoCreateRequest) string {
	for _, model := range []string{r.RequestedModel, r.Model} {
		family := grokManagedModelFamily(model)
		if family == "grok-imagine-video" || family == "grok-imagine-video-1.5" || family == "grok-imagine-video-1.5-lite" {
			return family
		}
	}
	return grokManagedModelFamily(firstNonEmptyString(r.RequestedModel, r.Model))
}
func grokCapabilityKey(r VideoCreateRequest, caps VideoCapabilities) string {
	for _, model := range []string{r.RequestedModel, r.Model, grokCapabilityModel(r)} {
		key := strings.ToLower(strings.TrimSpace(model))
		if caps.SupportedModels[key] {
			return key
		}
	}
	return grokCapabilityModel(r)
}
func (p *GrokVideoProvider) ApplyRequestDefaults(r VideoCreateRequest) VideoCreateRequest {
	if normalizeVideoOperation(r.Operation) != VideoOperationGenerate {
		return r
	}
	caps := p.Capabilities()
	model := grokCapabilityKey(r, caps)
	if r.Seconds == 0 {
		r.Seconds = caps.DefaultSeconds[model]
	}
	if r.Resolution == "" {
		r.Resolution = caps.DefaultResolutions[model]
	}
	if r.Ratio == "" && len(r.ContentByRole(VideoInputRoleFirstFrame)) == 0 && len(r.ContentByRole(VideoInputRoleReferenceImage)) == 0 {
		r.Ratio = caps.DefaultRatios[model]
	}
	if r.AudioEnabled == nil {
		yes := true
		r.AudioEnabled = &yes
	}
	return r
}
func (p *GrokVideoProvider) ValidateSubmission(a *Account, r VideoCreateRequest, inputs []VideoInput) error {
	reject := func(message string) error {
		return rejectedVideoProviderError("validation", "grok_video_invalid_request", message, 400)
	}
	if p == nil || p.upstream == nil || !p.SupportsAccount(a) {
		return rejectedVideoProviderError("permission", "unsupported_account", "account does not support Grok videos", 403)
	}
	if err := ValidateVideoCreateCapabilities(p.Capabilities(), r, inputs); err != nil {
		rejected := rejectedVideoProviderError("validation", "grok_video_invalid_request", "Grok video request is not enabled by its capability catalog", http.StatusBadRequest)
		rejected.Cause = err
		return rejected
	}
	family := grokCapabilityModel(r)
	caps := p.Capabilities()
	capKey := grokCapabilityKey(r, caps)
	if !slices.Contains([]string{"grok-imagine-video", "grok-imagine-video-1.5", "grok-imagine-video-1.5-lite"}, family) {
		return reject("Grok video model capability is unknown")
	}
	if r.SmartDuration || r.Frames != 0 || r.Seed != nil || r.CameraFixed != nil || r.Watermark != nil || r.ReturnLastFrame != nil || r.ServiceTier != "" || r.Size != "" || r.Quality != "" || r.AspectRatio != "" || r.ExecutionExpiresAfter != 0 || r.ParentTask != nil || r.InputReference != nil || len(r.Characters) != 0 || len(r.ProviderOptions) != 0 {
		return reject("unsupported Grok video parameter")
	}
	op := normalizeVideoOperation(r.Operation)
	if op == VideoOperationEdit || op == VideoOperationExtend {
		if family != "grok-imagine-video" {
			return reject("this Grok model does not support editing or extension")
		}
		if r.Grok == nil || r.Grok.Source == nil || r.Grok.Source.Input.Open == nil {
			return reject("a verified immutable source video is required")
		}
		src := r.Grok.Source
		if !src.verified || !finitePositive(src.Seconds) || src.Width <= 0 || src.Height <= 0 || src.Input.SHA256 == "" {
			return reject("source video specification is unavailable")
		}
		if len(r.Content) > 0 || len(r.Grok.VoiceIDs) > 0 || len(r.Grok.Keyframes) > 0 || r.AudioEnabled != nil {
			return reject("editing and extension cannot mix generation references")
		}
		if r.Ratio != "" || r.Size != "" || r.Quality != "" {
			return reject("derived video geometry is determined by the verified source")
		}
		if op == VideoOperationEdit && src.Seconds > 8.7 {
			return reject("source video exceeds editing duration limit")
		}
		if op == VideoOperationExtend && (src.Seconds < 2 || src.Seconds > 15 || r.Seconds < 2 || r.Seconds > 10) {
			return reject("invalid source or extension duration")
		}
		if r.Prompt == "" {
			return reject("prompt is required for editing and extension")
		}
		return nil
	}
	if op != VideoOperationGenerate {
		return reject("unsupported Grok video operation")
	}
	if allowed := caps.SupportedSeconds[capKey]; len(allowed) > 0 && !slices.Contains(allowed, r.Seconds) {
		return reject("duration is not enabled for this Grok model")
	}
	if r.Seconds < 1 || r.Seconds > 15 {
		return reject("duration must be 1 to 15 seconds")
	}
	if !slices.Contains(caps.SupportedResolutions[capKey], r.Resolution) {
		return reject("unsupported video resolution")
	}
	if r.Ratio != "" && !slices.Contains(caps.SupportedRatios[capKey], r.Ratio) {
		return reject("unsupported aspect ratio")
	}
	if r.Grok != nil && r.Grok.Source != nil {
		return reject("source video is not a generation reference")
	}
	counts := map[VideoInputRole]int{}
	for _, item := range r.Content {
		if item.Type == VideoContentTypeText {
			continue
		}
		if item.Type != VideoContentTypeImage || !caps.SupportsInputForOperation(op, item.Role) {
			return reject("unsupported reference role")
		}
		if (item.Input == nil) == (strings.TrimSpace(item.URL) == "") {
			return reject("each reference needs exactly one input")
		}
		counts[item.Role]++
	}
	if counts[VideoInputRoleFirstFrame]+counts[VideoInputRoleReferenceImage] > 1 || counts[VideoInputRoleLastFrame] > 1 {
		return reject("only one first and last frame is allowed")
	}
	refs := counts[VideoInputRoleTaggedReferenceImage]
	hasPinned := counts[VideoInputRoleLastFrame] > 0
	voices := 0
	if r.Grok != nil {
		voices = len(r.Grok.VoiceIDs)
		if voices > 3 || len(r.Grok.Keyframes) > 4 {
			return reject("too many voice references or keyframes")
		}
		last := -1.0
		for _, frame := range r.Grok.Keyframes {
			ts := frame.TimestampSeconds
			if !finitePositive(ts) || ts >= float64(r.Seconds) || math.Abs(ts*3-math.Round(ts*3)) > 1e-6 || ts <= last {
				return reject("keyframes must have increasing interior timestamps on a one-third second grid")
			}
			if frame.Image.Type != VideoContentTypeImage || (frame.Image.Input == nil) == (strings.TrimSpace(frame.Image.URL) == "") {
				return reject("invalid keyframe image")
			}
			last = ts
		}
		hasPinned = hasPinned || len(r.Grok.Keyframes) > 0
		for _, voice := range r.Grok.VoiceIDs {
			if strings.TrimSpace(voice) == "" || len(voice) > 128 {
				return reject("invalid voice identifier")
			}
		}
	}
	referenceMode := refs > 0 || voices > 0 || hasPinned
	if family == "grok-imagine-video-1.5-lite" && referenceMode {
		return reject("the lite model does not support reference-to-video")
	}
	if family == "grok-imagine-video" && (refs > 7 || voices > 0 || hasPinned || refs > 0 && counts[VideoInputRoleFirstFrame]+counts[VideoInputRoleReferenceImage] > 0 || refs > 0 && r.Seconds > 10) {
		return reject("unsupported classic Grok reference combination")
	}
	if refs > 14 {
		return reject("too many reference images")
	}
	if referenceMode && r.Resolution == "1080p" {
		return reject("reference-to-video is limited to 720p")
	}
	if r.Prompt == "" && counts[VideoInputRoleFirstFrame]+counts[VideoInputRoleReferenceImage]+refs == 0 && !hasPinned {
		return reject("a prompt or visual input is required")
	}
	for _, input := range inputs {
		if !caps.SupportsInput(input.Role, input.MIMEType, input.Size) {
			return reject("unsupported binary input")
		}
	}
	return nil
}
func grokManagedImage(ctx context.Context, item VideoContentItem) (map[string]string, error) {
	if item.Input == nil {
		value := strings.TrimSpace(item.URL)
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return nil, errors.New("image reference must use a public HTTPS URL or an uploaded input")
		}
		return map[string]string{"url": value}, nil
	}
	input := item.Input
	if input.Open == nil || input.Size < 1 || input.Size > MaxContentModerationImageBytes {
		return nil, ErrVideoInputUnsupported
	}
	reader, err := input.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(io.LimitReader(reader, MaxContentModerationImageBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != input.Size {
		return nil, ErrVideoInputUnsupported
	}
	return map[string]string{"url": "data:" + input.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(raw)}, nil
}
func (p *GrokVideoProvider) Create(ctx context.Context, a *Account, r VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	if err := p.ValidateSubmission(a, r, inputs); err != nil {
		return nil, err
	}
	body := map[string]any{"model": r.Model, "prompt": r.Prompt, "duration": r.Seconds, "resolution": r.Resolution}
	if r.Ratio != "" {
		body["aspect_ratio"] = r.Ratio
	}
	if r.AudioEnabled != nil {
		body["generate_audio"] = *r.AudioEnabled
	}
	refs := []map[string]string{}
	for _, item := range r.Content {
		if item.Type == VideoContentTypeText {
			continue
		}
		image, err := grokManagedImage(ctx, item)
		if err != nil {
			return nil, rejectedVideoProviderError("validation", "invalid_image_reference", "video image reference could not be read", 400)
		}
		switch item.Role {
		case VideoInputRoleFirstFrame, VideoInputRoleReferenceImage:
			body["image"] = image
		case VideoInputRoleLastFrame:
			body["last_frame"] = image
		case VideoInputRoleTaggedReferenceImage:
			refs = append(refs, image)
		}
	}
	if len(refs) > 0 {
		body["reference_images"] = refs
	}
	if r.Grok != nil {
		voices := []map[string]string{}
		for _, v := range r.Grok.VoiceIDs {
			voices = append(voices, map[string]string{"voice_id": v})
		}
		if len(voices) > 0 {
			body["reference_audios"] = voices
		}
		frames := []map[string]any{}
		for _, f := range r.Grok.Keyframes {
			image, err := grokManagedImage(ctx, f.Image)
			if err != nil {
				return nil, rejectedVideoProviderError("validation", "invalid_keyframe", "keyframe could not be read", 400)
			}
			frames = append(frames, map[string]any{"image": image, "timestamp_s": f.TimestampSeconds})
		}
		if len(frames) > 0 {
			body["keyframes"] = frames
		}
	}
	return p.submit(ctx, a, GrokMediaEndpointVideosGenerations, body)
}
func (p *GrokVideoProvider) Edit(ctx context.Context, a *Account, r VideoEditRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	r.Operation = VideoOperationEdit
	return p.submitDerived(ctx, a, r.VideoCreateRequest, inputs)
}
func (p *GrokVideoProvider) Extend(ctx context.Context, a *Account, r VideoExtendRequest) (*ProviderVideoTask, error) {
	r.Operation = VideoOperationExtend
	return p.submitDerived(ctx, a, r.VideoCreateRequest, nil)
}
func (p *GrokVideoProvider) submitDerived(ctx context.Context, a *Account, r VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	if err := p.ValidateSubmission(a, r, inputs); err != nil {
		return nil, err
	}
	input := r.Grok.Source.Input
	reader, err := input.Open(ctx)
	if err != nil {
		return nil, rejectedVideoProviderError("input", "source_unavailable", "source video could not be read", 400)
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(io.LimitReader(reader, (100<<20)+1))
	digest := sha256.Sum256(raw)
	if err != nil || int64(len(raw)) != input.Size || len(raw) > 100<<20 || hex.EncodeToString(digest[:]) != input.SHA256 {
		return nil, rejectedVideoProviderError("input", "source_unavailable", "source video could not be read", 400)
	}
	body := map[string]any{"model": r.Model, "prompt": r.Prompt, "video": map[string]string{"url": "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(raw)}}
	endpoint := GrokMediaEndpointVideosEdits
	if r.Operation == VideoOperationExtend {
		body["duration"] = r.Seconds
		endpoint = GrokMediaEndpointVideosExtensions
	}
	return p.submit(ctx, a, endpoint, body)
}
func (p *GrokVideoProvider) submit(ctx context.Context, a *Account, e GrokMediaEndpoint, body map[string]any) (*ProviderVideoTask, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_body", "invalid video request", 400)
	}
	response, err := p.request(ctx, a, e, "", raw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := readGrokManagedBody(response.Body)
	if err != nil {
		return nil, unknownVideoProviderError("upstream", "invalid_acceptance", "could not read video acceptance", nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, grokManagedResponseError(response, true)
	}
	var accepted struct {
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(data, &accepted) != nil || !validVideoProviderIdentifier(accepted.RequestID) {
		return nil, unknownVideoProviderError("upstream", "missing_task_id", "video submission outcome is unknown", nil)
	}
	return &ProviderVideoTask{ProviderTaskID: accepted.RequestID, Status: VideoGenerationQueued, RawStatus: "pending", Metadata: map[string]any{}, SuggestedPollInterval: 5 * time.Second}, nil
}
func (p *GrokVideoProvider) Get(ctx context.Context, a *Account, ref ProviderTaskRef) (*ProviderVideoTask, error) {
	if a == nil || ref.Provider != VideoProviderGrok || ref.AccountID != a.ID || !validVideoProviderIdentifier(ref.ProviderTaskID) {
		return nil, ErrVideoInvalidRequest
	}
	if a.IsPermanentlyUnschedulable() {
		return nil, controlVideoProviderError(ErrVideoNoAccountAvailable)
	}
	response, err := p.request(ctx, a, GrokMediaEndpointVideoStatus, ref.ProviderTaskID, nil)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, grokManagedResponseError(response, false)
	}
	raw, err := readGrokManagedBody(response.Body)
	if err != nil {
		return nil, controlVideoProviderError(errors.New("invalid video status body"))
	}
	task, err := decodeGrokManagedTask(raw, ref.ProviderTaskID)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if task.VideoURL != "" {
		if isGrokMediaVideoContentURL(task.VideoURL, ref.ProviderTaskID) {
			task.VideoURL, err = buildGrokMediaURL(a, p.cfg, GrokMediaEndpointVideoContent, ref.ProviderTaskID)
		} else {
			task.VideoURL, err = normalizeProviderVideoURL(task.VideoURL)
		}
		if err != nil {
			return nil, controlVideoProviderError(errors.New("invalid video content URL"))
		}
	}
	return task, nil
}
func decodeGrokManagedTask(raw []byte, id string) (*ProviderVideoTask, error) {
	var value struct {
		Status   string   `json:"status"`
		Progress *float64 `json:"progress"`
		Model    string   `json:"model"`
		Video    *struct {
			URL        string  `json:"url"`
			Duration   float64 `json:"duration"`
			Moderation *bool   `json:"respect_moderation"`
		} `json:"video"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("invalid Grok status response")
	}
	task := &ProviderVideoTask{ProviderTaskID: id, RawStatus: value.Status, Progress: value.Progress, Metadata: map[string]any{}, Usage: map[string]any{}, SuggestedPollInterval: 5 * time.Second}
	if value.Progress != nil && (!finiteNonNegative(*value.Progress) || *value.Progress > 100) {
		return nil, errors.New("invalid video progress")
	}
	switch value.Status {
	case "pending":
		task.Status = VideoGenerationQueued
		if value.Progress != nil && *value.Progress > 0 {
			task.Status = VideoGenerationInProgress
		}
	case "done":
		if value.Video == nil {
			return nil, errors.New("completed Grok video has no content")
		}
		if value.Video.Moderation != nil {
			task.Metadata["respect_moderation"] = *value.Video.Moderation
		}
		if value.Video.Moderation != nil && !*value.Video.Moderation {
			task.Status = VideoGenerationFailed
			task.ErrorCode = "content_rejected"
			task.ErrorMessage = "video did not pass content moderation"
			break
		}
		if value.Video.URL == "" {
			return nil, errors.New("completed Grok video has no content")
		}
		task.Status = VideoGenerationCompleted
		task.VideoURL = value.Video.URL
		task.ContentVariants = []string{"video"}
		progress := 100.0
		task.Progress = &progress
		// Do not clamp extended output or fractional edited durations to generation's 15s limit.
		if finitePositive(value.Video.Duration) {
			task.Metadata["seconds"] = value.Video.Duration
		}
	case "failed":
		task.Status = VideoGenerationFailed
		task.ErrorCode = "generation_failed"
		task.ErrorMessage = "upstream video generation failed"
	case "expired":
		task.Status = VideoGenerationExpired
		task.ErrorCode = "generation_expired"
		task.ErrorMessage = "upstream video generation expired"
	default:
		return nil, errors.New("unknown Grok video status")
	}
	if model := strings.TrimSpace(value.Model); model != "" {
		task.Metadata["model"] = model
	}
	task.Usage = grokManagedUsageSnapshot(value.Usage)
	return task, nil
}
func readGrokManagedBody(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, grokManagedVideoMaxResponse+1))
	if err == nil && len(b) > grokManagedVideoMaxResponse {
		err = errors.New("video response exceeds limit")
	}
	return b, err
}
func (p *GrokVideoProvider) token(ctx context.Context, a *Account) (string, error) {
	if a == nil || a.Platform != PlatformGrok {
		return "", ErrVideoProviderUnsupported
	}
	if a.Type == AccountTypeAPIKey {
		if token := a.GetCredential("api_key"); token != "" {
			return token, nil
		}
		return "", errors.New("missing video credential")
	}
	if !a.IsGrokOAuth() || p.tokens == nil {
		return "", errors.New("missing Grok OAuth provider")
	}
	return p.tokens.GetAccessToken(ctx, a)
}
func (p *GrokVideoProvider) request(ctx context.Context, a *Account, e GrokMediaEndpoint, id string, body []byte) (*http.Response, error) {
	target, err := buildGrokMediaURL(a, p.cfg, e, id)
	if err != nil {
		return nil, rejectedVideoProviderError("configuration", "invalid_base_url", "Grok upstream URL is unavailable", 503)
	}
	token, err := p.token(ctx, a)
	if err != nil {
		return nil, rejectedVideoProviderError("credential", "credential_unavailable", "Grok video credential is unavailable", 503)
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), e.httpMethod(), target, bytes.NewReader(body))
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_url", "Grok upstream request could not be built", 400)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.IsGrokOAuth() && isGrokCLIProxyTarget(target) {
		applyGrokCLIHeaders(req.Header)
	}
	a.ApplyHeaderOverrides(req.Header)
	applyInternalRelayHeaderFromContext(ctx, a, req.Header)
	proxy := ""
	if a.ProxyID != nil && a.Proxy != nil {
		proxy = a.Proxy.URL()
	}
	response, err := p.upstream.Do(req, proxy, a.ID, a.Concurrency)
	if err != nil {
		return nil, unknownVideoProviderError("transport", "submission_unknown", "Grok request outcome is unknown", nil)
	}
	return response, nil
}

// An HTTP error on a known task is not evidence that generation failed.
// Only a terminal provider payload may release its hold.
func grokManagedResponseError(response *http.Response, submission bool) *VideoProviderError {
	certainty := VideoSubmissionRejected
	if submission && (response.StatusCode >= 500 || response.StatusCode == 408 || response.StatusCode >= 300 && response.StatusCode < 400) {
		certainty = VideoSubmissionUnknown
	}
	retry := time.Duration(0)
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
		retry = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil && date.After(time.Now()) {
		retry = time.Until(date)
	}
	return &VideoProviderError{Kind: "upstream", Code: fmt.Sprintf("grok_http_%d", response.StatusCode), Message: "Grok video upstream request failed", StatusCode: response.StatusCode, Certainty: certainty, Retryable: !submission, RetryAfter: retry}
}
func (*GrokVideoProvider) Delete(context.Context, *Account, ProviderTaskRef) error {
	return ErrVideoCapabilityUnsupported
}
func (p *GrokVideoProvider) OpenContent(ctx context.Context, a *Account, r ProviderContentRequest) (*ProviderContent, error) {
	if a == nil || r.TaskRef.Provider != VideoProviderGrok || r.TaskRef.AccountID != a.ID {
		return nil, ErrVideoInvalidRequest
	}
	if a.IsPermanentlyUnschedulable() {
		return nil, ErrVideoNoAccountAvailable
	}
	if r.Variant != "" && r.Variant != "video" {
		return nil, ErrVideoCapabilityUnsupported
	}
	// Only the exact configured task-content endpoint receives a bearer token.
	expected, err := buildGrokMediaURL(a, p.cfg, GrokMediaEndpointVideoContent, r.TaskRef.ProviderTaskID)
	if err != nil {
		return nil, err
	}
	if r.UpstreamURL == expected {
		method := r.Method
		if method == "" {
			method = http.MethodGet
		}
		if method != http.MethodGet && method != http.MethodHead {
			return nil, ErrVideoInvalidRequest
		}
		token, err := p.token(ctx, a)
		if err != nil {
			return nil, controlVideoProviderError(err)
		}
		req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), method, expected, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Range", r.Range)
		req.Header.Set("If-Range", r.IfRange)
		if a.IsGrokOAuth() && isGrokCLIProxyTarget(expected) {
			applyGrokCLIHeaders(req.Header)
		}
		a.ApplyHeaderOverrides(req.Header)
		applyInternalRelayHeaderFromContext(ctx, a, req.Header)
		proxy := ""
		if a.ProxyID != nil && a.Proxy != nil {
			proxy = a.Proxy.URL()
		}
		resp, err := p.upstream.Do(req, proxy, a.ID, a.Concurrency)
		if err != nil {
			return nil, controlVideoProviderError(errors.New("grok content transport failed"))
		}
		if resp.StatusCode != 200 && resp.StatusCode != 206 && resp.StatusCode != 416 {
			_ = resp.Body.Close()
			return nil, grokManagedResponseError(resp, false)
		}
		return &ProviderContent{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
	}
	return p.content.openExternalVideoContent(ctx, a, r)
}
