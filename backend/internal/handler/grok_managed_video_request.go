package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
	"github.com/google/uuid"
)

type grokManagedMediaJSON struct {
	URL      string `json:"url"`
	ImageURL string `json:"image_url,omitempty"`
	Type     string `json:"type,omitempty"`
	ID       string `json:"id,omitempty"`
}
type grokManagedVideoJSON struct {
	Model         string                 `json:"model"`
	Prompt        string                 `json:"prompt"`
	Duration      arkVideoDurationJSON   `json:"duration"`
	Seconds       arkVideoDurationJSON   `json:"seconds"`
	Resolution    string                 `json:"resolution"`
	AspectRatio   string                 `json:"aspect_ratio"`
	GenerateAudio *bool                  `json:"generate_audio"`
	Image         *grokManagedMediaJSON  `json:"image"`
	References    []grokManagedMediaJSON `json:"reference_images"`
	LastFrame     *grokManagedMediaJSON  `json:"last_frame"`
	Keyframes     []struct {
		Image     grokManagedMediaJSON `json:"image"`
		Timestamp float64              `json:"timestamp_s"`
	} `json:"keyframes"`
	Voices []struct {
		VoiceID string `json:"voice_id"`
	} `json:"reference_audios"`
	Video       *grokManagedMediaJSON `json:"video"`
	CallbackURL string                `json:"callback_url"`
}

// This parser normalizes the xAI envelope without teaching the common task
// engine any xAI JSON. Binary references are kept in the existing encrypted spool.
func (h *VideoHandler) parseGrokManagedRequest(ctx context.Context, key *service.APIKey, operation string, raw []byte, verify *service.GrokVideoSourceVerifier, uploaded ...*grokManagedUploads) (request service.VideoSubmitRequest, session *service.VideoSpoolSession, err error) {
	var uploads *grokManagedUploads
	if len(uploaded) > 0 {
		uploads = uploaded[0]
		if uploads != nil {
			session = uploads.session
		}
	}
	if key == nil {
		return request, nil, service.ErrVideoInvalidRequest
	}
	if err = jsonstrict.RejectDuplicateKeys(raw); err != nil {
		return request, nil, service.ErrVideoInvalidRequest
	}
	var payload grokManagedVideoJSON
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&payload); err != nil {
		return request, nil, fmt.Errorf("%w: invalid Grok video fields", service.ErrVideoInvalidRequest)
	}
	if err = ensureJSONEOF(decoder); err != nil {
		return request, nil, service.ErrVideoInvalidRequest
	}
	if payload.Duration.set && payload.Seconds.set {
		return request, nil, service.ErrVideoInvalidRequest
	}
	duration := payload.Duration
	if !duration.set {
		duration = payload.Seconds
	}
	if len(payload.References) > 14 || len(payload.Keyframes) > 4 || len(payload.Voices) > 3 || int64(len(payload.Prompt)) > videoScalarFieldMaxBytes || len(payload.Model) > 200 {
		return request, nil, service.ErrVideoInputTooLarge
	}
	request = service.VideoSubmitRequest{APIKey: key, Operation: operation, Model: strings.TrimSpace(payload.Model), Prompt: strings.TrimSpace(payload.Prompt), Seconds: duration.value, Resolution: strings.TrimSpace(payload.Resolution), Ratio: strings.TrimSpace(payload.AspectRatio), AudioEnabled: payload.GenerateAudio, CallbackURL: strings.TrimSpace(payload.CallbackURL), Grok: &service.GrokVideoOptions{}}
	if request.Model == "" {
		return request, nil, service.ErrVideoInvalidRequest
	}
	ensureSession := func() error {
		if session != nil {
			return nil
		}
		if h == nil || h.spool == nil {
			return service.ErrVideoInputUnsupported
		}
		var e error
		session, e = h.spool.Begin(ctx, key.UserID)
		return e
	}
	mediaURL := func(media grokManagedMediaJSON) (string, error) {
		if media.ID != "" || media.Type != "" && media.Type != "image_url" || media.URL != "" && media.ImageURL != "" {
			return "", service.ErrVideoInvalidContent
		}
		value := media.URL
		if value == "" {
			value = media.ImageURL
		}
		if strings.TrimSpace(value) == "" {
			return "", service.ErrVideoInvalidContent
		}
		return strings.TrimSpace(value), nil
	}
	image := func(media grokManagedMediaJSON, role service.VideoInputRole) (service.VideoContentItem, error) {
		item := service.VideoContentItem{Type: service.VideoContentTypeImage, Role: role}
		value, e := mediaURL(media)
		if e != nil {
			return item, e
		}
		if strings.HasPrefix(value, "data:") {
			if e = ensureSession(); e != nil {
				return item, e
			}
			input, e := arkStoreDataURI(ctx, session, role, value)
			if e != nil {
				return item, e
			}
			request.Inputs = append(request.Inputs, input)
			item.Input = &request.Inputs[len(request.Inputs)-1]
		} else {
			parsed, e := url.Parse(value)
			if e != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return item, service.ErrVideoInvalidContent
			}
			item.URL = value
		}
		return item, nil
	}
	appendImage := func(media *grokManagedMediaJSON, role service.VideoInputRole) error {
		if media == nil {
			return nil
		}
		item, e := image(*media, role)
		if e == nil {
			request.Content = append(request.Content, item)
		}
		return e
	}
	if err = appendImage(payload.Image, service.VideoInputRoleFirstFrame); err != nil {
		return request, session, err
	}
	if err = appendImage(payload.LastFrame, service.VideoInputRoleLastFrame); err != nil {
		return request, session, err
	}
	for _, ref := range payload.References {
		if err = appendImage(&ref, service.VideoInputRoleTaggedReferenceImage); err != nil {
			return request, session, err
		}
	}
	for _, frame := range payload.Keyframes {
		item, e := image(frame.Image, service.VideoInputRoleReferenceImage)
		if e != nil {
			return request, session, e
		}
		request.Grok.Keyframes = append(request.Grok.Keyframes, service.GrokVideoKeyframe{Image: item, TimestampSeconds: frame.Timestamp})
	}
	for _, voice := range payload.Voices {
		request.Grok.VoiceIDs = append(request.Grok.VoiceIDs, voice.VoiceID)
	}
	if uploads != nil {
		for _, upload := range uploads.images {
			input := upload.input
			request.Inputs = append(request.Inputs, input)
			request.Content = append(request.Content, service.VideoContentItem{Type: service.VideoContentTypeImage, Role: input.Role, Input: &request.Inputs[len(request.Inputs)-1]})
		}
	}
	var sourceUpload *service.VideoInput
	if uploads != nil {
		sourceUpload = uploads.source
	}
	if operation == service.VideoOperationGenerate {
		if payload.Video != nil || sourceUpload != nil {
			return request, session, service.ErrVideoInputUnsupported
		}
		return request, session, nil
	}
	if operation != service.VideoOperationEdit && operation != service.VideoOperationExtend {
		return request, session, service.ErrVideoCapabilityUnsupported
	}
	if (payload.Video == nil && sourceUpload == nil) || (payload.Video != nil && sourceUpload != nil) || (uploads != nil && len(uploads.images) > 0) || payload.Image != nil || payload.LastFrame != nil || len(payload.References) > 0 || len(payload.Keyframes) > 0 || len(payload.Voices) > 0 || payload.GenerateAudio != nil || payload.Resolution != "" || payload.AspectRatio != "" || (operation == service.VideoOperationEdit && duration.set) {
		return request, session, service.ErrVideoInvalidRequest
	}
	ctx, cancelSource := context.WithTimeout(ctx, 20*time.Second)
	defer cancelSource()
	if err = ensureSession(); err != nil {
		return request, session, err
	}
	if verify == nil {
		verify = service.NewGrokVideoSourceVerifier()
	}
	src := payload.Video
	if sourceUpload != nil {
		request.Grok.Source, err = verify.Verify(ctx, *sourceUpload)
	} else if src.ID != "" {
		if src.URL != "" || src.ImageURL != "" || src.Type != "" {
			return request, session, service.ErrVideoInvalidContent
		}
		id, e := grokManagedInternalID(src.ID)
		if e != nil {
			return request, session, e
		}
		task, e := h.tasks.GetContentTaskForOwner(ctx, key.UserID, id)
		if e != nil {
			return request, session, e
		}
		if task.Provider != service.VideoProviderGrok || task.APIKeyID == nil || *task.APIKeyID != key.ID || task.GroupID == nil || key.GroupID == nil || *task.GroupID != *key.GroupID {
			return request, session, service.ErrVideoTaskNotFound
		}
		content, e := h.tasks.OpenContentForTask(ctx, task, service.ProviderContentRequest{Method: "GET", Variant: "video"})
		if e != nil {
			return request, session, e
		}
		input, e := session.Store(ctx, service.VideoInputRoleSourceVideo, "source.mp4", "video/mp4", content.Body)
		content.Body.Close()
		if e != nil {
			return request, session, e
		}
		request.Grok.Source, err = verify.Verify(ctx, input)
		request.SourceVideoID = id
	} else {
		if src.URL == "" || src.ImageURL != "" || src.Type != "" {
			return request, session, service.ErrVideoInvalidContent
		}
		request.Grok.Source, err = verify.StoreURL(ctx, session, src.URL)
	}
	if err != nil {
		return request, session, err
	}
	request.Inputs = append(request.Inputs, request.Grok.Source.Input)
	return request, session, nil
}

func grokManagedInternalID(reference string) (string, error) {
	if service.IsValidVideoTaskID(reference) {
		return reference, nil
	}
	id, err := uuid.Parse(reference)
	if err != nil {
		return "", service.ErrVideoTaskNotFound
	}
	return "video_" + strings.ReplaceAll(id.String(), "-", ""), nil
}
func grokManagedPublicID(id string) string {
	if !service.IsValidVideoTaskID(id) {
		return ""
	}
	parsed, err := uuid.Parse(strings.TrimPrefix(id, "video_"))
	if err != nil {
		return ""
	}
	return parsed.String()
}

// Retain the existing URL-string and image_url alias forms, while rejecting
// ambiguous objects and unknown nested fields before any hold is created.
func (m *grokManagedMediaJSON) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return service.ErrVideoInvalidContent
	}
	if raw[0] == '"' {
		return json.Unmarshal(raw, &m.URL)
	}
	var value struct {
		URL      string          `json:"url"`
		Type     string          `json:"type"`
		ID       string          `json:"id"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	m.URL, m.Type, m.ID = value.URL, value.Type, value.ID
	if len(value.ImageURL) > 0 {
		if value.ImageURL[0] == '"' {
			return json.Unmarshal(value.ImageURL, &m.ImageURL)
		}
		var nested struct {
			URL string `json:"url"`
		}
		d := json.NewDecoder(bytes.NewReader(value.ImageURL))
		d.DisallowUnknownFields()
		if err := d.Decode(&nested); err != nil {
			return err
		}
		m.ImageURL = nested.URL
	}
	return nil
}
