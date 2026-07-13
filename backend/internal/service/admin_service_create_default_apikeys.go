package service

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// createDefaultAPIKeysAsync 异步为新用户创建所有公开分组的 API Key
func (s *adminServiceImpl) createDefaultAPIKeysAsync(userID int64) {
	go func() {
		// 使用独立的 context，避免依赖请求上下文
		bgCtx := context.Background()

		// 调用同步方法创建 API Keys
		if err := s.createDefaultAPIKeys(bgCtx, userID); err != nil {
			logger.LegacyPrintf("service.admin",
				"[Admin] Failed to create default API keys for user %d: %v",
				userID, err)
		}
	}()
}

// createDefaultAPIKeys 为新用户创建所有公开分组的 API Key（同步方法）
// 公开分组 = 非订阅类型 && 非专属 && 活跃状态
func (s *adminServiceImpl) createDefaultAPIKeys(ctx context.Context, userID int64) error {
	if s.apiKeyService == nil || s.groupRepo == nil {
		return fmt.Errorf("apiKeyService or groupRepo not initialized")
	}

	// 1. 获取所有活跃分组
	allGroups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active groups: %w", err)
	}

	// 2. 过滤出公开分组（非订阅类型 && 非专属）
	publicGroups := make([]Group, 0)
	for _, group := range allGroups {
		if !group.IsSubscriptionType() && !group.IsExclusive {
			publicGroups = append(publicGroups, group)
		}
	}

	if len(publicGroups) == 0 {
		logger.LegacyPrintf("service.admin",
			"[Admin] No public groups found, skipping default API key creation for user %d",
			userID)
		return nil
	}

	// 3. 为每个公开分组创建 API Key
	successCount := 0
	for _, group := range publicGroups {
		groupID := group.ID
		req := CreateAPIKeyRequest{
			Name:          fmt.Sprintf("%s_default", group.Name),
			GroupID:       &groupID,
			CustomKey:     nil,        // 自动生成
			IPWhitelist:   []string{}, // 无限制
			IPBlacklist:   []string{}, // 无限制
			Quota:         0,          // 0 = 无限制
			ExpiresInDays: nil,        // nil = 永不过期
			RateLimit5h:   0,          // 0 = 无限制
			RateLimit1d:   0,          // 0 = 无限制
			RateLimit7d:   0,          // 0 = 无限制
		}

		if _, err := s.apiKeyService.Create(ctx, userID, req); err != nil {
			// 记录失败但继续创建其他分组的 API Key
			logger.LegacyPrintf("service.admin",
				"[Admin] Failed to create default API key for user %d, group %d (%s): %v",
				userID, group.ID, group.Name, err)
		} else {
			successCount++
		}
	}

	logger.LegacyPrintf("service.admin",
		"[Admin] Created %d/%d default API keys for user %d",
		successCount, len(publicGroups), userID)

	return nil
}
