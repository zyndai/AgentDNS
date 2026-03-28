package mesh

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/agentdns/agent-dns/internal/models"
)

// Message types for the mesh protocol.
const (
	MsgHello     = "hello"
	MsgHeartbeat = "heartbeat"
	MsgGossip    = "gossip"
	MsgSearch    = "search"
	MsgSearchAck = "search_ack"
	MsgDHT       = "dht"
)

// Maximum message size: 1MB. Search responses can be large.
const maxMessageSize = 1 << 20

// Envelope wraps all mesh protocol messages with a type discriminator.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// HelloMessage is exchanged during the initial handshake between two peers.
type HelloMessage struct {
	RegistryID   string `json:"registry_id"`
	Name         string `json:"name"`
	PublicKey    string `json:"public_key"`
	AgentCount   int    `json:"agent_count"`
	Version      string `json:"version"`
	ListenPort   int    `json:"listen_port"`
	RegistryHost string `json:"registry_host,omitempty"` // ZNS: domain name for this registry (e.g., "dns01.zynd.ai")
}

// HeartbeatMessage is sent periodically to maintain peer liveness.
type HeartbeatMessage struct {
	RegistryID  string   `json:"registry_id"`
	AgentCount  int      `json:"agent_count"`
	BloomFilter []byte   `json:"bloom_filter,omitempty"`
	BloomSize   uint     `json:"bloom_size,omitempty"`
	BloomHashes uint     `json:"bloom_hashes,omitempty"`
	PeerAddrs   []string `json:"peer_addrs,omitempty"` // peer exchange
}

// GossipMessage wraps a gossip announcement for mesh transmission.
type GossipMessage struct {
	Announcement *models.GossipAnnouncement `json:"announcement"`
}

// SearchMessage is a federated search request forwarded to peers.
type SearchMessage struct {
	RequestID string                `json:"request_id"`
	Request   *models.SearchRequest `json:"request"`
	OriginID  string                `json:"origin_id"` // registry that originated the search
	TTL       int                   `json:"ttl"`       // remaining hops for federation
}

// SearchAckMessage is the response to a federated search.
type SearchAckMessage struct {
	RequestID string                `json:"request_id"`
	Results   []models.SearchResult `json:"results"`
	Stats     *models.SearchStats   `json:"stats,omitempty"`
}

// writeMessage sends a length-prefixed JSON message over a TCP connection.
// Frame format: [4 bytes big-endian length][JSON payload]
func writeMessage(conn net.Conn, msg *Envelope) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if len(data) > maxMessageSize {
		return fmt.Errorf("message too large: %d bytes (max %d)", len(data), maxMessageSize)
	}

	// Write length prefix
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(lenBuf); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

// readMessage reads a length-prefixed JSON message from a TCP connection.
func readMessage(conn net.Conn, timeout time.Duration) (*Envelope, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))

	// Read length prefix
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, fmt.Errorf("read length: %w", err)
	}

	msgLen := binary.BigEndian.Uint32(lenBuf)
	if msgLen > uint32(maxMessageSize) {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", msgLen, maxMessageSize)
	}

	// Read payload
	data := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}

	return &env, nil
}

// sendTyped is a helper that wraps a typed payload in an Envelope and sends it.
func sendTyped(conn net.Conn, msgType string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return writeMessage(conn, &Envelope{
		Type:    msgType,
		Payload: json.RawMessage(data),
	})
}

// decodePayload unmarshals an Envelope's payload into the given target.
func decodePayload(env *Envelope, target interface{}) error {
	return json.Unmarshal(env.Payload, target)
}
