package handler

import (
	"context"
	"encoding/json"
	"image/color"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestParseManagedGrokVideoNativeReferences(t *testing.T) {
	h := newArkParseTestHandler(t, true)
	key := &service.APIKey{ID: 2, UserID: 1}
	image := arkTestDataURI("image/png", arkTestPNG(t, color.RGBA{R: 100, A: 255}))
	payload := map[string]any{"model": "grok-imagine-video-1.5", "prompt": "scene", "duration": 8, "resolution": "480p", "aspect_ratio": "9:16", "reference_images": []any{map[string]any{"url": image}}, "keyframes": []any{map[string]any{"image": map[string]any{"url": "https://example.com/frame.png"}, "timestamp_s": 3}}, "reference_audios": []any{map[string]any{"voice_id": "eve"}}}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	r, session, err := h.parseGrokManagedRequest(context.Background(), key, service.VideoOperationGenerate, raw, nil)
	require.NoError(t, err)
	require.NotNil(t, session)
	defer session.Close()
	require.Equal(t, 8, r.Seconds)
	require.Equal(t, "9:16", r.Ratio)
	require.Len(t, r.Content, 1)
	require.Len(t, r.Inputs, 1)
	require.Equal(t, service.VideoInputRoleTaggedReferenceImage, r.Content[0].Role)
	require.Len(t, r.Grok.Keyframes, 1)
	require.Equal(t, []string{"eve"}, r.Grok.VoiceIDs)
	audit, err := videoModerationBody(context.Background(), r)
	require.NoError(t, err)
	require.Contains(t, string(audit), "https://example.com/frame.png")
	require.Contains(t, string(audit), "data:image/png;base64,")
}
func TestParseManagedGrokRejectsAmbiguousParameters(t *testing.T) {
	for _, raw := range []string{
		`{"model":"grok-imagine-video","prompt":"a","prompt":"b"}`,
		`{"model":"grok-imagine-video","content":[{"text":"a"}]}`,
		`{"model":"grok-imagine-video","prompt":"a","duration":8,"seconds":8}`,
		`{"model":"grok-imagine-video","prompt":"a","image":{"file_id":"unowned"}}`,
		`{"model":"grok-imagine-video","prompt":"a","image":{"url":"http://127.0.0.1/x"}}`,
		`{"model":"grok-imagine-video","prompt":"a","video":{"url":"https://example.com/source.mp4"}}`,
	} {
		h := newArkParseTestHandler(t, true)
		_, session, err := h.parseGrokManagedRequest(context.Background(), &service.APIKey{ID: 2, UserID: 1}, service.VideoOperationGenerate, []byte(raw), nil)
		if session != nil {
			session.Close()
		}
		require.Error(t, err, raw)
	}
}
func TestGrokManagedPublicIDRoundTrip(t *testing.T) {
	id := service.NewVideoTaskID()
	public := grokManagedPublicID(id)
	require.NotEmpty(t, public)
	require.NotEqual(t, id, public)
	internal, err := grokManagedInternalID(public)
	require.NoError(t, err)
	require.Equal(t, id, internal)
	_, err = grokManagedInternalID("invalid-task")
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
}
