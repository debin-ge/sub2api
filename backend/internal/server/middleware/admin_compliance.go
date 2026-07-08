package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

func AdminComplianceGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil || isAdminComplianceBypassPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		subject, ok := GetAuthSubjectFromContext(c)
		if !ok {
			AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization required")
			return
		}

		status, err := settingService.GetAdminComplianceStatus(c.Request.Context(), subject.UserID)
		if err != nil {
			AbortWithError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
			return
		}
		if status == nil {
			AbortWithError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
			return
		}
		if !status.Required {
			c.Next()
			return
		}

		c.JSON(http.StatusLocked, gin.H{
			"code":    "ADMIN_COMPLIANCE_ACK_REQUIRED",
			"message": "administrator compliance acknowledgement is required",
			"metadata": gin.H{
				"version":          service.AdminComplianceVersion,
				"site_name":        status.SiteName,
				"document_path_zh": service.AdminComplianceDocumentPathZH,
				"document_path_en": service.AdminComplianceDocumentPathEN,
				"document_url_zh":  service.AdminComplianceDocumentURLZH,
				"document_url_en":  service.AdminComplianceDocumentURLEN,
				"ack_phrase_zh":    status.AckPhraseZH,
				"ack_phrase_en":    status.AckPhraseEN,
			},
		})
		c.Abort()
	}
}

func isAdminComplianceBypassPath(path string) bool {
	path = strings.TrimSpace(path)
	return path == "/api/v1/admin/compliance" || strings.HasPrefix(path, "/api/v1/admin/compliance/")
}
