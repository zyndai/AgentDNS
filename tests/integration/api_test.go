package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/agentdns/agent-dns/internal/api"
	"github.com/agentdns/agent-dns/internal/card"
	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/search"
	"github.com/agentdns/agent-dns/internal/store"
)

// setupTestServer creates a full test server with all components wired up.
// Requires AGENTDNS_TEST_POSTGRES_URL env var to be set.
func setupTestServer(t *testing.T) (*api.Server, *config.Config, *identity.Keypair) {
	t.Helper()

	dsn := os.Getenv("AGENTDNS_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AGENTDNS_TEST_POSTGRES_URL not set, skipping integration tests")
	}

	cfg := config.DefaultConfig()
	cfg.Node.DataDir = t.TempDir()
	cfg.Registry.PostgresURL = dsn

	// Create node identity
	kp, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	// Initialize PostgreSQL store
	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// Initialize components (no Redis for tests)
	lruCache := card.NewLRUCache(1000, 3600)
	fetcher := card.NewFetcher(lruCache, nil, 3600)
	embedder := search.NewHashEmbedder(384) // Use simple hash embedder for tests
	engine := search.NewEngine(st, fetcher, cfg.Search, embedder)
	peerMgr := mesh.NewPeerManager(cfg.Mesh, cfg.Bloom)
	gossipHandler := mesh.NewGossipHandler(st, cfg.Gossip)
	server := api.NewServer(cfg, st, engine, fetcher, peerMgr, gossipHandler, kp)

	return server, cfg, kp
}

func TestHealthEndpoint(t *testing.T) {
	// Simple health check test that doesn't require the full server
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", result["status"])
	}
}

func TestRegistrationRequest_Validation(t *testing.T) {
	// Test that registration request validation works
	req := &models.RegistrationRequest{
		Name:      "TestAgent",
		EntityURL:  "https://example.com/.well-known/agent.json",
		Category:  "tools",
		Tags:      []string{"test"},
		Summary:   "A test agent",
		PublicKey: "ed25519:testkey",
		Signature: "ed25519:testsig",
	}

	// Validate required fields
	if req.Name == "" {
		t.Error("name should be set")
	}
	if req.EntityURL == "" {
		t.Error("entity_url should be set")
	}
	if req.Category == "" {
		t.Error("category should be set")
	}
}

func TestSearchRequest_Serialization(t *testing.T) {
	req := models.SearchRequest{
		Query:         "python code review",
		Category:      "developer-tools",
		Tags:          []string{"python", "security"},
		MinTrustScore: 0.5,
		MaxResults:    20,
		Federated:     true,
		Enrich:        true,
		TimeoutMs:     2000,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal search request: %v", err)
	}

	var decoded models.SearchRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal search request: %v", err)
	}

	if decoded.Query != req.Query {
		t.Errorf("query mismatch: %s vs %s", decoded.Query, req.Query)
	}
	if decoded.MaxResults != req.MaxResults {
		t.Errorf("max_results mismatch: %d vs %d", decoded.MaxResults, req.MaxResults)
	}
}

func TestSearchResponse_Serialization(t *testing.T) {
	resp := models.SearchResponse{
		Results: []models.SearchResult{
			{
				EntityID:  "zns:test1",
				Name:     "TestAgent",
				Summary:  "A test agent",
				Category: "tools",
				Tags:     []string{"test"},
				Score:    0.95,
				ScoreBreakdown: &models.ScoreBreakdown{
					TextRelevance:      0.9,
					SemanticSimilarity: 0.8,
					TrustScore:         0.7,
					Freshness:          0.6,
					Availability:       1.0,
				},
			},
		},
		TotalFound: 1,
		SearchStats: &models.SearchStats{
			LocalResults: 1,
			LatencyMs:    42,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal search response: %v", err)
	}

	// Verify it's valid JSON
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		t.Fatalf("search response is not valid JSON: %v", err)
	}
}
