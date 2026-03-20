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

	// Dedup check -- use agent_id for agent announcements, developer_id for developer announcements
	dedupID := ann.AgentID
	if ann.Type == "developer_announce" {
		dedupID = ann.DeveloperID
	}
	dedupKey := dedupID + ":" + ann.Timestamp
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

	// Process based on announcement type and action
	switch ann.Type {
	case "developer_announce":
		switch ann.Action {
		case "register", "update":
			entry := &models.GossipDeveloperEntry{
				DeveloperID:  ann.DeveloperID,
				Name:         ann.Name,
				PublicKey:    ann.PublicKey,
				ProfileURL:   ann.ProfileURL,
				GitHub:       ann.GitHub,
				HomeRegistry: ann.HomeRegistry,
				ReceivedAt:   time.Now().UTC().Format(time.RFC3339),
			}
			if err := gh.store.UpsertGossipDeveloper(entry); err != nil {
				log.Printf("failed to store gossip developer entry: %v", err)
			}
		case "deregister":
			if err := gh.store.TombstoneGossipDeveloper(ann.DeveloperID); err != nil {
				log.Printf("failed to tombstone gossip developer: %v", err)
			}
		}

	default: // agent_announce
		switch ann.Action {
		case "agent_status":
			if !gh.verifyOriginAuthorization(ann.AgentID, ann.OriginPublicKey) {
				return false
			}
			if err := gh.store.UpdateGossipEntryStatus(ann.AgentID, ann.Status); err != nil {
				log.Printf("gossip: failed to update gossip entry status for %s: %v", ann.AgentID, err)
			}

		case "register", "update":
			if ann.Action == "update" {
				if !gh.verifyOriginAuthorization(ann.AgentID, ann.OriginPublicKey) {
					return false
				}
			}
			entry := &models.GossipEntry{
				AgentID:            ann.AgentID,
				Name:               ann.Name,
				Category:           ann.Category,
				Tags:               ann.Tags,
				Summary:            ann.Summary,
				HomeRegistry:       ann.HomeRegistry,
				AgentURL:           ann.AgentURL,
				ReceivedAt:         time.Now().UTC().Format(time.RFC3339),
				DeveloperID:        ann.DeveloperID,
				DeveloperPublicKey: ann.DeveloperPublicKey,
				DeveloperProof:     ann.DeveloperProof,
				OriginPublicKey:    ann.OriginPublicKey,
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
			if !gh.verifyOriginAuthorization(ann.AgentID, ann.OriginPublicKey) {
				return false
			}
			if err := gh.store.TombstoneGossipEntry(ann.AgentID); err != nil {
				log.Printf("failed to tombstone gossip entry: %v", err)
			}
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
// If the agent has developer identity fields, they are included in the announcement
// so remote registries can verify the developer-agent chain of trust.
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

	// Include developer identity fields if present
	if agent.DeveloperID != "" {
		ann.DeveloperID = agent.DeveloperID
		if agent.DeveloperProof != nil {
			ann.DeveloperPublicKey = agent.DeveloperProof.DeveloperPublicKey
			ann.DeveloperProof = agent.DeveloperProof
		}
	}

	// Sign the announcement (with Signature empty so verification can reproduce this)
	data, _ := json.Marshal(ann)
	ann.Signature = signFn(data)

	return ann
}

// CreateDeveloperAnnouncement creates a gossip announcement for a developer identity event.
func (gh *GossipHandler) CreateDeveloperAnnouncement(
	dev *models.DeveloperRecord,
	action string,
	registryID string,
	pubKey string,
	signFn func([]byte) string,
) *models.GossipAnnouncement {
	ann := &models.GossipAnnouncement{
		Type:            "developer_announce",
		Name:            dev.Name,
		HomeRegistry:    registryID,
		Action:          action,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		OriginPublicKey: pubKey,
		HopCount:        0,
		MaxHops:         gh.cfg.MaxHops,
		DeveloperID:     dev.DeveloperID,
		PublicKey:       dev.PublicKey,
		ProfileURL:      dev.ProfileURL,
		GitHub:          dev.GitHub,
	}

	// Sign the announcement
	data, _ := json.Marshal(ann)
	ann.Signature = signFn(data)

	return ann
}

// CreateStatusAnnouncement creates a gossip announcement for an agent status change.
func (gh *GossipHandler) CreateStatusAnnouncement(
	agentID string,
	status string,
	registryID string,
	pubKey string,
	signFn func([]byte) string,
) *models.GossipAnnouncement {
	ann := &models.GossipAnnouncement{
		Type:            "agent_announce",
		AgentID:         agentID,
		HomeRegistry:    registryID,
		Action:          "agent_status",
		Status:          status,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		OriginPublicKey: pubKey,
		HopCount:        0,
		MaxHops:         gh.cfg.MaxHops,
	}

	// Sign the announcement
	data, _ := json.Marshal(ann)
	ann.Signature = signFn(data)

	return ann
}

// verifyOriginAuthorization checks that the announcement's origin public key
// matches the pinned key for this agent. Returns true if the action is authorized.
func (gh *GossipHandler) verifyOriginAuthorization(agentID, originKey string) bool {
	existing, err := gh.store.GetGossipEntry(agentID)
	if err != nil {
		log.Printf("gossip: origin auth lookup failed for %s: %v", agentID, err)
		return false
	}
	// No existing entry or no stored key = backward compat, allow
	if existing == nil || existing.OriginPublicKey == "" {
		return true
	}
	if existing.OriginPublicKey != originKey {
		log.Printf("gossip: rejecting action for %s: origin key mismatch", agentID)
		return false
	}
	return true
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
