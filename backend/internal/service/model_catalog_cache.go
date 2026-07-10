package service

import (
	"sync"
	"time"
)

type catalogCacheState uint8

const (
	catalogCacheMiss catalogCacheState = iota
	catalogCacheFresh
	catalogCacheStale
)

type modelCatalogCacheEntry struct {
	models      []string
	fetchedAt   time.Time
	lastErrorAt time.Time
}

type modelCatalogCache struct {
	mu          sync.RWMutex
	entries     map[int64]modelCatalogCacheEntry
	generations map[int64]uint64
}

func newModelCatalogCache() *modelCatalogCache {
	return &modelCatalogCache{
		entries:     make(map[int64]modelCatalogCacheEntry),
		generations: make(map[int64]uint64),
	}
}

func (c *modelCatalogCache) load(accountID int64) (modelCatalogCacheEntry, bool) {
	if c == nil {
		return modelCatalogCacheEntry{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[accountID]
	entry.models = cloneStringSlice(entry.models)
	return entry, ok
}

func (c *modelCatalogCache) state(accountID int64, now time.Time, freshTTL, staleTTL time.Duration) catalogCacheState {
	_, state := c.loadState(accountID, now, freshTTL, staleTTL)
	return state
}

func (c *modelCatalogCache) loadState(accountID int64, now time.Time, freshTTL, staleTTL time.Duration) (modelCatalogCacheEntry, catalogCacheState) {
	if c == nil {
		return modelCatalogCacheEntry{}, catalogCacheMiss
	}
	c.mu.RLock()
	entry, ok := c.entries[accountID]
	entry.models = cloneStringSlice(entry.models)
	c.mu.RUnlock()
	if !ok || entry.fetchedAt.IsZero() || len(entry.models) == 0 {
		return entry, catalogCacheMiss
	}
	age := now.Sub(entry.fetchedAt)
	if age < 0 || age < freshTTL {
		return entry, catalogCacheFresh
	}
	if age < staleTTL {
		return entry, catalogCacheStale
	}
	return entry, catalogCacheMiss
}

func (c *modelCatalogCache) storeSuccess(accountID int64, models []string, fetchedAt time.Time) {
	if c == nil || len(models) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[int64]modelCatalogCacheEntry)
	}
	c.entries[accountID] = modelCatalogCacheEntry{
		models:    cloneStringSlice(models),
		fetchedAt: fetchedAt,
	}
}

func (c *modelCatalogCache) storeSuccessForGeneration(accountID int64, models []string, fetchedAt time.Time, generation uint64) bool {
	if c == nil || len(models) == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generations[accountID] != generation {
		return false
	}
	if c.entries == nil {
		c.entries = make(map[int64]modelCatalogCacheEntry)
	}
	c.entries[accountID] = modelCatalogCacheEntry{
		models:    cloneStringSlice(models),
		fetchedAt: fetchedAt,
	}
	return true
}

func (c *modelCatalogCache) storeFailureForGeneration(accountID int64, failedAt time.Time, generation uint64) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generations[accountID] != generation {
		return false
	}
	if c.entries == nil {
		c.entries = make(map[int64]modelCatalogCacheEntry)
	}
	entry := c.entries[accountID]
	entry.lastErrorAt = failedAt
	c.entries[accountID] = entry
	return true
}

func (c *modelCatalogCache) inFailureBackoff(accountID int64, now time.Time, backoff time.Duration) bool {
	if backoff <= 0 {
		return false
	}
	entry, ok := c.load(accountID)
	if !ok || entry.lastErrorAt.IsZero() {
		return false
	}
	age := now.Sub(entry.lastErrorAt)
	return age < 0 || age < backoff
}

func (c *modelCatalogCache) generation(accountID int64) uint64 {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.generations[accountID]
}

func (c *modelCatalogCache) invalidate(accountID int64) uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generations == nil {
		c.generations = make(map[int64]uint64)
	}
	oldGeneration := c.generations[accountID]
	c.generations[accountID] = oldGeneration + 1
	delete(c.entries, accountID)
	return oldGeneration
}
