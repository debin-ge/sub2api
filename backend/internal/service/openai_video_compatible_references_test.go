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

func TestByteDanceArkRequestValidation(t *testing.T) {
	seed, badSeed, hugeSeed := int64(-1), int64(-2), byteDanceMaxSeed+1
	for name, request := range map[string]VideoCreateRequest{
		"lite":  {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Lite, Seconds: 10},
		"pro":   {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10},
		"flex":  {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, ServiceTier: "flex"},
		"smart": {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, SmartDuration: true},
		// frames 走 25+4n；duration 留空。
		"frames":      {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Frames: 121},
		"random seed": {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Seed: &seed},
		"expiry":      {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, ExecutionExpiresAfter: 3600},
		"adaptive":    {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Resolution: "1080p", Ratio: ArkVideoRatioAdaptive},
		// An Ark release this build has never heard of still validates: the
		// account model_mapping is the routing whitelist, not this snapshot.
		"unknown release": {Operation: VideoOperationGenerate, Model: "doubao-seedance-1-1-pro-260101", Seconds: 10},
	} {
		t.Run("accepts "+name, func(t *testing.T) {
			require.NoError(t, validateByteDanceArkRequest(request))
		})
	}
	for name, request := range map[string]VideoCreateRequest{
		"uppercase model":  {Operation: VideoOperationGenerate, Model: "Doubao-Seedance-1-0-Pro-250528", Seconds: 10},
		"missing model":    {Operation: VideoOperationGenerate, Seconds: 10},
		"missing duration": {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro},
		"absurd duration":  {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 600},
		// Ark derives the frame size from resolution plus ratio, so an OpenAI
		// "WxH" would have to be guessed at — and a wrong guess bills one
		// resolution while generating another.
		"size":            {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, Size: "1280x720"},
		"quality":         {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, Quality: "high"},
		"aspect ratio":    {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, AspectRatio: "16:9"},
		"input reference": {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, InputReference: &ProviderInputReference{FileID: "file_1"}},
		"characters":      {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 10, Characters: []ProviderResourceRef{{ProviderResourceID: "c"}}},
		// duration 与 frames 二选一；frames 必须落在 25+4n 且在 [29, 289] 内。
		"frames and duration":  {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Frames: 29},
		"frames and smart":     {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, SmartDuration: true, Frames: 29},
		"frames off grid":      {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Frames: 30},
		"frames too few":       {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Frames: 25},
		"frames too many":      {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Frames: 293},
		"unknown resolution":   {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Resolution: "8k"},
		"unknown ratio":        {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Ratio: "5:2"},
		"seed below -1":        {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Seed: &badSeed},
		"seed above 2^32-1":    {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, Seed: &hugeSeed},
		"expiry too short":     {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, ExecutionExpiresAfter: 60},
		"expiry too long":      {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, ExecutionExpiresAfter: 259201},
		"unknown service tier": {Operation: VideoOperationGenerate, Model: ByteDanceVideoModelSeedance10Pro, Seconds: 5, ServiceTier: "priority"},
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			require.Error(t, validateByteDanceArkRequest(request))
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

func TestByteDanceRelayModelWithResolutionSuffix(t *testing.T) {
	for _, model := range []string{
		"doubao-seedance-2.0-mini-480p",
		"doubao-seedance-2.0-fast-720p",
		"doubao-seedance-2.0-pro-1080p",
		"doubao-seedance-2.0-pro-4k",
		"doubao-seedance-2.5-720p",
		"doubao-seedance-1.5-pro-1080p",
	} {
		require.True(t, byteDanceRelayModelWithResolutionSuffix(VideoCreateRequest{Model: model}), model)
	}
	for _, model := range []string{
		"mini-480p",
		"doubao-seedance-2.0-pro-1440p",
		"Doubao-Seedance-2.0-Pro-720p",
		"doubao-seedance-2.5",
		ByteDanceVideoModelSeedance10Pro,
	} {
		require.False(t, byteDanceRelayModelWithResolutionSuffix(VideoCreateRequest{Model: model}), model)
	}
	require.True(t, byteDanceRelayModelWithResolutionSuffix(VideoCreateRequest{Model: "ep-relay", RequestedModel: "doubao-seedance-2.5-720p"}), "公开模型名同样参与判断")
}

func TestByteDanceOpenAICompatibleSecondsEncoding(t *testing.T) {
	relay := VideoCreateRequest{Model: "doubao-seedance-2.5-720p", Seconds: 4}
	ark := VideoCreateRequest{Model: ByteDanceVideoModelSeedance10Pro, Seconds: 4}
	plain := &Account{Credentials: map[string]any{}}
	require.Equal(t, "4", byteDanceOpenAICompatibleSeconds(plain, relay), "分辨率写进模型名的中继用字符串")
	require.Equal(t, 4, byteDanceOpenAICompatibleSeconds(plain, ark), "其余用数字")
	require.Equal(t, 4, byteDanceOpenAICompatibleSeconds(nil, ark))
	// 凭据显式指定时覆盖模型名推断。
	asNumber := &Account{Credentials: map[string]any{byteDanceSecondsEncodingCredentialKey: "number"}}
	asString := &Account{Credentials: map[string]any{byteDanceSecondsEncodingCredentialKey: "String"}}
	require.Equal(t, 4, byteDanceOpenAICompatibleSeconds(asNumber, relay))
	require.Equal(t, "4", byteDanceOpenAICompatibleSeconds(asString, ark))
	// 未知取值回到推断。
	unknown := &Account{Credentials: map[string]any{byteDanceSecondsEncodingCredentialKey: "float"}}
	require.Equal(t, "4", byteDanceOpenAICompatibleSeconds(unknown, relay))
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
