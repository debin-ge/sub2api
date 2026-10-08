// video-vendor-profiles 从厂商机器可读 schema 生成聚合媒体平台的 Tier B 视频模型档案
// （internal/pkg/vendorprofile.Document），输出到 internal/service/video_vendor_profiles/<vendor>.json。
//
//	go run ./cmd/tools/video-vendor-profiles -vendor runware
//	WAVESPEED_API_KEY=... go run ./cmd/tools/video-vendor-profiles -vendor wavespeed
//
// 生成结果是数据不是代码：手写的 Tier A 档案始终优先，这里只补长尾模型。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/vendorprofile"
)

const (
	runwareIndexURL      = "https://runware.ai/docs/models/index.json"
	waveSpeedDefaultBase = "https://api.wavespeed.ai/api/v3"
	httpTimeout          = 60 * time.Second
	maxBodyBytes         = 8 << 20
)

func main() {
	vendor := flag.String("vendor", "", "runware | wavespeed")
	out := flag.String("out", "", "output path (default internal/service/video_vendor_profiles/<vendor>.json)")
	concurrency := flag.Int("concurrency", 4, "parallel schema fetches")
	waveSpeedBase := flag.String("wavespeed-base", waveSpeedDefaultBase, "WaveSpeed API base URL")
	flag.Parse()

	if *out == "" {
		*out = "internal/service/video_vendor_profiles/" + *vendor + ".json"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: httpTimeout}

	var document vendorprofile.Document
	var err error
	switch strings.ToLower(strings.TrimSpace(*vendor)) {
	case "runware":
		document, err = generateRunware(ctx, client, *concurrency)
	case "wavespeed":
		document, err = generateWaveSpeed(ctx, client, *waveSpeedBase, os.Getenv("WAVESPEED_API_KEY"))
	default:
		err = errors.New("-vendor must be runware or wavespeed")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	sort.Slice(document.Profiles, func(i, j int) bool { return document.Profiles[i].Model < document.Profiles[j].Model })
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d profiles to %s\n", len(document.Profiles), *out)
}

// ---------------------------------------------------------------------------
// Runware
// ---------------------------------------------------------------------------

type runwareIndexEntry struct {
	ID           string   `json:"id"`
	AIR          string   `json:"air"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
	Schema       string   `json:"schema"`
}

func generateRunware(ctx context.Context, client *http.Client, concurrency int) (vendorprofile.Document, error) {
	document := vendorprofile.Document{Vendor: "runware", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Source: runwareIndexURL}
	raw, err := fetch(ctx, client, runwareIndexURL, "")
	if err != nil {
		return document, err
	}
	var entries []runwareIndexEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return document, fmt.Errorf("decode runware index: %w", err)
	}
	candidates := make([]runwareIndexEntry, 0, len(entries))
	for _, entry := range entries {
		if !strings.EqualFold(entry.Status, "live") || entry.Schema == "" || entry.AIR == "" {
			continue
		}
		if !hasAny(entry.Capabilities, "text-to-video", "image-to-video") {
			continue
		}
		candidates = append(candidates, entry)
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for _, entry := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(entry runwareIndexEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			profile, skip, err := runwareProfileFor(ctx, client, entry)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip %s: %v\n", entry.AIR, err)
				return
			}
			if skip != "" {
				fmt.Fprintf(os.Stderr, "skip %s: %s\n", entry.AIR, skip)
				return
			}
			mu.Lock()
			document.Profiles = append(document.Profiles, profile)
			mu.Unlock()
		}(entry)
	}
	wg.Wait()
	return document, nil
}

func runwareProfileFor(ctx context.Context, client *http.Client, entry runwareIndexEntry) (vendorprofile.Entry, string, error) {
	raw, err := fetch(ctx, client, entry.Schema, "")
	if err != nil {
		return vendorprofile.Entry{}, "", err
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return vendorprofile.Entry{}, "", fmt.Errorf("decode schema: %w", err)
	}
	info := asMap(schema["info"])
	if status := asString(info["x-status"]); status != "" && !strings.EqualFold(status, "live") {
		return vendorprofile.Entry{}, "status " + status, nil
	}
	body := asMap(asMap(asMap(schema["components"])["schemas"])["RequestBody"])
	items := asMap(body["items"])
	if items == nil {
		items = body
	}
	props := asMap(items["properties"])
	if props == nil {
		return vendorprofile.Entry{}, "no request properties", nil
	}
	profile := vendorprofile.Entry{Model: strings.ToLower(entry.AIR), Name: entry.Name}

	// 时长
	if duration := asMap(props["duration"]); duration != nil {
		profile.Seconds, profile.MinSeconds, profile.MaxSeconds, profile.DefaultSeconds = secondsFromSchema(duration)
		if asString(duration["type"]) == "number" {
			profile.DurationEnc = "float"
		}
	} else {
		return vendorprofile.Entry{}, "no duration parameter", nil
	}

	// 尺寸：allOf 里的 oneOf 封闭尺寸对
	pairs := map[string]struct{}{}
	collectSizePairs(items["allOf"], pairs)
	for size := range pairs {
		profile.Sizes = append(profile.Sizes, size)
	}
	sort.Strings(profile.Sizes)
	if _, ok := props["width"]; ok {
		profile.SizeEnc = "width_height"
	} else {
		profile.SizeEnc = "none"
	}
	if contains(profile.Sizes, "1280x720") {
		profile.DefaultSize = "1280x720"
	} else if contains(profile.Sizes, "1920x1080") {
		profile.DefaultSize = "1920x1080"
	}
	if resolution := asMap(props["resolution"]); resolution != nil && len(asSlice(resolution["enum"])) > 0 {
		profile.ImageResolutionField = "resolution"
		profile.Resolutions, profile.ResolutionLabels = resolutionVocabulary(asSlice(resolution["enum"]))
	}

	// 输入：首帧 / 首尾帧
	inputs := asMap(asMap(props["inputs"])["properties"])
	if frames := asMap(inputs["frameImages"]); frames != nil {
		profile.AllowsImage = true
		profile.MaxImages = 1
		if maxItems, ok := asInt(frames["maxItems"]); ok && maxItems >= 2 {
			profile.MaxImages = 2
		}
	}
	hasTextMode := false
	for _, mode := range asSlice(info["x-modes"]) {
		modeMap := asMap(mode)
		if asString(modeMap["id"]) == "text" || len(asSlice(modeMap["inputs"])) == 0 {
			hasTextMode = true
			break
		}
	}
	if len(asSlice(info["x-modes"])) == 0 {
		hasTextMode = true
	}
	if !hasTextMode && !profile.AllowsImage {
		return vendorprofile.Entry{}, "neither text nor first-frame mode", nil
	}
	profile.RequiresImage = !hasTextMode

	// 音频
	if audio := asMap(asMap(asMap(props["settings"])["properties"])["audio"]); audio != nil {
		profile.AudioField = "settings.audio"
	} else {
		for providerKey, providerRaw := range asMap(asMap(props["providerSettings"])["properties"]) {
			providerProps := asMap(asMap(providerRaw)["properties"])
			for _, candidate := range []string{"sound", "generateAudio", "audio"} {
				if _, ok := providerProps[candidate]; ok {
					profile.AudioField = "providerSettings." + providerKey + "." + candidate
					break
				}
			}
			if profile.AudioField != "" {
				break
			}
		}
	}

	// 选项白名单
	profile.Options = map[string]string{}
	for _, key := range []string{"seed", "negativePrompt", "steps", "CFGScale"} {
		if _, ok := props[key]; ok {
			profile.Options[key] = key
		}
	}
	for providerKey, providerRaw := range asMap(asMap(props["providerSettings"])["properties"]) {
		providerProps := asMap(asMap(providerRaw)["properties"])
		for _, candidate := range []string{"cameraFixed", "promptExpansion"} {
			if _, ok := providerProps[candidate]; ok {
				profile.Options[candidate] = "providerSettings." + providerKey + "." + candidate
			}
		}
	}
	return profile, "", nil
}

func collectSizePairs(value any, pairs map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for _, option := range asSlice(typed["oneOf"]) {
			optionProps := asMap(asMap(option)["properties"])
			width, okW := asInt(asMap(optionProps["width"])["const"])
			height, okH := asInt(asMap(optionProps["height"])["const"])
			if okW && okH && width > 0 && height > 0 {
				pairs[strconv.Itoa(width)+"x"+strconv.Itoa(height)] = struct{}{}
			}
		}
		for _, child := range typed {
			collectSizePairs(child, pairs)
		}
	case []any:
		for _, child := range typed {
			collectSizePairs(child, pairs)
		}
	}
}

// ---------------------------------------------------------------------------
// WaveSpeed
// ---------------------------------------------------------------------------

type waveSpeedModel struct {
	ModelID   string `json:"model_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	APISchema struct {
		APISchemas []struct {
			Type          string         `json:"type"`
			RequestSchema map[string]any `json:"request_schema"`
		} `json:"api_schemas"`
	} `json:"api_schema"`
}

func generateWaveSpeed(ctx context.Context, client *http.Client, base, apiKey string) (vendorprofile.Document, error) {
	document := vendorprofile.Document{Vendor: "wavespeed", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Source: strings.TrimRight(base, "/") + "/models"}
	if strings.TrimSpace(apiKey) == "" {
		return document, errors.New("WAVESPEED_API_KEY is required")
	}
	raw, err := fetch(ctx, client, strings.TrimRight(base, "/")+"/models", "Bearer "+strings.TrimSpace(apiKey))
	if err != nil {
		return document, err
	}
	var envelope struct {
		Code    int              `json:"code"`
		Message string           `json:"message"`
		Data    []waveSpeedModel `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return document, fmt.Errorf("decode wavespeed models: %w", err)
	}
	if envelope.Code != 200 {
		return document, fmt.Errorf("wavespeed models code %d: %s", envelope.Code, envelope.Message)
	}
	for _, model := range envelope.Data {
		kind := strings.ToLower(strings.TrimSpace(model.Type))
		if kind != "text-to-video" && kind != "image-to-video" {
			continue
		}
		profile, skip := waveSpeedProfileFor(model, kind == "image-to-video")
		if skip != "" {
			fmt.Fprintf(os.Stderr, "skip %s: %s\n", model.ModelID, skip)
			continue
		}
		document.Profiles = append(document.Profiles, profile)
	}
	return document, nil
}

func waveSpeedProfileFor(model waveSpeedModel, imageToVideo bool) (vendorprofile.Entry, string) {
	var schema map[string]any
	for _, candidate := range model.APISchema.APISchemas {
		if candidate.RequestSchema != nil && (schema == nil || candidate.Type == "model_run") {
			schema = candidate.RequestSchema
		}
	}
	props := asMap(schema["properties"])
	if props == nil {
		return vendorprofile.Entry{}, "no request schema"
	}
	required := map[string]bool{}
	for _, name := range asSlice(schema["required"]) {
		required[asString(name)] = true
	}
	profile := vendorprofile.Entry{Model: strings.ToLower(strings.TrimSpace(model.ModelID)), Name: model.Name, Endpoint: strings.TrimSpace(model.ModelID)}
	if duration := asMap(props["duration"]); duration != nil {
		profile.Seconds, profile.MinSeconds, profile.MaxSeconds, profile.DefaultSeconds = secondsFromSchema(duration)
	} else {
		return vendorprofile.Entry{}, "no duration parameter"
	}
	for _, aspect := range asSlice(asMap(props["aspect_ratio"])["enum"]) {
		if text := asString(aspect); text != "" {
			profile.Aspects = append(profile.Aspects, text)
		}
	}
	if enum := asSlice(asMap(props["resolution"])["enum"]); len(enum) > 0 {
		profile.Resolutions, profile.ResolutionLabels = resolutionVocabulary(enum)
	}
	if _, ok := props["image"]; ok {
		profile.AllowsImage = true
		profile.RequiresImage = required["image"] || imageToVideo
		profile.MaxImages = 1
		if _, ok := props["last_image"]; ok {
			profile.MaxImages = 2
		}
	} else if imageToVideo {
		return vendorprofile.Entry{}, "image-to-video without image parameter"
	}
	hasAspect, hasResolution := len(profile.Aspects) > 0, len(profile.Resolutions) > 0
	switch {
	case profile.RequiresImage && hasResolution:
		profile.SizeEnc = "resolution_only"
	case profile.RequiresImage:
		profile.SizeEnc = "none"
	case hasAspect && hasResolution:
		profile.SizeEnc = "aspect_ratio_resolution"
	case hasAspect:
		profile.SizeEnc = "aspect_ratio"
		profile.Resolutions = []string{"1080p"}
	case hasResolution:
		profile.SizeEnc = "resolution_only"
	default:
		profile.SizeEnc = "none"
	}
	if profile.SizeEnc == "resolution_only" && len(profile.Aspects) == 0 {
		profile.Aspects = []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9"}
	}
	for _, candidate := range []string{"generate_audio", "sound", "enable_audio"} {
		if _, ok := props[candidate]; ok {
			profile.AudioField = candidate
			break
		}
	}
	profile.Options = map[string]string{}
	for _, key := range []string{"seed", "negative_prompt", "camera_fixed", "cfg_scale", "shot_type", "enable_web_search", "guidance_scale", "num_inference_steps"} {
		if _, ok := props[key]; ok {
			profile.Options[key] = key
		}
	}
	return profile, ""
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func secondsFromSchema(duration map[string]any) (seconds []int, minSeconds, maxSeconds, defaultSeconds int) {
	for _, value := range asSlice(duration["enum"]) {
		if number, ok := asInt(value); ok && number > 0 {
			seconds = append(seconds, number)
		}
	}
	sort.Ints(seconds)
	if len(seconds) == 0 {
		if minimum, ok := asFloat(duration["minimum"]); ok {
			minSeconds = int(math.Ceil(minimum))
		}
		if maximum, ok := asFloat(duration["maximum"]); ok {
			maxSeconds = int(math.Floor(maximum))
		}
		if minSeconds <= 0 {
			minSeconds = 1
		}
		if maxSeconds < minSeconds {
			maxSeconds = minSeconds
		}
	}
	if value, ok := asInt(duration["default"]); ok && value > 0 {
		defaultSeconds = value
	}
	return seconds, minSeconds, maxSeconds, defaultSeconds
}

func fetch(ctx context.Context, client *http.Client, url, authorization string) ([]byte, error) {
	var lastErr error
	for attempt := range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second * time.Duration(attempt+1))
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
			time.Sleep(time.Second * time.Duration(attempt+1))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
		}
		return body, nil
	}
	return nil, lastErr
}

// resolutionVocabulary 把厂商的分辨率枚举规整成通用档位词表，并记录需要改写的名字
// （768p 对应通用的 720p 档，2160p 对应 4k）。通用档位之外的名字（540p 等）照原样保留，
// 只是不会被按尺寸推导出来。
func resolutionVocabulary(enum []any) ([]string, map[string]string) {
	resolutions := make([]string, 0, len(enum))
	labels := map[string]string{}
	for _, raw := range enum {
		original := strings.ToLower(asString(raw))
		text := original
		if text == "" {
			continue
		}
		switch text {
		case "768p":
			labels["720p"] = original
		case "2160p":
			text = "4k"
			labels["4k"] = original
		}
		resolutions = append(resolutions, text)
	}
	if len(labels) == 0 {
		labels = nil
	}
	return resolutions, labels
}

func hasAny(values []string, wanted ...string) bool {
	for _, value := range values {
		for _, candidate := range wanted {
			if strings.EqualFold(strings.TrimSpace(value), candidate) {
				return true
			}
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	return slices.Contains(values, wanted)
}

func asMap(value any) map[string]any {
	typed, _ := value.(map[string]any)
	return typed
}

func asSlice(value any) []any {
	typed, _ := value.([]any)
	return typed
}

func asString(value any) string {
	typed, _ := value.(string)
	return strings.TrimSpace(typed)
}

func asFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(typed, "s")), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func asInt(value any) (int, bool) {
	number, ok := asFloat(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) {
		return 0, false
	}
	return int(number), true
}
