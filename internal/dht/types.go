// Package dht implements a Kademlia-style distributed hash table for agent discovery.
//
// The DHT provides O(log n) lookups for agent records across the decentralized
// agent-dns mesh network. It complements the existing gossip protocol (eventual
// consistency) and bloom filter routing (fuzzy search) with exact key-based lookups.
package dht

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/bits"
	"time"
)

const (
	// IDLength is the length of a NodeID in bytes (256 bits).
	IDLength = 32

	// DefaultK is the default k-bucket size (max peers per bucket).
	DefaultK = 20

	// DefaultAlpha is the default concurrency parameter for iterative lookups.
	DefaultAlpha = 3
)

// NodeID is a 256-bit identifier used as both node IDs and DHT keys.
type NodeID [IDLength]byte

// NodeIDFromBytes creates a NodeID from a byte slice.
func NodeIDFromBytes(b []byte) NodeID {
	var id NodeID
	copy(id[:], b)
	return id
}

// NodeIDFromPublicKey derives a NodeID from an Ed25519 public key using SHA-256.
// This produces the same bytes used in agent_id and registry_id derivation.
func NodeIDFromPublicKey(pubKey []byte) NodeID {
	return sha256.Sum256(pubKey)
}

// NodeIDFromAgentID converts a "zns:..." entity ID to a NodeID.
// The ID format is "zns:" + hex(SHA256(pubkey)[:16]), so we parse
// the hex suffix and zero-extend to 32 bytes.
func NodeIDFromAgentID(agentID string) (NodeID, error) {
	var id NodeID
	// Strip prefix: "zns:registry:", "zns:svc:", "zns:dev:", or "zns:"
	hexPart := agentID
	for _, prefix := range []string{"zns:registry:", "zns:svc:", "zns:dev:", "zns:"} {
		if len(agentID) > len(prefix) && agentID[:len(prefix)] == prefix {
			hexPart = agentID[len(prefix):]
			break
		}
	}
	b, err := hex.DecodeString(hexPart)
	if err != nil {
		return id, fmt.Errorf("invalid agent ID hex: %w", err)
	}
	copy(id[:], b)
	return id, nil
}

// Distance computes the XOR distance between two NodeIDs.
func Distance(a, b NodeID) NodeID {
	var d NodeID
	for i := 0; i < IDLength; i++ {
		d[i] = a[i] ^ b[i]
	}
	return d
}

// IsZero returns true if the NodeID is all zeros.
func (id NodeID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

// LeadingZeros returns the number of leading zero bits in the NodeID.
// Used to determine k-bucket index.
func (id NodeID) LeadingZeros() int {
	for i := 0; i < IDLength; i++ {
		if id[i] != 0 {
			return i*8 + bits.LeadingZeros8(id[i])
		}
	}
	return IDLength * 8
}

// Less returns true if a < b when compared as big-endian unsigned integers.
func Less(a, b NodeID) bool {
	for i := 0; i < IDLength; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// Hex returns the hex-encoded string of the NodeID.
func (id NodeID) Hex() string {
	return hex.EncodeToString(id[:])
}

// Short returns the first 8 hex characters for logging.
func (id NodeID) Short() string {
	return hex.EncodeToString(id[:4])
}

// NodeContact represents a known node in the DHT network.
type NodeContact struct {
	ID       NodeID `json:"id"`
	Address  string `json:"address"`
	LastSeen int64  `json:"last_seen"` // Unix timestamp
}

// AgentDHTRecord is the value stored in the DHT for each agent.
type AgentDHTRecord struct {
	AgentID       string   `json:"agent_id"`
	Name          string   `json:"name"`
	Category      string   `json:"category"`
	Tags          []string `json:"tags,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	EntityURL     string   `json:"entity_url"`
	PublicKey     string   `json:"public_key"`
	HomeRegistry  string   `json:"home_registry"`
	DeveloperID   string   `json:"developer_id,omitempty"`
	Status        string   `json:"status,omitempty"`
	LastHeartbeat string   `json:"last_heartbeat,omitempty"`
	StoredAt      string   `json:"stored_at"`
}

// --- DHT Message Types ---

// MsgType identifies the type of DHT message.
type MsgType string

const (
	MsgPing      MsgType = "dht_ping"
	MsgPingReply MsgType = "dht_ping_reply"
	MsgStore     MsgType = "dht_store"
	MsgStoreReply MsgType = "dht_store_reply"
	MsgFindNode  MsgType = "dht_find_node"
	MsgFindNodeReply MsgType = "dht_find_node_reply"
	MsgFindValue MsgType = "dht_find_value"
	MsgFindValueReply MsgType = "dht_find_value_reply"
)

// Message is a DHT protocol message.
type Message struct {
	Type     MsgType       `json:"type"`
	SenderID NodeID        `json:"sender_id"`
	RequestID string       `json:"request_id,omitempty"` // correlate request/response

	// Ping
	SenderAddr string `json:"sender_addr,omitempty"`

	// Store
	Key    NodeID          `json:"key,omitempty"`
	Record *AgentDHTRecord `json:"record,omitempty"`

	// FindNode / FindValue
	Target NodeID `json:"target,omitempty"`

	// Responses
	Nodes []NodeContact   `json:"nodes,omitempty"`  // FindNode/FindValue response
	Value *AgentDHTRecord `json:"value,omitempty"`  // FindValue response (if found)
	OK    bool            `json:"ok,omitempty"`     // Store/Ping ack

	// Timestamp for freshness
	Timestamp time.Time `json:"timestamp"`
}
