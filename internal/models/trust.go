package models

// ReputationAttestation is a signed observation of an agent's behavior
// from a specific registry over a given time period.
type ReputationAttestation struct {
	AgentID          string  `json:"agent_id"`
	ObserverRegistry string  `json:"observer_registry"`
	Period           string  `json:"period"` // e.g. "2026-02-01/2026-03-01"
	Invocations      int64   `json:"invocations"`
	Successes        int64   `json:"successes"`
	Failures         int64   `json:"failures"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	AvgRating        float64 `json:"avg_rating"`
	Signature        string  `json:"signature"`
}

// TrustScore is the computed trust score for an agent,
// aggregated from multiple attestations via EigenTrust.
type TrustScore struct {
	AgentID          string  `json:"agent_id"`
	Score            float64 `json:"score"`      // 0.0 - 1.0
	Confidence       float64 `json:"confidence"` // how much data backs this score
	AttestationCount int     `json:"attestation_count"`
	ComputedAt       string  `json:"computed_at"`
}

// NetworkStatus describes the current state of the local registry node.
type NetworkStatus struct {
	RegistryID    string `json:"registry_id"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Uptime        string `json:"uptime"`
	PeerCount     int    `json:"peer_count"`
	LocalAgents   int    `json:"local_agents"`
	GossipEntries int    `json:"gossip_entries"`
	CachedCards   int    `json:"cached_cards"`
	NodeType      string `json:"node_type"` // full, light, gateway
}

// NetworkStats provides estimated global network statistics.
type NetworkStats struct {
	EstimatedRegistries int     `json:"estimated_registries"`
	EstimatedAgents     int     `json:"estimated_agents"`
	GossipMessagesHour  int     `json:"gossip_messages_per_hour"`
	SearchesHour        int     `json:"searches_per_hour"`
	MeshConnectivity    float64 `json:"mesh_connectivity"` // 0-1, estimated
}
