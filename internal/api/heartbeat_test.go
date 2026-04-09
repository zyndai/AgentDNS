package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/store"
)

// testHeartbeatServer sets up a minimal Server with a real store for heartbeat testing.
func testHeartbeatServer(t *testing.T) (*Server, store.Store, *identity.Keypair) {
	t.Helper()

	dsn := os.Getenv("AGENTDNS_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AGENTDNS_TEST_POSTGRES_URL not set, skipping heartbeat tests")
	}

	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	agentKP, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate agent keypair: %v", err)
	}

	cfg := config.DefaultConfig()

	s := &Server{
		cfg:      cfg,
		store:    st,
		eventBus: events.NewBus(),
	}

	return s, st, agentKP
}

// testHeartbeatServerWithGossip sets up a Server with gossip handler and node identity
// for testing gossip broadcast on heartbeat reconnect.
func testHeartbeatServerWithGossip(t *testing.T) (*Server, store.Store, *identity.Keypair, *mesh.GossipHandler) {
	t.Helper()

	dsn := os.Getenv("AGENTDNS_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AGENTDNS_TEST_POSTGRES_URL not set, skipping heartbeat tests")
	}

	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	agentKP, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate agent keypair: %v", err)
	}

	nodeKP, err := identity.GenerateKeypair()
	if err != nil {
		t.Fatalf("failed to generate node keypair: %v", err)
	}

	cfg := config.DefaultConfig()

	gossipCfg := config.GossipConfig{
		MaxHops:            3,
		DedupWindowSeconds: 60,
	}
	gh := mesh.NewGossipHandler(st, gossipCfg)

	s := &Server{
		cfg:          cfg,
		store:        st,
		eventBus:     events.NewBus(),
		gossip:       gh,
		nodeIdentity: nodeKP,
	}

	return s, st, agentKP, gh
}

// registerTestAgent creates a test agent in the store and returns its ID.
func registerTestAgent(t *testing.T, st store.Store, kp *identity.Keypair, suffix string) string {
	t.Helper()
	agentID := models.GenerateAgentID(kp.PublicKey)
	agent := &models.RegistryRecord{
		AgentID:      agentID,
		Name:         "HeartbeatTestAgent-" + suffix,
		Owner:        "did:key:test",
		AgentURL:     "https://example.com/agent.json",
		Category:     "tools",
		Tags:         []string{},
		Summary:      "Test agent for heartbeat",
		PublicKey:    kp.PublicKeyString(),
		HomeRegistry: "zns:registry:test",
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		TTL:          86400,
		Signature:    "ed25519:testsig",
	}
	if err := st.CreateAgent(agent); err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}
	return agentID
}

func TestHeartbeat_ValidSignedMessage(t *testing.T) {
	s, st, agentKP := testHeartbeatServer(t)
	agentID := registerTestAgent(t, st, agentKP, "valid")

	// Set up HTTP test server with the heartbeat handler
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agents/{agentID}/ws", s.handleAgentHeartbeat)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Connect via WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents/" + agentID + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	defer conn.Close()

	// Send a valid heartbeat
	timestamp := time.Now().UTC().Format(time.RFC3339)
	sig := agentKP.Sign([]byte(timestamp))

	msg := models.HeartbeatMessage{
		Timestamp: timestamp,
		Signature: sig,
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("failed to write heartbeat: %v", err)
	}

	// Give the server a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify agent status is active
	agent, err := st.GetAgent(agentID)
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", agent.Status)
	}
	if agent.LastHeartbeat == "" {
		t.Error("expected last_heartbeat to be set")
	}
}

func TestHeartbeat_InvalidSignature(t *testing.T) {
	s, st, agentKP := testHeartbeatServer(t)
	agentID := registerTestAgent(t, st, agentKP, "invalid-sig")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agents/{agentID}/ws", s.handleAgentHeartbeat)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents/" + agentID + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	defer conn.Close()

	// Send a heartbeat with wrong signature
	timestamp := time.Now().UTC().Format(time.RFC3339)
	msg := models.HeartbeatMessage{
		Timestamp: timestamp,
		Signature: "ed25519:aW52YWxpZHNpZ25hdHVyZQ==", // invalid
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("failed to write heartbeat: %v", err)
	}

	// Connection should still be open — send another message to verify
	time.Sleep(100 * time.Millisecond)

	// Send a valid one now to prove connection is still alive
	sig := agentKP.Sign([]byte(timestamp))
	msg.Signature = sig
	err = conn.WriteJSON(msg)
	if err != nil {
		t.Error("connection should still be open after invalid signature")
	}
}

func TestHeartbeat_TimestampOutsideClockSkew(t *testing.T) {
	s, st, agentKP := testHeartbeatServer(t)
	agentID := registerTestAgent(t, st, agentKP, "skew")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agents/{agentID}/ws", s.handleAgentHeartbeat)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents/" + agentID + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	defer conn.Close()

	// Send a heartbeat with a timestamp far in the past
	oldTimestamp := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	sig := agentKP.Sign([]byte(oldTimestamp))

	msg := models.HeartbeatMessage{
		Timestamp: oldTimestamp,
		Signature: sig,
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("failed to write heartbeat: %v", err)
	}

	// Connection should still be open
	time.Sleep(100 * time.Millisecond)

	currentTimestamp := time.Now().UTC().Format(time.RFC3339)
	validSig := agentKP.Sign([]byte(currentTimestamp))
	msg2 := models.HeartbeatMessage{Timestamp: currentTimestamp, Signature: validSig}
	err = conn.WriteJSON(msg2)
	if err != nil {
		t.Error("connection should still be open after clock skew rejection")
	}
}

func TestHeartbeat_AgentNotFound(t *testing.T) {
	s, _, _ := testHeartbeatServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agents/{agentID}/ws", s.handleAgentHeartbeat)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Try to connect for a non-existent agent — should get 404
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents/zns:nonexistent/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error connecting for non-existent agent")
	}
	if resp != nil && resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHeartbeat_ReconnectBroadcastsActiveGossip(t *testing.T) {
	s, st, agentKP, gh := testHeartbeatServerWithGossip(t)
	agentID := registerTestAgent(t, st, agentKP, "gossip-broadcast")

	// Install a tracking broadcast function
	var mu sync.Mutex
	var broadcasts []*models.GossipAnnouncement
	gh.SetBroadcastFunc(func(ann *models.GossipAnnouncement) {
		mu.Lock()
		broadcasts = append(broadcasts, ann)
		mu.Unlock()
	})

	// Set up HTTP test server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agents/{agentID}/ws", s.handleAgentHeartbeat)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Connect via WebSocket — this should trigger a gossip broadcast
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents/" + agentID + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	defer conn.Close()

	// Give the server time to process the connection and broadcast
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(broadcasts) == 0 {
		t.Fatal("expected at least one gossip broadcast on WebSocket connect")
	}

	// Find the agent_status/active announcement
	found := false
	for _, ann := range broadcasts {
		if ann.AgentID == agentID && ann.Action == "agent_status" && ann.Status == "active" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected gossip broadcast with action=agent_status and status=active")
	}
}
