package models

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
