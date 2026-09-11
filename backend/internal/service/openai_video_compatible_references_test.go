package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAICompatibleVideoReferenceFields(t *testing.T) {
	referenceURL := "https://media.example.com/reference.mp4?preview=1&auth_key=signed"
	fields, err := openAICompatibleVideoReferenceFields(ProviderVideoReferenceMedia{
		AspectRatio:     "16:9",
		ReferenceVideos: []string{referenceURL},
		ReferenceAudios: []string{"data:audio/mpeg;base64,YXVkaW8="},
	})

	require.NoError(t, err)
	require.NotContains(t, fields, "aspect_ratio")
	require.Equal(t, []string{referenceURL}, fields["reference_videos"])
	require.Equal(t, []string{"data:audio/mpeg;base64,YXVkaW8="}, fields["reference_audios"])
}

func TestByteDanceSeedanceValidation(t *testing.T) {
	for _, model := range []string{
		ByteDanceVideoModelSeedance10Lite,
		ByteDanceVideoModelSeedance10Pro,
		// An Ark release this build has never heard of still validates: the
		// account model_mapping is the routing whitelist, not this snapshot.
		"doubao-seedance-1-1-pro-260101",
	} {
		require.NoError(t, validateByteDanceSeedanceRequest(VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: model, Seconds: 10,
		}))
	}
	for name, request := range map[string]VideoCreateRequest{
		"uppercase model": {Operation: VideoOperationGenerate, Model: "Doubao-Seedance-1-0-Pro-250528", Seconds: 10},
		"missing model":   {Operation: VideoOperationGenerate, Seconds: 10},
		"missing seconds": {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro},
		"absurd seconds":  {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 600},
		// Ark derives the frame size from resolution plus ratio, so an OpenAI
		// "WxH" would have to be guessed at — and a wrong guess bills one
		// resolution while generating another.
		"size":   {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, Size: "1280x720"},
		"width":  {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, Width: 1280},
		"height": {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, Height: 720},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, validateByteDanceSeedanceRequest(request))
		})
	}
}

// The Seedance guard keys off a fully qualified Ark prefix. A bare variant name
// must not be mistaken for a Seedance model, or an unrelated model would be
// diverted away from the OpenAI provider.
func TestHasByteDanceSeedanceModelRequiresArkPrefix(t *testing.T) {
	require.True(t, hasByteDanceSeedanceModel(VideoCreateRequest{Model: ByteDanceVideoModelSeedance10Pro}))
	require.True(t, hasByteDanceSeedanceModel(VideoCreateRequest{RequestedModel: "DOUBAO-SEEDANCE-1-0-PRO-250528"}))
	for _, model := range []string{"mini-480p", "pro-720p", "seedance-1-0-pro", "sora-2", ""} {
		require.False(t, hasByteDanceSeedanceModel(VideoCreateRequest{Model: model, RequestedModel: model}), model)
	}
}

func TestLegacyOpenAICompatibleSeedance20Request(t *testing.T) {
	for _, model := range []string{
		"doubao-seedance-2.0-mini-480p",
		"doubao-seedance-2.0-fast-720p",
		"doubao-seedance-2.0-pro-1080p",
		"doubao-seedance-2.0-pro-4k",
	} {
		require.True(t, isLegacyOpenAICompatibleSeedance20Request(VideoCreateRequest{Model: model}), model)
	}
	for _, model := range []string{
		"mini-480p",
		"doubao-seedance-2.0-mini-4k",
		"doubao-seedance-2.0-pro-1440p",
		"Doubao-Seedance-2.0-Pro-720p",
		ByteDanceVideoModelSeedance10Pro,
	} {
		require.False(t, isLegacyOpenAICompatibleSeedance20Request(VideoCreateRequest{Model: model}), model)
	}
}

func TestOpenAICompatibleVideoReferenceFieldsRejectsInvalidCombinations(t *testing.T) {
	tests := []struct {
		name       string
		references ProviderVideoReferenceMedia
	}{
		{name: "both ratio aliases", references: ProviderVideoReferenceMedia{Ratio: "16:9", AspectRatio: "16:9"}},
		{name: "too many videos", references: ProviderVideoReferenceMedia{ReferenceVideos: []string{
			"https://media.example.com/1.mp4", "https://media.example.com/2.mp4",
			"https://media.example.com/3.mp4", "https://media.example.com/4.mp4",
		}}},
		{name: "private video URL", references: ProviderVideoReferenceMedia{ReferenceVideos: []string{"http://127.0.0.1/video.mp4"}}},
		{name: "inline video", references: ProviderVideoReferenceMedia{ReferenceVideos: []string{"data:video/mp4;base64,dmlkZW8="}}},
		// 接口文档 §4.3 与能力目录的 reference_audios.requires_any 都这么承诺，这一层必须同口径。
		{name: "audio alone", references: ProviderVideoReferenceMedia{ReferenceAudios: []string{"https://media.example.com/audio.mp3"}}},
		{name: "first frame and reference video", references: ProviderVideoReferenceMedia{
			FirstImageURL:   "https://media.example.com/first.png",
			ReferenceVideos: []string{"https://media.example.com/reference.mp4"},
		}},
		{name: "duplicate reference", references: ProviderVideoReferenceMedia{ReferenceImages: []string{
			"https://media.example.com/image.png", "https://media.example.com/image.png",
		}}},
		{name: "oversized reference", references: ProviderVideoReferenceMedia{
			ImageURL: "data:image/png;base64," + strings.Repeat("A", openAIVideoMaxReferenceBytes),
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := openAICompatibleVideoReferenceFields(test.references)
			require.Error(t, err)
		})
	}
}
