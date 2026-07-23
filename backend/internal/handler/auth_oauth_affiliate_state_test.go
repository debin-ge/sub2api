package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOAuthAffiliateCodeCookieRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/auth/oauth/linuxdo/start?aff_code=AFF123",
		nil,
	)

	captureOAuthAffiliateCode(c, false)

	var affiliateCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == oauthAffiliateCodeCookieName {
			affiliateCookie = cookie
			break
		}
	}
	require.NotNil(t, affiliateCookie)
	require.True(t, affiliateCookie.HttpOnly)
	require.Equal(t, oauthPendingBrowserCookiePath, affiliateCookie.Path)

	readRecorder := httptest.NewRecorder()
	readContext, _ := gin.CreateTestContext(readRecorder)
	readContext.Request = httptest.NewRequest(http.MethodGet, "/callback", nil)
	readContext.Request.AddCookie(affiliateCookie)
	require.Equal(t, "AFF123", readOAuthAffiliateCode(readContext))
}

func TestPendingOAuthAffiliateCodeReadsServerSideFallback(t *testing.T) {
	session := &dbent.PendingAuthSession{
		LocalFlowState: map[string]any{
			oauthAffiliateCodeStateKey: "AFF456",
		},
	}

	require.Equal(t, "AFF456", pendingOAuthAffiliateCode(session))
	require.Empty(t, pendingOAuthAffiliateCode(nil))
}
