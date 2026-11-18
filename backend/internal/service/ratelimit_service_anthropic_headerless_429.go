package service

import "time"

const (
	// anthropicHeaderless429Threshold 是"无限流头 429"连续出现多少次（期间账号没有任何成功响应）
	// 才对账号做兜底冷却。低于该次数只换号、不动账号状态。
	anthropicHeaderless429Threshold = 3
	// anthropicHeaderless429Window 是连续计数的有效期：距上一次无头 429 超过该时长则重新计数。
	anthropicHeaderless429Window = 60 * time.Second
)

// anthropicHeaderless429Streak 记录单个账号连续的无头 429。
type anthropicHeaderless429Streak struct {
	count int
	last  time.Time
}

// observeAnthropicHeaderless429 记录一次无限流头、无 Retry-After 的 Anthropic 429，
// 返回是否应对账号做兜底冷却。
//
// 这类 429 不携带任何账号窗口信息。线上观察到它常常是请求级拒绝：同一账号对某一类
// 请求（例如非流式）每次必拒，而其他请求照常成功。此时冷却整个账号只会让它在管理端
// 反复显示"限流中 0m"，并挡住本来能成功的请求。只有账号在窗口内连续多次命中且期间
// 没有成功响应时，才认为是账号本身被限制。
func (s *RateLimitService) observeAnthropicHeaderless429(accountID int64, now time.Time) bool {
	s.anthropicHeaderless429Mu.Lock()
	defer s.anthropicHeaderless429Mu.Unlock()

	if s.anthropicHeaderless429 == nil {
		s.anthropicHeaderless429 = make(map[int64]*anthropicHeaderless429Streak)
	}
	streak := s.anthropicHeaderless429[accountID]
	if streak == nil || now.Sub(streak.last) > anthropicHeaderless429Window {
		streak = &anthropicHeaderless429Streak{}
		s.anthropicHeaderless429[accountID] = streak
	}
	streak.count++
	streak.last = now
	return streak.count >= anthropicHeaderless429Threshold
}

// resetAnthropicHeaderless429 在账号拿到成功响应后清零连续计数。
func (s *RateLimitService) resetAnthropicHeaderless429(accountID int64) {
	s.anthropicHeaderless429Mu.Lock()
	defer s.anthropicHeaderless429Mu.Unlock()
	delete(s.anthropicHeaderless429, accountID)
}
