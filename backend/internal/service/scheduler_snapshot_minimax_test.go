package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestMiniMaxSchedulingDefaultBucketsIncludePlatform(t *testing.T) {
	svc := &SchedulerSnapshotService{
		cfg: &config.Config{RunMode: config.RunModeSimple},
	}

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets error = %v", err)
	}

	if !schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformMiniMax, Mode: SchedulerModeSingle}) {
		t.Fatalf("expected MiniMax single scheduler bucket, got %+v", buckets)
	}
	if !schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformMiniMax, Mode: SchedulerModeForced}) {
		t.Fatalf("expected MiniMax forced scheduler bucket, got %+v", buckets)
	}
	if schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformMiniMax, Mode: SchedulerModeMixed}) {
		t.Fatalf("MiniMax must not use mixed scheduler bucket")
	}
}

func TestGLMSchedulingDefaultBucketsIncludePlatform(t *testing.T) {
	svc := &SchedulerSnapshotService{
		cfg: &config.Config{RunMode: config.RunModeSimple},
	}

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets error = %v", err)
	}

	if !schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformGLM, Mode: SchedulerModeSingle}) {
		t.Fatalf("expected GLM single scheduler bucket, got %+v", buckets)
	}
	if !schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformGLM, Mode: SchedulerModeForced}) {
		t.Fatalf("expected GLM forced scheduler bucket, got %+v", buckets)
	}
	if schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformGLM, Mode: SchedulerModeMixed}) {
		t.Fatalf("GLM must not use mixed scheduler bucket")
	}
}

func TestDeepSeekSchedulingDefaultBucketsIncludePlatform(t *testing.T) {
	svc := &SchedulerSnapshotService{
		cfg: &config.Config{RunMode: config.RunModeSimple},
	}

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets error = %v", err)
	}

	if !schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformDeepSeek, Mode: SchedulerModeSingle}) {
		t.Fatalf("expected DeepSeek single scheduler bucket, got %+v", buckets)
	}
	if !schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformDeepSeek, Mode: SchedulerModeForced}) {
		t.Fatalf("expected DeepSeek forced scheduler bucket, got %+v", buckets)
	}
	if schedulerBucketExists(buckets, SchedulerBucket{GroupID: 0, Platform: PlatformDeepSeek, Mode: SchedulerModeMixed}) {
		t.Fatalf("DeepSeek must not use mixed scheduler bucket")
	}
}

func schedulerBucketExists(buckets []SchedulerBucket, want SchedulerBucket) bool {
	for _, bucket := range buckets {
		if bucket == want {
			return true
		}
	}
	return false
}
