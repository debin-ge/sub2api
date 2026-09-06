package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	_ "golang.org/x/image/webp"
)

const (
	videoSpoolMagic       = "S2VSP001"
	videoSpoolHeaderBytes = len(videoSpoolMagic) + 8 + 4
)

type VideoSubmissionSpool struct {
	enabled              bool
	directory            string
	maxPartBytes         int64
	maxRequestBytes      int64
	maxGlobalBytes       int64
	maxUserConcurrency   int
	maxGlobalConcurrency int
	chunkBytes           int
	orphanTTL            time.Duration

	diskBytes       atomic.Int64
	orphans         atomic.Int64
	failures        atomic.Uint64
	mu              sync.Mutex
	active          map[string]struct{}
	byUser          map[int64]int
	global          int
	healthMu        sync.RWMutex
	lastSweepAt     *time.Time
	lastSweepResult string
}

type VideoSpoolHealth struct {
	Enabled             bool       `json:"enabled"`
	ActiveSessions      int64      `json:"active_sessions"`
	CurrentBytes        int64      `json:"current_bytes"`
	MaxBytes            int64      `json:"max_bytes"`
	Utilization         float64    `json:"utilization"`
	OrphanCandidates    int64      `json:"orphan_candidates"`
	LastSweepAt         *time.Time `json:"last_sweep_at,omitempty"`
	LastSweepResult     string     `json:"last_sweep_result"`
	CleanupFailureCount uint64     `json:"cleanup_failure_count"`
}

type VideoSpoolSession struct {
	spool      *VideoSubmissionSpool
	userID     int64
	directory  string
	mu         sync.Mutex
	inputs     []VideoInput
	plainBytes int64
	diskBytes  int64
	closed     bool
}

type videoSpooledFile struct {
	path        string
	key         [32]byte
	noncePrefix [8]byte
	chunkBytes  int
	manifest    VideoInputManifestEntry
}

func NewVideoSubmissionSpool(cfg *config.Config) (*VideoSubmissionSpool, error) {
	if cfg == nil {
		return nil, errors.New("video spool config is required")
	}
	spoolCfg := cfg.Gateway.Video.Spool
	if !cfg.Gateway.Video.Enabled {
		return &VideoSubmissionSpool{
			active: make(map[string]struct{}), byUser: make(map[int64]int),
		}, nil
	}
	directory, err := filepath.Abs(strings.TrimSpace(spoolCfg.Directory))
	if err != nil || directory == "" {
		return nil, errors.New("video spool directory is invalid")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create video spool directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("restrict video spool directory: %w", err)
	}
	spool := &VideoSubmissionSpool{
		enabled: true, directory: directory, maxPartBytes: spoolCfg.MaxPartBytes,
		maxRequestBytes: spoolCfg.MaxRequestBytes, maxGlobalBytes: spoolCfg.MaxGlobalBytes,
		maxUserConcurrency:   spoolCfg.MaxUserConcurrency,
		maxGlobalConcurrency: spoolCfg.MaxGlobalConcurrency,
		chunkBytes:           spoolCfg.ChunkBytes,
		orphanTTL:            time.Duration(spoolCfg.OrphanTTLMinutes) * time.Minute,
		active:               make(map[string]struct{}), byUser: make(map[int64]int),
	}
	if spool.chunkBytes <= 0 {
		spool.chunkBytes = 64 * 1024
	}
	if spool.maxGlobalBytes <= 0 || spool.maxRequestBytes <= 0 || spool.maxPartBytes <= 0 ||
		spool.maxUserConcurrency <= 0 || spool.maxGlobalConcurrency <= 0 || spool.orphanTTL <= 0 {
		return nil, errors.New("video spool limits are invalid")
	}
	initialBytes, err := directorySize(directory)
	if err != nil {
		return nil, fmt.Errorf("inspect video spool directory: %w", err)
	}
	spool.diskBytes.Store(initialBytes)
	spool.publishHealth()
	return spool, nil
}

func (s *VideoSubmissionSpool) Begin(ctx context.Context, userID int64) (*VideoSpoolSession, error) {
	if s == nil || userID <= 0 {
		return nil, ErrVideoInvalidRequest
	}
	if !s.enabled {
		return nil, ErrVideoDisabled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.global >= s.maxGlobalConcurrency || s.byUser[userID] >= s.maxUserConcurrency {
		s.mu.Unlock()
		return nil, ErrVideoSpoolLimited
	}
	s.global++
	s.byUser[userID]++
	s.mu.Unlock()

	directory, err := os.MkdirTemp(s.directory, "request-")
	if err != nil {
		s.releaseSession(userID, "")
		return nil, fmt.Errorf("create video spool request directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.RemoveAll(directory)
		s.releaseSession(userID, "")
		return nil, fmt.Errorf("restrict video spool request directory: %w", err)
	}
	s.mu.Lock()
	s.active[directory] = struct{}{}
	s.mu.Unlock()
	s.publishHealth()
	return &VideoSpoolSession{spool: s, userID: userID, directory: directory}, nil
}

func (s *VideoSubmissionSpool) releaseSession(userID int64, directory string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if directory != "" {
		delete(s.active, directory)
	}
	if s.global > 0 {
		s.global--
	}
	if s.byUser[userID] <= 1 {
		delete(s.byUser, userID)
	} else {
		s.byUser[userID]--
	}
}

func (s *VideoSpoolSession) Store(
	ctx context.Context,
	role VideoInputRole,
	filename string,
	contentTypeHint string,
	source io.Reader,
) (VideoInput, error) {
	if s == nil || s.spool == nil || source == nil {
		return VideoInput{}, ErrVideoInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return VideoInput{}, errors.New("video spool session is closed")
	}
	if err := validateVideoSpoolRole(role); err != nil {
		return VideoInput{}, err
	}
	filename, err := validateVideoSpoolFilename(filename)
	if err != nil {
		return VideoInput{}, err
	}
	if err := ctx.Err(); err != nil {
		return VideoInput{}, err
	}

	file, err := s.newFile(role)
	if err != nil {
		return VideoInput{}, err
	}
	plainSize, diskSize, digest, detectedMIME, err := s.writeEncrypted(ctx, file, source, contentTypeHint)
	if err != nil {
		_ = os.Remove(file.path)
		if diskSize > 0 {
			s.spool.diskBytes.Add(-diskSize)
		}
		return VideoInput{}, err
	}
	width, height := 0, 0
	if strings.HasPrefix(detectedMIME, "image/") {
		width, height, err = videoSpoolImageDimensions(file)
		if err != nil {
			_ = os.Remove(file.path)
			s.spool.diskBytes.Add(-diskSize)
			return VideoInput{}, ErrVideoInputUnsupported
		}
	}
	s.plainBytes += plainSize
	s.diskBytes += diskSize
	file.manifest = VideoInputManifestEntry{
		Role: role, FileName: filename, MIMEType: detectedMIME,
		Size: plainSize, SHA256: digest, Width: width, Height: height,
	}
	input := VideoInput{
		VideoInputManifestEntry: file.manifest,
		Open: func(openCtx context.Context) (io.ReadCloser, error) {
			if err := openCtx.Err(); err != nil {
				return nil, err
			}
			return file.open()
		},
	}
	s.inputs = append(s.inputs, input)
	return input, nil
}

func (s *VideoSpoolSession) newFile(role VideoInputRole) (*videoSpooledFile, error) {
	var file videoSpooledFile
	file.chunkBytes = s.spool.chunkBytes
	if _, err := io.ReadFull(rand.Reader, file.key[:]); err != nil {
		return nil, fmt.Errorf("generate video spool key: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, file.noncePrefix[:]); err != nil {
		return nil, fmt.Errorf("generate video spool nonce: %w", err)
	}
	nameBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nameBytes); err != nil {
		return nil, fmt.Errorf("generate video spool filename: %w", err)
	}
	file.path = filepath.Join(s.directory, hex.EncodeToString(nameBytes)+".spool")
	return &file, nil
}

func (s *VideoSpoolSession) writeEncrypted(
	ctx context.Context,
	file *videoSpooledFile,
	source io.Reader,
	contentTypeHint string,
) (plainSize, diskSize int64, digest, detectedMIME string, returnErr error) {
	block, err := aes.NewCipher(file.key[:])
	if err != nil {
		return 0, 0, "", "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return 0, 0, "", "", err
	}
	destination, err := os.OpenFile(file.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, 0, "", "", err
	}
	defer func() {
		if closeErr := destination.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()

	header := make([]byte, videoSpoolHeaderBytes)
	copy(header, videoSpoolMagic)
	copy(header[len(videoSpoolMagic):], file.noncePrefix[:])
	binary.BigEndian.PutUint32(header[len(videoSpoolMagic)+8:], uint32(file.chunkBytes))
	if !s.spool.reserveDisk(int64(len(header))) {
		return 0, 0, "", "", ErrVideoSpoolLimited
	}
	diskSize += int64(len(header))
	if _, err := destination.Write(header); err != nil {
		return 0, diskSize, "", "", err
	}

	hasher := sha256.New()
	buffer := make([]byte, file.chunkBytes)
	sniff := make([]byte, 0, 512)
	var counter uint32
	for {
		if err := ctx.Err(); err != nil {
			return plainSize, diskSize, "", "", err
		}
		n, readErr := io.ReadFull(source, buffer)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return plainSize, diskSize, "", "", readErr
		}
		if n > 0 {
			if plainSize+int64(n) > s.spool.maxPartBytes || s.plainBytes+plainSize+int64(n) > s.spool.maxRequestBytes {
				return plainSize, diskSize, "", "", ErrVideoInputTooLarge
			}
			chunk := buffer[:n]
			_, _ = hasher.Write(chunk)
			if len(sniff) < cap(sniff) {
				need := cap(sniff) - len(sniff)
				if need > n {
					need = n
				}
				sniff = append(sniff, chunk[:need]...)
			}
			sealed := aead.Seal(nil, videoSpoolNonce(file.noncePrefix, counter), chunk, videoSpoolAAD(counter, uint32(n), false))
			recordBytes := int64(4 + len(sealed))
			if !s.spool.reserveDisk(recordBytes) {
				return plainSize, diskSize, "", "", ErrVideoSpoolLimited
			}
			diskSize += recordBytes
			if err := binary.Write(destination, binary.BigEndian, uint32(n)); err != nil {
				return plainSize, diskSize, "", "", err
			}
			if _, err := destination.Write(sealed); err != nil {
				return plainSize, diskSize, "", "", err
			}
			plainSize += int64(n)
			counter++
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
	}

	sealedFinal := aead.Seal(nil, videoSpoolNonce(file.noncePrefix, counter), nil, videoSpoolAAD(counter, 0, true))
	finalBytes := int64(4 + len(sealedFinal))
	if !s.spool.reserveDisk(finalBytes) {
		return plainSize, diskSize, "", "", ErrVideoSpoolLimited
	}
	diskSize += finalBytes
	if err := binary.Write(destination, binary.BigEndian, uint32(0)); err != nil {
		return plainSize, diskSize, "", "", err
	}
	if _, err := destination.Write(sealedFinal); err != nil {
		return plainSize, diskSize, "", "", err
	}
	if err := destination.Sync(); err != nil {
		return plainSize, diskSize, "", "", err
	}

	detectedMIME = normalizeVideoContentType(http.DetectContentType(sniff))
	return plainSize, diskSize, hex.EncodeToString(hasher.Sum(nil)), detectedMIME, nil
}

func videoSpoolImageDimensions(file *videoSpooledFile) (width, height int, returnErr error) {
	reader, err := file.open()
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if closeErr := reader.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()
	configuration, _, err := image.DecodeConfig(reader)
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 {
		return 0, 0, ErrVideoInputUnsupported
	}
	return configuration.Width, configuration.Height, nil
}

func (s *VideoSubmissionSpool) reserveDisk(bytes int64) bool {
	for {
		current := s.diskBytes.Load()
		if bytes < 0 || current > s.maxGlobalBytes-bytes {
			return false
		}
		if s.diskBytes.CompareAndSwap(current, current+bytes) {
			return true
		}
	}
}

func (s *VideoSpoolSession) Inputs() []VideoInput {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]VideoInput(nil), s.inputs...)
}

func (s *VideoSpoolSession) Close() error {
	if s == nil || s.spool == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	directory := s.directory
	diskBytes := s.diskBytes
	s.mu.Unlock()

	err := os.RemoveAll(directory)
	if err == nil {
		s.spool.diskBytes.Add(-diskBytes)
		observability.DefaultVideoMetrics().RecordSpoolCleanup("session", "success")
	} else {
		s.spool.failures.Add(1)
		observability.DefaultVideoMetrics().RecordSpoolCleanup("session", "error")
	}
	s.spool.releaseSession(s.userID, directory)
	s.spool.publishHealth()
	return err
}

func (s *VideoSubmissionSpool) SweepOrphans(ctx context.Context, now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		s.recordSweep(now, 0, 1, err)
		return 0, err
	}
	removed := 0
	remaining := int64(0)
	failureCount := uint64(0)
	var failures []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "request-") {
			continue
		}
		path := filepath.Join(s.directory, entry.Name())
		s.mu.Lock()
		_, active := s.active[path]
		s.mu.Unlock()
		if active {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			failureCount++
			failures = append(failures, err)
			continue
		}
		if now.Sub(info.ModTime()) < s.orphanTTL {
			continue
		}
		bytes, err := directorySize(path)
		if err != nil {
			remaining++
			failureCount++
			failures = append(failures, err)
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			remaining++
			failureCount++
			failures = append(failures, err)
			continue
		}
		s.diskBytes.Add(-bytes)
		removed++
	}
	joined := errors.Join(failures...)
	s.recordSweep(now, remaining, failureCount, joined)
	return removed, joined
}

func (s *VideoSubmissionSpool) Health() VideoSpoolHealth {
	if s == nil {
		return VideoSpoolHealth{LastSweepResult: "unavailable"}
	}
	s.mu.Lock()
	active := s.global
	s.mu.Unlock()
	s.healthMu.RLock()
	lastSweepAt := s.lastSweepAt
	lastSweepResult := s.lastSweepResult
	s.healthMu.RUnlock()
	bytes := s.diskBytes.Load()
	maximum := s.maxGlobalBytes
	utilization := 0.0
	if maximum > 0 && bytes > 0 {
		utilization = float64(bytes) / float64(maximum)
	}
	if lastSweepResult == "" {
		if s.enabled {
			lastSweepResult = "not_run"
		} else {
			lastSweepResult = "disabled"
		}
	}
	var copiedSweepAt *time.Time
	if lastSweepAt != nil {
		value := *lastSweepAt
		copiedSweepAt = &value
	}
	return VideoSpoolHealth{
		Enabled: s.enabled, ActiveSessions: int64(active), CurrentBytes: bytes,
		MaxBytes: maximum, Utilization: utilization, OrphanCandidates: s.orphans.Load(),
		LastSweepAt: copiedSweepAt, LastSweepResult: lastSweepResult,
		CleanupFailureCount: s.failures.Load(),
	}
}

func (s *VideoSubmissionSpool) recordSweep(now time.Time, remaining int64, failureCount uint64, err error) {
	if s == nil {
		return
	}
	s.orphans.Store(remaining)
	if failureCount > 0 {
		s.failures.Add(failureCount)
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	value := now.UTC()
	s.healthMu.Lock()
	s.lastSweepAt = &value
	s.lastSweepResult = result
	s.healthMu.Unlock()
	observability.DefaultVideoMetrics().RecordSpoolCleanup("orphan", result)
	s.publishHealth()
}

func (s *VideoSubmissionSpool) publishHealth() {
	if s == nil {
		return
	}
	health := s.Health()
	observability.DefaultVideoMetrics().SetSpoolHealth(health.ActiveSessions, health.CurrentBytes, health.MaxBytes, health.OrphanCandidates)
}

type VideoSpoolRuntime struct {
	spool  *VideoSubmissionSpool
	cfg    *config.Config
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

func NewVideoSpoolRuntime(spool *VideoSubmissionSpool, cfg *config.Config) *VideoSpoolRuntime {
	return &VideoSpoolRuntime{spool: spool, cfg: cfg}
}

func (r *VideoSpoolRuntime) Start() {
	if r == nil || r.spool == nil || r.cfg == nil || !r.cfg.Gateway.Video.Enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel, r.done = cancel, done
	go func() {
		defer close(done)
		r.sweep(ctx)
		ticker := time.NewTicker(videoSpoolSweepInterval(r.spool.orphanTTL))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				r.sweepAt(ctx, now.UTC())
			}
		}
	}()
}

func (r *VideoSpoolRuntime) sweep(ctx context.Context) {
	r.sweepAt(ctx, time.Now().UTC())
}

func (r *VideoSpoolRuntime) sweepAt(ctx context.Context, now time.Time) {
	removed, err := r.spool.SweepOrphans(ctx, now)
	if err != nil && ctx.Err() == nil {
		slog.Error("video spool orphan sweep failed", "error", err)
		return
	}
	if removed > 0 {
		slog.Info("video spool orphan sweep completed", "removed_directories", removed)
	}
}

func (r *VideoSpoolRuntime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func ProvideVideoSpoolRuntime(spool *VideoSubmissionSpool, cfg *config.Config) *VideoSpoolRuntime {
	runtime := NewVideoSpoolRuntime(spool, cfg)
	runtime.Start()
	return runtime
}

func videoSpoolSweepInterval(orphanTTL time.Duration) time.Duration {
	interval := orphanTTL / 2
	if interval < time.Minute {
		return time.Minute
	}
	if interval > 10*time.Minute {
		return 10 * time.Minute
	}
	return interval
}

func (file *videoSpooledFile) open() (io.ReadCloser, error) {
	source, err := os.Open(file.path)
	if err != nil {
		return nil, err
	}
	header := make([]byte, videoSpoolHeaderBytes)
	if _, err := io.ReadFull(source, header); err != nil {
		_ = source.Close()
		return nil, ErrVideoSpoolCorrupt
	}
	if string(header[:len(videoSpoolMagic)]) != videoSpoolMagic ||
		!equalBytes(header[len(videoSpoolMagic):len(videoSpoolMagic)+8], file.noncePrefix[:]) ||
		int(binary.BigEndian.Uint32(header[len(videoSpoolMagic)+8:])) != file.chunkBytes {
		_ = source.Close()
		return nil, ErrVideoSpoolCorrupt
	}
	block, err := aes.NewCipher(file.key[:])
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	return &videoSpoolDecryptReader{source: source, aead: aead, noncePrefix: file.noncePrefix, chunkBytes: file.chunkBytes}, nil
}

type videoSpoolDecryptReader struct {
	source      *os.File
	aead        cipher.AEAD
	noncePrefix [8]byte
	chunkBytes  int
	counter     uint32
	plain       []byte
	plainOffset int
	done        bool
}

func (r *videoSpoolDecryptReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.plainOffset < len(r.plain) {
		n := copy(p, r.plain[r.plainOffset:])
		r.plainOffset += n
		return n, nil
	}
	if r.done {
		return 0, io.EOF
	}
	var plainLength uint32
	if err := binary.Read(r.source, binary.BigEndian, &plainLength); err != nil {
		return 0, ErrVideoSpoolCorrupt
	}
	if plainLength > uint32(r.chunkBytes) {
		return 0, ErrVideoSpoolCorrupt
	}
	sealed := make([]byte, int(plainLength)+r.aead.Overhead())
	if _, err := io.ReadFull(r.source, sealed); err != nil {
		return 0, ErrVideoSpoolCorrupt
	}
	final := plainLength == 0
	plain, err := r.aead.Open(nil, videoSpoolNonce(r.noncePrefix, r.counter), sealed, videoSpoolAAD(r.counter, plainLength, final))
	if err != nil {
		return 0, ErrVideoSpoolCorrupt
	}
	r.counter++
	if final {
		var trailing [1]byte
		if n, err := r.source.Read(trailing[:]); n != 0 || !errors.Is(err, io.EOF) {
			return 0, ErrVideoSpoolCorrupt
		}
		r.done = true
		return 0, io.EOF
	}
	r.plain = plain
	r.plainOffset = 0
	n := copy(p, r.plain)
	r.plainOffset = n
	return n, nil
}

func (r *videoSpoolDecryptReader) Close() error {
	if r == nil || r.source == nil {
		return nil
	}
	return r.source.Close()
}

func videoSpoolNonce(prefix [8]byte, counter uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix[:])
	binary.BigEndian.PutUint32(nonce[8:], counter)
	return nonce
}

func videoSpoolAAD(counter, length uint32, final bool) []byte {
	aad := make([]byte, len(videoSpoolMagic)+4+4+1)
	copy(aad, videoSpoolMagic)
	binary.BigEndian.PutUint32(aad[len(videoSpoolMagic):], counter)
	binary.BigEndian.PutUint32(aad[len(videoSpoolMagic)+4:], length)
	if final {
		aad[len(aad)-1] = 1
	}
	return aad
}

func validateVideoSpoolRole(role VideoInputRole) error {
	if !IsValidVideoInputRole(role) {
		return ErrVideoInputUnsupported
	}
	return nil
}

func validateVideoSpoolFilename(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || filename != filepath.Base(filename) || strings.ContainsAny(filename, `/\\`) {
		return "", ErrVideoInvalidRequest
	}
	for _, r := range filename {
		if unicode.IsControl(r) {
			return "", ErrVideoInvalidRequest
		}
	}
	if len(filename) > 255 {
		return "", ErrVideoInvalidRequest
	}
	return filename, nil
}

func normalizeVideoContentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	if value == "image/jpg" {
		return "image/jpeg"
	}
	return value
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}
