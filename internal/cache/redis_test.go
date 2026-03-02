package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/agentdns/agent-dns/internal/models"
)

// newTestRedisCache creates a RedisCache for testing.
// Requires AGENTDNS_TEST_REDIS_URL env var to be set.
func newTestRedisCache(t *testing.T) *RedisCache {
	t.Helper()

	url := os.Getenv("AGENTDNS_TEST_REDIS_URL")
	if url == "" {
		t.Skip("AGENTDNS_TEST_REDIS_URL not set, skipping Redis tests")
	}

	rc, err := NewRedisCache(RedisConfig{
		URL:    url,
		Prefix: "agdns:test:",
	})
	if err != nil {
		t.Fatalf("failed to create redis cache: %v", err)
	}
	t.Cleanup(func() { rc.Close() })
	return rc
}

func TestRedisCache_AgentCard(t *testing.T) {
	rc := newTestRedisCache(t)
	ctx := context.Background()

	card := &models.AgentCard{
		AgentID: "agdns:redis-test-1",
		Version: "1.0.0",
		Status:  "online",
		Capabilities: []models.Capability{
			{
				Name:        "code-review",
				Description: "Reviews code for security issues",
			},
		},
	}

	// Set
	err := rc.SetAgentCard(ctx, "agdns:redis-test-1", card, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to set agent card: %v", err)
	}

	// Get
	got, err := rc.GetAgentCard(ctx, "agdns:redis-test-1")
	if err != nil {
		t.Fatalf("failed to get agent card: %v", err)
	}
	if got == nil {
		t.Fatal("agent card not found")
	}
	if got.AgentID != card.AgentID {
		t.Errorf("agent_id mismatch: %s vs %s", got.AgentID, card.AgentID)
	}
	if got.Status != "online" {
		t.Errorf("status mismatch: %s vs %s", got.Status, card.Status)
	}

	// Delete
	err = rc.DeleteAgentCard(ctx, "agdns:redis-test-1")
	if err != nil {
		t.Fatalf("failed to delete agent card: %v", err)
	}

	// Verify deleted
	got, err = rc.GetAgentCard(ctx, "agdns:redis-test-1")
	if err != nil {
		t.Fatalf("get after delete failed: %v", err)
	}
	if got != nil {
		t.Error("agent card should be deleted")
	}
}

func TestRedisCache_SearchResult(t *testing.T) {
	rc := newTestRedisCache(t)
	ctx := context.Background()

	resp := &models.SearchResponse{
		Results: []models.SearchResult{
			{
				AgentID:  "agdns:test1",
				Name:     "TestAgent",
				Score:    0.95,
				Category: "tools",
			},
		},
		TotalFound: 1,
	}

	// Set
	err := rc.SetSearchResult(ctx, "testhash123", resp, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to set search result: %v", err)
	}

	// Get
	got, err := rc.GetSearchResult(ctx, "testhash123")
	if err != nil {
		t.Fatalf("failed to get search result: %v", err)
	}
	if got == nil {
		t.Fatal("search result not found")
	}
	if got.TotalFound != 1 {
		t.Errorf("total_found mismatch: %d vs %d", got.TotalFound, resp.TotalFound)
	}
}

func TestRedisCache_RateLimit(t *testing.T) {
	rc := newTestRedisCache(t)
	ctx := context.Background()

	// First request should be allowed
	allowed, err := rc.RateLimit(ctx, "test", "127.0.0.1", 3)
	if err != nil {
		t.Fatalf("rate limit check failed: %v", err)
	}
	if !allowed {
		t.Error("first request should be allowed")
	}

	// Second and third should also be allowed
	allowed, _ = rc.RateLimit(ctx, "test", "127.0.0.1", 3)
	if !allowed {
		t.Error("second request should be allowed")
	}
	allowed, _ = rc.RateLimit(ctx, "test", "127.0.0.1", 3)
	if !allowed {
		t.Error("third request should be allowed")
	}

	// Fourth should be rate limited
	allowed, _ = rc.RateLimit(ctx, "test", "127.0.0.1", 3)
	if allowed {
		t.Error("fourth request should be rate limited")
	}
}

func TestRedisCache_BloomFilter(t *testing.T) {
	rc := newTestRedisCache(t)
	ctx := context.Background()

	bloomData := []byte{0xFF, 0xAA, 0x55, 0x00}

	// Set
	err := rc.SetBloomFilter(ctx, "registry-test-1", bloomData, 10*time.Second)
	if err != nil {
		t.Fatalf("failed to set bloom filter: %v", err)
	}

	// Get
	got, err := rc.GetBloomFilter(ctx, "registry-test-1")
	if err != nil {
		t.Fatalf("failed to get bloom filter: %v", err)
	}
	if len(got) != len(bloomData) {
		t.Errorf("bloom filter length mismatch: %d vs %d", len(got), len(bloomData))
	}
	for i, b := range got {
		if b != bloomData[i] {
			t.Errorf("bloom filter byte %d mismatch: %02x vs %02x", i, b, bloomData[i])
		}
	}
}

func TestRedisCache_Ping(t *testing.T) {
	rc := newTestRedisCache(t)
	ctx := context.Background()

	if err := rc.Ping(ctx); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}
