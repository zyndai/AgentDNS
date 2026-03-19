package api

import (
	"log"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/store"
)

// LivenessMonitor periodically checks for agents whose heartbeat has expired
// and marks them as inactive. Status transitions are gossiped to the mesh.
type LivenessMonitor struct {
	store        store.Store
	cfg          config.HeartbeatConfig
	gossip       *mesh.GossipHandler
	eventBus     *events.Bus
	nodeIdentity interface {
		RegistryID() string
		PublicKeyString() string
		Sign([]byte) string
	}
}

// NewLivenessMonitor creates a new liveness monitor.
func NewLivenessMonitor(
	st store.Store,
	cfg config.HeartbeatConfig,
	gossip *mesh.GossipHandler,
	eventBus *events.Bus,
	nodeIdentity interface {
		RegistryID() string
		PublicKeyString() string
		Sign([]byte) string
	},
) *LivenessMonitor {
	return &LivenessMonitor{
		store:        st,
		cfg:          cfg,
		gossip:       gossip,
		eventBus:     eventBus,
		nodeIdentity: nodeIdentity,
	}
}

// Start begins the background liveness sweep loop. Call this in a goroutine.
func (m *LivenessMonitor) Start() {
	interval := time.Duration(m.cfg.SweepIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("liveness monitor started (timeout=%ds, sweep=%ds)",
		m.cfg.TimeoutSeconds, m.cfg.SweepIntervalSeconds)

	for range ticker.C {
		m.sweep()
	}
}

// sweep checks for agents whose heartbeat has expired and marks them inactive.
func (m *LivenessMonitor) sweep() {
	timeout := time.Duration(m.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	cutoff := time.Now().Add(-timeout)

	agentIDs, err := m.store.MarkInactiveAgents(cutoff)
	if err != nil {
		log.Printf("liveness sweep error: %v", err)
		return
	}

	if len(agentIDs) == 0 {
		return
	}

	log.Printf("liveness sweep: marked %d agents inactive", len(agentIDs))

	for _, agentID := range agentIDs {
		// Gossip the status change to mesh peers
		ann := m.createStatusAnnouncement(agentID, models.AgentStatusInactive)
		m.gossip.BroadcastAnnouncement(ann)

		// Emit event for WebSocket activity stream
		if m.eventBus != nil {
			m.eventBus.Publish(events.EventAgentInactive, events.HeartbeatEventData{
				AgentID: agentID,
				Status:  models.AgentStatusInactive,
			})
		}
	}
}

// createStatusAnnouncement creates a gossip announcement for an agent status change.
func (m *LivenessMonitor) createStatusAnnouncement(agentID string, status string) *models.GossipAnnouncement {
	ann := &models.GossipAnnouncement{
		Type:            "agent_status",
		AgentID:         agentID,
		Action:          status, // "online" or "inactive"
		Status:          status,
		HomeRegistry:    m.nodeIdentity.RegistryID(),
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		OriginPublicKey: m.nodeIdentity.PublicKeyString(),
		HopCount:        0,
		MaxHops:         10,
	}

	// Sign the announcement
	ann.Signature = m.nodeIdentity.Sign([]byte(agentID + ":" + status + ":" + ann.Timestamp))

	return ann
}
