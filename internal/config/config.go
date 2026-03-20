// Package config handles loading and managing configuration for Agent DNS nodes.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration for an Agent DNS node.
type Config struct {
	Node     NodeConfig     `toml:"node"`
	Mesh     MeshConfig     `toml:"mesh"`
	Gossip   GossipConfig   `toml:"gossip"`
	Registry RegistryConfig `toml:"registry"`
	Search   SearchConfig   `toml:"search"`
	Cache    CacheConfig    `toml:"cache"`
	Redis    RedisConfig    `toml:"redis"`
	Trust    TrustConfig    `toml:"trust"`
	API       APIConfig       `toml:"api"`
	Bloom     BloomConfig     `toml:"bloom"`
	Heartbeat HeartbeatConfig `toml:"heartbeat"`
}

// HeartbeatConfig configures agent heartbeat liveness detection.
type HeartbeatConfig struct {
	Enabled            bool `toml:"enabled"`
	InactiveThresholdS int  `toml:"inactive_threshold_seconds"`
	SweepIntervalS     int  `toml:"sweep_interval_seconds"`
	MaxClockSkewS      int  `toml:"max_clock_skew_seconds"`
}

// NodeConfig describes the node identity and type.
type NodeConfig struct {
	Name       string `toml:"name"`
	Type       string `toml:"type"` // full, light, gateway
	DataDir    string `toml:"data_dir"`
	ExternalIP string `toml:"external_ip"`
}

// MeshConfig describes mesh networking parameters.
type MeshConfig struct {
	ListenPort     int      `toml:"listen_port"`
	MaxPeers       int      `toml:"max_peers"`
	BootstrapPeers []string `toml:"bootstrap_peers"`
	TLSEnabled     bool     `toml:"tls_enabled"`
}

// GossipConfig tunes gossip behavior.
type GossipConfig struct {
	MaxHops                   int `toml:"max_hops"`
	MaxAnnouncementsPerSecond int `toml:"max_announcements_per_second"`
	DedupWindowSeconds        int `toml:"dedup_window_seconds"`
}

// RegistryConfig specifies the PostgreSQL storage backend.
type RegistryConfig struct {
	PostgresURL    string `toml:"postgres_url"`
	MaxLocalAgents int    `toml:"max_local_agents"`
}

// SearchConfig configures the search engine.
type SearchConfig struct {
	// EmbeddingBackend selects the embedder: "hash" (default), "onnx", "http",
	// or any name registered via search.RegisterEmbedder.
	EmbeddingBackend    string `toml:"embedding_backend"`
	EmbeddingModel      string `toml:"embedding_model"`
	EmbeddingDimensions int    `toml:"embedding_dimensions"`
	// EmbeddingModelDir is the directory containing model.onnx + tokenizer.json (for "onnx" backend).
	EmbeddingModelDir string `toml:"embedding_model_dir"`
	// EmbeddingEndpoint is the HTTP URL for the embedding service (for "http" backend).
	EmbeddingEndpoint string `toml:"embedding_endpoint"`
	// UseImprovedKeyword enables advanced BM25 with field boosting, stemming, and synonyms.
	UseImprovedKeyword bool          `toml:"use_improved_keyword"`
	MaxFederatedPeers  int           `toml:"max_federated_peers"`
	FederatedTimeoutMs int           `toml:"federated_timeout_ms"`
	DefaultMaxResults  int           `toml:"default_max_results"`
	Ranking            RankingConfig `toml:"ranking"`
}

// RankingConfig defines weights for the scoring algorithm.
type RankingConfig struct {
	// Method selects the ranking algorithm: "weighted" (default) or "rrf".
	// "weighted" uses a linear combination of signals with the weights below.
	// "rrf" uses Reciprocal Rank Fusion — no weight tuning needed, works well out of the box.
	Method                   string  `toml:"method"`
	TextRelevanceWeight      float64 `toml:"text_relevance_weight"`
	SemanticSimilarityWeight float64 `toml:"semantic_similarity_weight"`
	TrustWeight              float64 `toml:"trust_weight"`
	FreshnessWeight          float64 `toml:"freshness_weight"`
	AvailabilityWeight       float64 `toml:"availability_weight"`
}

// CacheConfig manages in-process cache sizes and TTLs.
type CacheConfig struct {
	MaxAgentCards       int `toml:"max_agent_cards"`
	AgentCardTTLSeconds int `toml:"agent_card_ttl_seconds"`
	MaxGossipEntries    int `toml:"max_gossip_entries"`
}

// RedisConfig configures the optional Redis cache layer.
// Redis is used for Agent Card caching, search result caching,
// bloom filter storage, rate limiting, and peer heartbeat state.
// Leave URL empty to disable Redis (uses in-process caches only).
type RedisConfig struct {
	URL      string `toml:"url"`      // redis://localhost:6379/0
	Password string `toml:"password"` // optional auth password
	DB       int    `toml:"db"`       // database number (default 0)
	Prefix   string `toml:"prefix"`   // key prefix (default "agdns:")
}

// TrustConfig tunes the trust/reputation system.
type TrustConfig struct {
	MinDisplayScore               float64 `toml:"min_display_score"`
	EigentrustIterations          int     `toml:"eigentrust_iterations"`
	AttestationGossipIntervalSecs int     `toml:"attestation_gossip_interval_seconds"`
}

// APIConfig configures the HTTP API gateway.
type APIConfig struct {
	Listen            string   `toml:"listen"`
	RateLimitSearch   int      `toml:"rate_limit_search"`
	RateLimitRegister int      `toml:"rate_limit_register"`
	CORSOrigins       []string `toml:"cors_origins"`
}

// BloomConfig configures bloom filter parameters.
type BloomConfig struct {
	ExpectedTokens        int     `toml:"expected_tokens"`
	FalsePositiveRate     float64 `toml:"false_positive_rate"`
	UpdateIntervalSeconds int     `toml:"update_interval_seconds"`
}

// DefaultConfig returns a Config with all default values.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		Node: NodeConfig{
			Name:       "my-registry",
			Type:       "full",
			DataDir:    filepath.Join(homeDir, ".agentdns", "data"),
			ExternalIP: "auto",
		},
		Mesh: MeshConfig{
			ListenPort:     4001,
			MaxPeers:       15,
			BootstrapPeers: []string{},
			TLSEnabled:     true,
		},
		Gossip: GossipConfig{
			MaxHops:                   10,
			MaxAnnouncementsPerSecond: 100,
			DedupWindowSeconds:        300,
		},
		Registry: RegistryConfig{
			PostgresURL:    "postgres://agentdns:agentdns@localhost:5432/agentdns?sslmode=disable",
			MaxLocalAgents: 100000,
		},
		Search: SearchConfig{
			EmbeddingBackend:    "hash",
			EmbeddingModel:      "all-MiniLM-L6-v2",
			EmbeddingDimensions: 384,
			UseImprovedKeyword:  true,
			MaxFederatedPeers:   10,
			FederatedTimeoutMs:  1500,
			DefaultMaxResults:   20,
			Ranking: RankingConfig{
				Method:                   "weighted",
				TextRelevanceWeight:      0.30,
				SemanticSimilarityWeight: 0.30,
				TrustWeight:              0.20,
				FreshnessWeight:          0.10,
				AvailabilityWeight:       0.10,
			},
		},
		Cache: CacheConfig{
			MaxAgentCards:       50000,
			AgentCardTTLSeconds: 3600,
			MaxGossipEntries:    2000000,
		},
		Redis: RedisConfig{
			URL:    "",
			Prefix: "agdns:",
		},
		Trust: TrustConfig{
			MinDisplayScore:               0.1,
			EigentrustIterations:          5,
			AttestationGossipIntervalSecs: 3600,
		},
		API: APIConfig{
			Listen:            "0.0.0.0:8080",
			RateLimitSearch:   100,
			RateLimitRegister: 10,
			CORSOrigins:       []string{"*"},
		},
		Bloom: BloomConfig{
			ExpectedTokens:        500000,
			FalsePositiveRate:     0.01,
			UpdateIntervalSeconds: 300,
		},
		Heartbeat: HeartbeatConfig{
			Enabled:            true,
			InactiveThresholdS: 300,
			SweepIntervalS:     60,
			MaxClockSkewS:      60,
		},
	}
}

// Load reads a TOML config file, starting with defaults and overriding from file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Use defaults if no config file
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// Save writes the current config to a TOML file.
func Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// DataDir returns the resolved data directory path, creating it if needed.
func (c *Config) DataDir() (string, error) {
	dir := c.Node.DataDir
	if dir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(homeDir, ".agentdns", "data")
	}
	// Expand ~
	if len(dir) > 0 && dir[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(homeDir, dir[1:])
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}
	return dir, nil
}
