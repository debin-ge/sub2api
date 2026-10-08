package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

// testFlovaAccountConnection：POST /user 验 Key 并回显积分 / 档位；skill_id 为空时自动创建
// 私有 Skill 并回填凭据（方案 D9），非空时校验可读；最后写一次积分快照。
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

	skillID := provider.skillID(account)
	if skillID == "" {
		created, err := provider.CreateSkill(ctx, account)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Skill creation failed: %s", err.Error()))
		}
		if account.Credentials == nil {
			account.Credentials = map[string]any{}
		}
		account.Credentials[flovaCredentialSkillID] = created
		if s.accountRepo != nil {
			if err := s.accountRepo.Update(ctx, account); err != nil {
				return s.sendErrorAndEnd(c, fmt.Sprintf("Skill %s was created but could not be saved to the account: %s", created, err.Error()))
			}
		}
		s.sendEvent(c, TestEvent{Type: "content", Text: "Private skill created and saved: " + created})
	} else if err := provider.GetSkill(ctx, account, skillID); err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Skill %s is not readable: %s", skillID, err.Error()))
	} else {
		s.sendEvent(c, TestEvent{Type: "content", Text: "Private skill verified: " + skillID})
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
