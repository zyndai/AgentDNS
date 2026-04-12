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
// Supports both agents (autonomous LLM entities) and services (stateless API tools).
type RegistryRecord struct {
	AgentID           string             `json:"agent_id" db:"agent_id"`
	Name              string             `json:"name" db:"name"`
	Owner             string             `json:"owner" db:"owner"`
	EntityURL         string             `json:"entity_url" db:"agent_url"`
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

	// Developer identity fields
	DeveloperID    string          `json:"developer_id,omitempty" db:"developer_id"`
	AgentIndex     *int            `json:"agent_index,omitempty" db:"agent_index"`
	DeveloperProof *DeveloperProof `json:"developer_proof,omitempty" db:"-"` // stored as JSONB

	// Codebase integrity
	CodebaseHash string `json:"codebase_hash,omitempty" db:"codebase_hash"`

	// Heartbeat liveness fields (server-managed, excluded from signing)
	Status        string `json:"status,omitempty" db:"status"`
	LastHeartbeat string `json:"last_heartbeat,omitempty" db:"last_heartbeat"`

	// Service directory fields (entity_type discriminates agent vs service)
	EntityType      string          `json:"entity_type,omitempty" db:"entity_type"`
	ServiceEndpoint string          `json:"service_endpoint,omitempty" db:"service_endpoint"`
	OpenAPIURL      string          `json:"openapi_url,omitempty" db:"openapi_url"`
	EntityPricing  *EntityPricing `json:"entity_pricing,omitempty" db:"-"`
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

// RegistrationRequest is submitted to register a new entity (agent or service).
type RegistrationRequest struct {
	Name              string             `json:"name" validate:"required,min=1,max=100"`
	EntityURL         string             `json:"entity_url" validate:"omitempty,url"`
	Category          string             `json:"category" validate:"required,min=1,max=50"`
	Tags              []string           `json:"tags" validate:"max=20"`
	Summary           string             `json:"summary" validate:"required,max=200"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty"`
	PublicKey         string             `json:"public_key" validate:"required"`
	Signature         string             `json:"signature" validate:"required"`

	// Developer identity fields
	DeveloperID    string          `json:"developer_id,omitempty"`
	DeveloperProof *DeveloperProof `json:"developer_proof,omitempty"`

	// ZNS naming fields (optional — requires developer with claimed handle)
	AgentName string `json:"agent_name,omitempty"` // e.g., "doc-translator" or "svc:openai-proxy"
	Version   string `json:"version,omitempty"`    // semver, e.g., "2.1.0"

	// Service directory fields (entity_type discriminates agent vs service)
	EntityType      string          `json:"entity_type,omitempty"`
	ServiceEndpoint string          `json:"service_endpoint,omitempty"`
	OpenAPIURL      string          `json:"openapi_url,omitempty"`
	EntityPricing  *EntityPricing `json:"entity_pricing,omitempty"`
}

// UpdateRequest is submitted by agent owners to update their registry record.
type UpdateRequest struct {
	Name              *string            `json:"name,omitempty"`
	EntityURL         *string            `json:"entity_url,omitempty"`
	Category          *string            `json:"category,omitempty"`
	Tags              []string           `json:"tags,omitempty"`
	Summary           *string            `json:"summary,omitempty"`
	CapabilitySummary *CapabilitySummary `json:"capability_summary,omitempty"`
	TTL               *int               `json:"ttl,omitempty"`
	CodebaseHash      *string            `json:"codebase_hash,omitempty"`
	Signature         string             `json:"signature" validate:"required"`
}

// GenerateAgentID derives an agent_id from an Ed25519 public key.
// Format: zns:<first 16 bytes of SHA-256 of public key as hex>
func GenerateAgentID(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return "zns:" + hex.EncodeToString(hash[:16])
}

// GenerateServiceID derives a service_id from an Ed25519 public key.
// Format: zns:svc:<first 16 bytes of SHA-256 of public key as hex>
func GenerateServiceID(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return "zns:svc:" + hex.EncodeToString(hash[:16])
}

// GenerateRegistryID derives a registry_id from an Ed25519 public key.
// Format: zns:registry:<first 16 bytes of SHA-256 of public key as hex>
func GenerateRegistryID(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return "zns:registry:" + hex.EncodeToString(hash[:16])
}

// SignableBytes returns the canonical JSON bytes of the record for signing,
// excluding the signature field itself.
func (r *RegistryRecord) SignableBytes() ([]byte, error) {
	// Create a copy without signature and server-managed fields
	rec := *r
	rec.Signature = ""
	rec.Status = ""
	rec.LastHeartbeat = ""
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
	if r.EntityType == "" || r.EntityType == "agent" {
		if r.EntityURL == "" {
			return fmt.Errorf("entity_url is required for agent type")
		}
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
	if r.EntityType != "" && !ValidEntityTypes[r.EntityType] {
		return fmt.Errorf("invalid entity_type: %s (must be 'agent' or 'service')", r.EntityType)
	}
	if r.EntityType == "service" {
		if err := ValidateServiceFields(r); err != nil {
			return err
		}
	}
	return nil
}

// NowRFC3339 returns the current time in RFC3339 format.
func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
