package mesh

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
	"github.com/agentdns/agent-dns/internal/store"
)

// GossipHandler processes incoming and outgoing gossip announcements.
type GossipHandler struct {
	mu          sync.RWMutex
	store       store.Store
	cfg         config.GossipConfig
	seen        map[string]time.Time             // dedup: agent_id+timestamp -> received_at
	onAnnounce  func(*models.GossipAnnouncement) // callback for indexing
	onBroadcast func(*models.GossipAnnouncement) // callback to broadcast to peers
	eventBus    *events.Bus
}

// NewGossipHandler creates a new gossip protocol handler.
func NewGossipHandler(st store.Store, cfg config.GossipConfig) *GossipHandler {
	gh := &GossipHandler{
		store: st,
		cfg:   cfg,
		seen:  make(map[string]time.Time),
	}

	// Start dedup window cleaner
	go gh.cleanDedupWindow()

	return gh
}

// SetAnnounceCallback registers a function to be called when a new valid
// announcement is received (for indexing in the search engine).
func (gh *GossipHandler) SetAnnounceCallback(fn func(*models.GossipAnnouncement)) {
	gh.mu.Lock()
	gh.onAnnounce = fn
	gh.mu.Unlock()
}

// SetEventBus attaches an event bus for publishing gossip activity.
func (gh *GossipHandler) SetEventBus(bus *events.Bus) {
	gh.mu.Lock()
	gh.eventBus = bus
	gh.mu.Unlock()
}

// SetBroadcastFunc registers the function used to broadcast announcements
// to connected mesh peers. This is typically Transport.Broadcast.
func (gh *GossipHandler) SetBroadcastFunc(fn func(*models.GossipAnnouncement)) {
	gh.mu.Lock()
	gh.onBroadcast = fn
	gh.mu.Unlock()
}

// BroadcastAnnouncement sends a locally-created announcement to all mesh peers.
// Call this after creating an announcement via CreateAnnouncement.
func (gh *GossipHandler) BroadcastAnnouncement(ann *models.GossipAnnouncement) {
	if ann == nil {
		return
	}

	// Mark as seen locally to prevent processing our own announcement if echoed back
	dedupKey := ann.AgentID + ":" + ann.Timestamp
	gh.mu.Lock()
	gh.seen[dedupKey] = time.Now()
	broadcastFn := gh.onBroadcast
	gh.mu.Unlock()

	if broadcastFn != nil {
		broadcastFn(ann)
	}

	gh.mu.RLock()
	bus := gh.eventBus
	gh.mu.RUnlock()
	if bus != nil {
		bus.Publish(events.EventGossipOutgoing, events.GossipEventData{
			AgentID:      ann.AgentID,
			Name:         ann.Name,
			Action:       ann.Action,
			HomeRegistry: ann.HomeRegistry,
			HopCount:     ann.HopCount,
			Direction:    "outgoing",
		})
	}
}

// HandleAnnouncement processes an incoming gossip announcement.
// Returns true if the announcement is new and should be forwarded.
func (gh *GossipHandler) HandleAnnouncement(ann *models.GossipAnnouncement) bool {
	// Check hop count
	if ann.HopCount >= ann.MaxHops {
		return false
	}

	// Verify signature
	if ann.Signature == "" || ann.OriginPublicKey == "" {
		log.Printf("gossip: rejecting unsigned announcement for %s", ann.AgentID)
		return false
	}
	annCopy := *ann
	annCopy.Signature = ""
	data, err := json.Marshal(&annCopy)
	if err != nil {
		log.Printf("gossip: failed to marshal announcement for verification: %v", err)
		return false
	}
	pubKey := ann.OriginPublicKey
	if strings.HasPrefix(pubKey, "ed25519:") {
		pubKey = pubKey[8:]
	}
	valid, err := identity.Verify(pubKey, data, ann.Signature)
	if err != nil || !valid {
		log.Printf("gossip: invalid signature on announcement for %s: %v", ann.AgentID, err)
		return false
	}

	// Dedup check
	dedupKey := ann.AgentID + ":" + ann.Timestamp
	gh.mu.RLock()
	_, seen := gh.seen[dedupKey]
	gh.mu.RUnlock()
	if seen {
		return false
	}

	// Mark as seen
	gh.mu.Lock()
	gh.seen[dedupKey] = time.Now()
	gh.mu.Unlock()

	// Process based on action
	switch ann.Action {
	case "register", "update":
		entry := &models.GossipEntry{
			AgentID:      ann.AgentID,
			Name:         ann.Name,
			Category:     ann.Category,
			Tags:         ann.Tags,
			Summary:      ann.Summary,
			HomeRegistry: ann.HomeRegistry,
			AgentURL:     ann.AgentURL,
			ReceivedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		if err := gh.store.UpsertGossipEntry(entry); err != nil {
			log.Printf("failed to store gossip entry: %v", err)
		}

		// Notify search engine to index
		gh.mu.RLock()
		cb := gh.onAnnounce
		gh.mu.RUnlock()
		if cb != nil {
			cb(ann)
		}

	case "deregister":
		if err := gh.store.TombstoneGossipEntry(ann.AgentID); err != nil {
			log.Printf("failed to tombstone gossip entry: %v", err)
		}
	}

	// Emit incoming gossip event
	gh.mu.RLock()
	bus := gh.eventBus
	gh.mu.RUnlock()
	if bus != nil {
		bus.Publish(events.EventGossipIncoming, events.GossipEventData{
			AgentID:      ann.AgentID,
			Name:         ann.Name,
			Action:       ann.Action,
			HomeRegistry: ann.HomeRegistry,
			HopCount:     ann.HopCount,
			Direction:    "incoming",
		})
	}

	// Increment hop count for forwarding
	ann.HopCount++

	return true // forward to peers
}

// CreateAnnouncement creates a gossip announcement for a local agent event.
func (gh *GossipHandler) CreateAnnouncement(
	agent *models.RegistryRecord,
	action string,
	registryID string,
	pubKey string,
	signFn func([]byte) string,
) *models.GossipAnnouncement {
	ann := &models.GossipAnnouncement{
		Type:            "agent_announce",
		AgentID:         agent.AgentID,
		Name:            agent.Name,
		Category:        agent.Category,
		Tags:            agent.Tags,
		Summary:         agent.Summary,
		HomeRegistry:    registryID,
		AgentURL:        agent.AgentURL,
		Action:          action,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		OriginPublicKey: pubKey,
		HopCount:        0,
		MaxHops:         gh.cfg.MaxHops,
	}

	// Sign the announcement (with Signature empty so verification can reproduce this)
	data, _ := json.Marshal(ann)
	ann.Signature = signFn(data)

	return ann
}

// cleanDedupWindow periodically removes old entries from the dedup map.
func (gh *GossipHandler) cleanDedupWindow() {
	ticker := time.NewTicker(time.Duration(gh.cfg.DedupWindowSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		window := time.Duration(gh.cfg.DedupWindowSeconds) * time.Second
		cutoff := time.Now().Add(-window)

		gh.mu.Lock()
		for key, receivedAt := range gh.seen {
			if receivedAt.Before(cutoff) {
				delete(gh.seen, key)
			}
		}
		gh.mu.Unlock()
	}
}
