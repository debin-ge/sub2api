package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
	"github.com/gin-gonic/gin"
)

// Ark「创建视频生成任务」请求体的严格解析。字段集合以火山方舟文档为准；sub2api 自己的
// 扩展只有 provider_options（厂商特有参数）与 callback_url 的本地回调语义。

const (
	arkVideoMaxContentItems   = 32
	arkVideoMaxSeconds        = 120
	arkVideoDefaultBodyLimit  = int64(16 << 20)
	arkVideoMaxDataURIPayload = int64(service.MaxContentModerationImageBytes)
)

type arkVideoContentItemJSON struct {
	Type     string                `json:"type"`
	Text     string                `json:"text,omitempty"`
	Role     string                `json:"role,omitempty"`
	ImageURL videoURLReferenceJSON `json:"image_url,omitempty"`
	VideoURL videoURLReferenceJSON `json:"video_url,omitempty"`
	AudioURL videoURLReferenceJSON `json:"audio_url,omitempty"`
}

// arkVideoDurationJSON 接受整数或整数字符串，允许 -1（智能时长）。
type arkVideoDurationJSON struct {
	set   bool
	value int
}

func (d *arkVideoDurationJSON) UnmarshalJSON(data []byte) error {
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
		text = strings.TrimSpace(typed)
	default:
		return errors.New("duration must be an integer")
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return errors.New("duration must be an integer")
	}
	d.set, d.value = true, parsed
	return nil
}

type arkVideoRequestJSON struct {
	Model                 string                    `json:"model"`
	Content               []arkVideoContentItemJSON `json:"content"`
	Duration              arkVideoDurationJSON      `json:"duration,omitempty"`
	Frames                *int                      `json:"frames,omitempty"`
	Resolution            string                    `json:"resolution,omitempty"`
	Ratio                 string                    `json:"ratio,omitempty"`
	Seed                  *int64                    `json:"seed,omitempty"`
	CameraFixed           *bool                     `json:"camera_fixed,omitempty"`
	Watermark             *bool                     `json:"watermark,omitempty"`
	GenerateAudio         *bool                     `json:"generate_audio,omitempty"`
	ReturnLastFrame       *bool                     `json:"return_last_frame,omitempty"`
	ServiceTier           string                    `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *int                      `json:"execution_expires_after,omitempty"`
	CallbackURL           string                    `json:"callback_url,omitempty"`
	ProviderOptions       map[string]any            `json:"provider_options,omitempty"`
	// 下面这些是 Ark 协议里存在、本网关尚未承接的字段：出现即明确拒绝，而不是静默吞掉。
	Priority              json.RawMessage `json:"priority,omitempty"`
	Draft                 json.RawMessage `json:"draft,omitempty"`
	SafetyIdentifier      json.RawMessage `json:"safety_identifier,omitempty"`
	OutputFormat          json.RawMessage `json:"output_format,omitempty"`
	Tools                 json.RawMessage `json:"tools,omitempty"`
	OmniReferenceTaskType json.RawMessage `json:"omni_reference_task_type,omitempty"`
}

// arkVideoPromptFlagPattern 匹配 Ark 的弱校验传参 `--key value`。
var arkVideoPromptFlagPattern = regexp.MustCompile(`(?:^|\s)--([A-Za-z][A-Za-z0-9_]*)(?:\s+|=)(\S+)`)

func arkUnsupportedParameter(name string) error {
	return fmt.Errorf("%w: %s", service.ErrVideoUnsupportedParameter, name)
}

// ArkJSONBodyLimit 对外暴露 Ark 创建请求的 JSON 体上限，供路由层的合成分组中间件对齐。
func (h *VideoHandler) ArkJSONBodyLimit() int64 { return h.arkJSONBodyLimit() }

// arkJSONBodyLimit 返回 Ark 创建请求的 JSON 体上限（配置缺省 16MB，不超过创建意图哈希的上限）。
func (h *VideoHandler) arkJSONBodyLimit() int64 {
	if h != nil && h.cfg != nil && h.cfg.Gateway.Video.TaskJSONBodyMaxBytes > 0 {
		return min(h.cfg.Gateway.Video.TaskJSONBodyMaxBytes, int64(service.VideoCreateJSONMaxBytes))
	}
	return arkVideoDefaultBodyLimit
}

// parseArkSubmitRequest 读取并解析 Ark 形态的创建请求。data: URI 图片会被解码后写入加密
// spool（与 multipart 上传同一条路），返回的 session 由调用方关闭。
func (h *VideoHandler) parseArkSubmitRequest(c *gin.Context, userID int64) (service.VideoSubmitRequest, *service.VideoSpoolSession, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidRequest
	}
	if encoding := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Encoding"))); encoding != "" && encoding != "identity" {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidRequest
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidRequest
	}
	limit := h.arkJSONBodyLimit()
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
	if err != nil {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidRequest
	}
	if int64(len(raw)) > limit {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInputTooLarge
	}
	return h.parseArkVideoRequest(c.Request.Context(), raw, userID)
}

func (h *VideoHandler) parseArkVideoRequest(ctx context.Context, raw []byte, userID int64) (_ service.VideoSubmitRequest, session *service.VideoSpoolSession, returnErr error) {
	defer func() {
		if returnErr != nil && session != nil {
			_ = session.Close()
			session = nil
		}
	}()
	if err := jsonstrict.RejectDuplicateKeys(raw); err != nil {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload arkVideoRequestJSON
	if err := decoder.Decode(&payload); err != nil {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidRequest
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidRequest
	}
	for name, present := range map[string]json.RawMessage{
		"priority": payload.Priority, "draft": payload.Draft, "safety_identifier": payload.SafetyIdentifier,
		"output_format": payload.OutputFormat, "tools": payload.Tools, "omni_reference_task_type": payload.OmniReferenceTaskType,
	} {
		if len(present) > 0 && !bytes.Equal(bytes.TrimSpace(present), []byte("null")) {
			return service.VideoSubmitRequest{}, nil, arkUnsupportedParameter(name)
		}
	}
	model := strings.TrimSpace(payload.Model)
	if model == "" {
		return service.VideoSubmitRequest{}, nil, fmt.Errorf("%w: model is required", service.ErrVideoInvalidRequest)
	}
	if len(payload.Content) == 0 || len(payload.Content) > arkVideoMaxContentItems {
		return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidContent
	}

	texts := make([]string, 0, 1)
	content := make([]service.VideoContentItem, 0, len(payload.Content))
	var inputs []service.VideoInput
	for _, item := range payload.Content {
		itemType := strings.ToLower(strings.TrimSpace(item.Type))
		role := strings.ToLower(strings.TrimSpace(item.Role))
		text := item.Text
		imageURL := strings.TrimSpace(string(item.ImageURL))
		videoURL := strings.TrimSpace(string(item.VideoURL))
		audioURL := strings.TrimSpace(string(item.AudioURL))
		switch itemType {
		case service.VideoContentTypeText:
			if strings.TrimSpace(text) == "" || role != "" || imageURL != "" || videoURL != "" || audioURL != "" {
				return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidContent
			}
			texts = append(texts, text)
			content = append(content, service.VideoContentItem{Type: service.VideoContentTypeText, Text: text})
		case service.VideoContentTypeImage, service.VideoContentTypeVideo, service.VideoContentTypeAudio:
			if strings.TrimSpace(text) != "" {
				return service.VideoSubmitRequest{}, nil, service.ErrVideoInvalidContent
			}
			mediaURL, contentRole, err := arkResolveContentRole(itemType, role, imageURL, videoURL, audioURL)
			if err != nil {
				return service.VideoSubmitRequest{}, nil, err
			}
			entry := service.VideoContentItem{Type: itemType, Role: contentRole}
			if strings.HasPrefix(strings.ToLower(mediaURL), "data:") {
				if itemType != service.VideoContentTypeImage {
					return service.VideoSubmitRequest{}, nil, fmt.Errorf("%w: %s must be an https URL", service.ErrVideoInputUnsupported, itemType)
				}
				if h == nil || h.spool == nil {
					return service.VideoSubmitRequest{}, nil, service.ErrVideoInputUnsupported
				}
				if session == nil {
					var err error
					session, err = h.spool.Begin(ctx, userID)
					if err != nil {
						return service.VideoSubmitRequest{}, nil, err
					}
				}
				input, err := arkStoreDataURI(ctx, session, contentRole, mediaURL)
				if err != nil {
					return service.VideoSubmitRequest{}, session, err
				}
				inputs = append(inputs, input)
				entry.Input = &inputs[len(inputs)-1]
			} else {
				parsed, err := url.Parse(mediaURL)
				if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
					return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: %s url must be an https URL or data URI", service.ErrVideoInvalidContent, itemType)
				}
				if len(mediaURL) > 512<<10 {
					return service.VideoSubmitRequest{}, session, service.ErrVideoInputTooLarge
				}
				entry.URL = mediaURL
			}
			content = append(content, entry)
		default:
			return service.VideoSubmitRequest{}, session, service.ErrVideoInvalidContent
		}
	}
	if len(texts) == 0 {
		return service.VideoSubmitRequest{}, session, service.ErrVideoInvalidContent
	}
	// inputs 切片可能在 append 时搬家：最后一次性把指针重新指到最终位置。
	for i := range content {
		if content[i].Input != nil {
			for j := range inputs {
				if inputs[j].SHA256 == content[i].Input.SHA256 && inputs[j].Role == content[i].Input.Role {
					content[i].Input = &inputs[j]
					break
				}
			}
		}
	}

	// 弱校验 flag：覆盖顶层字段，再统一校验。
	if err := applyArkPromptFlags(&payload, texts); err != nil {
		return service.VideoSubmitRequest{}, session, err
	}

	request := service.VideoSubmitRequest{
		Operation: service.VideoOperationGenerate, Model: model, Prompt: strings.TrimSpace(strings.Join(texts, "\n\n")),
		Content: content, Inputs: inputs,
		Resolution:   strings.ToLower(strings.TrimSpace(payload.Resolution)),
		Ratio:        strings.ToLower(strings.TrimSpace(payload.Ratio)),
		AudioEnabled: payload.GenerateAudio, Seed: payload.Seed, CameraFixed: payload.CameraFixed, Watermark: payload.Watermark,
		ReturnLastFrame: payload.ReturnLastFrame, ServiceTier: strings.TrimSpace(payload.ServiceTier),
		CallbackURL: strings.TrimSpace(payload.CallbackURL), ProviderOptions: cloneVideoOptions(payload.ProviderOptions),
	}
	if payload.ExecutionExpiresAfter != nil {
		request.ExecutionExpiresAfter = *payload.ExecutionExpiresAfter
	}
	if payload.Frames != nil {
		if payload.Duration.set {
			return service.VideoSubmitRequest{}, session, service.ErrVideoDurationFramesConflict
		}
		if *payload.Frames <= 0 {
			return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: frames must be positive", service.ErrVideoInvalidRequest)
		}
		request.Frames = *payload.Frames
	}
	if payload.Duration.set {
		switch {
		case payload.Duration.value == -1:
			request.SmartDuration = true
		case payload.Duration.value <= 0 || payload.Duration.value > arkVideoMaxSeconds:
			return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: duration must be -1 or within [1, %d]", service.ErrVideoInvalidRequest, arkVideoMaxSeconds)
		default:
			request.Seconds = payload.Duration.value
		}
	}
	if request.Resolution != "" && !service.IsArkVideoResolution(request.Resolution) {
		return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: resolution %q", service.ErrVideoUnsupportedParameter, request.Resolution)
	}
	if request.Ratio != "" && !service.IsArkVideoRatio(request.Ratio) {
		return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: ratio %q", service.ErrVideoUnsupportedParameter, request.Ratio)
	}
	if request.Ratio == service.ArkVideoRatioAdaptive && len(request.ContentByRole(service.VideoInputRoleFirstFrame)) == 0 {
		return service.VideoSubmitRequest{}, session, service.ErrVideoAdaptiveRatioRequiresImage
	}
	if request.Seed != nil && (*request.Seed < -1 || *request.Seed > int64(1)<<32-1) {
		return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: seed must be within [-1, 2^32-1]", service.ErrVideoInvalidRequest)
	}
	if request.ExecutionExpiresAfter < 0 {
		return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: execution_expires_after must be positive", service.ErrVideoInvalidRequest)
	}
	for key, value := range request.ProviderOptions {
		if strings.TrimSpace(key) == "" || value == nil {
			return service.VideoSubmitRequest{}, session, fmt.Errorf("%w: provider_options.%s", service.ErrVideoInvalidRequest, key)
		}
	}
	return request, session, nil
}

// arkResolveContentRole 校验媒体项的 type / role 组合并补默认角色（Ark：image_url 默认 first_frame）。
func arkResolveContentRole(itemType, role, imageURL, videoURL, audioURL string) (string, service.VideoInputRole, error) {
	switch itemType {
	case service.VideoContentTypeImage:
		if imageURL == "" || videoURL != "" || audioURL != "" {
			return "", "", service.ErrVideoInvalidContent
		}
		switch role {
		case "", string(service.VideoInputRoleFirstFrame):
			return imageURL, service.VideoInputRoleFirstFrame, nil
		case string(service.VideoInputRoleLastFrame):
			return imageURL, service.VideoInputRoleLastFrame, nil
		case string(service.VideoInputRoleTaggedReferenceImage):
			return imageURL, service.VideoInputRoleTaggedReferenceImage, nil
		}
	case service.VideoContentTypeVideo:
		if videoURL == "" || imageURL != "" || audioURL != "" {
			return "", "", service.ErrVideoInvalidContent
		}
		if role == "" || role == string(service.VideoInputRoleReferenceVideo) {
			return videoURL, service.VideoInputRoleReferenceVideo, nil
		}
	case service.VideoContentTypeAudio:
		if audioURL == "" || imageURL != "" || videoURL != "" {
			return "", "", service.ErrVideoInvalidContent
		}
		if role == "" || role == string(service.VideoInputRoleReferenceAudio) {
			return audioURL, service.VideoInputRoleReferenceAudio, nil
		}
	}
	return "", "", fmt.Errorf("%w: %s role %q", service.ErrVideoInvalidContentRole, itemType, role)
}

// arkStoreDataURI 解码 data:<mime>;base64,<payload> 并写入 spool。只接受图片，大小受内容审核上限约束。
func arkStoreDataURI(ctx context.Context, session *service.VideoSpoolSession, role service.VideoInputRole, dataURI string) (service.VideoInput, error) {
	comma := strings.IndexByte(dataURI, ',')
	if comma <= len("data:") || comma == len(dataURI)-1 {
		return service.VideoInput{}, fmt.Errorf("%w: invalid data URI", service.ErrVideoInvalidContent)
	}
	header := strings.ToLower(dataURI[len("data:"):comma])
	if !strings.HasSuffix(header, ";base64") {
		return service.VideoInput{}, fmt.Errorf("%w: data URI must be base64 encoded", service.ErrVideoInvalidContent)
	}
	mediaType := strings.TrimSuffix(header, ";base64")
	if parsed, _, err := mime.ParseMediaType(mediaType); err != nil || !strings.HasPrefix(parsed, "image/") {
		return service.VideoInput{}, fmt.Errorf("%w: data URI must carry an image media type", service.ErrVideoInvalidContent)
	}
	encoded := dataURI[comma+1:]
	if int64(base64.StdEncoding.DecodedLen(len(encoded))) > arkVideoMaxDataURIPayload+3 {
		return service.VideoInput{}, service.ErrVideoInputTooLarge
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return service.VideoInput{}, fmt.Errorf("%w: invalid base64 payload", service.ErrVideoInvalidContent)
	}
	if int64(len(decoded)) > arkVideoMaxDataURIPayload {
		return service.VideoInput{}, service.ErrVideoInputTooLarge
	}
	filename := string(role) + arkImageExtension(mediaType)
	return session.Store(ctx, role, filename, mediaType, bytes.NewReader(decoded))
}

func arkImageExtension(mediaType string) string {
	switch mediaType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/tiff":
		return ".tiff"
	default:
		return ".img"
	}
}

// applyArkPromptFlags 解析提示词里的弱校验 flag 并写回显式字段（flag 优先，重复取最后）。
// 原文不改动：Ark 自己也会解析同一段文本，二者结果一致，预扣就不会与产出错位。
func applyArkPromptFlags(payload *arkVideoRequestJSON, texts []string) error {
	for _, text := range texts {
		for _, match := range arkVideoPromptFlagPattern.FindAllStringSubmatch(text, -1) {
			key := strings.ToLower(match[1])
			value := strings.TrimSpace(match[2])
			switch key {
			case "rs", "resolution":
				payload.Resolution = value
			case "rt", "ratio":
				payload.Ratio = value
			case "dur", "duration":
				parsed, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("%w: --%s %s", service.ErrVideoInvalidRequest, key, value)
				}
				payload.Duration = arkVideoDurationJSON{set: true, value: parsed}
			case "seed":
				parsed, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return fmt.Errorf("%w: --seed %s", service.ErrVideoInvalidRequest, value)
				}
				payload.Seed = &parsed
			case "cf", "camerafixed", "camera_fixed":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("%w: --%s %s", service.ErrVideoInvalidRequest, key, value)
				}
				payload.CameraFixed = &parsed
			case "wm", "watermark":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("%w: --%s %s", service.ErrVideoInvalidRequest, key, value)
				}
				payload.Watermark = &parsed
			default:
				// 未知 flag（含 --fps）不参与派生，交给上游自行处理或忽略。
			}
		}
	}
	return nil
}

// arkVideoError 把服务错误写成 Ark 风格的错误体（与 videoError 同一信封，补 Ark 的 code 命名）。
func arkVideoError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrVideoTaskNotFound) {
		videoJSONError(c, http.StatusNotFound, "task_not_found", "video task not found")
		return
	}
	videoError(c, err)
}
