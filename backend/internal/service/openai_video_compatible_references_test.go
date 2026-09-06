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

func TestOpenAICompatibleSeedance20Validation(t *testing.T) {
	for _, model := range []string{
		"doubao-seedance-2.0-mini-480p",
		"doubao-seedance-2.0-fast-720p",
		"doubao-seedance-2.0-pro-1080p",
		"doubao-seedance-2.0-pro-4k",
	} {
		require.NoError(t, validateOpenAICompatibleSeedance20Request(VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: model, Seconds: 10,
		}))
	}
	for _, request := range []VideoCreateRequest{
		{Operation: VideoOperationGenerate, Model: "doubao-seedance-2.0-mini-4k", Seconds: 10},
		{Operation: VideoOperationGenerate, Model: "doubao-seedance-2.0-pro-720p", Seconds: 3},
		{Operation: VideoOperationGenerate, Model: "doubao-seedance-2.0-pro-720p", Seconds: 16},
		{Operation: VideoOperationGenerate, Model: "Doubao-Seedance-2.0-Pro-720p", Seconds: 10},
	} {
		require.Error(t, validateOpenAICompatibleSeedance20Request(request))
	}
	require.Error(t, validateOpenAICompatibleSeedance20Request(VideoCreateRequest{
		Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2,
		RequestedModel: "doubao-seedance-2.0-pro-720p", Seconds: 16,
	}))
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
