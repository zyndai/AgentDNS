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
	s.pool.Exec(t.Context(), "DELETE FROM node_meta")

	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_CreateAndGetAgent(t *testing.T) {
	s := newTestStore(t)

	agent := &models.RegistryRecord{
		AgentID:      "zns:test123",
		Name:         "TestAgent",
		Owner:        "did:key:testowner",
		EntityURL:     "https://example.com/.well-known/agent.json",
		Category:     "developer-tools",
		Tags:         []string{"python", "security"},
		Summary:      "A test agent",
		PublicKey:    "ed25519:testpubkey",
		HomeRegistry: "zns:registry:test",
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
	got, err := s.GetAgent("zns:test123")
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
		AgentID:      "zns:update123",
		Name:         "Original",
		Owner:        "did:key:owner1",
		EntityURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{"test"},
		Summary:      "Original summary",
		PublicKey:    "ed25519:pubkey",
		HomeRegistry: "zns:registry:test",
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

	got, _ := s.GetAgent("zns:update123")
	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got '%s'", got.Name)
	}
}

func TestStore_DeleteAgent(t *testing.T) {
	s := newTestStore(t)

	agent := &models.RegistryRecord{
		AgentID:      "zns:delete123",
		Name:         "ToDelete",
		Owner:        "did:key:owner1",
		EntityURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Will be deleted",
		PublicKey:    "ed25519:pubkey",
		HomeRegistry: "zns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:sig",
	}

	s.CreateAgent(agent)

	// Delete
	if err := s.DeleteAgent("zns:delete123", "did:key:owner1"); err != nil {
		t.Fatalf("failed to delete agent: %v", err)
	}

	got, _ := s.GetAgent("zns:delete123")
	if got != nil {
		t.Error("agent should be deleted")
	}
}

func TestStore_SearchByKeyword(t *testing.T) {
	s := newTestStore(t)

	agents := []*models.RegistryRecord{
		{
			AgentID: "zns:search1", Name: "PythonReviewer", Owner: "did:key:o1",
			EntityURL: "https://example.com/a1.json", Category: "developer-tools",
			Tags: []string{"python", "code-review"}, Summary: "Reviews Python code",
			PublicKey: "ed25519:pk1", HomeRegistry: "zns:registry:test",
			RegisteredAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			TTL:          86400, Signature: "ed25519:sig1",
		},
		{
			AgentID: "zns:search2", Name: "JapaneseTranslator", Owner: "did:key:o2",
			EntityURL: "https://example.com/a2.json", Category: "translation",
			Tags: []string{"japanese", "english", "legal"}, Summary: "Translates legal documents",
			PublicKey: "ed25519:pk2", HomeRegistry: "zns:registry:test",
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
		AgentID: "zns:count1", Name: "Test", Owner: "did:key:o",
		EntityURL: "https://example.com/a.json", Category: "test",
		Tags: []string{}, Summary: "Test", PublicKey: "ed25519:pk",
		HomeRegistry: "zns:registry:test",
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
		AgentID:      "zns:gossip1",
		Name:         "RemoteAgent",
		Category:     "translation",
		Tags:         []string{"japanese"},
		Summary:      "Remote translation agent",
		HomeRegistry: "zns:registry:remote",
		EntityURL:     "https://remote.example.com/agent.json",
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
	s.TombstoneGossipEntry("zns:gossip1")
	count, _ = s.CountGossipEntries()
	if count != 0 {
		t.Errorf("expected 0 active gossip entries after tombstone, got %d", count)
	}
}

func TestStore_CreateAgentSetsLastHeartbeat(t *testing.T) {
	s := newTestStore(t)

	agent := &models.RegistryRecord{
		AgentID:      "zns:create-hb-test",
		Name:         "CreateHBAgent",
		Owner:        "did:key:owner",
		EntityURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Test that CreateAgent sets last_heartbeat",
		PublicKey:    "ed25519:pk",
		HomeRegistry: "zns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:sig",
	}

	before := time.Now().Add(-2 * time.Second)
	if err := s.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	got, err := s.GetAgent("zns:create-hb-test")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if got.LastHeartbeat == "" {
		t.Fatal("expected last_heartbeat to be set on CreateAgent")
	}

	hb, err := time.Parse(time.RFC3339, got.LastHeartbeat)
	if err != nil {
		t.Fatalf("failed to parse last_heartbeat: %v", err)
	}
	if hb.Before(before) || hb.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("last_heartbeat %v is not within expected range", hb)
	}
}

func TestStore_UpsertGossipEntryWithStatus(t *testing.T) {
	s := newTestStore(t)

	entry := &models.GossipEntry{
		AgentID:      "zns:gossip-status-upsert",
		Name:         "StatusUpsertAgent",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Test upsert preserves status",
		HomeRegistry: "zns:registry:remote",
		EntityURL:     "https://remote.example.com/agent.json",
		ReceivedAt:   time.Now().UTC().Format(time.RFC3339),
		Status:       "active",
	}

	if err := s.UpsertGossipEntry(entry); err != nil {
		t.Fatalf("failed to upsert gossip entry: %v", err)
	}

	got, err := s.GetGossipEntry("zns:gossip-status-upsert")
	if err != nil {
		t.Fatalf("failed to get gossip entry: %v", err)
	}
	if got == nil {
		t.Fatal("expected gossip entry to exist")
	}
	if got.Status != "active" {
		t.Fatalf("expected status 'active', got '%s'", got.Status)
	}
}

func TestStore_UpdateAgentHeartbeat(t *testing.T) {
	s := newTestStore(t)

	agent := &models.RegistryRecord{
		AgentID:      "zns:hb1",
		Name:         "HeartbeatAgent",
		Owner:        "did:key:owner",
		EntityURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Heartbeat test agent",
		PublicKey:    "ed25519:pk",
		HomeRegistry: "zns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:sig",
	}
	if err := s.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Update heartbeat
	if err := s.UpdateAgentHeartbeat("zns:hb1"); err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	// Verify agent is active with a non-empty last_heartbeat
	got, err := s.GetAgent("zns:hb1")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", got.Status)
	}
	if got.LastHeartbeat == "" {
		t.Error("expected last_heartbeat to be set")
	}
}

func TestStore_MarkInactiveAgents(t *testing.T) {
	s := newTestStore(t)

	// Create two agents
	for _, id := range []string{"zns:active1", "zns:stale1"} {
		agent := &models.RegistryRecord{
			AgentID:      id,
			Name:         "Agent-" + id,
			Owner:        "did:key:owner",
			EntityURL:     "https://example.com/agent.json",
			Category:     "tools",
			Tags:         []string{},
			Summary:      "Test agent",
			PublicKey:    "ed25519:pk-" + id,
			HomeRegistry: "zns:registry:test",
			RegisteredAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			TTL:          86400,
			Signature:    "ed25519:sig",
		}
		if err := s.CreateAgent(agent); err != nil {
			t.Fatalf("failed to create agent %s: %v", id, err)
		}
	}

	// Give active1 a fresh heartbeat
	if err := s.UpdateAgentHeartbeat("zns:active1"); err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	// stale1 has no heartbeat (NULL last_heartbeat) — should be marked inactive
	// with a threshold of 1 second (active1 just got a heartbeat, so it's safe)
	ids, err := s.MarkInactiveAgents(1 * time.Second)
	if err != nil {
		t.Fatalf("failed to mark inactive agents: %v", err)
	}

	// stale1 should be in the list (NULL last_heartbeat)
	found := false
	for _, id := range ids {
		if id == "zns:stale1" {
			found = true
		}
		if id == "zns:active1" {
			t.Error("active1 should not be marked inactive")
		}
	}
	if !found {
		t.Error("stale1 should have been marked inactive")
	}

	// Verify stale1 is now inactive
	got, _ := s.GetAgent("zns:stale1")
	if got.Status != "inactive" {
		t.Errorf("expected stale1 status 'inactive', got '%s'", got.Status)
	}

	// Verify active1 is still active
	got, _ = s.GetAgent("zns:active1")
	if got.Status != "active" {
		t.Errorf("expected active1 status 'active', got '%s'", got.Status)
	}
}

func TestStore_UpdateGossipEntryStatus(t *testing.T) {
	s := newTestStore(t)

	entry := &models.GossipEntry{
		AgentID:      "zns:gossip-status1",
		Name:         "GossipStatusAgent",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Gossip status test",
		HomeRegistry: "zns:registry:remote",
		EntityURL:     "https://remote.example.com/agent.json",
		ReceivedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.UpsertGossipEntry(entry); err != nil {
		t.Fatalf("failed to upsert gossip entry: %v", err)
	}

	// Update status to inactive
	if err := s.UpdateGossipEntryStatus("zns:gossip-status1", "inactive"); err != nil {
		t.Fatalf("failed to update gossip entry status: %v", err)
	}

	// Search should still find it (status filtering is in the search engine layer)
	results, err := s.SearchGossipByKeyword("gossip", "", nil, 10)
	if err != nil {
		t.Fatalf("gossip search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 gossip result, got %d", len(results))
	}
	if results[0].Status != "inactive" {
		t.Errorf("expected gossip entry status 'inactive', got '%s'", results[0].Status)
	}
}
