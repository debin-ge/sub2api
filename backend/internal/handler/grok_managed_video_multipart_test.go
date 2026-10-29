package handler

import (
	"bytes"
	"context"
	"image/color"
	"mime/multipart"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func grokMultipartTestBody(t *testing.T, boundary string, png []byte) ([]byte, string) {
	t.Helper()
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	require.NoError(t, writer.SetBoundary(boundary))
	require.NoError(t, writer.WriteField("model", "grok-imagine-video-1.5"))
	require.NoError(t, writer.WriteField("prompt", "a scene"))
	require.NoError(t, writer.WriteField("seconds", "8"))
	require.NoError(t, writer.WriteField("resolution", "480p"))
	part, err := writer.CreateFormFile("input_reference", "image.png")
	require.NoError(t, err)
	_, err = part.Write(png)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return out.Bytes(), writer.FormDataContentType()
}
func TestGrokManagedMultipartCanonicalReplayAndSpoolReuse(t *testing.T) {
	h := newArkParseTestHandler(t, true)
	png := arkTestPNG(t, color.RGBA{R: 99, A: 255})
	raw, contentType := grokMultipartTestBody(t, "first-boundary", png)
	fields, uploads, err := h.parseGrokManagedMultipart(context.Background(), 42, service.VideoOperationGenerate, contentType, raw)
	require.NoError(t, err)
	defer uploads.session.Close()
	key := &service.APIKey{ID: 7, UserID: 42}
	request, session, err := h.parseGrokManagedRequest(context.Background(), key, service.VideoOperationGenerate, fields, nil, uploads)
	require.NoError(t, err)
	require.Same(t, uploads.session, session)
	require.Len(t, request.Inputs, 1)
	require.Equal(t, service.VideoInputRoleFirstFrame, request.Content[0].Role)
	require.Equal(t, 8, request.Seconds)
	changedBody, changedType := grokMultipartTestBody(t, "different-boundary", png)
	repeated, again, err := h.parseGrokManagedMultipart(context.Background(), 42, service.VideoOperationGenerate, changedType, changedBody, uploads.session)
	require.NoError(t, err)
	require.Equal(t, uploads.hash, again.hash)
	require.Equal(t, fields, repeated)
	require.Same(t, uploads.session, again.session)
	require.Len(t, uploads.session.Inputs(), 1, "reuse already encrypted files instead of taking a second spool session")
}
func TestGrokManagedMultipartDetectsChangedImage(t *testing.T) {
	h := newArkParseTestHandler(t, true)
	raw, kind := grokMultipartTestBody(t, "a", arkTestPNG(t, color.RGBA{R: 100, A: 255}))
	_, first, err := h.parseGrokManagedMultipart(context.Background(), 42, service.VideoOperationGenerate, kind, raw)
	require.NoError(t, err)
	defer first.session.Close()
	raw, kind = grokMultipartTestBody(t, "b", arkTestPNG(t, color.RGBA{G: 100, A: 255}))
	_, second, err := h.parseGrokManagedMultipart(context.Background(), 42, service.VideoOperationGenerate, kind, raw)
	require.NoError(t, err)
	defer second.session.Close()
	require.NotEqual(t, first.hash, second.hash)
}
