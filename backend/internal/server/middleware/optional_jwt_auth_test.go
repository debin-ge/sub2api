//go:build unit

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOptionalJWTAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	strictCalls := 0
	router := gin.New()
	router.Use(OptionalJWTAuth(JWTAuthMiddleware(func(c *gin.Context) {
		strictCalls++
		if c.GetHeader("Authorization") != "Bearer valid" {
			AbortWithError(c, http.StatusUnauthorized, "INVALID_TOKEN", "Invalid token")
			return
		}
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 7})
		c.Next()
	})))
	router.GET("/plaza", func(c *gin.Context) {
		subject, authenticated := GetAuthSubjectFromContext(c)
		c.JSON(http.StatusOK, gin.H{"authenticated": authenticated, "user_id": subject.UserID})
	})

	t.Run("anonymous", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/plaza", nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), `"authenticated":false`)
	})

	t.Run("valid token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/plaza", nil)
		req.Header.Set("Authorization", "Bearer valid")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), `"authenticated":true`)
		require.Contains(t, w.Body.String(), `"user_id":7`)
	})

	t.Run("invalid token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/plaza", nil)
		req.Header.Set("Authorization", "Bearer invalid")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	require.Equal(t, 2, strictCalls)
}
