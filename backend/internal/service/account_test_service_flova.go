package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

// testFlovaAccountConnection：POST /user 验 Key 并回显积分 / 档位；配置了 skill_id 时校验可读；
// 最后写一次积分快照。不创建 Skill，也不改写账号凭据。
func (s *AccountTestService) testFlovaAccountConnection(c *gin.Context, account *Account) error {
	ctx := c.Request.Context()
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	s.sendEvent(c, TestEvent{Type: "test_start", Model: "Flova auth probe"})
	if account == nil || account.Type != AccountTypeAPIKey || strings.TrimSpace(account.GetCredential(flovaCredentialAPIKey)) == "" {
		return s.sendErrorAndEnd(c, "account must be an apikey account with api_key configured")
	}
	provider := s.flovaProvider()

	user, err := provider.FetchUser(ctx, account)
	if err != nil {
		var typed *VideoProviderError
		if errors.As(err, &typed) && typed != nil && (typed.Kind == "authentication" || typed.Kind == "permission") {
			if s.accountRepo != nil {
				_ = s.accountRepo.SetError(ctx, account.ID, "Flova rejected the API key: "+typed.Message)
			}
		}
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	s.sendEvent(c, TestEvent{Type: "content", Text: fmt.Sprintf("Credentials accepted. credits_balance=%s", formatFlovaCredits(user.CreditsBalance))})
	if len(user.SubscriptionInfo) == 0 {
		s.sendEvent(c, TestEvent{Type: "content", Text: "subscription_info is empty: this looks like a free-tier account (watermarked output, Seedance gated). Upgrade before routing traffic."})
	} else {
		plan, _ := user.SubscriptionInfo["lookup_key"].(string)
		status, _ := user.SubscriptionInfo["status"].(string)
		s.sendEvent(c, TestEvent{Type: "content", Text: fmt.Sprintf("Subscription: %s (%s)", flovaNonEmpty(plan, "unknown plan"), flovaNonEmpty(status, "unknown status"))})
	}

	// Skill 可选：单镜头约束写在每轮指令里。配置了 skill_id 时只校验它对这个 API Key 可读。
	if skillID := provider.skillID(account); skillID == "" {
		s.sendEvent(c, TestEvent{Type: "content", Text: "No skill_id configured (optional): generation rules are sent with every request."})
	} else if err := provider.GetSkill(ctx, account, skillID); err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Skill %s is not readable: %s", skillID, err.Error()))
	} else {
		s.sendEvent(c, TestEvent{Type: "content", Text: "Configured skill verified: " + skillID})
	}

	if s.accountRepo != nil {
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, flovaCreditsExtraUpdates(user, time.Now())); err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Credits snapshot could not be saved: %s", err.Error()))
		}
	}
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

func (s *AccountTestService) flovaProvider() *FlovaVideoProvider {
	var cfg config.GatewayFlovaConfig
	if s != nil && s.cfg != nil {
		cfg = s.cfg.Gateway.Flova
	}
	return NewFlovaVideoProvider(s.httpUpstream, s.tlsFPProfileService, cfg)
}
