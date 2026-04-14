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

	// Run pending embedded migrations FIRST — these rename legacy columns
	// (agent_id → entity_id etc.) on existing databases. On fresh installs
	// they no-op via DO block exception handlers. This has to happen before
	// migrate() because migrate()'s CREATE INDEX IF NOT EXISTS statements
	// reference entity_* columns by name and would fail on an old schema.
	if err := runMigrations(context.Background(), pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	if err := s.migrate(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return s, nil
}

// migrate creates the database schema.
func (s *PostgresStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS entities (
		entity_id      TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		owner         TEXT NOT NULL,
		entity_url     TEXT NOT NULL,
		category      TEXT NOT NULL,
		tags          JSONB NOT NULL DEFAULT '[]',
		summary       TEXT NOT NULL DEFAULT '',
		public_key    TEXT NOT NULL,
		home_registry TEXT NOT NULL,
		registered_at TIMESTAMPTZ NOT NULL,
		updated_at    TIMESTAMPTZ NOT NULL,
		ttl           INTEGER NOT NULL DEFAULT 86400,
		signature     TEXT NOT NULL,
		codebase_hash TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_entities_category ON entities(category);
	CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);
	CREATE INDEX IF NOT EXISTS idx_entities_owner ON entities(owner);
	CREATE INDEX IF NOT EXISTS idx_entities_updated_at ON entities(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_entities_tags ON entities USING GIN(tags);

	CREATE TABLE IF NOT EXISTS gossip_entities (
		entity_id      TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		category      TEXT NOT NULL,
		tags          JSONB NOT NULL DEFAULT '[]',
		summary       TEXT NOT NULL DEFAULT '',
		home_registry TEXT NOT NULL,
		entity_url     TEXT NOT NULL,
		received_at   TIMESTAMPTZ NOT NULL,
		tombstoned    BOOLEAN NOT NULL DEFAULT FALSE,
		tombstone_at  TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_gossip_category ON gossip_entities(category);
	CREATE INDEX IF NOT EXISTS idx_gossip_tombstoned ON gossip_entities(tombstoned);

	CREATE TABLE IF NOT EXISTS tombstones (
		entity_id   TEXT PRIMARY KEY,
		reason     TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		signature  TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_tombstones_expires ON tombstones(expires_at);

	CREATE TABLE IF NOT EXISTS attestations (
		entity_id          TEXT NOT NULL,
		observer_registry TEXT NOT NULL,
		period            TEXT NOT NULL,
		invocations       BIGINT NOT NULL DEFAULT 0,
		successes         BIGINT NOT NULL DEFAULT 0,
		failures          BIGINT NOT NULL DEFAULT 0,
		avg_latency_ms    DOUBLE PRECISION NOT NULL DEFAULT 0,
		avg_rating        DOUBLE PRECISION NOT NULL DEFAULT 0,
		signature         TEXT NOT NULL,
		PRIMARY KEY (entity_id, observer_registry, period)
	);

	CREATE TABLE IF NOT EXISTS node_meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	-- Schema evolution: add schema_version column if not present
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS schema_version TEXT NOT NULL DEFAULT '1.0';

	-- Schema evolution: add codebase_hash column if not present
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS codebase_hash TEXT NOT NULL DEFAULT '';

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
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS developer_id TEXT;
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS entity_index INTEGER;
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS developer_proof JSONB;

	CREATE INDEX IF NOT EXISTS idx_entities_developer_id ON entities(developer_id);

	-- Developer fields on gossip_entities table
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS developer_id TEXT;
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS developer_public_key TEXT;
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS developer_proof JSONB;

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

	-- Agent heartbeat liveness
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS last_heartbeat TIMESTAMPTZ;
	CREATE INDEX IF NOT EXISTS idx_entities_status ON entities(status);
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'inactive';

	-- Origin registry public key pinning for gossip authorization
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS origin_public_key TEXT;

	CREATE INDEX IF NOT EXISTS idx_gossip_entities_status ON gossip_entities(status);

	-- ============================================================
	-- ZNS (Zynd Naming Service) schema extensions
	-- ============================================================

	-- Developer handle columns
	ALTER TABLE developers ADD COLUMN IF NOT EXISTS dev_handle TEXT;
	ALTER TABLE developers ADD COLUMN IF NOT EXISTS dev_handle_verified BOOLEAN DEFAULT FALSE;
	ALTER TABLE developers ADD COLUMN IF NOT EXISTS verification_method TEXT;
	ALTER TABLE developers ADD COLUMN IF NOT EXISTS verification_proof TEXT;
	CREATE UNIQUE INDEX IF NOT EXISTS idx_developers_handle
		ON developers(dev_handle, home_registry) WHERE dev_handle IS NOT NULL;

	-- Gossip developer handle columns
	ALTER TABLE gossip_developers ADD COLUMN IF NOT EXISTS dev_handle TEXT;
	ALTER TABLE gossip_developers ADD COLUMN IF NOT EXISTS dev_handle_verified BOOLEAN DEFAULT FALSE;
	ALTER TABLE gossip_developers ADD COLUMN IF NOT EXISTS verification_method TEXT;
	ALTER TABLE gossip_developers ADD COLUMN IF NOT EXISTS verification_proof TEXT;

	-- ZNS name bindings
	CREATE TABLE IF NOT EXISTS zns_names (
		fqan             TEXT PRIMARY KEY,
		entity_name       TEXT NOT NULL,
		developer_handle TEXT NOT NULL,
		registry_host    TEXT NOT NULL,
		entity_id         TEXT NOT NULL REFERENCES entities(entity_id),
		developer_id     TEXT NOT NULL,
		current_version  TEXT,
		capability_tags  TEXT[] DEFAULT '{}',
		registered_at    TIMESTAMPTZ NOT NULL,
		updated_at       TIMESTAMPTZ NOT NULL,
		signature        TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_zns_entity_id ON zns_names(entity_id);
	CREATE INDEX IF NOT EXISTS idx_zns_developer ON zns_names(developer_handle, registry_host);
	CREATE INDEX IF NOT EXISTS idx_zns_capability ON zns_names USING GIN(capability_tags);

	-- ZNS version history
	CREATE TABLE IF NOT EXISTS zns_versions (
		fqan          TEXT NOT NULL REFERENCES zns_names(fqan) ON DELETE CASCADE,
		version       TEXT NOT NULL,
		entity_id      TEXT NOT NULL,
		build_hash    TEXT,
		registered_at TIMESTAMPTZ NOT NULL,
		signature     TEXT NOT NULL,
		PRIMARY KEY (fqan, version)
	);

	-- Gossip ZNS name entries (from remote registries)
	CREATE TABLE IF NOT EXISTS gossip_zns_names (
		fqan             TEXT PRIMARY KEY,
		entity_name       TEXT NOT NULL,
		developer_handle TEXT NOT NULL,
		registry_host    TEXT NOT NULL,
		entity_id         TEXT NOT NULL,
		current_version  TEXT,
		capability_tags  TEXT[] DEFAULT '{}',
		received_at      TIMESTAMPTZ NOT NULL,
		tombstoned       BOOLEAN DEFAULT FALSE
	);
	CREATE INDEX IF NOT EXISTS idx_gossip_zns_registry ON gossip_zns_names(registry_host);

	-- Registry identity proofs
	CREATE TABLE IF NOT EXISTS registry_identity_proofs (
		registry_id          TEXT PRIMARY KEY,
		domain               TEXT NOT NULL UNIQUE,
		ed25519_public_key   TEXT NOT NULL,
		tls_spki_fingerprint TEXT NOT NULL,
		proof_json           JSONB NOT NULL,
		proof_signature      TEXT NOT NULL,
		verification_tier    TEXT NOT NULL DEFAULT 'self-announced',
		issued_at            TIMESTAMPTZ NOT NULL,
		expires_at           TIMESTAMPTZ NOT NULL,
		received_at          TIMESTAMPTZ NOT NULL
	);

	-- Peer attestations
	CREATE TABLE IF NOT EXISTS peer_attestations (
		attester_id     TEXT NOT NULL,
		subject_id      TEXT NOT NULL,
		verified_layers TEXT[] NOT NULL,
		attested_at     TIMESTAMPTZ NOT NULL,
		signature       TEXT NOT NULL,
		PRIMARY KEY (attester_id, subject_id)
	);
	CREATE INDEX IF NOT EXISTS idx_attestations_subject ON peer_attestations(subject_id);

	-- Services Directory schema extensions
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS entity_type TEXT NOT NULL DEFAULT 'agent';
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS service_endpoint TEXT NOT NULL DEFAULT '';
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS openapi_url TEXT NOT NULL DEFAULT '';
	ALTER TABLE entities ADD COLUMN IF NOT EXISTS entity_pricing JSONB;
	CREATE INDEX IF NOT EXISTS idx_entities_entity_type ON entities(entity_type);

	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS entity_type TEXT NOT NULL DEFAULT 'agent';
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS service_endpoint TEXT NOT NULL DEFAULT '';
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS openapi_url TEXT NOT NULL DEFAULT '';
	ALTER TABLE gossip_entities ADD COLUMN IF NOT EXISTS entity_pricing JSONB;
	CREATE INDEX IF NOT EXISTS idx_gossip_entity_type ON gossip_entities(entity_type);
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

	var pricingJSON []byte
	if agent.EntityPricing != nil {
		pricingJSON, _ = json.Marshal(agent.EntityPricing)
	}

	entityType := agent.EntityType
	if entityType == "" {
		entityType = "agent"
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO entities (entity_id, name, owner, entity_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
			developer_id, entity_index, developer_proof, last_heartbeat,
			entity_type, service_endpoint, openapi_url, entity_pricing)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NOW(),
			$18, $19, $20, $21)`,
		agent.EntityID, agent.Name, agent.Owner, agent.EntityURL, agent.Category,
		string(tagsJSON), agent.Summary, agent.PublicKey, agent.HomeRegistry,
		schemaVersion, agent.RegisteredAt, agent.UpdatedAt, agent.TTL, agent.Signature,
		nilIfEmpty(agent.DeveloperID), agent.EntityIndex, nilIfEmptyBytes(developerProofJSON),
		entityType, agent.ServiceEndpoint, agent.OpenAPIURL, nilIfEmptyBytes(pricingJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to insert agent: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetEntity(agentID string) (*models.RegistryRecord, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT entity_id, name, owner, entity_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
			developer_id, entity_index, developer_proof,
			status, last_heartbeat,
			entity_type, service_endpoint, openapi_url, entity_pricing
		FROM entities WHERE entity_id = $1`, agentID)

	agent := &models.RegistryRecord{}
	var tagsJSON []byte
	var registeredAt, updatedAt time.Time
	var developerID *string
	var agentIndex *int
	var developerProofJSON []byte
	var lastHeartbeat *time.Time
	var pricingBytes []byte
	var entityType, serviceEndpoint, openapiURL *string
	err := row.Scan(
		&agent.EntityID, &agent.Name, &agent.Owner, &agent.EntityURL,
		&agent.Category, &tagsJSON, &agent.Summary, &agent.PublicKey,
		&agent.HomeRegistry, &agent.SchemaVersion, &registeredAt, &updatedAt,
		&agent.TTL, &agent.Signature,
		&developerID, &agentIndex, &developerProofJSON,
		&agent.Status, &lastHeartbeat,
		&entityType, &serviceEndpoint, &openapiURL, &pricingBytes,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	agent.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
	agent.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if lastHeartbeat != nil {
		agent.LastHeartbeat = lastHeartbeat.UTC().Format(time.RFC3339)
	}

	if err := json.Unmarshal(tagsJSON, &agent.Tags); err != nil {
		agent.Tags = []string{}
	}

	if developerID != nil {
		agent.DeveloperID = *developerID
	}
	if agentIndex != nil {
		agent.EntityIndex = agentIndex
	}
	if len(developerProofJSON) > 0 {
		agent.DeveloperProof = &models.DeveloperProof{}
		json.Unmarshal(developerProofJSON, agent.DeveloperProof)
	}
	if entityType != nil {
		agent.EntityType = *entityType
	}
	if serviceEndpoint != nil {
		agent.ServiceEndpoint = *serviceEndpoint
	}
	if openapiURL != nil {
		agent.OpenAPIURL = *openapiURL
	}
	if len(pricingBytes) > 0 {
		agent.EntityPricing = &models.EntityPricing{}
		json.Unmarshal(pricingBytes, agent.EntityPricing)
	}

	return agent, nil
}

func (s *PostgresStore) UpdateEntity(agent *models.RegistryRecord) error {
	tagsJSON, err := json.Marshal(agent.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	entityType := agent.EntityType
	if entityType == "" {
		entityType = "agent"
	}

	var pricingJSON []byte
	if agent.EntityPricing != nil {
		pricingJSON, _ = json.Marshal(agent.EntityPricing)
	}

	ct, err := s.pool.Exec(context.Background(), `
		UPDATE entities SET name=$1, entity_url=$2, category=$3, tags=$4, summary=$5,
			updated_at=$6, ttl=$7, signature=$8, schema_version=$9, codebase_hash=$10,
			entity_type=$11, service_endpoint=$12, openapi_url=$13, entity_pricing=$14
		WHERE entity_id = $15 AND owner = $16`,
		agent.Name, agent.EntityURL, agent.Category, string(tagsJSON),
		agent.Summary, agent.UpdatedAt, agent.TTL, agent.Signature,
		agent.SchemaVersion, agent.CodebaseHash,
		entityType, agent.ServiceEndpoint, agent.OpenAPIURL, nilIfEmptyBytes(pricingJSON),
		agent.EntityID, agent.Owner,
	)
	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return fmt.Errorf("entity not found or not owned by caller")
	}
	return nil
}

func (s *PostgresStore) DeleteEntity(agentID string, owner string) error {
	ct, err := s.pool.Exec(context.Background(),
		`DELETE FROM entities WHERE entity_id = $1 AND owner = $2`, agentID, owner)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return fmt.Errorf("entity not found or not owned by caller")
	}
	return nil
}

func (s *PostgresStore) ListEntities(category string, limit, offset int) ([]*models.RegistryRecord, error) {
	var query string
	var args []interface{}

	if category != "" {
		query = `
			SELECT entity_id, name, owner, entity_url, category, tags, summary,
				public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
				developer_id, entity_index, developer_proof,
				status, last_heartbeat,
				entity_type, service_endpoint, openapi_url, entity_pricing
			FROM entities WHERE category = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{category, limit, offset}
	} else {
		query = `
			SELECT entity_id, name, owner, entity_url, category, tags, summary,
				public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
				developer_id, entity_index, developer_proof,
				status, last_heartbeat,
				entity_type, service_endpoint, openapi_url, entity_pricing
			FROM entities ORDER BY updated_at DESC LIMIT $1 OFFSET $2`
		args = []interface{}{limit, offset}
	}

	rows, err := s.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	defer rows.Close()

	return scanAgentRows(rows)
}

func (s *PostgresStore) CountEntities() (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM entities").Scan(&count)
	return count, err
}

func (s *PostgresStore) SearchAgentsByKeyword(query string, category string, tags []string, limit int) ([]*models.RegistryRecord, error) {
	likeQuery := "%" + strings.ToLower(query) + "%"

	baseQuery := `
		SELECT entity_id, name, owner, entity_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
			developer_id, entity_index, developer_proof,
			status, last_heartbeat,
			entity_type, service_endpoint, openapi_url, entity_pricing
		FROM entities
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

	return scanAgentRows(rows)
}

// --- Gossip Entries ---

func (s *PostgresStore) GetGossipEntry(agentID string) (*models.GossipEntry, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT entity_id, name, category, tags, summary, home_registry, entity_url,
			received_at, tombstoned, status, origin_public_key,
			entity_type, service_endpoint, openapi_url, entity_pricing
		FROM gossip_entities WHERE entity_id = $1`, agentID)

	entry := &models.GossipEntry{}
	var tagsJSON []byte
	var receivedAt time.Time
	var originPubKey *string
	var pricingBytes []byte
	var entityType, serviceEndpoint, openapiURL *string
	err := row.Scan(
		&entry.EntityID, &entry.Name, &entry.Category, &tagsJSON,
		&entry.Summary, &entry.HomeRegistry, &entry.EntityURL,
		&receivedAt, &entry.Tombstoned, &entry.Status, &originPubKey,
		&entityType, &serviceEndpoint, &openapiURL, &pricingBytes,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get gossip entry: %w", err)
	}
	entry.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
	if err := json.Unmarshal(tagsJSON, &entry.Tags); err != nil {
		entry.Tags = []string{}
	}
	if originPubKey != nil {
		entry.OriginPublicKey = *originPubKey
	}
	if entityType != nil {
		entry.EntityType = *entityType
	}
	if serviceEndpoint != nil {
		entry.ServiceEndpoint = *serviceEndpoint
	}
	if openapiURL != nil {
		entry.OpenAPIURL = *openapiURL
	}
	if len(pricingBytes) > 0 {
		entry.EntityPricing = &models.EntityPricing{}
		json.Unmarshal(pricingBytes, entry.EntityPricing)
	}
	return entry, nil
}

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

	status := entry.Status
	if status == "" {
		status = "inactive"
	}

	entityType := entry.EntityType
	if entityType == "" {
		entityType = "agent"
	}

	var pricingJSON []byte
	if entry.EntityPricing != nil {
		pricingJSON, _ = json.Marshal(entry.EntityPricing)
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO gossip_entities (entity_id, name, category, tags, summary,
			home_registry, entity_url, received_at, tombstoned,
			developer_id, developer_public_key, developer_proof,
			origin_public_key, status,
			entity_type, service_endpoint, openapi_url, entity_pricing)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18)
		ON CONFLICT(entity_id) DO UPDATE SET
			name=EXCLUDED.name, category=EXCLUDED.category, tags=EXCLUDED.tags,
			summary=EXCLUDED.summary, entity_url=EXCLUDED.entity_url,
			received_at=EXCLUDED.received_at,
			developer_id=EXCLUDED.developer_id, developer_public_key=EXCLUDED.developer_public_key,
			developer_proof=EXCLUDED.developer_proof,
			origin_public_key=COALESCE(gossip_entities.origin_public_key, EXCLUDED.origin_public_key),
			status=EXCLUDED.status,
			entity_type=EXCLUDED.entity_type, service_endpoint=EXCLUDED.service_endpoint,
			openapi_url=EXCLUDED.openapi_url, entity_pricing=EXCLUDED.entity_pricing`,
		entry.EntityID, entry.Name, entry.Category, string(tagsJSON),
		entry.Summary, entry.HomeRegistry, entry.EntityURL,
		entry.ReceivedAt, entry.Tombstoned,
		nilIfEmpty(entry.DeveloperID), nilIfEmpty(entry.DeveloperPublicKey),
		nilIfEmptyBytes(developerProofJSON),
		nilIfEmpty(entry.OriginPublicKey),
		status,
		entityType, entry.ServiceEndpoint, entry.OpenAPIURL, nilIfEmptyBytes(pricingJSON),
	)
	return err
}

func (s *PostgresStore) SearchGossipByKeyword(query string, category string, tags []string, limit int) ([]*models.GossipEntry, error) {
	likeQuery := "%" + strings.ToLower(query) + "%"

	baseQuery := `
		SELECT entity_id, name, category, tags, summary, home_registry, entity_url, received_at, tombstoned, status,
			entity_type, service_endpoint, openapi_url, entity_pricing
		FROM gossip_entities
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
		var pricingBytes []byte
		var entityType, serviceEndpoint, openapiURL *string
		if err := rows.Scan(
			&entry.EntityID, &entry.Name, &entry.Category, &tagsJSON,
			&entry.Summary, &entry.HomeRegistry, &entry.EntityURL,
			&receivedAt, &entry.Tombstoned, &entry.Status,
			&entityType, &serviceEndpoint, &openapiURL, &pricingBytes,
		); err != nil {
			return nil, fmt.Errorf("failed to scan gossip entry: %w", err)
		}
		entry.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
		if err := json.Unmarshal(tagsJSON, &entry.Tags); err != nil {
			entry.Tags = []string{}
		}
		if entityType != nil {
			entry.EntityType = *entityType
		}
		if serviceEndpoint != nil {
			entry.ServiceEndpoint = *serviceEndpoint
		}
		if openapiURL != nil {
			entry.OpenAPIURL = *openapiURL
		}
		if len(pricingBytes) > 0 {
			entry.EntityPricing = &models.EntityPricing{}
			json.Unmarshal(pricingBytes, entry.EntityPricing)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *PostgresStore) TombstoneGossipEntry(agentID string) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE gossip_entities SET tombstoned = TRUE, tombstone_at = $1 WHERE entity_id = $2`,
		time.Now().UTC(), agentID)
	return err
}

func (s *PostgresStore) CountGossipEntries() (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM gossip_entities WHERE tombstoned = FALSE").Scan(&count)
	return count, err
}

// --- Tombstones ---

func (s *PostgresStore) CreateTombstone(t *models.Tombstone) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO tombstones (entity_id, reason, created_at, expires_at, signature)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(entity_id) DO UPDATE SET
			reason=EXCLUDED.reason, created_at=EXCLUDED.created_at,
			expires_at=EXCLUDED.expires_at, signature=EXCLUDED.signature`,
		t.EntityID, t.Reason, t.CreatedAt, t.ExpiresAt, t.Signature,
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
		"DELETE FROM gossip_entities WHERE tombstoned = TRUE AND tombstone_at < $1", now)

	return int(ct.RowsAffected()), nil
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

	// Include dev_handle if provided (atomic handle claim during registration)
	var handleVal *string
	if dev.DevHandle != "" {
		handleVal = &dev.DevHandle
	}

	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO developers (developer_id, name, public_key, profile_url, github,
			home_registry, schema_version, registered_at, updated_at, signature, dev_handle)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		dev.DeveloperID, dev.Name, dev.PublicKey, dev.ProfileURL, dev.GitHub,
		dev.HomeRegistry, schemaVersion, dev.RegisteredAt, dev.UpdatedAt, dev.Signature, handleVal,
	)
	if err != nil {
		return fmt.Errorf("failed to insert developer: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetDeveloper(developerID string) (*models.DeveloperRecord, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT developer_id, name, public_key, profile_url, github,
			home_registry, schema_version, registered_at, updated_at, signature,
			dev_handle, dev_handle_verified, verification_method, verification_proof
		FROM developers WHERE developer_id = $1`, developerID)

	dev := &models.DeveloperRecord{}
	var registeredAt, updatedAt time.Time
	var devHandle, verMethod, verProof *string
	var devHandleVerified *bool
	err := row.Scan(
		&dev.DeveloperID, &dev.Name, &dev.PublicKey, &dev.ProfileURL, &dev.GitHub,
		&dev.HomeRegistry, &dev.SchemaVersion, &registeredAt, &updatedAt, &dev.Signature,
		&devHandle, &devHandleVerified, &verMethod, &verProof,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get developer: %w", err)
	}

	dev.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
	dev.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if devHandle != nil {
		dev.DevHandle = *devHandle
	}
	if devHandleVerified != nil {
		dev.DevHandleVerified = *devHandleVerified
	}
	if verMethod != nil {
		dev.VerificationMethod = *verMethod
	}
	if verProof != nil {
		dev.VerificationProof = *verProof
	}
	return dev, nil
}

func (s *PostgresStore) GetDeveloperByPublicKey(publicKey string) (*models.DeveloperRecord, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT developer_id, name, public_key, profile_url, github,
			home_registry, schema_version, registered_at, updated_at, signature,
			dev_handle, dev_handle_verified, verification_method, verification_proof
		FROM developers WHERE public_key = $1`, publicKey)

	dev := &models.DeveloperRecord{}
	var registeredAt, updatedAt time.Time
	var devHandle, verMethod, verProof *string
	var devHandleVerified *bool
	err := row.Scan(
		&dev.DeveloperID, &dev.Name, &dev.PublicKey, &dev.ProfileURL, &dev.GitHub,
		&dev.HomeRegistry, &dev.SchemaVersion, &registeredAt, &updatedAt, &dev.Signature,
		&devHandle, &devHandleVerified, &verMethod, &verProof,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get developer by public key: %w", err)
	}

	dev.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
	dev.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if devHandle != nil {
		dev.DevHandle = *devHandle
	}
	if devHandleVerified != nil {
		dev.DevHandleVerified = *devHandleVerified
	}
	if verMethod != nil {
		dev.VerificationMethod = *verMethod
	}
	if verProof != nil {
		dev.VerificationProof = *verProof
	}
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

func (s *PostgresStore) ListEntitiesByDeveloper(developerID string, limit, offset int) ([]*models.RegistryRecord, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT entity_id, name, owner, entity_url, category, tags, summary,
			public_key, home_registry, schema_version, registered_at, updated_at, ttl, signature,
			developer_id, entity_index, developer_proof,
			status, last_heartbeat,
			entity_type, service_endpoint, openapi_url, entity_pricing
		FROM entities WHERE developer_id = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3`,
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

// --- Agent Heartbeat Liveness ---

func (s *PostgresStore) UpdateEntityHeartbeat(agentID string) error {
	ct, err := s.pool.Exec(context.Background(),
		`UPDATE entities SET last_heartbeat = NOW(), status = 'active' WHERE entity_id = $1`, agentID)
	if err != nil {
		return fmt.Errorf("failed to update agent heartbeat: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("entity not found")
	}
	return nil
}

func (s *PostgresStore) MarkInactiveAgents(threshold time.Duration) ([]string, error) {
	cutoff := time.Now().UTC().Add(-threshold)
	rows, err := s.pool.Query(context.Background(), `
		UPDATE entities SET status = 'inactive'
		WHERE status = 'active' AND type != 'service'
		AND (last_heartbeat IS NULL OR last_heartbeat < $1)
		RETURNING entity_id`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to mark inactive agents: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan entity_id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *PostgresStore) UpdateGossipEntryStatus(agentID, status string) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE gossip_entities SET status = $1 WHERE entity_id = $2`, status, agentID)
	if err != nil {
		return fmt.Errorf("failed to update gossip entry status: %w", err)
	}
	return nil
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

// scanAgentRows scans rows into RegistryRecord slices, handling developer and heartbeat fields.
func scanAgentRows(rows pgx.Rows) ([]*models.RegistryRecord, error) {
	var agents []*models.RegistryRecord
	for rows.Next() {
		agent := &models.RegistryRecord{}
		var tagsJSON []byte
		var registeredAt, updatedAt time.Time
		var developerID *string
		var agentIndex *int
		var developerProofJSON []byte
		var lastHeartbeat *time.Time
		var pricingBytes []byte
		var entityType, serviceEndpoint, openapiURL *string
		if err := rows.Scan(
			&agent.EntityID, &agent.Name, &agent.Owner, &agent.EntityURL,
			&agent.Category, &tagsJSON, &agent.Summary, &agent.PublicKey,
			&agent.HomeRegistry, &agent.SchemaVersion, &registeredAt, &updatedAt,
			&agent.TTL, &agent.Signature,
			&developerID, &agentIndex, &developerProofJSON,
			&agent.Status, &lastHeartbeat,
			&entityType, &serviceEndpoint, &openapiURL, &pricingBytes,
		); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agent.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
		agent.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if lastHeartbeat != nil {
			agent.LastHeartbeat = lastHeartbeat.UTC().Format(time.RFC3339)
		}
		if err := json.Unmarshal(tagsJSON, &agent.Tags); err != nil {
			agent.Tags = []string{}
		}
		if developerID != nil {
			agent.DeveloperID = *developerID
		}
		if agentIndex != nil {
			agent.EntityIndex = agentIndex
		}
		if len(developerProofJSON) > 0 {
			agent.DeveloperProof = &models.DeveloperProof{}
			json.Unmarshal(developerProofJSON, agent.DeveloperProof)
		}
		if entityType != nil {
			agent.EntityType = *entityType
		}
		if serviceEndpoint != nil {
			agent.ServiceEndpoint = *serviceEndpoint
		}
		if openapiURL != nil {
			agent.OpenAPIURL = *openapiURL
		}
		if len(pricingBytes) > 0 {
			agent.EntityPricing = &models.EntityPricing{}
			json.Unmarshal(pricingBytes, agent.EntityPricing)
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func (s *PostgresStore) GetAllTags() ([]string, error) {
	rows, err := s.pool.Query(context.Background(),
		"SELECT DISTINCT jsonb_array_elements_text(tags) AS tag FROM entities ORDER BY tag")
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
		"SELECT DISTINCT category FROM entities ORDER BY category")
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

// ============================================================
// ZNS (Zynd Naming Service) Store Methods
// ============================================================

// --- Handle Operations ---

func (s *PostgresStore) ClaimHandle(developerID, handle, homeRegistry string) error {
	ct, err := s.pool.Exec(context.Background(), `
		UPDATE developers SET dev_handle=$1, updated_at=$2
		WHERE developer_id = $3 AND (dev_handle IS NULL OR dev_handle = '')`,
		handle, time.Now().UTC(), developerID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return fmt.Errorf("handle %q is already taken on this registry", handle)
		}
		return fmt.Errorf("failed to claim handle: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("developer not found or already has a handle")
	}
	return nil
}

func (s *PostgresStore) GetDeveloperByHandle(handle, homeRegistry string) (*models.DeveloperRecord, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT developer_id, name, public_key, profile_url, github,
			home_registry, schema_version, registered_at, updated_at, signature,
			dev_handle, dev_handle_verified, verification_method, verification_proof
		FROM developers WHERE dev_handle = $1 AND home_registry = $2`, handle, homeRegistry)

	dev := &models.DeveloperRecord{}
	var registeredAt, updatedAt time.Time
	var devHandle, verMethod, verProof *string
	var devHandleVerified *bool
	err := row.Scan(
		&dev.DeveloperID, &dev.Name, &dev.PublicKey, &dev.ProfileURL, &dev.GitHub,
		&dev.HomeRegistry, &dev.SchemaVersion, &registeredAt, &updatedAt, &dev.Signature,
		&devHandle, &devHandleVerified, &verMethod, &verProof,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get developer by handle: %w", err)
	}
	dev.RegisteredAt = registeredAt.UTC().Format(time.RFC3339)
	dev.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if devHandle != nil {
		dev.DevHandle = *devHandle
	}
	if devHandleVerified != nil {
		dev.DevHandleVerified = *devHandleVerified
	}
	if verMethod != nil {
		dev.VerificationMethod = *verMethod
	}
	if verProof != nil {
		dev.VerificationProof = *verProof
	}
	return dev, nil
}

func (s *PostgresStore) ReleaseHandle(developerID, handle string) error {
	ct, err := s.pool.Exec(context.Background(), `
		UPDATE developers SET dev_handle=NULL, dev_handle_verified=FALSE,
			verification_method=NULL, verification_proof=NULL, updated_at=$1
		WHERE developer_id = $2 AND dev_handle = $3`,
		time.Now().UTC(), developerID, handle)
	if err != nil {
		return fmt.Errorf("failed to release handle: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("developer not found or handle mismatch")
	}
	return nil
}

func (s *PostgresStore) UpdateHandleVerification(developerID string, verified bool, method, proof string) error {
	ct, err := s.pool.Exec(context.Background(), `
		UPDATE developers SET dev_handle_verified=$1, verification_method=$2,
			verification_proof=$3, updated_at=$4
		WHERE developer_id = $5`,
		verified, method, proof, time.Now().UTC(), developerID)
	if err != nil {
		return fmt.Errorf("failed to update handle verification: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("developer not found")
	}
	return nil
}

// --- ZNS Name Binding Operations ---

func (s *PostgresStore) CreateZNSName(name *models.ZNSName) error {
	capTags := name.CapabilityTags
	if capTags == nil {
		capTags = []string{}
	}
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO zns_names (fqan, entity_name, developer_handle, registry_host,
			entity_id, developer_id, current_version, capability_tags,
			registered_at, updated_at, signature)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		name.FQAN, name.EntityName, name.DeveloperHandle, name.RegistryHost,
		name.EntityID, name.DeveloperID, name.CurrentVersion, capTags,
		name.RegisteredAt, name.UpdatedAt, name.Signature,
	)
	if err != nil {
		return fmt.Errorf("failed to create ZNS name: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetZNSName(fqan string) (*models.ZNSName, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT fqan, entity_name, developer_handle, registry_host, entity_id,
			developer_id, current_version, capability_tags, registered_at, updated_at, signature
		FROM zns_names WHERE fqan = $1`, fqan)
	return scanZNSName(row)
}

func (s *PostgresStore) GetZNSNameByParts(devHandle, agentName, registryHost string) (*models.ZNSName, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT fqan, entity_name, developer_handle, registry_host, entity_id,
			developer_id, current_version, capability_tags, registered_at, updated_at, signature
		FROM zns_names
		WHERE developer_handle = $1 AND entity_name = $2 AND registry_host = $3`,
		devHandle, agentName, registryHost)
	return scanZNSName(row)
}

func (s *PostgresStore) GetZNSNameByAgentID(agentID string) (*models.ZNSName, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT fqan, entity_name, developer_handle, registry_host, entity_id,
			developer_id, current_version, capability_tags, registered_at, updated_at, signature
		FROM zns_names WHERE entity_id = $1`, agentID)
	return scanZNSName(row)
}

func scanZNSName(row pgx.Row) (*models.ZNSName, error) {
	n := &models.ZNSName{}
	var regAt, updAt time.Time
	err := row.Scan(
		&n.FQAN, &n.EntityName, &n.DeveloperHandle, &n.RegistryHost, &n.EntityID,
		&n.DeveloperID, &n.CurrentVersion, &n.CapabilityTags, &regAt, &updAt, &n.Signature,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan ZNS name: %w", err)
	}
	n.RegisteredAt = regAt.UTC().Format(time.RFC3339)
	n.UpdatedAt = updAt.UTC().Format(time.RFC3339)
	return n, nil
}

func (s *PostgresStore) GetZNSNamesByAgentIDs(agentIDs []string) (map[string]*models.ZNSName, error) {
	if len(agentIDs) == 0 {
		return map[string]*models.ZNSName{}, nil
	}
	rows, err := s.pool.Query(context.Background(), `
		SELECT fqan, entity_name, developer_handle, registry_host, entity_id,
			developer_id, current_version, capability_tags, registered_at, updated_at, signature
		FROM zns_names WHERE entity_id = ANY($1)`, agentIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query ZNS names by agent IDs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*models.ZNSName)
	for rows.Next() {
		n := &models.ZNSName{}
		var regAt, updAt time.Time
		if err := rows.Scan(
			&n.FQAN, &n.EntityName, &n.DeveloperHandle, &n.RegistryHost, &n.EntityID,
			&n.DeveloperID, &n.CurrentVersion, &n.CapabilityTags, &regAt, &updAt, &n.Signature,
		); err != nil {
			return nil, fmt.Errorf("failed to scan ZNS name row: %w", err)
		}
		n.RegisteredAt = regAt.UTC().Format(time.RFC3339)
		n.UpdatedAt = updAt.UTC().Format(time.RFC3339)
		result[n.EntityID] = n
	}
	return result, nil
}

func (s *PostgresStore) UpdateZNSName(name *models.ZNSName) error {
	capTags := name.CapabilityTags
	if capTags == nil {
		capTags = []string{}
	}
	ct, err := s.pool.Exec(context.Background(), `
		UPDATE zns_names SET current_version=$1, capability_tags=$2,
			updated_at=$3, signature=$4
		WHERE fqan = $5`,
		name.CurrentVersion, capTags, name.UpdatedAt, name.Signature, name.FQAN)
	if err != nil {
		return fmt.Errorf("failed to update ZNS name: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("ZNS name not found")
	}
	return nil
}

func (s *PostgresStore) DeleteZNSName(fqan string) error {
	ct, err := s.pool.Exec(context.Background(),
		"DELETE FROM zns_names WHERE fqan = $1", fqan)
	if err != nil {
		return fmt.Errorf("failed to delete ZNS name: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("ZNS name not found")
	}
	return nil
}

func (s *PostgresStore) ListZNSNamesByDeveloper(devHandle, registryHost string) ([]*models.ZNSName, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT fqan, entity_name, developer_handle, registry_host, entity_id,
			developer_id, current_version, capability_tags, registered_at, updated_at, signature
		FROM zns_names WHERE developer_handle = $1 AND registry_host = $2
		ORDER BY entity_name`, devHandle, registryHost)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []*models.ZNSName
	for rows.Next() {
		n := &models.ZNSName{}
		var regAt, updAt time.Time
		if err := rows.Scan(
			&n.FQAN, &n.EntityName, &n.DeveloperHandle, &n.RegistryHost, &n.EntityID,
			&n.DeveloperID, &n.CurrentVersion, &n.CapabilityTags, &regAt, &updAt, &n.Signature,
		); err != nil {
			return nil, err
		}
		n.RegisteredAt = regAt.UTC().Format(time.RFC3339)
		n.UpdatedAt = updAt.UTC().Format(time.RFC3339)
		names = append(names, n)
	}
	return names, nil
}

// --- ZNS Version Operations ---

func (s *PostgresStore) CreateZNSVersion(v *models.ZNSVersion) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO zns_versions (fqan, version, entity_id, build_hash, registered_at, signature)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		v.FQAN, v.Version, v.EntityID, v.BuildHash, v.RegisteredAt, v.Signature,
	)
	if err != nil {
		return fmt.Errorf("failed to create ZNS version: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetZNSVersions(fqan string) ([]*models.ZNSVersion, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT fqan, version, entity_id, build_hash, registered_at, signature
		FROM zns_versions WHERE fqan = $1 ORDER BY registered_at DESC`, fqan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []*models.ZNSVersion
	for rows.Next() {
		v := &models.ZNSVersion{}
		var regAt time.Time
		if err := rows.Scan(&v.FQAN, &v.Version, &v.EntityID, &v.BuildHash, &regAt, &v.Signature); err != nil {
			return nil, err
		}
		v.RegisteredAt = regAt.UTC().Format(time.RFC3339)
		versions = append(versions, v)
	}
	return versions, nil
}

func (s *PostgresStore) GetZNSVersion(fqan, version string) (*models.ZNSVersion, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT fqan, version, entity_id, build_hash, registered_at, signature
		FROM zns_versions WHERE fqan = $1 AND version = $2`, fqan, version)

	v := &models.ZNSVersion{}
	var regAt time.Time
	err := row.Scan(&v.FQAN, &v.Version, &v.EntityID, &v.BuildHash, &regAt, &v.Signature)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get ZNS version: %w", err)
	}
	v.RegisteredAt = regAt.UTC().Format(time.RFC3339)
	return v, nil
}

// --- ZNS Gossip ---

func (s *PostgresStore) UpsertGossipZNSName(entry *models.GossipZNSName) error {
	capTags := entry.CapabilityTags
	if capTags == nil {
		capTags = []string{}
	}
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO gossip_zns_names (fqan, entity_name, developer_handle, registry_host,
			entity_id, current_version, capability_tags, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(fqan) DO UPDATE SET
			entity_name=EXCLUDED.entity_name, developer_handle=EXCLUDED.developer_handle,
			registry_host=EXCLUDED.registry_host, entity_id=EXCLUDED.entity_id,
			current_version=EXCLUDED.current_version, capability_tags=EXCLUDED.capability_tags,
			received_at=EXCLUDED.received_at, tombstoned=FALSE`,
		entry.FQAN, entry.EntityName, entry.DeveloperHandle, entry.RegistryHost,
		entry.EntityID, entry.CurrentVersion, capTags, entry.ReceivedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert gossip ZNS name: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetGossipZNSName(fqan string) (*models.GossipZNSName, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT fqan, entity_name, developer_handle, registry_host, entity_id,
			current_version, capability_tags, received_at, tombstoned
		FROM gossip_zns_names WHERE fqan = $1 AND tombstoned = FALSE`, fqan)
	return scanGossipZNSName(row)
}

func (s *PostgresStore) GetGossipZNSNameByParts(devHandle, agentName string) (*models.GossipZNSName, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT fqan, entity_name, developer_handle, registry_host, entity_id,
			current_version, capability_tags, received_at, tombstoned
		FROM gossip_zns_names
		WHERE developer_handle = $1 AND entity_name = $2 AND tombstoned = FALSE`,
		devHandle, agentName)
	return scanGossipZNSName(row)
}

func scanGossipZNSName(row pgx.Row) (*models.GossipZNSName, error) {
	n := &models.GossipZNSName{}
	var recvAt time.Time
	err := row.Scan(
		&n.FQAN, &n.EntityName, &n.DeveloperHandle, &n.RegistryHost, &n.EntityID,
		&n.CurrentVersion, &n.CapabilityTags, &recvAt, &n.Tombstoned,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan gossip ZNS name: %w", err)
	}
	n.ReceivedAt = recvAt.UTC().Format(time.RFC3339)
	return n, nil
}

func (s *PostgresStore) TombstoneGossipZNSName(fqan string) error {
	_, err := s.pool.Exec(context.Background(),
		"UPDATE gossip_zns_names SET tombstoned=TRUE WHERE fqan = $1", fqan)
	return err
}

// --- Registry Verification ---

func (s *PostgresStore) UpsertRegistryProof(proof *models.RegistryIdentityProof) error {
	proofJSON, err := json.Marshal(proof)
	if err != nil {
		return fmt.Errorf("failed to marshal registry proof: %w", err)
	}
	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO registry_identity_proofs (registry_id, domain, ed25519_public_key,
			tls_spki_fingerprint, proof_json, proof_signature, verification_tier,
			issued_at, expires_at, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT(registry_id) DO UPDATE SET
			domain=EXCLUDED.domain, ed25519_public_key=EXCLUDED.ed25519_public_key,
			tls_spki_fingerprint=EXCLUDED.tls_spki_fingerprint, proof_json=EXCLUDED.proof_json,
			proof_signature=EXCLUDED.proof_signature, verification_tier=EXCLUDED.verification_tier,
			issued_at=EXCLUDED.issued_at, expires_at=EXCLUDED.expires_at,
			received_at=EXCLUDED.received_at`,
		proof.RegistryID, proof.Domain, proof.Ed25519PublicKey,
		proof.TLSSPKIFingerprint, proofJSON, proof.Signature,
		proof.VerificationTier, proof.IssuedAt, proof.ExpiresAt, proof.ReceivedAt,
	)
	return err
}

func (s *PostgresStore) GetRegistryProof(registryID string) (*models.RegistryIdentityProof, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT registry_id, domain, ed25519_public_key, tls_spki_fingerprint,
			proof_signature, verification_tier, issued_at, expires_at, received_at
		FROM registry_identity_proofs WHERE registry_id = $1`, registryID)

	p := &models.RegistryIdentityProof{Type: "registry-identity-proof", Version: "1.0"}
	var issuedAt, expiresAt, receivedAt time.Time
	err := row.Scan(
		&p.RegistryID, &p.Domain, &p.Ed25519PublicKey, &p.TLSSPKIFingerprint,
		&p.Signature, &p.VerificationTier, &issuedAt, &expiresAt, &receivedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get registry proof: %w", err)
	}
	p.IssuedAt = issuedAt.UTC().Format(time.RFC3339)
	p.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	p.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
	return p, nil
}

func (s *PostgresStore) GetRegistryProofByDomain(domain string) (*models.RegistryIdentityProof, error) {
	row := s.pool.QueryRow(context.Background(), `
		SELECT registry_id, domain, ed25519_public_key, tls_spki_fingerprint,
			proof_signature, verification_tier, issued_at, expires_at, received_at
		FROM registry_identity_proofs WHERE domain = $1`, domain)

	p := &models.RegistryIdentityProof{Type: "registry-identity-proof", Version: "1.0"}
	var issuedAt, expiresAt, receivedAt time.Time
	err := row.Scan(
		&p.RegistryID, &p.Domain, &p.Ed25519PublicKey, &p.TLSSPKIFingerprint,
		&p.Signature, &p.VerificationTier, &issuedAt, &expiresAt, &receivedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get registry proof by domain: %w", err)
	}
	p.IssuedAt = issuedAt.UTC().Format(time.RFC3339)
	p.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	p.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
	return p, nil
}

func (s *PostgresStore) CreatePeerAttestation(att *models.PeerAttestation) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO peer_attestations (attester_id, subject_id, verified_layers, attested_at, signature)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(attester_id, subject_id) DO UPDATE SET
			verified_layers=EXCLUDED.verified_layers, attested_at=EXCLUDED.attested_at,
			signature=EXCLUDED.signature`,
		att.AttesterID, att.SubjectID, att.VerifiedLayers, att.AttestedAt, att.Signature,
	)
	return err
}

func (s *PostgresStore) CountPeerAttestations(subjectID string) (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM peer_attestations WHERE subject_id = $1", subjectID).Scan(&count)
	return count, err
}

func (s *PostgresStore) UpdateRegistryVerificationTier(registryID, tier string) error {
	_, err := s.pool.Exec(context.Background(),
		"UPDATE registry_identity_proofs SET verification_tier=$1 WHERE registry_id=$2",
		tier, registryID)
	return err
}
