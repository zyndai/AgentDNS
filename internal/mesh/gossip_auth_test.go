package mesh

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/store"
)

func newTestGossipHandler(t *testing.T) (*GossipHandler, store.Store) {
	t.Helper()

	dsn := os.Getenv("AGENTDNS_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AGENTDNS_TEST_POSTGRES_URL not set, skipping gossip auth tests")
	}

	s, err := store.New(dsn)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	t.Cleanup(func() { s.Close() })

	cfg := config.GossipConfig{
		MaxHops:            3,
		DedupWindowSeconds: 60,
	}
	gh := NewGossipHandler(s, cfg)
	return gh, s
}

func makeSignedAnnouncement(t *testing.T, kp *identity.Keypair, ann *models.GossipAnnouncement) *models.GossipAnnouncement {
	t.Helper()
	ann.OriginPublicKey = kp.PublicKeyB64
	ann.Signature = ""
	data, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("failed to marshal announcement: %v", err)
	}
	ann.Signature = kp.Sign(data)
	return ann
}

func TestOriginAuth_RegisterStoresKey(t *testing.T) {
	gh, s := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-register-" + time.Now().Format("150405.000")

	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if !gh.HandleAnnouncement(ann) {
		t.Fatal("register announcement should be accepted")
	}

	entry, err := s.GetGossipEntry(agentID)
	if err != nil {
		t.Fatalf("GetGossipEntry failed: %v", err)
	}
	if entry == nil {
		t.Fatal("expected gossip entry to exist")
	}
	if entry.OriginPublicKey != kpA.PublicKeyB64 {
		t.Fatalf("expected origin key %s, got %s", kpA.PublicKeyB64, entry.OriginPublicKey)
	}
}

func TestOriginAuth_StatusFromSameOriginSucceeds(t *testing.T) {
	gh, _ := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-status-ok-" + time.Now().Format("150405.000")

	// Register
	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})
	gh.HandleAnnouncement(ann)

	// Status update from same key
	statusAnn := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		HomeRegistry: "registry-a",
		Action:       "agent_status",
		Status:       "inactive",
		Timestamp:    time.Now().Add(time.Second).UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if !gh.HandleAnnouncement(statusAnn) {
		t.Fatal("status update from same origin should be accepted")
	}
}

func TestOriginAuth_StatusFromDifferentOriginRejected(t *testing.T) {
	gh, _ := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	kpB, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-status-reject-" + time.Now().Format("150405.000")

	// Register with key A
	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})
	gh.HandleAnnouncement(ann)

	// Status update from key B
	statusAnn := makeSignedAnnouncement(t, kpB, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		HomeRegistry: "registry-b",
		Action:       "agent_status",
		Status:       "inactive",
		Timestamp:    time.Now().Add(time.Second).UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if gh.HandleAnnouncement(statusAnn) {
		t.Fatal("status update from different origin should be rejected")
	}
}

func TestOriginAuth_UpdateFromDifferentOriginRejected(t *testing.T) {
	gh, _ := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	kpB, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-update-reject-" + time.Now().Format("150405.000")

	// Register with key A
	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})
	gh.HandleAnnouncement(ann)

	// Update from key B
	updateAnn := makeSignedAnnouncement(t, kpB, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "EvilAgent",
		Category:     "test",
		HomeRegistry: "registry-b",
		AgentURL:     "https://evil.com/agent",
		Action:       "update",
		Timestamp:    time.Now().Add(time.Second).UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if gh.HandleAnnouncement(updateAnn) {
		t.Fatal("update from different origin should be rejected")
	}
}

func TestOriginAuth_DeregisterFromDifferentOriginRejected(t *testing.T) {
	gh, s := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	kpB, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-dereg-reject-" + time.Now().Format("150405.000")

	// Register with key A
	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})
	gh.HandleAnnouncement(ann)

	// Deregister from key B
	deregAnn := makeSignedAnnouncement(t, kpB, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		HomeRegistry: "registry-b",
		Action:       "deregister",
		Timestamp:    time.Now().Add(time.Second).UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if gh.HandleAnnouncement(deregAnn) {
		t.Fatal("deregister from different origin should be rejected")
	}

	// Verify entry is NOT tombstoned
	entry, err := s.GetGossipEntry(agentID)
	if err != nil {
		t.Fatalf("GetGossipEntry failed: %v", err)
	}
	if entry == nil {
		t.Fatal("entry should still exist")
	}
	if entry.Tombstoned {
		t.Fatal("entry should NOT be tombstoned after rejected deregister")
	}
}

func TestOriginAuth_StatusForUnknownAgentRejected(t *testing.T) {
	gh, _ := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-unknown-status-" + time.Now().Format("150405.000")

	// Send agent_status for an agent that has never been registered via gossip
	statusAnn := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "UnknownAgent",
		HomeRegistry: "registry-a",
		Action:       "agent_status",
		Status:       "inactive",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if gh.HandleAnnouncement(statusAnn) {
		t.Fatal("agent_status for unknown agent should be rejected")
	}
}

func TestOriginAuth_DeregisterForUnknownAgentRejected(t *testing.T) {
	gh, _ := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-unknown-dereg-" + time.Now().Format("150405.000")

	// Send deregister for an agent that has never been registered via gossip
	deregAnn := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		HomeRegistry: "registry-a",
		Action:       "deregister",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if gh.HandleAnnouncement(deregAnn) {
		t.Fatal("deregister for unknown agent should be rejected")
	}
}

func TestOriginAuth_BackwardCompat_NullKey(t *testing.T) {
	gh, s := newTestGossipHandler(t)

	kpB, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-compat-" + time.Now().Format("150405.000")

	// Manually insert entry with no origin_public_key (simulates pre-fix data)
	entry := &models.GossipEntry{
		AgentID:      agentID,
		Name:         "LegacyAgent",
		Category:     "test",
		Tags:         []string{},
		Summary:      "legacy",
		HomeRegistry: "registry-old",
		AgentURL:     "https://example.com/agent",
		ReceivedAt:   time.Now().UTC().Format(time.RFC3339),
		// OriginPublicKey intentionally empty
	}
	if err := s.UpsertGossipEntry(entry); err != nil {
		t.Fatalf("failed to upsert legacy entry: %v", err)
	}

	// Status update from any key should succeed
	statusAnn := makeSignedAnnouncement(t, kpB, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "LegacyAgent",
		HomeRegistry: "registry-old",
		Action:       "agent_status",
		Status:       "inactive",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if !gh.HandleAnnouncement(statusAnn) {
		t.Fatal("status update on entry with NULL origin key should be accepted (backward compat)")
	}
}

func TestOriginAuth_ReRegisterDoesNotOverwriteKey(t *testing.T) {
	gh, s := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	kpB, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-reregister-" + time.Now().Format("150405.000")

	// Register with key A
	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})
	gh.HandleAnnouncement(ann)

	// Re-register with key B
	reregAnn := makeSignedAnnouncement(t, kpB, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "EvilAgent",
		Category:     "test",
		HomeRegistry: "registry-b",
		AgentURL:     "https://evil.com/agent",
		Action:       "register",
		Timestamp:    time.Now().Add(time.Second).UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})
	gh.HandleAnnouncement(reregAnn)

	// Stored key should still be A (COALESCE pinning)
	entry, err := s.GetGossipEntry(agentID)
	if err != nil {
		t.Fatalf("GetGossipEntry failed: %v", err)
	}
	if entry.OriginPublicKey != kpA.PublicKeyB64 {
		t.Fatalf("origin key should still be A (%s), got %s", kpA.PublicKeyB64, entry.OriginPublicKey)
	}

	// Status update from B should be rejected
	statusAnn := makeSignedAnnouncement(t, kpB, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "EvilAgent",
		HomeRegistry: "registry-b",
		Action:       "agent_status",
		Status:       "inactive",
		Timestamp:    time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if gh.HandleAnnouncement(statusAnn) {
		t.Fatal("status from key B should be rejected after pinned key A")
	}
}

func TestOriginAuth_RegisterSetsStatusActive(t *testing.T) {
	gh, s := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-reg-status-" + time.Now().Format("150405.000")

	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if !gh.HandleAnnouncement(ann) {
		t.Fatal("register announcement should be accepted")
	}

	entry, err := s.GetGossipEntry(agentID)
	if err != nil {
		t.Fatalf("GetGossipEntry failed: %v", err)
	}
	if entry == nil {
		t.Fatal("expected gossip entry to exist")
	}
	if entry.Status != "active" {
		t.Fatalf("expected status 'active', got '%s'", entry.Status)
	}
}

func TestOriginAuth_DeregisterFromCorrectOriginSucceeds(t *testing.T) {
	gh, s := newTestGossipHandler(t)

	kpA, _ := identity.GenerateKeypair()
	agentID := "zns:auth-test-dereg-ok-" + time.Now().Format("150405.000")

	// Register with key A
	ann := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		Name:         "TestAgent",
		Category:     "test",
		HomeRegistry: "registry-a",
		AgentURL:     "https://example.com/agent",
		Action:       "register",
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})
	gh.HandleAnnouncement(ann)

	// Deregister from same key A
	deregAnn := makeSignedAnnouncement(t, kpA, &models.GossipAnnouncement{
		Type:         "agent_announce",
		AgentID:      agentID,
		HomeRegistry: "registry-a",
		Action:       "deregister",
		Timestamp:    time.Now().Add(time.Second).UTC().Format(time.RFC3339),
		HopCount:     0,
		MaxHops:      3,
	})

	if !gh.HandleAnnouncement(deregAnn) {
		t.Fatal("deregister from correct origin should be accepted")
	}

	// Verify entry is tombstoned
	entry, err := s.GetGossipEntry(agentID)
	if err != nil {
		t.Fatalf("GetGossipEntry failed: %v", err)
	}
	if entry == nil {
		t.Fatal("entry should still exist (tombstoned, not deleted)")
	}
	if !entry.Tombstoned {
		t.Fatal("entry should be tombstoned after authorized deregister")
	}
}
