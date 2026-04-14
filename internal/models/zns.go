package models

import (
	"encoding/json"
	"fmt"
)

// ZNSName is a name binding that maps a Fully Qualified Agent Name (FQAN) to an entity_id.
// The FQAN format is: {registry-host}/{developer-handle}/{agent-name}
// Example: dns01.zynd.ai/acme-corp/doc-translator
type ZNSName struct {
	FQAN            string   `json:"fqan" db:"fqan"`                         // PRIMARY KEY
	EntityName       string   `json:"entity_name" db:"entity_name"`             // e.g., "doc-translator"
	DeveloperHandle string   `json:"developer_handle" db:"developer_handle"` // e.g., "acme-corp"
	RegistryHost    string   `json:"registry_host" db:"registry_host"`       // e.g., "dns01.zynd.ai"
	EntityID        string   `json:"entity_id" db:"entity_id"`                // zns:<hash> or zns:svc:<hash>
	DeveloperID     string   `json:"developer_id" db:"developer_id"`         // zns:dev:<hash>
	CurrentVersion  string   `json:"current_version,omitempty" db:"current_version"`
	CapabilityTags  []string `json:"capability_tags,omitempty" db:"-"` // stored as TEXT[]
	RegisteredAt    string   `json:"registered_at" db:"registered_at"`
	UpdatedAt       string   `json:"updated_at" db:"updated_at"`
	Signature       string   `json:"signature" db:"signature"`
}

// SignableBytes returns canonical JSON for signing, excluding the signature field.
func (n *ZNSName) SignableBytes() ([]byte, error) {
	c := *n
	c.Signature = ""
	return json.Marshal(c)
}

// Validate performs basic validation of a ZNSName.
func (n *ZNSName) Validate() error {
	if n.FQAN == "" {
		return fmt.Errorf("fqan is required")
	}
	if n.EntityName == "" {
		return fmt.Errorf("entity_name is required")
	}
	if n.DeveloperHandle == "" {
		return fmt.Errorf("developer_handle is required")
	}
	if n.RegistryHost == "" {
		return fmt.Errorf("registry_host is required")
	}
	if n.EntityID == "" {
		return fmt.Errorf("entity_id is required")
	}
	if n.DeveloperID == "" {
		return fmt.Errorf("developer_id is required")
	}
	return nil
}

// ZNSVersion tracks a specific version of an entity bound to a FQAN.
type ZNSVersion struct {
	FQAN         string `json:"fqan" db:"fqan"`
	Version      string `json:"version" db:"version"`
	EntityID     string `json:"entity_id" db:"entity_id"` // entity_id at this version
	BuildHash    string `json:"build_hash,omitempty" db:"build_hash"`
	RegisteredAt string `json:"registered_at" db:"registered_at"`
	Signature    string `json:"signature" db:"signature"`
}

// SignableBytes returns canonical JSON for signing.
func (v *ZNSVersion) SignableBytes() ([]byte, error) {
	c := *v
	c.Signature = ""
	return json.Marshal(c)
}

// GossipZNSName is a remote name binding learned via gossip from other registries.
type GossipZNSName struct {
	FQAN            string   `json:"fqan" db:"fqan"`
	EntityName       string   `json:"entity_name" db:"entity_name"`
	DeveloperHandle string   `json:"developer_handle" db:"developer_handle"`
	RegistryHost    string   `json:"registry_host" db:"registry_host"`
	EntityID        string   `json:"entity_id" db:"entity_id"`
	CurrentVersion  string   `json:"current_version,omitempty" db:"current_version"`
	CapabilityTags  []string `json:"capability_tags,omitempty" db:"-"`
	ReceivedAt      string   `json:"received_at" db:"received_at"`
	Tombstoned      bool     `json:"tombstoned" db:"tombstoned"`
}

// HandleClaimRequest is submitted by a developer to claim a human-readable handle.
type HandleClaimRequest struct {
	Handle      string `json:"handle" validate:"required"`
	DeveloperID string `json:"developer_id" validate:"required"`
	PublicKey   string `json:"public_key" validate:"required"`
	Signature   string `json:"signature" validate:"required"`
}

// HandleVerifyRequest is submitted to verify a developer handle.
type HandleVerifyRequest struct {
	Method string `json:"method" validate:"required"` // "dns" or "github"
	Proof  string `json:"proof" validate:"required"`  // domain name or github username
}

// NameBindingRequest is submitted to register or update a ZNS name binding.
type NameBindingRequest struct {
	EntityName       string   `json:"entity_name" validate:"required"`
	DeveloperHandle string   `json:"developer_handle" validate:"required"`
	EntityID        string   `json:"entity_id" validate:"required"`
	Version         string   `json:"version,omitempty"`
	CapabilityTags  []string `json:"capability_tags,omitempty"`
	Signature       string   `json:"signature" validate:"required"`
}

// ZNSResolveResponse is returned by the resolution endpoint.
type ZNSResolveResponse struct {
	FQAN             string   `json:"fqan"`
	EntityID         string   `json:"entity_id"`
	DeveloperID      string   `json:"developer_id"`
	DeveloperHandle  string   `json:"developer_handle"`
	RegistryHost     string   `json:"registry_host"`
	Version          string   `json:"version,omitempty"`
	EntityURL        string   `json:"entity_url"`
	PublicKey        string   `json:"public_key"`
	Protocols        []string `json:"protocols,omitempty"`
	TrustScore       float64  `json:"trust_score"`
	VerificationTier string   `json:"verification_tier,omitempty"`
	Status           string   `json:"status"`
}

// RegistryIdentityProof is a signed document binding a domain, TLS cert, and Ed25519 key.
// Published at /.well-known/zynd-registry.json
type RegistryIdentityProof struct {
	Type               string `json:"type" db:"-"`                                            // "registry-identity-proof"
	Version            string `json:"version" db:"-"`                                         // "1.0"
	Domain             string `json:"domain" db:"domain"`                                     // e.g., "dns01.zynd.ai"
	RegistryID         string `json:"registry_id" db:"registry_id"`                           // agdns:registry:<hash>
	Ed25519PublicKey   string `json:"ed25519_public_key" db:"ed25519_public_key"`             // base64
	TLSSPKIFingerprint string `json:"tls_spki_fingerprint" db:"tls_spki_fingerprint"`         // sha256:<hex>
	IssuedAt           string `json:"issued_at" db:"issued_at"`
	ExpiresAt          string `json:"expires_at" db:"expires_at"`
	Signature          string `json:"signature" db:"proof_signature"`
	// Server-managed fields
	VerificationTier string `json:"verification_tier,omitempty" db:"verification_tier"` // self-announced, domain-verified, dns-published, mesh-verified
	ReceivedAt       string `json:"received_at,omitempty" db:"received_at"`
}

// SignableBytes returns canonical JSON for signing (excluding signature and server fields).
func (p *RegistryIdentityProof) SignableBytes() ([]byte, error) {
	c := *p
	c.Signature = ""
	c.VerificationTier = ""
	c.ReceivedAt = ""
	return json.Marshal(c)
}

// PeerAttestation is a co-signature by an existing trusted registry vouching for another.
type PeerAttestation struct {
	AttesterID     string   `json:"attester_id" db:"attester_id"`
	AttesterDomain string   `json:"attester_domain,omitempty" db:"-"`
	SubjectID      string   `json:"subject_id" db:"subject_id"`
	SubjectDomain  string   `json:"subject_domain,omitempty" db:"-"`
	VerifiedLayers []string `json:"verified_layers" db:"-"` // stored as TEXT[]
	AttestedAt     string   `json:"attested_at" db:"attested_at"`
	Signature      string   `json:"signature" db:"signature"`
}

// SignableBytes returns canonical JSON for signing.
func (a *PeerAttestation) SignableBytes() ([]byte, error) {
	c := *a
	c.Signature = ""
	return json.Marshal(c)
}
