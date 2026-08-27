package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// OptionalJWTAuth lets anonymous requests pass while preserving strict JWT
// validation whenever an Authorization header is present.
func OptionalJWTAuth(strict JWTAuthMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(c.GetHeader("Authorization")) == "" {
			c.Next()
			return
		}
		gin.HandlerFunc(strict)(c)
	}
}
