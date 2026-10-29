package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func grokSourceProbeInput(raw []byte) VideoInput {
	sum := sha256.Sum256(raw)
	return VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleSourceVideo, MIMEType: "video/mp4", Size: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}
}
func TestGrokSourceVerifierBoundsAndCleanup(t *testing.T) {
	input := grokSourceProbeInput([]byte("frozen source bytes"))
	var path string
	verifier := &GrokVideoSourceVerifier{probe: func(_ context.Context, file *os.File) ([]byte, error) {
		path = file.Name()
		stat, e := file.Stat()
		require.NoError(t, e)
		require.Equal(t, os.FileMode(0600), stat.Mode().Perm())
		b, e := io.ReadAll(file)
		require.NoError(t, e)
		require.Equal(t, "frozen source bytes", string(b))
		return []byte(`{"streams":[{"codec_type":"video","width":1080,"height":1920,"duration":"8.25"}],"format":{"duration":"1"},"frames":[{"best_effort_timestamp_time":"0","duration_time":"4"},{"best_effort_timestamp_time":"4","duration_time":"4.25"}]}`), nil
	}}
	result, err := verifier.Verify(context.Background(), input)
	require.NoError(t, err)
	require.True(t, result.verified)
	require.Equal(t, 8.25, result.Seconds)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	verifier.probe = func(_ context.Context, file *os.File) ([]byte, error) {
		path = file.Name()
		return nil, errors.New("probe failed")
	}
	_, err = verifier.Verify(context.Background(), input)
	require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	input.SHA256 = "tampered"
	called := false
	verifier.probe = func(context.Context, *os.File) ([]byte, error) { called = true; return nil, nil }
	_, err = verifier.Verify(context.Background(), input)
	require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)
	require.False(t, called)
}
func TestGrokSourceVerifierRejectsUnusableSpecifications(t *testing.T) {
	for _, raw := range []string{`{}`, `{"streams":[{"codec_type":"audio","width":100,"height":100,"duration":"8"}]}`, `{"streams":[{"codec_type":"video","width":100,"height":100,"duration":"NaN"}]}`, `{"streams":[{"codec_type":"video","width":100,"height":100,"duration":"16"}]}`, `{"streams":[{"codec_type":"video","width":0,"height":100,"duration":"8"}]}`} {
		v := &GrokVideoSourceVerifier{probe: func(context.Context, *os.File) ([]byte, error) { return []byte(raw), nil }}
		_, err := v.Verify(context.Background(), grokSourceProbeInput([]byte("bytes")))
		require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)
	}
}
func TestGrokSourceVerifierRealMP4(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for media probe integration")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is required")
	}
	path := filepath.Join(t.TempDir(), "input.mp4")
	command := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x48:r=4", "-t", "8.25", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an", "-y", path)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()
	input, err := session.Store(context.Background(), VideoInputRoleSourceVideo, "input.mp4", "video/mp4", bytes.NewReader(raw))
	require.NoError(t, err)
	src, err := NewGrokVideoSourceVerifier().Verify(context.Background(), input)
	require.NoError(t, err)
	require.InDelta(t, 8.25, src.Seconds, 1e-6)
	require.Equal(t, 64, src.Width)
	require.Equal(t, 48, src.Height)
	// ffprobe inspected a private temporary copy; the input is still encrypted in spool.
	encrypted, err := os.ReadDir(session.directory)
	require.NoError(t, err)
	require.Len(t, encrypted, 1)
}
func TestGrokSourceURLRejectsPrivateNetwork(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()
	_, err = NewGrokVideoSourceVerifier().StoreURL(context.Background(), session, "https://127.0.0.1/video.mp4")
	require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)
	require.Empty(t, session.Inputs())
}

func TestGrokDecodedTimelineRejectsUnmeasurableDuration(t *testing.T) {
	seconds, err := grokDecodedVideoSeconds([]grokSourceFrame{{Timestamp: "-2", Duration: "1"}, {Timestamp: "-1", Duration: "0.25"}})
	require.NoError(t, err)
	require.Equal(t, 1.25, seconds)
	for _, frames := range [][]grokSourceFrame{nil, {{Timestamp: "0"}}, {{Timestamp: "0", Duration: "16"}}, {{Timestamp: "2", Duration: "1"}, {Timestamp: "1", Duration: "1"}}, {{Timestamp: "NaN", Duration: "1"}}} {
		_, err = grokDecodedVideoSeconds(frames)
		require.ErrorIs(t, err, ErrVideoSourceSpecUnavailable)
	}
}
