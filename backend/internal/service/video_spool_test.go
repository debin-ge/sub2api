package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newVideoSpoolForTest(t *testing.T, mutate func(*config.GatewayVideoSpoolConfig)) *VideoSubmissionSpool {
	t.Helper()
	spoolConfig := config.GatewayVideoSpoolConfig{
		Directory:            t.TempDir(),
		MaxPartBytes:         1024 * 1024,
		MaxRequestBytes:      2 * 1024 * 1024,
		MaxGlobalBytes:       8 * 1024 * 1024,
		MaxUserConcurrency:   2,
		MaxGlobalConcurrency: 4,
		ChunkBytes:           4096,
		OrphanTTLMinutes:     1,
	}
	if mutate != nil {
		mutate(&spoolConfig)
	}
	spool, err := NewVideoSubmissionSpool(&config.Config{
		Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Enabled: true, Spool: spoolConfig}},
	})
	require.NoError(t, err)
	return spool
}

func videoSpoolTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, width, height))))
	return encoded.Bytes()
}

func TestVideoSubmissionSpoolDisabledHasNoFilesystemSideEffects(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "disabled-spool")
	spool, err := NewVideoSubmissionSpool(&config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
		Enabled: false,
		Spool:   config.GatewayVideoSpoolConfig{Directory: directory},
	}}})
	require.NoError(t, err)
	_, err = os.Stat(directory)
	require.True(t, os.IsNotExist(err))
	_, err = spool.Begin(context.Background(), 42)
	require.ErrorIs(t, err, ErrVideoDisabled)
}

func TestVideoSubmissionSpoolRoundTripIsEncryptedAndCleansUp(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	directory := session.directory
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1280, 720))))
	plaintext := encoded.Bytes()

	input, err := session.Store(
		context.Background(),
		VideoInputRoleReferenceImage,
		"reference.png",
		"image/png",
		bytes.NewReader(plaintext),
	)
	require.NoError(t, err)
	require.Equal(t, "image/png", input.MIMEType)
	require.Equal(t, 1280, input.Width)
	require.Equal(t, 720, input.Height)
	require.Equal(t, int64(len(plaintext)), input.Size)
	sum := sha256.Sum256(plaintext)
	require.Equal(t, hex.EncodeToString(sum[:]), input.SHA256)

	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	ciphertext, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	require.NoError(t, err)
	require.False(t, bytes.Contains(ciphertext, []byte("private-video-input")))

	reader, err := input.Open(context.Background())
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, plaintext, decoded)

	require.NoError(t, session.Close())
	_, err = os.Stat(directory)
	require.True(t, os.IsNotExist(err))
	require.Zero(t, spool.diskBytes.Load())
	require.NoError(t, session.Close(), "close must be idempotent")
}

func TestVideoSubmissionSpoolDoesNotTrustSpoofedImageContentType(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()

	input, err := session.Store(
		context.Background(), VideoInputRoleReferenceImage, "fake.png", "image/png",
		bytes.NewReader([]byte{0, 1, 2, 3}),
	)

	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", input.MIMEType)
	require.Zero(t, input.Width)
	require.Zero(t, input.Height)
}

func TestVideoSubmissionSpoolDetectsTamperAndTruncation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{
			name: "tamper",
			mutate: func(t *testing.T, path string) {
				file, err := os.OpenFile(path, os.O_RDWR, 0)
				require.NoError(t, err)
				defer file.Close()
				offset := int64(videoSpoolHeaderBytes + 4 + 8)
				var value [1]byte
				_, err = file.ReadAt(value[:], offset)
				require.NoError(t, err)
				value[0] ^= 0xff
				_, err = file.WriteAt(value[:], offset)
				require.NoError(t, err)
			},
		},
		{
			name: "truncate",
			mutate: func(t *testing.T, path string) {
				info, err := os.Stat(path)
				require.NoError(t, err)
				require.NoError(t, os.Truncate(path, info.Size()-1))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			spool := newVideoSpoolForTest(t, nil)
			session, err := spool.Begin(context.Background(), 42)
			require.NoError(t, err)
			defer session.Close()
			input, err := session.Store(
				context.Background(), VideoInputRoleSourceVideo, "source.mp4", "video/mp4",
				bytes.NewReader(append([]byte("\x00\x00\x00\x18ftypmp42"), bytes.Repeat([]byte("video"), 2000)...)),
			)
			require.NoError(t, err)
			entries, err := os.ReadDir(session.directory)
			require.NoError(t, err)
			path := filepath.Join(session.directory, entries[0].Name())
			test.mutate(t, path)

			reader, err := input.Open(context.Background())
			require.NoError(t, err)
			_, err = io.ReadAll(reader)
			require.ErrorIs(t, err, ErrVideoSpoolCorrupt)
			require.NoError(t, reader.Close())
		})
	}
}

func TestVideoSubmissionSpoolEnforcesRequestAndConcurrencyLimits(t *testing.T) {
	spool := newVideoSpoolForTest(t, func(cfg *config.GatewayVideoSpoolConfig) {
		cfg.MaxPartBytes = 10
		cfg.MaxRequestBytes = 12
		cfg.MaxUserConcurrency = 1
		cfg.MaxGlobalConcurrency = 1
	})
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	_, err = spool.Begin(context.Background(), 42)
	require.ErrorIs(t, err, ErrVideoSpoolLimited)

	_, err = session.Store(context.Background(), VideoInputRoleMask, "mask.bin", "application/octet-stream", bytes.NewReader(bytes.Repeat([]byte{1}, 10)))
	require.NoError(t, err)
	_, err = session.Store(context.Background(), VideoInputRoleMask, "mask2.bin", "application/octet-stream", bytes.NewReader(bytes.Repeat([]byte{2}, 3)))
	require.ErrorIs(t, err, ErrVideoInputTooLarge)
	require.Len(t, session.Inputs(), 1)
	require.NoError(t, session.Close())

	second, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	require.NoError(t, second.Close())
}

func TestVideoSubmissionSpoolAcceptsProviderDefinedRolesAndRejectsUnsafeNames(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	defer session.Close()

	_, err = session.Store(context.Background(), VideoInputRoleReferenceImage, "../secret.png", "image/png", bytes.NewReader([]byte("x")))
	require.ErrorIs(t, err, ErrVideoInvalidRequest)
	input, err := session.Store(context.Background(), VideoInputRole("depth_map"), "input.bin", "application/octet-stream", bytes.NewReader([]byte{0, 1, 2}))
	require.NoError(t, err)
	require.Equal(t, VideoInputRole("depth_map"), input.Role)
	_, err = session.Store(context.Background(), VideoInputRole("../unsafe"), "input.bin", "application/octet-stream", bytes.NewReader([]byte("x")))
	require.ErrorIs(t, err, ErrVideoInputUnsupported)
}

func TestVideoSubmissionSpoolSweepsOnlyExpiredInactiveDirectories(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	orphan := filepath.Join(spool.directory, "request-orphan")
	require.NoError(t, os.Mkdir(orphan, 0o700))
	file := filepath.Join(orphan, "orphan.spool")
	require.NoError(t, os.WriteFile(file, []byte("ciphertext"), 0o600))
	old := time.Now().UTC().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(orphan, old, old))
	spool.diskBytes.Add(int64(len("ciphertext")))

	active, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	require.NoError(t, os.Chtimes(active.directory, old, old))
	removed, err := spool.SweepOrphans(context.Background(), time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	_, err = os.Stat(orphan)
	require.True(t, errors.Is(err, os.ErrNotExist))
	_, err = os.Stat(active.directory)
	require.NoError(t, err)
	require.NoError(t, active.Close())
}

func TestVideoSpoolRuntimeSweepsOnStartAndStopsIdempotently(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	orphan := filepath.Join(spool.directory, "request-startup-orphan")
	require.NoError(t, os.Mkdir(orphan, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "orphan.spool"), []byte("ciphertext"), 0o600))
	old := time.Now().UTC().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(orphan, old, old))

	runtime := NewVideoSpoolRuntime(spool, &config.Config{
		Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Enabled: true}},
	})
	runtime.Start()
	require.Eventually(t, func() bool {
		_, err := os.Stat(orphan)
		return errors.Is(err, os.ErrNotExist)
	}, time.Second, 10*time.Millisecond)
	runtime.Stop()
	require.NotPanics(t, runtime.Stop)
}

func TestVideoSpoolSweepIntervalIsBounded(t *testing.T) {
	require.Equal(t, time.Minute, videoSpoolSweepInterval(time.Minute))
	require.Equal(t, 5*time.Minute, videoSpoolSweepInterval(10*time.Minute))
	require.Equal(t, 10*time.Minute, videoSpoolSweepInterval(time.Hour))
}

func TestVideoSpoolHealthExposesCountsWithoutFilesystemPaths(t *testing.T) {
	spool := newVideoSpoolForTest(t, nil)
	health := spool.Health()
	require.True(t, health.Enabled)
	require.Zero(t, health.ActiveSessions)
	require.Equal(t, int64(8*1024*1024), health.MaxBytes)
	require.Equal(t, "not_run", health.LastSweepResult)

	session, err := spool.Begin(context.Background(), 42)
	require.NoError(t, err)
	_, err = session.Store(context.Background(), VideoInputRoleReferenceImage, "reference.png", "image/png", bytes.NewReader(videoSpoolTestPNG(t, 2, 2)))
	require.NoError(t, err)
	health = spool.Health()
	require.Equal(t, int64(1), health.ActiveSessions)
	require.Positive(t, health.CurrentBytes)
	require.Positive(t, health.Utilization)

	require.NoError(t, session.Close())
	health = spool.Health()
	require.Zero(t, health.ActiveSessions)
	require.Zero(t, health.CurrentBytes)
	require.Zero(t, health.CleanupFailureCount)

	now := time.Now().UTC()
	_, err = spool.SweepOrphans(context.Background(), now)
	require.NoError(t, err)
	health = spool.Health()
	require.Equal(t, "success", health.LastSweepResult)
	require.NotNil(t, health.LastSweepAt)

	encoded, err := json.Marshal(health)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), spool.directory)
	require.NotContains(t, string(encoded), "reference.png")
}
