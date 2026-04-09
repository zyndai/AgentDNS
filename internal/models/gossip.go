package models

// GossipAnnouncement is the lightweight message propagated across the mesh.
// Size: ~300-600 bytes (with capability summary). Designed for efficient gossip propagation.
// Supports both agent_announce and developer_announce types.
type GossipAnnouncement struct {
	Type              string             `json:"type"` // agent_announce, developer_announce
	AgentID           string             `json:"agent_id,omitempty"`
	Name              string             `json:"name"`
	Category          string             `json:"category,omitempty"`
	Tags              []string           `json:"tags,omitempty"`
	Summary           string             `json:"summary,omitempty"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty"`
	HomeRegistry      string             `json:"home_registry"`
	AgentURL          string             `json:"agent_url,omitempty"`
	Action            string             `json:"action"` // register, update, deregister, agent_status
	Status            string             `json:"status,omitempty"`
	Timestamp         string             `json:"timestamp"`
	OriginPublicKey   string             `json:"origin_public_key"` // public key of originating registry
	Signature         string             `json:"signature"`         // signed by originating registry
	HopCount          int                `json:"hop_count"`
	MaxHops           int                `json:"max_hops"`

	// Entity type (for agent_announce — distinguishes agents from services)
	EntityType      string `json:"entity_type,omitempty"` // "agent" or "service"
	ServiceEndpoint string `json:"service_endpoint,omitempty"`
	OpenAPIURL      string `json:"openapi_url,omitempty"`

	// Developer identity fields (for agent_announce with developer chain)
	DeveloperID        string          `json:"developer_id,omitempty"`
	DeveloperPublicKey string          `json:"developer_public_key,omitempty"`
	DeveloperProof     *DeveloperProof `json:"developer_proof,omitempty"`

	// Developer-specific fields (for developer_announce type)
	ProfileURL string `json:"profile_url,omitempty"`
	GitHub     string `json:"github,omitempty"`
	PublicKey  string `json:"public_key,omitempty"` // developer's own public key (for developer_announce)

	// ZNS handle fields (for dev_handle type)
	DevHandle          string `json:"dev_handle,omitempty"`
	DevHandleVerified  bool   `json:"dev_handle_verified,omitempty"`
	VerificationMethod string `json:"verification_method,omitempty"`

	// ZNS name binding fields (for name_binding type)
	FQAN           string   `json:"fqan,omitempty"`
	AgentNameZNS   string   `json:"agent_name_zns,omitempty"` // distinct from Name to avoid collision
	RegistryHost   string   `json:"registry_host,omitempty"`
	Version        string   `json:"version,omitempty"`
	CapabilityTags []string `json:"capability_tags,omitempty"`

	// Registry verification fields (for registry_proof and peer_attestation types)
	Domain             string   `json:"domain,omitempty"`
	Ed25519PublicKey   string   `json:"ed25519_public_key,omitempty"`
	TLSSPKIFingerprint string   `json:"tls_spki_fingerprint,omitempty"`
	VerificationTier   string   `json:"verification_tier,omitempty"`
	AttesterID         string   `json:"attester_id,omitempty"`
	SubjectID          string   `json:"subject_id,omitempty"`
	VerifiedLayers     []string `json:"verified_layers,omitempty"`
}

// GossipEntry is stored in the local gossip index -- a lightweight version
// of remote agent information learned from gossip announcements.
type GossipEntry struct {
	AgentID           string             `json:"agent_id" db:"agent_id"`
	Name              string             `json:"name" db:"name"`
	Category          string             `json:"category" db:"category"`
	Tags              []string           `json:"tags" db:"-"`
	Summary           string             `json:"summary" db:"summary"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty" db:"-"`
	HomeRegistry      string             `json:"home_registry" db:"home_registry"`
	AgentURL          string             `json:"agent_url" db:"agent_url"`
	ReceivedAt        string             `json:"received_at" db:"received_at"`
	Tombstoned        bool               `json:"tombstoned" db:"tombstoned"`
	TombstoneAt       string             `json:"tombstone_at,omitempty" db:"tombstone_at"`

	// Entity type
	Type            string `json:"type,omitempty" db:"type"`
	ServiceEndpoint string `json:"service_endpoint,omitempty" db:"service_endpoint"`
	OpenAPIURL      string `json:"openapi_url,omitempty" db:"openapi_url"`

	// Developer identity fields
	DeveloperID        string          `json:"developer_id,omitempty" db:"developer_id"`
	DeveloperPublicKey string          `json:"developer_public_key,omitempty" db:"developer_public_key"`
	DeveloperProof     *DeveloperProof `json:"developer_proof,omitempty" db:"-"` // stored as JSONB

	// Heartbeat liveness status
	Status string `json:"status,omitempty" db:"status"`

	// Origin registry public key — pinned on first register to prevent spoofed updates
	OriginPublicKey string `json:"origin_public_key,omitempty" db:"origin_public_key"`
}

// PeerInfo describes a connected peer registry in the mesh.
type PeerInfo struct {
	RegistryID   string `json:"registry_id"`
	Name         string `json:"name"`
	Address      string `json:"address"`
	PublicKey    string `json:"public_key"`
	AgentCount   int    `json:"agent_count"`
	ConnectedAt  string `json:"connected_at"`
	LastSeen     string `json:"last_seen"`
	Latency      int    `json:"latency_ms"`             // last measured latency
	BloomFilter  []byte `json:"bloom_filter,omitempty"` // tags/categories bloom filter
	RegistryHost string `json:"registry_host,omitempty"` // ZNS: the peer's domain name (e.g., "dns01.zynd.ai")
}

// Tombstone marks an agent for deletion across the network.
type Tombstone struct {
	AgentID   string `json:"agent_id" db:"agent_id"`
	Reason    string `json:"reason" db:"reason"`
	CreatedAt string `json:"created_at" db:"created_at"`
	ExpiresAt string `json:"expires_at" db:"expires_at"`
	Signature string `json:"signature" db:"signature"`
}
