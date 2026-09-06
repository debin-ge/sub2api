package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func videoMultipartHashPart(name, body string) VideoCreateMultipartPart {
	digest := sha256.Sum256([]byte(body))
	return VideoCreateMultipartPart{Name: name, Size: int64(len(body)), Digest: hex.EncodeToString(digest[:])}
}

func TestVideoCreateMultipartPartsHashPreservesMeaning(t *testing.T) {
	model := videoMultipartHashPart("model", "video-alias")
	prompt := videoMultipartHashPart("prompt", "waves")
	file := videoMultipartHashPart("input_reference", "private-image-data")
	file.File, file.Filename, file.ContentType = true, "frame.png", "image/png"
	first, err := CanonicalVideoCreateMultipartPartsHash([]VideoCreateMultipartPart{model, prompt, file})
	require.NoError(t, err)
	reordered, err := CanonicalVideoCreateMultipartPartsHash([]VideoCreateMultipartPart{file, prompt, model})
	require.NoError(t, err)
	require.Equal(t, first, reordered)
	for _, change := range []string{"model", "prompt", "bytes", "size", "filename", "content_type", "file_order"} {
		t.Run(change, func(t *testing.T) {
			changedModel, changedPrompt, changedFile := model, prompt, file
			switch change {
			case "model":
				changedModel = videoMultipartHashPart("model", "other-alias")
			case "prompt":
				changedPrompt = videoMultipartHashPart("prompt", "other-prompt")
			case "bytes", "file_order":
				changedFile.Digest = strings.Repeat("a", 64)
			case "size":
				changedFile.Size++
			case "filename":
				changedFile.Filename = "other.png"
			case "content_type":
				changedFile.ContentType = "image/jpeg"
			}
			changed, err := CanonicalVideoCreateMultipartPartsHash([]VideoCreateMultipartPart{changedModel, changedPrompt, changedFile})
			require.NoError(t, err)
			require.NotEqual(t, first, changed)
			if change == "file_order" {
				forward, err := CanonicalVideoCreateMultipartPartsHash([]VideoCreateMultipartPart{model, file, changedFile})
				require.NoError(t, err)
				reverse, err := CanonicalVideoCreateMultipartPartsHash([]VideoCreateMultipartPart{model, changedFile, file})
				require.NoError(t, err)
				require.NotEqual(t, forward, reverse)
			}
		})
	}
}

func TestVideoCreateMultipartPartsHashLeavesFileSizeAdmissionToSpool(t *testing.T) {
	file := videoMultipartHashPart("video", "streamed-native-input")
	file.File, file.Filename, file.ContentType, file.Size = true, "source.mp4", "video/mp4", 128<<20
	_, err := CanonicalVideoCreateMultipartPartsHash([]VideoCreateMultipartPart{file})
	require.NoError(t, err)
}

func TestVideoCreateMultipartPartsHashRejectsInvalidDescriptors(t *testing.T) {
	for _, scenario := range []string{"duplicate_scalar", "mixed_scalar_file", "missing_name", "empty_filename", "scalar_filename", "control_name", "control_filename", "invalid_digest", "uppercase_digest", "negative_size", "content_type", "scalar_limit", "part_limit", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			part := videoMultipartHashPart("prompt", "waves")
			parts := []VideoCreateMultipartPart{part}
			expected := ErrVideoInvalidRequest
			switch scenario {
			case "duplicate_scalar":
				parts = append(parts, part)
			case "mixed_scalar_file":
				part.File, part.Filename = true, "frame.png"
				parts = append(parts, part)
			case "missing_name":
				parts[0].Name = ""
			case "empty_filename":
				parts[0].File = true
			case "scalar_filename":
				parts[0].Filename = "frame.png"
			case "control_name":
				parts[0].Name = "bad\x00name"
			case "control_filename":
				parts[0].File, parts[0].Filename = true, "bad\x00name"
			case "invalid_digest":
				parts[0].Digest = "invalid"
			case "uppercase_digest":
				parts[0].Digest = strings.Repeat("A", 64)
			case "negative_size":
				parts[0].Size = -1
			case "content_type":
				parts[0].ContentType = "image/png; charset="
			case "scalar_limit":
				parts[0].Size = VideoCreateMultipartScalarMaxBytes + 1
				expected = ErrVideoInputTooLarge
			case "part_limit":
				parts = make([]VideoCreateMultipartPart, VideoCreateMultipartMaxParts+1)
			case "empty":
				parts = nil
			}
			_, err := CanonicalVideoCreateMultipartPartsHash(parts)
			require.ErrorIs(t, err, expected)
		})
	}
}
