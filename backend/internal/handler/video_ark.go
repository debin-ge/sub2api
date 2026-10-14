package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Ark 任务协议入口：POST/GET/DELETE {prefix}/contents/generations/tasks[/{id}]，列表，以及
// sub2api 扩展的签名内容代理与能力目录。只服务五家 Ark 形态平台的分组（含 composite 落到
// 它们的请求）；OpenAI 与 Grok 继续走 /v1/videos。

const (
	arkVideoContentURLSignLabel = "sub2api/ark-video-content-url/v1\x00"
	arkVideoTasksPublicPrefix   = "/api/v3" + service.ArkVideoTasksPathSuffix + "/"
	arkVideoListMaxPageSize     = 100
	arkVideoListMaxPageNum      = 50
)

// arkGroupAllowed 校验分组平台是否允许使用 Ark 入口。
func (h *VideoHandler) arkGroupAllowed(c *gin.Context) (*service.APIKey, bool) {
	apiKey, ok := h.authorizedAPIKey(c)
	if !ok {
		return nil, false
	}
	if apiKey.Group == nil || (apiKey.Group.Platform != service.PlatformComposite && !service.IsArkVideoPlatform(apiKey.Group.Platform)) {
		videoError(c, service.ErrVideoArkPlatformUnsupported)
		return nil, false
	}
	return apiKey, true
}

// ArkCreate 创建任务，应答 {"id"}。
func (h *VideoHandler) ArkCreate(c *gin.Context) {
	if _, ok := h.arkGroupAllowed(c); !ok {
		return
	}
	h.submitWithShape(c, service.VideoOperationGenerate, true)
}

// ArkRetrieve 查询任务，应答 Ark「查询视频生成任务」响应体。
func (h *VideoHandler) ArkRetrieve(c *gin.Context) {
	apiKey, ok := h.arkGroupAllowed(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	task, err := h.tasks.GetForOwner(c.Request.Context(), apiKey.UserID, videoPathID(c))
	if err != nil {
		arkVideoError(c, err)
		return
	}
	if !service.IsArkVideoProvider(task.Provider) {
		// 这把 Key 的分组若是 composite，用户可能用 Ark 路径查 OpenAI 任务；两套形态不互查。
		arkVideoError(c, service.ErrVideoTaskNotFound)
		return
	}
	response, err := h.projectArkTask(c.Request.Context(), apiKey.UserID, task, c.Request)
	if err != nil {
		arkVideoError(c, err)
		return
	}
	h.setNoStore(c)
	if status := service.ProjectArkVideoStatus(task); status == service.ArkVideoStatusQueued || status == service.ArkVideoStatusRunning {
		c.Header("Retry-After", "10")
	}
	c.PureJSON(http.StatusOK, response)
}

// ArkList 列出本用户的任务。接受 Ark 的 page_num / page_size / filter.status / filter.model，
// 也接受裸的 status / model。所有筛选在仓储分页之前完成，不扫描用户全部历史。
func (h *VideoHandler) ArkList(c *gin.Context) {
	apiKey, ok := h.arkGroupAllowed(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	pageNum, pageSize := 1, 20
	if raw := strings.TrimSpace(c.Query("page_num")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > arkVideoListMaxPageNum {
			videoError(c, service.ErrVideoInvalidRequest)
			return
		}
		pageNum = parsed
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > arkVideoListMaxPageSize {
			videoError(c, service.ErrVideoInvalidRequest)
			return
		}
		pageSize = parsed
	}
	status := strings.ToLower(strings.TrimSpace(firstNonEmptyVideoString(c.Query("filter.status"), c.Query("status"))))
	if status != "" && !service.IsArkVideoStatus(status) {
		videoError(c, service.ErrVideoInvalidRequest)
		return
	}
	model := strings.TrimSpace(firstNonEmptyVideoString(c.Query("filter.model"), c.Query("model")))

	page, err := h.tasks.ListForOwner(c.Request.Context(), apiKey.UserID, service.VideoTaskFilter{
		Model: model, Operation: service.VideoOperationGenerate, Limit: pageSize, Order: "desc",
		ArkOnly: true, ArkStatus: status, Offset: (pageNum - 1) * pageSize,
	})
	if err != nil {
		if errors.Is(err, service.ErrVideoListRateLimited) {
			c.Header("Retry-After", "60")
		}
		videoError(c, err)
		return
	}
	items := make([]map[string]any, 0, pageSize)
	for _, task := range page.Data {
		item, projectErr := h.projectArkTask(c.Request.Context(), apiKey.UserID, task, c.Request)
		if projectErr != nil {
			videoError(c, projectErr)
			return
		}
		items = append(items, item)
	}
	response := gin.H{"items": items, "page_num": pageNum, "page_size": pageSize, "has_more": page.HasMore}
	// An empty page past the end does not reveal the exact total.
	if !page.HasMore && (len(items) > 0 || pageNum == 1) {
		response["total"] = (pageNum-1)*pageSize + len(items)
	}
	h.setNoStore(c)
	c.PureJSON(http.StatusOK, response)
}

// ArkDelete 取消运行中的任务或删除已完成任务；与 Ark 一致返回 200 空对象。
func (h *VideoHandler) ArkDelete(c *gin.Context) {
	apiKey, ok := h.arkGroupAllowed(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	id := videoPathID(c)
	task, err := h.tasks.GetForOwner(c.Request.Context(), apiKey.UserID, id)
	if err != nil {
		arkVideoError(c, err)
		return
	}
	if !service.IsArkVideoProvider(task.Provider) {
		arkVideoError(c, service.ErrVideoTaskNotFound)
		return
	}
	if _, err := h.tasks.DeleteForOwner(c.Request.Context(), apiKey.UserID, id); err != nil {
		arkVideoError(c, err)
		return
	}
	h.setNoStore(c)
	c.PureJSON(http.StatusOK, gin.H{})
}

// ArkModels 返回 Ark 维度的能力目录。
func (h *VideoHandler) ArkModels(c *gin.Context) {
	apiKey, ok := h.arkGroupAllowed(c)
	if !ok {
		return
	}
	if !h.readEnabled(c) {
		return
	}
	response, err := h.tasks.ListArkVideoModels(c.Request.Context(), apiKey)
	if err != nil {
		videoError(c, err)
		return
	}
	h.setNoStore(c)
	c.PureJSON(http.StatusOK, response)
}

// ArkSignedContent 按签名链接流式代理内容（video / last_frame 变体），不挂 API Key。
func (h *VideoHandler) ArkSignedContent(c *gin.Context) {
	startedAt := time.Now()
	variant := strings.ToLower(strings.TrimSpace(c.Query("variant")))
	if variant == "" {
		variant = "video"
	}
	defer func() {
		observability.DefaultVideoMetrics().RecordContent(variant, c.Writer.Status(), time.Since(startedAt))
	}()
	if h == nil || h.tasks == nil || h.cfg == nil || !h.cfg.Gateway.Video.Enabled || !h.cfg.Gateway.Video.ContentProxy.Enabled {
		videoError(c, service.ErrVideoDisabled)
		return
	}
	if c.Request == nil || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
		videoError(c, service.ErrVideoTaskNotFound)
		return
	}
	publicID := strings.TrimSpace(videoPathID(c))
	exp, err := strconv.ParseInt(strings.TrimSpace(c.Query("exp")), 10, 64)
	if err != nil || !service.IsValidVideoTaskID(publicID) || (variant != "video" && variant != "last_frame") ||
		!h.verifyArkContentSignature(publicID, variant, exp, c.Query("sig")) {
		videoError(c, service.ErrVideoTaskNotFound)
		return
	}
	if !time.Now().Before(time.Unix(exp, 0)) {
		videoError(c, service.ErrVideoContentExpired)
		return
	}
	task, err := h.tasks.GetContentTaskByPublicID(c.Request.Context(), publicID)
	if err != nil {
		videoError(c, err)
		return
	}
	if task == nil || task.PublicID != publicID || task.DeleteState == service.VideoDeleteDeleted || task.DeletedAt != nil || !service.IsArkVideoProvider(task.Provider) {
		videoError(c, service.ErrVideoTaskNotFound)
		return
	}
	if task.GenerationState != service.VideoGenerationCompleted || !hasVideoContentVariant(task.ContentVariants, variant) {
		videoError(c, service.ErrVideoContentNotReady)
		return
	}
	h.serveVideoContent(c, task, variant, true, func(ctx context.Context, request service.ProviderContentRequest) (*service.ProviderContent, error) {
		return h.tasks.OpenContentForTask(ctx, task, request)
	})
}

// projectArkTask 把任务投影成 Ark 响应体，并按披露策略追加 sub2api 扩展字段。
func (h *VideoHandler) projectArkTask(ctx context.Context, userID int64, task *service.VideoTask, request *http.Request) (map[string]any, error) {
	if task == nil {
		return nil, service.ErrVideoTaskNotFound
	}
	urls := service.ArkVideoContentURLs{}
	if service.ProjectArkVideoStatus(task) == service.ArkVideoStatusSucceeded && h.cfg != nil && h.cfg.Gateway.Video.ContentProxy.Enabled {
		for _, variant := range []string{"video", "last_frame"} {
			if hasVideoContentVariant(task.ContentVariants, variant) {
				if signed := h.arkSignedContentURL(task, request, variant, time.Now()); signed != "" {
					urls[variant] = signed
				}
			}
		}
	}
	object := service.BuildArkVideoTaskObject(task, urls)
	if prompt := videoAttributeString(task.RequestAttributes, "prompt"); prompt != "" {
		object["prompt"] = prompt
	}
	disclosure, err := h.tasks.DisclosureForOwner(ctx, userID, task.PublicID)
	if err != nil {
		return nil, err
	}
	if disclosure == nil || disclosure.Policy == config.VideoDisclosureNone {
		return object, nil
	}
	// 实际消费只在 succeeded（已结算）时暴露；失败/取消路径的 ActualCost 是 &0.0，不能透出。
	if service.ProjectArkVideoStatus(task) == service.ArkVideoStatusSucceeded && task.ActualCost != nil {
		object["actual_cost"] = *task.ActualCost
		if currency := strings.TrimSpace(task.Currency); currency != "" {
			object["currency"] = currency
		}
	}
	object["provider"] = disclosure.Provider
	if disclosure.Access != nil && disclosure.Access.Value != "" {
		access := map[string]any{"kind": disclosure.Access.Kind, "value": disclosure.Access.Value, "scope": disclosure.Access.Scope}
		if disclosure.Access.ExpiresAt != nil {
			access["expires_at"] = disclosure.Access.ExpiresAt.Unix()
		}
		object["provider_access"] = access
	}
	return object, nil
}

// arkContentSignMessage 把公开路径、变体与过期时间一起签进去：换路径、换变体都会使签名失效。
func arkContentSignMessage(publicID, variant string, exp int64) string {
	return arkVideoContentURLSignLabel + arkVideoTasksPublicPrefix + publicID + "/content\n" + variant + "\n" + strconv.FormatInt(exp, 10)
}

func (h *VideoHandler) signArkContentURL(publicID, variant string, exp int64) string {
	if h == nil || h.contentURLSigner == nil || !h.contentURLSigner.ok || publicID == "" || exp <= 0 {
		return ""
	}
	return hexEncode(h.contentURLSigner.macMessage(arkContentSignMessage(publicID, variant, exp)))
}

func (h *VideoHandler) verifyArkContentSignature(publicID, variant string, exp int64, sig string) bool {
	if h == nil || h.contentURLSigner == nil || !h.contentURLSigner.ok || publicID == "" || exp <= 0 {
		return false
	}
	return hmacHexEqual(sig, h.contentURLSigner.macMessage(arkContentSignMessage(publicID, variant, exp)))
}

// arkSignedContentURL 生成 <scheme>://<host>/api/v3/contents/generations/tasks/{id}/content?variant=..&exp=..&sig=..。
func (h *VideoHandler) arkSignedContentURL(task *service.VideoTask, request *http.Request, variant string, now time.Time) string {
	if task == nil || strings.TrimSpace(task.PublicID) == "" {
		return ""
	}
	base := videoPublicBaseURL(request)
	if base == "" {
		return ""
	}
	exp := videoContentURLExpiry(task, now)
	if exp == 0 {
		return ""
	}
	sig := h.signArkContentURL(task.PublicID, variant, exp)
	if sig == "" {
		return ""
	}
	query := url.Values{}
	query.Set("variant", variant)
	query.Set("exp", strconv.FormatInt(exp, 10))
	query.Set("sig", sig)
	return base + arkVideoTasksPublicPrefix + url.PathEscape(task.PublicID) + "/content?" + query.Encode()
}

// IsArkVideoGroup 报告当前 API Key 的分组是否按 Ark 形态提供视频（五家平台或 composite）。
func IsArkVideoGroup(c *gin.Context) bool {
	key, ok := middleware.GetAPIKeyFromContext(c)
	return ok && key != nil && key.Group != nil && (key.Group.Platform == service.PlatformComposite || service.IsArkVideoPlatform(key.Group.Platform))
}
