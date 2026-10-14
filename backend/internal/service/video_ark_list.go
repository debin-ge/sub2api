package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ArkVideoListMaxOffset         = 4900
	ArkVideoListRequestsPerMinute = 60
	ArkVideoListQueryTimeout      = 3 * time.Second
)

var ErrVideoListRateLimited = infraerrors.New(http.StatusTooManyRequests, "VIDEO_LIST_RATE_LIMITED", "video task list rate limit exceeded; retry after one minute")

// VideoTaskListLimiter is shared by all API keys and instances for an owner.
// The Redis queue also supplies this independent admission capability.
type VideoTaskListLimiter interface {
	AllowVideoTaskList(context.Context, int64) (bool, error)
}

func ValidateVideoTaskListFilter(filter VideoTaskFilter) error {
	if !filter.ArkOnly {
		if filter.ArkStatus != "" || filter.Offset != 0 {
			return ErrVideoInvalidRequest
		}
		return nil
	}
	if filter.Offset < 0 || filter.Offset > ArkVideoListMaxOffset || filter.Limit < 1 || filter.Limit > 100 ||
		filter.After != "" || filter.Status != "" || filter.Operation != VideoOperationGenerate ||
		filter.Order != "desc" || len(filter.Model) > 256 || strings.TrimSpace(filter.Model) != filter.Model ||
		(filter.ArkStatus != "" && !IsArkVideoStatus(filter.ArkStatus)) {
		return ErrVideoInvalidRequest
	}
	return nil
}
