package handler

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

const usageRecordSyncFallbackTimeout = 10 * time.Second

// submitUsageRecordTaskWithFallback runs billing tasks inline so a successful
// upstream request reaches the durable stage-0 billing outbox before the
// handler returns. The process-local pool is consulted only as an overflow
// guard: if it is saturated, an explicitly configured drop/sample policy may
// still discard the task; the default sync policy remains lossless.
//
// Forwarders call this only after the upstream response/stream has completed.
// Inline execution therefore does not delay stream chunks; it only keeps the
// handler alive until the billing task has reached its durable path (or the
// bounded task context expires).
func submitUsageRecordTaskWithFallback(
	parent context.Context,
	pool *service.UsageRecordWorkerPool,
	component string,
	task service.UsageRecordTask,
) {
	if task == nil {
		return
	}

	if pool != nil {
		switch mode := pool.InspectInlineAdmission(); mode {
		case service.UsageRecordSubmitModeDropped:
			logger.L().With(
				zap.String("component", normalizedUsageRecordComponent(component)),
				zap.String("submit_mode", mode.String()),
			).Warn("usage_record.task_dropped_by_explicit_overflow_policy")
			return
		case service.UsageRecordSubmitModeDroppedStopped:
			logger.L().With(
				zap.String("component", normalizedUsageRecordComponent(component)),
			).Warn("usage_record.task_stopped_sync_fallback")
		}
	}
	runUsageRecordTaskInline(parent, component, usageRecordSyncFallbackTimeout, task)
}

func runUsageRecordTaskInline(
	parent context.Context,
	component string,
	timeout time.Duration,
	task service.UsageRecordTask,
) {
	if task == nil {
		return
	}

	task = wrapUsageRecordTaskContext(parent, task)
	if timeout <= 0 {
		timeout = usageRecordSyncFallbackTimeout
	}

	// Deliberately start from Background rather than parent. Request
	// cancellation (including a client disconnect after receiving a stream)
	// must not prevent the durable stage-0 write. wrapUsageRecordTaskContext
	// copies only the stable request identifiers needed for idempotency.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", normalizedUsageRecordComponent(component)),
				zap.Any("panic", recovered),
			).Error("usage_record.task_panic_recovered")
		}
	}()
	task(ctx)
}

func normalizedUsageRecordComponent(component string) string {
	if component = strings.TrimSpace(component); component != "" {
		return component
	}
	return "handler.usage_record"
}
