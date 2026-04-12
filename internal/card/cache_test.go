package card

import (
	"testing"
	"time"

	"github.com/agentdns/agent-dns/internal/models"
)

func TestLRUCache_PutAndGet(t *testing.T) {
	cache := NewLRUCache(10, 3600)

	card := &models.AgentCard{
		AgentID: "zns:test1",
		Version: "1.0",
		Status:  "online",
	}

	cache.Put("zns:test1", card)

	got := cache.Get("zns:test1")
	if got == nil {
		t.Fatal("expected card from cache")
	}
	if got.AgentID != "zns:test1" {
		t.Errorf("expected zns:test1, got %s", got.AgentID)
	}
}

func TestLRUCache_Miss(t *testing.T) {
	cache := NewLRUCache(10, 3600)

	got := cache.Get("nonexistent")
	if got != nil {
		t.Error("expected nil for cache miss")
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	cache := NewLRUCache(2, 3600) // max 2 entries

	cache.Put("a", &models.AgentCard{AgentID: "a"})
	cache.Put("b", &models.AgentCard{AgentID: "b"})
	cache.Put("c", &models.AgentCard{AgentID: "c"}) // should evict "a"

	if cache.Get("a") != nil {
		t.Error("'a' should have been evicted")
	}
	if cache.Get("b") == nil {
		t.Error("'b' should still be in cache")
	}
	if cache.Get("c") == nil {
		t.Error("'c' should be in cache")
	}
}

func TestLRUCache_TTLExpiry(t *testing.T) {
	cache := NewLRUCache(10, 1) // 1 second TTL

	cache.Put("test", &models.AgentCard{AgentID: "test"})

	// Should be available immediately
	if cache.Get("test") == nil {
		t.Error("expected card immediately after put")
	}

	// Wait for TTL to expire
	time.Sleep(1100 * time.Millisecond)

	// Should be expired now
	if cache.Get("test") != nil {
		t.Error("expected nil after TTL expiry")
	}
}

func TestLRUCache_Remove(t *testing.T) {
	cache := NewLRUCache(10, 3600)

	cache.Put("test", &models.AgentCard{AgentID: "test"})
	cache.Remove("test")

	if cache.Get("test") != nil {
		t.Error("expected nil after removal")
	}
	if cache.Len() != 0 {
		t.Errorf("expected length 0 after removal, got %d", cache.Len())
	}
}

func TestLRUCache_Update(t *testing.T) {
	cache := NewLRUCache(10, 3600)

	cache.Put("test", &models.AgentCard{AgentID: "test", Version: "1.0"})
	cache.Put("test", &models.AgentCard{AgentID: "test", Version: "2.0"})

	got := cache.Get("test")
	if got == nil {
		t.Fatal("expected card from cache")
	}
	if got.Version != "2.0" {
		t.Errorf("expected version 2.0, got %s", got.Version)
	}
	if cache.Len() != 1 {
		t.Errorf("expected length 1, got %d", cache.Len())
	}
}

func TestLRUCache_Clear(t *testing.T) {
	cache := NewLRUCache(10, 3600)

	cache.Put("a", &models.AgentCard{AgentID: "a"})
	cache.Put("b", &models.AgentCard{AgentID: "b"})
	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected length 0 after clear, got %d", cache.Len())
	}
}
