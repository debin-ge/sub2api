package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// arkTestPNG 生成一张可被 image.DecodeConfig 识别的最小 PNG：spool 会校验图片尺寸。
func arkTestPNG(t *testing.T, fill color.Color) []byte {
	t.Helper()
	canvas := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			canvas.Set(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, canvas))
	return buffer.Bytes()
}

func arkTestDataURI(mediaType string, payload []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(payload)
}

func newArkParseTestHandler(t *testing.T, withSpool bool) *VideoHandler {
	t.Helper()
	cfg := videoHandlerTestConfig(t)
	var spool *service.VideoSubmissionSpool
	if withSpool {
		var err error
		spool, err = service.NewVideoSubmissionSpool(cfg)
		require.NoError(t, err)
	}
	return newVideoHandler(&videoTaskAPIFake{}, spool, cfg)
}

func parseArkTestRequest(t *testing.T, handler *VideoHandler, raw string) (service.VideoSubmitRequest, *service.VideoSpoolSession, error) {
	t.Helper()
	request, session, err := handler.parseArkVideoRequest(context.Background(), []byte(raw), 42)
	if session != nil {
		t.Cleanup(func() { _ = session.Close() })
	}
	return request, session, err
}

func TestParseArkVideoRequestHappyPathAppliesDefaultRolesAndPromptFlags(t *testing.T) {
	prompt := "海浪轻轻拍打沙滩 --rs 1080p --rt 9:16 --dur 10 --seed 7 --cf true --wm false --fps 24 --unknown 1"
	request, session, err := parseArkTestRequest(t, newArkParseTestHandler(t, false), `{
		"model":" doubao-seedance-1-0-pro-250528 ",
		"content":[
			{"type":"text","text":"`+prompt+`"},
			{"type":"image_url","image_url":{"url":"https://media.example.com/first.png"}},
			{"type":"image_url","image_url":"https://media.example.com/last.png","role":"last_frame"},
			{"type":"image_url","image_url":{"url":"https://media.example.com/ref.png"},"role":"reference_image"},
			{"type":"video_url","video_url":{"url":"https://media.example.com/motion.mp4"}},
			{"type":"audio_url","audio_url":"https://media.example.com/beat.mp3"}
		],
		"duration":5,"resolution":"720p","ratio":"16:9","seed":11,"camera_fixed":false,"watermark":true,
		"generate_audio":true,"return_last_frame":true,"service_tier":"default","execution_expires_after":3600,
		"callback_url":"https://example.com/hook","provider_options":{"enhance_prompt":true},
		"priority":null
	}`)
	require.NoError(t, err)
	require.Nil(t, session, "https references never open the spool")
	require.Equal(t, service.VideoOperationGenerate, request.Operation)
	require.Equal(t, "doubao-seedance-1-0-pro-250528", request.Model)
	require.Equal(t, prompt, request.Prompt, "the raw text is forwarded untouched; Ark parses the same flags")

	// flag 覆盖顶层字段。
	require.Equal(t, "1080p", request.Resolution)
	require.Equal(t, "9:16", request.Ratio)
	require.Equal(t, 10, request.Seconds)
	require.False(t, request.SmartDuration)
	require.Zero(t, request.Frames)
	require.NotNil(t, request.Seed)
	require.Equal(t, int64(7), *request.Seed)
	require.NotNil(t, request.CameraFixed)
	require.True(t, *request.CameraFixed)
	require.NotNil(t, request.Watermark)
	require.False(t, *request.Watermark)

	// 没有 flag 的字段按顶层取值。
	require.NotNil(t, request.AudioEnabled)
	require.True(t, *request.AudioEnabled)
	require.NotNil(t, request.ReturnLastFrame)
	require.True(t, *request.ReturnLastFrame)
	require.Equal(t, "default", request.ServiceTier)
	require.Equal(t, 3600, request.ExecutionExpiresAfter)
	require.Equal(t, "https://example.com/hook", request.CallbackURL)
	require.Equal(t, map[string]any{"enhance_prompt": true}, request.ProviderOptions)
	require.Empty(t, request.Inputs)
	require.Empty(t, request.Size)
	require.Empty(t, request.AspectRatio)
	require.Nil(t, request.InputReference)

	// 媒体项默认角色：image → first_frame，video → reference_video，audio → reference_audio。
	require.Equal(t, []service.VideoContentItem{
		{Type: service.VideoContentTypeText, Text: prompt},
		{Type: service.VideoContentTypeImage, Role: service.VideoInputRoleFirstFrame, URL: "https://media.example.com/first.png"},
		{Type: service.VideoContentTypeImage, Role: service.VideoInputRoleLastFrame, URL: "https://media.example.com/last.png"},
		{Type: service.VideoContentTypeImage, Role: service.VideoInputRoleTaggedReferenceImage, URL: "https://media.example.com/ref.png"},
		{Type: service.VideoContentTypeVideo, Role: service.VideoInputRoleReferenceVideo, URL: "https://media.example.com/motion.mp4"},
		{Type: service.VideoContentTypeAudio, Role: service.VideoInputRoleReferenceAudio, URL: "https://media.example.com/beat.mp3"},
	}, request.Content)
}

func TestParseArkVideoRequestSpoolsDataURIImages(t *testing.T) {
	first := arkTestPNG(t, color.NRGBA{R: 255, A: 255})
	last := arkTestPNG(t, color.NRGBA{B: 255, A: 255})
	request, session, err := parseArkTestRequest(t, newArkParseTestHandler(t, true), `{
		"model":"doubao-seedance-1-0-pro-250528",
		"content":[
			{"type":"text","text":"Transition from day to night"},
			{"type":"image_url","image_url":{"url":"`+arkTestDataURI("image/png", first)+`"}},
			{"type":"image_url","image_url":{"url":"`+arkTestDataURI("image/png", last)+`"},"role":"last_frame"},
			{"type":"image_url","image_url":{"url":"https://media.example.com/ref.png"},"role":"reference_image"}
		]
	}`)
	require.NoError(t, err)
	require.NotNil(t, session, "data URIs are decoded into the encrypted spool")
	require.Len(t, request.Inputs, 2)
	require.Len(t, request.Content, 4)

	for i, expected := range []struct {
		role    service.VideoInputRole
		payload []byte
	}{
		{service.VideoInputRoleFirstFrame, first},
		{service.VideoInputRoleLastFrame, last},
	} {
		input := request.Inputs[i]
		digest := sha256.Sum256(expected.payload)
		require.Equal(t, expected.role, input.Role)
		require.Equal(t, "image/png", input.MIMEType)
		require.Equal(t, int64(len(expected.payload)), input.Size)
		require.Equal(t, hex.EncodeToString(digest[:]), input.SHA256)
		require.Equal(t, 2, input.Width)
		require.Equal(t, 2, input.Height)
		reader, openErr := input.Open(context.Background())
		require.NoError(t, openErr)
		stored, readErr := io.ReadAll(reader)
		require.NoError(t, readErr)
		require.NoError(t, reader.Close())
		require.Equal(t, expected.payload, stored)

		item := request.Content[i+1]
		require.Equal(t, service.VideoContentTypeImage, item.Type)
		require.Equal(t, expected.role, item.Role)
		require.Empty(t, item.URL, "spooled data URIs must not be echoed as URLs")
		require.Same(t, &request.Inputs[i], item.Input, "content items point at the final Inputs slot")
	}
	require.Equal(t, "https://media.example.com/ref.png", request.Content[3].URL)
	require.Nil(t, request.Content[3].Input)
	require.True(t, request.HasImageContent())
	require.Len(t, request.ContentByRole(service.VideoInputRoleFirstFrame), 1)

	require.NoError(t, session.Close())
	_, err = request.Inputs[0].Open(context.Background())
	require.Error(t, err, "spool readers are invalid once the session is closed")
}

func TestParseArkVideoRequestRejectsNonImageDataURIs(t *testing.T) {
	handler := newArkParseTestHandler(t, true)
	pngURI := arkTestDataURI("image/png", arkTestPNG(t, color.Black))
	for name, item := range map[string]string{
		"video data uri":       `{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AAAA"}}`,
		"audio data uri":       `{"type":"audio_url","audio_url":{"url":"data:audio/mpeg;base64,AAAA"}}`,
		"video data uri image": `{"type":"video_url","video_url":{"url":"` + pngURI + `"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, session, err := parseArkTestRequest(t, handler, `{"model":"m","content":[{"type":"text","text":"waves"},`+item+`]}`)
			require.ErrorIs(t, err, service.ErrVideoInputUnsupported)
			require.Nil(t, session, "failed parsing must close the spool session")
		})
	}
	for name, uri := range map[string]string{
		"not base64":        "data:image/png,rawbytes",
		"non image type":    "data:text/plain;base64,aGVsbG8=",
		"invalid base64":    "data:image/png;base64,!!!!",
		"empty payload":     "data:image/png;base64,",
		"missing separator": "data:image/png;base64",
	} {
		t.Run(name, func(t *testing.T) {
			_, session, err := parseArkTestRequest(t, handler, `{"model":"m","content":[{"type":"text","text":"waves"},{"type":"image_url","image_url":{"url":"`+uri+`"}}]}`)
			require.Error(t, err)
			require.Nil(t, session)
			require.True(t, infraErrorIsAny(err, service.ErrVideoInvalidContent, service.ErrVideoInputUnsupported), "%v", err)
		})
	}
	t.Run("without a spool data uris are unsupported", func(t *testing.T) {
		_, session, err := parseArkTestRequest(t, newArkParseTestHandler(t, false), `{"model":"m","content":[{"type":"text","text":"waves"},{"type":"image_url","image_url":{"url":"`+pngURI+`"}}]}`)
		require.ErrorIs(t, err, service.ErrVideoInputUnsupported)
		require.Nil(t, session)
	})
}

func infraErrorIsAny(err error, targets ...error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func TestParseArkVideoRequestPromptFlagsLastWinsAndSmartDuration(t *testing.T) {
	handler := newArkParseTestHandler(t, false)
	t.Run("--dur -1 means smart duration", func(t *testing.T) {
		request, _, err := parseArkTestRequest(t, handler, `{"model":"m","duration":5,"content":[{"type":"text","text":"waves --dur -1"}]}`)
		require.NoError(t, err)
		require.True(t, request.SmartDuration)
		require.Zero(t, request.Seconds)
	})
	t.Run("duration -1 top level", func(t *testing.T) {
		request, _, err := parseArkTestRequest(t, handler, `{"model":"m","duration":-1,"content":[{"type":"text","text":"waves"}]}`)
		require.NoError(t, err)
		require.True(t, request.SmartDuration)
		request, _, err = parseArkTestRequest(t, handler, `{"model":"m","duration":"5","content":[{"type":"text","text":"waves"}]}`)
		require.NoError(t, err)
		require.Equal(t, 5, request.Seconds)
		require.False(t, request.SmartDuration)
	})
	t.Run("duplicate flags across text items take the last value", func(t *testing.T) {
		request, _, err := parseArkTestRequest(t, handler, `{"model":"m","resolution":"480p","ratio":"1:1","content":[
			{"type":"text","text":"shot one --rs 720p --ratio 4:3 --seed 1"},
			{"type":"text","text":"shot two --resolution=1080p --rt 21:9 --seed 2 --camerafixed false --watermark true"}
		]}`)
		require.NoError(t, err)
		require.Equal(t, "1080p", request.Resolution)
		require.Equal(t, "21:9", request.Ratio)
		require.Equal(t, int64(2), *request.Seed)
		require.False(t, *request.CameraFixed)
		require.True(t, *request.Watermark)
		require.Equal(t, "shot one --rs 720p --ratio 4:3 --seed 1\n\nshot two --resolution=1080p --rt 21:9 --seed 2 --camerafixed false --watermark true", request.Prompt)
	})
	t.Run("malformed flag values are rejected", func(t *testing.T) {
		for _, text := range []string{"waves --dur five", "waves --seed abc", "waves --cf maybe", "waves --wm 2"} {
			_, _, err := parseArkTestRequest(t, handler, `{"model":"m","content":[{"type":"text","text":"`+text+`"}]}`)
			require.ErrorIs(t, err, service.ErrVideoInvalidRequest, text)
		}
	})
	t.Run("flag values are validated like top-level fields", func(t *testing.T) {
		_, _, err := parseArkTestRequest(t, handler, `{"model":"m","content":[{"type":"text","text":"waves --rs 2160p"}]}`)
		require.ErrorIs(t, err, service.ErrVideoUnsupportedParameter)
		_, _, err = parseArkTestRequest(t, handler, `{"model":"m","content":[{"type":"text","text":"waves --rt 5:4"}]}`)
		require.ErrorIs(t, err, service.ErrVideoUnsupportedParameter)
		_, _, err = parseArkTestRequest(t, handler, `{"model":"m","content":[{"type":"text","text":"waves --dur 0"}]}`)
		require.ErrorIs(t, err, service.ErrVideoInvalidRequest)
	})
}

func TestParseArkVideoRequestRejectsMalformedBodies(t *testing.T) {
	handler := newArkParseTestHandler(t, false)
	text := `{"type":"text","text":"waves"}`
	for name, scenario := range map[string]struct {
		body string
		err  error
	}{
		"duplicate keys":                     {`{"model":"m","model":"n","content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"duplicate nested keys":              {`{"model":"m","content":[{"type":"text","text":"a","text":"b"}]}`, service.ErrVideoInvalidRequest},
		"unknown top-level field":            {`{"model":"m","prompt":"waves","content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"unknown content field":              {`{"model":"m","content":[{"type":"text","text":"waves","weight":1}]}`, service.ErrVideoInvalidRequest},
		"openai size field":                  {`{"model":"m","size":"1280x720","content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"trailing garbage":                   {`{"model":"m","content":[` + text + `]} {}`, service.ErrVideoInvalidRequest},
		"not an object":                      {`[]`, service.ErrVideoInvalidRequest},
		"missing model":                      {`{"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"blank model":                        {`{"model":"  ","content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"empty content":                      {`{"model":"m","content":[]}`, service.ErrVideoInvalidContent},
		"missing content":                    {`{"model":"m"}`, service.ErrVideoInvalidContent},
		"no text item":                       {`{"model":"m","content":[{"type":"image_url","image_url":{"url":"https://x.example/a.png"}}]}`, service.ErrVideoInvalidContent},
		"blank text":                         {`{"model":"m","content":[{"type":"text","text":"   "}]}`, service.ErrVideoInvalidContent},
		"text with role":                     {`{"model":"m","content":[{"type":"text","text":"waves","role":"first_frame"}]}`, service.ErrVideoInvalidContent},
		"text with media":                    {`{"model":"m","content":[{"type":"text","text":"waves","image_url":{"url":"https://x.example/a.png"}}]}`, service.ErrVideoInvalidContent},
		"image with text":                    {`{"model":"m","content":[` + text + `,{"type":"image_url","text":"x","image_url":{"url":"https://x.example/a.png"}}]}`, service.ErrVideoInvalidContent},
		"image without url":                  {`{"model":"m","content":[` + text + `,{"type":"image_url"}]}`, service.ErrVideoInvalidContent},
		"image with video url":               {`{"model":"m","content":[` + text + `,{"type":"image_url","image_url":{"url":"https://x.example/a.png"},"video_url":{"url":"https://x.example/a.mp4"}}]}`, service.ErrVideoInvalidContent},
		"unknown item type":                  {`{"model":"m","content":[` + text + `,{"type":"file","image_url":{"url":"https://x.example/a.png"}}]}`, service.ErrVideoInvalidContent},
		"ftp media url":                      {`{"model":"m","content":[` + text + `,{"type":"image_url","image_url":{"url":"ftp://x.example/a.png"}}]}`, service.ErrVideoInvalidContent},
		"relative media url":                 {`{"model":"m","content":[` + text + `,{"type":"image_url","image_url":{"url":"/a.png"}}]}`, service.ErrVideoInvalidContent},
		"image role reference_video":         {`{"model":"m","content":[` + text + `,{"type":"image_url","image_url":{"url":"https://x.example/a.png"},"role":"reference_video"}]}`, service.ErrVideoInvalidContentRole},
		"video role first_frame":             {`{"model":"m","content":[` + text + `,{"type":"video_url","video_url":{"url":"https://x.example/a.mp4"},"role":"first_frame"}]}`, service.ErrVideoInvalidContentRole},
		"audio role reference_image":         {`{"model":"m","content":[` + text + `,{"type":"audio_url","audio_url":{"url":"https://x.example/a.mp3"},"role":"reference_image"}]}`, service.ErrVideoInvalidContentRole},
		"unknown image role":                 {`{"model":"m","content":[` + text + `,{"type":"image_url","image_url":{"url":"https://x.example/a.png"},"role":"input_reference"}]}`, service.ErrVideoInvalidContentRole},
		"frames and duration":                {`{"model":"m","duration":5,"frames":121,"content":[` + text + `]}`, service.ErrVideoDurationFramesConflict},
		"frames and --dur flag":              {`{"model":"m","frames":121,"content":[{"type":"text","text":"waves --dur 5"}]}`, service.ErrVideoDurationFramesConflict},
		"non-positive frames":                {`{"model":"m","frames":0,"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"zero duration":                      {`{"model":"m","duration":0,"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"duration over limit":                {`{"model":"m","duration":121,"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"fractional duration":                {`{"model":"m","duration":5.5,"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"adaptive without first frame":       {`{"model":"m","ratio":"adaptive","content":[` + text + `]}`, service.ErrVideoAdaptiveRatioRequiresImage},
		"adaptive with only reference image": {`{"model":"m","ratio":"adaptive","content":[` + text + `,{"type":"image_url","image_url":{"url":"https://x.example/a.png"},"role":"reference_image"}]}`, service.ErrVideoAdaptiveRatioRequiresImage},
		"adaptive via flag without image":    {`{"model":"m","content":[{"type":"text","text":"waves --rt adaptive"}]}`, service.ErrVideoAdaptiveRatioRequiresImage},
		"unsupported resolution":             {`{"model":"m","resolution":"2160p","content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"unsupported ratio":                  {`{"model":"m","ratio":"5:4","content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"priority":                           {`{"model":"m","priority":1,"content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"draft":                              {`{"model":"m","draft":true,"content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"safety_identifier":                  {`{"model":"m","safety_identifier":"u1","content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"output_format":                      {`{"model":"m","output_format":"mp4","content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"tools":                              {`{"model":"m","tools":[{"type":"web_search"}],"content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"omni_reference_task_type":           {`{"model":"m","omni_reference_task_type":"x","content":[` + text + `]}`, service.ErrVideoUnsupportedParameter},
		"seed out of range":                  {`{"model":"m","seed":4294967296,"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"seed below -1":                      {`{"model":"m","seed":-2,"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"negative execution_expires_after":   {`{"model":"m","execution_expires_after":-1,"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"blank provider option key":          {`{"model":"m","provider_options":{" ":true},"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
		"null provider option":               {`{"model":"m","provider_options":{"enhance_prompt":null},"content":[` + text + `]}`, service.ErrVideoInvalidRequest},
	} {
		t.Run(name, func(t *testing.T) {
			_, session, err := parseArkTestRequest(t, handler, scenario.body)
			require.ErrorIs(t, err, scenario.err, "%s => %v", scenario.body, err)
			require.Nil(t, session)
		})
	}

	// 对照：adaptive 搭配 first_frame、frames 单独出现、null 的未承接字段都是合法的。
	request, _, err := parseArkTestRequest(t, handler, `{"model":"m","ratio":"adaptive","frames":121,"priority":null,"draft":null,"content":[`+text+`,{"type":"image_url","image_url":{"url":"https://x.example/a.png"}}]}`)
	require.NoError(t, err)
	require.Equal(t, service.ArkVideoRatioAdaptive, request.Ratio)
	require.Equal(t, 121, request.Frames)
	require.Zero(t, request.Seconds)
	require.Equal(t, 1, strings.Count(request.Prompt, "waves"))
}

func TestParseArkSubmitRequestEnforcesTransportRules(t *testing.T) {
	cfg := videoHandlerTestConfig(t)
	cfg.Gateway.Video.TaskJSONBodyMaxBytes = 1 << 20
	handler := newVideoHandler(&videoTaskAPIFake{}, nil, cfg)
	require.Equal(t, int64(1<<20), handler.ArkJSONBodyLimit())
	body := `{"model":"m","content":[{"type":"text","text":"waves"}]}`

	t.Run("json body parses", func(t *testing.T) {
		ctx, _ := newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "application/json; charset=utf-8", strings.NewReader(body))
		request, session, err := handler.parseArkSubmitRequest(ctx, 42)
		require.NoError(t, err)
		require.Nil(t, session)
		require.Equal(t, "m", request.Model)
	})
	t.Run("multipart is not an Ark transport", func(t *testing.T) {
		ctx, _ := newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "multipart/form-data; boundary=x", strings.NewReader(body))
		_, _, err := handler.parseArkSubmitRequest(ctx, 42)
		require.ErrorIs(t, err, service.ErrVideoInvalidRequest)
	})
	t.Run("content encoding is rejected", func(t *testing.T) {
		ctx, _ := newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "application/json", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Encoding", "gzip")
		_, _, err := handler.parseArkSubmitRequest(ctx, 42)
		require.ErrorIs(t, err, service.ErrVideoInvalidRequest)
	})
	t.Run("oversized body is 413", func(t *testing.T) {
		padding := strings.Repeat(" ", int(handler.ArkJSONBodyLimit())+1)
		ctx, _ := newVideoHandlerTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", "application/json", strings.NewReader(body+padding))
		_, _, err := handler.parseArkSubmitRequest(ctx, 42)
		require.ErrorIs(t, err, service.ErrVideoInputTooLarge)
	})
	t.Run("default and clamped limits", func(t *testing.T) {
		require.Equal(t, arkVideoDefaultBodyLimit, newVideoHandler(&videoTaskAPIFake{}, nil, videoHandlerTestConfig(t)).ArkJSONBodyLimit())
		oversized := videoHandlerTestConfig(t)
		oversized.Gateway.Video.TaskJSONBodyMaxBytes = 1 << 40
		require.Equal(t, int64(service.VideoCreateJSONMaxBytes), newVideoHandler(&videoTaskAPIFake{}, nil, oversized).ArkJSONBodyLimit())
	})
}

// execution_expires_after 对所有上游都按 Ark 的取值范围校验：Ark 直连透传，其他上游由本网关执行。
func TestParseArkVideoRequestValidatesExecutionExpiresAfterRange(t *testing.T) {
	handler := newArkParseTestHandler(t, false)
	body := func(value string) string {
		return `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}],"duration":5,"execution_expires_after":` + value + `}`
	}
	for _, value := range []string{"60", "3599", "259201", "-1"} {
		_, _, err := parseArkTestRequest(t, handler, body(value))
		require.ErrorIs(t, err, service.ErrVideoInvalidRequest, "execution_expires_after=%s", value)
	}
	for _, value := range []string{"3600", "86400", "259200"} {
		request, _, err := parseArkTestRequest(t, handler, body(value))
		require.NoError(t, err, "execution_expires_after=%s", value)
		require.NotZero(t, request.ExecutionExpiresAfter)
	}
	// 中性值不在解析层处理：watermark=false 等照常进入请求，由任务服务按上游能力决定是否为空操作。
	request, _, err := parseArkTestRequest(t, handler, `{"model":"bytedance/seedance-2.5/text-to-video","content":[{"type":"text","text":"waves"}],"duration":5,"watermark":false,"return_last_frame":false,"service_tier":"default"}`)
	require.NoError(t, err)
	require.NotNil(t, request.Watermark)
	require.False(t, *request.Watermark)
	require.Equal(t, "default", request.ServiceTier)
}
