package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/store"
)

func testMonitorSetup(t *testing.T) (store.Store, *LivenessMonitor) {
	t.Helper()

	dsn := os.Getenv("AGENTDNS_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AGENTDNS_TEST_POSTGRES_URL not set, skipping monitor tests")
	}

	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	kp, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	gossipHandler := mesh.NewGossipHandler(st, config.GossipConfig{
		MaxHops:            10,
		DedupWindowSeconds: 300,
	})

	hbCfg := config.HeartbeatConfig{
		Enabled:            true,
		InactiveThresholdS: 2, // short for testing
		SweepIntervalS:     1,
		MaxClockSkewS:      60,
	}

	bus := events.NewBus()
	monitor := NewLivenessMonitor(st, hbCfg, gossipHandler, kp, bus)

	return st, monitor
}

func createMonitorTestAgent(t *testing.T, st store.Store, agentID string) {
	t.Helper()
	agent := &models.RegistryRecord{
		EntityID:      agentID,
		Name:         "MonitorAgent-" + agentID,
		Owner:        "did:key:test",
		EntityURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Monitor test agent",
		PublicKey:    "ed25519:pk-" + agentID,
		HomeRegistry: "zns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:sig",
	}
	if err := st.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create agent %s: %v", agentID, err)
	}
}

func TestMonitor_StaleAgentsMarkedInactive(t *testing.T) {
	st, monitor := testMonitorSetup(t)

	// Create two agents
	createMonitorTestAgent(t, st, "zns:mon-stale")
	createMonitorTestAgent(t, st, "zns:mon-fresh")

	// Give fresh agent a heartbeat
	if err := st.UpdateEntityHeartbeat("zns:mon-fresh"); err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	// Run the monitor briefly
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go monitor.Run(ctx)

	// Wait for at least one sweep
	time.Sleep(2 * time.Second)

	// Stale agent should be inactive (NULL last_heartbeat, threshold is 2s)
	stale, _ := st.GetEntity("zns:mon-stale")
	if stale.Status != "inactive" {
		t.Errorf("expected stale agent to be 'inactive', got '%s'", stale.Status)
	}

	// Fresh agent should still be active
	fresh, _ := st.GetEntity("zns:mon-fresh")
	if fresh.Status != "active" {
		t.Errorf("expected fresh agent to be 'active', got '%s'", fresh.Status)
	}
}

func TestMonitor_RecentHeartbeatsRemainActive(t *testing.T) {
	st, monitor := testMonitorSetup(t)

	createMonitorTestAgent(t, st, "zns:mon-alive")

	// Give it a heartbeat
	if err := st.UpdateEntityHeartbeat("zns:mon-alive"); err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	// Run a single sweep
	monitor.sweep()

	// Should still be active
	agent, _ := st.GetEntity("zns:mon-alive")
	if agent.Status != "active" {
		t.Errorf("expected agent to remain 'active', got '%s'", agent.Status)
	}
}
