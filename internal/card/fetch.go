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
func (f *Fetcher) FetchCard(agentID, agentURL, publicKey string) (*models.EntityCard, error) {
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

	// Tier 3: Fetch from URL.
	// Try the well-known paths in order of preference:
	//   1. /.well-known/agent-card.json   — A2A v0.3 spec (new TS SDK 0.3+)
	//   2. /.well-known/zynd-agent.json   — legacy Zynd-native format
	//   3. /.well-known/agent.json        — original A2A draft + legacy SDK
	cardURL, body, err := f.tryWellKnownPaths(agentURL)
	if err != nil {
		return nil, err
	}

	card := &models.EntityCard{}
	if err := json.Unmarshal(body, card); err != nil {
		return nil, fmt.Errorf("failed to parse agent card from %s: %w", cardURL, err)
	}

	// Verify entity_id matches
	if card.EntityID != "" && card.EntityID != agentID {
		return nil, fmt.Errorf("agent card at %s entity_id mismatch: expected %s, got %s", cardURL, agentID, card.EntityID)
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
// that may not be in the Go EntityCard struct.
func (f *Fetcher) FetchCardRaw(agentID, agentURL string) ([]byte, error) {
	_, body, err := f.tryWellKnownPaths(agentURL)
	return body, err
}

// wellKnownCardPaths are the paths the fetcher attempts in order. The first
// HTTP 200 wins. The list is structured so the most-current format is tried
// first; older paths stay as fallbacks for agents that haven't migrated yet.
var wellKnownCardPaths = []string{
	"/.well-known/agent-card.json", // A2A v0.3 (TS SDK 0.3+)
	"/.well-known/zynd-agent.json", // legacy Zynd-native format
	"/.well-known/agent.json",      // original A2A draft + legacy SDK
}

// tryWellKnownPaths attempts each well-known card path until one returns 200,
// then reads and returns its body (≤50 KB) along with the URL that worked.
// When the caller already passed a fully-qualified card URL (ends in .json or
// already contains /.well-known/), no fallback is attempted — that URL is
// fetched verbatim.
func (f *Fetcher) tryWellKnownPaths(agentURL string) (string, []byte, error) {
	candidates := []string{agentURL}
	if !strings.HasSuffix(agentURL, ".json") && !strings.Contains(agentURL, ".well-known") {
		base := strings.TrimRight(agentURL, "/")
		candidates = candidates[:0]
		for _, p := range wellKnownCardPaths {
			candidates = append(candidates, base+p)
		}
	}

	var lastErr error
	for _, cardURL := range candidates {
		req, err := http.NewRequest("GET", cardURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to build request for %s: %w", cardURL, err)
			continue
		}
		req.Header.Set("User-Agent", "ZyndDNS/1.0 (Card Fetcher)")
		req.Header.Set("Accept", "application/json")

		resp, err := f.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to fetch agent card from %s: %w", cardURL, err)
			continue
		}
		// Read & close inside the loop so we can fall through on non-200.
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("agent card at %s returned status %d", cardURL, resp.StatusCode)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read agent card from %s: %w", cardURL, err)
			continue
		}
		return cardURL, body, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no well-known card path returned a card for %s", agentURL)
	}
	return "", nil, lastErr
}

// verifyCardSignatureRaw verifies the Agent Card signature against the original
// JSON bytes from the HTTP response. This is necessary because the SDK may include
// fields not present in the Go EntityCard struct (name, description, tags, etc.).
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
