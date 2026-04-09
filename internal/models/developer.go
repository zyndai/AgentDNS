package models

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// DeveloperRecord represents a registered developer identity on the network.
// Developers are the entities who build and deploy agents. Each developer has
// an Ed25519 keypair and can register multiple agents under their identity.
// Developer identities are propagated across the mesh via gossip.
type DeveloperRecord struct {
	DeveloperID   string `json:"developer_id" db:"developer_id"`         // agdns:dev:<hash>
	Name          string `json:"name" db:"name"`                         // human-readable developer name
	PublicKey     string `json:"public_key" db:"public_key"`             // ed25519:<base64>
	ProfileURL    string `json:"profile_url,omitempty" db:"profile_url"` // optional website/profile
	GitHub        string `json:"github,omitempty" db:"github"`           // optional GitHub handle
	HomeRegistry  string `json:"home_registry" db:"home_registry"`       // registry where first registered
	SchemaVersion string `json:"schema_version" db:"schema_version"`     // schema version
	RegisteredAt  string `json:"registered_at" db:"registered_at"`
	UpdatedAt     string `json:"updated_at" db:"updated_at"`
	Signature     string `json:"signature" db:"signature"` // developer signs the registration

	// ZNS handle fields (optional — developers can exist without handles)
	DevHandle          string `json:"dev_handle,omitempty" db:"dev_handle"`                     // human-readable handle, e.g., "acme-corp"
	DevHandleVerified  bool   `json:"dev_handle_verified,omitempty" db:"dev_handle_verified"`   // true if handle is domain/github verified
	VerificationMethod string `json:"verification_method,omitempty" db:"verification_method"`   // "dns", "github", or "" (self-claimed)
	VerificationProof  string `json:"verification_proof,omitempty" db:"verification_proof"`     // domain name or github username
}

// DeveloperRegistrationRequest is submitted to register a new developer identity.
type DeveloperRegistrationRequest struct {
	Name       string `json:"name" validate:"required,min=1,max=100"`
	PublicKey  string `json:"public_key" validate:"required"`
	ProfileURL string `json:"profile_url,omitempty"`
	GitHub     string `json:"github,omitempty"`
	Signature  string `json:"signature" validate:"required"`
	Handle     string `json:"handle,omitempty"` // optional ZNS handle, claimed atomically during registration
}

// DeveloperUpdateRequest is submitted to update a developer profile.
type DeveloperUpdateRequest struct {
	Name       *string `json:"name,omitempty"`
	ProfileURL *string `json:"profile_url,omitempty"`
	GitHub     *string `json:"github,omitempty"`
	Signature  string  `json:"signature" validate:"required"`
}

// DeveloperProof is a cryptographic proof linking a developer to an agent.
// It contains the developer's signature over (agent_public_key || agent_index),
// proving the developer authorized this specific agent key.
// This proof can be verified offline using only the developer's public key.
type DeveloperProof struct {
	DeveloperPublicKey string `json:"developer_public_key"` // ed25519:<base64>
	AgentIndex         int    `json:"agent_index"`          // derivation index
	DeveloperSignature string `json:"developer_signature"`  // ed25519:<base64> over (agent_pub || index)
}

// GossipDeveloperEntry is stored in the gossip index for developer identities
// received from remote registries.
type GossipDeveloperEntry struct {
	DeveloperID  string `json:"developer_id" db:"developer_id"`
	Name         string `json:"name" db:"name"`
	PublicKey    string `json:"public_key" db:"public_key"`
	ProfileURL   string `json:"profile_url,omitempty" db:"profile_url"`
	GitHub       string `json:"github,omitempty" db:"github"`
	HomeRegistry string `json:"home_registry" db:"home_registry"`
	ReceivedAt   string `json:"received_at" db:"received_at"`
	Tombstoned   bool   `json:"tombstoned" db:"tombstoned"`
	TombstoneAt  string `json:"tombstone_at,omitempty" db:"tombstone_at"`

	// ZNS handle fields
	DevHandle          string `json:"dev_handle,omitempty" db:"dev_handle"`
	DevHandleVerified  bool   `json:"dev_handle_verified,omitempty" db:"dev_handle_verified"`
	VerificationMethod string `json:"verification_method,omitempty" db:"verification_method"`
	VerificationProof  string `json:"verification_proof,omitempty" db:"verification_proof"`
}

// GenerateDeveloperID derives a developer_id from an Ed25519 public key.
// Format: zns:dev:<first 16 bytes of SHA-256 of public key as hex>
func GenerateDeveloperID(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return "zns:dev:" + hex.EncodeToString(hash[:16])
}

// SignableBytes returns the canonical JSON bytes of the developer record for signing,
// excluding the signature field itself.
func (d *DeveloperRecord) SignableBytes() ([]byte, error) {
	rec := *d
	rec.Signature = ""
	return json.Marshal(rec)
}

// Validate performs basic validation of a DeveloperRecord.
func (d *DeveloperRecord) Validate() error {
	if d.DeveloperID == "" {
		return fmt.Errorf("developer_id is required")
	}
	if d.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(d.Name) > 100 {
		return fmt.Errorf("name must be 100 characters or less")
	}
	if d.PublicKey == "" {
		return fmt.Errorf("public_key is required")
	}
	return nil
}
