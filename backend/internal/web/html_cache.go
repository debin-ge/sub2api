//go:build embed

package web

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// HTMLCache manages the cached index.html with injected settings
type HTMLCache struct {
	mu              sync.RWMutex
	cachedHTML      []byte
	etag            string
	baseHTMLHash    string // Hash of the original index.html (immutable after build)
	settingsVersion uint64 // Incremented when settings change
	stale           bool
}

// CachedHTML represents the cache state
type CachedHTML struct {
	Content []byte
	ETag    string
	Stale   bool
}

// NewHTMLCache creates a new HTML cache instance
func NewHTMLCache() *HTMLCache {
	return &HTMLCache{}
}

// SetBaseHTML initializes the cache with the base HTML template
func (c *HTMLCache) SetBaseHTML(baseHTML []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := sha256.Sum256(baseHTML)
	c.baseHTMLHash = hex.EncodeToString(hash[:8]) // First 8 bytes for brevity
}

// Invalidate marks the cache as stale
func (c *HTMLCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.settingsVersion++
	if c.cachedHTML != nil {
		c.stale = true
	}
}

// Get returns the last successfully rendered HTML, including stale content.
func (c *HTMLCache) Get() *CachedHTML {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cachedHTML == nil {
		return nil
	}
	return &CachedHTML{
		Content: c.cachedHTML,
		ETag:    c.etag,
		Stale:   c.stale,
	}
}

// Version returns the current settings generation.
func (c *HTMLCache) Version() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.settingsVersion
}

// Set updates the cache with new rendered HTML
func (c *HTMLCache) Set(html []byte, settingsJSON []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.set(html, settingsJSON)
}

// SetIfVersion updates the cache only if settings were not invalidated while
// the rendered HTML was being prepared.
func (c *HTMLCache) SetIfVersion(html []byte, settingsJSON []byte, version uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.settingsVersion != version {
		return false
	}

	c.set(html, settingsJSON)
	return true
}

func (c *HTMLCache) set(html []byte, settingsJSON []byte) {
	c.cachedHTML = html
	c.etag = c.generateETag(settingsJSON)
	c.stale = false
}

// generateETag creates an ETag from base HTML hash + settings hash
func (c *HTMLCache) generateETag(settingsJSON []byte) string {
	settingsHash := sha256.Sum256(settingsJSON)
	return `"` + c.baseHTMLHash + "-" + hex.EncodeToString(settingsHash[:8]) + `"`
}
