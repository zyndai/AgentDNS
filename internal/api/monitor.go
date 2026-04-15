package api

import (
	"context"
	"log"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/mesh"
	"github.com/agentdns/agent-dns/internal/store"
)

// LivenessMonitor periodically sweeps agents and marks those with stale
// heartbeats as inactive.
type LivenessMonitor struct {
	store    store.Store
	cfg      config.HeartbeatConfig
	gossip   *mesh.GossipHandler
	kp       *identity.Keypair
	eventBus *events.Bus
}

// NewLivenessMonitor creates a new liveness monitor.
func NewLivenessMonitor(
	st store.Store,
	cfg config.HeartbeatConfig,
	gossip *mesh.GossipHandler,
	kp *identity.Keypair,
	eventBus *events.Bus,
) *LivenessMonitor {
	return &LivenessMonitor{
		store:    st,
		cfg:      cfg,
		gossip:   gossip,
		kp:       kp,
		eventBus: eventBus,
	}
}

// Run starts the periodic sweep loop. It blocks until ctx is cancelled.
func (m *LivenessMonitor) Run(ctx context.Context) {
	interval := time.Duration(m.cfg.SweepIntervalS) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("liveness monitor started (sweep=%v, threshold=%ds)", interval, m.cfg.InactiveThresholdS)

	for {
		select {
		case <-ctx.Done():
			log.Println("liveness monitor stopped")
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

// sweep marks stale entities as inactive and propagates status via gossip.
func (m *LivenessMonitor) sweep() {
	threshold := time.Duration(m.cfg.InactiveThresholdS) * time.Second
	ids, err := m.store.MarkInactiveEntities(threshold)
	if err != nil {
		log.Printf("liveness sweep error: %v", err)
		return
	}

	for _, entityID := range ids {
		// Publish event
		m.eventBus.Publish(events.EventEntityBecameInactive, events.HeartbeatEventData{
			EntityID: entityID,
			Status:   "inactive",
		})

		// Propagate via gossip
		ann := m.gossip.CreateStatusAnnouncement(
			entityID,
			"inactive",
			m.kp.RegistryID(),
			m.kp.PublicKeyString(),
			m.kp.Sign,
		)
		m.gossip.BroadcastAnnouncement(ann)
	}

	if len(ids) > 0 {
		log.Printf("liveness sweep: marked %d entities inactive", len(ids))
	}
}
