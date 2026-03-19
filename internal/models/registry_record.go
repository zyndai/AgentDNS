// Package models defines core data structures for the Agent DNS system.
package models

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// CurrentSchemaVersion is the current schema version for registry records and agent cards.
const CurrentSchemaVersion = "1.0"

// RegistryRecord is the stable, lightweight record stored on the registry network.
// It acts as a static pointer to the dynamic Agent Card hosted by the agent itself.
// Size: ~500-800 bytes (without capabilities) or ~800-1200 bytes (with capability summary).
// Cheap to replicate, store, and index.
type RegistryRecord struct {
	AgentID           string             `json:"agent_id" db:"agent_id"`
	Name              string             `json:"name" db:"name"`
	Owner             string             `json:"owner" db:"owner"`
	AgentURL          string             `json:"agent_url" db:"agent_url"`
	Category          string             `json:"category" db:"category"`
	Tags              []string           `json:"tags" db:"-"`
	Summary           string             `json:"summary" db:"summary"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty" db:"-"`
	PublicKey         string             `json:"public_key" db:"public_key"`
	HomeRegistry      string             `json:"home_registry" db:"home_registry"`
	SchemaVersion     string             `json:"schema_version" db:"schema_version"`
	RegisteredAt      string             `json:"registered_at" db:"registered_at"`
	UpdatedAt         string             `json:"updated_at" db:"updated_at"`
	TTL               int                `json:"ttl" db:"ttl"`
	Signature         string             `json:"signature" db:"signature"`

	// Developer identity fields (optional for backward compatibility)
	DeveloperID    string          `json:"developer_id,omitempty" db:"developer_id"`
	AgentIndex     *int            `json:"agent_index,omitempty" db:"agent_index"`
	DeveloperProof *DeveloperProof `json:"developer_proof,omitempty" db:"-"` // stored as JSONB

	// Liveness fields (managed by heartbeat protocol)
	Status        string `json:"status,omitempty" db:"status"`                 // online, inactive, unknown
	LastHeartbeat string `json:"last_heartbeat,omitempty" db:"last_heartbeat"` // RFC3339
}

// CapabilitySummary provides searchable metadata about agent capabilities.
// This is a lightweight summary stored in the registry (500 bytes max).
// Full capability details (schemas, examples, etc.) remain in the Agent Card.
type CapabilitySummary struct {
	Skills      []string `json:"skills,omitempty"`       // e.g., ["code-review", "linting", "security-audit"]
	Protocols   []string `json:"protocols,omitempty"`    // e.g., ["a2a", "mcp", "jsonrpc"]
	Languages   []string `json:"languages,omitempty"`    // e.g., ["python", "javascript", "go"]
	Models      []string `json:"models,omitempty"`       // e.g., ["gpt-4", "claude-3.5-sonnet"]
	InputTypes  []string `json:"input_types,omitempty"`  // e.g., ["text", "code", "image"]
	OutputTypes []string `json:"output_types,omitempty"` // e.g., ["text", "json", "markdown"]
}

// RegistrationRequest is submitted by agent owners to register a new agent.
type RegistrationRequest struct {
	Name              string             `json:"name" validate:"required,min=1,max=100"`
	AgentURL          string             `json:"agent_url" validate:"required,url"`
	Category          string             `json:"category" validate:"required,min=1,max=50"`
	Tags              []string           `json:"tags" validate:"max=20"`
	Summary           string             `json:"summary" validate:"required,max=200"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty"`
	PublicKey         string             `json:"public_key" validate:"required"`
	Signature         string             `json:"signature" validate:"required"`

	// Developer identity fields (optional -- agents can register without a developer)
	DeveloperID    string          `json:"developer_id,omitempty"`
	DeveloperProof *DeveloperProof `json:"developer_proof,omitempty"`
}

// UpdateRequest is submitted by agent owners to update their registry record.
type UpdateRequest struct {
	AgentURL          *string            `json:"agent_url,omitempty"`
	Category          *string            `json:"category,omitempty"`
	Tags              []string           `json:"tags,omitempty"`
	Summary           *string            `json:"summary,omitempty"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty"`
	TTL               *int               `json:"ttl,omitempty"`
	Signature         string             `json:"signature" validate:"required"`
}

// GenerateAgentID derives an agent_id from an Ed25519 public key.
// Format: agdns:<first 16 bytes of SHA-256 of public key as hex>
func GenerateAgentID(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return "agdns:" + hex.EncodeToString(hash[:16])
}

// GenerateRegistryID derives a registry_id from an Ed25519 public key.
// Format: agdns:registry:<first 16 bytes of SHA-256 of public key as hex>
func GenerateRegistryID(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return "agdns:registry:" + hex.EncodeToString(hash[:16])
}

// SignableBytes returns the canonical JSON bytes of the record for signing,
// excluding the signature field itself.
func (r *RegistryRecord) SignableBytes() ([]byte, error) {
	// Create a copy without signature
	rec := *r
	rec.Signature = ""
	return json.Marshal(rec)
}

// Validate performs basic validation of a RegistryRecord.
func (r *RegistryRecord) Validate() error {
	if r.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(r.Name) > 100 {
		return fmt.Errorf("name must be 100 characters or less")
	}
	if r.Owner == "" {
		return fmt.Errorf("owner is required")
	}
	if r.AgentURL == "" {
		return fmt.Errorf("agent_url is required")
	}
	if r.Category == "" {
		return fmt.Errorf("category is required")
	}
	if len(r.Summary) > 200 {
		return fmt.Errorf("summary must be 200 characters or less")
	}
	if r.PublicKey == "" {
		return fmt.Errorf("public_key is required")
	}
	if len(r.Tags) > 20 {
		return fmt.Errorf("maximum 20 tags allowed")
	}
	return nil
}

// NowRFC3339 returns the current time in RFC3339 format.
func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
