package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type grokManagedUpload struct {
	name  string
	input service.VideoInput
	index int
}
type grokManagedUploads struct {
	session *service.VideoSpoolSession
	images  []grokManagedUpload
	source  *service.VideoInput
	hash    string
}

// Preserve the gateway's multipart clients while submitting native JSON to xAI.
// Files stream into the encrypted spool; they are never expanded into JSON just
// to parse or hash the incoming request.
func (h *VideoHandler) parseGrokManagedMultipart(ctx context.Context, userID int64, operation, contentType string, raw []byte, reuse ...*service.VideoSpoolSession) (fields []byte, uploads *grokManagedUploads, err error) {
	_, params, e := mime.ParseMediaType(contentType)
	if e != nil || params["boundary"] == "" {
		return nil, nil, service.ErrVideoInvalidRequest
	}
	uploads = &grokManagedUploads{}
	var reuseInputs []service.VideoInput
	if len(reuse) > 0 && reuse[0] != nil {
		uploads.session = reuse[0]
		reuseInputs = reuse[0].Inputs()
	}
	defer func() {
		if err != nil && uploads.session != nil {
			_ = uploads.session.Close()
		}
	}()
	values := map[string]json.RawMessage{}
	seen := map[string]bool{}
	parts := []service.VideoCreateMultipartPart{}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	for count := 0; ; count++ {
		part, e := reader.NextRawPart()
		if e == io.EOF {
			break
		}
		if e != nil || count >= service.VideoCreateMultipartMaxParts {
			return nil, uploads, service.ErrVideoInvalidRequest
		}
		name := part.FormName()
		if name == "" || name != strings.TrimSpace(name) || hasControlCharacter(name) {
			_ = part.Close()
			return nil, uploads, service.ErrVideoInvalidRequest
		}
		for key, entries := range part.Header {
			if len(entries) != 1 || (key != "Content-Type" && key != "Content-Disposition") {
				_ = part.Close()
				return nil, uploads, service.ErrVideoInvalidRequest
			}
		}
		disposition, attributes, parseErr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if parseErr != nil || disposition != "form-data" {
			_ = part.Close()
			return nil, uploads, service.ErrVideoInvalidRequest
		}
		for attribute := range attributes {
			if attribute != "name" && attribute != "filename" {
				_ = part.Close()
				return nil, uploads, service.ErrVideoInvalidRequest
			}
		}
		filename, file := attributes["filename"]
		if file && strings.TrimSpace(filename) == "" {
			_ = part.Close()
			return nil, uploads, service.ErrVideoInvalidRequest
		}
		if previous, duplicate := seen[name]; duplicate && (!file || !previous) {
			_ = part.Close()
			return nil, uploads, service.ErrVideoInvalidRequest
		}
		seen[name] = file
		if file {
			role, index := service.VideoInputRole(""), -1
			switch name {
			case "image", "input_reference":
				role = service.VideoInputRoleFirstFrame
			case "last_frame":
				role = service.VideoInputRoleLastFrame
			case "reference_images", "reference_images[]", "image[]":
				role = service.VideoInputRoleTaggedReferenceImage
			case "video":
				if operation == service.VideoOperationEdit || operation == service.VideoOperationExtend {
					role = service.VideoInputRoleSourceVideo
				}
			}
			if strings.HasPrefix(name, "image[") && strings.HasSuffix(name, "]") && name != "image[]" {
				index, e = strconv.Atoi(name[6 : len(name)-1])
				if e != nil || index < 0 || index > 13 {
					_ = part.Close()
					return nil, uploads, service.ErrVideoInvalidRequest
				}
				role = service.VideoInputRoleTaggedReferenceImage
			}
			if role == "" || h == nil || h.spool == nil {
				_ = part.Close()
				return nil, uploads, service.ErrVideoInputUnsupported
			}
			if uploads.session == nil {
				uploads.session, e = h.spool.Begin(ctx, userID)
				if e != nil {
					_ = part.Close()
					return nil, uploads, e
				}
			}
			var input service.VideoInput
			if len(reuseInputs) > 0 {
				hash := sha256.New()
				size, readErr := io.Copy(hash, io.LimitReader(part, service.VideoCreateMultipartMaxBytes+1))
				if readErr != nil || size > service.VideoCreateMultipartMaxBytes {
					_ = part.Close()
					return nil, uploads, service.ErrVideoInputTooLarge
				}
				digest := hex.EncodeToString(hash.Sum(nil))
				for _, existing := range reuseInputs {
					if existing.Size == size && existing.SHA256 == digest {
						input = existing
						input.Role = role
						break
					}
				}
				if input.Open == nil {
					_ = part.Close()
					return nil, uploads, service.ErrVideoInputUnsupported
				}
			} else {
				input, e = uploads.session.Store(ctx, role, filename, part.Header.Get("Content-Type"), part)
			}
			_ = part.Close()
			if e != nil {
				return nil, uploads, e
			}
			parts = append(parts, service.VideoCreateMultipartPart{Name: name, File: true, Filename: filename, ContentType: part.Header.Get("Content-Type"), Size: input.Size, Digest: input.SHA256})
			if role == service.VideoInputRoleSourceVideo {
				if uploads.source != nil {
					return nil, uploads, service.ErrVideoInvalidRequest
				}
				copy := input
				uploads.source = &copy
			} else {
				uploads.images = append(uploads.images, grokManagedUpload{name: name, input: input, index: index})
			}
			continue
		}
		data, e := io.ReadAll(io.LimitReader(part, service.VideoCreateMultipartScalarMaxBytes+1))
		_ = part.Close()
		if e != nil || len(data) > service.VideoCreateMultipartScalarMaxBytes {
			return nil, uploads, service.ErrVideoInputTooLarge
		}
		sum := sha256.Sum256(data)
		parts = append(parts, service.VideoCreateMultipartPart{Name: name, Size: int64(len(data)), Digest: hex.EncodeToString(sum[:]), ContentType: part.Header.Get("Content-Type")})
		target := name
		switch name {
		case "ratio":
			target = "aspect_ratio"
		case "audio":
			target = "generate_audio"
		case "image_url", "first_image_url":
			target = "image"
		case "last_image_url":
			target = "last_frame"
		}
		if _, duplicate := values[target]; duplicate {
			return nil, uploads, service.ErrVideoInvalidRequest
		}
		switch target {
		case "model", "prompt", "resolution", "aspect_ratio", "duration", "seconds", "callback_url":
			values[target], _ = json.Marshal(string(data))
		case "generate_audio":
			flag, e := strconv.ParseBool(strings.TrimSpace(string(data)))
			if e != nil {
				return nil, uploads, service.ErrVideoInvalidRequest
			}
			values[target], _ = json.Marshal(flag)
		case "image", "last_frame", "video":
			if json.Valid(data) {
				values[target] = append([]byte(nil), data...)
			} else {
				values[target], _ = json.Marshal(string(data))
			}
		case "reference_images", "reference_audios", "keyframes":
			if !json.Valid(data) {
				return nil, uploads, service.ErrVideoInvalidRequest
			}
			values[target] = append([]byte(nil), data...)
		default:
			return nil, uploads, service.ErrVideoUnsupportedParameter
		}
	}
	// Explicit image[n] indices determine reference order, independently of form
	// part order. Repeated array fields retain their original order.
	sort.SliceStable(uploads.images, func(i, j int) bool {
		a, b := uploads.images[i], uploads.images[j]
		if a.index >= 0 && b.index >= 0 {
			return a.index < b.index
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.index < b.index
	})
	if uploads.hash, err = service.CanonicalVideoCreateMultipartPartsHash(parts); err != nil {
		return nil, uploads, err
	}
	fields, err = json.Marshal(values)
	return fields, uploads, err
}
