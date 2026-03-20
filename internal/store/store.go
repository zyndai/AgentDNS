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

	// GetAgent retrieves an agent by ID from local storage.
	// Returns nil, nil if the agent is not found.
	GetAgent(agentID string) (*models.RegistryRecord, error)

	// UpdateAgent updates an existing agent's registry record.
	UpdateAgent(agent *models.RegistryRecord) error

	// DeleteAgent removes an agent from local storage.
	DeleteAgent(agentID string, owner string) error

	// ListAgents returns all local agents, optionally filtered by category.
	ListAgents(category string, limit, offset int) ([]*models.RegistryRecord, error)

	// CountAgents returns the number of local agents.
	CountAgents() (int, error)

	// SearchAgentsByKeyword performs a keyword search on local agents.
	SearchAgentsByKeyword(query string, category string, tags []string, limit int) ([]*models.RegistryRecord, error)

	// ListAgentsByDeveloper returns all agents registered by a specific developer.
	ListAgentsByDeveloper(developerID string, limit, offset int) ([]*models.RegistryRecord, error)

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

	// --- Attestations ---

	// UpsertAttestation stores a reputation attestation.
	UpsertAttestation(a *models.ReputationAttestation) error

	// GetAttestations retrieves all attestations for an agent.
	GetAttestations(agentID string) ([]*models.ReputationAttestation, error)

	// --- Node Metadata ---

	// SetMeta stores a key-value pair in node metadata.
	SetMeta(key, value string) error

	// GetMeta retrieves a value from node metadata.
	// Returns empty string if key not found.
	GetMeta(key string) (string, error)

	// --- Agent Heartbeat Liveness ---

	// UpdateAgentHeartbeat sets an agent's last_heartbeat to now and status to active.
	UpdateAgentHeartbeat(agentID string) error

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
}

// New creates a new PostgreSQL-backed Store.
// dsn is a PostgreSQL connection string, e.g.:
//
//	"postgres://user:pass@localhost:5432/agentdns?sslmode=disable"
func New(dsn string) (Store, error) {
	return NewPostgresStore(dsn)
}
