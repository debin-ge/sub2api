package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultOpenAIVideoCapabilities(t *testing.T) {
	caps := DefaultOpenAIVideoCapabilities()
	require.True(t, caps.Supports(VideoCapabilityCreate))
	require.True(t, caps.Supports(VideoCapabilityCharacters))
	require.True(t, caps.Supports(VideoCapabilityEdits))
	require.True(t, caps.Supports(VideoCapabilityExtensions))
	for _, variant := range []string{"video", "thumbnail", "spritesheet"} {
		require.True(t, caps.SupportsVariant(variant))
	}
}

func TestValidateVideoCreateCapabilities(t *testing.T) {
	caps := DefaultOpenAIVideoCapabilities()
	referenceImage := VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{
		Role: VideoInputRoleReferenceImage, MIMEType: "image/png", Size: 1024, Width: 1280, Height: 720,
	}}
	require.NoError(t, ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
		Operation: VideoOperationGenerate,
		Model:     OpenAIVideoModelSora2,
		Seconds:   8,
		Size:      "1280x720",
	}, []VideoInput{referenceImage}))

	t.Run("reference video cannot masquerade as input_reference", func(t *testing.T) {
		video := VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{
			Role: VideoInputRoleReferenceImage, MIMEType: "video/mp4", Size: 1024,
		}}
		err := ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2, Seconds: 8, Size: "1280x720",
		}, []VideoInput{video})
		require.ErrorIs(t, err, ErrVideoInputUnsupported)
	})

	t.Run("reference image dimensions must match requested size", func(t *testing.T) {
		mismatched := referenceImage
		mismatched.Width = 720
		mismatched.Height = 1280
		err := ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2, Seconds: 8, Size: "1280x720",
		}, []VideoInput{mismatched})
		require.ErrorIs(t, err, ErrVideoInputUnsupported)
	})

	t.Run("uploaded source video belongs to edit", func(t *testing.T) {
		video := VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{
			Role: VideoInputRoleSourceVideo, MIMEType: "video/mp4", Size: 1024,
		}}
		require.NoError(t, ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationEdit, Model: OpenAIVideoModelSora2Pro, Seconds: 8, Size: "1280x720",
		}, []VideoInput{video}))
		err := ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2Pro, Seconds: 8, Size: "1280x720",
		}, []VideoInput{video})
		require.ErrorIs(t, err, ErrVideoInputUnsupported)

		noUploads := cloneVideoCapabilities(caps)
		noUploads.Operations[VideoCapabilityUploadedVideoEdits] = false
		err = ValidateVideoCreateCapabilities(noUploads, VideoCreateRequest{
			Operation: VideoOperationEdit, Model: OpenAIVideoModelSora2Pro, Seconds: 8, Size: "1280x720",
		}, []VideoInput{video})
		require.ErrorIs(t, err, ErrVideoCapabilityUnsupported)
	})

	t.Run("1080p requires sora pro", func(t *testing.T) {
		err := ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2, Seconds: 8, Size: "1920x1080",
		}, nil)
		require.ErrorIs(t, err, ErrVideoCapabilityUnsupported)
		require.NoError(t, ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2Pro, Seconds: 8, Size: "1920x1080",
		}, nil))
	})

	t.Run("extensions reject binary input", func(t *testing.T) {
		err := ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationExtend, Model: OpenAIVideoModelSora2, Seconds: 8, Size: "1280x720",
		}, []VideoInput{referenceImage})
		require.ErrorIs(t, err, ErrVideoInputUnsupported)
	})

	t.Run("provider declared binary role is operation scoped", func(t *testing.T) {
		role := VideoInputRole("depth_map")
		caps.InputRolesByOperation[VideoOperationEdit][role] = true
		caps.InputMIMETypes[role] = map[string]bool{"application/octet-stream": true}
		input := VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{
			Role: role, MIMEType: "application/octet-stream", Size: 2048,
		}}
		require.NoError(t, ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationEdit, Model: OpenAIVideoModelSora2, Seconds: 8, Size: "1280x720",
		}, []VideoInput{input}))
		err := ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
			Operation: VideoOperationGenerate, Model: OpenAIVideoModelSora2, Seconds: 8, Size: "1280x720",
		}, []VideoInput{input})
		require.ErrorIs(t, err, ErrVideoInputUnsupported)
	})
}

func TestValidateVideoCreateCapabilitiesUsesPublicModelForMappedUpstream(t *testing.T) {
	caps := DefaultOpenAIVideoCapabilities()
	require.NoError(t, ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
		Model:          "custom-video-model",
		RequestedModel: OpenAIVideoModelSora2,
		Operation:      VideoOperationGenerate,
		Seconds:        8,
		Size:           "1280x720",
	}, nil))

	require.NoError(t, ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
		Model:          "custom-upstream-model",
		RequestedModel: "custom-public-model",
		Operation:      VideoOperationGenerate,
		Seconds:        6,
		Size:           "1920x1080",
	}, nil))

	err := ValidateVideoCreateCapabilities(caps, VideoCreateRequest{
		Model:          "custom-upstream-model",
		RequestedModel: OpenAIVideoModelSora2,
		Operation:      VideoOperationGenerate,
		Seconds:        8,
		Size:           "1920x1080",
	}, nil)
	require.ErrorIs(t, err, ErrVideoCapabilityUnsupported)
}
