package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const MiniMaxTokenPlanTextWindowSeconds int64 = 5 * 60 * 60
const MiniMaxTokenPlanDefaultText5hLimit int64 = 4500
const miniMaxQuotaMaxInt64ExclusiveFloat = 9223372036854775808.0
const miniMaxOfficialRemainsFreshSeconds int64 = 15 * 60

type MiniMaxQuotaDecision struct {
	Allowed bool
	Used    int64
	Limit   int64
	Reason  string
}

type MiniMaxQuotaService struct {
	cache  MiniMaxQuotaCache
	client *MiniMaxTokenPlanClient
}

func NewMiniMaxQuotaService(cache MiniMaxQuotaCache, client *MiniMaxTokenPlanClient) *MiniMaxQuotaService {
	return &MiniMaxQuotaService{cache: cache, client: client}
}

func (s *MiniMaxQuotaService) ReserveTextRequest(ctx context.Context, account *Account, requestID string) (*MiniMaxQuotaDecision, error) {
	if account == nil || account.Platform != PlatformMiniMax || account.ID <= 0 {
		return &MiniMaxQuotaDecision{Allowed: false, Reason: "invalid_minimax_account"}, fmt.Errorf("invalid minimax account")
	}
	if s == nil || s.cache == nil {
		return &MiniMaxQuotaDecision{Allowed: false, Reason: "quota_cache_unavailable"}, fmt.Errorf("minimax quota cache unavailable")
	}

	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return &MiniMaxQuotaDecision{Allowed: false, Reason: "request_id_required"}, fmt.Errorf("request id required")
	}

	limit, err := resolveMiniMaxText5hLimit(account)
	if err != nil {
		return &MiniMaxQuotaDecision{Allowed: false, Reason: "invalid_quota_limit"}, err
	}

	if shouldBlockByFreshMiniMaxOfficialRemains(account) {
		return &MiniMaxQuotaDecision{Allowed: false, Used: limit, Limit: limit, Reason: "official_remains_exhausted"}, nil
	}

	allowed, used, err := s.cache.ReserveTextRequest(ctx, account.ID, requestID, limit, MiniMaxTokenPlanTextWindowSeconds)
	if err != nil {
		return &MiniMaxQuotaDecision{Allowed: false, Used: used, Limit: limit, Reason: "quota_cache_error"}, err
	}
	if !allowed {
		return &MiniMaxQuotaDecision{Allowed: false, Used: used, Limit: limit, Reason: "quota_exhausted"}, nil
	}

	return &MiniMaxQuotaDecision{Allowed: true, Used: used, Limit: limit}, nil
}

func (s *MiniMaxQuotaService) CalibrateTextRequests(ctx context.Context, accountID int64, targetUsed int64) (localUsed int64, syntheticAdded int64, syntheticRemoved int64, err error) {
	if s == nil || s.cache == nil {
		return 0, 0, 0, fmt.Errorf("minimax quota cache unavailable")
	}
	if targetUsed < 0 {
		targetUsed = 0
	}
	return s.cache.CalibrateTextRequests(ctx, accountID, targetUsed, MiniMaxTokenPlanTextWindowSeconds)
}

func (s *MiniMaxQuotaService) RollbackTextRequest(ctx context.Context, accountID int64, requestID string) error {
	if s == nil || s.cache == nil {
		return nil
	}

	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}

	return s.cache.RollbackTextRequest(ctx, accountID, requestID)
}

func resolveMiniMaxText5hLimit(account *Account) (int64, error) {
	if account == nil || account.Extra == nil {
		return MiniMaxTokenPlanDefaultText5hLimit, nil
	}
	value, ok := account.Extra["text_5h_limit"]
	if !ok {
		return MiniMaxTokenPlanDefaultText5hLimit, nil
	}
	limit, ok := miniMaxQuotaLimitFromAny(value)
	if !ok || limit <= 0 {
		return 0, fmt.Errorf("invalid minimax text_5h_limit")
	}
	return limit, nil
}

func miniMaxQuotaLimitFromAny(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case float64:
		if v < 1 || v >= miniMaxQuotaMaxInt64ExclusiveFloat || math.Trunc(v) != v {
			return 0, false
		}
		return int64(v), true
	case json.Number:
		i, err := v.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

func shouldBlockByFreshMiniMaxOfficialRemains(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	remaining, ok := miniMaxQuotaNonNegativeIntFromAny(account.Extra["minimax_text_5h_remaining"])
	if !ok || remaining > 0 {
		return false
	}
	syncedAt, ok := miniMaxExtraTime(account.Extra["minimax_remains_synced_at"])
	if !ok {
		return false
	}
	return time.Since(syncedAt) <= time.Duration(miniMaxOfficialRemainsFreshSeconds)*time.Second
}

func miniMaxQuotaNonNegativeIntFromAny(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), v >= 0
	case int64:
		return v, v >= 0
	case int32:
		return int64(v), v >= 0
	case float64:
		if v < 0 || v >= miniMaxQuotaMaxInt64ExclusiveFloat || math.Trunc(v) != v {
			return 0, false
		}
		return int64(v), true
	case json.Number:
		i, err := v.Int64()
		return i, err == nil && i >= 0
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return i, err == nil && i >= 0
	default:
		return 0, false
	}
}

func miniMaxExtraTime(value any) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		return v, !v.IsZero()
	case string:
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
		return t, err == nil
	default:
		return time.Time{}, false
	}
}
