# Agent DNS — Architecture & Internals

How every layer of the Agent DNS network works, from cryptographic identity to federated search.

---

## Table of Contents

1. [System Overview](#system-overview)
2. [Identity Layer](#1-identity-layer)
3. [Registry Store](#2-registry-store)
4. [Gossip Mesh](#3-gossip-mesh)
5. [Search Engine](#4-search-engine)
6. [DHT (Distributed Hash Table)](#5-dht-distributed-hash-table)
7. [Zynd Naming Service (ZNS)](#6-zynd-naming-service-zns)
8. [Trust & Reputation](#7-trust--reputation)
9. [Agent Cards](#8-agent-cards)
10. [Caching](#9-caching)
11. [Heartbeat & Liveness](#10-heartbeat--liveness)
12. [REST API](#11-rest-api)
13. [Event Bus](#12-event-bus)
14. [Data Flow Diagrams](#data-flow-diagrams)

---

## System Overview

Agent DNS is a decentralized registry and discovery network for AI agents. It maps natural-language queries to discoverable, verifiable AI agents across a federated peer-to-peer mesh.

Each registry node is a single Go binary that runs:
- An **HTTP API** (default port 8080) for clients
- A **TCP mesh transport** (default port 4001) for peer-to-peer gossip
- Background services for heartbeat monitoring, bloom filter rebuilds, DHT republishing, and tombstone cleanup

```
┌─────────────────────────────────────────────────────────────────┐
│                     Agent DNS Registry Node                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────────┐  │
│  │ HTTP API │  │  Gossip  │  │   DHT    │  │    Search     │  │
│  │  :8080   │  │  Mesh    │  │(Kademlia)│  │   Engine      │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └───────┬───────┘  │
│       │              │             │                │           │
│  ┌────┴──────────────┴─────────────┴────────────────┴───────┐  │
│  │                    Internal Bus                           │  │
│  └────┬──────────────┬─────────────┬────────────────┬───────┘  │
│       │              │             │                │           │
│  ┌────┴─────┐  ┌─────┴────┐  ┌────┴─────┐  ┌──────┴──────┐   │
│  │ Registry │  │ Identity │  │  Trust   │  │   Card      │   │
│  │  Store   │  │  (Ed25519)│  │(EigenTrust)│ │  Fetcher    │   │
│  └────┬─────┘  └──────────┘  └──────────┘  └─────────────┘   │
│       │                                                        │
│  ┌────┴─────────────────────────────────────────────────────┐  │
│  │              PostgreSQL          Redis (optional)         │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Startup Sequence

When `agentdns start` runs, the node initializes in this order:

1. Load configuration from `~/.zynd/config.toml`
2. Load node identity (Ed25519 keypair) from `~/.zynd/identity.json`
3. Connect to PostgreSQL (connection pool: 2–20 connections)
4. Connect to Redis (optional — used for caching, not required)
5. Initialize search engine with configured embedder (hash, ONNX, or HTTP)
6. Create peer manager and gossip handler
7. Create EigenTrust calculator for reputation scoring
8. Start mesh transport (TCP listener on port 4001)
9. Wire federated search into mesh
10. Initialize DHT (Kademlia) if enabled
11. Start background loops:
    - DHT republish (every 1 hour)
    - Mesh heartbeat (every 30 seconds)
    - Peer reconnect (every 30 seconds)
    - Tombstone garbage collection (every 1 hour)
    - Liveness monitor sweep (every 60 seconds)
    - Bloom filter rebuild (every 5 minutes)
12. Start HTTP API server
13. Wait for SIGINT/SIGTERM for graceful shutdown

---

## 1. Identity Layer

**Source:** `internal/identity/identity.go`

Every entity in the network — registries, developers, and agents — has an Ed25519 keypair. All IDs are deterministically derived from public keys.

### ID Generation

```
Public Key (32 bytes)
    │
    ▼ SHA-256
Hash (32 bytes)
    │
    ▼ Take first 16 bytes, hex-encode
"agdns:" + hex
```

| Entity | ID Format | Example |
|--------|-----------|---------|
| Agent | `agdns:<hex>` | `agdns:7f3a9c2e1d8b4a06` |
| Registry | `agdns:registry:<hex>` | `agdns:registry:a1b2c3d4e5f6` |
| Developer | `agdns:dev:<hex>` | `agdns:dev:f2a1c3e8b9d7` |

### Developer → Agent Derivation (HD-Style)

Developers can deterministically derive agent keypairs from their own key, similar to BIP-32 hierarchical deterministic wallets:

```
Developer Private Key (seed bytes)
    │
    ▼ SHA-512( dev_seed || "agdns:agent:" || uint32_be(index) )
64 bytes
    │
    ▼ Take first 32 bytes as agent seed
Agent Ed25519 Keypair
```

**Properties:**
- Same developer + same index = identical agent keypair (recoverable)
- Different indexes produce different keys
- Developer signs a **derivation proof** over `(agent_pub_key || index)` to prove authorization
- Proof is verifiable by anyone with the developer's public key — no private key needed

### Signing Convention

All signatures use the format `ed25519:<base64-encoded-signature>`. Public keys use `ed25519:<base64-encoded-key>`.

### TLS from Ed25519

Each node generates a self-signed TLS certificate from its Ed25519 key for mesh connections. Identity is verified at the application layer (HELLO handshake), not by CA trust chains. TLS 1.3 minimum.

---

## 2. Registry Store

**Source:** `internal/store/store.go`, `internal/store/postgres.go`

The store is a PostgreSQL-backed persistence layer that holds all registry state. The `Store` interface defines ~60 methods grouped by domain.

### Database Schema

#### `agents` — Locally registered agents
```sql
agent_id        TEXT PRIMARY KEY   -- agdns:<hash>
name            TEXT NOT NULL
owner           TEXT NOT NULL
agent_url       TEXT NOT NULL      -- where to fetch EntityCard
category        TEXT
tags            JSONB              -- ["python", "security"]
summary         TEXT               -- max 200 chars
public_key      TEXT NOT NULL      -- ed25519:<base64>
home_registry   TEXT
registered_at   TIMESTAMPTZ
updated_at      TIMESTAMPTZ
ttl             INTEGER            -- seconds
signature       TEXT
status          TEXT DEFAULT 'active'     -- active | inactive
last_heartbeat  TIMESTAMPTZ
developer_id    TEXT               -- agdns:dev:<hash>
entity_index     INTEGER            -- derivation index
developer_proof JSONB              -- derivation proof
codebase_hash   TEXT               -- SHA-256 of agent source
```

Indexes: `category`, `name`, `owner`, `updated_at DESC`, `tags` (GIN), `developer_id`, `status`

#### `gossip_entries` — Remote agents learned via gossip
Same schema as `agents`, plus:
- `received_at` — when the gossip arrived
- `tombstoned` / `tombstone_at` — soft-delete markers
- `origin_public_key` — pinned on first registration to prevent takeover

#### `developers` — Developer identities
```sql
developer_id    TEXT PRIMARY KEY
name            TEXT
public_key      TEXT UNIQUE
profile_url     TEXT
github          TEXT
home_registry   TEXT
dev_handle      TEXT               -- ZNS handle (e.g., "acme-corp")
dev_handle_verified BOOLEAN
verification_method TEXT           -- "dns" | "github"
verification_proof  TEXT           -- domain or username
```

#### `zns_names` — FQAN → Agent mappings
```sql
fqan              TEXT PRIMARY KEY  -- "dns01.zynd.ai/acme-corp/doc-translator"
entity_name        TEXT
developer_handle  TEXT
registry_host     TEXT
agent_id          TEXT REFERENCES agents(agent_id)
developer_id      TEXT
current_version   TEXT
capability_tags   TEXT[]
```

#### `zns_versions` — Version history per FQAN
```sql
fqan         TEXT    -- composite PK
version      TEXT    -- composite PK
agent_id     TEXT
build_hash   TEXT
```

#### Other tables
- `tombstones` — Deletion markers with TTL (garbage collected hourly)
- `attestations` — Reputation observations from peer registries
- `registry_identity_proofs` — Registry verification documents
- `peer_attestations` — Peer-to-peer verification records
- `gossip_developers`, `gossip_zns_names` — Remote developer/ZNS data from gossip

### Connection Pool

Uses `pgxpool` with tuning:
- Max connections: 20
- Min idle connections: 2
- Max connection lifetime: 30 minutes
- Max idle time: 5 minutes

---

## 3. Gossip Mesh

**Source:** `internal/mesh/`

The gossip mesh is a peer-to-peer network that propagates registrations, updates, status changes, and ZNS name bindings across registry nodes.

### Transport Layer (`transport.go`)

Each registry node runs a TCP listener (default port 4001). Connections use TLS derived from the node's Ed25519 key.

**Wire protocol:** Length-prefixed JSON
```
[4 bytes big-endian length][JSON payload]
```
- Max message size: 1 MB
- Write timeout: 10 seconds
- Read timeout: 90 seconds (3x heartbeat interval)

**Message types:**
| Type | Purpose |
|------|---------|
| `MsgHello` | Initial handshake (registry ID, name, version, agent count) |
| `MsgHeartbeat` | Liveness + bloom filter exchange + peer addresses |
| `MsgGossip` | Gossip announcements (registrations, updates, etc.) |
| `MsgSearch` | Federated search request |
| `MsgSearchAck` | Federated search response |
| `MsgDHT` | DHT protocol messages |

**Connection handshake:**
1. Initiator sends `HELLO` with its registry ID, public key, and agent count
2. Responder replies with its own `HELLO`
3. Self-connections and duplicate peers are rejected
4. On success, both sides enter the read loop

### Gossip Protocol (`gossip.go`)

**Announcement types:**
| Type | Purpose |
|------|---------|
| `agent_announce` | Agent registration, update, deregistration |
| `developer_announce` | Developer identity registration |
| `dev_handle` | Handle claim and verification |
| `name_binding` | ZNS FQAN registration |
| `registry_proof` | Registry identity proof publication |
| `peer_attestation` | Peer-to-peer verification |

**Processing pipeline for incoming announcements:**
1. **Deduplication** — Check if `(type + ID + timestamp)` was seen within the dedup window (default 300s). Drop if duplicate.
2. **Signature verification** — Verify Ed25519 signature on the announcement.
3. **Origin authorization** — For updates/deregisters, verify the origin public key matches the key pinned on first registration. Prevents agent takeover.
4. **Hop count check** — Drop if `HopCount >= MaxHops` (default 10).
5. **Store** — Upsert into the appropriate gossip table.
6. **Index** — Trigger search engine indexing callback.
7. **Forward** — Increment hop count, broadcast to all peers except the sender.

**Origin authorization (key pinning):**
- On first `register` action, the sender's `OriginPublicKey` is stored in the gossip entry.
- Subsequent `update`, `deregister`, or `agent_status` actions must come from the same key.
- Prevents a malicious registry from taking over another registry's agents.
- Backward compatible: entries without a stored key accept any key (pre-pinning data).

### Peer Management (`peers.go`)

- Tracks connected peers with `PeerInfo` (registry ID, name, address, agent count, last seen, bloom filter).
- Max peers configurable (default 15). When exceeded, evicts the peer with the oldest `last_seen`.
- **Smart peer selection** for federated search: scores peers by bloom filter match count against query tokens.

### Bootstrap & Reconnect (`bootstrap.go`)

- On startup, connects to configured `bootstrap_peers` with exponential backoff (1s → 60s max, unlimited retries).
- **Reconnect loop** runs every 30 seconds, re-establishing connections to any disconnected bootstrap peers.

### Heartbeat & Bloom Filters (`heartbeat.go`, `bloom.go`)

Every 30 seconds, each node broadcasts a heartbeat containing:
- Registry ID and agent count
- Serialized bloom filter of all local agent tokens (names, categories, tags, summaries)
- Peer addresses for peer exchange (discovery of new nodes)

**Bloom filter:**
- Probabilistic data structure for smart query routing
- Sized for expected token count with configurable false positive rate (default 1%)
- Uses double hashing (FNV-1a + FNV-1) for index computation
- Rebuilt periodically (default every 5 minutes) from all local + gossip agents
- Peers store received bloom filters and use `MatchCount()` to select the most relevant peers for federated search

### Federated Search (`federated.go`)

When a search request has `Federated: true`:
1. Tokenize the query (keywords, category, tags)
2. Score peers by bloom filter match count
3. Select top N peers (default 10)
4. Fan out search requests concurrently (each with TTL=1 to prevent recursion)
5. Collect results with timeout (default 1.5 seconds)
6. Merge and return combined results

Peers receiving a federated search request execute **local-only** search (Federated=false) to prevent infinite forwarding.

---

## 4. Search Engine

**Source:** `internal/search/`

The search engine combines multiple retrieval strategies for hybrid search.

### Architecture

```
Search Request
    │
    ├─── Keyword Search (BM25)     ──► Local agents
    ├─── Semantic Search (vectors)  ──► Local agents
    ├─── Gossip Search             ──► Remote agents in gossip store
    └─── Federated Search          ──► Live queries to peer registries
    │
    ▼
Merge + Deduplicate
    │
    ▼
Rank (weighted or RRF)
    │
    ▼
Enrich top 10 with Agent Cards
    │
    ▼
Return paginated results
```

### Keyword Search (`keyword.go`, `keyword_v2.go`)

**Basic BM25** (`keyword.go`):
- Classic BM25 with K1=1.2, B=0.75
- Tokenizes on non-alphanumeric characters, minimum 2-char terms
- Combined text from name, summary, category, and tags

**Improved BM25** (`keyword_v2.go`, enabled by default):
- Per-field indexing with boosted weights:
  - Name: 3.0x
  - Tags: 2.0x
  - Category: 1.5x
  - Summary: 1.0x
- Advanced tokenization with Porter stemming, stopword removal, and synonym expansion
- Phrase matching bonus (+5.0 for exact quoted phrases)

### Semantic Search (`semantic.go`)

- Converts text to dense vectors using a pluggable embedder
- Brute-force cosine similarity search (O(n), suitable for <100K agents)
- All vectors L2-normalized on insertion for efficient dot product computation
- Only returns positive similarities (>0)

### Embedding Backends (`embeddings.go`)

Three pluggable backends via the `Embedder` interface:

| Backend | Dependencies | Quality | Speed |
|---------|-------------|---------|-------|
| **Hash** (`hash`) | None | Basic | Fastest |
| **ONNX** (`onnx`) | Rust tokenizers + ONNX Runtime | Best | Fast (local) |
| **HTTP** (`http`) | External service (Ollama, OpenAI) | Varies | Network-bound |

**Hash Embedder** — Feature hashing with FNV-64a. Includes unigram and bigram features. No ML required.

**ONNX Embedder** (`onnx_embedder.go`) — Runs transformer models in-process:
- Supported models: `all-MiniLM-L6-v2` (90MB), `bge-small-en-v1.5` (130MB), `e5-small-v2` (130MB)
- Process: tokenize → ONNX inference → mean pooling → L2 normalize
- Max sequence length: 128 tokens
- Pre-allocated tensors for efficiency
- Thread-safe with mutex

**HTTP Embedder** (`http_embedder.go`) — Sends text to an external endpoint:
- Compatible with OpenAI and Ollama response formats
- 10-second timeout
- Returns zero vector on error (graceful degradation)

### Tokenizer (`tokenizer.go`)

Advanced tokenization pipeline:
1. Split on non-alphanumeric (preserving hyphens/underscores)
2. Filter stopwords (24 common English words)
3. Apply Porter stemmer (suffix removal)
4. Expand with synonyms (e.g., "ai" → ["artificial-intelligence", "ml"])
5. Generate n-grams (if configured)

### Ranking (`ranking.go`)

Two ranking methods:

**Weighted Linear (default):**
```
FinalScore = 0.30 * TextRelevance
           + 0.30 * SemanticSimilarity
           + 0.20 * TrustScore
           + 0.10 * Freshness
           + 0.10 * Availability
```

**Reciprocal Rank Fusion (RRF):**
```
RRF_score = Σ 1/(60 + rank_i)  for each ranking source
```

**Score components:**
| Component | Range | How it's computed |
|-----------|-------|-------------------|
| TextRelevance | 0–1 | BM25 score (normalized) |
| SemanticSimilarity | 0–1 | Cosine similarity of embedding vectors |
| TrustScore | 0–1 | EigenTrust aggregation (local=0.5, gossip=0.3 baseline) |
| Freshness | 0–1 | Exponential decay: `e^(-0.023 * days)` (0.5 at ~30 days) |
| Availability | 0–1 | From Agent Card status: online=1.0, degraded=0.5, offline=0.0 |

### Model Registry (`model_registry.go`)

Catalog of downloadable ONNX embedding models with:
- HuggingFace download URLs
- SHA-256 integrity verification
- Automatic download to `~/.zynd/models/<name>/`

---

## 5. DHT (Distributed Hash Table)

**Source:** `internal/dht/`

A Kademlia-based DHT enables decentralized agent lookups without relying on gossip alone.

### Design

- **Node IDs:** 256-bit (32 bytes), derived from Ed25519 public key via SHA-256
- **Distance metric:** XOR
- **Routing table:** 256 k-buckets (one per bit), each holding up to K contacts (default 20)
- **Lookup concurrency:** Alpha (default 3)

### Operations

| Operation | Description |
|-----------|-------------|
| `Ping` | Check if peer is alive, add to routing table |
| `Store` | Store agent record at K closest nodes |
| `FindValue` | Look up agent by key (local first, then network) |
| `FindNode` | Find K closest nodes to a target |

### Iterative Lookup

1. Find Alpha closest known nodes to target
2. Query them in parallel
3. If they return closer nodes, query those too
4. Repeat until no closer nodes found or K closest identified
5. For `FindValue`: return the value if any node has it, cache locally

### Background Loops

- **Republish** (every 1 hour): Re-stores all local records at closest nodes
- **Expire** (every 10 minutes): Removes records older than 24 hours

### Mesh Adapter (`mesh_adapter.go`)

Bridges DHT messages to the existing mesh transport layer. DHT messages are serialized to JSON and sent through the same TCP connections used for gossip.

---

## 6. Zynd Naming Service (ZNS)

**Source:** `internal/zns/`, `internal/models/zns.go`

ZNS provides human-readable names for agents, similar to how DNS maps domain names to IP addresses.

### Fully Qualified Agent Name (FQAN)

```
{registry-host}/{developer-handle}/{agent-name}[@version][#capability]
```

**Examples:**
```
dns01.zynd.ai/acme-corp/doc-translator              -- latest version
dns01.zynd.ai/acme-corp/doc-translator@2.1.0         -- exact version
dns01.zynd.ai/acme-corp/doc-translator@2              -- latest v2.x
dns01.zynd.ai/acme-corp/doc-translator#nlp.translation -- capability filter
agdns://dns01.zynd.ai/acme-corp/doc-translator@2.1.0  -- URI form
```

**Why slashes, not dots?** Registry hosts already contain dots (e.g., `dns01.zynd.ai`). Dot-separated names would be ambiguous — the parser couldn't determine where the developer namespace ends and the hostname begins.

### Three-Part Naming

| Part | Source | Uniqueness |
|------|--------|------------|
| Registry Host | Derived from `https_endpoint` config | Globally unique (verified via TLS + DNS) |
| Developer Handle | Claimed by developer | Unique within registry |
| Agent Name | Set at registration | Unique within developer namespace |

The combination is globally unique.

### Developer Handles

Handles are optional human-readable aliases for developer IDs. Three verification tiers:

| Tier | How | Badge |
|------|-----|-------|
| Self-Claimed | First-come-first-served, no verification | None |
| Domain-Verified | DNS TXT record at `_zynd-verify.{domain}` with developer's public key | Verified |
| GitHub-Verified | OAuth flow linking GitHub account | Verified |

**Handle rules:** Lowercase alphanumeric + hyphens, 3–40 chars, starts with letter. Reserved names: `zynd`, `system`, `admin`, `test`, `root`, `registry`, `anonymous`, `unknown`.

### Registry Identity Verification (4 Layers)

Prevents impersonation on the gossip mesh.

**Layer 1 — TLS Certificate (Automatic)**
Every HTTPS connection verifies domain ownership via CA-issued certificates.

**Layer 2 — Registry Identity Proof (RIP)**
Published at `/.well-known/zynd-registry.json`:
```json
{
  "domain": "dns01.zynd.ai",
  "registry_id": "agdns:registry:a1b2c3d4...",
  "ed25519_public_key": "gKH4VSJ838fG1jg6Y14EwwAkQ5PbXs...",
  "tls_spki_fingerprint": "sha256:b4de3a9f8c2e...",
  "signature": "ed25519:Pfix+qwQxg0ztDjnR..."
}
```
Binds domain → TLS certificate → Ed25519 key in one signed document. Uses SPKI fingerprint (not full cert), so it survives Let's Encrypt 90-day renewals as long as the TLS private key doesn't rotate.

**Verification flow:**
1. Connect via HTTPS → TLS proves domain ownership
2. Extract server cert's SPKI fingerprint during TLS handshake
3. Fetch `/.well-known/zynd-registry.json`
4. Verify: SPKI fingerprint in proof matches handshake cert
5. Verify: Ed25519 signature on proof is valid
6. Result: the entity controlling the domain is the entity holding the Ed25519 key

**Layer 3 — DNS TXT Record**
```
_zynd.dns01.zynd.ai  TXT  "v=zynd1 id=agdns:registry:a1b2c3d4 key=ed25519:gKH4..."
```
Enables pre-connection verification and Trust-on-First-Use (TOFU). With DNSSEC, this is cryptographically tamper-proof.

**Layer 4 — Peer Attestation**
After successful verification (TLS + RIP + DNS), existing trusted registries co-sign the newcomer's identity and gossip the attestation. After N attestations (default 3), the registry reaches "mesh-verified" tier.

### Verification Tiers

| Tier | Requirements | Trust Signal |
|------|-------------|--------------|
| Self-Announced | Ed25519 keypair only | Lowest |
| Domain-Verified | TLS + RIP at `/.well-known/zynd-registry.json` | Medium |
| DNS-Published | Domain-verified + `_zynd.` DNS TXT record | Higher |
| Mesh-Verified | DNS-published + N peer attestations | Highest |

### DNS Bridge (`dns_verify.go`)

Agents hosted on non-Zynd infrastructure can publish a DNS TXT record pointing to their FQAN:
```
_zynd.translator.acme-corp.com  TXT  "fqan=dns01.zynd.ai/acme-corp/doc-translator"
```
This enables DNS-native discovery without requiring agents to run Zynd software.

### Resolution Flow

```
Client: "resolve acme-corp/doc-translator"
    │
    ▼
Parse FQAN → registry_host, dev_handle, entity_name
    │
    ▼
Look up ZNS name in PostgreSQL (handle + entity_name)
    │
    ▼
Get associated agent_id
    │
    ▼
Look up full agent record from agents table
    │
    ▼
Return ZNSResolveResponse (FQAN, agent_id, developer info, agent URL, trust score, verification tier)
```

---

## 7. Trust & Reputation

**Source:** `internal/trust/eigentrust.go`

The EigenTrust algorithm computes global trust scores from signed observations across registry peers.

### Reputation Attestations

Each registry periodically publishes observations about agents it has interacted with:
```json
{
  "agent_id": "agdns:7f3a...",
  "observer_registry": "agdns:registry:a1b2...",
  "period": "2026-02-01/2026-03-01",
  "invocations": 5420,
  "successes": 5350,
  "failures": 70,
  "avg_latency_ms": 230,
  "avg_rating": 4.7
}
```

### Per-Attestation Reputation Score

```
successRate   = successes / invocations
ratingScore   = avg_rating / 5.0  (clamped to 1.0)
latencyScore  = 1 / (1 + exp((latencyMs - 1000) / 300))  -- sigmoid curve

reputation = 0.4 * successRate + 0.3 * ratingScore + 0.3 * latencyScore
```

The latency sigmoid maps: 100ms → 0.95, 1000ms → 0.5, 5000ms → 0.1.

### Trust Computation

```
Trust(agent) = Σ  weight(registry_i) * reputation_i(agent)
               ─────────────────────────────────────────────
               Σ  weight(registry_i)
```

- Unknown registries get weight 0.5 by default
- Custom weights can be set per registry via `SetRegistryTrust()`
- Trust is transitive but attenuated through indirect observations

### Confidence

```
attestationConfidence = 1 - exp(-attestationCount / 3.0)   -- saturates at ~10
invocationConfidence  = 1 - exp(-totalInvocations / 300.0)  -- saturates at ~1000
confidence = 0.5 * attestationConfidence + 0.5 * invocationConfidence
```

High confidence requires both multiple independent observers AND significant invocation volume.

---

## 8. Agent Cards

**Source:** `internal/models/agent_card.go`, `internal/models/zynd_agent_card.go`, `internal/card/`

Agent Cards are dynamic metadata documents hosted by agents themselves. While the registry stores a lightweight pointer (~500–1200 bytes), the Agent Card contains full capabilities, pricing, endpoints, and live status (2–10 KB).

### Registry Record vs Agent Card

| Aspect | Registry Record | Agent Card |
|--------|----------------|------------|
| Stored at | Registry (PostgreSQL) | Agent's own URL |
| Size | ~500–1200 bytes | 2–10 KB |
| Updates | Requires re-registration | Agent updates at will |
| Contains | Name, category, tags, public key, URL | Capabilities, pricing, endpoints, trust metrics |
| Propagation | Gossip mesh | Fetched on demand |

### Agent Card Structure

```json
{
  "agent_id": "agdns:7f3a9c2e...",
  "version": "2.4.1",
  "status": "online",
  "capabilities": [{
    "name": "code-review",
    "protocols": ["a2a", "mcp", "jsonrpc"],
    "languages": ["python", "go"],
    "latency_p95_ms": 3000
  }],
  "pricing": {
    "model": "per-request",
    "currency": "USD",
    "rates": { "code-review": 0.01 },
    "payment_methods": ["x402", "stripe"]
  },
  "endpoints": {
    "invoke": "https://agent.example.com/v1/invoke",
    "health": "https://agent.example.com/health",
    "websocket": "wss://agent.example.com/ws"
  }
}
```

### Zynd Agent Card (Extended Format)

The `ZyndAgentCard` is a modular format served at `/.well-known/zynd-agent.json` with protocol-specific sections:

- **Zynd section:** Identity (FQAN, agent ID, developer, public key, home registry)
- **Agent section:** Protocol-neutral metadata (name, description, capabilities)
- **Endpoints section:** Invoke, health, WebSocket URLs
- **Protocols section:** Protocol-specific metadata:
  - **A2A:** Card URL, skills list
  - **MCP:** Transport type, endpoint, tool definitions
  - **REST:** OpenAPI URL, invoke endpoints
- **Trust section:** Trust score, verification tier, ZTP count

### Card Fetching

The `Fetcher` resolves agent card URLs with fallback:
1. If URL ends in `.json` or contains `.well-known` → use as-is
2. Otherwise try `{url}/.well-known/zynd-agent.json`
3. Fall back to `{url}/.well-known/agent.json`

---

## 9. Caching

**Source:** `internal/card/cache.go`, `internal/cache/redis.go`

### Two-Tier Agent Card Cache

```
Request for Agent Card
    │
    ├─► Tier 1: In-process LRU cache (O(1), per-instance)
    │   Hit? Return immediately.
    │
    ├─► Tier 2: Redis cache (shared across instances)
    │   Hit? Return, populate Tier 1.
    │
    └─► Tier 3: HTTP fetch from agent URL
        Populate both Tier 1 and Tier 2.
```

**LRU Cache (`card/cache.go`):**
- Thread-safe doubly-linked list with map
- Configurable max size (default 50,000) and TTL (default 1 hour)
- Lazy expiration on access (no background cleanup)
- O(1) get/put/remove

**Redis Cache (`cache/redis.go`):**
- Optional (nil if not configured — system works without it)
- Connection pool: 20 connections, 3 min idle
- Fail-open on errors (graceful degradation)
- Key namespacing with configurable prefix (default `agdns:`)

### Redis Key Patterns

| Pattern | Purpose | TTL |
|---------|---------|-----|
| `agdns:card:{agent_id}` | Agent card cache | Configurable |
| `agdns:search:{hash}` | Search result cache | Configurable |
| `agdns:bloom:{registry_id}` | Peer bloom filter | Configurable |
| `agdns:rate:{bucket}:{ip}` | Rate limit counter | 1 minute |
| `agdns:peer:{registry_id}` | Peer heartbeat | Configurable |

---

## 10. Heartbeat & Liveness

**Source:** `internal/api/heartbeat.go`, `internal/api/monitor.go`

### Agent Heartbeat Protocol

Agents prove liveness by maintaining a WebSocket connection and sending signed heartbeat messages.

```
Agent connects to: GET /v1/agents/{agentID}/ws
    │
    ▼ WebSocket upgrade
Agent sends: { "timestamp": "2026-03-28T...", "signature": "ed25519:..." }
    │
    ▼ Server verifies:
    1. Timestamp is within clock_skew window (default 60s)
    2. Ed25519 signature over timestamp bytes is valid
    │
    ▼ On first valid signature:
    - Mark agent as "active"
    - Broadcast status via gossip to all peers
    │
    ▼ On every valid signature:
    - Update last_heartbeat in PostgreSQL
    - Publish EventEntityHeartbeat
```

**Read deadline:** `inactive_threshold + 60s`. If no message received within this window, the WebSocket connection is closed.

### Liveness Monitor

A background sweep runs every `sweep_interval_seconds` (default 60s):

1. Query all agents where `status='active'` AND `last_heartbeat < NOW() - threshold`
2. Mark them as `inactive` in PostgreSQL
3. For each newly-inactive agent:
   - Publish `EventEntityBecameInactive`
   - Create gossip status announcement
   - Broadcast to mesh peers

Default inactive threshold: 300 seconds (5 minutes).

---

## 11. REST API

**Source:** `internal/api/server.go`

The HTTP API serves on port 8080 (configurable) with Swagger docs at `/swagger/`.

### Endpoint Groups

#### Agent Management
| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/agents` | Register agent (with optional developer proof and ZNS name) |
| GET | `/v1/agents/{agentID}` | Get agent (checks local → gossip → DHT) |
| PUT | `/v1/agents/{agentID}` | Update agent (dual-key auth: agent OR developer) |
| DELETE | `/v1/agents/{agentID}` | Deregister with tombstone |
| GET | `/v1/agents/{agentID}/card` | Fetch live Agent Card |
| GET | `/v1/agents/{agentID}/ws` | WebSocket heartbeat |

#### Developer Identity
| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/developers` | Register developer |
| GET | `/v1/developers/{devID}` | Get developer record |
| PUT | `/v1/developers/{devID}` | Update profile |
| DELETE | `/v1/developers/{devID}` | Deregister developer |
| GET | `/v1/developers/{devID}/agents` | List developer's agents |

#### Search & Discovery
| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/search` | Hybrid search with filters |
| GET | `/v1/categories` | List all categories |
| GET | `/v1/tags` | List all tags |

#### ZNS: Handles
| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/handles` | Claim handle |
| GET | `/v1/handles/{handle}` | Resolve handle |
| GET | `/v1/handles/{handle}/available` | Check availability |
| DELETE | `/v1/handles/{handle}` | Release handle |
| POST | `/v1/handles/{handle}/verify` | Verify handle (DNS/GitHub) |
| GET | `/v1/handles/{handle}/agents` | List agents under handle |

#### ZNS: Names
| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/names` | Register FQAN binding |
| GET | `/v1/names/{dev}/{agent}` | Resolve FQAN |
| PUT | `/v1/names/{dev}/{agent}` | Update binding |
| DELETE | `/v1/names/{dev}/{agent}` | Release name |
| GET | `/v1/names/{dev}/{agent}/versions` | Version history |

#### ZNS: Resolution
| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/resolve/{dev}/{agent}` | Resolve FQAN to full agent record |

#### Network
| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/network/status` | Node status |
| GET | `/v1/network/peers` | Connected peers |
| POST | `/v1/network/peers` | Add peer manually |
| GET | `/v1/network/stats` | Network statistics |
| GET | `/v1/info` | Registry metadata and capabilities |
| GET | `/.well-known/zynd-registry.json` | Registry identity proof |

#### Health & Admin
| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/v1/ws/activity` | WebSocket activity stream |
| POST | `/v1/admin/developers/approve` | Webhook-authenticated developer approval |

### Middleware Stack

1. **CORS** — Configurable origins (default `*`), allows GET/POST/PUT/DELETE/OPTIONS
2. **Logging** — Logs `METHOD PATH STATUS DURATION` for every request
3. **Rate Limiting** — Per-IP token bucket:
   - Search: 100 requests/minute (default)
   - Register: 10 requests/minute (default)
   - Stale buckets cleaned after 5 minutes
4. **Webhook Auth** — For restricted onboarding, uses constant-time comparison of `Authorization: Bearer {secret}`

### Registration Flow (POST /v1/agents)

1. Parse JSON request body
2. Verify Ed25519 signature over signable bytes
3. Derive `agent_id` from public key
4. If `developer_id` + `developer_proof` provided:
   - Look up developer (local → gossip)
   - Verify derivation proof
   - Set owner = developer_id
5. Create `RegistryRecord`, validate, store in PostgreSQL
6. Index in search engine (keyword + semantic)
7. Create gossip announcement, broadcast to mesh
8. Publish `EventEntityRegistered`
9. If `entity_name` provided and developer has a handle, create FQAN binding automatically
10. Return agent_id

### Agent Lookup (GET /v1/agents/{agentID})

Three-tier resolution:
1. **Local store** — Check PostgreSQL agents table
2. **Gossip store** — Check gossip_entries table (remote agents)
3. **DHT** — Distributed lookup via Kademlia (if enabled)

---

## 12. Event Bus

**Source:** `internal/events/bus.go`

In-process fan-out pub/sub for real-time observability. All events are broadcast to all subscribers.

### Event Types

| Category | Events |
|----------|--------|
| Agent | `agent_registered`, `agent_deregistered`, `agent_heartbeat`, `agent_became_active`, `agent_became_inactive` |
| Gossip | `gossip_outgoing`, `gossip_incoming` |
| Search | `search_outgoing`, `search_result_incoming` |
| Peer | `peer_connected`, `peer_disconnected` |
| ZNS | `handle_claimed`, `handle_verified`, `name_registered`, `name_resolved` |

### Design

- Subscribers receive a buffered channel (256-item buffer)
- Non-blocking send: events are dropped for slow subscribers (prevents backpressure)
- All subscribers get all events (no filtering at the bus level)
- Thread-safe concurrent access via RWMutex
- The WebSocket activity stream at `/v1/ws/activity` exposes events to external clients

---

## Data Flow Diagrams

### Agent Registration

```
Client signs agent payload with Ed25519 key
         │
         ▼
    POST /v1/agents
         │
         ▼
    Verify signature ──────────── reject if invalid
         │
         ▼
    Generate agent_id = agdns:sha256(pubkey)[:16]
         │
         ▼
    (Optional) Verify developer_proof
         │
         ▼
    Store in PostgreSQL (agents table)
         │
         ├──► Index in search engine (keyword + semantic)
         │
         ├──► Create GossipAnnouncement, broadcast to mesh
         │         │
         │         ▼
         │    Peer registries receive, verify, store in gossip_entries
         │         │
         │         ▼
         │    Forward to their peers (hop count limits propagation)
         │
         ├──► (Optional) Create ZNS name binding
         │
         └──► Publish EventEntityRegistered
```

### Search

```
Client sends: POST /v1/search { query, category, tags, federated: true }
         │
         ▼
    ┌────────────────────────── Parallel ──────────────────────────┐
    │                           │                                  │
    ▼                           ▼                                  ▼
Keyword Search           Semantic Search                    Gossip Search
(BM25 on local)         (Cosine similarity              (BM25 on gossip
                         on local vectors)                  entries)
    │                           │                                  │
    └───────────┬───────────────┘                                  │
                ▼                                                  │
         Merge local results                                       │
                │                                                  │
                └──────────────────────┬───────────────────────────┘
                                       │
                                       ▼
                              Federated Search
                        (Fan out to top N bloom-matched peers)
                                       │
                                       ▼
                              Merge all results
                                       │
                                       ▼
                              Deduplicate by agent_id
                                       │
                                       ▼
                              Rank (weighted or RRF)
                                       │
                                       ▼
                              Enrich top 10 with Agent Cards
                                       │
                                       ▼
                              Return paginated response
```

### Gossip Propagation

```
Local event (registration, update, heartbeat)
         │
         ▼
    CreateAnnouncement() — sign with registry key
         │
         ▼
    BroadcastAnnouncement() — send to all connected peers
         │
    ═══════════════════════════════════════════════
         │              │              │
    Peer A          Peer B          Peer C
         │              │              │
         ▼              ▼              ▼
    HandleAnnouncement():
    1. Dedup check (seen before?)
    2. Verify signature
    3. Verify origin authorization (key pinning)
    4. Check hop count < max_hops
    5. Store in gossip table
    6. Index in search engine
    7. Increment hop count
    8. Forward to own peers (except sender)
```

### Heartbeat Liveness

```
Agent ──WebSocket──► Registry Node
         │
    Send signed heartbeat every N seconds
         │
         ▼
    Verify signature ──────── reject if invalid
         │
         ▼
    Update last_heartbeat in PostgreSQL
         │
         ▼
    (First heartbeat) Mark active, gossip status
         │
         ▼
    ┌──────────────────────────────────┐
    │     Background: LivenessMonitor  │
    │     (runs every 60 seconds)      │
    │                                  │
    │  Query: active agents where      │
    │  last_heartbeat < threshold      │
    │         │                        │
    │         ▼                        │
    │  Mark inactive in PostgreSQL     │
    │  Gossip status to mesh           │
    │  Publish EventEntityBecameInactive│
    └──────────────────────────────────┘
```
