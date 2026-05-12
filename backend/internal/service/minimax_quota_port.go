package service

import "context"

type MiniMaxQuotaCache interface {
	ReserveTextRequest(ctx context.Context, accountID int64, requestID string, limit int64, windowSeconds int64) (allowed bool, used int64, err error)
	RollbackTextRequest(ctx context.Context, accountID int64, requestID string) error
	CountTextRequests(ctx context.Context, accountID int64, windowSeconds int64) (int64, error)
	CalibrateTextRequests(ctx context.Context, accountID int64, targetUsed int64, windowSeconds int64) (localUsed int64, syntheticAdded int64, syntheticRemoved int64, err error)
}
