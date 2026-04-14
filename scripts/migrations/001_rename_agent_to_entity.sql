-- Migration 001: full agent → entity rename across every schema element.
--
-- Covers:
--   * agent_id   → entity_id   (entities, gossip_entities, tombstones,
--                              attestations, zns_names, zns_versions,
--                              gossip_zns_names)
--   * agent_url  → entity_url  (entities, gossip_entities)
--   * agent_index → entity_index (entities)
--   * agent_name → entity_name (zns_names, gossip_zns_names)
--   * index idx_zns_agent_id → idx_zns_entity_id
--
-- Foreign keys are updated automatically by ALTER TABLE RENAME COLUMN —
-- the constraint on zns_names.agent_id REFERENCES entities(agent_id)
-- re-binds to entity_id on both sides without a separate DROP/ADD.
--
-- Safe to run once on an existing database.
-- If you'd rather start fresh (dev-only): DROP DATABASE <name>; CREATE DATABASE <name>;
-- then restart agentdns, which runs migrate() on startup with the new schema.

BEGIN;

-- Primary entity table
ALTER TABLE entities          RENAME COLUMN agent_id    TO entity_id;
ALTER TABLE entities          RENAME COLUMN agent_url   TO entity_url;
ALTER TABLE entities          RENAME COLUMN agent_index TO entity_index;

-- Gossip index (remote entities learned via mesh)
ALTER TABLE gossip_entities   RENAME COLUMN agent_id  TO entity_id;
ALTER TABLE gossip_entities   RENAME COLUMN agent_url TO entity_url;

-- Tombstones (deletion markers)
ALTER TABLE tombstones        RENAME COLUMN agent_id TO entity_id;

-- Attestations (trust scoring, part of primary key)
ALTER TABLE attestations      RENAME COLUMN agent_id TO entity_id;

-- ZNS name bindings (has FK to entities)
ALTER TABLE zns_names         RENAME COLUMN agent_id   TO entity_id;
ALTER TABLE zns_names         RENAME COLUMN agent_name TO entity_name;

-- ZNS version history
ALTER TABLE zns_versions      RENAME COLUMN agent_id TO entity_id;

-- Gossip ZNS names (from remote registries)
ALTER TABLE gossip_zns_names  RENAME COLUMN agent_id   TO entity_id;
ALTER TABLE gossip_zns_names  RENAME COLUMN agent_name TO entity_name;

-- Rename the one index that had the old name baked in
ALTER INDEX IF EXISTS idx_zns_agent_id RENAME TO idx_zns_entity_id;

COMMIT;
