package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// testVideoVendorAccountConnection 对聚合媒体平台账号做一次只读鉴权探测：不生成视频、
// 不计费。401 / 403 判定为凭证无效，其余状态都说明 key 被接受。
func (s *AccountTestService) testVideoVendorAccountConnection(c *gin.Context, account *Account) error {
	ctx := c.Request.Context()
	adapter := videoVendorAdapterByName(account.Platform)
	if adapter == nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("platform %s has no video adapter", account.Platform))
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	s.sendEvent(c, TestEvent{Type: "test_start", Model: VideoOnlyPlatformLabel(account.Platform) + " auth probe"})

	probe, ok := adapter.BuildProbe()
	if !ok {
		return s.sendErrorAndEnd(c, fmt.Sprintf("%s has no read-only probe endpoint", account.Platform))
	}
	provider := newVideoVendorProvider(adapter, s.httpUpstream, s.tlsFPProfileService)
	if !provider.SupportsAccount(account) {
		return s.sendErrorAndEnd(c, "account must be an apikey account with api_key configured")
	}
	response, err := provider.do(ctx, account, probe, "", false)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		errMsg := fmt.Sprintf("API returned %d: %s", response.StatusCode, strings.TrimSpace(string(response.Body)))
		if s.accountRepo != nil {
			_ = s.accountRepo.SetError(ctx, account.ID, errMsg)
		}
		return s.sendErrorAndEnd(c, errMsg)
	}
	s.sendEvent(c, TestEvent{
		Type: "content", Text: fmt.Sprintf("Credentials accepted (HTTP %d)", response.StatusCode),
	})
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}
