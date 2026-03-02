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

	// --- Gossip Entries ---

	// UpsertGossipEntry inserts or updates a gossip entry from a remote registry.
	UpsertGossipEntry(entry *models.GossipEntry) error

	// SearchGossipByKeyword searches gossip entries by keyword.
	SearchGossipByKeyword(query string, category string, tags []string, limit int) ([]*models.GossipEntry, error)

	// TombstoneGossipEntry marks a gossip entry as tombstoned.
	TombstoneGossipEntry(agentID string) error

	// CountGossipEntries returns the number of active (non-tombstoned) gossip entries.
	CountGossipEntries() (int, error)

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
