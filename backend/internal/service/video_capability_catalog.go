package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
)

const (
	VideoCapabilityCatalogVersion = 1
	videoCapabilityCatalogMaxSize = 256 << 10
	videoCapabilityCatalogTTL     = 30 * time.Second
)

type VideoCapabilityCatalogDocument struct {
	Version   int                          `json:"version"`
	Providers map[string]VideoCapabilities `json:"providers"`
}

type VideoCapabilityCatalogView struct {
	VideoCapabilityCatalogDocument
	Source           string    `json:"source"`
	LoadedAt         time.Time `json:"loaded_at"`
	LastRefreshError string    `json:"last_refresh_error,omitempty"`
}

type videoCapabilityCatalogSnapshot struct {
	document VideoCapabilityCatalogDocument
	source   string
	loadedAt time.Time
}

type VideoCapabilityCatalog struct {
	settings       SettingRepository
	snapshot       atomic.Pointer[videoCapabilityCatalogSnapshot]
	lastAttempt    atomic.Int64
	lastError      atomic.Pointer[string]
	refreshMu      sync.Mutex
	refreshTimeout time.Duration
	ttl            time.Duration
	now            func() time.Time
}

func NewVideoCapabilityCatalog(settings SettingRepository) *VideoCapabilityCatalog {
	now := time.Now().UTC()
	catalog := &VideoCapabilityCatalog{
		settings: settings, refreshTimeout: 5 * time.Second, ttl: videoCapabilityCatalogTTL, now: time.Now,
	}
	catalog.snapshot.Store(&videoCapabilityCatalogSnapshot{
		document: DefaultVideoCapabilityCatalogDocument(), source: "builtin", loadedAt: now,
	})
	return catalog
}

func DefaultVideoCapabilityCatalogDocument() VideoCapabilityCatalogDocument {
	return VideoCapabilityCatalogDocument{
		Version: VideoCapabilityCatalogVersion,
		Providers: map[string]VideoCapabilities{
			VideoProviderOpenAI: DefaultOpenAIVideoCapabilities(),
		},
	}
}

func DecodeVideoCapabilityCatalog(raw []byte) (VideoCapabilityCatalogDocument, error) {
	if len(bytes.TrimSpace(raw)) == 0 || len(raw) > videoCapabilityCatalogMaxSize {
		return VideoCapabilityCatalogDocument{}, fmt.Errorf("%w: capability catalog is empty or too large", ErrVideoInvalidRequest)
	}
	if err := jsonstrict.RejectDuplicateKeys(raw); err != nil {
		return VideoCapabilityCatalogDocument{}, fmt.Errorf("%w: %v", ErrVideoInvalidRequest, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document VideoCapabilityCatalogDocument
	if err := decoder.Decode(&document); err != nil {
		return VideoCapabilityCatalogDocument{}, fmt.Errorf("%w: invalid capability catalog: %v", ErrVideoInvalidRequest, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return VideoCapabilityCatalogDocument{}, fmt.Errorf("%w: trailing capability catalog JSON", ErrVideoInvalidRequest)
	}
	if err := ValidateVideoCapabilityCatalog(document); err != nil {
		return VideoCapabilityCatalogDocument{}, err
	}
	return cloneVideoCapabilityCatalog(document), nil
}

func ValidateVideoCapabilityCatalog(document VideoCapabilityCatalogDocument) error {
	if document.Version != VideoCapabilityCatalogVersion {
		return fmt.Errorf("%w: capability catalog version must be %d", ErrVideoInvalidRequest, VideoCapabilityCatalogVersion)
	}
	if len(document.Providers) == 0 || len(document.Providers) > 32 {
		return fmt.Errorf("%w: capability catalog providers are required", ErrVideoInvalidRequest)
	}
	if _, ok := document.Providers[VideoProviderOpenAI]; !ok {
		return fmt.Errorf("%w: OpenAI video capabilities are required", ErrVideoInvalidRequest)
	}
	for rawProvider, capabilities := range document.Providers {
		provider := strings.ToLower(strings.TrimSpace(rawProvider))
		if !validVideoProviderName(rawProvider) || provider != rawProvider {
			return fmt.Errorf("%w: provider keys must be normalized", ErrVideoInvalidRequest)
		}
		if err := validateVideoCapabilities(provider, capabilities); err != nil {
			return err
		}
	}
	return nil
}

func validateVideoCapabilities(provider string, capabilities VideoCapabilities) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: provider %s: %s", ErrVideoInvalidRequest, provider, fmt.Sprintf(format, args...))
	}
	defaultModel := strings.ToLower(strings.TrimSpace(capabilities.DefaultModel))
	if defaultModel == "" || !capabilities.SupportedModels[defaultModel] {
		return fail("default_model must name a supported model")
	}
	for operation := range capabilities.Operations {
		switch operation {
		case VideoCapabilityCreate, VideoCapabilityInputReference, VideoCapabilityCharacters,
			VideoCapabilityUploadedVideoEdits, VideoCapabilityEdits, VideoCapabilityExtensions,
			VideoCapabilityCancel, VideoCapabilityWebhook, VideoCapabilityTaskSearch:
		default:
			return fail("unknown operation capability %q", operation)
		}
	}
	validatedModels := make(map[string]struct{}, len(capabilities.SupportedModels))
	for model, enabled := range capabilities.SupportedModels {
		if strings.TrimSpace(model) == "" || model != strings.ToLower(strings.TrimSpace(model)) || !enabled {
			return fail("supported_models must contain normalized enabled entries")
		}
		canonicalModel := canonicalOpenAIVideoModel(model)
		if _, checked := validatedModels[canonicalModel]; checked {
			continue
		}
		validatedModels[canonicalModel] = struct{}{}
		seconds := capabilities.SupportedSeconds[canonicalModel]
		if len(seconds) == 0 {
			return fail("model %q requires supported durations", model)
		}
		seenSeconds := make(map[int]struct{}, len(seconds))
		for _, value := range seconds {
			if value <= 0 || value > 120 {
				return fail("model %q has invalid duration", model)
			}
			if _, duplicate := seenSeconds[value]; duplicate {
				return fail("model %q has duplicate duration %d", model, value)
			}
			seenSeconds[value] = struct{}{}
		}
		sizes := capabilities.SupportedSizes[canonicalModel]
		if len(sizes) == 0 {
			return fail("model %q requires supported sizes", model)
		}
		seenSizes := make(map[string]struct{}, len(sizes))
		for _, size := range sizes {
			normalizedSize, width, height, ok := parseVideoDimensions(size)
			if !ok || width > 8192 || height > 8192 {
				return fail("model %q has invalid size %q", model, size)
			}
			if _, duplicate := seenSizes[normalizedSize]; duplicate {
				return fail("model %q has duplicate size %q", model, size)
			}
			seenSizes[normalizedSize] = struct{}{}
		}
		if value := capabilities.DefaultSeconds[canonicalModel]; value <= 0 || !slices.Contains(seconds, value) {
			return fail("model %q default duration is not supported", model)
		}
		if value := strings.ToLower(strings.TrimSpace(capabilities.DefaultSizes[canonicalModel])); value == "" || !slices.Contains(sizes, value) {
			return fail("model %q default size is not supported", model)
		}
	}
	for field, values := range map[string]map[string]int{
		"default_seconds": capabilities.DefaultSeconds,
	} {
		for model := range values {
			if err := validateVideoCapabilitySpecModel(model, validatedModels); err != nil {
				return fail("%s: %v", field, err)
			}
		}
	}
	for field, values := range map[string]map[string]string{
		"default_sizes": capabilities.DefaultSizes,
	} {
		for model := range values {
			if err := validateVideoCapabilitySpecModel(model, validatedModels); err != nil {
				return fail("%s: %v", field, err)
			}
		}
	}
	for model := range capabilities.SupportedSeconds {
		if err := validateVideoCapabilitySpecModel(model, validatedModels); err != nil {
			return fail("supported_seconds: %v", err)
		}
	}
	for model := range capabilities.SupportedSizes {
		if err := validateVideoCapabilitySpecModel(model, validatedModels); err != nil {
			return fail("supported_sizes: %v", err)
		}
	}
	for operation, roles := range capabilities.InputRolesByOperation {
		if !isKnownVideoInputOperation(operation) {
			return fail("input operation %q is not supported", operation)
		}
		if len(roles) > 0 && capabilities.MaxInputsByOperation[operation] <= 0 {
			return fail("input operation %q requires a positive maximum input count", operation)
		}
		for role, enabled := range roles {
			if !enabled || !IsValidVideoInputRole(role) {
				return fail("operation %q has invalid input role %q", operation, role)
			}
			if len(capabilities.InputMIMETypes[role]) == 0 || capabilities.MaxInputBytes[role] <= 0 {
				return fail("operation %q input role %q requires MIME types and a byte limit", operation, role)
			}
		}
	}
	for role, mimeTypes := range capabilities.InputMIMETypes {
		if !IsValidVideoInputRole(role) || capabilities.MaxInputBytes[role] <= 0 {
			return fail("input role %q requires a positive byte limit", role)
		}
		if role == VideoInputRoleReferenceImage && capabilities.MaxInputBytes[role] > MaxContentModerationImageBytes {
			return fail("input role %q exceeds the content moderation byte limit", role)
		}
		for value, enabled := range mimeTypes {
			mediaType, _, err := mime.ParseMediaType(value)
			if err != nil || mediaType != strings.ToLower(strings.TrimSpace(value)) || !enabled {
				return fail("input role %q has invalid MIME type %q", role, value)
			}
		}
	}
	for operation, maximum := range capabilities.MaxInputsByOperation {
		if !isKnownVideoInputOperation(operation) || maximum < 0 || maximum > 16 {
			return fail("operation %q has invalid maximum input count", operation)
		}
	}
	for variant, enabled := range capabilities.ContentVariants {
		if strings.TrimSpace(variant) == "" || variant != strings.ToLower(strings.TrimSpace(variant)) || !enabled {
			return fail("content variants must contain normalized enabled entries")
		}
	}
	return nil
}

func validateVideoCapabilitySpecModel(model string, supported map[string]struct{}) error {
	normalized := strings.ToLower(strings.TrimSpace(model))
	canonical := canonicalOpenAIVideoModel(normalized)
	if normalized == "" || model != normalized || canonical != normalized {
		return fmt.Errorf("model key %q must be canonical and normalized", model)
	}
	if _, ok := supported[canonical]; !ok {
		return fmt.Errorf("model key %q is not supported", model)
	}
	return nil
}

func isKnownVideoInputOperation(operation string) bool {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case VideoOperationGenerate, VideoOperationEdit, VideoOperationExtend, VideoOperationCharacterCreate:
		return operation == strings.ToLower(strings.TrimSpace(operation))
	default:
		return false
	}
}

func (c *VideoCapabilityCatalog) Capabilities(provider string) (VideoCapabilities, bool) {
	if c == nil {
		return VideoCapabilities{}, false
	}
	c.triggerRefresh()
	snapshot := c.snapshot.Load()
	if snapshot == nil {
		return VideoCapabilities{}, false
	}
	capabilities, ok := snapshot.document.Providers[strings.ToLower(strings.TrimSpace(provider))]
	return cloneVideoCapabilities(capabilities), ok
}

func (c *VideoCapabilityCatalog) Refresh(ctx context.Context) error {
	if c == nil || c.settings == nil {
		return nil
	}
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.lastAttempt.Store(c.now().UnixNano())
	raw, err := c.settings.GetValue(ctx, SettingKeyVideoCapabilityCatalog)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && strings.TrimSpace(raw) == "") {
		c.storeSnapshot(DefaultVideoCapabilityCatalogDocument(), "builtin")
		return nil
	}
	if err != nil {
		c.storeError(err)
		return err
	}
	document, err := DecodeVideoCapabilityCatalog([]byte(raw))
	if err != nil {
		c.storeError(err)
		return err
	}
	c.storeSnapshot(document, "settings")
	return nil
}

func (c *VideoCapabilityCatalog) Update(ctx context.Context, document VideoCapabilityCatalogDocument) (*VideoCapabilityCatalogView, error) {
	if c == nil || c.settings == nil {
		return nil, errors.New("video capability catalog settings are unavailable")
	}
	if err := ValidateVideoCapabilityCatalog(document); err != nil {
		return nil, err
	}
	document = cloneVideoCapabilityCatalog(document)
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	if len(raw) > videoCapabilityCatalogMaxSize {
		return nil, fmt.Errorf("%w: capability catalog is too large", ErrVideoInvalidRequest)
	}
	if err := c.settings.Set(ctx, SettingKeyVideoCapabilityCatalog, string(raw)); err != nil {
		return nil, err
	}
	c.storeSnapshot(document, "settings")
	return c.View(), nil
}

func (c *VideoCapabilityCatalog) View() *VideoCapabilityCatalogView {
	if c == nil || c.snapshot.Load() == nil {
		return nil
	}
	snapshot := c.snapshot.Load()
	view := &VideoCapabilityCatalogView{
		VideoCapabilityCatalogDocument: cloneVideoCapabilityCatalog(snapshot.document),
		Source:                         snapshot.source, LoadedAt: snapshot.loadedAt,
	}
	if lastError := c.lastError.Load(); lastError != nil {
		view.LastRefreshError = *lastError
	}
	return view
}

func (c *VideoCapabilityCatalog) triggerRefresh() {
	if c.settings == nil {
		return
	}
	now := c.now()
	last := c.lastAttempt.Load()
	if last != 0 && now.Sub(time.Unix(0, last)) < c.ttl {
		return
	}
	if !c.lastAttempt.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), c.refreshTimeout)
		defer cancel()
		_ = c.Refresh(ctx)
	}()
}

func (c *VideoCapabilityCatalog) storeSnapshot(document VideoCapabilityCatalogDocument, source string) {
	c.snapshot.Store(&videoCapabilityCatalogSnapshot{document: cloneVideoCapabilityCatalog(document), source: source, loadedAt: c.now().UTC()})
	c.lastError.Store(nil)
}

func (c *VideoCapabilityCatalog) storeError(err error) {
	message := err.Error()
	c.lastError.Store(&message)
}

func cloneVideoCapabilityCatalog(document VideoCapabilityCatalogDocument) VideoCapabilityCatalogDocument {
	clone := VideoCapabilityCatalogDocument{Version: document.Version, Providers: make(map[string]VideoCapabilities, len(document.Providers))}
	for provider, capabilities := range document.Providers {
		clone.Providers[provider] = cloneVideoCapabilities(capabilities)
	}
	return clone
}

func cloneVideoCapabilities(value VideoCapabilities) VideoCapabilities {
	clone := value
	clone.DefaultSeconds = cloneStringIntMap(value.DefaultSeconds)
	clone.DefaultSizes = cloneStringStringMap(value.DefaultSizes)
	clone.Operations = make(map[VideoCapability]bool, len(value.Operations))
	for key, enabled := range value.Operations {
		clone.Operations[key] = enabled
	}
	clone.InputRolesByOperation = make(map[string]map[VideoInputRole]bool, len(value.InputRolesByOperation))
	for operation, roles := range value.InputRolesByOperation {
		copied := make(map[VideoInputRole]bool, len(roles))
		for role, enabled := range roles {
			copied[role] = enabled
		}
		clone.InputRolesByOperation[operation] = copied
	}
	clone.InputMIMETypes = make(map[VideoInputRole]map[string]bool, len(value.InputMIMETypes))
	for role, mimeTypes := range value.InputMIMETypes {
		copied := make(map[string]bool, len(mimeTypes))
		for mimeType, enabled := range mimeTypes {
			copied[mimeType] = enabled
		}
		clone.InputMIMETypes[role] = copied
	}
	clone.MaxInputBytes = make(map[VideoInputRole]int64, len(value.MaxInputBytes))
	for role, maximum := range value.MaxInputBytes {
		clone.MaxInputBytes[role] = maximum
	}
	clone.MaxInputsByOperation = cloneStringIntMap(value.MaxInputsByOperation)
	clone.ContentVariants = cloneStringBoolMap(value.ContentVariants)
	clone.SupportedModels = cloneStringBoolMap(value.SupportedModels)
	clone.SupportedSeconds = make(map[string][]int, len(value.SupportedSeconds))
	for model, seconds := range value.SupportedSeconds {
		clone.SupportedSeconds[model] = append([]int(nil), seconds...)
	}
	clone.SupportedSizes = make(map[string][]string, len(value.SupportedSizes))
	for model, sizes := range value.SupportedSizes {
		clone.SupportedSizes[model] = append([]string(nil), sizes...)
	}
	return clone
}

func cloneStringIntMap(value map[string]int) map[string]int {
	clone := make(map[string]int, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func cloneStringStringMap(value map[string]string) map[string]string {
	clone := make(map[string]string, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func cloneStringBoolMap(value map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}
