package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGrokManagedLookupDeliveryRequiresCapturedFunds(t *testing.T) {
	for _, billing := range []string{service.VideoBillingHeld, service.VideoBillingCapturePending, service.VideoBillingCaptured} {
		t.Run(billing, func(t *testing.T) {
			task := videoHandlerTask()
			task.Provider = service.VideoProviderGrok
			task.GenerationState = service.VideoGenerationCompleted
			task.BillingState = billing
			keyID, groupID := int64(7), int64(8)
			task.APIKeyID = &keyID
			task.GroupID = &groupID
			task.Progress = float64Pointer(100)
			task.ResponseMetadata = map[string]any{"seconds": 8.25}
			fake := &videoTaskAPIFake{getTask: task}
			h := newVideoHandler(fake, nil, videoHandlerTestConfig(t))
			id := grokManagedPublicID(task.PublicID)
			c, w := newVideoHandlerTestContext(http.MethodGet, "https://api.example/v1/videos/"+id, "", nil)
			c.Params = gin.Params{{Key: "request_id", Value: id}}
			require.True(t, h.tryGrokManagedLookup(c, false))
			require.Equal(t, 200, w.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.NotContains(t, w.Body.String(), "video_upstream_1")
			if billing == service.VideoBillingCaptured {
				require.Equal(t, "done", body["status"])
				video := body["video"].(map[string]any)
				require.Equal(t, 8.25, video["duration"])
				require.Contains(t, video["url"], "sig=")
			} else {
				require.Equal(t, "pending", body["status"])
				require.NotContains(t, body, "video")
				require.Equal(t, float64(99), body["progress"])
			}
		})
	}
}
func TestGrokManagedLookupPreservesKeyOwnership(t *testing.T) {
	task := videoHandlerTask()
	task.Provider = service.VideoProviderGrok
	keyID, groupID := int64(99), int64(8)
	task.APIKeyID = &keyID
	task.GroupID = &groupID
	h := newVideoHandler(&videoTaskAPIFake{getTask: task}, nil, videoHandlerTestConfig(t))
	id := grokManagedPublicID(task.PublicID)
	for _, content := range []bool{false, true} {
		c, w := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+id, "", nil)
		c.Params = gin.Params{{Key: "request_id", Value: id}}
		require.True(t, h.tryGrokManagedLookup(c, content))
		require.Equal(t, 404, w.Code)
	}
}
func TestGrokManagedContentDoesNotBypassPendingSettlement(t *testing.T) {
	task := videoHandlerTask()
	task.Provider = service.VideoProviderGrok
	task.GenerationState = service.VideoGenerationCompleted
	task.BillingState = service.VideoBillingCapturePending
	keyID, groupID := int64(7), int64(8)
	task.APIKeyID = &keyID
	task.GroupID = &groupID
	h := newVideoHandler(&videoTaskAPIFake{getTask: task}, nil, videoHandlerTestConfig(t))
	id := grokManagedPublicID(task.PublicID)
	c, w := newVideoHandlerTestContext(http.MethodGet, "/v1/videos/"+id+"/content", "", nil)
	c.Params = gin.Params{{Key: "request_id", Value: id}}
	require.True(t, h.tryGrokManagedLookup(c, true))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "video_settlement_pending")
}
