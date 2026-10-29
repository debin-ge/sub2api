package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const grokSourceVideoMaxBytes = 100 << 20

// Bound decoding and private scratch copies independently of request volume.
var grokVideoProbeSlots = make(chan struct{}, 2)

// GrokVideoSourceVerifier freezes the source before measuring it. The provider
// receives those exact bytes, never a mutable URL probed earlier.
type GrokVideoSourceVerifier struct {
	content *ByteDanceVideoProvider
	probe   func(context.Context, *os.File) ([]byte, error)
}

func NewGrokVideoSourceVerifier() *GrokVideoSourceVerifier {
	return &GrokVideoSourceVerifier{content: NewByteDanceVideoProvider(nil, nil), probe: probeGrokSourceVideo}
}
func (v *GrokVideoSourceVerifier) StoreURL(ctx context.Context, session *VideoSpoolSession, raw string) (*GrokVerifiedVideoSource, error) {
	if v == nil || session == nil {
		return nil, ErrVideoSourceSpecUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var reader io.ReadCloser
	if strings.HasPrefix(raw, "data:") {
		prefix, payload, ok := strings.Cut(raw, ",")
		if !ok || prefix != "data:video/mp4;base64" || len(payload) > base64.StdEncoding.EncodedLen(grokSourceVideoMaxBytes) {
			return nil, ErrVideoInputUnsupported
		}
		reader = io.NopCloser(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
	} else {
		response, err := v.content.openExternalVideoContent(ctx, nil, ProviderContentRequest{UpstreamURL: raw, Method: "GET", Variant: "video", ResponseHeaderTimeout: 10 * time.Second})
		if err != nil || response == nil {
			return nil, ErrVideoSourceSpecUnavailable
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return nil, ErrVideoSourceSpecUnavailable
		}
		reader = response.Body
	}
	defer reader.Close()
	input, err := session.Store(ctx, VideoInputRoleSourceVideo, "source.mp4", "video/mp4", io.LimitReader(reader, grokSourceVideoMaxBytes+1))
	if err != nil {
		return nil, err
	}
	return v.Verify(ctx, input)
}
func (v *GrokVideoSourceVerifier) Verify(ctx context.Context, input VideoInput) (*GrokVerifiedVideoSource, error) {
	if v == nil || v.probe == nil || input.Open == nil || input.Role != VideoInputRoleSourceVideo || input.MIMEType != "video/mp4" || input.Size <= 0 || input.Size > grokSourceVideoMaxBytes || input.SHA256 == "" {
		return nil, ErrVideoSourceSpecUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case grokVideoProbeSlots <- struct{}{}:
		defer func() { <-grokVideoProbeSlots }()
	case <-ctx.Done():
		return nil, ErrVideoSourceSpecUnavailable
	}
	reader, err := input.Open(ctx)
	if err != nil {
		return nil, ErrVideoSourceSpecUnavailable
	}
	defer reader.Close()
	// ffprobe requires seeking for non-faststart MP4 files. The decrypted copy is
	// private (0600) and unlinked immediately. An inherited descriptor lets the
	// decoder seek without leaving plaintext behind if the gateway crashes.
	file, err := os.CreateTemp("", "sub2api-grok-source-*.mp4")
	if err != nil {
		return nil, ErrVideoSourceSpecUnavailable
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = os.Remove(file.Name()); err != nil {
		return nil, ErrVideoSourceSpecUnavailable
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, grokSourceVideoMaxBytes+1))
	if err != nil || size != input.Size || hex.EncodeToString(hash.Sum(nil)) != input.SHA256 {
		return nil, ErrVideoSourceSpecUnavailable
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrVideoSourceSpecUnavailable
	}
	raw, err := v.probe(ctx, file)
	if err != nil {
		return nil, ErrVideoSourceSpecUnavailable
	}
	var result struct {
		Frames  []grokSourceFrame `json:"frames"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Duration  string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Streams) != 1 || result.Streams[0].CodecType != "video" {
		return nil, ErrVideoSourceSpecUnavailable
	}
	stream := result.Streams[0]
	seconds, err := grokDecodedVideoSeconds(result.Frames)
	if err != nil || !finitePositive(seconds) || seconds > 15 || stream.Width <= 0 || stream.Height <= 0 || stream.Width > 8192 || stream.Height > 8192 {
		return nil, ErrVideoSourceSpecUnavailable
	}
	return &GrokVerifiedVideoSource{Input: input, Seconds: seconds, Width: stream.Width, Height: stream.Height, verified: true}, nil
}

type grokProbeOutput struct{ bytes.Buffer }

func (b *grokProbeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2<<20 {
		return 0, errors.New("media probe output limit")
	}
	return b.Buffer.Write(p)
}
func probeGrokSourceVideo(ctx context.Context, file *os.File) ([]byte, error) {
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-threads", "1", "-max_alloc", "67108864", "-protocol_whitelist", "file,pipe", "-f", "mov", "-enable_drefs", "0", "-use_absolute_path", "0", "-select_streams", "v:0", "-show_frames", "-show_entries", "stream=codec_type,width,height:frame=best_effort_timestamp_time,duration_time,pkt_duration_time", "-of", "json", "/dev/fd/3")
	command.ExtraFiles = []*os.File{file}
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	var out grokProbeOutput
	command.Stdout = &out
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type grokSourceFrame struct {
	Timestamp      string `json:"best_effort_timestamp_time"`
	Duration       string `json:"duration_time"`
	PacketDuration string `json:"pkt_duration_time"`
}

// Use decoded presentation timestamps, not user-editable mvhd/mdhd duration
// headers. Otherwise a shortened header can under-reserve an expensive edit.
func grokDecodedVideoSeconds(frames []grokSourceFrame) (float64, error) {
	if len(frames) == 0 || len(frames) > 10000 {
		return 0, ErrVideoSourceSpecUnavailable
	}
	first, previous, end := 0.0, 0.0, math.Inf(-1)
	for index, frame := range frames {
		ts, err := strconv.ParseFloat(frame.Timestamp, 64)
		if err != nil || math.IsNaN(ts) || math.IsInf(ts, 0) || math.Abs(ts) > 86400 || (index > 0 && ts <= previous) {
			return 0, ErrVideoSourceSpecUnavailable
		}
		if index == 0 {
			first = ts
		}
		previous = ts
		duration, _ := strconv.ParseFloat(firstNonEmptyString(frame.Duration, frame.PacketDuration), 64)
		if index == len(frames)-1 && !finitePositive(duration) {
			return 0, ErrVideoSourceSpecUnavailable
		}
		if finitePositive(duration) && ts+duration > end {
			end = ts + duration
		}
	}
	seconds := math.Round((end-first)*1e6) / 1e6
	if !finitePositive(seconds) || seconds > 15 {
		return 0, ErrVideoSourceSpecUnavailable
	}
	return seconds, nil
}
