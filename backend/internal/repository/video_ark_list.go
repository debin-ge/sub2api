package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Keep this projection identical to ProjectArkVideoStatus and migration 279.
// CASE preserves the public queued fallback for legacy unknown states too.
const arkVideoStatusSQL = `CASE
 WHEN generation_state = 'completed' AND billing_state = 'captured' THEN 'succeeded'
 WHEN generation_state IN ('in_progress', 'completed') THEN 'running'
 WHEN generation_state = 'failed' THEN 'failed'
 WHEN generation_state = 'cancelled' THEN 'cancelled'
 WHEN generation_state = 'expired' THEN 'expired'
 ELSE 'queued' END`

const arkVideoProviderSQL = "provider IN ('bytedance', 'fal', 'wavespeed', 'runware', 'flova')"

func (q *videoTaskQueue) AllowVideoTaskList(ctx context.Context, userID int64) (bool, error) {
	if q == nil || q.rdb == nil || userID <= 0 {
		return false, errors.New("video list limiter is unavailable")
	}
	result, err := middleware.NewRateLimiter(q.rdb).Allow(ctx,
		"video:ark:list:user:"+strconv.FormatInt(userID, 10), service.ArkVideoListRequestsPerMinute, time.Minute)
	return result.Allowed, err
}
