// Package store provides the PostgreSQL-backed storage layer for registry records,
// gossip entries, tombstones, and attestations.
//
// PostgreSQL is used for all node sizes:
//   - Concurrent writes, strong ACID guarantees
//   - Built-in full-text search (tsvector/tsquery)
//   - pgvector extension ready for embedding storage
//   - Excellent indexing for large datasets (100K-10M agents)
package store

import (
	"time"

	"github.com/agentdns/agent-dns/internal/models"
)

// Store defines the interface for persistent storage of registry data.
// Implementations must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Close closes the database connection pool.
	Close() error

	// --- Agent CRUD ---

	// CreateAgent inserts a new agent registry record.
	CreateAgent(agent *models.RegistryRecord) error

	// GetEntity retrieves an agent by ID from local storage.
	// Returns nil, nil if the agent is not found.
	GetEntity(agentID string) (*models.RegistryRecord, error)

	// UpdateEntity updates an existing agent's registry record.
	UpdateEntity(agent *models.RegistryRecord) error

	// DeleteEntity removes an agent from local storage.
	DeleteEntity(agentID string, owner string) error

	// ListEntities returns all local agents, optionally filtered by category.
	ListEntities(category string, limit, offset int) ([]*models.RegistryRecord, error)

	// CountEntities returns the number of local agents.
	CountEntities() (int, error)

	// SearchAgentsByKeyword performs a keyword search on local agents.
	SearchAgentsByKeyword(query string, category string, tags []string, limit int) ([]*models.RegistryRecord, error)

	// ListEntitiesByDeveloper returns all agents registered by a specific developer.
	ListEntitiesByDeveloper(developerID string, limit, offset int) ([]*models.RegistryRecord, error)

	// --- Developer CRUD ---

	// CreateDeveloper inserts a new developer identity record.
	CreateDeveloper(dev *models.DeveloperRecord) error

	// GetDeveloper retrieves a developer by ID from local storage.
	// Returns nil, nil if the developer is not found.
	GetDeveloper(developerID string) (*models.DeveloperRecord, error)

	// GetDeveloperByPublicKey retrieves a developer by their public key.
	// Returns nil, nil if not found.
	GetDeveloperByPublicKey(publicKey string) (*models.DeveloperRecord, error)

	// UpdateDeveloper updates an existing developer's record.
	UpdateDeveloper(dev *models.DeveloperRecord) error

	// DeleteDeveloper removes a developer from local storage.
	DeleteDeveloper(developerID string) error

	// CountDevelopers returns the number of locally registered developers.
	CountDevelopers() (int, error)

	// --- Gossip Entries ---

	// GetGossipEntry retrieves a gossip entry by agent ID.
	// Returns nil, nil if the entry is not found.
	GetGossipEntry(agentID string) (*models.GossipEntry, error)

	// UpsertGossipEntry inserts or updates a gossip entry from a remote registry.
	UpsertGossipEntry(entry *models.GossipEntry) error

	// SearchGossipByKeyword searches gossip entries by keyword.
	SearchGossipByKeyword(query string, category string, tags []string, limit int) ([]*models.GossipEntry, error)

	// TombstoneGossipEntry marks a gossip entry as tombstoned.
	TombstoneGossipEntry(agentID string) error

	// CountGossipEntries returns the number of active (non-tombstoned) gossip entries.
	CountGossipEntries() (int, error)

	// --- Gossip Developer Entries ---

	// UpsertGossipDeveloper inserts or updates a developer identity from gossip.
	UpsertGossipDeveloper(entry *models.GossipDeveloperEntry) error

	// GetGossipDeveloper retrieves a gossip developer entry by ID.
	GetGossipDeveloper(developerID string) (*models.GossipDeveloperEntry, error)

	// TombstoneGossipDeveloper marks a gossip developer entry as tombstoned.
	TombstoneGossipDeveloper(developerID string) error

	// --- Tombstones ---

	// CreateTombstone creates a tombstone record for a deregistered agent.
	CreateTombstone(t *models.Tombstone) error

	// CleanExpiredTombstones removes tombstones past their expiry.
	// Returns the number of tombstones removed.
	CleanExpiredTombstones() (int, error)

	// --- Node Metadata ---

	// SetMeta stores a key-value pair in node metadata.
	SetMeta(key, value string) error

	// GetMeta retrieves a value from node metadata.
	// Returns empty string if key not found.
	GetMeta(key string) (string, error)

	// --- Agent Heartbeat Liveness ---

	// UpdateEntityHeartbeat sets an agent's last_heartbeat to now and status to active.
	UpdateEntityHeartbeat(agentID string) error

	// MarkInactiveAgents marks agents as inactive whose last heartbeat is older than threshold.
	// Returns the list of agent IDs that were newly marked inactive.
	MarkInactiveAgents(threshold time.Duration) ([]string, error)

	// UpdateGossipEntryStatus updates the status field on a gossip entry.
	UpdateGossipEntryStatus(agentID, status string) error

	// --- Tags & Categories ---

	// GetAllTags returns all unique tags from local agents.
	GetAllTags() ([]string, error)

	// GetAllCategories returns all unique categories from local agents.
	GetAllCategories() ([]string, error)

	// --- ZNS Handle Operations ---

	// ClaimHandle assigns a handle to a developer. Returns error if handle is taken.
	ClaimHandle(developerID, handle, homeRegistry string) error

	// GetDeveloperByHandle retrieves a developer by their handle on a specific registry.
	GetDeveloperByHandle(handle, homeRegistry string) (*models.DeveloperRecord, error)

	// ReleaseHandle removes a handle from a developer.
	ReleaseHandle(developerID, handle string) error

	// UpdateHandleVerification marks a handle as verified with a method and proof.
	UpdateHandleVerification(developerID string, verified bool, method, proof string) error

	// --- ZNS Name Binding Operations ---

	// CreateZNSName creates a new ZNS name binding.
	CreateZNSName(name *models.ZNSName) error

	// GetZNSName retrieves a ZNS name binding by FQAN.
	GetZNSName(fqan string) (*models.ZNSName, error)

	// GetZNSNameByParts retrieves a ZNS name by developer handle + agent name + registry host.
	GetZNSNameByParts(devHandle, agentName, registryHost string) (*models.ZNSName, error)

	// GetZNSNameByAgentID retrieves a ZNS name by agent ID.
	GetZNSNameByAgentID(agentID string) (*models.ZNSName, error)

	// GetZNSNamesByAgentIDs retrieves ZNS name bindings for multiple agent IDs in a single query.
	GetZNSNamesByAgentIDs(agentIDs []string) (map[string]*models.ZNSName, error)

	// UpdateZNSName updates an existing ZNS name binding.
	UpdateZNSName(name *models.ZNSName) error

	// DeleteZNSName removes a ZNS name binding.
	DeleteZNSName(fqan string) error

	// ListZNSNamesByDeveloper lists all ZNS names under a developer handle on a registry.
	ListZNSNamesByDeveloper(devHandle, registryHost string) ([]*models.ZNSName, error)

	// --- ZNS Version Operations ---

	// CreateZNSVersion creates a new version record.
	CreateZNSVersion(version *models.ZNSVersion) error

	// GetZNSVersions lists all versions for a FQAN.
	GetZNSVersions(fqan string) ([]*models.ZNSVersion, error)

	// GetZNSVersion retrieves a specific version.
	GetZNSVersion(fqan, version string) (*models.ZNSVersion, error)

	// --- ZNS Gossip ---

	// UpsertGossipZNSName inserts or updates a gossip ZNS name entry.
	UpsertGossipZNSName(entry *models.GossipZNSName) error

	// GetGossipZNSName retrieves a gossip ZNS name by FQAN.
	GetGossipZNSName(fqan string) (*models.GossipZNSName, error)

	// GetGossipZNSNameByParts retrieves a gossip ZNS name by developer handle + agent name.
	GetGossipZNSNameByParts(devHandle, agentName string) (*models.GossipZNSName, error)

	// TombstoneGossipZNSName marks a gossip ZNS name as tombstoned.
	TombstoneGossipZNSName(fqan string) error

	// --- Registry Verification ---

	// UpsertRegistryProof stores or updates a registry identity proof.
	UpsertRegistryProof(proof *models.RegistryIdentityProof) error

	// GetRegistryProof retrieves a registry proof by registry ID.
	GetRegistryProof(registryID string) (*models.RegistryIdentityProof, error)

	// GetRegistryProofByDomain retrieves a registry proof by domain name.
	GetRegistryProofByDomain(domain string) (*models.RegistryIdentityProof, error)

	// CreatePeerAttestation stores a peer attestation.
	CreatePeerAttestation(att *models.PeerAttestation) error

	// CountPeerAttestations counts attestations for a subject registry.
	CountPeerAttestations(subjectID string) (int, error)

	// UpdateRegistryVerificationTier updates the verification tier of a registry proof.
	UpdateRegistryVerificationTier(registryID, tier string) error
}

// New creates a new PostgreSQL-backed Store.
// dsn is a PostgreSQL connection string, e.g.:
//
//	"postgres://user:pass@localhost:5432/agentdns?sslmode=disable"
func New(dsn string) (Store, error) {
	return NewPostgresStore(dsn)
}
