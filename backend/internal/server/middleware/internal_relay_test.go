package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestInternalRelayVerifiesAndAlwaysStripsHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-jwt-secret-32-bytes-long-value"
	signer := internalrelay.NewSigner(secret)
	validMarker, err := signer.Sign(7, "client:parent", time.Now())
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		marker    string
		wantRelay bool
	}{
		{name: "valid", marker: validMarker, wantRelay: true},
		{name: "invalid", marker: validMarker + "tampered", wantRelay: false},
		{name: "missing", marker: "", wantRelay: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(InternalRelay(secret))
			router.GET("/", func(c *gin.Context) {
				contextSigner, ok := c.Request.Context().Value(ctxkey.InternalRelaySigner).(*internalrelay.Signer)
				require.True(t, ok)
				require.NotNil(t, contextSigner)
				_, relay := c.Request.Context().Value(ctxkey.InternalRelay).(internalrelay.Metadata)
				require.Equal(t, tc.wantRelay, relay)
				require.Empty(t, c.Request.Header.Get(internalrelay.HeaderName))
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.marker != "" {
				req.Header.Set(internalrelay.HeaderName, tc.marker)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusNoContent, w.Code)
		})
	}
}
