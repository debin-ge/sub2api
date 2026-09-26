//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newInvalidAuthLimiterForTest(threshold, capacity int) *invalidAuthAbuseLimiter {
	cfg := &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{
		InvalidAbuse: config.InvalidAuthAbuseConfig{
			Enabled: true, Threshold: threshold, WindowSeconds: 60, BlockSeconds: 10, Capacity: capacity,
		},
	}}
	return newInvalidAuthAbuseLimiter(cfg)
}

// trackedEntry 返回 key 当前的跟踪条目（测试专用，直接读分片）。
func (l *invalidAuthAbuseLimiter) trackedEntry(clientKey string) *invalidAuthAbuseEntry {
	shard := l.shard(clientKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	return shard.entries[clientKey]
}

// keysInSameShard 生成 n 个落在同一分片的 key，让驱逐顺序可预测。
func keysInSameShard(l *invalidAuthAbuseLimiter, n int) []string {
	groups := make(map[*invalidAuthAbuseShard][]string)
	for i := 0; ; i++ {
		key := fmt.Sprintf("shard-key-%d", i)
		shard := l.shard(key)
		groups[shard] = append(groups[shard], key)
		if len(groups[shard]) == n {
			return groups[shard]
		}
	}
}

func TestInvalidAuthAbuseLimiterBlocksAndExpires(t *testing.T) {
	l := newInvalidAuthLimiterForTest(3, 16)
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	for range 3 {
		l.record("203.0.113.1")
	}
	retry, blocked := l.check("203.0.113.1")
	require.True(t, blocked)
	require.Equal(t, 10*time.Second, retry)

	now = now.Add(11 * time.Second)
	_, blocked = l.check("203.0.113.1")
	require.False(t, blocked)
	now = now.Add(61 * time.Second)
	_, blocked = l.check("203.0.113.1")
	require.False(t, blocked)
	require.Zero(t, l.health().Tracked)
}

// SEC-004：容量打满后不再全局封禁未跟踪客户端，而是驱逐最旧条目接纳新 key。
func TestInvalidAuthAbuseLimiterCapacityEvictsOldestInsteadOfGlobalBlock(t *testing.T) {
	l := newInvalidAuthLimiterForTest(2, 2)
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for _, key := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3", "198.51.100.4"} {
		l.record(key)
		now = now.Add(time.Second)
	}

	// 从未出现过的 key 与已跟踪但未达阈值的 key 都不会被封禁
	_, blocked := l.check("198.51.100.5")
	require.False(t, blocked, "untracked key must never be blocked at capacity")
	_, blocked = l.check("198.51.100.4")
	require.False(t, blocked)

	health := l.health()
	require.Equal(t, int64(2), health.Tracked)
	require.Equal(t, uint64(2), health.Overflowed, "two admissions at capacity => two evictions")
	require.Zero(t, health.GlobalBlocked)
	require.Zero(t, health.Rejected)
	require.Zero(t, health.Blocks)
}

func TestInvalidAuthAbuseLimiterNewKeyNeverBlockedAtCapacityAndEvictedKeyRestarts(t *testing.T) {
	const threshold, capacity = 3, 4
	l := newInvalidAuthLimiterForTest(threshold, capacity)
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	tick := func() { now = now.Add(time.Second) }

	keys := []string{"src-a", "src-b", "src-c", "src-d"}
	for _, key := range keys {
		// 每个 key 累计 threshold-1 次失败：再失败一次就会被封禁
		for range threshold - 1 {
			l.record(key)
			tick()
		}
	}
	require.Equal(t, int64(capacity), l.health().Tracked)
	for _, key := range keys {
		_, blocked := l.check(key)
		require.False(t, blocked)
		tick()
	}

	// 容量已满：全新 key 的 check 永不封禁，record 也必须被接纳
	_, blocked := l.check("src-new")
	require.False(t, blocked)
	l.record("src-new")
	tick()
	require.Equal(t, int64(capacity), l.health().Tracked)
	require.NotNil(t, l.trackedEntry("src-new"))

	var evicted string
	for _, key := range keys {
		if l.trackedEntry(key) == nil {
			require.Empty(t, evicted, "exactly one old key must have been evicted")
			evicted = key
		}
	}
	require.NotEmpty(t, evicted)

	// 被驱逐的 key 重新从零计数：若计数未重置，再失败 1 次就会被封禁
	for range threshold - 1 {
		l.record(evicted)
		tick()
	}
	_, blocked = l.check(evicted)
	require.False(t, blocked, "evicted key must restart counting from zero")
	l.record(evicted)
	tick()
	retry, blocked := l.check(evicted)
	require.True(t, blocked)
	// 封禁从 record 时刻起算，check 已推进 1s，剩余 block-1s。
	require.Equal(t, 9*time.Second, retry)

	health := l.health()
	require.LessOrEqual(t, health.Tracked, int64(capacity))
	require.Equal(t, uint64(2), health.Overflowed, "src-new admission + evicted-key re-admission")
	require.Zero(t, health.GlobalBlocked)
	require.Equal(t, uint64(1), health.Blocks)
}

// check 命中会刷新 lastSeen：持续活跃（例如仍在被拒绝）的 key 不会被驱逐，
// 驱逐的是最久没有出现的那个。
func TestInvalidAuthAbuseLimiterEvictionPrefersLeastRecentlySeen(t *testing.T) {
	l := newInvalidAuthLimiterForTest(100, 2)
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	tick := func() { now = now.Add(time.Second) }

	keys := keysInSameShard(l, 3)
	older, newer, fresh := keys[0], keys[1], keys[2]
	l.record(older)
	tick()
	l.record(newer)
	tick()
	// older 被 check 命中，lastSeen 变为最新
	_, blocked := l.check(older)
	require.False(t, blocked)
	tick()

	l.record(fresh)
	require.NotNil(t, l.trackedEntry(fresh))
	require.NotNil(t, l.trackedEntry(older), "recently checked key must survive")
	require.Nil(t, l.trackedEntry(newer), "least recently seen key is evicted")
	require.Equal(t, int64(2), l.health().Tracked)
}

func TestInvalidAuthAbuseLimiterConcurrentCapacityIsBounded(t *testing.T) {
	const capacity = 64
	l := newInvalidAuthLimiterForTest(1000, capacity)
	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.record(fmt.Sprintf("198.51.100.%d", i))
		}(i)
	}
	wg.Wait()
	health := l.health()
	require.LessOrEqual(t, health.Tracked, int64(capacity))
	require.Equal(t, uint64(1000), health.Recorded)
	// 每次 record 都被接纳：超出容量的部分全部通过驱逐腾位
	require.Equal(t, uint64(1000), health.Overflowed+uint64(health.Tracked))
	require.Zero(t, health.GlobalBlocked)

	// tracked 与实际条目数一致
	var actual int64
	for i := range l.shards {
		l.shards[i].mu.Lock()
		actual += int64(len(l.shards[i].entries))
		l.shards[i].mu.Unlock()
	}
	require.Equal(t, actual, health.Tracked)
}

func TestInvalidAuthAbuseLimiterReclaimsExpiredCapacity(t *testing.T) {
	const capacity = 16
	l := newInvalidAuthLimiterForTest(100, capacity)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := range capacity {
		l.record(fmt.Sprintf("source-%d", i))
	}
	require.Equal(t, int64(capacity), l.health().Tracked)

	now = now.Add(61 * time.Second)
	for i := range invalidAuthAbuseShardCount {
		l.check(fmt.Sprintf("new-source-%d", i))
		now = now.Add(101 * time.Millisecond)
	}
	require.Less(t, l.health().Tracked, int64(capacity))
	l.record("fresh-source")
	require.LessOrEqual(t, l.health().Tracked, int64(capacity))
}

func TestInvalidAuthAbuseHealthKeepsJSONFieldNames(t *testing.T) {
	l := newInvalidAuthLimiterForTest(3, 16)
	raw, err := json.Marshal(l.health())
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	for _, name := range []string{"enabled", "tracked", "capacity", "recorded", "blocks", "rejected", "expired", "overflowed", "global_blocked"} {
		require.Contains(t, fields, name)
	}
	require.EqualValues(t, 0, fields["global_blocked"])
}
