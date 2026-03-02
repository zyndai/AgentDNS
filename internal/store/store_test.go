package store

import (
	"os"
	"testing"
	"time"

	"github.com/agentdns/agent-dns/internal/models"
)

// newTestStore creates a PostgresStore for testing.
// Requires AGENTDNS_TEST_POSTGRES_URL env var to be set.
// Tests are skipped if no PostgreSQL is available.
func newTestStore(t *testing.T) Store {
	t.Helper()

	dsn := os.Getenv("AGENTDNS_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AGENTDNS_TEST_POSTGRES_URL not set, skipping PostgreSQL tests")
	}

	s, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Clean tables before each test
	s.pool.Exec(t.Context(), "DELETE FROM agents")
	s.pool.Exec(t.Context(), "DELETE FROM gossip_entries")
	s.pool.Exec(t.Context(), "DELETE FROM tombstones")
	s.pool.Exec(t.Context(), "DELETE FROM attestations")
	s.pool.Exec(t.Context(), "DELETE FROM node_meta")

	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_CreateAndGetAgent(t *testing.T) {
	s := newTestStore(t)

	agent := &models.RegistryRecord{
		AgentID:      "agdns:test123",
		Name:         "TestAgent",
		Owner:        "did:key:testowner",
		AgentURL:     "https://example.com/.well-known/agent.json",
		Category:     "developer-tools",
		Tags:         []string{"python", "security"},
		Summary:      "A test agent",
		PublicKey:    "ed25519:testpubkey",
		HomeRegistry: "agdns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:testsig",
	}

	// Create
	if err := s.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Get
	got, err := s.GetAgent("agdns:test123")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if got == nil {
		t.Fatal("agent not found")
	}
	if got.Name != "TestAgent" {
		t.Errorf("expected name 'TestAgent', got '%s'", got.Name)
	}
	if len(got.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(got.Tags))
	}
}

func TestStore_UpdateAgent(t *testing.T) {
	s := newTestStore(t)

	agent := &models.RegistryRecord{
		AgentID:      "agdns:update123",
		Name:         "Original",
		Owner:        "did:key:owner1",
		AgentURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{"test"},
		Summary:      "Original summary",
		PublicKey:    "ed25519:pubkey",
		HomeRegistry: "agdns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:sig",
	}

	s.CreateAgent(agent)

	// Update
	agent.Name = "Updated"
	agent.Summary = "Updated summary"
	agent.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.UpdateAgent(agent); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	got, _ := s.GetAgent("agdns:update123")
	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got '%s'", got.Name)
	}
}

func TestStore_DeleteAgent(t *testing.T) {
	s := newTestStore(t)

	agent := &models.RegistryRecord{
		AgentID:      "agdns:delete123",
		Name:         "ToDelete",
		Owner:        "did:key:owner1",
		AgentURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Will be deleted",
		PublicKey:    "ed25519:pubkey",
		HomeRegistry: "agdns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:sig",
	}

	s.CreateAgent(agent)

	// Delete
	if err := s.DeleteAgent("agdns:delete123", "did:key:owner1"); err != nil {
		t.Fatalf("failed to delete agent: %v", err)
	}

	got, _ := s.GetAgent("agdns:delete123")
	if got != nil {
		t.Error("agent should be deleted")
	}
}

func TestStore_SearchByKeyword(t *testing.T) {
	s := newTestStore(t)

	agents := []*models.RegistryRecord{
		{
			AgentID: "agdns:search1", Name: "PythonReviewer", Owner: "did:key:o1",
			AgentURL: "https://example.com/a1.json", Category: "developer-tools",
			Tags: []string{"python", "code-review"}, Summary: "Reviews Python code",
			PublicKey: "ed25519:pk1", HomeRegistry: "agdns:registry:test",
			RegisteredAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			TTL:          86400, Signature: "ed25519:sig1",
		},
		{
			AgentID: "agdns:search2", Name: "JapaneseTranslator", Owner: "did:key:o2",
			AgentURL: "https://example.com/a2.json", Category: "translation",
			Tags: []string{"japanese", "english", "legal"}, Summary: "Translates legal documents",
			PublicKey: "ed25519:pk2", HomeRegistry: "agdns:registry:test",
			RegisteredAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			TTL:          86400, Signature: "ed25519:sig2",
		},
	}

	for _, a := range agents {
		s.CreateAgent(a)
	}

	// Search for Python
	results, err := s.SearchAgentsByKeyword("python", "", nil, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'python', got %d", len(results))
	}

	// Search with category filter
	results, err = s.SearchAgentsByKeyword("legal", "translation", nil, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'legal' in translation, got %d", len(results))
	}
}

func TestStore_CountAgents(t *testing.T) {
	s := newTestStore(t)

	count, _ := s.CountAgents()
	if count != 0 {
		t.Errorf("expected 0 agents, got %d", count)
	}

	agent := &models.RegistryRecord{
		AgentID: "agdns:count1", Name: "Test", Owner: "did:key:o",
		AgentURL: "https://example.com/a.json", Category: "test",
		Tags: []string{}, Summary: "Test", PublicKey: "ed25519:pk",
		HomeRegistry: "agdns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400, Signature: "ed25519:sig",
	}
	s.CreateAgent(agent)

	count, _ = s.CountAgents()
	if count != 1 {
		t.Errorf("expected 1 agent, got %d", count)
	}
}

func TestStore_GossipEntries(t *testing.T) {
	s := newTestStore(t)

	entry := &models.GossipEntry{
		AgentID:      "agdns:gossip1",
		Name:         "RemoteAgent",
		Category:     "translation",
		Tags:         []string{"japanese"},
		Summary:      "Remote translation agent",
		HomeRegistry: "agdns:registry:remote",
		AgentURL:     "https://remote.example.com/agent.json",
		ReceivedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.UpsertGossipEntry(entry); err != nil {
		t.Fatalf("failed to upsert gossip entry: %v", err)
	}

	count, _ := s.CountGossipEntries()
	if count != 1 {
		t.Errorf("expected 1 gossip entry, got %d", count)
	}

	// Search gossip
	results, err := s.SearchGossipByKeyword("japanese", "", nil, 10)
	if err != nil {
		t.Fatalf("gossip search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 gossip result, got %d", len(results))
	}

	// Tombstone
	s.TombstoneGossipEntry("agdns:gossip1")
	count, _ = s.CountGossipEntries()
	if count != 0 {
		t.Errorf("expected 0 active gossip entries after tombstone, got %d", count)
	}
}
