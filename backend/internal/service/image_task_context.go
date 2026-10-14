package service

import (
	"context"
	"time"
)

type imageTaskExecutionDeadlineKey struct{}

// WithImageTaskExecutionDeadline marks a server-owned absolute task deadline.
// Gateway calls may detach from a client cancellation, but must not discard
// this admission deadline and outlive the corresponding shared pending slot.
// Only the async task handler sets this marker; request headers cannot set it.
func WithImageTaskExecutionDeadline(ctx context.Context, deadline time.Time) context.Context {
	return context.WithValue(ctx, imageTaskExecutionDeadlineKey{}, deadline)
}

func imageTaskExecutionDeadline(ctx context.Context) (time.Time, bool) {
	deadline, ok := ctx.Value(imageTaskExecutionDeadlineKey{}).(time.Time)
	return deadline, ok && !deadline.IsZero()
}
