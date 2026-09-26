package service

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const invalidAuthAbuseShardCount = 16

// invalidAuthAbuseAllocateAttempts 容量打满时为新 key 腾出名额的「跨分片驱逐 + 重试」
// 次数上限；耗尽后直接接纳（tracked 短暂超出 capacity），绝不拒绝跟踪新 key。
const invalidAuthAbuseAllocateAttempts = invalidAuthAbuseShardCount * 4

type invalidAuthAbuseEntry struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
	// lastSeen 最近一次 record / check 命中的时间；容量打满时驱逐 lastSeen 最早的条目。
	lastSeen time.Time
}

type invalidAuthAbuseShard struct {
	mu      sync.Mutex
	entries map[string]*invalidAuthAbuseEntry
}

// invalidAuthAbuseLimiter 按客户端 key（通常是安全客户端 IP）统计无效 API Key 认证失败，
// 达到阈值后短时封禁该 key。
//
// 容量语义：tracked 条目数以 capacity 为上限；打满后接纳新 key 时驱逐 lastSeen 最早的
// 旧条目（LRU 近似），而不是像旧版那样把所有未跟踪的客户端丢进一个全局 overflow 桶——
// 那会让攻击者只要撑满容量，就能让所有陌生客户端一起被拒绝（SEC-004）。
type invalidAuthAbuseLimiter struct {
	threshold int
	window    time.Duration
	block     time.Duration
	capacity  int64
	shards    [invalidAuthAbuseShardCount]invalidAuthAbuseShard
	now       func() time.Time

	tracked  atomic.Int64
	recorded atomic.Uint64
	blocked  atomic.Uint64
	rejected atomic.Uint64
	expired  atomic.Uint64
	// overflowed 统计容量打满后为接纳新 key 而驱逐旧条目的次数（纯指标）。
	overflowed    atomic.Uint64
	cleanupNext   atomic.Int64
	cleanupCursor atomic.Uint32
}

type InvalidAuthAbuseHealth struct {
	Enabled  bool   `json:"enabled"`
	Tracked  int64  `json:"tracked"`
	Capacity int64  `json:"capacity"`
	Recorded uint64 `json:"recorded"`
	Blocks   uint64 `json:"blocks"`
	Rejected uint64 `json:"rejected"`
	Expired  uint64 `json:"expired"`
	// Overflowed 容量打满后驱逐旧条目以接纳新 key 的次数。
	Overflowed uint64 `json:"overflowed"`
	// GlobalBlocked 历史字段：旧版容量打满后会全局封禁未跟踪客户端并在此计数。
	// 全局封禁已移除，该值恒为 0；保留字段名以兼容既有监控面板。
	GlobalBlocked uint64 `json:"global_blocked"`
}

func newInvalidAuthAbuseLimiter(cfg *config.Config) *invalidAuthAbuseLimiter {
	if cfg == nil || !cfg.APIKeyAuth.InvalidAbuse.Enabled {
		return nil
	}
	c := cfg.APIKeyAuth.InvalidAbuse
	if c.Threshold <= 0 || c.WindowSeconds <= 0 || c.BlockSeconds <= 0 || c.Capacity <= 0 {
		return nil
	}
	l := &invalidAuthAbuseLimiter{
		threshold: c.Threshold,
		window:    time.Duration(c.WindowSeconds) * time.Second,
		block:     time.Duration(c.BlockSeconds) * time.Second,
		capacity:  int64(c.Capacity),
		now:       time.Now,
	}
	for i := range l.shards {
		l.shards[i].entries = make(map[string]*invalidAuthAbuseEntry)
	}
	return l
}

func (s *APIKeyService) CheckInvalidAuthAbuse(clientKey string) (time.Duration, bool) {
	if s == nil || s.invalidAuthAbuse == nil {
		return 0, false
	}
	return s.invalidAuthAbuse.check(clientKey)
}

func (s *APIKeyService) RecordInvalidAuthFailure(clientKey string) {
	if s == nil || s.invalidAuthAbuse == nil {
		return
	}
	s.invalidAuthAbuse.record(clientKey)
}

func (s *APIKeyService) InvalidAuthAbuseHealth() InvalidAuthAbuseHealth {
	if s == nil || s.invalidAuthAbuse == nil {
		return InvalidAuthAbuseHealth{}
	}
	return s.invalidAuthAbuse.health()
}

// check 返回 clientKey 当前是否处于封禁中。未跟踪的 key 永不封禁。
func (l *invalidAuthAbuseLimiter) check(clientKey string) (time.Duration, bool) {
	if l == nil || clientKey == "" {
		return 0, false
	}
	now := l.now()
	l.maybeCleanupAtCapacity(now)
	shard := l.shard(clientKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	entry := l.liveEntryLocked(shard, clientKey, now)
	if entry == nil {
		return 0, false
	}
	entry.lastSeen = now
	if entry.blockedUntil.After(now) {
		l.rejected.Add(1)
		return entry.blockedUntil.Sub(now), true
	}
	return 0, false
}

func (l *invalidAuthAbuseLimiter) record(clientKey string) {
	if l == nil || clientKey == "" {
		return
	}
	l.recorded.Add(1)
	now := l.now()
	l.maybeCleanupAtCapacity(now)
	shard := l.shard(clientKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	entry := l.liveEntryLocked(shard, clientKey, now)
	if entry == nil {
		entry = l.allocateEntryLocked(shard, clientKey, now)
	}
	entry.lastSeen = now
	if entry.blockedUntil.After(now) {
		return
	}
	if entry.windowStart.After(now) || !now.Before(entry.windowStart.Add(l.window)) {
		entry.windowStart = now
		entry.failures = 0
	}
	entry.failures++
	if entry.failures >= l.threshold {
		entry.failures = 0
		entry.blockedUntil = now.Add(l.block)
		entry.windowStart = entry.blockedUntil
		l.blocked.Add(1)
	}
}

// liveEntryLocked 返回 clientKey 的未过期条目；过期条目就地删除并返回 nil。调用方持有 shard.mu。
func (l *invalidAuthAbuseLimiter) liveEntryLocked(shard *invalidAuthAbuseShard, clientKey string, now time.Time) *invalidAuthAbuseEntry {
	entry := shard.entries[clientKey]
	if entry != nil && l.entryExpired(entry, now) {
		delete(shard.entries, clientKey)
		l.tracked.Add(-1)
		l.expired.Add(1)
		return nil
	}
	return entry
}

// allocateEntryLocked 为尚未跟踪的 clientKey 创建条目并返回。调用方持有 shard.mu，
// 返回时同样持有（跨分片驱逐期间会短暂释放）。新 key 永远会被接纳：
//   - 容量未满：CAS 预留名额。
//   - 容量已满且当前分片非空：驱逐当前分片 lastSeen 最早的条目，复用其名额（tracked 不变）。
//   - 容量已满且当前分片为空：释放本分片锁，从其它分片驱逐一个最旧条目后重试预留
//     （持有本分片锁时不得再锁其它分片，避免锁序死锁）。
//   - 极端竞争下重试耗尽：直接接纳，tracked 短暂超出 capacity（计数仍然准确）。
func (l *invalidAuthAbuseLimiter) allocateEntryLocked(shard *invalidAuthAbuseShard, clientKey string, now time.Time) *invalidAuthAbuseEntry {
	for attempt := 0; attempt < invalidAuthAbuseAllocateAttempts; attempt++ {
		if l.reserveEntry() {
			return l.insertLocked(shard, clientKey, now)
		}
		if l.evictOldestLocked(shard) {
			return l.insertLocked(shard, clientKey, now)
		}
		shard.mu.Unlock()
		l.evictOldestFromOtherShards(shard)
		shard.mu.Lock()
		if existing := l.liveEntryLocked(shard, clientKey, now); existing != nil {
			// 释放锁期间已有并发请求插入了同一个 key
			return existing
		}
	}
	l.tracked.Add(1)
	return l.insertLocked(shard, clientKey, now)
}

func (l *invalidAuthAbuseLimiter) insertLocked(shard *invalidAuthAbuseShard, clientKey string, now time.Time) *invalidAuthAbuseEntry {
	entry := &invalidAuthAbuseEntry{windowStart: now, lastSeen: now}
	shard.entries[clientKey] = entry
	return entry
}

// evictOldestLocked 删除 shard 中 lastSeen 最早的条目。不改动 tracked：名额由调用方复用
// 或显式释放。调用方持有 shard.mu。分片内线性扫描，分片规模 ≈ capacity/16，可接受。
func (l *invalidAuthAbuseLimiter) evictOldestLocked(shard *invalidAuthAbuseShard) bool {
	var oldestKey string
	var oldest *invalidAuthAbuseEntry
	for key, entry := range shard.entries {
		if oldest == nil || entry.lastSeen.Before(oldest.lastSeen) {
			oldestKey, oldest = key, entry
		}
	}
	if oldest == nil {
		return false
	}
	delete(shard.entries, oldestKey)
	l.overflowed.Add(1)
	return true
}

// evictOldestFromOtherShards 轮询其它分片，驱逐其中一个分片的最旧条目并释放名额
// （tracked 减一）。调用方不得持有任何分片锁。
func (l *invalidAuthAbuseLimiter) evictOldestFromOtherShards(current *invalidAuthAbuseShard) bool {
	start := l.cleanupCursor.Add(1)
	for i := uint32(0); i < invalidAuthAbuseShardCount; i++ {
		shard := &l.shards[(start+i)%invalidAuthAbuseShardCount]
		if shard == current {
			continue
		}
		shard.mu.Lock()
		evicted := l.evictOldestLocked(shard)
		shard.mu.Unlock()
		if evicted {
			l.tracked.Add(-1)
			return true
		}
	}
	return false
}

func (l *invalidAuthAbuseLimiter) reserveEntry() bool {
	for {
		current := l.tracked.Load()
		if current >= l.capacity {
			return false
		}
		if l.tracked.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (l *invalidAuthAbuseLimiter) maybeCleanupAtCapacity(now time.Time) {
	if l.tracked.Load() < l.capacity {
		return
	}
	nowUnixNano := now.UnixNano()
	for {
		next := l.cleanupNext.Load()
		if nowUnixNano < next {
			return
		}
		if l.cleanupNext.CompareAndSwap(next, now.Add(100*time.Millisecond).UnixNano()) {
			break
		}
	}
	index := l.cleanupCursor.Add(1) - 1
	shard := &l.shards[index%invalidAuthAbuseShardCount]
	shard.mu.Lock()
	for key, entry := range shard.entries {
		if l.entryExpired(entry, now) {
			delete(shard.entries, key)
			l.tracked.Add(-1)
			l.expired.Add(1)
		}
	}
	shard.mu.Unlock()
}

func (l *invalidAuthAbuseLimiter) entryExpired(entry *invalidAuthAbuseEntry, now time.Time) bool {
	return entry != nil && !entry.blockedUntil.After(now) && !entry.windowStart.After(now) && !now.Before(entry.windowStart.Add(l.window))
}

func (l *invalidAuthAbuseLimiter) shard(clientKey string) *invalidAuthAbuseShard {
	const fnvOffset32 = uint32(2166136261)
	const fnvPrime32 = uint32(16777619)
	hash := fnvOffset32
	for i := 0; i < len(clientKey); i++ {
		hash ^= uint32(clientKey[i])
		hash *= fnvPrime32
	}
	return &l.shards[hash%invalidAuthAbuseShardCount]
}

func (l *invalidAuthAbuseLimiter) health() InvalidAuthAbuseHealth {
	return InvalidAuthAbuseHealth{
		Enabled:       true,
		Tracked:       l.tracked.Load(),
		Capacity:      l.capacity,
		Recorded:      l.recorded.Load(),
		Blocks:        l.blocked.Load(),
		Rejected:      l.rejected.Load(),
		Expired:       l.expired.Load(),
		Overflowed:    l.overflowed.Load(),
		GlobalBlocked: 0,
	}
}
