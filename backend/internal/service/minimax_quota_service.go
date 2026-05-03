package service

import (
	"context"
	"fmt"
	"strings"
)

const MiniMaxTokenPlanTextWindowSeconds int64 = 5 * 60 * 60
const MiniMaxTokenPlanDefaultText5hLimit int64 = 4500

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
	if account == nil || account.Platform != PlatformMiniMax {
		return &MiniMaxQuotaDecision{Allowed: false, Reason: "invalid_minimax_account"}, fmt.Errorf("invalid minimax account")
	}
	if s == nil || s.cache == nil {
		return &MiniMaxQuotaDecision{Allowed: false, Reason: "quota_cache_unavailable"}, fmt.Errorf("minimax quota cache unavailable")
	}

	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return &MiniMaxQuotaDecision{Allowed: false, Reason: "request_id_required"}, fmt.Errorf("request id required")
	}

	limit := int64FromAny(account.Extra["text_5h_limit"])
	if limit <= 0 {
		limit = MiniMaxTokenPlanDefaultText5hLimit
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
