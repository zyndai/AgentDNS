package models

// EntityCard is the dynamic metadata document hosted by the entity itself.
// It is fetched on-demand via the entity_url in the RegistryRecord.
// Size: 2-10KB. Updated freely by the entity without touching the registry.
type EntityCard struct {
	EntityID      string       `json:"entity_id"`
	SchemaVersion string       `json:"schema_version"`
	Version       string       `json:"version"`
	Status        string       `json:"status"` // online, offline, degraded, maintenance
	LastHeartbeat string       `json:"last_heartbeat"`
	Capabilities  []Capability `json:"capabilities"`
	Pricing       *Pricing     `json:"pricing,omitempty"`
	Trust         *TrustInfo   `json:"trust,omitempty"`
	Endpoints     *Endpoints   `json:"endpoints,omitempty"`
	Metadata      *CardMeta    `json:"metadata,omitempty"`
	Signature     string       `json:"signature"`
	SignedAt      string       `json:"signed_at"`
}

// Capability describes a single capability offered by an agent.
type Capability struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema,omitempty"`
	OutputSchema map[string]interface{} `json:"output_schema,omitempty"`
	Protocols    []string               `json:"protocols,omitempty"` // a2a, mcp, jsonrpc
	Languages    []string               `json:"languages,omitempty"` // programming languages supported
	LatencyP95Ms int                    `json:"latency_p95_ms,omitempty"`
	Examples     []Example              `json:"examples,omitempty"`
}

// Example provides a sample input/output for a capability.
type Example struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// Pricing describes the agent's pricing model.
type Pricing struct {
	Model          string             `json:"model"` // per-request, subscription, free
	Currency       string             `json:"currency"`
	Rates          map[string]float64 `json:"rates,omitempty"`
	PaymentMethods []string           `json:"payment_methods,omitempty"` // x402, stripe, lightning
}

// TrustInfo contains self-reported trust metrics from the agent.
type TrustInfo struct {
	TotalInvocations int64          `json:"total_invocations"`
	SuccessRate      float64        `json:"success_rate"`
	AvgRating        float64        `json:"avg_rating"`
	Uptime30d        float64        `json:"uptime_30d"`
	Verifications    []Verification `json:"verifications,omitempty"`
}

// Verification is a third-party attestation of agent capabilities.
type Verification struct {
	Issuer string `json:"issuer"`
	Type   string `json:"type"`
	Issued string `json:"issued"`
}

// Endpoints describes the agent's network endpoints.
type Endpoints struct {
	Invoke    string `json:"invoke,omitempty"`
	Health    string `json:"health,omitempty"`
	WebSocket string `json:"websocket,omitempty"`
}

// CardMeta contains optional metadata about the agent.
type CardMeta struct {
	Framework     string `json:"framework,omitempty"`
	Model         string `json:"model,omitempty"`
	OwnerContact  string `json:"owner_contact,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	SourceCode    string `json:"source_code,omitempty"`
}
