package models

// HeartbeatRequest is sent by agents to prove liveness.
// The agent signs the timestamp with its Ed25519 key, and the registry
// verifies both the timestamp freshness and the signature.
type HeartbeatRequest struct {
	Timestamp string `json:"timestamp" validate:"required"` // RFC3339 format
}

// HeartbeatResponse is returned by the registry after a successful heartbeat.
type HeartbeatResponse struct {
	Status               string `json:"status"`                 // "ok"
	NextHeartbeatSeconds int    `json:"next_heartbeat_seconds"` // suggested interval
}

// AgentStatus constants for liveness tracking.
const (
	AgentStatusOnline   = "online"   // heartbeat received within timeout
	AgentStatusInactive = "inactive" // heartbeat missed beyond timeout
	AgentStatusUnknown  = "unknown"  // never heartbeated (new agent)
)
