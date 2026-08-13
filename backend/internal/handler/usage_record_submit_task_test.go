package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newUsageRecordTestPool(t *testing.T) *service.UsageRecordWorkerPool {
	t.Helper()
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             8,
		TaskTimeout:           time.Second,
		OverflowPolicy:        "drop",
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
	})
	t.Cleanup(pool.Stop)
	return pool
}

func newSaturatedDroppedUsageRecordTestPool(t *testing.T) *service.UsageRecordWorkerPool {
	t.Helper()
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        "drop",
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
	})

	started := make(chan struct{})
	release := make(chan struct{})
	require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-release
	}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocking worker task did not start")
	}
	require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {}))

	t.Cleanup(func() {
		close(release)
		pool.Stop()
	})
	return pool
}

func TestGatewayHandlerSubmitUsageRecordTask_WithPoolRunsInline(t *testing.T) {
	pool := newUsageRecordTestPool(t)
	h := &GatewayHandler{usageRecordWorkerPool: pool}

	started := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		h.submitUsageRecordTask(context.Background(), func(context.Context) {
			close(started)
			<-release
		})
		close(returned)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("ordinary token billing task did not start")
	}
	select {
	case <-returned:
		t.Fatal("ordinary token billing submit returned before the task completed")
	default:
	}
	require.Zero(t, pool.Stats().SubmittedTasks, "ordinary token billing must bypass the process-local queue")

	close(release)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("ordinary token billing submit did not return after the task completed")
	}
}

func TestGatewayHandlerSubmitUsageRecordTask_WithoutPoolSyncFallback(t *testing.T) {
	h := &GatewayHandler{}
	var called atomic.Bool

	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("expected deadline in fallback context")
		}
		called.Store(true)
	})

	require.True(t, called.Load())
}

func TestGatewayHandlerSubmitUsageRecordTask_NilTask(t *testing.T) {
	h := &GatewayHandler{}
	require.NotPanics(t, func() {
		h.submitUsageRecordTask(context.Background(), nil)
	})
}

func TestGatewayHandlerSubmitUsageRecordTask_WithoutPool_TaskPanicRecovered(t *testing.T) {
	h := &GatewayHandler{}
	var called atomic.Bool

	require.NotPanics(t, func() {
		h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
			panic("usage task panic")
		})
	})

	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		called.Store(true)
	})
	require.True(t, called.Load(), "panic 后后续任务应仍可执行")
}

func TestGatewayHandlerSubmitUsageRecordTask_WithPool_TaskPanicRecoveredInline(t *testing.T) {
	pool := newUsageRecordTestPool(t)
	h := &GatewayHandler{usageRecordWorkerPool: pool}

	require.NotPanics(t, func() {
		h.submitUsageRecordTask(context.Background(), func(context.Context) {
			panic("inline usage task panic")
		})
	})

	require.Zero(t, pool.Stats().SubmittedTasks, "panicking billing task must still bypass the process-local queue")
}

func TestGatewayHandlerSubmitUsageRecordTask_SaturatedPoolDropPolicyDrops(t *testing.T) {
	pool := newSaturatedDroppedUsageRecordTestPool(t)
	h := &GatewayHandler{usageRecordWorkerPool: pool}
	var called atomic.Bool
	before := pool.Stats()

	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("expected deadline in inline billing context")
		}
		called.Store(true)
	})

	after := pool.Stats()
	require.False(t, called.Load(), "explicit drop policy must remain effective when the queue is saturated")
	require.Equal(t, before.SubmittedTasks, after.SubmittedTasks, "billing task must not be submitted to the saturated pool")
	require.Greater(t, after.DroppedQueueFull, before.DroppedQueueFull, "explicit overflow drops must remain observable")
}

func TestGatewayHandlerSubmitGatewayUsageRecordTask_ImageRunsInlineBeforeReturn(t *testing.T) {
	pool := newUsageRecordTestPool(t)
	h := &GatewayHandler{usageRecordWorkerPool: pool}
	var called atomic.Bool

	h.submitGatewayUsageRecordTask(
		context.Background(),
		&service.ForwardResult{ImageCount: 1},
		func(ctx context.Context) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("expected deadline in mandatory media billing context")
			}
			called.Store(true)
		},
	)

	require.True(t, called.Load(), "generic media billing must complete before submit returns")
	require.Zero(t, pool.Stats().SubmittedTasks, "generic media billing must bypass the process-local queue")
}

func TestGatewayHandlerSubmitGatewayUsageRecordTask_TextBypassesPool(t *testing.T) {
	pool := newUsageRecordTestPool(t)
	h := &GatewayHandler{usageRecordWorkerPool: pool}
	var called atomic.Bool

	h.submitGatewayUsageRecordTask(
		context.Background(),
		&service.ForwardResult{},
		func(context.Context) { called.Store(true) },
	)

	require.True(t, called.Load(), "generic text billing task must complete before submit returns")
	require.Zero(t, pool.Stats().SubmittedTasks, "generic text billing must bypass the process-local queue")
}

func TestOpenAIGatewayHandlerSubmitUsageRecordTask_WithPoolRunsInline(t *testing.T) {
	pool := newUsageRecordTestPool(t)
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}

	var called atomic.Bool
	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("expected bounded inline billing context")
		}
		called.Store(true)
	})

	require.True(t, called.Load(), "ordinary OpenAI token billing must finish before submit returns")
	require.Zero(t, pool.Stats().SubmittedTasks, "ordinary OpenAI token billing must bypass the process-local queue")
}

func TestSpecializedGatewayUsageRecordTasks_BypassPool(t *testing.T) {
	type submitFunc func(context.Context, service.UsageRecordTask)
	tests := []struct {
		name    string
		factory func(*service.UsageRecordWorkerPool) submitFunc
	}{
		{
			name: "deepseek",
			factory: func(pool *service.UsageRecordWorkerPool) submitFunc {
				return (&DeepSeekGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
			},
		},
		{
			name: "glm",
			factory: func(pool *service.UsageRecordWorkerPool) submitFunc {
				return (&GLMGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
			},
		},
		{
			name: "kimi",
			factory: func(pool *service.UsageRecordWorkerPool) submitFunc {
				return (&KimiGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
			},
		},
		{
			name: "minimax",
			factory: func(pool *service.UsageRecordWorkerPool) submitFunc {
				return (&MiniMaxGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
			},
		},
		{
			name: "windsurf",
			factory: func(pool *service.UsageRecordWorkerPool) submitFunc {
				return (&WindsurfGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
			},
		},
		{
			name: "opencode",
			factory: func(pool *service.UsageRecordWorkerPool) submitFunc {
				return (&OpenCodeGatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := newUsageRecordTestPool(t)
			var called atomic.Bool
			relayMetadata := internalrelay.Metadata{
				Version:         "v1",
				AccountID:       42,
				IssuedAt:        time.Unix(1_700_000_000, 0).UTC(),
				ParentRequestID: "client:outer-request-123",
			}
			parent := context.WithValue(context.Background(), ctxkey.InternalRelay, relayMetadata)

			tt.factory(pool)(parent, func(ctx context.Context) {
				require.NotNil(t, ctx)
				require.Equal(t, relayMetadata, ctx.Value(ctxkey.InternalRelay))
				called.Store(true)
			})

			require.True(t, called.Load(), "ordinary token billing must complete inline")
			require.Zero(t, pool.Stats().SubmittedTasks, "ordinary token billing must not enter the process-local pool")
		})
	}
}

func TestOpenAIGatewayHandlerSubmitUsageRecordTask_WithoutPoolSyncFallback(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	var called atomic.Bool

	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("expected deadline in fallback context")
		}
		called.Store(true)
	})

	require.True(t, called.Load())
}

func TestOpenAIGatewayHandlerSubmitUsageRecordTask_NilTask(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	require.NotPanics(t, func() {
		h.submitUsageRecordTask(context.Background(), nil)
	})
}

func TestOpenAIGatewayHandlerSubmitUsageRecordTask_WithoutPool_TaskPanicRecovered(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	var called atomic.Bool

	require.NotPanics(t, func() {
		h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
			panic("usage task panic")
		})
	})

	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		called.Store(true)
	})
	require.True(t, called.Load(), "panic 后后续任务应仍可执行")
}

func TestOpenAIGatewayHandlerSubmitOpenAIUsageRecordTask_TextDropPolicyDropsWhenSaturated(t *testing.T) {
	pool := newSaturatedDroppedUsageRecordTestPool(t)
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}
	var called atomic.Bool
	before := pool.Stats()

	h.submitOpenAIUsageRecordTask(
		context.Background(),
		&service.OpenAIForwardResult{ImageCount: 0},
		func(ctx context.Context) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("expected deadline in inline billing context")
			}
			called.Store(true)
		},
	)

	after := pool.Stats()
	require.False(t, called.Load(), "explicit drop policy must remain effective when the queue is saturated")
	require.Equal(t, before.SubmittedTasks, after.SubmittedTasks, "OpenAI text billing must not enter the saturated pool")
	require.Greater(t, after.DroppedQueueFull, before.DroppedQueueFull, "explicit overflow drops must remain observable")
}

func TestUsageRecordSubmitTask_StoppedPoolIsNotConsulted(t *testing.T) {
	t.Run("generic", func(t *testing.T) {
		pool := newUsageRecordTestPool(t)
		pool.Stop()
		h := &GatewayHandler{usageRecordWorkerPool: pool}
		var called atomic.Bool

		h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("expected deadline in stopped-pool fallback context")
			}
			called.Store(true)
		})

		require.True(t, called.Load(), "generic billing task must not be lost after pool stop")
		require.Zero(t, pool.Stats().DroppedPoolStopped, "inline billing must not consult the stopped pool")
	})

	t.Run("openai_text", func(t *testing.T) {
		pool := newUsageRecordTestPool(t)
		pool.Stop()
		h := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}
		var called atomic.Bool

		h.submitOpenAIUsageRecordTask(
			context.Background(),
			&service.OpenAIForwardResult{ImageCount: 0},
			func(ctx context.Context) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("expected deadline in stopped-pool fallback context")
				}
				called.Store(true)
			},
		)

		require.True(t, called.Load(), "OpenAI text billing task must not be lost after pool stop")
		require.Zero(t, pool.Stats().DroppedPoolStopped, "inline billing must not consult the stopped pool")
	})
}

func TestRunUsageRecordTaskInline_DetachesCanceledParentAndCopiesRequestIDs(t *testing.T) {
	type contextKey string
	const unrelatedKey contextKey = "unrelated"

	parent := context.WithValue(context.Background(), unrelatedKey, "must-not-copy")
	parent = context.WithValue(parent, ctxkey.ClientRequestID, "client-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "request-456")
	relayMetadata := internalrelay.Metadata{
		Version:         "v1",
		AccountID:       42,
		IssuedAt:        time.Unix(1_700_000_000, 0).UTC(),
		ParentRequestID: "client:outer-request-123",
	}
	parent = context.WithValue(parent, ctxkey.InternalRelay, relayMetadata)
	parent, cancel := context.WithCancel(parent)
	cancel()

	var called atomic.Bool
	runUsageRecordTaskInline(parent, "test.usage", time.Second, func(ctx context.Context) {
		require.NoError(t, ctx.Err(), "request cancellation must not cancel stage-0 billing")
		require.Equal(t, "client-123", ctx.Value(ctxkey.ClientRequestID))
		require.Equal(t, "request-456", ctx.Value(ctxkey.RequestID))
		require.Equal(t, relayMetadata, ctx.Value(ctxkey.InternalRelay))
		require.Nil(t, ctx.Value(unrelatedKey), "only stable billing metadata should be copied")
		called.Store(true)
	})

	require.True(t, called.Load())
}

func TestRunUsageRecordTaskInline_SignalsDeadlineToCooperativeTask(t *testing.T) {
	var taskErr error
	started := time.Now()

	runUsageRecordTaskInline(context.Background(), "test.timeout", 20*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
		taskErr = ctx.Err()
	})

	require.ErrorIs(t, taskErr, context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second, "test timeout should stop a cooperative billing task")
}

func TestOpenAIGatewayHandlerSubmitMandatoryUsageRecordTask_DroppedTaskSyncFallback(t *testing.T) {
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        "drop",
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
	})
	t.Cleanup(pool.Stop)
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}

	block := make(chan struct{})
	release := make(chan struct{})
	pool.Submit(func(ctx context.Context) {
		close(block)
		<-release
	})
	<-block
	pool.Submit(func(ctx context.Context) {})

	var called atomic.Bool
	h.submitMandatoryUsageRecordTask(context.Background(), func(ctx context.Context) {
		called.Store(true)
	})
	close(release)

	require.True(t, called.Load(), "mandatory usage task must run synchronously when async submit is dropped")
}

func TestOpenAIGatewayHandlerSubmitOpenAIUsageRecordTask_ImageResultUsesMandatoryFallback(t *testing.T) {
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        "drop",
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
	})
	t.Cleanup(pool.Stop)
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}

	block := make(chan struct{})
	release := make(chan struct{})
	pool.Submit(func(ctx context.Context) {
		close(block)
		<-release
	})
	<-block
	pool.Submit(func(ctx context.Context) {})

	var called atomic.Bool
	h.submitOpenAIUsageRecordTask(context.Background(), &service.OpenAIForwardResult{ImageCount: 1}, func(ctx context.Context) {
		called.Store(true)
	})
	close(release)

	require.True(t, called.Load(), "image usage task must be mandatory when async submit is dropped")
}

func TestOpenAIGatewayHandlerSubmitOpenAIUsageRecordTask_SearchCountUsesMandatoryFallback(t *testing.T) {
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        "drop",
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
	})
	t.Cleanup(pool.Stop)
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: pool}

	block := make(chan struct{})
	release := make(chan struct{})
	pool.Submit(func(ctx context.Context) {
		close(block)
		<-release
	})
	<-block
	pool.Submit(func(ctx context.Context) {})

	var called atomic.Bool
	h.submitOpenAIUsageRecordTask(context.Background(), &service.OpenAIForwardResult{SearchCount: 3}, func(ctx context.Context) {
		called.Store(true)
	})
	close(release)

	require.True(t, called.Load(), "search surcharge usage task must be mandatory when async submit is dropped")
}
