package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func nativeVideoReleasePolicyApplies(c *gin.Context) bool {
	if target, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context()); resolved {
		return service.IsManagedVideoPlatform(target)
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	// Composite multipart requests are classified after parsing. Preserve the
	// existing Grok forwarder and apply the policy before any native intent.
	return !ok || key == nil || key.Group == nil || service.IsManagedVideoPlatform(key.Group.Platform)
}
