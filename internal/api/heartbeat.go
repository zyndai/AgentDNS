package api

import (
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/agentdns/agent-dns/internal/events"
	"github.com/agentdns/agent-dns/internal/identity"
	"github.com/agentdns/agent-dns/internal/models"
)

// handleAgentHeartbeat upgrades to a WebSocket and accepts signed heartbeat
// messages from an agent to prove liveness.
func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// Look up agent to get public key
	agent, err := s.store.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up agent")
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Upgrade to WebSocket
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("heartbeat ws: upgrade failed for %s: %v", agentID, err)
		return
	}
	defer conn.Close()

	// Mark agent active on connect (initial heartbeat)
	if err := s.store.UpdateAgentHeartbeat(agentID); err != nil {
		log.Printf("heartbeat ws: failed initial heartbeat for %s: %v", agentID, err)
	}
	s.eventBus.Publish(events.EventAgentBecameActive, events.HeartbeatEventData{
		AgentID: agentID,
		Status:  "active",
	})

	// Extract public key for signature verification
	pubKeyStr := agent.PublicKey
	if strings.HasPrefix(pubKeyStr, "ed25519:") {
		pubKeyStr = pubKeyStr[8:]
	}

	maxClockSkew := time.Duration(s.cfg.Heartbeat.MaxClockSkewS) * time.Second
	readDeadline := time.Duration(s.cfg.Heartbeat.InactiveThresholdS) * time.Second

	// Read loop
	for {
		conn.SetReadDeadline(time.Now().Add(readDeadline))

		var msg models.HeartbeatMessage
		if err := conn.ReadJSON(&msg); err != nil {
			// Connection closed or read deadline exceeded — agent will be marked
			// inactive by the background monitor after the threshold passes.
			return
		}

		// Validate timestamp is within clock skew window (prevents replay)
		ts, err := time.Parse(time.RFC3339, msg.Timestamp)
		if err != nil {
			log.Printf("heartbeat ws: invalid timestamp from %s: %v", agentID, err)
			continue
		}
		skew := time.Duration(math.Abs(float64(time.Since(ts))))
		if skew > maxClockSkew {
			log.Printf("heartbeat ws: timestamp outside clock skew for %s (skew=%v)", agentID, skew)
			continue
		}

		// Verify Ed25519 signature over timestamp bytes
		valid, err := identity.Verify(pubKeyStr, []byte(msg.Timestamp), msg.Signature)
		if err != nil || !valid {
			log.Printf("heartbeat ws: invalid signature from %s: %v", agentID, err)
			continue
		}

		// Valid heartbeat — update store
		if err := s.store.UpdateAgentHeartbeat(agentID); err != nil {
			log.Printf("heartbeat ws: failed to update heartbeat for %s: %v", agentID, err)
			continue
		}

		s.eventBus.Publish(events.EventAgentHeartbeat, events.HeartbeatEventData{
			AgentID: agentID,
			Status:  "active",
		})
	}
}
