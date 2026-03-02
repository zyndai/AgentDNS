package models

// GossipAnnouncement is the lightweight message propagated across the mesh.
// Size: ~300-400 bytes. Designed for efficient gossip propagation.
type GossipAnnouncement struct {
	Type         string   `json:"type"` // agent_announce, agent_update, agent_tombstone
	AgentID      string   `json:"agent_id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	Summary      string   `json:"summary"`
	HomeRegistry string   `json:"home_registry"`
	AgentURL     string   `json:"agent_url"`
	Action       string   `json:"action"` // register, update, deregister
	Timestamp    string   `json:"timestamp"`
	Signature    string   `json:"signature"` // signed by originating registry
	HopCount     int      `json:"hop_count"`
	MaxHops      int      `json:"max_hops"`
}

// GossipEntry is stored in the local gossip index — a lightweight version
// of remote agent information learned from gossip announcements.
type GossipEntry struct {
	AgentID      string   `json:"agent_id" db:"agent_id"`
	Name         string   `json:"name" db:"name"`
	Category     string   `json:"category" db:"category"`
	Tags         []string `json:"tags" db:"-"`
	Summary      string   `json:"summary" db:"summary"`
	HomeRegistry string   `json:"home_registry" db:"home_registry"`
	AgentURL     string   `json:"agent_url" db:"agent_url"`
	ReceivedAt   string   `json:"received_at" db:"received_at"`
	Tombstoned   bool     `json:"tombstoned" db:"tombstoned"`
	TombstoneAt  string   `json:"tombstone_at,omitempty" db:"tombstone_at"`
}

// PeerInfo describes a connected peer registry in the mesh.
type PeerInfo struct {
	RegistryID  string `json:"registry_id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	PublicKey   string `json:"public_key"`
	AgentCount  int    `json:"agent_count"`
	ConnectedAt string `json:"connected_at"`
	LastSeen    string `json:"last_seen"`
	Latency     int    `json:"latency_ms"`             // last measured latency
	BloomFilter []byte `json:"bloom_filter,omitempty"` // tags/categories bloom filter
}

// Tombstone marks an agent for deletion across the network.
type Tombstone struct {
	AgentID   string `json:"agent_id" db:"agent_id"`
	Reason    string `json:"reason" db:"reason"`
	CreatedAt string `json:"created_at" db:"created_at"`
	ExpiresAt string `json:"expires_at" db:"expires_at"`
	Signature string `json:"signature" db:"signature"`
}
