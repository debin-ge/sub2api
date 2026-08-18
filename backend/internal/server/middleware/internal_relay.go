package middleware

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
	"github.com/gin-gonic/gin"
)

// InternalRelay authenticates the private loopback marker and always strips
// the header. Invalid, malformed, and expired markers are intentionally
// ignored so they never change request or response behavior.
func InternalRelay(jwtSecret string) gin.HandlerFunc {
	signer := internalrelay.NewSigner(jwtSecret)
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}

		raw := c.Request.Header.Get(internalrelay.HeaderName)
		c.Request.Header.Del(internalrelay.HeaderName)
		ctx := context.WithValue(c.Request.Context(), ctxkey.InternalRelaySigner, signer)
		if metadata, err := signer.Verify(raw, time.Now()); err == nil {
			ctx = context.WithValue(ctx, ctxkey.InternalRelay, metadata)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
