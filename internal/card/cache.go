// Package card handles fetching and caching Agent Cards from agent URLs.
package card

import (
	"container/list"
	"sync"
	"time"

	"github.com/agentdns/agent-dns/internal/models"
)

// cacheEntry wraps a cached Agent Card with TTL information.
type cacheEntry struct {
	card      *models.EntityCard
	expiresAt time.Time
	key       string
}

// LRUCache is a thread-safe LRU cache for Agent Cards with TTL expiry.
type LRUCache struct {
	mu       sync.RWMutex
	maxSize  int
	ttl      time.Duration
	items    map[string]*list.Element
	eviction *list.List
}

// NewLRUCache creates a new LRU cache with the given max size and TTL.
func NewLRUCache(maxSize int, ttlSeconds int) *LRUCache {
	return &LRUCache{
		maxSize:  maxSize,
		ttl:      time.Duration(ttlSeconds) * time.Second,
		items:    make(map[string]*list.Element),
		eviction: list.New(),
	}
}

// Get retrieves a cached Agent Card by entity_id. Returns nil if not found or expired.
func (c *LRUCache) Get(agentID string) *models.EntityCard {
	c.mu.RLock()
	elem, ok := c.items[agentID]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	entry := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		// Expired — remove it
		c.mu.Lock()
		c.removeElement(elem)
		c.mu.Unlock()
		return nil
	}

	// Move to front (most recently used)
	c.mu.Lock()
	c.eviction.MoveToFront(elem)
	c.mu.Unlock()

	return entry.card
}

// Put stores an Agent Card in the cache. Evicts the least recently used entry if full.
func (c *LRUCache) Put(agentID string, card *models.EntityCard) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If already exists, update it
	if elem, ok := c.items[agentID]; ok {
		c.eviction.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry)
		entry.card = card
		entry.expiresAt = time.Now().Add(c.ttl)
		return
	}

	// Evict if at capacity
	if c.eviction.Len() >= c.maxSize {
		c.evictOldest()
	}

	entry := &cacheEntry{
		card:      card,
		expiresAt: time.Now().Add(c.ttl),
		key:       agentID,
	}
	elem := c.eviction.PushFront(entry)
	c.items[agentID] = elem
}

// Remove deletes a specific entry from the cache.
func (c *LRUCache) Remove(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[agentID]; ok {
		c.removeElement(elem)
	}
}

// Len returns the current number of cached entries.
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear removes all entries from the cache.
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.eviction.Init()
}

func (c *LRUCache) evictOldest() {
	elem := c.eviction.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

func (c *LRUCache) removeElement(elem *list.Element) {
	c.eviction.Remove(elem)
	entry := elem.Value.(*cacheEntry)
	delete(c.items, entry.key)
}
