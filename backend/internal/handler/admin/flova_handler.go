package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// FlovaHandler 暴露 Flova 账号的积分手动刷新端点：
//
//   - POST /admin/flova/accounts/:id/credits/refresh  立即拉一次 /user，写快照并按低水位暂停 / 恢复
type FlovaHandler struct {
	credits *service.FlovaCreditsSyncService
}

func NewFlovaHandler(credits *service.FlovaCreditsSyncService) *FlovaHandler {
	return &FlovaHandler{credits: credits}
}

func (h *FlovaHandler) RefreshCredits(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.credits == nil {
		response.BadRequest(c, "Flova credits sync is not available")
		return
	}
	user, err := h.credits.SyncAccountByID(c.Request.Context(), accountID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{
		"credits_balance":   user.CreditsBalance,
		"subscription_info": user.SubscriptionInfo,
		"upgrade_url":       user.UpgradeURL,
	})
}
