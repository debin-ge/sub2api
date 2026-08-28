package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	_ "golang.org/x/image/webp"
)

const maxContactQRCodeImageBytes = 500 * 1024

var contactQRCodeSignatures = map[string]func([]byte) bool{
	"image/png": func(data []byte) bool {
		return len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	},
	"image/jpeg": func(data []byte) bool {
		return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
	},
	"image/webp": func(data []byte) bool {
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	},
}

var contactQRCodeImageFormats = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpeg",
	"image/webp": "webp",
}

// ContactQRCodeImage is the decoded public QR image served by the dedicated endpoint.
type ContactQRCodeImage struct {
	Data        []byte
	ContentType string
	Revision    string
	ETag        string
}

func normalizeContactQRCode(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if _, _, err := decodeContactQRCodeDataURL(value); err != nil {
		return "", err
	}
	return value, nil
}

func decodeContactQRCodeDataURL(value string) ([]byte, string, error) {
	comma := strings.IndexByte(value, ',')
	if comma < 0 {
		return nil, "", invalidContactQRCode("contact QR code must be a PNG, JPEG, or WEBP image")
	}
	header := strings.ToLower(value[:comma+1])
	contentType := ""
	switch header {
	case "data:image/png;base64,":
		contentType = "image/png"
	case "data:image/jpeg;base64,":
		contentType = "image/jpeg"
	case "data:image/webp;base64,":
		contentType = "image/webp"
	}
	if contentType == "" {
		return nil, "", invalidContactQRCode("contact QR code must be a PNG, JPEG, or WEBP image")
	}

	decoded, err := base64.StdEncoding.DecodeString(value[comma+1:])
	if err != nil || len(decoded) == 0 {
		return nil, "", invalidContactQRCode("contact QR code contains invalid image data")
	}
	if len(decoded) > maxContactQRCodeImageBytes {
		return nil, "", invalidContactQRCode("contact QR code exceeds the 500KB limit")
	}
	if !contactQRCodeSignatures[contentType](decoded) {
		return nil, "", invalidContactQRCode("contact QR code bytes do not match the declared image type")
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || config.Width <= 0 || config.Height <= 0 || format != contactQRCodeImageFormats[contentType] {
		return nil, "", invalidContactQRCode("contact QR code is not a valid decodable image")
	}

	return decoded, contentType, nil
}

func invalidContactQRCode(message string) error {
	return infraerrors.BadRequest("INVALID_CONTACT_QR_CODE", message)
}

// GetContactQRCodeImage reads and decodes the QR code only when a user opens the support dialog.
func (s *SettingService) GetContactQRCodeImage(ctx context.Context) (*ContactQRCodeImage, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContactQRCode)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return nil, nil
		}
		return nil, err
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}

	data, contentType, err := decodeContactQRCodeDataURL(value)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	revision := hex.EncodeToString(digest[:])
	return &ContactQRCodeImage{
		Data:        data,
		ContentType: contentType,
		Revision:    revision,
		ETag:        "\"" + revision + "\"",
	}, nil
}
