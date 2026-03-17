package models

// SearchRequest represents a search query from a client.
type SearchRequest struct {
	Query    string   `json:"query"`
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	// Capability filters (NEW)
	Skills        []string `json:"skills,omitempty"`    // e.g., ["code-review", "linting"]
	Protocols     []string `json:"protocols,omitempty"` // e.g., ["a2a", "mcp"]
	Languages     []string `json:"languages,omitempty"` // e.g., ["python", "javascript"]
	Models        []string `json:"models,omitempty"`    // e.g., ["gpt-4"]
	MinTrustScore float64  `json:"min_trust_score,omitempty"`
	Status        string   `json:"status,omitempty"` // online, offline, any
	MaxResults    int      `json:"max_results,omitempty"`
	Offset        int      `json:"offset,omitempty"`
	Federated     bool     `json:"federated"`
	Enrich        bool     `json:"enrich"`
	TimeoutMs     int      `json:"timeout_ms,omitempty"`
}

// SearchResponse is returned to the client with ranked results.
type SearchResponse struct {
	Results     []SearchResult `json:"results"`
	TotalFound  int            `json:"total_found"`
	Offset      int            `json:"offset"`
	HasMore     bool           `json:"has_more"`
	SearchStats *SearchStats   `json:"search_stats,omitempty"`
}

// SearchResult is a single result in the search response.
type SearchResult struct {
	AgentID           string             `json:"agent_id"`
	Name              string             `json:"name"`
	Summary           string             `json:"summary"`
	Category          string             `json:"category"`
	Tags              []string           `json:"tags"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty"`
	AgentURL          string             `json:"agent_url"`
	HomeRegistry      string             `json:"home_registry"`
	Score             float64            `json:"score"`
	ScoreBreakdown    *ScoreBreakdown    `json:"score_breakdown,omitempty"`
	Card              *AgentCard         `json:"card,omitempty"` // included if enrich=true
}

// ScoreBreakdown explains how the score was computed.
type ScoreBreakdown struct {
	TextRelevance      float64 `json:"text_relevance"`
	SemanticSimilarity float64 `json:"semantic_similarity"`
	TrustScore         float64 `json:"trust_score"`
	Freshness          float64 `json:"freshness"`
	Availability       float64 `json:"availability"`
}

// SearchStats provides metadata about the search execution.
type SearchStats struct {
	LocalResults     int `json:"local_results"`
	GossipResults    int `json:"gossip_results"`
	FederatedResults int `json:"federated_results"`
	PeersQueried     int `json:"peers_queried"`
	LatencyMs        int `json:"latency_ms"`
}
