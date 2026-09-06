package service

import (
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	VideoCreateMultipartMaxBytes       = 64 << 20
	VideoCreateMultipartMaxParts       = 64
	VideoCreateMultipartScalarMaxBytes = 64 << 10
)

type VideoCreateMultipartPart struct {
	Name        string `json:"name"`
	File        bool   `json:"file"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Digest      string `json:"sha256"`
}

func CanonicalVideoCreateMultipartPartsHash(parts []VideoCreateMultipartPart) (string, error) {
	if len(parts) == 0 || len(parts) > VideoCreateMultipartMaxParts {
		return "", ErrVideoInvalidRequest
	}
	normalized := append([]VideoCreateMultipartPart(nil), parts...)
	seen := make(map[string]bool, len(normalized))
	for index := range normalized {
		part := &normalized[index]
		if part.Name == "" || part.Name != strings.TrimSpace(part.Name) || !utf8.ValidString(part.Name) || strings.IndexFunc(part.Name, unicode.IsControl) >= 0 ||
			!utf8.ValidString(part.Filename) || strings.IndexFunc(part.Filename, unicode.IsControl) >= 0 || (part.File && strings.TrimSpace(part.Filename) == "") ||
			(!part.File && part.Filename != "") || part.Size < 0 {
			return "", ErrVideoInvalidRequest
		}
		previousFile, duplicate := seen[part.Name]
		if duplicate && (!part.File || !previousFile) {
			return "", ErrVideoInvalidRequest
		}
		seen[part.Name] = part.File
		if !part.File && part.Size > VideoCreateMultipartScalarMaxBytes {
			return "", ErrVideoInputTooLarge
		}
		digest, err := hex.DecodeString(part.Digest)
		if err != nil || len(digest) != sha256.Size || strings.ToLower(part.Digest) != part.Digest {
			return "", ErrVideoInvalidRequest
		}
		if part.ContentType != "" {
			mediaType, parameters, err := mime.ParseMediaType(part.ContentType)
			if err != nil {
				return "", ErrVideoInvalidRequest
			}
			part.ContentType = mime.FormatMediaType(mediaType, parameters)
		}
	}
	sort.SliceStable(normalized, func(first, second int) bool { return normalized[first].Name < normalized[second].Name })
	return HashVideoRequest(struct {
		Contract string                     `json:"contract"`
		Parts    []VideoCreateMultipartPart `json:"parts"`
	}{Contract: VideoCreateIntentMultipartContract, Parts: normalized})
}
