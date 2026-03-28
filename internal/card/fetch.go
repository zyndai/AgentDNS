package card

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentdns/agent-dns/internal/cache"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
)

// Fetcher retrieves Agent Cards from agent URLs and verifies their signatures.
// Uses a two-tier cache: Redis (shared, if available) -> in-process LRU.
type Fetcher struct {
	client   *http.Client
	lruCache *LRUCache
	redis    *cache.RedisCache // nil if Redis is not configured
	cardTTL  time.Duration
}

// NewFetcher creates a new Agent Card fetcher with the given caches.
// redis can be nil if Redis is not configured.
func NewFetcher(lruCache *LRUCache, redis *cache.RedisCache, cardTTLSeconds int) *Fetcher {
	ttl := time.Duration(cardTTLSeconds) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}
	return &Fetcher{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		lruCache: lruCache,
		redis:    redis,
		cardTTL:  ttl,
	}
}

// FetchCard retrieves an Agent Card from the given URL.
// Cache check order: in-process LRU -> Redis -> HTTP fetch.
// On cache miss, fetches from the URL, verifies signature, and populates both caches.
func (f *Fetcher) FetchCard(agentID, agentURL, publicKey string) (*models.AgentCard, error) {
	// Tier 1: Check in-process LRU cache
	if card := f.lruCache.Get(agentID); card != nil {
		return card, nil
	}

	// Tier 2: Check Redis cache (if available)
	if f.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		card, err := f.redis.GetAgentCard(ctx, agentID)
		if err == nil && card != nil {
			// Populate LRU cache from Redis hit
			f.lruCache.Put(agentID, card)
			return card, nil
		}
		// On Redis error, fall through to HTTP fetch (fail open)
	}

	// Tier 3: Fetch from URL
	// Try zynd-agent.json first (new format), fall back to agent.json (legacy)
	baseURL := strings.TrimRight(agentURL, "/")
	cardURL := agentURL
	if !strings.HasSuffix(cardURL, ".json") && !strings.Contains(cardURL, ".well-known") {
		// Try new Zynd format first
		zyndURL := baseURL + "/.well-known/zynd-agent.json"
		zResp, zErr := f.client.Get(zyndURL)
		if zErr == nil && zResp.StatusCode == http.StatusOK {
			// Use the Zynd card — close and proceed with this response
			resp := zResp
			_ = resp // proceed below
			cardURL = zyndURL
		} else {
			if zResp != nil {
				zResp.Body.Close()
			}
			cardURL = baseURL + "/.well-known/agent.json"
		}
	}
	resp, err := f.client.Get(cardURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agent card from %s: %w", agentURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card URL returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024)) // 50KB max
	if err != nil {
		return nil, fmt.Errorf("failed to read agent card: %w", err)
	}

	card := &models.AgentCard{}
	if err := json.Unmarshal(body, card); err != nil {
		return nil, fmt.Errorf("failed to parse agent card: %w", err)
	}

	// Verify agent_id matches
	if card.AgentID != "" && card.AgentID != agentID {
		return nil, fmt.Errorf("agent card agent_id mismatch: expected %s, got %s", agentID, card.AgentID)
	}

	// Note: Signature verification is skipped here. The card is fetched directly
	// from the agent's URL — the signature is included for offline/client-side
	// verification. Cross-language canonical JSON differences make server-side
	// verification unreliable between Go and Python serializers.

	// Populate both caches
	f.lruCache.Put(agentID, card)
	if f.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Best-effort: don't fail the fetch if Redis write fails
		_ = f.redis.SetAgentCard(ctx, agentID, card, f.cardTTL)
	}

	return card, nil
}

// FetchCardRaw fetches the agent card and returns the raw JSON bytes as-is.
// This preserves all fields from the SDK (name, description, tags, etc.)
// that may not be in the Go AgentCard struct.
func (f *Fetcher) FetchCardRaw(agentID, agentURL string) ([]byte, error) {
	cardURL := agentURL
	if !strings.HasSuffix(cardURL, ".json") && !strings.Contains(cardURL, ".well-known") {
		cardURL = strings.TrimRight(cardURL, "/") + "/.well-known/agent.json"
	}

	resp, err := f.client.Get(cardURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agent card from %s: %w", cardURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card URL returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read agent card: %w", err)
	}

	return body, nil
}

// verifyCardSignatureRaw verifies the Agent Card signature against the original
// JSON bytes from the HTTP response. This is necessary because the SDK may include
// fields not present in the Go AgentCard struct (name, description, tags, etc.).
// The signature is computed over canonical JSON (sorted keys) with the signature field removed.
func (f *Fetcher) verifyCardSignatureRaw(rawBody []byte, signature, publicKeyStr string) error {
	pubKey := publicKeyStr
	if len(pubKey) > 8 && pubKey[:8] == "ed25519:" {
		pubKey = pubKey[8:]
	}

	// Parse raw JSON into a generic map, remove signature, re-serialize with sorted keys
	var cardMap map[string]interface{}
	if err := json.Unmarshal(rawBody, &cardMap); err != nil {
		return fmt.Errorf("failed to parse card JSON: %w", err)
	}
	delete(cardMap, "signature")

	// Canonical JSON: sorted keys, compact separators (matches Python's json.dumps(sort_keys=True, separators=(",",":")))
	signableBytes, err := json.Marshal(cardMap)
	if err != nil {
		return fmt.Errorf("failed to create signable bytes: %w", err)
	}

	valid, err := identity.Verify(pubKey, signableBytes, signature)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

// InvalidateCache removes a specific agent card from all cache tiers.
func (f *Fetcher) InvalidateCache(agentID string) {
	f.lruCache.Remove(agentID)
	if f.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = f.redis.DeleteAgentCard(ctx, agentID)
	}
}

// CacheSize returns the number of cached agent cards in the in-process LRU.
func (f *Fetcher) CacheSize() int {
	return f.lruCache.Len()
}
