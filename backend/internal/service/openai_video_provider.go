package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/util/jsonstrict"
	xproxy "golang.org/x/net/proxy"
)

const (
	openAIVideosEndpoint                = "/v1/videos"
	openAIVideoCharactersEndpoint       = "/v1/videos/characters"
	openAIVideoEditsEndpoint            = "/v1/videos/edits"
	openAIVideoExtensionsEndpoint       = "/v1/videos/extensions"
	openAIVideoMaxJSONResponse          = 1 << 20
	openAIVideoMaxErrorResponse         = 64 << 10
	openAIVideoWebhookTolerance         = 5 * time.Minute
	openAIVideoWebhookSecretKey         = "webhook_secret"
	openAIVideoWebhookPreviousSecretKey = "webhook_secret_previous"
	openAIVideoContentDialTimeout       = 10 * time.Second
	openAIVideoContentTLSSTimeout       = 10 * time.Second
	openAIVideoContentHeadTimeout       = 30 * time.Second
	openAIVideoCapabilityProbeTimeout   = 15 * time.Second
)

var videoProviderURLPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

type videoContentRedirectExecutor func(context.Context, *http.Request, *Account, []netip.Addr) (*http.Response, error)

type OpenAIVideoProvider struct {
	httpUpstream HTTPUpstream
	tlsProfiles  *TLSFingerprintProfileService
	capabilities VideoCapabilities
	catalog      *VideoCapabilityCatalog
	now          func() time.Time
	resolver     videoCallbackIPResolver
	redirect     videoContentRedirectExecutor
}

func NewOpenAIVideoProvider(httpUpstream HTTPUpstream, tlsProfiles *TLSFingerprintProfileService) *OpenAIVideoProvider {
	return &OpenAIVideoProvider{
		httpUpstream: httpUpstream, tlsProfiles: tlsProfiles,
		capabilities: DefaultOpenAIVideoCapabilities(), now: time.Now, resolver: net.DefaultResolver,
		redirect: executePinnedVideoContentRedirect,
	}
}

func (p *OpenAIVideoProvider) Name() string { return VideoProviderOpenAI }

func (p *OpenAIVideoProvider) Capabilities() VideoCapabilities {
	if p == nil {
		return VideoCapabilities{}
	}
	if p.catalog != nil {
		if capabilities, ok := p.catalog.Capabilities(p.Name()); ok {
			return capabilities
		}
	}
	return cloneVideoCapabilities(p.capabilities)
}

func (p *OpenAIVideoProvider) SupportsAccount(account *Account) bool {
	return account != nil && account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos)
}

func (p *OpenAIVideoProvider) ProbeCapability(ctx context.Context, account *Account, capability VideoCapability) (*VideoCapabilityProbeResult, error) {
	if p == nil || p.httpUpstream == nil {
		return nil, errors.New("OpenAI video capability probe is not configured")
	}
	if capability != VideoCapabilityCreate || account == nil || !account.IsOpenAIApiKey() || isAzureOpenAIAPIKeyAccount(account) {
		return nil, ErrVideoCapabilityUnsupported
	}
	checkedAt := time.Now().UTC()
	if p.now != nil {
		checkedAt = p.now().UTC()
	}
	result := &VideoCapabilityProbeResult{
		Provider: VideoProviderOpenAI, Capability: string(OpenAIEndpointCapabilityVideos),
		Status: VideoCapabilityProbeUnknown, CheckedAt: checkedAt,
	}
	baseURL := strings.TrimSpace(account.GetOpenAIBaseURL())
	parsed, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "api.openai.com") {
		result.ErrorSummary = "custom_base_url_requires_override"
		return result, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, openAIVideoCapabilityProbeTimeout)
	defer cancel()
	target := buildOpenAIEndpointURL(baseURL, openAIVideosEndpoint)
	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(probeCtx, HTTPUpstreamProfileOpenAI)),
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		return nil, err
	}
	query := req.URL.Query()
	query.Set("limit", "1")
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Authorization", "Bearer "+account.GetOpenAIApiKey())
	req.Header.Set("Accept", "application/json")
	if userAgent := account.GetOpenAIUserAgent(); userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	account.ApplyHeaderOverrides(req.Header)
	response, err := p.execute(req, account)
	if err != nil {
		result.ErrorSummary = "transport_error"
		return result, nil
	}
	defer func() { _ = response.Body.Close() }()
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 2*openAIVideoMaxErrorResponse))
	result.HTTPStatus = response.StatusCode
	if readErr != nil {
		result.ErrorSummary = "response_read_error"
		return result, nil
	}
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		result.Status = VideoCapabilityProbeSupported
	case response.StatusCode == http.StatusUnauthorized:
		result.Status = VideoCapabilityProbeUnsupported
		result.ErrorSummary = "authentication_failed"
	case response.StatusCode == http.StatusForbidden:
		result.Status = VideoCapabilityProbeUnsupported
		result.ErrorSummary = "videos_access_denied"
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed:
		result.Status = VideoCapabilityProbeUnsupported
		result.ErrorSummary = "videos_endpoint_unavailable"
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
		result.ErrorSummary = "transient_upstream_error"
	default:
		result.ErrorSummary = "probe_rejected"
	}
	return result, nil
}

func (p *OpenAIVideoProvider) ValidateSubmission(account *Account, request VideoCreateRequest, inputs []VideoInput) error {
	if request.Operation != VideoOperationCharacterCreate {
		return p.validateAccountAndRequest(account, request, inputs)
	}
	name, _ := request.ProviderOptions["name"].(string)
	for key := range request.ProviderOptions {
		if key != "name" {
			return rejectedVideoProviderError("validation", "unsupported_option", "OpenAI character request contains an unsupported option", http.StatusBadRequest)
		}
	}
	if len(inputs) != 1 {
		return rejectedVideoProviderError("validation", "unsupported_character", "OpenAI character creation requires one video", http.StatusBadRequest)
	}
	return p.validateCharacterInput(account, strings.TrimSpace(name), inputs[0])
}

func (p *OpenAIVideoProvider) Create(ctx context.Context, account *Account, request VideoCreateRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	if err := p.validateAccountAndRequest(account, request, inputs); err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		body, err := openAIVideoCreateJSON(account, request)
		if err != nil {
			return nil, rejectedVideoProviderError("validation", "invalid_reference_media", "compatible video reference fields are invalid", http.StatusBadRequest)
		}
		return p.submitJSON(ctx, account, openAIVideosEndpoint, body, request.ClientToken)
	}
	if len(inputs) != 1 || inputs[0].Role != VideoInputRoleReferenceImage {
		return nil, rejectedVideoProviderError("validation", "unsupported_input", "OpenAI video create accepts one image input_reference", http.StatusBadRequest)
	}
	fields := openAIVideoMultipartFields(request)
	return p.submitMultipart(ctx, account, openAIVideosEndpoint, fields, "input_reference", inputs[0], request.ClientToken)
}

func (p *OpenAIVideoProvider) Edit(ctx context.Context, account *Account, request VideoEditRequest, inputs []VideoInput) (*ProviderVideoTask, error) {
	request.Operation = VideoOperationEdit
	if err := p.validateAccountAndRequest(account, request.VideoCreateRequest, inputs); err != nil {
		return nil, err
	}
	if len(inputs) == 1 {
		if inputs[0].Role != VideoInputRoleSourceVideo || request.SourceTask != nil {
			return nil, rejectedVideoProviderError("validation", "unsupported_input", "video edit upload must use source_video", http.StatusBadRequest)
		}
		if _, _, err := resolveVideoExecutionSpec(request.VideoCreateRequest, account.ID, p.Name(), nil); err != nil {
			return nil, rejectedVideoProviderError("validation", "unsupported_edit_specification", "uploaded edit output parameters are not supported", http.StatusBadRequest)
		}
		fields := map[string]string{"model": request.Model, "prompt": request.Prompt}
		return p.submitMultipart(ctx, account, openAIVideoEditsEndpoint, fields, "video", inputs[0], request.ClientToken)
	}
	if len(inputs) != 0 || request.SourceTask == nil || strings.TrimSpace(request.SourceTask.ProviderTaskID) == "" {
		return nil, rejectedVideoProviderError("validation", "source_video_required", "video edit source is required", http.StatusBadRequest)
	}
	body := map[string]any{
		"prompt": request.Prompt,
		"video":  map[string]string{"id": strings.TrimSpace(request.SourceTask.ProviderTaskID)},
	}
	return p.submitJSON(ctx, account, openAIVideoEditsEndpoint, body, request.ClientToken)
}

func (p *OpenAIVideoProvider) Extend(ctx context.Context, account *Account, request VideoExtendRequest) (*ProviderVideoTask, error) {
	request.Operation = VideoOperationExtend
	if err := p.validateAccountAndRequest(account, request.VideoCreateRequest, nil); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.SourceTask.ProviderTaskID) == "" || len(request.Characters) > 0 || request.InputReference != nil ||
		!validVideoExtensionSeconds(request.Seconds) {
		return nil, rejectedVideoProviderError("validation", "invalid_extension", "video extension requires only a source video and prompt", http.StatusBadRequest)
	}
	body := map[string]any{
		"video":  map[string]string{"id": strings.TrimSpace(request.SourceTask.ProviderTaskID)},
		"prompt": request.Prompt,
	}
	if request.Seconds > 0 {
		body["seconds"] = openAIVideoJSONSeconds(account, request.VideoCreateRequest)
	}
	return p.submitJSON(ctx, account, openAIVideoExtensionsEndpoint, body, request.ClientToken)
}

func (p *OpenAIVideoProvider) CreateCharacter(ctx context.Context, account *Account, request VideoCharacterRequest, input VideoInput) (*ProviderVideoResource, error) {
	for key := range request.ProviderOptions {
		if key != "name" {
			return nil, rejectedVideoProviderError("validation", "unsupported_option", "OpenAI character request contains an unsupported option", http.StatusBadRequest)
		}
	}
	if err := p.validateCharacterInput(account, request.Name, input); err != nil {
		return nil, err
	}
	fields := map[string]string{"name": strings.TrimSpace(request.Name)}
	response, err := p.doMultipart(ctx, account, openAIVideoCharactersEndpoint, fields, "video", input, request.ClientToken)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, true)
	}
	var resource openAIVideoCharacterResponse
	if err := decodeBoundedJSON(response.Body, openAIVideoMaxJSONResponse, &resource); err != nil {
		return nil, unknownVideoProviderError("upstream", "invalid_response", "OpenAI character response could not be decoded", err)
	}
	if !validVideoProviderIdentifier(resource.ID) {
		return nil, unknownVideoProviderError("upstream", "missing_id", "OpenAI character response did not include an id", nil)
	}
	return &ProviderVideoResource{
		ProviderResourceID: resource.ID,
		Status:             "ready",
		Metadata:           map[string]any{"name": resource.Name},
		ExpiresAt:          unixTimePointer(resource.ExpiresAt),
	}, nil
}

func (p *OpenAIVideoProvider) validateCharacterInput(account *Account, name string, input VideoInput) error {
	capabilities := p.Capabilities()
	if !p.SupportsAccount(account) || !capabilities.Supports(VideoCapabilityCharacters) ||
		input.Role != VideoInputRoleCharacterClip || !capabilities.SupportsInput(input.Role, input.MIMEType, input.Size) {
		return rejectedVideoProviderError("validation", "unsupported_character", "video character input is not supported", http.StatusBadRequest)
	}
	if strings.TrimSpace(name) == "" {
		return rejectedVideoProviderError("validation", "character_name_required", "OpenAI character name is required", http.StatusBadRequest)
	}
	return nil
}

func (p *OpenAIVideoProvider) GetCharacter(ctx context.Context, account *Account, ref ProviderResourceRef) (*ProviderVideoResource, error) {
	response, err := p.do(ctx, account, http.MethodGet, openAIVideoCharactersEndpoint+"/"+url.PathEscape(ref.ProviderResourceID), nil, "", false)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, false)
	}
	var resource openAIVideoCharacterResponse
	if err := decodeBoundedJSON(response.Body, openAIVideoMaxJSONResponse, &resource); err != nil {
		return nil, controlVideoProviderError(err)
	}
	if !validVideoProviderIdentifier(resource.ID) {
		return nil, controlVideoProviderError(errors.New("OpenAI character response included an invalid id"))
	}
	return &ProviderVideoResource{ProviderResourceID: resource.ID, Status: "ready", Metadata: map[string]any{"name": resource.Name}, ExpiresAt: unixTimePointer(resource.ExpiresAt)}, nil
}

func (p *OpenAIVideoProvider) DeleteCharacter(ctx context.Context, account *Account, ref ProviderResourceRef) error {
	return p.delete(ctx, account, openAIVideoCharactersEndpoint+"/"+url.PathEscape(ref.ProviderResourceID))
}

func (p *OpenAIVideoProvider) Get(ctx context.Context, account *Account, ref ProviderTaskRef) (*ProviderVideoTask, error) {
	response, err := p.do(ctx, account, http.MethodGet, openAIVideosEndpoint+"/"+url.PathEscape(ref.ProviderTaskID), nil, "", false)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, false)
	}
	task, err := decodeOpenAIVideoTask(response.Body, false)
	if err != nil {
		return nil, err
	}
	if task.ProviderTaskID != strings.TrimSpace(ref.ProviderTaskID) {
		return nil, controlVideoProviderError(errors.New("OpenAI video response identity does not match the requested task"))
	}
	return task, nil
}

func (p *OpenAIVideoProvider) Delete(ctx context.Context, account *Account, ref ProviderTaskRef) error {
	return p.delete(ctx, account, openAIVideosEndpoint+"/"+url.PathEscape(ref.ProviderTaskID))
}

func (p *OpenAIVideoProvider) delete(ctx context.Context, account *Account, endpoint string) error {
	response, err := p.do(ctx, account, http.MethodDelete, endpoint, nil, "", false)
	if err != nil {
		return controlVideoProviderError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return p.responseError(response, account, false)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, openAIVideoMaxJSONResponse))
	return nil
}

func (p *OpenAIVideoProvider) OpenContent(ctx context.Context, account *Account, request ProviderContentRequest) (*ProviderContent, error) {
	variant := strings.ToLower(strings.TrimSpace(request.Variant))
	if variant == "" {
		variant = "video"
	}
	if !p.Capabilities().SupportsVariant(variant) {
		return nil, rejectedVideoProviderError("validation", "unsupported_variant", "video content variant is not supported", http.StatusBadRequest)
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, rejectedVideoProviderError("validation", "unsupported_method", "video content supports GET and HEAD", http.StatusMethodNotAllowed)
	}
	execute := func(requestMethod string) (*http.Response, error) {
		var req *http.Request
		var initialAddresses []netip.Addr
		upstreamURL := strings.TrimSpace(request.UpstreamURL)
		if variant != "video" {
			upstreamURL = ""
		}
		if upstreamURL == "" {
			endpoint := openAIVideosEndpoint + "/" + url.PathEscape(request.TaskRef.ProviderTaskID) + "/content"
			var err error
			req, err = p.newRequest(ctx, account, requestMethod, endpoint, nil, "")
			if err != nil {
				return nil, err
			}
			query := req.URL.Query()
			query.Set("variant", variant)
			req.URL.RawQuery = query.Encode()
		} else {
			normalized, err := normalizeProviderVideoURL(upstreamURL)
			if err != nil {
				return nil, err
			}
			target, _ := url.Parse(normalized)
			requestCtx := ctx
			if videoContentURLMatchesAccount(target, account) {
				requestCtx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
			} else {
				if !strings.EqualFold(target.Scheme, "https") {
					return nil, errors.New("external video_url must use HTTPS")
				}
				resolver := p.resolver
				if resolver == nil {
					resolver = net.DefaultResolver
				}
				_, initialAddresses, err = validateVideoContentRedirect(ctx, target, normalized, resolver)
				if err != nil {
					return nil, err
				}
			}
			req, err = http.NewRequestWithContext(requestCtx, requestMethod, normalized, nil)
			if err != nil {
				return nil, err
			}
			if len(initialAddresses) == 0 {
				req.Header.Set("Authorization", "Bearer "+account.GetOpenAIApiKey())
				if userAgent := account.GetOpenAIUserAgent(); userAgent != "" {
					req.Header.Set("User-Agent", userAgent)
				}
				account.ApplyHeaderOverrides(req.Header)
			} else {
				req.Header.Set("User-Agent", "sub2api-video-content/1")
			}
		}
		if value := strings.TrimSpace(request.Range); value != "" {
			req.Header.Set("Range", value)
		}
		if value := strings.TrimSpace(request.IfRange); value != "" {
			req.Header.Set("If-Range", value)
		}
		req.Header.Set("Accept", "*/*")
		return p.executeContentRedirectsFrom(ctx, req, account, request.ResponseHeaderTimeout, initialAddresses)
	}
	response, err := execute(method)
	if err != nil {
		return nil, controlVideoProviderError(err)
	}
	if method == http.MethodHead && (response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented) {
		_ = response.Body.Close()
		response, err = execute(http.MethodGet)
		if err != nil {
			return nil, controlVideoProviderError(err)
		}
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent &&
		response.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		defer func() { _ = response.Body.Close() }()
		return nil, p.responseError(response, account, false)
	}
	return &ProviderContent{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: response.Body}, nil
}

const openAIVideoContentMaxRedirects = 3

func (p *OpenAIVideoProvider) executeContentRedirects(ctx context.Context, request *http.Request, account *Account, responseHeaderTimeout time.Duration) (*http.Response, error) {
	return p.executeContentRedirectsFrom(ctx, request, account, responseHeaderTimeout, nil)
}

func (p *OpenAIVideoProvider) executeContentRedirectsFrom(ctx context.Context, request *http.Request, account *Account, responseHeaderTimeout time.Duration, initialAddresses []netip.Addr) (*http.Response, error) {
	resolver := p.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	current := request
	resolvedAddresses := append([]netip.Addr(nil), initialAddresses...)
	for redirects := 0; ; redirects++ {
		response, err := executeVideoContentWithHeaderTimeout(ctx, responseHeaderTimeout, func(callCtx context.Context) (*http.Response, error) {
			if len(resolvedAddresses) == 0 {
				return p.execute(current.WithContext(callCtx), account)
			}
			if p.redirect == nil {
				return nil, errors.New("video content redirect transport is not configured")
			}
			return p.redirect(callCtx, current, account, resolvedAddresses)
		})
		if err != nil {
			return nil, err
		}
		if !isVideoContentRedirect(response.StatusCode) {
			return response, nil
		}
		if redirects >= openAIVideoContentMaxRedirects {
			_ = response.Body.Close()
			return nil, errors.New("video content redirect limit exceeded")
		}
		location := strings.TrimSpace(response.Header.Get("Location"))
		_ = response.Body.Close()
		target, addresses, err := validateVideoContentRedirect(ctx, current.URL, location, resolver)
		if err != nil {
			return nil, err
		}
		next, err := http.NewRequestWithContext(current.Context(), current.Method, target.String(), nil)
		if err != nil {
			return nil, err
		}
		next.Header.Set("Accept", "*/*")
		if value := strings.TrimSpace(current.Header.Get("Range")); value != "" {
			next.Header.Set("Range", value)
		}
		if value := strings.TrimSpace(current.Header.Get("If-Range")); value != "" {
			next.Header.Set("If-Range", value)
		}
		next.Header.Set("User-Agent", "sub2api-video-content/1")
		current = next
		resolvedAddresses = addresses
	}
}

func videoContentURLMatchesAccount(target *url.URL, account *Account) bool {
	if target == nil || account == nil {
		return false
	}
	base, err := url.Parse(strings.TrimSpace(account.GetOpenAIBaseURL()))
	if err != nil || base.Hostname() == "" {
		return false
	}
	return strings.EqualFold(target.Scheme, base.Scheme) &&
		strings.EqualFold(strings.TrimSuffix(target.Hostname(), "."), strings.TrimSuffix(base.Hostname(), ".")) &&
		videoURLPort(target) == videoURLPort(base)
}

func videoURLPort(value *url.URL) string {
	if value == nil {
		return ""
	}
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	return ""
}

type videoContentHTTPResult struct {
	response *http.Response
	err      error
}

func executeVideoContentWithHeaderTimeout(ctx context.Context, timeout time.Duration, execute func(context.Context) (*http.Response, error)) (*http.Response, error) {
	if timeout <= 0 {
		timeout = openAIVideoContentHeadTimeout
	}
	callCtx, cancel := context.WithCancel(ctx)
	result := make(chan videoContentHTTPResult)
	go func() {
		response, err := execute(callCtx)
		select {
		case result <- videoContentHTTPResult{response: response, err: err}:
		case <-callCtx.Done():
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	case <-timer.C:
		cancel()
		return nil, context.DeadlineExceeded
	case completed := <-result:
		if completed.err != nil {
			cancel()
			return nil, completed.err
		}
		if completed.response == nil {
			cancel()
			return nil, errors.New("video content request returned no response")
		}
		if completed.response.Body == nil {
			completed.response.Body = http.NoBody
		}
		completed.response.Body = &videoContentContextBody{ReadCloser: completed.response.Body, cancel: cancel}
		return completed.response, nil
	}
}

type videoContentContextBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (b *videoContentContextBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cancel)
	return err
}

func isVideoContentRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func validateVideoContentRedirect(ctx context.Context, base *url.URL, location string, resolver videoCallbackIPResolver) (*url.URL, []netip.Addr, error) {
	if base == nil || location == "" || len(location) > 8192 {
		return nil, nil, errors.New("invalid video content redirect")
	}
	target, err := base.Parse(location)
	if err != nil || !strings.EqualFold(target.Scheme, "https") || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
		return nil, nil, errors.New("unsafe video content redirect")
	}
	if port := target.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, nil, errors.New("unsafe video content redirect port")
		}
	}
	addresses, err := resolvePublicVideoCallbackIPs(ctx, resolver, target.Hostname())
	if err != nil {
		return nil, nil, errors.New("unsafe video content redirect address")
	}
	return target, addresses, nil
}

type pinnedVideoContentDialer struct {
	targetHostname string
	addresses      []netip.Addr
	proxyURL       *url.URL
	dialContext    func(context.Context, string, string) (net.Conn, error)
}

func executePinnedVideoContentRedirect(ctx context.Context, request *http.Request, account *Account, addresses []netip.Addr) (*http.Response, error) {
	if request == nil || request.URL == nil || !strings.EqualFold(request.URL.Scheme, "https") {
		return nil, errors.New("invalid pinned video content request")
	}
	public := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !isPublicVideoCallbackIP(address) {
			return nil, errors.New("video content redirect resolved to a blocked address")
		}
		public = append(public, address)
	}
	if len(public) == 0 {
		return nil, errors.New("video content redirect has no pinned address")
	}
	var parsedProxy *url.URL
	if account != nil && account.Proxy != nil {
		_, value, err := proxyurl.Parse(account.Proxy.URL())
		if err != nil {
			return nil, err
		}
		parsedProxy = value
	}
	dialer := &pinnedVideoContentDialer{
		targetHostname: request.URL.Hostname(), addresses: public, proxyURL: parsedProxy,
		dialContext: (&net.Dialer{Timeout: openAIVideoContentDialTimeout, KeepAlive: 30 * time.Second}).DialContext,
	}
	transport := &http.Transport{
		Proxy: nil, DialTLSContext: dialer.DialTLSContext,
		ForceAttemptHTTP2: false, DisableKeepAlives: true,
		TLSHandshakeTimeout:   openAIVideoContentTLSSTimeout,
		ResponseHeaderTimeout: openAIVideoContentHeadTimeout,
	}
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	response.Body = &videoContentTransportBody{ReadCloser: response.Body, transport: transport}
	return response, nil
}

func (d *pinnedVideoContentDialer) DialTLSContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d == nil || d.dialContext == nil || strings.TrimSpace(d.targetHostname) == "" {
		return nil, errors.New("pinned video content dialer is not configured")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(d.targetHostname, ".")) {
		return nil, errors.New("pinned video content dial target changed")
	}
	var failures []error
	for _, resolved := range d.addresses {
		endpoint := net.JoinHostPort(resolved.String(), port)
		connection, connectErr := d.connect(ctx, network, endpoint)
		if connectErr != nil {
			failures = append(failures, connectErr)
			continue
		}
		tlsConnection, handshakeErr := videoContentTLSHandshake(ctx, connection, d.targetHostname)
		if handshakeErr == nil {
			return tlsConnection, nil
		}
		failures = append(failures, handshakeErr)
	}
	return nil, errors.Join(failures...)
}

func (d *pinnedVideoContentDialer) connect(ctx context.Context, network, endpoint string) (net.Conn, error) {
	if d.proxyURL == nil {
		return d.dialContext(ctx, network, endpoint)
	}
	switch strings.ToLower(d.proxyURL.Scheme) {
	case "http", "https":
		return d.connectHTTPProxy(ctx, network, endpoint)
	case "socks5", "socks5h":
		return d.connectSOCKS5Proxy(ctx, network, endpoint)
	default:
		return nil, errors.New("unsupported proxy scheme for video content redirect")
	}
}

func (d *pinnedVideoContentDialer) connectHTTPProxy(ctx context.Context, network, endpoint string) (net.Conn, error) {
	proxyAddress := d.proxyURL.Host
	if d.proxyURL.Port() == "" {
		port := "80"
		if strings.EqualFold(d.proxyURL.Scheme, "https") {
			port = "443"
		}
		proxyAddress = net.JoinHostPort(d.proxyURL.Hostname(), port)
	}
	connection, err := d.dialContext(ctx, network, proxyAddress)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(d.proxyURL.Scheme, "https") {
		connection, err = videoContentTLSHandshake(ctx, connection, d.proxyURL.Hostname())
		if err != nil {
			return nil, err
		}
	}
	connectRequest := &http.Request{
		Method: http.MethodConnect, URL: &url.URL{Opaque: endpoint}, Host: endpoint, Header: make(http.Header),
	}
	if d.proxyURL.User != nil {
		password, _ := d.proxyURL.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(d.proxyURL.User.Username() + ":" + password))
		connectRequest.Header.Set("Proxy-Authorization", "Basic "+token)
	}
	if err := connectRequest.Write(connection); err != nil {
		_ = connection.Close()
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), connectRequest)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = connection.Close()
		return nil, fmt.Errorf("video content proxy CONNECT returned %s", response.Status)
	}
	return connection, nil
}

func (d *pinnedVideoContentDialer) connectSOCKS5Proxy(ctx context.Context, network, endpoint string) (net.Conn, error) {
	proxyAddress := d.proxyURL.Host
	if d.proxyURL.Port() == "" {
		proxyAddress = net.JoinHostPort(d.proxyURL.Hostname(), "1080")
	}
	var auth *xproxy.Auth
	if d.proxyURL.User != nil {
		password, _ := d.proxyURL.User.Password()
		auth = &xproxy.Auth{User: d.proxyURL.User.Username(), Password: password}
	}
	forward := &net.Dialer{Timeout: openAIVideoContentDialTimeout, KeepAlive: 30 * time.Second}
	socksDialer, err := xproxy.SOCKS5("tcp", proxyAddress, auth, forward)
	if err != nil {
		return nil, err
	}
	if contextDialer, ok := socksDialer.(xproxy.ContextDialer); ok {
		return contextDialer.DialContext(ctx, network, endpoint)
	}
	return nil, errors.New("SOCKS5 video content proxy does not support context dialing")
}

func videoContentTLSHandshake(ctx context.Context, connection net.Conn, serverName string) (net.Conn, error) {
	handshakeCtx, cancel := context.WithTimeout(ctx, openAIVideoContentTLSSTimeout)
	defer cancel()
	tlsConnection := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName})
	if err := tlsConnection.HandshakeContext(handshakeCtx); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return tlsConnection, nil
}

type videoContentTransportBody struct {
	io.ReadCloser
	transport *http.Transport
	once      sync.Once
}

func (b *videoContentTransportBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() {
		if b.transport != nil {
			b.transport.CloseIdleConnections()
		}
	})
	return err
}

func (p *OpenAIVideoProvider) VerifyWebhook(ctx context.Context, account *Account, request ProviderWebhookRequest) (*ProviderWebhookEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if account == nil {
		return nil, rejectedVideoProviderError("authentication", "account_missing", "OpenAI webhook account is missing", http.StatusUnauthorized)
	}
	secrets := []string{
		strings.TrimSpace(account.GetCredential(openAIVideoWebhookSecretKey)),
		strings.TrimSpace(account.GetCredential(openAIVideoWebhookPreviousSecretKey)),
	}
	if secrets[0] == "" && secrets[1] == "" {
		return nil, rejectedVideoProviderError("authentication", "webhook_secret_missing", "OpenAI webhook secret is not configured", http.StatusUnauthorized)
	}
	id := strings.TrimSpace(request.Headers.Get("webhook-id"))
	timestampValue := strings.TrimSpace(request.Headers.Get("webhook-timestamp"))
	signatures := strings.TrimSpace(request.Headers.Get("webhook-signature"))
	timestamp, err := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil || id == "" || signatures == "" {
		return nil, rejectedVideoProviderError("authentication", "invalid_webhook_signature", "OpenAI webhook signature headers are invalid", http.StatusUnauthorized)
	}
	now := p.now().UTC()
	occurred := time.Unix(timestamp, 0).UTC()
	if occurred.Before(now.Add(-openAIVideoWebhookTolerance)) || occurred.After(now.Add(openAIVideoWebhookTolerance)) {
		return nil, rejectedVideoProviderError("authentication", "webhook_timestamp_out_of_range", "OpenAI webhook timestamp is outside the accepted window", http.StatusUnauthorized)
	}
	message := []byte(id + "." + timestampValue + "." + string(request.Body))
	matched := false
	validSecret := false
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		key, decodeErr := decodeStandardWebhookSecret(secret)
		if decodeErr != nil {
			continue
		}
		validSecret = true
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(message)
		matched = matchesStandardWebhookSignature(signatures, mac.Sum(nil)) || matched
	}
	if !validSecret {
		return nil, rejectedVideoProviderError("authentication", "invalid_webhook_secret", "OpenAI webhook secret is invalid", http.StatusUnauthorized)
	}
	if !matched {
		return nil, rejectedVideoProviderError("authentication", "invalid_webhook_signature", "OpenAI webhook signature is invalid", http.StatusUnauthorized)
	}
	var event struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		CreatedAt int64  `json:"created_at"`
		Data      struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := jsonstrict.RejectDuplicateKeys(request.Body); err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_webhook_payload", "OpenAI webhook payload is invalid", http.StatusBadRequest)
	}
	if err := json.Unmarshal(request.Body, &event); err != nil ||
		!validVideoProviderIdentifier(event.ID) || !validVideoProviderIdentifier(event.Data.ID) {
		return nil, rejectedVideoProviderError("validation", "invalid_webhook_payload", "OpenAI webhook payload is invalid", http.StatusBadRequest)
	}
	status := ""
	switch event.Type {
	case "video.completed":
		status = VideoGenerationCompleted
	case "video.failed":
		status = VideoGenerationFailed
	default:
		return nil, rejectedVideoProviderError("validation", "unsupported_webhook_event", "OpenAI webhook event type is not supported", http.StatusBadRequest)
	}
	if event.CreatedAt > 0 {
		occurred = time.Unix(event.CreatedAt, 0).UTC()
	}
	return &ProviderWebhookEvent{
		ProviderEventID: event.ID, ProviderTaskID: event.Data.ID,
		Status: status, OccurredAt: occurred,
		Payload: map[string]any{"type": event.Type},
	}, nil
}

func (p *OpenAIVideoProvider) validateAccountAndRequest(account *Account, request VideoCreateRequest, inputs []VideoInput) error {
	request.ReferenceMedia = normalizeOpenAICompatibleVideoReferenceFraming(request.ReferenceMedia)
	if p == nil || p.httpUpstream == nil || !p.SupportsAccount(account) {
		return rejectedVideoProviderError("permission", "unsupported_account", "account does not support OpenAI videos", http.StatusForbidden)
	}
	if err := ValidateVideoCreateCapabilities(p.Capabilities(), request, inputs); err != nil {
		return rejectedVideoProviderError("validation", "unsupported_capability", err.Error(), http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return rejectedVideoProviderError("validation", "prompt_required", "video prompt is required", http.StatusBadRequest)
	}
	if err := validateOpenAICompatibleSeedance20Request(request); err != nil {
		return rejectedVideoProviderError("validation", "invalid_seedance_request", err.Error(), http.StatusBadRequest)
	}
	if request.AudioEnabled != nil || strings.TrimSpace(request.ServiceTier) != "" || len(request.ProviderOptions) > 0 {
		return rejectedVideoProviderError("validation", "unsupported_option", "OpenAI video request contains an unsupported option", http.StatusBadRequest)
	}
	if !request.ReferenceMedia.Empty() {
		hasRatio := strings.TrimSpace(request.ReferenceMedia.Ratio) != "" ||
			strings.TrimSpace(request.ReferenceMedia.AspectRatio) != ""
		if request.Operation != VideoOperationGenerate || isOfficialOpenAIVideoAccount(account) || len(inputs) > 0 ||
			request.InputReference != nil || (strings.TrimSpace(request.Size) != "" && hasRatio) {
			return rejectedVideoProviderError("validation", "unsupported_reference_media", "video reference fields are not supported for this request", http.StatusBadRequest)
		}
		if _, err := openAICompatibleVideoReferenceFields(request.ReferenceMedia); err != nil {
			return rejectedVideoProviderError("validation", "invalid_reference_media", "compatible video reference fields are invalid", http.StatusBadRequest)
		}
	}
	if request.InputReference != nil {
		hasFile := strings.TrimSpace(request.InputReference.FileID) != ""
		hasURL := strings.TrimSpace(request.InputReference.ImageURL) != ""
		if hasFile == hasURL {
			return rejectedVideoProviderError("validation", "invalid_input_reference", "input_reference must provide exactly one of file_id or image_url", http.StatusBadRequest)
		}
		if hasFile && !account.hasVerifiedDedicatedIsolation() {
			return rejectedVideoProviderError("permission", "file_reference_requires_dedicated_account", "OpenAI file references require verified dedicated upstream isolation", http.StatusForbidden)
		}
	}
	for _, character := range request.Characters {
		if character.Provider != VideoProviderOpenAI || character.AccountID != account.ID || strings.TrimSpace(character.ProviderResourceID) == "" {
			return rejectedVideoProviderError("validation", "invalid_character", "video character must belong to the selected OpenAI account", http.StatusBadRequest)
		}
	}
	return nil
}

func openAIVideoCreateJSON(account *Account, request VideoCreateRequest) (map[string]any, error) {
	request.ReferenceMedia = normalizeOpenAICompatibleVideoReferenceFraming(request.ReferenceMedia)
	body := map[string]any{"model": request.Model, "prompt": request.Prompt}
	if request.Seconds > 0 {
		body["seconds"] = openAIVideoJSONSeconds(account, request)
	}
	if strings.TrimSpace(request.Size) != "" {
		body["size"] = strings.TrimSpace(request.Size)
	}
	if strings.TrimSpace(request.Quality) != "" {
		body["quality"] = strings.TrimSpace(request.Quality)
	}
	if request.InputReference != nil {
		reference := map[string]string{}
		if value := strings.TrimSpace(request.InputReference.FileID); value != "" {
			reference["file_id"] = value
		}
		if value := strings.TrimSpace(request.InputReference.ImageURL); value != "" {
			reference["image_url"] = value
		}
		if len(reference) == 1 {
			body["input_reference"] = reference
		}
	}
	if len(request.Characters) > 0 {
		characters := make([]map[string]string, 0, len(request.Characters))
		for _, character := range request.Characters {
			characters = append(characters, map[string]string{"id": character.ProviderResourceID})
		}
		body["characters"] = characters
	}
	if !request.ReferenceMedia.Empty() {
		fields, err := openAICompatibleVideoReferenceFields(request.ReferenceMedia)
		if err != nil {
			return nil, err
		}
		for key, value := range fields {
			body[key] = value
		}
	}
	return body, nil
}

func openAIVideoJSONSeconds(account *Account, request VideoCreateRequest) any {
	if isOfficialOpenAIVideoAccount(account) || isOpenAICompatibleSeedance20Request(request) {
		return strconv.Itoa(request.Seconds)
	}
	return request.Seconds
}

func isOpenAICompatibleSeedance20Request(request VideoCreateRequest) bool {
	for _, candidate := range []string{request.Model, request.RequestedModel} {
		model := strings.TrimSpace(candidate)
		if model == strings.ToLower(model) && validOpenAICompatibleSeedance20Model(model) {
			return true
		}
	}
	return false
}

func openAIVideoMultipartFields(request VideoCreateRequest) map[string]string {
	fields := map[string]string{"model": request.Model, "prompt": request.Prompt}
	if request.Seconds > 0 {
		fields["seconds"] = strconv.Itoa(request.Seconds)
	}
	if request.Size != "" {
		fields["size"] = request.Size
	}
	if request.Quality != "" {
		fields["quality"] = request.Quality
	}
	if len(request.Characters) > 0 {
		characters := make([]map[string]string, 0, len(request.Characters))
		for _, character := range request.Characters {
			characters = append(characters, map[string]string{"id": character.ProviderResourceID})
		}
		if encoded, err := json.Marshal(characters); err == nil {
			fields["characters"] = string(encoded)
		}
	}
	return fields
}

func (p *OpenAIVideoProvider) submitJSON(ctx context.Context, account *Account, endpoint string, payload map[string]any, clientToken string) (*ProviderVideoTask, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, rejectedVideoProviderError("validation", "invalid_payload", "video payload could not be encoded", http.StatusBadRequest)
	}
	response, err := p.doSubmission(ctx, account, endpoint, bytes.NewReader(body), "application/json", clientToken)
	if err != nil {
		return nil, unknownVideoProviderError("transport", "request_failed", "OpenAI video submission transport failed", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		prompt, _ := payload["prompt"].(string)
		return nil, p.responseError(response, account, true, prompt)
	}
	return decodeOpenAIVideoTask(response.Body, true)
}

func (p *OpenAIVideoProvider) submitMultipart(ctx context.Context, account *Account, endpoint string, fields map[string]string, fileField string, input VideoInput, clientToken string) (*ProviderVideoTask, error) {
	response, err := p.doMultipart(ctx, account, endpoint, fields, fileField, input, clientToken)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, p.responseError(response, account, true, fields["prompt"])
	}
	return decodeOpenAIVideoTask(response.Body, true)
}

func (p *OpenAIVideoProvider) doMultipart(ctx context.Context, account *Account, endpoint string, fields map[string]string, fileField string, input VideoInput, clientToken string) (*http.Response, error) {
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	done := make(chan error, 1)
	go func() {
		defer close(done)
		keys := []string{"model", "prompt", "seconds", "size", "quality", "name", "characters"}
		for _, key := range keys {
			if value, ok := fields[key]; ok && value != "" {
				if err := writer.WriteField(key, value); err != nil {
					_ = pipeWriter.CloseWithError(err)
					done <- err
					return
				}
			}
		}
		reader, err := input.Open(ctx)
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
			done <- err
			return
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, input.FileName))
		header.Set("Content-Type", input.MIMEType)
		part, err := writer.CreatePart(header)
		if err == nil {
			_, err = io.CopyBuffer(part, reader, make([]byte, 64*1024))
		}
		closeErr := reader.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = writer.Close()
		}
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
			done <- err
			return
		}
		err = pipeWriter.Close()
		done <- err
	}()

	response, requestErr := p.doSubmission(ctx, account, endpoint, pipeReader, writer.FormDataContentType(), clientToken)
	_ = pipeReader.Close()
	uploadErr := <-done
	if requestErr != nil {
		return nil, unknownVideoProviderError("transport", "request_failed", "OpenAI video upload transport failed", requestErr)
	}
	if uploadErr != nil {
		if response != nil && (response.StatusCode < 200 || response.StatusCode >= 300) {
			return response, nil
		}
		if response != nil {
			_ = response.Body.Close()
		}
		return nil, unknownVideoProviderError("transport", "upload_failed", "OpenAI video upload did not complete", uploadErr)
	}
	return response, nil
}

func (p *OpenAIVideoProvider) doSubmission(ctx context.Context, account *Account, endpoint string, body io.Reader, contentType, clientToken string) (*http.Response, error) {
	req, err := p.newRequest(ctx, account, http.MethodPost, endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	clientToken = strings.TrimSpace(clientToken)
	if clientToken != "" {
		req.Header.Set("Idempotency-Key", clientToken)
	}
	return p.execute(req, account)
}

func (p *OpenAIVideoProvider) do(ctx context.Context, account *Account, method, endpoint string, body io.Reader, contentType string, submission bool) (*http.Response, error) {
	req, err := p.newRequest(ctx, account, method, endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	return p.execute(req, account)
}

func (p *OpenAIVideoProvider) newRequest(ctx context.Context, account *Account, method, endpoint string, body io.Reader, contentType string) (*http.Request, error) {
	if !p.SupportsAccount(account) {
		return nil, rejectedVideoProviderError("permission", "unsupported_account", "account does not support OpenAI videos", http.StatusForbidden)
	}
	baseURL := account.GetOpenAIBaseURL()
	target := buildOpenAIEndpointURL(baseURL, endpoint)
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	req, err := http.NewRequestWithContext(requestCtx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+account.GetOpenAIApiKey())
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if userAgent := account.GetOpenAIUserAgent(); userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

func (p *OpenAIVideoProvider) execute(req *http.Request, account *Account) (*http.Response, error) {
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if p.tlsProfiles == nil {
		return p.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	}
	return p.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, p.tlsProfiles.ResolveTLSProfile(account))
}

type openAIVideoResponse struct {
	ID          string          `json:"id"`
	VideoURL    string          `json:"video_url"`
	Status      string          `json:"status"`
	Model       string          `json:"model"`
	Progress    *float64        `json:"progress"`
	Size        string          `json:"size"`
	Seconds     json.RawMessage `json:"seconds"`
	Quality     string          `json:"quality"`
	CreatedAt   int64           `json:"created_at"`
	CompletedAt int64           `json:"completed_at"`
	ExpiresAt   int64           `json:"expires_at"`
	Usage       map[string]any  `json:"usage"`
	Error       *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type openAIVideoCharacterResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ExpiresAt int64  `json:"expires_at"`
}

func decodeOpenAIVideoTask(reader io.Reader, submission bool) (*ProviderVideoTask, error) {
	var response openAIVideoResponse
	if err := decodeBoundedJSON(reader, openAIVideoMaxJSONResponse, &response); err != nil {
		if submission {
			return nil, unknownVideoProviderError("upstream", "invalid_response", "OpenAI video response could not be decoded", err)
		}
		return nil, controlVideoProviderError(err)
	}
	if !validVideoProviderIdentifier(response.ID) {
		if submission {
			return nil, unknownVideoProviderError("upstream", "missing_id", "OpenAI video response did not include an id", nil)
		}
		return nil, controlVideoProviderError(errors.New("OpenAI video response did not include an id"))
	}
	videoURL, err := normalizeProviderVideoURL(response.VideoURL)
	if err != nil {
		if submission {
			return nil, unknownVideoProviderError("upstream", "invalid_video_url", "OpenAI video response included an invalid video_url", err)
		}
		return nil, controlVideoProviderError(err)
	}
	rawStatus := boundedVideoProviderStatus(response.Status)
	status := normalizeOpenAIVideoStatus(rawStatus)
	metadata := map[string]any{"model": response.Model, "size": response.Size, "quality": response.Quality}
	if seconds := jsonScalarString(response.Seconds); seconds != "" {
		metadata["seconds"] = seconds
	} else if len(response.Seconds) > 0 {
		metadata["specification_invalid"] = float64(1)
	}
	variants := []string(nil)
	if status == VideoGenerationCompleted {
		variants = []string{"video", "thumbnail", "spritesheet"}
	}
	task := &ProviderVideoTask{
		ProviderTaskID: strings.TrimSpace(response.ID), VideoURL: videoURL, Status: status, RawStatus: rawStatus,
		Progress: response.Progress, Usage: sanitizeVideoProviderUsage(response.Usage), Metadata: metadata,
		ContentVariants: variants, ContentExpiresAt: unixTimePointer(response.ExpiresAt),
		ProviderCreatedAt: unixTimePointer(response.CreatedAt), ProviderFinishedAt: unixTimePointer(response.CompletedAt),
		SuggestedPollInterval: 10 * time.Second,
	}
	if response.Error != nil {
		task.ErrorCode = boundedVideoProviderCode(response.Error.Code)
		task.ErrorMessage = boundedVideoProviderMessage(response.Error.Message, "OpenAI video generation failed")
		if task.ErrorMessage == "" {
			task.ErrorMessage = "OpenAI video generation failed"
		}
	}
	return task, nil
}

func normalizeProviderVideoURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 16<<10 || !utf8.ValidString(raw) {
		return "", errors.New("video_url is too large or invalid UTF-8")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" ||
		(!strings.EqualFold(parsed.Scheme, "https") && !strings.EqualFold(parsed.Scheme, "http")) {
		return "", errors.New("video_url must be an absolute HTTP(S) URL without credentials or fragment")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", errors.New("video_url has an invalid port")
		}
	}
	return parsed.String(), nil
}

func normalizeOpenAIVideoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued":
		return VideoGenerationQueued
	case "in_progress", "processing", "running":
		return VideoGenerationInProgress
	case "completed", "succeeded":
		return VideoGenerationCompleted
	case "cancelled", "canceled":
		return VideoGenerationCancelled
	case "expired":
		return VideoGenerationExpired
	case "failed":
		return VideoGenerationFailed
	default:
		return VideoGenerationInProgress
	}
}

func (p *OpenAIVideoProvider) responseError(response *http.Response, account *Account, submission bool, sensitiveValues ...string) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, openAIVideoMaxErrorResponse))
	secret := ""
	if account != nil {
		secret = account.GetOpenAIApiKey()
	}
	message, code := parseOpenAIVideoError(body)
	message = boundedProviderMessage(message, append([]string{secret}, sensitiveValues...)...)
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	code = boundedVideoProviderCode(code)
	kind := "upstream"
	switch response.StatusCode {
	case http.StatusUnauthorized:
		kind = "authentication"
	case http.StatusForbidden:
		kind = "permission"
	case http.StatusNotFound:
		kind = "not_found"
	case http.StatusConflict:
		kind = "conflict"
	case http.StatusTooManyRequests:
		kind = "rate_limit"
	default:
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			kind = "validation"
		}
	}
	certainty := VideoSubmissionRejected
	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	if submission && retryable {
		certainty = VideoSubmissionUnknown
	}
	return &VideoProviderError{
		Kind: kind, Code: code, Message: message, Retryable: retryable,
		Certainty: certainty, RetryAfter: parseRetryAfter(response.Header), StatusCode: response.StatusCode,
	}
}

type openAIVideoErrorFields struct {
	Error   json.RawMessage `json:"error"`
	Message json.RawMessage `json:"message"`
	Detail  json.RawMessage `json:"detail"`
	Msg     json.RawMessage `json:"msg"`
	Code    json.RawMessage `json:"code"`
	Type    json.RawMessage `json:"type"`
}

func parseOpenAIVideoError(body []byte) (message, code string) {
	var root openAIVideoErrorFields
	if json.Unmarshal(body, &root) != nil {
		return "", ""
	}
	var nested openAIVideoErrorFields
	if len(root.Error) > 0 {
		if value := jsonScalarString(root.Error); value != "" {
			message = value
		} else {
			_ = json.Unmarshal(root.Error, &nested)
			message = firstVideoErrorMessage(nested.Message, nested.Detail, nested.Msg)
			code = firstVideoErrorCode(nested.Code, nested.Type)
		}
	}
	if message == "" {
		message = firstVideoErrorMessage(root.Message, root.Detail, root.Msg)
	}
	if code == "" {
		code = firstVideoErrorCode(root.Code, root.Type)
	}
	return strings.TrimSpace(message), strings.TrimSpace(code)
}

func firstVideoErrorMessage(values ...json.RawMessage) string {
	for _, raw := range values {
		if value := jsonScalarString(raw); value != "" {
			return value
		}
		var details []struct {
			Location []json.RawMessage `json:"loc"`
			Message  string            `json:"message"`
			Msg      string            `json:"msg"`
		}
		if json.Unmarshal(raw, &details) != nil || len(details) == 0 {
			continue
		}
		detail := details[0]
		message := strings.TrimSpace(detail.Message)
		if message == "" {
			message = strings.TrimSpace(detail.Msg)
		}
		if message == "" {
			continue
		}
		location := make([]string, 0, len(detail.Location))
		for _, item := range detail.Location {
			if value := jsonScalarString(item); value != "" {
				location = append(location, value)
			}
		}
		if len(location) > 0 {
			return strings.Join(location, ".") + ": " + message
		}
		return message
	}
	return ""
}

func firstVideoErrorCode(values ...json.RawMessage) string {
	for _, raw := range values {
		if value := jsonScalarString(raw); value != "" {
			return value
		}
	}
	return ""
}

func rejectedVideoProviderError(kind, code, message string, status int) *VideoProviderError {
	return &VideoProviderError{Kind: kind, Code: code, Message: boundedProviderMessage(message, ""), Certainty: VideoSubmissionRejected, StatusCode: status}
}

func unknownVideoProviderError(kind, code, message string, cause error) *VideoProviderError {
	return &VideoProviderError{Kind: kind, Code: code, Message: boundedProviderMessage(message, ""), Retryable: false, Certainty: VideoSubmissionUnknown, Cause: cause}
}

func controlVideoProviderError(cause error) *VideoProviderError {
	return &VideoProviderError{Kind: "transport", Code: "request_failed", Message: "OpenAI video control request failed", Retryable: true, Certainty: VideoSubmissionAccepted, Cause: cause}
}

func boundedProviderMessage(message string, sensitiveValues ...string) string {
	message = strings.ToValidUTF8(strings.ReplaceAll(message, "\x00", "\uFFFD"), "\uFFFD")
	for _, sensitive := range sensitiveValues {
		if sensitive = strings.TrimSpace(sensitive); sensitive != "" {
			message = strings.ReplaceAll(message, sensitive, "[REDACTED]")
		}
	}
	message = videoProviderURLPattern.ReplaceAllStringFunc(message, redactVideoProviderURL)
	const maxBytes = 1024
	if len(message) <= maxBytes {
		return message
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}

func redactVideoProviderURL(raw string) string {
	trailing := ""
	for len(raw) > 0 && strings.ContainsRune(".,);]}", rune(raw[len(raw)-1])) {
		trailing = raw[len(raw)-1:] + trailing
		raw = raw[:len(raw)-1]
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "[REDACTED_URL]" + trailing
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String() + trailing
}

func decodeBoundedJSON(reader io.Reader, maxBytes int64, target any) error {
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return err
	}
	if limited.N <= 0 {
		return errors.New("JSON response exceeds limit")
	}
	return nil
}

func jsonScalarString(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) == nil {
		return number.String()
	}
	return ""
}

func unixTimePointer(seconds int64) *time.Time {
	if seconds <= 0 {
		return nil
	}
	value := time.Unix(seconds, 0).UTC()
	return &value
}

func parseRetryAfter(header http.Header) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := time.Until(at); delay > 0 {
			return delay
		}
	}
	return 0
}

func decodeStandardWebhookSecret(secret string) ([]byte, error) {
	secret = strings.TrimSpace(strings.TrimPrefix(secret, "whsec_"))
	if decoded, err := base64.StdEncoding.DecodeString(secret); err == nil && len(decoded) > 0 {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(secret); err == nil && len(decoded) > 0 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(secret); err == nil && len(decoded) > 0 {
		return decoded, nil
	}
	if secret == "" {
		return nil, errors.New("empty webhook secret")
	}
	return []byte(secret), nil
}

func matchesStandardWebhookSignature(header string, expected []byte) bool {
	for _, item := range strings.Fields(strings.ReplaceAll(header, ",", " ")) {
		item = strings.TrimSpace(item)
		if strings.HasPrefix(item, "v1=") {
			item = strings.TrimPrefix(item, "v1=")
		} else if strings.HasPrefix(item, "v1,") {
			item = strings.TrimPrefix(item, "v1,")
		} else if item == "v1" {
			continue
		}
		actual, err := base64.StdEncoding.DecodeString(item)
		if err == nil && hmac.Equal(actual, expected) {
			return true
		}
	}
	return false
}
