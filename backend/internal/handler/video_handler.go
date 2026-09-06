package handler

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
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
	"github.com/gin-gonic/gin"
)

const (
	videoJSONBodyMaxBytes    = int64(1 << 20)
	videoScalarFieldMaxBytes = int64(64 << 10)
	videoMultipartMaxParts   = 64
)

type videoTaskAPI interface {
	Submit(context.Context, service.VideoSubmitRequest) (*service.VideoSubmitResult, error)
	GetForOwner(context.Context, int64, string) (*service.VideoTask, error)
	GetContentTaskForOwner(context.Context, int64, string) (*service.VideoTask, error)
	GetContentTaskByURLForOwner(context.Context, int64, string) (*service.VideoTask, error)
	GetContentTaskByURL(context.Context, string) (*service.VideoTask, error)
	VideoURLForOwner(context.Context, int64, string) (string, error)
	ListForOwner(context.Context, int64, service.VideoTaskFilter) (*service.VideoTaskPage, error)
	GetCharacterForOwner(context.Context, int64, string) (*service.VideoResource, error)
	DisclosureForOwner(context.Context, int64, string) (*service.VideoTaskDisclosure, error)
	ResourceDisclosureForOwner(context.Context, int64, string) (*service.VideoResourceDisclosure, error)
	OpenContentForOwner(context.Context, int64, string, service.ProviderContentRequest) (*service.ProviderContent, error)
	OpenContentForTask(context.Context, *service.VideoTask, service.ProviderContentRequest) (*service.ProviderContent, error)
	DeleteForOwner(context.Context, int64, string) (*service.VideoTask, error)
	DeleteCharacterForOwner(context.Context, int64, string) error
}

type videoGrokMediaHandler interface {
	GrokVideoGeneration(*gin.Context)
	GrokVideoEdit(*gin.Context)
	GrokVideoExtension(*gin.Context)
}

type VideoHandler struct {
	tasks         videoTaskAPI
	spool         *service.VideoSubmissionSpool
	cfg           *config.Config
	openAI        *OpenAIGatewayHandler
	grok          videoGrokMediaHandler
	webhooks      *service.VideoWebhookService
	contentLimits *videoContentLimiter
}

func NewVideoHandler(tasks *service.VideoTaskService, spool *service.VideoSubmissionSpool, cfg *config.Config, openAI *OpenAIGatewayHandler, webhooks *service.VideoWebhookService) *VideoHandler {
	handler := newVideoHandler(tasks, spool, cfg)
	handler.openAI = openAI
	handler.grok = openAI
	handler.webhooks = webhooks
	return handler
}

func newVideoHandler(tasks videoTaskAPI, spool *service.VideoSubmissionSpool, cfg *config.Config) *VideoHandler {
	return &VideoHandler{
		tasks:         tasks,
		spool:         spool,
		cfg:           cfg,
		contentLimits: newVideoContentLimiter(cfg),
	}
}

func (h *VideoHandler) Create(c *gin.Context) {
	h.submit(c, service.VideoOperationGenerate)
}

func (h *VideoHandler) Edit(c *gin.Context) {
	h.submit(c, service.VideoOperationEdit)
}

func (h *VideoHandler) Extend(c *gin.Context) {
	h.submit(c, service.VideoOperationExtend)
}

func (h *VideoHandler) CreateCharacter(c *gin.Context) {
	h.submit(c, service.VideoOperationCharacterCreate)
}

func (h *VideoHandler) submit(c *gin.Context, operation string) {
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return
	}
	if h == nil || h.tasks == nil || h.cfg == nil || !h.cfg.Gateway.Video.Enabled {
		videoError(c, service.ErrVideoDisabled)
		return
	}
	if nativeVideoReleasePolicyApplies(c) {
		if err := service.ValidateVideoReleaseOperation(operation); err != nil {
			videoError(c, err)
			return
		}
	}
	if !h.cfg.Gateway.Video.CreationEnabled && strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		videoError(c, service.ErrVideoCreationDisabled)
		return
	}
	request, session, multipartHash, err := h.parseSubmitRequest(c, operation, apiKey.UserID)
	if session != nil {
		defer func() {
			if closeErr := session.Close(); closeErr != nil {
				slog.Error("video submission spool cleanup failed", "error", closeErr)
			}
		}()
	}
	if err != nil {
		videoError(c, err)
		return
	}
	request.APIKey = apiKey
	request.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateVideoIdempotencyKey(request.IdempotencyKey); err != nil {
		videoError(c, err)
		return
	}
	if multipartHash != "" && h.routeParsedCompositeMultipart(c, apiKey, &request) {
		return
	}
	if err := service.ValidateVideoReleaseSubmission(request); err != nil {
		videoError(c, err)
		return
	}
	if multipartHash != "" && request.IdempotencyKey != "" {
		if _, exists := service.VideoCreateSessionFromContext(c.Request.Context()); !exists {
			api, available := h.tasks.(videoCreateIntentHashAPI)
			if !available {
				videoError(c, service.ErrBillingServiceUnavailable)
				return
			}
			intentSession, beginErr := api.BeginVideoCreateIntentWithHash(c.Request.Context(), apiKey, operation,
				request.IdempotencyKey, multipartHash, service.VideoCreateIntentMultipartContract)
			if beginErr != nil || intentSession == nil || intentSession.Intent == nil {
				if beginErr == nil {
					beginErr = service.ErrBillingServiceUnavailable
				}
				if errors.Is(beginErr, service.ErrVideoCreateInProgress) {
					c.Header("Retry-After", "3")
				}
				videoError(c, beginErr)
				return
			}
			runParsed := func() { h.submitParsed(c, apiKey, request, true) }
			h.executeVideoCreateIntentSession(c, intentSession, runParsed, runParsed)
			return
		}
	}
	h.submitParsed(c, apiKey, request, multipartHash != "")
}

func (h *VideoHandler) submitParsed(c *gin.Context, apiKey *service.APIKey, request service.VideoSubmitRequest, multipartRequest bool) {
	if request.Model == "" && request.Operation == service.VideoOperationCharacterCreate {
		request.Model = service.OpenAIVideoModelSora2
	}
	if multipartRequest && h.routeParsedCompositeMultipart(c, apiKey, &request) {
		return
	}
	if err := service.ValidateVideoReleaseSubmission(request); err != nil {
		videoError(c, err)
		return
	}
	if request.Operation != service.VideoOperationCharacterCreate && !h.checkSecurityAudit(c, apiKey, request) {
		return
	}

	result, err := h.tasks.Submit(c.Request.Context(), request)
	if err != nil {
		videoError(c, err)
		return
	}
	if result == nil {
		videoError(c, errors.New("video service returned an empty result"))
		return
	}
	h.setNoStore(c)
	if result.Resource != nil {
		response, projectErr := h.projectResource(c.Request.Context(), apiKey.UserID, result.Resource)
		if projectErr != nil {
			videoError(c, projectErr)
			return
		}
		c.PureJSON(http.StatusOK, response)
		return
	}
	response, err := h.projectTask(c.Request.Context(), apiKey.UserID, result.Task, c.Request)
	if err != nil {
		videoError(c, err)
		return
	}
	c.PureJSON(http.StatusOK, response)
}

func (h *VideoHandler) routeParsedCompositeMultipart(c *gin.Context, apiKey *service.APIKey, request *service.VideoSubmitRequest) bool {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.Group == nil || request == nil || apiKey.Group.Platform != service.PlatformComposite {
		return false
	}
	platform, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	if !resolved {
		router, available := h.tasks.(videoIngressRouter)
		if !available {
			videoError(c, service.ErrBillingServiceUnavailable)
			return true
		}
		route, err := router.ResolveVideoIngress(c.Request.Context(), apiKey, request.Operation, request.Model, request.SourceVideoID, request.IdempotencyKey)
		if err != nil || route == nil {
			if err == nil {
				err = service.ErrVideoNoAccountAvailable
			}
			videoError(c, err)
			return true
		}
		if route.ManagedReplay || route.ResolveAfterParsing {
			c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformOpenAI))
		} else {
			c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), route.Decision))
			if upstreamModel := strings.TrimSpace(route.Decision.UpstreamModel); route.Decision.TargetPlatform == service.PlatformGrok && upstreamModel != "" {
				request.Model = upstreamModel
			}
		}
		platform, resolved = service.ResolvedTargetPlatformFromContext(c.Request.Context())
	}
	if !resolved {
		videoError(c, service.ErrVideoNoAccountAvailable)
		return true
	}
	if platform == service.PlatformOpenAI {
		return false
	}
	if platform != service.PlatformGrok || request.Operation == service.VideoOperationCharacterCreate || h.grok == nil {
		videoError(c, service.ErrVideoCapabilityUnsupported)
		return true
	}
	if err := checkGrokMediaCreationKey(apiKey); err != nil {
		videoError(c, err)
		return true
	}
	body, contentType, err := videoGrokMultipartBody(c.Request.Context(), *request)
	if err != nil {
		videoError(c, err)
		return true
	}
	defer func() { _ = body.Close() }()
	limit := int64(service.VideoCreateMultipartMaxBytes)
	if h.cfg != nil && h.cfg.Gateway.MaxBodySize > 0 && h.cfg.Gateway.MaxBodySize < limit {
		limit = h.cfg.Gateway.MaxBodySize
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, body, limit)
	c.Request.ContentLength = -1
	c.Request.GetBody = nil
	c.Request.Header.Set("Content-Type", contentType)
	switch request.Operation {
	case service.VideoOperationGenerate:
		h.grok.GrokVideoGeneration(c)
	case service.VideoOperationEdit:
		h.grok.GrokVideoEdit(c)
	case service.VideoOperationExtend:
		h.grok.GrokVideoExtension(c)
	default:
		videoError(c, service.ErrVideoCapabilityUnsupported)
	}
	return true
}

func videoGrokMultipartBody(ctx context.Context, request service.VideoSubmitRequest) (io.ReadCloser, string, error) {
	if ctx == nil || request.Operation == service.VideoOperationCharacterCreate {
		return nil, "", service.ErrVideoCapabilityUnsupported
	}
	for _, input := range request.Inputs {
		if input.Open == nil || !service.IsValidVideoInputRole(input.Role) || strings.TrimSpace(input.FileName) == "" || strings.TrimSpace(input.MIMEType) == "" {
			return nil, "", service.ErrVideoInvalidRequest
		}
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	contentType := multipartWriter.FormDataContentType()
	go func() {
		writeErr := writeVideoGrokMultipart(ctx, multipartWriter, request)
		if closeErr := multipartWriter.Close(); writeErr == nil {
			writeErr = closeErr
		}
		_ = writer.CloseWithError(writeErr)
	}()
	return reader, contentType, nil
}

func writeVideoGrokMultipart(ctx context.Context, writer *multipart.Writer, request service.VideoSubmitRequest) error {
	writeField := func(name, value string) error {
		if value == "" {
			return nil
		}
		return writer.WriteField(name, value)
	}
	if err := writeField("model", request.Model); err != nil {
		return err
	}
	if err := writeField("prompt", request.Prompt); err != nil {
		return err
	}
	if request.Seconds > 0 {
		if err := writeField("seconds", strconv.Itoa(request.Seconds)); err != nil {
			return err
		}
	}
	for _, field := range []struct{ name, value string }{
		{"size", request.Size}, {"quality", request.Quality}, {"service_tier", request.ServiceTier}, {"callback_url", request.CallbackURL},
	} {
		if err := writeField(field.name, field.value); err != nil {
			return err
		}
	}
	if request.AudioEnabled != nil {
		if err := writeField("audio", strconv.FormatBool(*request.AudioEnabled)); err != nil {
			return err
		}
	}
	if len(request.CharacterIDs) > 0 {
		characters := make([]videoIDReference, 0, len(request.CharacterIDs))
		for _, id := range request.CharacterIDs {
			characters = append(characters, videoIDReference{ID: id})
		}
		encoded, err := json.Marshal(characters)
		if err != nil || writeField("characters", string(encoded)) != nil {
			return service.ErrVideoInvalidRequest
		}
	}
	if len(request.ProviderOptions) > 0 {
		encoded, err := json.Marshal(request.ProviderOptions)
		if err != nil || writeField("provider_options", string(encoded)) != nil {
			return service.ErrVideoInvalidRequest
		}
		if name, _ := request.ProviderOptions["name"].(string); request.Operation == service.VideoOperationCharacterCreate {
			if err := writeField("name", name); err != nil {
				return err
			}
		}
	}
	for _, input := range request.Inputs {
		fieldName := string(input.Role)
		if input.Role == service.VideoInputRoleSourceVideo || input.Role == service.VideoInputRoleCharacterClip {
			fieldName = "video"
		}
		mediaType, parameters, err := mime.ParseMediaType(input.MIMEType)
		if err != nil {
			return service.ErrVideoInvalidRequest
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": fieldName, "filename": input.FileName}))
		header.Set("Content-Type", mime.FormatMediaType(mediaType, parameters))
		part, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		source, err := input.Open(ctx)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(part, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (h *VideoHandler) Retrieve(c *gin.Context) {
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	task, err := h.tasks.GetForOwner(c.Request.Context(), apiKey.UserID, videoPathID(c))
	if err != nil {
		if errors.Is(err, service.ErrVideoTaskNotFound) {
			h.ContentURL(c)
			return
		}
		videoError(c, err)
		return
	}
	response, err := h.projectTask(c.Request.Context(), apiKey.UserID, task, c.Request)
	if err != nil {
		videoError(c, err)
		return
	}
	h.setNoStore(c)
	if service.ProjectVideoStatus(task) == service.VideoGenerationQueued || service.ProjectVideoStatus(task) == service.VideoGenerationInProgress {
		c.Header("Retry-After", "10")
	}
	c.PureJSON(http.StatusOK, response)
}

func (h *VideoHandler) List(c *gin.Context) {
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 100 {
			videoError(c, service.ErrVideoInvalidRequest)
			return
		}
		limit = parsed
	}
	order := strings.ToLower(strings.TrimSpace(c.Query("order")))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		videoError(c, service.ErrVideoInvalidRequest)
		return
	}
	page, err := h.tasks.ListForOwner(c.Request.Context(), apiKey.UserID, service.VideoTaskFilter{
		Status: strings.TrimSpace(c.Query("status")), Model: strings.TrimSpace(c.Query("model")),
		Operation: strings.TrimSpace(c.Query("operation")), Limit: limit,
		After: strings.TrimSpace(c.Query("after")), Order: order,
	})
	if err != nil {
		videoError(c, err)
		return
	}
	data := make([]videoTaskResponse, 0, len(page.Data))
	for _, task := range page.Data {
		item, projectErr := h.projectTask(c.Request.Context(), apiKey.UserID, task, c.Request)
		if projectErr != nil {
			videoError(c, projectErr)
			return
		}
		data = append(data, item)
	}
	response := videoListResponse{Object: "list", Data: data, HasMore: page.HasMore, After: page.After}
	if len(data) > 0 {
		response.FirstID = data[0].ID
		response.LastID = data[len(data)-1].ID
	}
	h.setNoStore(c)
	c.PureJSON(http.StatusOK, response)
}

func (h *VideoHandler) Delete(c *gin.Context) {
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	id := videoPathID(c)
	task, err := h.tasks.DeleteForOwner(c.Request.Context(), apiKey.UserID, id)
	if err != nil {
		videoError(c, err)
		return
	}
	h.setNoStore(c)
	if task.DeleteState != service.VideoDeleteDeleted {
		c.Header("Retry-After", "3")
		videoError(c, service.ErrVideoDeletePending)
		return
	}
	c.JSON(http.StatusOK, videoDeletedResponse{ID: task.PublicID, Object: "video.deleted", Deleted: true})
}

func (h *VideoHandler) GetCharacter(c *gin.Context) {
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	resource, err := h.tasks.GetCharacterForOwner(c.Request.Context(), apiKey.UserID, videoPathID(c))
	if err != nil {
		if errors.Is(err, service.ErrVideoSettlementPending) || errors.Is(err, service.ErrVideoDeletePending) {
			c.Header("Retry-After", "3")
		}
		videoError(c, err)
		return
	}
	response, err := h.projectResource(c.Request.Context(), apiKey.UserID, resource)
	if err != nil {
		videoError(c, err)
		return
	}
	h.setNoStore(c)
	c.JSON(http.StatusOK, response)
}

func (h *VideoHandler) DeleteCharacter(c *gin.Context) {
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	id := videoPathID(c)
	err := h.tasks.DeleteCharacterForOwner(c.Request.Context(), apiKey.UserID, id)
	if errors.Is(err, service.ErrVideoDeletePending) || errors.Is(err, service.ErrVideoSettlementPending) {
		c.Header("Retry-After", "3")
		videoError(c, err)
		return
	}
	if err != nil {
		videoError(c, err)
		return
	}
	h.setNoStore(c)
	c.JSON(http.StatusOK, videoDeletedResponse{ID: id, Object: "video.deleted", Deleted: true})
}

func (h *VideoHandler) Content(c *gin.Context) {
	startedAt := time.Now()
	variant := strings.TrimSpace(c.Query("variant"))
	defer func() {
		observability.DefaultVideoMetrics().RecordContent(variant, c.Writer.Status(), time.Since(startedAt))
	}()
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	if h.cfg == nil || !h.cfg.Gateway.Video.ContentProxy.Enabled {
		videoError(c, service.ErrVideoDisabled)
		return
	}
	id := videoPathID(c)
	task, err := h.tasks.GetContentTaskForOwner(c.Request.Context(), apiKey.UserID, id)
	if err != nil {
		videoError(c, err)
		return
	}
	if id != task.PublicID {
		variant = "video"
	}
	h.serveVideoContent(c, task, variant, false, func(ctx context.Context, request service.ProviderContentRequest) (*service.ProviderContent, error) {
		return h.tasks.OpenContentForOwner(ctx, apiKey.UserID, id, request)
	})
}

func (h *VideoHandler) ContentURL(c *gin.Context) {
	startedAt := time.Now()
	variant := "video"
	defer func() {
		observability.DefaultVideoMetrics().RecordContent(variant, c.Writer.Status(), time.Since(startedAt))
	}()
	if !h.readEnabled(c) {
		return
	}
	if h.cfg == nil || !h.cfg.Gateway.Video.ContentProxy.Enabled {
		videoError(c, service.ErrVideoDisabled)
		return
	}
	task, err := h.tasks.GetContentTaskByURL(c.Request.Context(), c.Request.URL.RequestURI())
	if err != nil {
		videoError(c, err)
		return
	}
	h.serveVideoContent(c, task, variant, true, func(ctx context.Context, request service.ProviderContentRequest) (*service.ProviderContent, error) {
		return h.tasks.OpenContentForTask(ctx, task, request)
	})
}

func (h *VideoHandler) PublicContentProxy(c *gin.Context) {
	if c == nil {
		return
	}
	if h == nil || c.Request == nil ||
		(c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) ||
		h.cfg == nil || !h.cfg.Gateway.Video.Enabled || !h.cfg.Gateway.Video.ContentProxy.Enabled || h.tasks == nil {
		c.Next()
		return
	}
	task, err := h.tasks.GetContentTaskByURL(c.Request.Context(), c.Request.URL.RequestURI())
	if errors.Is(err, service.ErrVideoTaskNotFound) {
		c.Next()
		return
	}
	if err != nil {
		videoError(c, err)
		c.Abort()
		return
	}
	h.serveVideoContent(c, task, "video", true, func(ctx context.Context, request service.ProviderContentRequest) (*service.ProviderContent, error) {
		return h.tasks.OpenContentForTask(ctx, task, request)
	})
	c.Abort()
}

type videoContentOpener func(context.Context, service.ProviderContentRequest) (*service.ProviderContent, error)

func (h *VideoHandler) serveVideoContent(c *gin.Context, task *service.VideoTask, variant string, inline bool, open videoContentOpener) {
	if task == nil || open == nil {
		videoError(c, service.ErrVideoTaskNotFound)
		return
	}
	if task.AccountID == nil {
		videoError(c, service.ErrVideoContentNotReady)
		return
	}
	release, ok := h.contentLimits.acquire(task.UserID, *task.AccountID)
	if !ok {
		videoJSONError(c, http.StatusTooManyRequests, "video_content_concurrency_limited", "video content download concurrency limit exceeded")
		return
	}
	observability.DefaultVideoMetrics().AddContentActive(1)
	defer observability.DefaultVideoMetrics().AddContentActive(-1)
	defer release()

	rangeValue := strings.TrimSpace(c.GetHeader("Range"))
	if !validSingleByteRange(rangeValue) {
		videoJSONError(c, http.StatusRequestedRangeNotSatisfiable, "video_invalid_range", "only a single byte range is supported")
		return
	}
	ifRange := strings.TrimSpace(c.GetHeader("If-Range"))
	if len(ifRange) > 1024 {
		videoJSONError(c, http.StatusBadRequest, "video_invalid_if_range", "If-Range is too large")
		return
	}
	totalTimeout := time.Hour
	if h.cfg.Gateway.Video.ContentProxy.TotalTimeoutSeconds > 0 {
		totalTimeout = time.Duration(h.cfg.Gateway.Video.ContentProxy.TotalTimeoutSeconds) * time.Second
	}
	streamCtx, cancelStream := context.WithTimeout(c.Request.Context(), totalTimeout)
	defer cancelStream()
	upstreamStartedAt := time.Now()
	content, err := open(streamCtx, service.ProviderContentRequest{
		Variant: variant, Method: c.Request.Method, Range: rangeValue, IfRange: ifRange,
		ResponseHeaderTimeout: time.Duration(h.cfg.Gateway.Video.ContentProxy.ResponseHeaderTimeoutSeconds) * time.Second,
	})
	observability.DefaultVideoMetrics().RecordContentTTFB(variant, time.Since(upstreamStartedAt))
	if err != nil {
		observability.DefaultVideoMetrics().RecordContentStream(variant, "upstream_error", 0)
		videoError(c, err)
		return
	}
	if content == nil || content.Body == nil {
		observability.DefaultVideoMetrics().RecordContentStream(variant, "upstream_error", 0)
		videoError(c, errors.New("video provider returned no content body"))
		return
	}
	defer func() { _ = content.Body.Close() }()
	copyVideoContentHeaders(c.Writer.Header(), content.Header)
	h.setNoStore(c)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename=%q`, disposition, videoContentFilename(task.PublicID, c.Query("variant"))))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(content.StatusCode)
	if c.Request.Method == http.MethodHead {
		observability.DefaultVideoMetrics().RecordContentStream(variant, "success", 0)
		return
	}
	idleTimeout := 60 * time.Second
	if h.cfg != nil && h.cfg.Gateway.Video.ContentProxy.IdleTimeoutSeconds > 0 {
		idleTimeout = time.Duration(h.cfg.Gateway.Video.ContentProxy.IdleTimeoutSeconds) * time.Second
	}
	written, streamErr := copyVideoContentWithIdleTimeout(streamCtx, c.Writer, content.Body, cancelStream, idleTimeout)
	observability.DefaultVideoMetrics().RecordContentStream(variant, videoContentStreamResult(streamErr, streamCtx), written)
}

var errVideoContentIdleTimeout = errors.New("video content stream idle timeout")
var errVideoContentUpstream = errors.New("video content upstream read failed")
var errVideoContentDownstream = errors.New("video content downstream write failed")

type videoContentReadResult struct {
	data []byte
	err  error
}

func copyVideoContentWithIdleTimeout(ctx context.Context, destination io.Writer, source io.ReadCloser, cancel context.CancelFunc, idleTimeout time.Duration) (int64, error) {
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	readResults := make(chan videoContentReadResult)
	acknowledged := make(chan struct{})
	go func() {
		buffer := make([]byte, 64*1024)
		for {
			n, err := source.Read(buffer)
			result := videoContentReadResult{data: buffer[:n], err: err}
			select {
			case readResults <- result:
			case <-ctx.Done():
				return
			}
			if n > 0 {
				select {
				case <-acknowledged:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()
	defer clearVideoContentWriteDeadline(destination)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		case <-timer.C:
			cancel()
			_ = source.Close()
			return written, errVideoContentIdleTimeout
		case result := <-readResults:
			if len(result.data) > 0 {
				setVideoContentWriteDeadline(destination, time.Now().Add(idleTimeout))
				n, err := destination.Write(result.data)
				written += int64(n)
				select {
				case acknowledged <- struct{}{}:
				case <-ctx.Done():
					return written, ctx.Err()
				}
				if err != nil {
					streamErr := fmt.Errorf("%w: %v", errVideoContentDownstream, err)
					if ctxErr := ctx.Err(); ctxErr != nil {
						streamErr = ctxErr
					}
					cancel()
					_ = source.Close()
					return written, streamErr
				}
				if n != len(result.data) {
					streamErr := fmt.Errorf("%w: %v", errVideoContentDownstream, io.ErrShortWrite)
					if ctxErr := ctx.Err(); ctxErr != nil {
						streamErr = ctxErr
					}
					cancel()
					_ = source.Close()
					return written, streamErr
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idleTimeout)
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return written, nil
				}
				return written, fmt.Errorf("%w: %v", errVideoContentUpstream, result.err)
			}
		}
	}
}

func videoContentStreamResult(err error, streamCtx context.Context) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, errVideoContentIdleTimeout) {
		return "idle_timeout"
	}
	if errors.Is(err, errVideoContentDownstream) {
		return "downstream_error"
	}
	if errors.Is(err, errVideoContentUpstream) {
		return "upstream_error"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(streamCtx.Err(), context.DeadlineExceeded) {
		return "total_timeout"
	}
	if errors.Is(err, context.Canceled) || errors.Is(streamCtx.Err(), context.Canceled) {
		return "client_abort"
	}
	return "upstream_error"
}

func setVideoContentWriteDeadline(writer io.Writer, deadline time.Time) {
	if responseWriter, ok := writer.(http.ResponseWriter); ok {
		_ = http.NewResponseController(responseWriter).SetWriteDeadline(deadline)
	}
}

func clearVideoContentWriteDeadline(writer io.Writer) {
	setVideoContentWriteDeadline(writer, time.Time{})
}

func (h *VideoHandler) ProviderWebhook(c *gin.Context) {
	if !service.VideoCallbacksAvailable() {
		videoError(c, service.ErrVideoCallbacksDisabled)
		return
	}
	if h == nil || h.webhooks == nil || h.cfg == nil || !h.cfg.Gateway.Video.Enabled {
		videoError(c, service.ErrVideoDisabled)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	accountID, err := strconv.ParseInt(strings.TrimSpace(c.Param("account_id")), 10, 64)
	if err != nil || accountID <= 0 || provider == "" {
		videoError(c, service.ErrVideoInvalidRequest)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, videoJSONBodyMaxBytes+1))
	if err != nil {
		videoError(c, service.ErrVideoInvalidRequest)
		return
	}
	if int64(len(body)) > videoJSONBodyMaxBytes {
		videoError(c, service.ErrVideoInputTooLarge)
		return
	}
	if _, err := h.webhooks.Handle(c.Request.Context(), provider, accountID, service.ProviderWebhookRequest{
		Headers: c.Request.Header.Clone(), Body: body,
	}); err != nil {
		videoError(c, err)
		return
	}
	h.setNoStore(c)
	c.JSON(http.StatusOK, gin.H{"received": true})
}

func (h *VideoHandler) parseSubmitRequest(c *gin.Context, operation string, userID int64) (service.VideoSubmitRequest, *service.VideoSpoolSession, string, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return service.VideoSubmitRequest{}, nil, "", service.ErrVideoInvalidRequest
	}
	if encoding := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Encoding"))); encoding != "" && encoding != "identity" {
		return service.VideoSubmitRequest{}, nil, "", service.ErrVideoInvalidRequest
	}
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		return service.VideoSubmitRequest{}, nil, "", service.ErrVideoInvalidRequest
	}
	switch strings.ToLower(mediaType) {
	case "application/json":
		if nativeVideoReleasePolicyApplies(c) {
			raw, err := io.ReadAll(io.LimitReader(c.Request.Body, videoJSONBodyMaxBytes+1))
			if err != nil {
				return service.VideoSubmitRequest{}, nil, "", service.ErrVideoInvalidRequest
			}
			if int64(len(raw)) > videoJSONBodyMaxBytes {
				return service.VideoSubmitRequest{}, nil, "", service.ErrVideoInputTooLarge
			}
			if err := service.ValidateVideoReleaseJSON(operation, raw); err != nil {
				return service.VideoSubmitRequest{}, nil, "", err
			}
			request, err := parseVideoJSONRequest(bytes.NewReader(raw), operation)
			return request, nil, "", err
		}
		request, err := parseVideoJSONRequest(c.Request.Body, operation)
		return request, nil, "", err
	case "multipart/form-data":
		enforceRelease := nativeVideoReleasePolicyApplies(c)
		if operation == service.VideoOperationExtend || strings.TrimSpace(params["boundary"]) == "" {
			return service.VideoSubmitRequest{}, nil, "", service.ErrVideoInvalidRequest
		}
		return h.parseVideoMultipart(c.Request.Context(), multipart.NewReader(c.Request.Body, params["boundary"]), operation, userID, enforceRelease)
	default:
		return service.VideoSubmitRequest{}, nil, "", service.ErrVideoInvalidRequest
	}
}

func (h *VideoHandler) parseVideoMultipart(ctx context.Context, reader *multipart.Reader, operation string, userID int64, enforceRelease bool) (_ service.VideoSubmitRequest, session *service.VideoSpoolSession, requestHash string, returnErr error) {
	values := make(map[string]string)
	seenParts := make(map[string]bool)
	inputs := make([]service.VideoInput, 0, 2)
	canonicalParts := make([]service.VideoCreateMultipartPart, 0, 4)
	defer func() {
		if returnErr != nil && session != nil {
			_ = session.Close()
			session = nil
		}
	}()
	for partCount := 0; ; partCount++ {
		part, err := reader.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
		}
		if partCount >= videoMultipartMaxParts {
			_ = part.Close()
			return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
		}
		for header, headerValues := range part.Header {
			if len(headerValues) != 1 || (header != "Content-Disposition" && header != "Content-Type") {
				_ = part.Close()
				return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
			}
		}
		disposition, dispositionParams, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if err != nil || !strings.EqualFold(disposition, "form-data") {
			_ = part.Close()
			return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
		}
		name := dispositionParams["name"]
		if enforceRelease {
			if err := service.ValidateVideoReleaseField(name); err != nil {
				_ = part.Close()
				return service.VideoSubmitRequest{}, session, "", err
			}
		}
		filename, isFile := dispositionParams["filename"]
		if name == "" || name != strings.TrimSpace(name) || !utf8.ValidString(name) || hasControlCharacter(name) ||
			!utf8.ValidString(filename) || hasControlCharacter(filename) || (isFile && strings.TrimSpace(filename) == "") {
			_ = part.Close()
			return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
		}
		for attribute := range dispositionParams {
			if attribute != "name" && attribute != "filename" {
				_ = part.Close()
				return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
			}
		}
		previousFile, duplicate := seenParts[name]
		if duplicate && (!isFile || !previousFile) {
			_ = part.Close()
			return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
		}
		seenParts[name] = isFile
		partType := part.Header.Get("Content-Type")
		if partType != "" {
			partMediaType, partParameters, parseErr := mime.ParseMediaType(partType)
			if parseErr != nil {
				_ = part.Close()
				return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
			}
			partType = mime.FormatMediaType(partMediaType, partParameters)
		}
		if isFile {
			role, allowed := videoMultipartRole(operation, name)
			if !allowed || strings.TrimSpace(filename) == "" || h == nil || h.spool == nil {
				_ = part.Close()
				return service.VideoSubmitRequest{}, session, "", service.ErrVideoInputUnsupported
			}
			if session == nil {
				session, err = h.spool.Begin(ctx, userID)
				if err != nil {
					_ = part.Close()
					return service.VideoSubmitRequest{}, nil, "", err
				}
			}
			input, err := session.Store(ctx, role, filename, partType, part)
			closeErr := part.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return service.VideoSubmitRequest{}, session, "", err
			}
			inputs = append(inputs, input)
			canonicalParts = append(canonicalParts, service.VideoCreateMultipartPart{Name: name, File: true, Filename: filename,
				ContentType: partType, Size: input.Size, Digest: input.SHA256})
			continue
		}
		if !allowedVideoMultipartScalar(operation, name) {
			_ = part.Close()
			return service.VideoSubmitRequest{}, session, "", service.ErrVideoInvalidRequest
		}
		value, err := readVideoScalar(part)
		closeErr := part.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return service.VideoSubmitRequest{}, session, "", err
		}
		values[name] = value
		digest := sha256.Sum256([]byte(value))
		canonicalParts = append(canonicalParts, service.VideoCreateMultipartPart{Name: name, ContentType: partType,
			Size: int64(len(value)), Digest: hex.EncodeToString(digest[:])})
	}
	request, err := videoSubmitRequestFromMultipart(values, inputs, operation)
	if err != nil {
		return service.VideoSubmitRequest{}, session, "", err
	}
	requestHash, err = service.CanonicalVideoCreateMultipartPartsHash(canonicalParts)
	if err != nil {
		return service.VideoSubmitRequest{}, session, "", err
	}
	return request, session, requestHash, nil
}

type videoFlexibleInt int

func (value *videoFlexibleInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	var raw any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	var text string
	switch typed := raw.(type) {
	case json.Number:
		text = typed.String()
	case string:
		text = typed
	default:
		return errors.New("seconds must be an integer or integer string")
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || parsed < 0 {
		return errors.New("seconds must be a non-negative integer")
	}
	*value = videoFlexibleInt(parsed)
	return nil
}

type videoIDReference struct {
	ID string `json:"id"`
}

func (reference *videoIDReference) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] == '"' {
		return json.Unmarshal(data, &reference.ID)
	}
	type alias videoIDReference
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*alias)(reference))
}

type videoInputReferenceJSON struct {
	FileID   string `json:"file_id,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type videoURLReferenceJSON string

func (reference *videoURLReferenceJSON) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return errors.New("media URL is required")
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*reference = videoURLReferenceJSON(value)
		return nil
	}
	var value struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	*reference = videoURLReferenceJSON(value.URL)
	return nil
}

type videoContentItemJSON struct {
	Type     string                `json:"type"`
	Text     string                `json:"text,omitempty"`
	Role     string                `json:"role,omitempty"`
	ImageURL videoURLReferenceJSON `json:"image_url,omitempty"`
	VideoURL videoURLReferenceJSON `json:"video_url,omitempty"`
	AudioURL videoURLReferenceJSON `json:"audio_url,omitempty"`
}

type videoJSONRequest struct {
	Model           string                   `json:"model"`
	Prompt          string                   `json:"prompt"`
	Content         []videoContentItemJSON   `json:"content,omitempty"`
	Seconds         videoFlexibleInt         `json:"seconds,omitempty"`
	Size            string                   `json:"size,omitempty"`
	Quality         string                   `json:"quality,omitempty"`
	Audio           *bool                    `json:"audio,omitempty"`
	ServiceTier     string                   `json:"service_tier,omitempty"`
	InputReference  *videoInputReferenceJSON `json:"input_reference,omitempty"`
	Ratio           string                   `json:"ratio,omitempty"`
	AspectRatio     string                   `json:"aspect_ratio,omitempty"`
	ImageURL        string                   `json:"image_url,omitempty"`
	FirstImageURL   string                   `json:"first_image_url,omitempty"`
	LastImageURL    string                   `json:"last_image_url,omitempty"`
	ReferenceImages []string                 `json:"reference_images,omitempty"`
	ReferenceVideos []string                 `json:"reference_videos,omitempty"`
	ReferenceAudios []string                 `json:"reference_audios,omitempty"`
	Characters      []videoIDReference       `json:"characters,omitempty"`
	Video           *videoIDReference        `json:"video,omitempty"`
	Name            string                   `json:"name,omitempty"`
	CallbackURL     string                   `json:"callback_url,omitempty"`
	ProviderOptions map[string]any           `json:"provider_options,omitempty"`
	presentFields   map[string]bool
}

func parseVideoJSONRequest(body io.Reader, operation string) (service.VideoSubmitRequest, error) {
	raw, err := io.ReadAll(io.LimitReader(body, videoJSONBodyMaxBytes+1))
	if err != nil {
		return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
	}
	if int64(len(raw)) > videoJSONBodyMaxBytes {
		return service.VideoSubmitRequest{}, service.ErrVideoInputTooLarge
	}
	if err := jsonstrict.RejectDuplicateKeys(raw); err != nil {
		return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload videoJSONRequest
	if err := decoder.Decode(&payload); err != nil {
		return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
	}
	payload.presentFields = make(map[string]bool, len(fields))
	for name := range fields {
		payload.presentFields[name] = true
	}
	return payload.submitRequest(operation)
}

func (payload videoJSONRequest) submitRequest(operation string) (service.VideoSubmitRequest, error) {
	if err := payload.normalizeContent(operation); err != nil {
		return service.VideoSubmitRequest{}, err
	}
	referenceImages, err := normalizeVideoReferenceList(payload.ReferenceImages)
	if err != nil {
		return service.VideoSubmitRequest{}, err
	}
	referenceVideos, err := normalizeVideoReferenceList(payload.ReferenceVideos)
	if err != nil {
		return service.VideoSubmitRequest{}, err
	}
	referenceAudios, err := normalizeVideoReferenceList(payload.ReferenceAudios)
	if err != nil {
		return service.VideoSubmitRequest{}, err
	}
	ratio, aspectRatio := strings.TrimSpace(payload.Ratio), strings.TrimSpace(payload.AspectRatio)
	if strings.TrimSpace(payload.ImageURL) != "" || strings.TrimSpace(payload.FirstImageURL) != "" ||
		strings.TrimSpace(payload.LastImageURL) != "" || len(referenceVideos) > 0 {
		ratio, aspectRatio = "", ""
	}
	request := service.VideoSubmitRequest{
		Operation: operation, Model: strings.TrimSpace(payload.Model), Prompt: strings.TrimSpace(payload.Prompt),
		Seconds: int(payload.Seconds), Size: strings.TrimSpace(payload.Size), Quality: strings.TrimSpace(payload.Quality),
		AudioEnabled: payload.Audio, ServiceTier: strings.TrimSpace(payload.ServiceTier),
		CallbackURL: strings.TrimSpace(payload.CallbackURL), ProviderOptions: cloneVideoOptions(payload.ProviderOptions),
		ReferenceMedia: service.ProviderVideoReferenceMedia{
			Ratio: ratio, AspectRatio: aspectRatio,
			ImageURL: strings.TrimSpace(payload.ImageURL), FirstImageURL: strings.TrimSpace(payload.FirstImageURL),
			LastImageURL: strings.TrimSpace(payload.LastImageURL), ReferenceImages: referenceImages,
			ReferenceVideos: referenceVideos, ReferenceAudios: referenceAudios,
		},
	}
	if payload.InputReference != nil {
		request.InputReference = &service.ProviderInputReference{
			FileID: strings.TrimSpace(payload.InputReference.FileID), ImageURL: strings.TrimSpace(payload.InputReference.ImageURL),
		}
	}
	for _, character := range payload.Characters {
		if id := strings.TrimSpace(character.ID); id != "" {
			request.CharacterIDs = append(request.CharacterIDs, id)
		} else {
			return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
		}
	}
	if payload.Video != nil {
		request.SourceVideoID = strings.TrimSpace(payload.Video.ID)
	}
	if name := strings.TrimSpace(payload.Name); name != "" {
		if request.ProviderOptions == nil {
			request.ProviderOptions = make(map[string]any)
		}
		request.ProviderOptions["name"] = name
	}
	if err := validateVideoPublicShape(request); err != nil {
		return service.VideoSubmitRequest{}, err
	}
	return request, nil
}

func (payload *videoJSONRequest) normalizeContent(operation string) error {
	if !payload.presentFields["content"] {
		return nil
	}
	if operation != service.VideoOperationGenerate || len(payload.Content) == 0 || len(payload.Content) > 32 {
		return service.ErrVideoInvalidRequest
	}
	for _, name := range []string{
		"prompt", "input_reference", "image_url", "first_image_url", "last_image_url",
		"reference_images", "reference_videos", "reference_audios",
	} {
		if payload.presentFields[name] {
			return service.ErrVideoInvalidRequest
		}
	}
	texts := make([]string, 0, 1)
	for _, item := range payload.Content {
		itemType := strings.ToLower(strings.TrimSpace(item.Type))
		role := strings.ToLower(strings.TrimSpace(item.Role))
		text := strings.TrimSpace(item.Text)
		imageURL := strings.TrimSpace(string(item.ImageURL))
		videoURL := strings.TrimSpace(string(item.VideoURL))
		audioURL := strings.TrimSpace(string(item.AudioURL))
		switch itemType {
		case "text":
			if text == "" || role != "" || imageURL != "" || videoURL != "" || audioURL != "" {
				return service.ErrVideoInvalidRequest
			}
			texts = append(texts, text)
		case "image_url":
			if text != "" || imageURL == "" || videoURL != "" || audioURL != "" {
				return service.ErrVideoInvalidRequest
			}
			if role == "" {
				role = "first_frame"
			}
			switch role {
			case "first_frame":
				if strings.TrimSpace(payload.FirstImageURL) != "" {
					return service.ErrVideoInvalidRequest
				}
				payload.FirstImageURL = imageURL
			case "last_frame":
				if strings.TrimSpace(payload.LastImageURL) != "" {
					return service.ErrVideoInvalidRequest
				}
				payload.LastImageURL = imageURL
			case "reference_image":
				payload.ReferenceImages = append(payload.ReferenceImages, imageURL)
			default:
				return service.ErrVideoInvalidRequest
			}
		case "video_url":
			if text != "" || imageURL != "" || videoURL == "" || audioURL != "" || (role != "" && role != "reference_video") {
				return service.ErrVideoInvalidRequest
			}
			payload.ReferenceVideos = append(payload.ReferenceVideos, videoURL)
		case "audio_url":
			if text != "" || imageURL != "" || videoURL != "" || audioURL == "" || (role != "" && role != "reference_audio") {
				return service.ErrVideoInvalidRequest
			}
			payload.ReferenceAudios = append(payload.ReferenceAudios, audioURL)
		default:
			return service.ErrVideoInvalidRequest
		}
	}
	if len(texts) == 0 {
		return service.ErrVideoInvalidRequest
	}
	payload.Prompt = strings.Join(texts, "\n\n")
	return nil
}

func videoSubmitRequestFromMultipart(values map[string]string, inputs []service.VideoInput, operation string) (service.VideoSubmitRequest, error) {
	payload := videoJSONRequest{
		Model: values["model"], Prompt: values["prompt"], Size: values["size"],
		Quality: values["quality"], ServiceTier: values["service_tier"],
		CallbackURL: values["callback_url"], Name: values["name"],
	}
	if raw := strings.TrimSpace(values["seconds"]); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 0 {
			return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
		}
		payload.Seconds = videoFlexibleInt(seconds)
	}
	if raw := strings.TrimSpace(values["audio"]); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
		}
		payload.Audio = &value
	}
	if raw := strings.TrimSpace(values["characters"]); raw != "" {
		if err := decodeStrictJSON([]byte(raw), &payload.Characters); err != nil {
			return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
		}
	}
	if raw := strings.TrimSpace(values["provider_options"]); raw != "" {
		if err := decodeStrictJSON([]byte(raw), &payload.ProviderOptions); err != nil {
			return service.VideoSubmitRequest{}, service.ErrVideoInvalidRequest
		}
	}
	request, err := payload.submitRequest(operation)
	if err != nil {
		return service.VideoSubmitRequest{}, err
	}
	request.Inputs = append([]service.VideoInput(nil), inputs...)
	return request, nil
}

func validateVideoPublicShape(request service.VideoSubmitRequest) error {
	switch request.Operation {
	case service.VideoOperationGenerate:
		if request.SourceVideoID != "" {
			return service.ErrVideoInvalidRequest
		}
	case service.VideoOperationEdit:
		if request.InputReference != nil || !request.ReferenceMedia.Empty() || len(request.CharacterIDs) != 0 {
			return service.ErrVideoInvalidRequest
		}
	case service.VideoOperationExtend:
		if request.InputReference != nil || !request.ReferenceMedia.Empty() || len(request.CharacterIDs) != 0 || request.SourceVideoID == "" {
			return service.ErrVideoInvalidRequest
		}
	case service.VideoOperationCharacterCreate:
		if request.InputReference != nil || !request.ReferenceMedia.Empty() || len(request.CharacterIDs) != 0 || request.SourceVideoID != "" {
			return service.ErrVideoInvalidRequest
		}
	default:
		return service.ErrVideoCapabilityUnsupported
	}
	return nil
}

func videoMultipartRole(operation, name string) (service.VideoInputRole, bool) {
	switch operation {
	case service.VideoOperationGenerate:
		if name == "input_reference" {
			return service.VideoInputRoleReferenceImage, true
		}
	case service.VideoOperationEdit:
		if name == "video" {
			return service.VideoInputRoleSourceVideo, true
		}
	case service.VideoOperationCharacterCreate:
		return service.VideoInputRoleCharacterClip, name == "video"
	default:
		return "", false
	}
	if operation != service.VideoOperationGenerate && operation != service.VideoOperationEdit {
		return "", false
	}
	if allowedVideoMultipartScalar(operation, name) {
		return "", false
	}
	role := service.VideoInputRole(strings.TrimSpace(name))
	return role, service.IsValidVideoInputRole(role)
}

func allowedVideoMultipartScalar(operation, name string) bool {
	common := map[string]bool{
		"model": true, "prompt": true, "seconds": true, "size": true,
		"quality": true, "audio": true, "service_tier": true,
		"callback_url": true, "provider_options": true,
	}
	if common[name] {
		return true
	}
	if name == "characters" {
		return operation == service.VideoOperationGenerate
	}
	return name == "name" && operation == service.VideoOperationCharacterCreate
}

func readVideoScalar(reader io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, videoScalarFieldMaxBytes+1))
	if err != nil || int64(len(raw)) > videoScalarFieldMaxBytes || !utf8.Valid(raw) {
		return "", service.ErrVideoInvalidRequest
	}
	if hasControlCharacterExceptWhitespace(string(raw)) {
		return "", service.ErrVideoInvalidRequest
	}
	return string(raw), nil
}

func decodeStrictJSON(raw []byte, destination any) error {
	if err := jsonstrict.RejectDuplicateKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func cloneVideoOptions(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func normalizeVideoReferenceList(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make([]string, len(values))
	for index, value := range values {
		normalized[index] = strings.TrimSpace(value)
		if normalized[index] == "" {
			return nil, service.ErrVideoInvalidRequest
		}
	}
	return normalized, nil
}

func validateVideoIdempotencyKey(value string) error {
	if len(value) > 255 || hasControlCharacter(value) {
		return service.ErrVideoInvalidRequest
	}
	return nil
}

func hasControlCharacter(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func hasControlCharacterExceptWhitespace(value string) bool {
	for _, character := range value {
		if character == '\n' || character == '\r' || character == '\t' {
			continue
		}
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

type videoTaskResponse struct {
	ID              string                       `json:"id"`
	Object          string                       `json:"object"`
	CreatedAt       int64                        `json:"created_at"`
	CompletedAt     *int64                       `json:"completed_at,omitempty"`
	ExpiresAt       *int64                       `json:"expires_at,omitempty"`
	Status          string                       `json:"status"`
	Model           string                       `json:"model,omitempty"`
	Progress        *float64                     `json:"progress,omitempty"`
	Seconds         string                       `json:"seconds,omitempty"`
	Size            string                       `json:"size,omitempty"`
	Operation       string                       `json:"operation,omitempty"`
	URL             string                       `json:"url,omitempty"`
	Error           *videoTaskErrorResponse      `json:"error,omitempty"`
	ContentVariants []string                     `json:"content_variants,omitempty"`
	Provider        string                       `json:"provider,omitempty"`
	ProviderAccess  *videoProviderAccessResponse `json:"provider_access,omitempty"`
}

type videoTaskErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type videoProviderAccessResponse struct {
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	Scope     string `json:"scope"`
	ExpiresAt *int64 `json:"expires_at,omitempty"`
}

type videoListResponse struct {
	Object  string              `json:"object"`
	Data    []videoTaskResponse `json:"data"`
	HasMore bool                `json:"has_more"`
	FirstID string              `json:"first_id,omitempty"`
	LastID  string              `json:"last_id,omitempty"`
	After   string              `json:"after,omitempty"`
}

type videoResourceResponse struct {
	ID                 string         `json:"id"`
	Object             string         `json:"object"`
	CreatedAt          int64          `json:"created_at"`
	ExpiresAt          *int64         `json:"expires_at,omitempty"`
	Name               string         `json:"name,omitempty"`
	Status             string         `json:"status"`
	Provider           string         `json:"provider,omitempty"`
	ProviderResourceID string         `json:"provider_resource_id,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type videoDeletedResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Deleted bool   `json:"deleted"`
}

func (h *VideoHandler) projectTask(ctx context.Context, userID int64, task *service.VideoTask, requests ...*http.Request) (videoTaskResponse, error) {
	if task == nil {
		return videoTaskResponse{}, service.ErrVideoTaskNotFound
	}
	response := videoTaskResponse{
		ID: task.PublicID, Object: "video", CreatedAt: task.CreatedAt.Unix(),
		Status: service.ProjectVideoStatus(task), Model: firstNonEmptyVideoString(task.PublicModel, task.RequestedModel),
		Progress: task.Progress, Operation: task.Operation,
		ContentVariants: append([]string(nil), task.ContentVariants...),
	}
	if task.FinishedAt != nil {
		value := task.FinishedAt.Unix()
		response.CompletedAt = &value
	}
	if task.ContentExpiresAt != nil {
		value := task.ContentExpiresAt.Unix()
		response.ExpiresAt = &value
	}
	if value := videoAttributeString(task.RequestAttributes, "size"); value != "" {
		response.Size = value
	}
	if value := videoAttributeInt(task.RequestAttributes, "seconds"); value > 0 {
		response.Seconds = strconv.Itoa(value)
	}
	if response.Status == service.VideoGenerationCompleted && h.cfg != nil &&
		h.cfg.Gateway.Video.ContentProxy.Enabled && hasVideoContentVariant(task.ContentVariants, "video") {
		providerURL, err := h.tasks.VideoURLForOwner(ctx, userID, task.PublicID)
		if err != nil {
			return videoTaskResponse{}, err
		}
		if len(requests) > 0 && requests[0] != nil && providerURL != "" {
			response.URL = rewriteVideoURLHost(providerURL, requests[0])
		}
		if response.URL == "" {
			response.URL = "/v1/videos/" + url.PathEscape(task.PublicID) + "/content"
		}
	}
	if response.Status == service.VideoGenerationFailed {
		response.Error = &videoTaskErrorResponse{
			Code:    firstNonEmptyVideoString(valueOrEmptyString(task.LastErrorCode), "video_generation_failed"),
			Message: firstNonEmptyVideoString(valueOrEmptyString(task.LastErrorMessage), "video generation failed"),
		}
	}
	disclosure, err := h.tasks.DisclosureForOwner(ctx, userID, task.PublicID)
	if err != nil {
		return videoTaskResponse{}, err
	}
	if disclosure == nil || disclosure.Policy == config.VideoDisclosureNone {
		return response, nil
	}
	response.Provider = disclosure.Provider
	if disclosure.Access != nil && disclosure.Access.Value != "" {
		access := &videoProviderAccessResponse{
			Kind: disclosure.Access.Kind, Value: disclosure.Access.Value, Scope: disclosure.Access.Scope,
		}
		if disclosure.Access.ExpiresAt != nil {
			value := disclosure.Access.ExpiresAt.Unix()
			access.ExpiresAt = &value
		}
		response.ProviderAccess = access
	}
	return response, nil
}

func rewriteVideoURLHost(providerURL string, request *http.Request) string {
	provider, err := url.Parse(strings.TrimSpace(providerURL))
	if err != nil || provider.Hostname() == "" || request == nil {
		return ""
	}
	host := strings.TrimSpace(request.Host)
	current, err := url.Parse("http://" + host)
	if err != nil || current.Host == "" || current.User != nil || current.Path != "" {
		return ""
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	provider.Scheme = scheme
	provider.Host = current.Host
	provider.User = nil
	return provider.String()
}

func hasVideoContentVariant(variants []string, expected string) bool {
	for _, variant := range variants {
		if strings.EqualFold(strings.TrimSpace(variant), expected) {
			return true
		}
	}
	return false
}

func (h *VideoHandler) projectResource(ctx context.Context, userID int64, resource *service.VideoResource) (videoResourceResponse, error) {
	if resource == nil {
		return videoResourceResponse{}, service.ErrVideoResourceNotFound
	}
	response := videoResourceResponse{
		ID: resource.PublicID, Object: "video.character", CreatedAt: resource.CreatedAt.Unix(),
		Status: resource.Status, Metadata: cloneVideoOptions(resource.Metadata),
	}
	if value, ok := resource.Metadata["name"].(string); ok {
		response.Name = value
	}
	if resource.ExpiresAt != nil {
		value := resource.ExpiresAt.Unix()
		response.ExpiresAt = &value
	}
	disclosure, err := h.tasks.ResourceDisclosureForOwner(ctx, userID, resource.PublicID)
	if errors.Is(err, service.ErrVideoSettlementPending) {
		return videoResourceResponse{ID: resource.PublicID, Object: "video.character", CreatedAt: resource.CreatedAt.Unix(), Status: "creating"}, nil
	}
	if err != nil {
		return videoResourceResponse{}, err
	}
	if disclosure != nil && disclosure.Policy != config.VideoDisclosureNone {
		response.Provider = disclosure.Provider
		response.ProviderResourceID = disclosure.ProviderResourceID
	}
	return response, nil
}

func (h *VideoHandler) authorizedAPIKey(c *gin.Context) (*service.APIKey, bool) {
	if h == nil || c == nil {
		return nil, false
	}
	apiKey, ok := servermiddleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.ID <= 0 || apiKey.UserID <= 0 {
		videoJSONError(c, http.StatusUnauthorized, "api_key_required", "API key is required")
		return nil, false
	}
	return apiKey, true
}

func (h *VideoHandler) checkSecurityAudit(c *gin.Context, apiKey *service.APIKey, request service.VideoSubmitRequest) bool {
	if h == nil || h.openAI == nil {
		return true
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		videoJSONError(c, http.StatusInternalServerError, "user_context_required", "User context not found")
		return false
	}
	auditBody, err := videoModerationBody(c.Request.Context(), request)
	if err != nil {
		videoError(c, err)
		return false
	}
	decision := h.openAI.checkSecurityAudit(
		c,
		requestLogger(c, "handler.video.security_audit"),
		apiKey,
		subject,
		service.ContentModerationProtocolOpenAIImages,
		request.Model,
		auditBody,
	)
	if decision != nil && !decision.AllowNextStage {
		h.openAI.openAISecurityAuditError(c, decision)
		return false
	}
	return true
}

func videoModerationBody(ctx context.Context, request service.VideoSubmitRequest) ([]byte, error) {
	images := make([]map[string]string, 0, 1+len(request.ReferenceMedia.ReferenceImages))
	if request.InputReference != nil {
		if imageURL := strings.TrimSpace(request.InputReference.ImageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, imageURL := range append([]string{
		request.ReferenceMedia.ImageURL,
		request.ReferenceMedia.FirstImageURL,
		request.ReferenceMedia.LastImageURL,
	}, request.ReferenceMedia.ReferenceImages...) {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, input := range request.Inputs {
		if input.Role != service.VideoInputRoleReferenceImage {
			continue
		}
		if input.Size < 0 || input.Size > service.MaxContentModerationImageBytes || input.Open == nil {
			return nil, service.ErrVideoInputTooLarge
		}
		reader, err := input.Open(ctx)
		if err != nil {
			return nil, err
		}
		var encoded strings.Builder
		encoded.Grow(int(base64.StdEncoding.EncodedLen(int(input.Size))) + len(input.MIMEType) + 13)
		_, _ = encoded.WriteString("data:")
		_, _ = encoded.WriteString(input.MIMEType)
		_, _ = encoded.WriteString(";base64,")
		encoder := base64.NewEncoder(base64.StdEncoding, &encoded)
		copied, copyErr := io.Copy(encoder, io.LimitReader(reader, service.MaxContentModerationImageBytes+1))
		encodeErr := encoder.Close()
		closeErr := reader.Close()
		if copied > service.MaxContentModerationImageBytes {
			return nil, service.ErrVideoInputTooLarge
		}
		if err := errors.Join(copyErr, encodeErr, closeErr); err != nil {
			return nil, err
		}
		images = append(images, map[string]string{"image_url": encoded.String()})
	}
	body := map[string]any{"prompt": request.Prompt}
	if len(images) > 0 {
		body["images"] = images
	}
	return json.Marshal(body)
}

func (h *VideoHandler) readEnabled(c *gin.Context) bool {
	if h == nil || h.tasks == nil || h.cfg == nil || !h.cfg.Gateway.Video.Enabled {
		videoError(c, service.ErrVideoDisabled)
		return false
	}
	return true
}

func (h *VideoHandler) setNoStore(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
}

func videoError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	code := strings.ToLower(strings.TrimSpace(infraerrors.Reason(err)))
	message := infraerrors.Message(err)
	var providerErr *service.VideoProviderError
	if errors.As(err, &providerErr) {
		if providerErr.StatusCode >= 400 && providerErr.StatusCode <= 599 {
			status = providerErr.StatusCode
		} else {
			status = http.StatusBadGateway
		}
		code = strings.ToLower(strings.TrimSpace(providerErr.Code))
		message = strings.TrimSpace(providerErr.Message)
	}
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	if code == "" {
		code = "video_api_error"
	}
	if message == "" {
		message = http.StatusText(status)
	}
	videoJSONError(c, status, code, message)
}

func videoJSONError(c *gin.Context, status int, code, message string) {
	if c == nil {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(status, gin.H{"error": gin.H{
		"message": message, "type": videoErrorType(status), "param": nil, "code": code,
	}})
}

func videoErrorType(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusConflict:
		return "conflict_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "api_error"
	}
}

func copyVideoContentHeaders(destination, source http.Header) {
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := strings.TrimSpace(source.Get(key)); value != "" {
			destination.Set(key, value)
		}
	}
}

func validSingleByteRange(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 200 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 || (parts[0] == "" && parts[1] == "") {
		return false
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func videoContentFilename(taskID, variant string) string {
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "thumbnail":
		return taskID + "-thumbnail.webp"
	case "spritesheet":
		return taskID + "-spritesheet.jpg"
	default:
		return taskID + ".mp4"
	}
}

func videoAttributeString(attributes map[string]any, key string) string {
	if attributes == nil {
		return ""
	}
	value, _ := attributes[key].(string)
	return strings.TrimSpace(value)
}

func videoAttributeInt(attributes map[string]any, key string) int {
	if attributes == nil {
		return 0
	}
	switch value := attributes[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	}
	return 0
}

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmptyVideoString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func videoPathID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return firstNonEmptyVideoString(c.Param("video_id"), c.Param("character_id"), c.Param("request_id"))
}

type videoContentLimiter struct {
	mu         sync.Mutex
	users      map[int64]int
	accounts   map[int64]int
	maxUser    int
	maxAccount int
}

func newVideoContentLimiter(cfg *config.Config) *videoContentLimiter {
	limiter := &videoContentLimiter{users: make(map[int64]int), accounts: make(map[int64]int)}
	if cfg != nil {
		limiter.maxUser = cfg.Gateway.Video.ContentProxy.MaxUserConcurrency
		limiter.maxAccount = cfg.Gateway.Video.ContentProxy.MaxAccountConcurrency
	}
	return limiter
}

func (l *videoContentLimiter) acquire(userID, accountID int64) (func(), bool) {
	if l == nil {
		return func() {}, true
	}
	l.mu.Lock()
	if (l.maxUser > 0 && l.users[userID] >= l.maxUser) || (l.maxAccount > 0 && l.accounts[accountID] >= l.maxAccount) {
		l.mu.Unlock()
		return nil, false
	}
	l.users[userID]++
	l.accounts[accountID]++
	l.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			if l.users[userID] <= 1 {
				delete(l.users, userID)
			} else {
				l.users[userID]--
			}
			if l.accounts[accountID] <= 1 {
				delete(l.accounts, accountID)
			} else {
				l.accounts[accountID]--
			}
			l.mu.Unlock()
		})
	}, true
}

var _ videoTaskAPI = (*service.VideoTaskService)(nil)
