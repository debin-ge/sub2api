package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIVideoUploadedEditRejectsUnsupportedOutputParameters(t *testing.T) {
	for _, field := range []string{"seconds", "size", "quality", "source_id"} {
		t.Run(field, func(t *testing.T) {
			calls := 0
			provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				calls++
				return openAIVideoResponseForTest(http.StatusOK, `{"id":"never","status":"queued"}`, nil), nil
			}}, nil)
			request := VideoEditRequest{VideoCreateRequest: VideoCreateRequest{Model: OpenAIVideoModelSora2, Prompt: "make it blue"}}
			switch field {
			case "seconds":
				request.Seconds = 4
			case "size":
				request.Size = "1280x720"
			case "quality":
				request.Quality = "low"
			case "source_id":
				request.SourceTask = &ProviderTaskRef{ProviderTaskID: "another-video"}
			}
			_, err := provider.Edit(context.Background(), openAIVideoTestAccount(), request, []VideoInput{{
				VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleSourceVideo, MIMEType: "video/mp4", Size: 1024},
			}})
			require.Error(t, err)
			require.Zero(t, calls)
		})
	}
}

func TestOpenAIVideoMalformedSecondsRetainsConflictWithoutDroppingID(t *testing.T) {
	for _, value := range []string{"null", `{}`, `[]`, `""`} {
		t.Run(value, func(t *testing.T) {
			observed, err := decodeOpenAIVideoTask(strings.NewReader(`{"id":"video_bad_seconds","status":"completed","seconds":`+value+`}`), true)
			require.NoError(t, err)
			require.Equal(t, "video_bad_seconds", observed.ProviderTaskID)
			require.Equal(t, float64(1), sanitizeVideoProviderMetadata(observed.Metadata)["specification_invalid"])
		})
	}
}

func TestOpenAIVideoGetRejectsDifferentResourceIdentity(t *testing.T) {
	provider := NewOpenAIVideoProvider(&openAIVideoHTTPStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return openAIVideoResponseForTest(http.StatusOK, `{"id":"video_other","status":"completed","seconds":"8"}`, nil), nil
	}}, nil)
	account := openAIVideoTestAccount()
	task, err := provider.Get(context.Background(), account, ProviderTaskRef{Provider: VideoProviderOpenAI, AccountID: account.ID, ProviderTaskID: "video_owned"})
	require.Error(t, err)
	require.Nil(t, task)
}
