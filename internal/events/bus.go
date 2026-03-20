// Package events provides a simple in-process fan-out event bus for streaming
// real-time network activity to WebSocket clients.
package events

import (
	"sync"
	"time"
)

// EventType identifies the kind of network activity.
type EventType string

const (
	EventAgentRegistered   EventType = "agent_registered"
	EventAgentDeregistered EventType = "agent_deregistered"
	EventGossipOutgoing    EventType = "gossip_outgoing"
	EventGossipIncoming    EventType = "gossip_incoming"
	EventSearchOutgoing    EventType = "search_outgoing"
	EventSearchIncoming    EventType = "search_result_incoming"
	EventPeerConnected     EventType = "peer_connected"
	EventPeerDisconnected  EventType = "peer_disconnected"
	EventAgentHeartbeat      EventType = "agent_heartbeat"
	EventAgentBecameInactive EventType = "agent_became_inactive"
	EventAgentBecameActive   EventType = "agent_became_active"
)

// Event is a single network activity event emitted on the bus.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// AgentEventData is the payload for agent_registered / agent_deregistered events.
type AgentEventData struct {
	AgentID  string   `json:"agent_id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Tags     []string `json:"tags,omitempty"`
	Summary  string   `json:"summary,omitempty"`
}

// GossipEventData is the payload for gossip_incoming / gossip_outgoing events.
type GossipEventData struct {
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	Action       string `json:"action"` // register | deregister | update
	HomeRegistry string `json:"home_registry"`
	HopCount     int    `json:"hop_count"`
	Direction    string `json:"direction"` // incoming | outgoing
}

// SearchEventData is the payload for search_outgoing / search_result_incoming events.
type SearchEventData struct {
	Query       string `json:"query"`
	PeerID      string `json:"peer_id,omitempty"`
	ResultCount int    `json:"result_count,omitempty"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
	Direction   string `json:"direction"` // outgoing | incoming
}

// HeartbeatEventData is the payload for heartbeat and agent status events.
type HeartbeatEventData struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

// PeerEventData is the payload for peer_connected / peer_disconnected events.
type PeerEventData struct {
	RegistryID string `json:"registry_id"`
	Name       string `json:"name"`
	Address    string `json:"address"`
}

// Bus is a fan-out event bus. Subscribers receive all published events.
// Slow subscribers are dropped (non-blocking sends) to prevent backpressure.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

// NewBus creates a new event bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[chan Event]struct{}),
	}
}

// Subscribe returns a buffered channel that will receive all future events.
func (b *Bus) Subscribe() chan Event {
	ch := make(chan Event, 256)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Publish broadcasts an event to all current subscribers.
// Non-blocking: events are dropped for slow subscribers.
func (b *Bus) Publish(eventType EventType, data interface{}) {
	e := Event{
		Type:      eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
	b.mu.RLock()
	for ch := range b.subscribers {
		select {
		case ch <- e:
		default:
			// subscriber too slow — drop
		}
	}
	b.mu.RUnlock()
}
