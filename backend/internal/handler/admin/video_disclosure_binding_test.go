package admin

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func bindVideoDisclosureJSON(t *testing.T, target any, body string) error {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("PUT", "/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c.ShouldBindJSON(target)
}

func TestVideoDisclosurePolicyUpdateBindingAcceptsExplicitClear(t *testing.T) {
	t.Run("account", func(t *testing.T) {
		var req UpdateAccountRequest
		require.NoError(t, bindVideoDisclosureJSON(t, &req, `{"video_disclosure_policy":""}`))
		require.NotNil(t, req.VideoDisclosurePolicy)
		require.Empty(t, *req.VideoDisclosurePolicy)
	})

	t.Run("group", func(t *testing.T) {
		var req UpdateGroupRequest
		require.NoError(t, bindVideoDisclosureJSON(t, &req, `{"video_disclosure_policy":""}`))
		require.NotNil(t, req.VideoDisclosurePolicy)
		require.Empty(t, *req.VideoDisclosurePolicy)
	})
}

func TestVideoDisclosurePolicyUpdateBindingRejectsInvalidValue(t *testing.T) {
	t.Run("account", func(t *testing.T) {
		var req UpdateAccountRequest
		require.Error(t, bindVideoDisclosureJSON(t, &req, `{"video_disclosure_policy":"invalid"}`))
	})

	t.Run("group", func(t *testing.T) {
		var req UpdateGroupRequest
		require.Error(t, bindVideoDisclosureJSON(t, &req, `{"video_disclosure_policy":"invalid"}`))
	})
}
