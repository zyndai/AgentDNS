package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentdns/agent-dns/internal/models"
)

// PostgresStore implements Store backed by PostgreSQL.
// Uses pgx connection pool for high-concurrency workloads.
// Recommended for all node sizes — handles <1K to 10M+ agents.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time check that PostgresStore implements Store.
var _ Store = (*PostgresStore)(nil)

// NewPostgresStore creates a new Store backed by PostgreSQL.
// dsn example: "postgres://user:pass@localhost:5432/agentdns?sslmode=disable"
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse postgres DSN: %w", err)
	}

	// Connection pool tuning
	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres connection pool: %w", err)
	}

	// Verify connectivity
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	s := &PostgresStore{pool: pool}
	if err := s.migrate(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return s, nil
}

// migrate creates the database schema.
func (s *PostgresStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agents (
		agent_id      TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		owner         TEXT NOT NULL,
		agent_url     TEXT NOT NULL,
		category      TEXT NOT NULL,
		tags          JSONB NOT NULL DEFAULT '[]',
		summary       TEXT NOT NULL DEFAULT '',
		public_key    TEXT NOT NULL,
		home_registry TEXT NOT NULL,
		registered_at TIMESTAMPTZ NOT NULL,
		updated_at    TIMESTAMPTZ NOT NULL,
		ttl           INTEGER NOT NULL DEFAULT 86400,
		signature     TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_agents_category ON agents(category);
	CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name);
	CREATE INDEX IF NOT EXISTS idx_agents_owner ON agents(owner);
	CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_agents_tags ON agents USING GIN(tags);

	CREATE TABLE IF NOT EXISTS gossip_entries (
		agent_id      TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		category      TEXT NOT NULL,
		tags          JSONB NOT NULL DEFAULT '[]',
		summary       TEXT NOT NULL DEFAULT '',
		home_registry TEXT NOT NULL,
		agent_url     TEXT NOT NULL,
		received_at   TIMESTAMPTZ NOT NULL,
		tombstoned    BOOLEAN NOT NULL DEFAULT FALSE,
		tombstone_at  TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_gossip_category ON gossip_entries(category);
	CREATE INDEX IF NOT EXISTS idx_gossip_tombstoned ON gossip_entries(tombstoned);

	CREATE TABLE IF NOT EXISTS tombstones (
		agent_id   TEXT PRIMARY KEY,
		reason     TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		signature  TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_tombstones_expires ON tombstones(expires_at);

	CREATE TABLE IF NOT EXISTS attestations (
		agent_id          TEXT NOT NULL,
		observer_registry TEXT NOT NULL,
		period            TEXT NOT NULL,
		invocations       BIGINT NOT NULL DEFAULT 0,
		successes         BIGINT NOT NULL DEFAULT 0,
		failures          BIGINT NOT NULL DEFAULT 0,
		avg_latency_ms    DOUBLE PRECISION NOT NULL DEFAULT 0,
		avg_rating        DOUBLE PRECISION NOT NULL DEFAULT 0,
		signature         TEXT NOT NULL,
		PRIMARY KEY (agent_id, observer_registry, period)
	);

	CREATE TABLE IF NOT EXISTS node_meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	-- Schema evolution: add schema_version column if not present
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS schema_version TEXT NOT NULL DEFAULT '1.0';

	-- Developer identity table
	CREATE TABLE IF NOT EXISTS developers (
		developer_id    TEXT PRIMARY KEY,
		name            TEXT NOT NULL,
		public_key      TEXT NOT NULL UNIQUE,
		profile_url     TEXT NOT NULL DEFAULT '',
		github          TEXT NOT NULL DEFAULT '',
		home_registry   TEXT NOT NULL,
		schema_version  TEXT NOT NULL DEFAULT '1.0',
		registered_at   TIMESTAMPTZ NOT NULL,
		updated_at      TIMESTAMPTZ NOT NULL,
		signature       TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_developers_public_key ON developers(public_key);
	CREATE INDEX IF NOT EXISTS idx_developers_name ON developers(name);

	-- Developer fields on agents table
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS developer_id TEXT;
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS agent_index INTEGER;
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS developer_proof JSONB;

	CREATE INDEX IF NOT EXISTS idx_agents_developer_id ON agents(developer_id);

	-- Developer fields on gossip_entries table
	ALTER TABLE gossip_entries ADD COLUMN IF NOT EXISTS developer_id TEXT;
	ALTER TABLE gossip_entries ADD COLUMN IF NOT EXISTS developer_public_key TEXT;
	ALTER TABLE gossip_entries ADD COLUMN IF NOT EXISTS developer_proof JSONB;

	-- Gossip developer entries table
	CREATE TABLE IF NOT EXISTS gossip_developers (
		developer_id    TEXT PRIMARY KEY,
		name            TEXT NOT NULL,
		public_key      TEXT NOT NULL,
		profile_url     TEXT NOT NULL DEFAULT '',
		github          TEXT NOT NULL DEFAULT '',
		home_registry   TEXT NOT NULL,
		received_at     TIMESTAMPTZ NOT NULL,
		tombstoned      BOOLEAN NOT NULL DEFAULT FALSE,
		tombstone_at    TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_gossip_developers_public_key ON gossip_developers(public_key);

	-- Heartbeat liveness columns
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'unknown';
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS last_heartbeat TIMESTAMPTZ;

	CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
	CREATE INDEX IF NOT EXISTS idx_agents_last_heartbeat ON agents(last_heartbeat);

	ALTER TABLE gossip_entries ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'unknown';
	`

	_, err := s.pool.Exec(context.Background(), schema)
	return err
}

// Close closes the connection pool.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// --- Agent CRUD ---

func (s *PostgresStore) CreateAgent(agent *models.RegistryRecord) error {
	tagsJSON, err := json.Marshal(agent.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	schemaVersion := agent.SchemaVersion
	if schemaVersion == "" {
		schemaVersion = models.CurrentSchemaVersion
	}

	var developerProofJSON []byte
	if agent.DeveloperProof != nil {
		developerProofJSON, err = json.Marshal(agent.DeveloperProof)
		if err != nil {
			return fmt.Errorf("failed to marshal developer_proof: %w", err)
		}
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO agents (agent_id, name, owner, agent_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
			developer_id, agent_index, developer_proof)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		agent.AgentID, agent.Name, agent.Owner, agent.AgentURL, agent.Category,
		string(tagsJSON), agent.Summary, agent.PublicKey, agent.HomeRegistry,
		schemaVersion, agent.RegisteredAt, agent.UpdatedAt, agent.TTL, agent.Signature,
		nilIfEmpty(agent.DeveloperID), agent.AgentIndex, nilIfEmptyBytes(developerProofJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to insert agent: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAgent(agentID string) (*models.RegistryRecord, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT agent_id, name, owner, agent_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
			developer_id, agent_index, developer_proof,
			status, last_heartbeat
		FROM agents WHERE agent_id = $1`, agentID)

	agent := &models.RegistryRecord{}
	var tagsJSON []byte
	var registeredAt, updatedAt time.Time
	var developerID *string
	var agentIndex *int
	var developerProofJSON []byte
	var status *string
	var lastHeartbeat *time.Time
	err := row.Scan(
		&agent.AgentID, &agent.Name, &agent.Owner, &agent.AgentURL,
		&agent.Category, &tagsJSON, &agent.Summary, &agent.PublicKey,
		&agent.HomeRegistry, &agent.SchemaVersion, &registeredAt, &updatedAt,
		&agent.TTL, &agent.Signature,
		&developerID, &agentIndex, &developerProofJSON,
		&status, &lastHeartbeat,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	agent.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
	agent.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)

	if err := json.Unmarshal(tagsJSON, &agent.Tags); err != nil {
		agent.Tags = []string{}
	}

	if developerID != nil {
		agent.DeveloperID = *developerID
	}
	if agentIndex != nil {
		agent.AgentIndex = agentIndex
	}
	if len(developerProofJSON) > 0 {
		agent.DeveloperProof = &models.DeveloperProof{}
		json.Unmarshal(developerProofJSON, agent.DeveloperProof)
	}

	if status != nil {
		agent.Status = *status
	}
	if lastHeartbeat != nil {
		agent.LastHeartbeat = lastHeartbeat.UTC().Format(time.RFC3339)
	}

	return agent, nil
}

func (s *PostgresStore) UpdateAgent(agent *models.RegistryRecord) error {
	tagsJSON, err := json.Marshal(agent.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	ct, err := s.pool.Exec(context.Background(), `
		UPDATE agents SET name=$1, agent_url=$2, category=$3, tags=$4, summary=$5,
			updated_at=$6, ttl=$7, signature=$8, schema_version=$9
		WHERE agent_id = $10 AND owner = $11`,
		agent.Name, agent.AgentURL, agent.Category, string(tagsJSON),
		agent.Summary, agent.UpdatedAt, agent.TTL, agent.Signature,
		agent.SchemaVersion, agent.AgentID, agent.Owner,
	)
	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent not found or not owned by caller")
	}
	return nil
}

func (s *PostgresStore) DeleteAgent(agentID string, owner string) error {
	ct, err := s.pool.Exec(context.Background(),
		`DELETE FROM agents WHERE agent_id = $1 AND owner = $2`, agentID, owner)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent not found or not owned by caller")
	}
	return nil
}

func (s *PostgresStore) ListAgents(category string, limit, offset int) ([]*models.RegistryRecord, error) {
	var query string
	var args []interface{}

	if category != "" {
		query = `
			SELECT agent_id, name, owner, agent_url, category, tags, summary,
				public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature
			FROM agents WHERE category = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{category, limit, offset}
	} else {
		query = `
			SELECT agent_id, name, owner, agent_url, category, tags, summary,
				public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature
			FROM agents ORDER BY updated_at DESC LIMIT $1 OFFSET $2`
		args = []interface{}{limit, offset}
	}

	rows, err := s.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	defer rows.Close()

	var agents []*models.RegistryRecord
	for rows.Next() {
		agent := &models.RegistryRecord{}
		var tagsJSON []byte
		var registeredAt, updatedAt time.Time
		if err := rows.Scan(
			&agent.AgentID, &agent.Name, &agent.Owner, &agent.AgentURL,
			&agent.Category, &tagsJSON, &agent.Summary, &agent.PublicKey,
			&agent.HomeRegistry, &agent.SchemaVersion, &registeredAt, &updatedAt,
			&agent.TTL, &agent.Signature,
		); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agent.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
		agent.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if err := json.Unmarshal(tagsJSON, &agent.Tags); err != nil {
			agent.Tags = []string{}
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func (s *PostgresStore) CountAgents() (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM agents").Scan(&count)
	return count, err
}

func (s *PostgresStore) SearchAgentsByKeyword(query string, category string, tags []string, limit int) ([]*models.RegistryRecord, error) {
	likeQuery := "%" + strings.ToLower(query) + "%"

	baseQuery := `
		SELECT agent_id, name, owner, agent_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature
		FROM agents
		WHERE (LOWER(name) LIKE $1 OR LOWER(summary) LIKE $1 OR tags::text ILIKE $1)`

	args := []interface{}{likeQuery}
	paramIdx := 2

	if category != "" {
		baseQuery += fmt.Sprintf(" AND category = $%d", paramIdx)
		args = append(args, category)
		paramIdx++
	}

	if len(tags) > 0 {
		for _, tag := range tags {
			baseQuery += fmt.Sprintf(" AND tags::text ILIKE $%d", paramIdx)
			args = append(args, "%"+strings.ToLower(tag)+"%")
			paramIdx++
		}
	}

	baseQuery += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d", paramIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(context.Background(), baseQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search agents: %w", err)
	}
	defer rows.Close()

	var agents []*models.RegistryRecord
	for rows.Next() {
		agent := &models.RegistryRecord{}
		var tagsJSON []byte
		var registeredAt, updatedAt time.Time
		if err := rows.Scan(
			&agent.AgentID, &agent.Name, &agent.Owner, &agent.AgentURL,
			&agent.Category, &tagsJSON, &agent.Summary, &agent.PublicKey,
			&agent.HomeRegistry, &agent.SchemaVersion, &registeredAt, &updatedAt,
			&agent.TTL, &agent.Signature,
		); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agent.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
		agent.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if err := json.Unmarshal(tagsJSON, &agent.Tags); err != nil {
			agent.Tags = []string{}
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

// --- Gossip Entries ---

func (s *PostgresStore) UpsertGossipEntry(entry *models.GossipEntry) error {
	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	var developerProofJSON []byte
	if entry.DeveloperProof != nil {
		developerProofJSON, err = json.Marshal(entry.DeveloperProof)
		if err != nil {
			return fmt.Errorf("failed to marshal developer_proof: %w", err)
		}
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO gossip_entries (agent_id, name, category, tags, summary,
			home_registry, agent_url, received_at, tombstoned,
			developer_id, developer_public_key, developer_proof)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT(agent_id) DO UPDATE SET
			name=EXCLUDED.name, category=EXCLUDED.category, tags=EXCLUDED.tags,
			summary=EXCLUDED.summary, agent_url=EXCLUDED.agent_url,
			received_at=EXCLUDED.received_at,
			developer_id=EXCLUDED.developer_id, developer_public_key=EXCLUDED.developer_public_key,
			developer_proof=EXCLUDED.developer_proof`,
		entry.AgentID, entry.Name, entry.Category, string(tagsJSON),
		entry.Summary, entry.HomeRegistry, entry.AgentURL,
		entry.ReceivedAt, entry.Tombstoned,
		nilIfEmpty(entry.DeveloperID), nilIfEmpty(entry.DeveloperPublicKey),
		nilIfEmptyBytes(developerProofJSON),
	)
	return err
}

func (s *PostgresStore) SearchGossipByKeyword(query string, category string, tags []string, limit int) ([]*models.GossipEntry, error) {
	likeQuery := "%" + strings.ToLower(query) + "%"

	baseQuery := `
		SELECT agent_id, name, category, tags, summary, home_registry, agent_url, received_at, tombstoned
		FROM gossip_entries
		WHERE tombstoned = FALSE AND (LOWER(name) LIKE $1 OR LOWER(summary) LIKE $1 OR tags::text ILIKE $1)`

	args := []interface{}{likeQuery}
	paramIdx := 2

	if category != "" {
		baseQuery += fmt.Sprintf(" AND category = $%d", paramIdx)
		args = append(args, category)
		paramIdx++
	}

	if len(tags) > 0 {
		for _, tag := range tags {
			baseQuery += fmt.Sprintf(" AND tags::text ILIKE $%d", paramIdx)
			args = append(args, "%"+strings.ToLower(tag)+"%")
			paramIdx++
		}
	}

	baseQuery += fmt.Sprintf(" ORDER BY received_at DESC LIMIT $%d", paramIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(context.Background(), baseQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search gossip: %w", err)
	}
	defer rows.Close()

	var entries []*models.GossipEntry
	for rows.Next() {
		entry := &models.GossipEntry{}
		var tagsJSON []byte
		var receivedAt time.Time
		if err := rows.Scan(
			&entry.AgentID, &entry.Name, &entry.Category, &tagsJSON,
			&entry.Summary, &entry.HomeRegistry, &entry.AgentURL,
			&receivedAt, &entry.Tombstoned,
		); err != nil {
			return nil, fmt.Errorf("failed to scan gossip entry: %w", err)
		}
		entry.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
		if err := json.Unmarshal(tagsJSON, &entry.Tags); err != nil {
			entry.Tags = []string{}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *PostgresStore) TombstoneGossipEntry(agentID string) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE gossip_entries SET tombstoned = TRUE, tombstone_at = $1 WHERE agent_id = $2`,
		time.Now().UTC(), agentID)
	return err
}

func (s *PostgresStore) CountGossipEntries() (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM gossip_entries WHERE tombstoned = FALSE").Scan(&count)
	return count, err
}

// --- Tombstones ---

func (s *PostgresStore) CreateTombstone(t *models.Tombstone) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO tombstones (agent_id, reason, created_at, expires_at, signature)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(agent_id) DO UPDATE SET
			reason=EXCLUDED.reason, created_at=EXCLUDED.created_at,
			expires_at=EXCLUDED.expires_at, signature=EXCLUDED.signature`,
		t.AgentID, t.Reason, t.CreatedAt, t.ExpiresAt, t.Signature,
	)
	return err
}

func (s *PostgresStore) CleanExpiredTombstones() (int, error) {
	now := time.Now().UTC()
	ct, err := s.pool.Exec(context.Background(),
		"DELETE FROM tombstones WHERE expires_at < $1", now)
	if err != nil {
		return 0, err
	}

	// Also clean tombstoned gossip entries
	s.pool.Exec(context.Background(),
		"DELETE FROM gossip_entries WHERE tombstoned = TRUE AND tombstone_at < $1", now)

	return int(ct.RowsAffected()), nil
}

// --- Attestations ---

func (s *PostgresStore) UpsertAttestation(a *models.ReputationAttestation) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO attestations (agent_id, observer_registry, period, invocations,
			successes, failures, avg_latency_ms, avg_rating, signature)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT(agent_id, observer_registry, period) DO UPDATE SET
			invocations=EXCLUDED.invocations, successes=EXCLUDED.successes,
			failures=EXCLUDED.failures, avg_latency_ms=EXCLUDED.avg_latency_ms,
			avg_rating=EXCLUDED.avg_rating, signature=EXCLUDED.signature`,
		a.AgentID, a.ObserverRegistry, a.Period, a.Invocations,
		a.Successes, a.Failures, a.AvgLatencyMs, a.AvgRating, a.Signature,
	)
	return err
}

func (s *PostgresStore) GetAttestations(agentID string) ([]*models.ReputationAttestation, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT agent_id, observer_registry, period, invocations, successes,
			failures, avg_latency_ms, avg_rating, signature
		FROM attestations WHERE agent_id = $1`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attestations []*models.ReputationAttestation
	for rows.Next() {
		a := &models.ReputationAttestation{}
		if err := rows.Scan(
			&a.AgentID, &a.ObserverRegistry, &a.Period, &a.Invocations,
			&a.Successes, &a.Failures, &a.AvgLatencyMs, &a.AvgRating,
			&a.Signature,
		); err != nil {
			return nil, err
		}
		attestations = append(attestations, a)
	}
	return attestations, nil
}

// --- Node Metadata ---

func (s *PostgresStore) SetMeta(key, value string) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO node_meta (key, value) VALUES ($1, $2)
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, key, value)
	return err
}

func (s *PostgresStore) GetMeta(key string) (string, error) {
	var value string
	err := s.pool.QueryRow(context.Background(),
		"SELECT value FROM node_meta WHERE key = $1", key).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return value, err
}

// --- Developer CRUD ---

func (s *PostgresStore) CreateDeveloper(dev *models.DeveloperRecord) error {
	schemaVersion := dev.SchemaVersion
	if schemaVersion == "" {
		schemaVersion = models.CurrentSchemaVersion
	}

	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO developers (developer_id, name, public_key, profile_url, github,
			home_registry, schema_version, registered_at, updated_at, signature)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		dev.DeveloperID, dev.Name, dev.PublicKey, dev.ProfileURL, dev.GitHub,
		dev.HomeRegistry, schemaVersion, dev.RegisteredAt, dev.UpdatedAt, dev.Signature,
	)
	if err != nil {
		return fmt.Errorf("failed to insert developer: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetDeveloper(developerID string) (*models.DeveloperRecord, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT developer_id, name, public_key, profile_url, github,
			home_registry, schema_version, registered_at, updated_at, signature
		FROM developers WHERE developer_id = $1`, developerID)

	dev := &models.DeveloperRecord{}
	var registeredAt, updatedAt time.Time
	err := row.Scan(
		&dev.DeveloperID, &dev.Name, &dev.PublicKey, &dev.ProfileURL, &dev.GitHub,
		&dev.HomeRegistry, &dev.SchemaVersion, &registeredAt, &updatedAt, &dev.Signature,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get developer: %w", err)
	}

	dev.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
	dev.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return dev, nil
}

func (s *PostgresStore) GetDeveloperByPublicKey(publicKey string) (*models.DeveloperRecord, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT developer_id, name, public_key, profile_url, github,
			home_registry, schema_version, registered_at, updated_at, signature
		FROM developers WHERE public_key = $1`, publicKey)

	dev := &models.DeveloperRecord{}
	var registeredAt, updatedAt time.Time
	err := row.Scan(
		&dev.DeveloperID, &dev.Name, &dev.PublicKey, &dev.ProfileURL, &dev.GitHub,
		&dev.HomeRegistry, &dev.SchemaVersion, &registeredAt, &updatedAt, &dev.Signature,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get developer by public key: %w", err)
	}

	dev.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
	dev.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return dev, nil
}

func (s *PostgresStore) UpdateDeveloper(dev *models.DeveloperRecord) error {
	ct, err := s.pool.Exec(context.Background(), `
		UPDATE developers SET name=$1, profile_url=$2, github=$3,
			updated_at=$4, signature=$5
		WHERE developer_id = $6`,
		dev.Name, dev.ProfileURL, dev.GitHub,
		dev.UpdatedAt, dev.Signature, dev.DeveloperID,
	)
	if err != nil {
		return fmt.Errorf("failed to update developer: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("developer not found")
	}
	return nil
}

func (s *PostgresStore) DeleteDeveloper(developerID string) error {
	ct, err := s.pool.Exec(context.Background(),
		`DELETE FROM developers WHERE developer_id = $1`, developerID)
	if err != nil {
		return fmt.Errorf("failed to delete developer: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("developer not found")
	}
	return nil
}

func (s *PostgresStore) CountDevelopers() (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM developers").Scan(&count)
	return count, err
}

func (s *PostgresStore) ListAgentsByDeveloper(developerID string, limit, offset int) ([]*models.RegistryRecord, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT agent_id, name, owner, agent_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
			developer_id, agent_index, developer_proof,
			status, last_heartbeat
		FROM agents WHERE developer_id = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3`,
		developerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents by developer: %w", err)
	}
	defer rows.Close()

	return scanAgentRows(rows)
}

// --- Gossip Developer Entries ---

func (s *PostgresStore) UpsertGossipDeveloper(entry *models.GossipDeveloperEntry) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO gossip_developers (developer_id, name, public_key, profile_url, github,
			home_registry, received_at, tombstoned)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(developer_id) DO UPDATE SET
			name=EXCLUDED.name, public_key=EXCLUDED.public_key,
			profile_url=EXCLUDED.profile_url, github=EXCLUDED.github,
			received_at=EXCLUDED.received_at`,
		entry.DeveloperID, entry.Name, entry.PublicKey, entry.ProfileURL, entry.GitHub,
		entry.HomeRegistry, entry.ReceivedAt, entry.Tombstoned,
	)
	return err
}

func (s *PostgresStore) GetGossipDeveloper(developerID string) (*models.GossipDeveloperEntry, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT developer_id, name, public_key, profile_url, github,
			home_registry, received_at, tombstoned
		FROM gossip_developers WHERE developer_id = $1`, developerID)

	entry := &models.GossipDeveloperEntry{}
	var receivedAt time.Time
	err := row.Scan(
		&entry.DeveloperID, &entry.Name, &entry.PublicKey, &entry.ProfileURL, &entry.GitHub,
		&entry.HomeRegistry, &receivedAt, &entry.Tombstoned,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get gossip developer: %w", err)
	}
	entry.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
	return entry, nil
}

func (s *PostgresStore) TombstoneGossipDeveloper(developerID string) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE gossip_developers SET tombstoned = TRUE, tombstone_at = $1 WHERE developer_id = $2`,
		time.Now().UTC(), developerID)
	return err
}

// --- Helpers ---

// nilIfEmpty returns nil if s is empty, otherwise returns &s.
// Used for nullable TEXT columns.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nilIfEmptyBytes returns nil if b is empty/nil, otherwise returns the string form.
// Used for nullable JSONB columns.
func nilIfEmptyBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// scanAgentRows scans rows into RegistryRecord slices, handling developer fields.
func scanAgentRows(rows pgx.Rows) ([]*models.RegistryRecord, error) {
	var agents []*models.RegistryRecord
	for rows.Next() {
		agent := &models.RegistryRecord{}
		var tagsJSON []byte
		var registeredAt, updatedAt time.Time
		var developerID *string
		var agentIndex *int
		var developerProofJSON []byte
		var status *string
		var lastHeartbeat *time.Time
		if err := rows.Scan(
			&agent.AgentID, &agent.Name, &agent.Owner, &agent.AgentURL,
			&agent.Category, &tagsJSON, &agent.Summary, &agent.PublicKey,
			&agent.HomeRegistry, &agent.SchemaVersion, &registeredAt, &updatedAt,
			&agent.TTL, &agent.Signature,
			&developerID, &agentIndex, &developerProofJSON,
			&status, &lastHeartbeat,
		); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agent.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
		agent.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if err := json.Unmarshal(tagsJSON, &agent.Tags); err != nil {
			agent.Tags = []string{}
		}
		if developerID != nil {
			agent.DeveloperID = *developerID
		}
		if agentIndex != nil {
			agent.AgentIndex = agentIndex
		}
		if len(developerProofJSON) > 0 {
			agent.DeveloperProof = &models.DeveloperProof{}
			json.Unmarshal(developerProofJSON, agent.DeveloperProof)
		}
		if status != nil {
			agent.Status = *status
		}
		if lastHeartbeat != nil {
			agent.LastHeartbeat = lastHeartbeat.UTC().Format(time.RFC3339)
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

// --- Heartbeat / Liveness ---

func (s *PostgresStore) UpdateAgentHeartbeat(agentID string, heartbeatTime time.Time) error {
	ct, err := s.pool.Exec(context.Background(), `
		UPDATE agents SET last_heartbeat = $1, status = 'online'
		WHERE agent_id = $2`,
		heartbeatTime.UTC(), agentID)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent not found")
	}
	return nil
}

func (s *PostgresStore) MarkInactiveAgents(cutoff time.Time) ([]string, error) {
	rows, err := s.pool.Query(context.Background(), `
		UPDATE agents SET status = 'inactive'
		WHERE status = 'online' AND last_heartbeat IS NOT NULL AND last_heartbeat < $1
		RETURNING agent_id`,
		cutoff.UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to mark inactive agents: %w", err)
	}
	defer rows.Close()

	var agentIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		agentIDs = append(agentIDs, id)
	}
	return agentIDs, nil
}

func (s *PostgresStore) UpdateGossipEntryStatus(agentID string, status string) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE gossip_entries SET status = $1 WHERE agent_id = $2`,
		status, agentID)
	return err
}

func (s *PostgresStore) CountAgentsByStatus() (online, inactive, unknown int, err error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT status, COUNT(*) FROM agents GROUP BY status`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			continue
		}
		switch status {
		case "online":
			online = count
		case "inactive":
			inactive = count
		default:
			unknown += count
		}
	}
	return
}

func (s *PostgresStore) GetAllTags() ([]string, error) {
	rows, err := s.pool.Query(context.Background(),
		"SELECT DISTINCT jsonb_array_elements_text(tags) AS tag FROM agents ORDER BY tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			continue
		}
		result = append(result, tag)
	}
	return result, nil
}

func (s *PostgresStore) GetAllCategories() ([]string, error) {
	rows, err := s.pool.Query(context.Background(),
		"SELECT DISTINCT category FROM agents ORDER BY category")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []string
	for rows.Next() {
		var cat string
		if err := rows.Scan(&cat); err != nil {
			continue
		}
		categories = append(categories, cat)
	}
	return categories, nil
}
