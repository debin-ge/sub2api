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

// submitUsageRecordTaskWithFallback runs every billing task inline, including
// ordinary token usage. The pool argument is retained for constructor/call-site
// compatibility only: accepting a task into the process-local pool is not a
// durability boundary, so a hard crash could otherwise lose a successful
// upstream request before RecordUsage reaches the durable billing outbox.
//
// Forwarders call this only after the upstream response/stream has completed.
// Inline execution therefore does not delay stream chunks; it only keeps the
// handler alive until the billing task has reached its durable path (or the
// bounded task context expires).
func submitUsageRecordTaskWithFallback(
	parent context.Context,
	_ *service.UsageRecordWorkerPool,
	component string,
	task service.UsageRecordTask,
) {
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
