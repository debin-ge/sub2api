package service

import (
	"encoding/base64"
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

const (
	openAIVideoMaxReferenceImages = 9
	openAIVideoMaxReferenceVideos = 3
	openAIVideoMaxReferenceAudios = 3
	openAIVideoMaxReferenceBytes  = 512 << 10
)

var openAIVideoCompatibleRatios = map[string]struct{}{
	"16:9": {}, "9:16": {}, "1:1": {}, "4:3": {}, "3:4": {}, "21:9": {},
	"landscape": {}, "portrait": {}, "square": {},
}

func (references ProviderVideoReferenceMedia) Empty() bool {
	return strings.TrimSpace(references.Ratio) == "" && strings.TrimSpace(references.AspectRatio) == "" &&
		strings.TrimSpace(references.ImageURL) == "" && strings.TrimSpace(references.FirstImageURL) == "" &&
		strings.TrimSpace(references.LastImageURL) == "" && len(references.ReferenceImages) == 0 &&
		len(references.ReferenceVideos) == 0 && len(references.ReferenceAudios) == 0
}

func (references ProviderVideoReferenceMedia) HasImage() bool {
	return strings.TrimSpace(references.ImageURL) != "" || strings.TrimSpace(references.FirstImageURL) != "" ||
		strings.TrimSpace(references.LastImageURL) != "" || len(references.ReferenceImages) > 0
}

func (references ProviderVideoReferenceMedia) HasVideo() bool {
	return len(references.ReferenceVideos) > 0
}

func isOfficialOpenAIVideoAccount(account *Account) bool {
	if account == nil {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(account.GetOpenAIBaseURL()))
	return err == nil && strings.EqualFold(parsed.Hostname(), "api.openai.com")
}

func openAICompatibleVideoReferenceFields(references ProviderVideoReferenceMedia) (map[string]any, error) {
	fields := make(map[string]any)
	ratio := strings.ToLower(strings.TrimSpace(references.Ratio))
	aspectRatio := strings.ToLower(strings.TrimSpace(references.AspectRatio))
	if references.FramingDeterminedByMedia() {
		ratio, aspectRatio = "", ""
	}
	if ratio != "" && aspectRatio != "" {
		return nil, errors.New("ratio and aspect_ratio cannot be combined")
	}
	if ratio != "" {
		if _, ok := openAIVideoCompatibleRatios[ratio]; !ok {
			return nil, errors.New("ratio is not supported")
		}
		fields["ratio"] = ratio
	}
	if aspectRatio != "" {
		if _, ok := openAIVideoCompatibleRatios[aspectRatio]; !ok {
			return nil, errors.New("aspect_ratio is not supported")
		}
		fields["aspect_ratio"] = aspectRatio
	}

	imageURL, err := normalizeOpenAIVideoMediaReference(references.ImageURL, "image", true)
	if err != nil {
		return nil, err
	}
	firstImageURL, err := normalizeOpenAIVideoMediaReference(references.FirstImageURL, "image", true)
	if err != nil {
		return nil, err
	}
	lastImageURL, err := normalizeOpenAIVideoMediaReference(references.LastImageURL, "image", true)
	if err != nil {
		return nil, err
	}
	if imageURL != "" && (firstImageURL != "" || lastImageURL != "") {
		return nil, errors.New("image_url cannot be combined with first or last image URLs")
	}
	if (firstImageURL != "" || lastImageURL != "") &&
		(len(references.ReferenceImages) > 0 || len(references.ReferenceVideos) > 0) {
		return nil, errors.New("first or last image URLs cannot be combined with reference media")
	}
	if imageURL != "" {
		fields["image_url"] = imageURL
	}
	if firstImageURL != "" {
		fields["first_image_url"] = firstImageURL
	}
	if lastImageURL != "" {
		fields["last_image_url"] = lastImageURL
	}

	referenceImages, err := normalizeOpenAIVideoMediaReferences(
		references.ReferenceImages, openAIVideoMaxReferenceImages, "image", true,
	)
	if err != nil {
		return nil, err
	}
	referenceVideos, err := normalizeOpenAIVideoMediaReferences(
		references.ReferenceVideos, openAIVideoMaxReferenceVideos, "video", false,
	)
	if err != nil {
		return nil, err
	}
	referenceAudios, err := normalizeOpenAIVideoMediaReferences(
		references.ReferenceAudios, openAIVideoMaxReferenceAudios, "audio", true,
	)
	if err != nil {
		return nil, err
	}
	// 参考音频不能单独使用，必须同时带至少一份图片或视频参考——这条写在对外接口文档
	// §4.3，能力目录也以 reference_audios.requires_any 的形式下发给前端。三处必须同口径：
	// 这里放行而文档与目录照旧拦，等于让直连 API 的客户端和 Playground 用户拿到两套规则。
	if len(referenceAudios) > 0 && imageURL == "" && firstImageURL == "" && lastImageURL == "" &&
		len(referenceImages) == 0 && len(referenceVideos) == 0 {
		return nil, errors.New("reference audio requires an image or video reference")
	}
	if len(referenceImages) > 0 {
		fields["reference_images"] = referenceImages
	}
	if len(referenceVideos) > 0 {
		fields["reference_videos"] = referenceVideos
	}
	if len(referenceAudios) > 0 {
		fields["reference_audios"] = referenceAudios
	}
	return fields, nil
}

func (references ProviderVideoReferenceMedia) FramingDeterminedByMedia() bool {
	return strings.TrimSpace(references.ImageURL) != "" || strings.TrimSpace(references.FirstImageURL) != "" ||
		strings.TrimSpace(references.LastImageURL) != "" || len(references.ReferenceVideos) > 0
}

func normalizeOpenAICompatibleVideoReferenceFraming(references ProviderVideoReferenceMedia) ProviderVideoReferenceMedia {
	if references.FramingDeterminedByMedia() {
		references.Ratio = ""
		references.AspectRatio = ""
	}
	return references
}

func normalizeOpenAIVideoMediaReferences(values []string, maximum int, kind string, allowData bool) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > maximum {
		return nil, errors.New("too many media references")
	}
	normalized := make([]string, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		var err error
		normalized[index], err = normalizeOpenAIVideoMediaReference(value, kind, allowData)
		if err != nil || normalized[index] == "" {
			return nil, errors.New("invalid media reference")
		}
		if _, exists := seen[normalized[index]]; exists {
			return nil, errors.New("duplicate media reference")
		}
		seen[normalized[index]] = struct{}{}
	}
	return normalized, nil
}

func normalizeOpenAIVideoMediaReference(raw, kind string, allowData bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > openAIVideoMaxReferenceBytes {
		return "", errors.New("media reference is too large")
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:") {
		if !allowData {
			return "", errors.New("data references are not supported")
		}
		comma := strings.IndexByte(value, ',')
		if comma <= len("data:") || comma == len(value)-1 {
			return "", errors.New("invalid data reference")
		}
		header := strings.ToLower(value[:comma])
		if !strings.HasPrefix(header, "data:"+kind+"/") || !strings.HasSuffix(header, ";base64") {
			return "", errors.New("invalid data reference media type")
		}
		if _, err := base64.StdEncoding.DecodeString(value[comma+1:]); err != nil {
			return "", errors.New("invalid data reference encoding")
		}
		return value, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("invalid media URL")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" || host == "localhost" || host == "localhost.localdomain" {
		return "", errors.New("media URL must be public")
	}
	if address, parseErr := netip.ParseAddr(host); parseErr == nil && !isPublicOpenAIVideoReferenceIP(address) {
		return "", errors.New("media URL must be public")
	}
	return value, nil
}

func isPublicOpenAIVideoReferenceIP(address netip.Addr) bool {
	return address.IsValid() && address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() &&
		!address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsUnspecified()
}
