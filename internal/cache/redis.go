// Package cache provides a Redis-backed caching layer for Agent DNS.
//
// Redis is used for ephemeral, high-speed data:
//   - Agent Card cache (fetched from agent URLs, TTL-based eviction)
//   - Search result cache (hot queries)
//   - Bloom filters (peer routing, fast bitwise ops)
//   - Rate limiting counters (INCR + EXPIRE)
//   - Peer heartbeat state (ephemeral, OK to lose)
//
// Redis is NOT used for durable data — that belongs in PostgreSQL.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/agentdns/agent-dns/internal/models"
)

// RedisCache wraps a Redis client for Agent DNS caching needs.
type RedisCache struct {
	client *redis.Client
	prefix string // key prefix for namespace isolation
}

// RedisConfig holds Redis connection parameters.
type RedisConfig struct {
	URL      string `toml:"url"`      // redis://localhost:6379/0
	Password string `toml:"password"` // optional
	DB       int    `toml:"db"`       // database number (default 0)
	Prefix   string `toml:"prefix"`   // key prefix (default "zns:")
}

// NewRedisCache creates a new Redis cache client.
// Returns nil (no error) if the URL is empty — Redis is optional.
func NewRedisCache(cfg RedisConfig) (*RedisCache, error) {
	if cfg.URL == "" {
		return nil, nil
	}

	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	if cfg.Password != "" {
		opts.Password = cfg.Password
	}
	if cfg.DB != 0 {
		opts.DB = cfg.DB
	}

	// Connection pool tuning
	opts.PoolSize = 20
	opts.MinIdleConns = 3
	opts.ConnMaxIdleTime = 5 * time.Minute
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second

	client := redis.NewClient(opts)

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "zns:"
	}

	return &RedisCache{
		client: client,
		prefix: prefix,
	}, nil
}

// Close closes the Redis connection.
func (rc *RedisCache) Close() error {
	return rc.client.Close()
}

// --- Agent Card Cache ---
// Keys: agdns:card:{agent_id}

// GetAgentCard retrieves a cached Agent Card.
// Returns nil if not found or expired.
func (rc *RedisCache) GetAgentCard(ctx context.Context, agentID string) (*models.AgentCard, error) {
	key := rc.prefix + "card:" + agentID
	data, err := rc.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get agent card: %w", err)
	}

	card := &models.AgentCard{}
	if err := json.Unmarshal(data, card); err != nil {
		// Corrupt cache entry — delete it
		rc.client.Del(ctx, key)
		return nil, nil
	}
	return card, nil
}

// SetAgentCard caches an Agent Card with TTL.
func (rc *RedisCache) SetAgentCard(ctx context.Context, agentID string, card *models.AgentCard, ttl time.Duration) error {
	key := rc.prefix + "card:" + agentID
	data, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("failed to marshal agent card: %w", err)
	}
	return rc.client.Set(ctx, key, data, ttl).Err()
}

// DeleteAgentCard removes a cached Agent Card.
func (rc *RedisCache) DeleteAgentCard(ctx context.Context, agentID string) error {
	key := rc.prefix + "card:" + agentID
	return rc.client.Del(ctx, key).Err()
}

// --- Search Result Cache ---
// Keys: agdns:search:{hash(query)}

// GetSearchResult retrieves a cached search response.
func (rc *RedisCache) GetSearchResult(ctx context.Context, queryHash string) (*models.SearchResponse, error) {
	key := rc.prefix + "search:" + queryHash
	data, err := rc.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get search result: %w", err)
	}

	resp := &models.SearchResponse{}
	if err := json.Unmarshal(data, resp); err != nil {
		rc.client.Del(ctx, key)
		return nil, nil
	}
	return resp, nil
}

// SetSearchResult caches a search response with TTL.
func (rc *RedisCache) SetSearchResult(ctx context.Context, queryHash string, resp *models.SearchResponse, ttl time.Duration) error {
	key := rc.prefix + "search:" + queryHash
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal search response: %w", err)
	}
	return rc.client.Set(ctx, key, data, ttl).Err()
}

// --- Bloom Filter Storage ---
// Keys: agdns:bloom:{registry_id}

// GetBloomFilter retrieves a cached bloom filter for a peer.
func (rc *RedisCache) GetBloomFilter(ctx context.Context, registryID string) ([]byte, error) {
	key := rc.prefix + "bloom:" + registryID
	data, err := rc.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get bloom filter: %w", err)
	}
	return data, nil
}

// SetBloomFilter stores a bloom filter for a peer with TTL.
func (rc *RedisCache) SetBloomFilter(ctx context.Context, registryID string, data []byte, ttl time.Duration) error {
	key := rc.prefix + "bloom:" + registryID
	return rc.client.Set(ctx, key, data, ttl).Err()
}

// --- Rate Limiting ---
// Keys: agdns:rate:{bucket}:{ip}

// RateLimit checks and increments a rate limit counter.
// Returns true if the request is allowed, false if rate limited.
func (rc *RedisCache) RateLimit(ctx context.Context, bucket string, ip string, maxPerMinute int) (bool, error) {
	key := rc.prefix + "rate:" + bucket + ":" + ip

	pipe := rc.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Minute)
	_, err := pipe.Exec(ctx)
	if err != nil {
		// On Redis error, allow the request (fail open)
		return true, fmt.Errorf("redis rate limit: %w", err)
	}

	return incr.Val() <= int64(maxPerMinute), nil
}

// --- Peer Heartbeat State ---
// Keys: agdns:peer:{registry_id}

// SetPeerHeartbeat records a peer's last heartbeat.
func (rc *RedisCache) SetPeerHeartbeat(ctx context.Context, registryID string, ttl time.Duration) error {
	key := rc.prefix + "peer:" + registryID
	return rc.client.Set(ctx, key, time.Now().UTC().Format(time.RFC3339), ttl).Err()
}

// GetPeerHeartbeat checks if a peer has a recent heartbeat.
func (rc *RedisCache) GetPeerHeartbeat(ctx context.Context, registryID string) (string, error) {
	key := rc.prefix + "peer:" + registryID
	val, err := rc.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// --- Stats / Info ---

// Ping checks Redis connectivity.
func (rc *RedisCache) Ping(ctx context.Context) error {
	return rc.client.Ping(ctx).Err()
}

// Info returns Redis server info for diagnostics.
func (rc *RedisCache) Info(ctx context.Context) (string, error) {
	return rc.client.Info(ctx, "memory", "clients", "stats").Result()
}
