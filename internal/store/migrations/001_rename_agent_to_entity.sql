-- Migration 001: agent → entity rename across every schema element.
--
-- Covers:
--   * agent_id    → entity_id    (entities, gossip_entities, tombstones,
--                                attestations, zns_names, zns_versions,
--                                gossip_zns_names)
--   * agent_url   → entity_url   (entities, gossip_entities)
--   * agent_index → entity_index (entities)
--   * agent_name  → entity_name  (zns_names, gossip_zns_names)
--   * index idx_zns_agent_id → idx_zns_entity_id
--
-- Idempotent by design:
--   - Fresh installs: target tables don't exist yet. Every ALTER raises
--     undefined_table, gets caught, and the migration records as applied.
--     migrate() then creates tables directly with the new entity_* names.
--   - Old installs: every ALTER renames successfully.
--   - Already-migrated DBs: schema_migrations guards against re-runs; if
--     that guard were ever bypassed, the ALTERs would raise
--     undefined_column (old names no longer exist) and get caught as no-ops.
--
-- Foreign keys re-bind automatically under ALTER TABLE RENAME COLUMN —
-- the zns_names.agent_id REFERENCES entities(agent_id) constraint tracks
-- both sides of the rename without a separate DROP/ADD.

DO $$
BEGIN
    -- entities
    BEGIN
        ALTER TABLE entities RENAME COLUMN agent_id TO entity_id;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;
    BEGIN
        ALTER TABLE entities RENAME COLUMN agent_url TO entity_url;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;
    BEGIN
        ALTER TABLE entities RENAME COLUMN agent_index TO entity_index;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;

    -- gossip_entities
    BEGIN
        ALTER TABLE gossip_entities RENAME COLUMN agent_id TO entity_id;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;
    BEGIN
        ALTER TABLE gossip_entities RENAME COLUMN agent_url TO entity_url;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;

    -- tombstones
    BEGIN
        ALTER TABLE tombstones RENAME COLUMN agent_id TO entity_id;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;

    -- attestations
    BEGIN
        ALTER TABLE attestations RENAME COLUMN agent_id TO entity_id;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;

    -- zns_names
    BEGIN
        ALTER TABLE zns_names RENAME COLUMN agent_id TO entity_id;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;
    BEGIN
        ALTER TABLE zns_names RENAME COLUMN agent_name TO entity_name;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;

    -- zns_versions
    BEGIN
        ALTER TABLE zns_versions RENAME COLUMN agent_id TO entity_id;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;

    -- gossip_zns_names
    BEGIN
        ALTER TABLE gossip_zns_names RENAME COLUMN agent_id TO entity_id;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;
    BEGIN
        ALTER TABLE gossip_zns_names RENAME COLUMN agent_name TO entity_name;
    EXCEPTION WHEN undefined_column OR undefined_table THEN NULL;
    END;

    -- Index rename. ALTER INDEX on a missing index raises undefined_table
    -- (42P01) in PostgreSQL, not undefined_object (42704) — catch both so
    -- fresh installs no-op cleanly.
    BEGIN
        ALTER INDEX idx_zns_agent_id RENAME TO idx_zns_entity_id;
    EXCEPTION WHEN undefined_object OR undefined_table THEN NULL;
    END;
END $$;
