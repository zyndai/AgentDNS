# Agent DNS: A Decentralized Agent Registry Network

## Implementation Guide

**Version:** 0.1 Draft
**Date:** March 2026
**Status:** Architecture Specification

---

## 1. The Problem

DNS maps names → IP addresses. It was designed for static servers in 1983.

AI agents are different:
- They **move** (endpoints change)
- They **evolve** (capabilities change hourly)
- They **multiply** (billions, eventually trillions)
- They **need trust** (is this agent legit? is it good?)
- They **need discovery** (find me an agent that can do X)

We need **DNS for agents** — but it can't be traditional DNS. It must handle dynamic capabilities, semantic discovery, decentralized trust, and real-time metadata that changes constantly.

---

## 2. Core Design Principle: Static Pointer, Dynamic Metadata

The key insight: **separate what's stable from what changes.**

```
┌─────────────────────────────────┐
│  REGISTRY RECORD (stable)       │  ← Stored on the registry network
│  agent_id, name, owner,         │     Replicated across shards
│  agent_url, category, created   │     Changes rarely
│  public_key, signature          │
├─────────────────────────────────┤
│  agent_url points to:           │
│  ┌───────────────────────────┐  │
│  │ AGENT CARD (dynamic)      │  │  ← Hosted by the agent itself
│  │ capabilities, pricing,    │  │     at a stable URL
│  │ status, version, load,    │  │     Changes frequently
│  │ skills, trust_stats,      │  │     Fetched on-demand + cached
│  │ protocols, examples       │  │
│  └───────────────────────────┘  │
└─────────────────────────────────┘
```

**Why this split?**
- Registry records are small (~500 bytes), replicated cheaply, change rarely
- Agent Cards are rich (2-10KB), hosted by the agent, updated anytime without touching the registry
- This mirrors how DNS points to IPs, but the website content (the dynamic part) lives on the server

---

## 3. Architecture Overview

```
                        ┌──────────────────┐
                        │   CLIENT / SDK   │
                        │  (any language)  │
                        └────────┬─────────┘
                                 │
                    ┌────────────▼────────────┐
                    │     ANY REGISTRY NODE    │ ← Client connects to nearest/preferred
                    │   (entry point / shard)  │
                    └────────────┬─────────────┘
                                 │
               ┌─────────────────┼─────────────────┐
               │                 │                  │
        ┌──────▼──────┐  ┌──────▼──────┐  ┌───────▼──────┐
        │  Registry A  │  │  Registry B  │  │  Registry C   │
        │  (Shard)     │  │  (Shard)     │  │  (Shard)      │
        │  1000 agents │  │  5000 agents │  │  200 agents   │
        └──────────────┘  └──────────────┘  └───────────────┘
               │                 │                  │
               └─────── MESH PROTOCOL ──────────────┘
                    (gossip + DHT + search)
```

**Anyone can run a registry.** Each registry:
- Stores its own agents (its "shard" of the global namespace)
- Connects to the mesh and discovers other registries
- Forwards searches to peer registries when local results are insufficient
- Caches popular results from other registries
- Operates independently if disconnected (local-first)

**The network IS the global registry.** No single node has everything. Together, they have everything.

---

## 4. System Components

### 4.1 Registry Node

A registry node is a standalone process that anyone can deploy. It consists of:

```
┌─────────────────────────────────────────────────┐
│                 REGISTRY NODE                    │
│                                                  │
│  ┌─────────────┐  ┌──────────────────────────┐  │
│  │  Local Store │  │  Search Engine            │  │
│  │  (agents     │  │  - Keyword index (BM25)   │  │
│  │   registered │  │  - Embedding index         │  │
│  │   on THIS    │  │  - Category filter         │  │
│  │   node)      │  │                            │  │
│  └─────────────┘  └──────────────────────────┘  │
│                                                  │
│  ┌─────────────┐  ┌──────────────────────────┐  │
│  │  Peer Cache  │  │  Mesh Client              │  │
│  │  (results    │  │  - Peer discovery          │  │
│  │   from other │  │  - Gossip protocol         │  │
│  │   registries)│  │  - Search federation       │  │
│  └─────────────┘  └──────────────────────────┘  │
│                                                  │
│  ┌─────────────────────────────────────────────┐ │
│  │  API Gateway                                 │ │
│  │  REST + WebSocket • Auth • Rate Limiting     │ │
│  └─────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

### 4.2 Registry Record (Static — stored on-network)

This is the minimal, stable record that lives in the registry network:

```json
{
  "agent_id": "agdns:7f3a9c2e...",
  "name": "CodeReviewer",
  "owner": "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
  "agent_url": "https://codereview.example.com/.well-known/agent.json",
  "category": "developer-tools",
  "tags": ["code-review", "security", "python"],
  "summary": "Reviews code for security vulnerabilities and style issues",
  "public_key": "ed25519:base64...",
  "home_registry": "agdns:registry:a1b2c3...",
  "registered_at": "2026-03-01T10:00:00Z",
  "updated_at": "2026-03-01T10:00:00Z",
  "ttl": 86400,
  "signature": "ed25519:base64..."
}
```

**Fields explained:**

| Field | Purpose | Changes? |
|---|---|---|
| `agent_id` | Globally unique identifier (hash of public key) | Never |
| `name` | Human-readable name | Rarely |
| `owner` | DID of the agent owner | Never |
| `agent_url` | **THE KEY FIELD** — URL where the full, dynamic Agent Card lives | Rarely (only if agent moves) |
| `category` | Primary category for filtering | Rarely |
| `tags` | Keywords for search indexing | Occasionally |
| `summary` | Short description for search (≤200 chars) | Occasionally |
| `public_key` | Ed25519 public key for verification | Never |
| `home_registry` | Which registry this agent was originally registered on | Never |
| `registered_at` | Creation timestamp | Never |
| `updated_at` | Last update to this record | On update |
| `ttl` | Cache duration hint (seconds) | On update |
| `signature` | Record signed by agent's private key | On update |

**Size: ~500-800 bytes.** Cheap to replicate, store, and index.

### 4.3 Agent Card (Dynamic — hosted by the agent)

The `agent_url` in the registry record points to a JSON document hosted by the agent itself (like `robots.txt` or Google's A2A `/.well-known/agent.json`):

```json
{
  "agent_id": "agdns:7f3a9c2e...",
  "version": "2.4.1",
  "status": "online",
  "last_heartbeat": "2026-03-02T15:30:00Z",

  "capabilities": [
    {
      "name": "code-review",
      "description": "Reviews pull requests for security vulnerabilities, style issues, and best practices",
      "input_schema": { "type": "object", "properties": { "repo_url": { "type": "string" }, "pr_number": { "type": "integer" } } },
      "output_schema": { "type": "object", "properties": { "issues": { "type": "array" }, "score": { "type": "number" } } },
      "protocols": ["a2a", "mcp", "jsonrpc"],
      "languages": ["python", "javascript", "rust", "go"],
      "latency_p95_ms": 3000,
      "examples": [
        { "input": "Review PR #42 on repo/xyz", "output": "Found 3 security issues..." }
      ]
    },
    {
      "name": "security-audit",
      "description": "Deep security audit of codebases",
      "input_schema": { "..." : "..." },
      "output_schema": { "..." : "..." },
      "protocols": ["a2a"],
      "latency_p95_ms": 30000
    }
  ],

  "pricing": {
    "model": "per-request",
    "currency": "USD",
    "rates": {
      "code-review": 0.01,
      "security-audit": 0.50
    },
    "payment_methods": ["x402", "stripe", "lightning"]
  },

  "trust": {
    "total_invocations": 154200,
    "success_rate": 0.987,
    "avg_rating": 4.7,
    "uptime_30d": 0.998,
    "verifications": [
      { "issuer": "did:web:trustregistry.example.com", "type": "capability-attestation", "issued": "2026-02-15" }
    ]
  },

  "endpoints": {
    "invoke": "https://codereview.example.com/v1/invoke",
    "health": "https://codereview.example.com/health",
    "websocket": "wss://codereview.example.com/ws"
  },

  "metadata": {
    "framework": "langchain",
    "model": "claude-3.5-sonnet",
    "owner_contact": "ops@example.com",
    "documentation": "https://docs.example.com/codereview",
    "source_code": "https://github.com/example/codereview-agent"
  },

  "signature": "ed25519:base64...",
  "signed_at": "2026-03-02T15:30:00Z"
}
```

**Why separate from the registry record?**
- The agent updates this anytime (new capabilities, pricing changes, status updates) **without touching the registry**
- Registries cache this with TTL — fresh data on demand, no replication storm
- Agent Cards can be as rich as needed — no size constraints from the registry

---

## 5. The Mesh Protocol

### 5.1 How Registries Find Each Other

Every registry node participates in a peer-to-peer mesh:

```
Registry A ◄──────► Registry B
    │                    │
    │                    │
    ▼                    ▼
Registry C ◄──────► Registry D ◄──────► Registry E
                         │
                         ▼
                    Registry F
```

**Peer Discovery (how nodes find each other):**

1. **Bootstrap seeds** — hardcoded list of well-known registries (like DNS root servers, but anyone can add their own)
2. **Peer exchange** — connected registries share their peer lists
3. **Local discovery (mDNS)** — find registries on the same LAN
4. **Manual peering** — operators explicitly peer with specific registries

**Connection protocol:**
- Each registry maintains connections to 8-15 peers (configurable)
- Connections are persistent WebSocket or QUIC (low-overhead, bidirectional)
- Heartbeats every 30s to detect dead peers
- Automatic reconnection with exponential backoff

### 5.2 Gossip Protocol (How Information Spreads)

When a new agent registers on Registry A, the network needs to know:

```
Time 0s:   Agent registers on Registry A
           Registry A indexes it locally

Time 1s:   Registry A gossips to its 8 peers:
           "New agent: agdns:7f3a9c2e, category: developer-tools,
            tags: [code-review, security], home: registry-A"

Time 3s:   Those 8 peers gossip to THEIR peers (minus the sender)
           ~60 registries now know

Time 6s:   Third round of gossip
           ~400 registries know

Time 12s:  Most of a 10K-node network knows
```

**What gets gossiped (lightweight announcements only):**
```json
{
  "type": "agent_announce",
  "agent_id": "agdns:7f3a9c2e...",
  "name": "CodeReviewer",
  "category": "developer-tools",
  "tags": ["code-review", "security", "python"],
  "summary": "Reviews code for security vulnerabilities",
  "home_registry": "agdns:registry:a1b2c3...",
  "agent_url": "https://codereview.example.com/.well-known/agent.json",
  "action": "register",
  "timestamp": "2026-03-01T10:00:00Z",
  "signature": "ed25519:base64...",
  "hop_count": 0,
  "max_hops": 10
}
```

**Size: ~300-400 bytes per announcement.** At 1000 new agents/hour across the network, that's ~400KB/hour of gossip — trivial.

**What does NOT get gossiped:**
- Full Agent Cards (too large, too dynamic — fetch on demand via `agent_url`)
- Invocation data, payment details, conversation logs

**Gossip rules:**
- Max hop count (default: 10) prevents infinite propagation
- Deduplication by `agent_id` + `timestamp` (ignore if already seen)
- Rate limiting: max 100 announcements/second per peer (prevent spam)
- Announcements are signed by the originating registry (verify before forwarding)

### 5.3 What Each Registry Stores

Registries do NOT store everything. They maintain:

| Data | Stored? | Source |
|---|---|---|
| Agents registered directly on this node | ✅ Full records | Local registration |
| Gossip announcements from other registries | ✅ Lightweight index (id, name, tags, category, home_registry) | Gossip |
| Agent Cards from other registries | ⚡ Cached with TTL | Fetched on-demand from `agent_url` |
| Search embeddings for local agents | ✅ Full vector index | Computed locally |
| Search embeddings for remote agents | ⚡ Computed on-demand from gossip summaries | Computed from gossip data |

**Storage estimate per node:**
- 1000 local agents: ~5MB (records + embeddings)
- 1M gossip announcements (global index): ~400MB (lightweight entries + keyword index)
- Cache: configurable, default 1GB (LRU eviction)

Any commodity server or even a Raspberry Pi (for small registries) can participate.

---

## 6. Search & Discovery — The Core Flow

### 6.1 How a Client Searches

```
Client: "Find me an agent that can translate legal documents from English to Japanese"
   │
   ▼
Registry X (client's connected registry)
   │
   ├─ STEP 1: Search local agents
   │   ├─ Keyword match on tags + summary (BM25)
   │   ├─ Semantic match on summary embeddings (cosine similarity)
   │   └─ Category filter: "translation" or "language"
   │   Result: 3 local matches
   │
   ├─ STEP 2: Search local gossip index (announcements from other registries)
   │   ├─ Same keyword + semantic search on gossip data
   │   └─ Result: 12 matches from other registries
   │
   ├─ STEP 3: Federated search (ask peers for deeper results)
   │   ├─ Select 6-10 peer registries (see routing below)
   │   ├─ Send search query to each
   │   ├─ Each peer searches its local agents + gossip index
   │   ├─ Each peer returns top-10 results
   │   └─ Result: 40-80 candidates from across the network
   │
   ├─ STEP 4: Merge & rank all results
   │   ├─ Deduplicate by agent_id
   │   ├─ Score using ranking algorithm (see below)
   │   └─ Result: top-20 ranked agents
   │
   ├─ STEP 5: Enrich top results (fetch Agent Cards)
   │   ├─ For top-10 results, fetch agent_url (Agent Card)
   │   ├─ Verify signature on Agent Card
   │   ├─ Check agent status (online/offline)
   │   ├─ Attach full capability details + pricing
   │   └─ Cache Agent Cards with TTL
   │
   └─ STEP 6: Return enriched results to client
       └─ Top-10 agents with full details, ranked by relevance + trust
```

### 6.2 Federated Search Routing (Which Peers to Ask)

Not all peers are equally useful. Smart routing reduces latency and improves results:

**Bloom Filter Routing:**
Each registry maintains a bloom filter summarizing the tags and categories of all agents it knows about (local + gossip):

```
Registry A bloom filter: {translation, language, japanese, english, legal, ...}
Registry B bloom filter: {code-review, security, python, devops, ...}
Registry C bloom filter: {translation, spanish, french, medical, ...}
```

When searching for "translate legal documents English to Japanese":
1. Extract query tokens: `[translate, legal, documents, english, japanese]`
2. Check each peer's bloom filter
3. Only query peers whose bloom filter matches ≥2 tokens
4. Result: skip Registry B (irrelevant), query A and C

**Bloom filter specs:**
- Updated every 5 minutes (or on significant registration changes)
- Size: ~10KB per registry (supports ~100K unique tokens at 1% false positive rate)
- Exchanged during peer heartbeats (minimal overhead)

**Fallback:** If bloom filters are unavailable (new peer, not yet exchanged), select peers randomly. The system always works — bloom filters are an optimization, not a requirement.

### 6.3 Ranking Algorithm

All results (local + remote) are merged and ranked:

```
final_score = w1 · text_relevance
            + w2 · semantic_similarity
            + w3 · trust_score
            + w4 · freshness
            + w5 · availability
```

| Factor | Weight (default) | Source | Description |
|---|---|---|---|
| `text_relevance` | 0.30 | BM25 on tags + summary + name | Keyword match quality |
| `semantic_similarity` | 0.30 | Cosine similarity of query embedding vs summary embedding | How semantically close the agent is to the query |
| `trust_score` | 0.20 | Agent Card `trust` section (invocations, success_rate, rating) | Is this agent reliable? |
| `freshness` | 0.10 | `updated_at` field, decay function | Prefer recently active agents |
| `availability` | 0.10 | Agent Card `status` + `uptime_30d` | Is the agent actually reachable? |

**Alternative: Reciprocal Rank Fusion (RRF)**

For simpler deployments that don't want to tune weights:

```
RRF_score(agent) = Σ  1 / (k + rank_i(agent))
                  i ∈ {bm25, semantic, trust}

where k = 60 (standard constant)
```

RRF merges ranked lists without weight tuning. Works well out of the box.

### 6.4 Search Embedding Model

Each registry runs a local embedding model for semantic search:

- **Default model:** `all-MiniLM-L6-v2` (384 dimensions, ~80MB, runs on CPU)
- **Runtime:** ONNX Runtime (fast inference, no GPU needed)
- **What gets embedded:** Agent `summary` field from registry records + gossip announcements
- **Index:** HNSW (Hierarchical Navigable Small World) for approximate nearest neighbor search — sub-millisecond query time for millions of vectors

**No external API calls.** All embedding computation is local. This is critical for:
- Privacy (queries don't leave the node)
- Speed (no network round trip for embedding)
- Cost (no per-query charges)
- Independence (works offline)

---

## 7. Agent Registration Flow

### 7.1 Registering an Agent

```
Agent Owner                          Registry Node
    │                                      │
    ├─── POST /v1/agents/register ────────►│
    │    {                                 │
    │      name: "CodeReviewer",           │
    │      agent_url: "https://...",       │── 1. Validate request
    │      category: "developer-tools",    │── 2. Fetch agent_url, verify Agent Card exists
    │      tags: [...],                    │── 3. Verify signature (owner's Ed25519 key)
    │      summary: "...",                 │── 4. Generate agent_id = hash(public_key)
    │      public_key: "ed25519:..."       │── 5. Store in local registry
    │    }                                 │── 6. Compute embedding for summary
    │    Signed with owner's private key   │── 7. Index in local search engine
    │                                      │── 8. Gossip announcement to peers
    │◄── 201 Created ─────────────────────┤
    │    { agent_id: "agdns:7f3a9c2e..." } │
    │                                      │
```

### 7.2 Updating an Agent Record

Only the `agent_url` and registry record fields need updating through the registry. Capability changes happen at the Agent Card (no registry update needed):

**Scenario A: Agent adds a new capability**
- Agent updates its Agent Card at `agent_url` → done
- No registry update needed
- Caches expire naturally (TTL), next fetch gets new capabilities
- If the new capability warrants new `tags` on the registry record → update the record

**Scenario B: Agent moves to a new endpoint**
- Agent updates its Agent Card `endpoints` section → done for invoke URL
- If the `agent_url` itself changes → update the registry record
- Gossip propagates the update

**Scenario C: Agent goes offline**
- Agent Card `status` changes to "offline" → consumers see it on next fetch
- If permanent → deregister from the registry

### 7.3 Deregistration

```
Agent Owner                          Registry Node
    │                                      │
    ├─── DELETE /v1/agents/{agent_id} ────►│
    │    Signed with owner's private key   │── 1. Verify ownership signature
    │                                      │── 2. Mark as tombstoned in local store
    │                                      │── 3. Remove from search index
    │                                      │── 4. Gossip tombstone to peers
    │◄── 200 OK ──────────────────────────┤
    │                                      │
```

Tombstones propagate via gossip. Peers remove the agent from their indexes. Tombstones are garbage collected after 7 days (configurable).

---

## 8. Identity & Trust

### 8.1 Agent Identity

Every agent has an Ed25519 keypair:
- **Private key:** held by the agent owner, never shared
- **Public key:** included in the registry record
- **agent_id:** derived from public key hash (`agdns:` + first 16 bytes of SHA-256 of public key)

**Why Ed25519?**
- Fast (sign: 15K ops/sec, verify: 8K ops/sec on commodity hardware)
- Small keys (32 bytes public, 64 bytes private)
- Deterministic signatures (no nonce reuse vulnerabilities)
- Widely supported (libsodium, OpenSSL, every language)

**Identity verification flow:**
1. Agent owner generates keypair locally
2. Registers with public key on a registry
3. Signs every registry record update and Agent Card with private key
4. Anyone can verify signatures using the public key from the registry record

**No central authority needed.** Identity is self-sovereign. Trust is built through reputation (see below).

### 8.2 Registry Identity

Registries also have Ed25519 keypairs:
- **registry_id:** derived from public key hash (`agdns:registry:` + hash)
- Used to sign gossip messages (so peers can verify who sent what)
- Used for peer authentication during mesh connections

### 8.3 Trust & Reputation

**The problem:** Agent self-declares capabilities. How do you know it's actually good?

**Three layers of trust:**

#### Layer 1: Cryptographic Identity (baseline)
- Agent is who it claims to be (signature verification)
- Agent Card is authentic (signed by the agent's key)
- This proves identity, NOT quality

#### Layer 2: Observed Reputation (earned)
- Registries track invocation outcomes for agents they proxy/observe
- Metrics: success_rate, response_time, error_rate, user_ratings
- Shared as signed attestations during gossip

**Reputation data structure (per registry, per agent):**
```json
{
  "agent_id": "agdns:7f3a9c2e...",
  "observer_registry": "agdns:registry:a1b2c3...",
  "period": "2026-02-01/2026-03-01",
  "invocations": 1542,
  "successes": 1522,
  "failures": 20,
  "avg_latency_ms": 450,
  "avg_rating": 4.7,
  "signature": "ed25519:base64..."
}
```

#### Layer 3: Trust Graph (aggregated)
- Each registry weights attestations from other registries based on how much it trusts them
- New registry? Low weight. Registry with consistent, verified attestations? High weight.
- Uses **EigenTrust-style** propagation: trust is transitive but attenuated

```
Trust(agent) = Σ  trust_weight(registry_i) × reputation_i(agent)
              i ∈ observing_registries
```

**Cold start:** New agents have no reputation. Options:
1. Appear in search results but with a "new / unverified" badge
2. Owner stakes a deposit (optional — for high-trust environments)
3. A trusted registry vouches for the agent (third-party attestation)

---

## 9. API Specification

Every registry node exposes this API:

### 9.1 Agent Management

```
POST   /v1/agents                    # Register a new agent
GET    /v1/agents/{agent_id}         # Get registry record
PUT    /v1/agents/{agent_id}         # Update registry record (owner only)
DELETE /v1/agents/{agent_id}         # Deregister (owner only)
GET    /v1/agents/{agent_id}/card    # Fetch + cache Agent Card from agent_url
```

### 9.2 Search

```
POST   /v1/search                    # Search agents (see below)
GET    /v1/categories                # List all known categories
GET    /v1/tags                      # List popular tags
```

**Search request:**
```json
{
  "query": "translate legal documents from English to Japanese",
  "category": "translation",
  "tags": ["japanese", "legal"],
  "min_trust_score": 0.5,
  "status": "online",
  "max_results": 20,
  "federated": true,
  "enrich": true,
  "timeout_ms": 2000
}
```

**Search response:**
```json
{
  "results": [
    {
      "agent_id": "agdns:9e8f7a6b...",
      "name": "LegalTranslatorJP",
      "summary": "Specializes in Japanese legal document translation",
      "category": "translation",
      "tags": ["japanese", "english", "legal", "certified"],
      "agent_url": "https://legaltranslator.jp/.well-known/agent.json",
      "home_registry": "agdns:registry:tokyo01...",
      "score": 0.94,
      "score_breakdown": {
        "text_relevance": 0.95,
        "semantic_similarity": 0.97,
        "trust_score": 0.89,
        "freshness": 0.92,
        "availability": 1.0
      },
      "card": { "...enriched Agent Card if enrich=true..." }
    }
  ],
  "total_found": 47,
  "search_stats": {
    "local_results": 3,
    "gossip_results": 12,
    "federated_results": 32,
    "peers_queried": 8,
    "latency_ms": 187
  }
}
```

### 9.3 Network

```
GET    /v1/network/status            # Node status, peer count, agent count
GET    /v1/network/peers             # Connected peers
POST   /v1/network/peers             # Manually add a peer
GET    /v1/network/stats             # Global network statistics (estimated)
```

### 9.4 Authentication

- **Registration/Update/Delete:** Signed with agent owner's Ed25519 key (in `Authorization` header or request body)
- **Search:** Open by default, rate-limited (configurable: API key, OAuth, etc.)
- **Network API:** Peer authentication via registry keypair (mutual TLS or signed handshake)

---

## 10. Implementation Guide

### 10.1 Technology Stack

| Component | Recommended | Alternatives | Why |
|---|---|---|---|
| **Language** | Rust | Go, TypeScript (Node.js) | Performance + safety for P2P networking |
| **Networking** | libp2p | Custom TCP/QUIC | Battle-tested P2P, NAT traversal, multiplexing |
| **DHT** | Kademlia (via libp2p) | Custom | O(log n) routing, proven at scale |
| **Gossip** | GossipSub (via libp2p) | Custom epidemic | Efficient pub/sub with mesh routing |
| **Local DB** | SQLite | PostgreSQL (large nodes) | Zero-config, embedded, fast for <1M records |
| **Full-text search** | Tantivy (Rust) / MeiliSearch | Elasticsearch (heavy) | BM25 search, lightweight, embeddable |
| **Vector search** | HNSW (via `usearch` or `hnswlib`) | FAISS | Sub-ms ANN search, small memory footprint |
| **Embeddings** | ONNX Runtime + all-MiniLM-L6-v2 | Any sentence-transformer | Local, fast, no API dependency |
| **API** | Axum (Rust) / Fastify (Node) | Actix, Hyper, Express | High-performance HTTP + WebSocket |
| **Identity** | Ed25519 (via `ed25519-dalek` or `libsodium`) | — | Fast, small, secure |
| **Serialization** | MessagePack (gossip) + JSON (API) | Protobuf, CBOR | MessagePack for compact gossip, JSON for human-readable API |

### 10.2 Project Structure

```
agent-dns/
├── src/
│   ├── main.rs                    # Entry point, CLI
│   ├── config.rs                  # Configuration management
│   ├── node/
│   │   ├── mod.rs                 # Node lifecycle (init, start, stop)
│   │   ├── identity.rs            # Ed25519 keypair management
│   │   └── bootstrap.rs           # Peer discovery & bootstrap
│   ├── registry/
│   │   ├── mod.rs                 # Registry record CRUD
│   │   ├── store.rs               # SQLite/Postgres storage layer
│   │   ├── validation.rs          # Record validation & signature verification
│   │   └── tombstone.rs           # Deregistration & tombstone GC
│   ├── mesh/
│   │   ├── mod.rs                 # Mesh network orchestration
│   │   ├── gossip.rs              # Gossip protocol (announce, propagate)
│   │   ├── peers.rs               # Peer management (connect, heartbeat, evict)
│   │   ├── bloom.rs               # Bloom filter construction & exchange
│   │   └── sync.rs                # State sync (CRDT merge for gossip data)
│   ├── search/
│   │   ├── mod.rs                 # Search orchestration (local + federated)
│   │   ├── keyword.rs             # BM25 keyword search (Tantivy)
│   │   ├── semantic.rs            # Embedding-based semantic search (HNSW)
│   │   ├── ranking.rs             # Scoring & ranking (weighted + RRF)
│   │   ├── federation.rs          # Federated search fan-out & merge
│   │   └── embeddings.rs          # ONNX embedding model wrapper
│   ├── trust/
│   │   ├── mod.rs                 # Trust score computation
│   │   ├── attestation.rs         # Create & verify reputation attestations
│   │   └── eigentrust.rs          # EigenTrust algorithm implementation
│   ├── api/
│   │   ├── mod.rs                 # API gateway setup (Axum routes)
│   │   ├── agents.rs              # /v1/agents endpoints
│   │   ├── search.rs              # /v1/search endpoints
│   │   ├── network.rs             # /v1/network endpoints
│   │   └── middleware.rs          # Auth, rate limiting, CORS
│   └── card/
│       ├── mod.rs                 # Agent Card fetcher & cache
│       ├── fetch.rs               # HTTP fetch + signature verification
│       └── cache.rs               # LRU cache with TTL
├── tests/
│   ├── integration/               # Multi-node integration tests
│   ├── unit/                      # Unit tests per module
│   └── fixtures/                  # Test data (agent cards, records)
├── config/
│   └── default.toml               # Default configuration
├── Cargo.toml
├── Dockerfile
├── docker-compose.yml             # Multi-node local testbed
└── README.md
```

### 10.3 Configuration

```toml
# ~/.zynd/config.toml

[node]
name = "my-registry"                    # Human-readable node name
type = "full"                           # full | light | gateway
data_dir = "~/.zynd/data"
external_ip = "auto"                    # auto-detect or set manually

[mesh]
listen_port = 4001
max_peers = 15                          # Target peer count
bootstrap_peers = [
    "/dns4/seed1.zynd.net/tcp/4001/p2p/12D3KooW...",
    "/dns4/seed2.zynd.net/tcp/4001/p2p/12D3KooW...",
]

[gossip]
max_hops = 10
max_announcements_per_second = 100
dedup_window_seconds = 300

[registry]
storage = "sqlite"                      # sqlite | postgres
max_local_agents = 100000
postgres_url = ""                       # Only if storage = "postgres"

[search]
embedding_model = "all-MiniLM-L6-v2"
embedding_dimensions = 384
max_federated_peers = 10                # How many peers to fan-out to
federated_timeout_ms = 1500             # Max wait for peer responses
default_max_results = 20

[search.ranking]
text_relevance_weight = 0.30
semantic_similarity_weight = 0.30
trust_weight = 0.20
freshness_weight = 0.10
availability_weight = 0.10

[cache]
max_agent_cards = 50000                 # LRU cache size
agent_card_ttl_seconds = 3600           # Default cache TTL
max_gossip_entries = 2000000            # Max gossip index entries

[trust]
min_display_score = 0.1                 # Don't show agents with trust below this
eigentrust_iterations = 5
attestation_gossip_interval_seconds = 3600

[api]
listen = "0.0.0.0:8080"
rate_limit_search = 100                 # Requests per minute per IP
rate_limit_register = 10                # Registrations per minute per IP
cors_origins = ["*"]

[bloom]
expected_tokens = 500000
false_positive_rate = 0.01
update_interval_seconds = 300
```

### 10.4 CLI

```bash
# Install
npm install -g agent-dns    # Node.js version
# or
cargo install agent-dns     # Rust version

# Initialize node (generates keypair, creates config)
agentdns init

# Start the node (joins mesh)
agentdns start

# Start in background
agentdns start --daemon

# Register an agent
agentdns register \
  --name "MyTranslator" \
  --agent-url "https://mytranslator.com/.well-known/agent.json" \
  --category "translation" \
  --tags "english,japanese,legal" \
  --summary "Translates legal documents between English and Japanese"

# Search the network
agentdns search "code review agent for Python security"

# Search with filters
agentdns search "translate english to japanese" \
  --category translation \
  --min-trust 0.5 \
  --status online \
  --max-results 10

# Get specific agent
agentdns resolve agdns:7f3a9c2e...

# Get agent's full card (dynamic data)
agentdns card agdns:7f3a9c2e...

# Node status
agentdns status

# Network info
agentdns peers
agentdns network-stats

# Deregister an agent
agentdns deregister agdns:7f3a9c2e...

# Stop the node
agentdns stop
```

---

## 11. Data Flow Examples

### 11.1 Full Registration → Discovery → Invocation Flow

```
PHASE 1: REGISTRATION

Agent Owner                    Registry A                    Network
    │                              │                            │
    ├── agentdns register ────────►│                            │
    │   (name, agent_url,          │── validate & store         │
    │    category, tags,           │── compute embedding        │
    │    summary, pubkey)          │── index in search          │
    │                              │── gossip announce ────────►│
    │◄── agent_id ────────────────┤                     (propagates in
    │                              │                      ~10-30 seconds)


PHASE 2: DISCOVERY

Consumer                       Registry B                    Network
    │                              │                            │
    ├── agentdns search ──────────►│                            │
    │   "translate legal docs      │── search local index       │
    │    English to Japanese"      │── search gossip index      │
    │                              │── fan-out to 8 peers ─────►│
    │                              │◄── peer results ──────────┤
    │                              │── merge & rank             │
    │                              │── fetch Agent Cards        │
    │                              │   (from agent_url)         │
    │◄── ranked results ──────────┤                            │
    │                              │                            │


PHASE 3: INVOCATION

Consumer                       Agent
    │                              │
    │  (Consumer got the agent's   │
    │   invoke endpoint from the   │
    │   Agent Card)                │
    │                              │
    ├── POST /v1/invoke ──────────►│
    │   {task: "translate this     │── process request
    │    document..."}             │── return result
    │   + x402 payment header      │
    │◄── result ──────────────────┤
    │                              │
```

**Key point:** The registry network handles discovery. Invocation is direct (agent-to-agent). The registry is not in the data path for invocations — only for finding agents.

### 11.2 Agent Updates Capabilities (No Registry Change Needed)

```
Agent Owner                    Agent Card URL                 Registry Network
    │                              │                              │
    │  Agent learns new skill      │                              │
    │  (e.g., adds Korean          │                              │
    │   translation)               │                              │
    │                              │                              │
    ├── Update Agent Card ────────►│                              │
    │   at agent_url               │  (just a JSON file update)   │
    │                              │                              │
    │                              │  Registry caches expire      │
    │                              │  naturally (TTL)             │
    │                              │                              │
    │  OPTIONAL: If new tags       │                              │
    │  needed on registry record   │                              │
    ├── agentdns update ──────────────────────────────────────────►│
    │   --add-tags "korean"        │                         (gossip update)
    │                              │                              │
```

**This is the power of the static/dynamic split.** 90% of agent changes don't touch the registry at all.

---

## 12. Consistency & Conflict Resolution

### 12.1 Eventual Consistency

The network is **eventually consistent**. This means:
- A newly registered agent might not appear in search on all registries for 10-30 seconds
- A deregistered agent might still appear briefly on some registries
- This is acceptable (same as DNS propagation delay)

### 12.2 Conflict Resolution

**Registry records:** Last-Writer-Wins (LWW) based on `updated_at` timestamp
- If two updates arrive with different timestamps → most recent wins
- If same timestamp (rare) → deterministic tiebreaker on signature hash

**Gossip deduplication:**
- Each announcement has `agent_id` + `timestamp`
- If a registry receives an announcement it's already seen (or with an older timestamp than what it has) → discard
- Prevents gossip storms and stale data overwriting fresh data

### 12.3 Tombstone Garbage Collection

When an agent is deregistered:
1. A tombstone record is created with a TTL (default: 7 days)
2. Tombstone propagates via gossip (like any announcement)
3. Peers remove the agent from their indexes upon receiving the tombstone
4. After TTL expires, the tombstone itself is garbage collected

**Why 7 days?** Ensures that even nodes that were offline for days will receive the tombstone when they reconnect and sync.

---

## 13. Security Considerations

### 13.1 Threat Model

| Threat | Mitigation |
|---|---|
| **Fake agent registration** | Agents must sign records with their private key. Anyone can verify. |
| **Sybil attack (flood fake agents)** | Rate limiting per IP/key. Optional proof-of-work for registration. Optional staking. |
| **Gossip poisoning (fake announcements)** | All gossip signed by originating registry. Verify before accepting. |
| **Registry impersonation** | Registry keypair authentication during peering. Mutual TLS. |
| **Agent Card tampering** | Agent Cards are signed. Verify signature against public key in registry record. |
| **Search result manipulation** | Trust scoring deranks low-reputation agents. Multiple registries provide independent results. |
| **Eclipse attack (isolate a registry from honest peers)** | Maintain diverse peer set. Periodically re-bootstrap from seed nodes. |
| **Replay attacks** | Timestamps + nonces in signed messages. Reject old messages. |

### 13.2 Privacy

**What's public:**
- Agent registry records (agent_id, name, category, tags, summary, agent_url)
- Agent Cards (capabilities, pricing, trust stats)

**What's private:**
- Search queries are seen by the querying registry and its peers — NOT globally broadcast
- Invocation data never touches the registry network
- Agent owners can use pseudonymous DIDs (no real-world identity required)

**Enhancement for privacy-sensitive environments:**
- Registry can offer encrypted Agent Cards (accessible only with decryption key from the agent owner)
- Private registries that don't gossip externally (enterprise use case)

---

## 14. Deployment Topologies

### 14.1 Public Open Network

```
Bootstrap Seeds (3-5 well-known nodes)
        │
        ▼
┌───────────────────────────────────────────┐
│           GLOBAL MESH                      │
│  Registry A ── Registry B ── Registry C    │
│       │            │            │          │
│  Registry D ── Registry E ── Registry F    │
│       │            │            │          │
│  Registry G ── Registry H ── Registry I    │
│  ...hundreds/thousands of registries...    │
└───────────────────────────────────────────┘
```

Anyone joins. Full gossip. Global discovery.

### 14.2 Enterprise Private Network

```
┌─────────────────────────────────────┐
│  COMPANY PRIVATE MESH                │
│  (internal registries only)          │
│                                      │
│  Registry HQ ── Registry EU          │
│       │              │               │
│  Registry US ── Registry APAC        │
│                                      │
│  Firewall: no external gossip        │
│  All agents internal only            │
└─────────────────────────────────────┘
```

Same software. No bootstrap seeds to public network. Internal-only peering.

### 14.3 Hybrid (Enterprise + Public)

```
┌──────────────────┐         ┌──────────────────────┐
│  COMPANY MESH     │         │  PUBLIC MESH          │
│  (private agents) │◄──────►│  (public agents)      │
│                   │ Gateway │                       │
│  Registry A       │  Node   │  Registry X           │
│  Registry B       │         │  Registry Y           │
└──────────────────┘         └──────────────────────┘
```

Gateway node bridges private and public. Selective exposure — only approved agents are visible externally.

---

## 15. Scaling Properties

| Metric | Behavior | Notes |
|---|---|---|
| **Registries (nodes)** | Horizontal — more nodes = more capacity | Each node is independent |
| **Agents** | Distributed — no single bottleneck | Gossip index is lightweight per agent (~400 bytes) |
| **Search latency** | ~100-300ms (local) / ~200-500ms (federated) | Parallel fan-out, bloom filter pruning |
| **Registration propagation** | ~10-30s for 10K nodes | Gossip convergence in O(log n) rounds |
| **Storage per node** | ~400MB for 1M agents (gossip index) | Only stores local agents fully |
| **Bandwidth per node** | ~1MB/hour gossip (1K new agents/hour) | Lightweight announcements only |
| **Network partition** | Nodes in same partition still work | Heal on reconnect, CRDT merge |

### Estimated Capacity by Node Size

| Node Class | RAM | Disk | Local Agents | Gossip Index | Search Latency |
|---|---|---|---|---|---|
| **Raspberry Pi** | 1GB | 16GB | 1K | 100K | 50ms local |
| **VPS (small)** | 2GB | 50GB | 10K | 500K | 30ms local |
| **VPS (medium)** | 8GB | 200GB | 100K | 2M | 20ms local |
| **Dedicated** | 32GB | 1TB | 500K | 10M | 10ms local |

---

## 16. Implementation Phases

### Phase 1: Single Node (Weeks 1-4)
**Goal:** Prove the agent card format, registration, and search quality

- [ ] SQLite-backed local registry
- [ ] Agent registration + validation + signature verification
- [ ] Agent Card fetch + cache + signature verification
- [ ] BM25 keyword search (Tantivy)
- [ ] Semantic search (ONNX + MiniLM + HNSW)
- [ ] Ranking algorithm (weighted scoring + RRF)
- [ ] REST API (register, search, resolve, card)
- [ ] CLI (init, start, register, search, resolve)
- [ ] Docker image

**Deliverable:** A standalone agent registry that works as a single node. Already useful for private deployments.

### Phase 2: Mesh Networking (Weeks 5-8)
**Goal:** Multiple registries discover each other and share data

- [ ] libp2p integration (peer discovery, transport)
- [ ] Kademlia DHT (peer routing)
- [ ] Gossip protocol (agent announcements, propagation)
- [ ] Gossip index (store + search announcements from other registries)
- [ ] Peer management (connect, heartbeat, eviction)
- [ ] Bootstrap seed nodes
- [ ] Multi-node integration tests (docker-compose testbed)

**Deliverable:** A mesh of registries that share agent announcements. Search still local per node.

### Phase 3: Federated Search (Weeks 9-12)
**Goal:** Search the entire network from any node

- [ ] Bloom filter construction + exchange during heartbeats
- [ ] Federated search fan-out (query routing via bloom filters)
- [ ] Result merging (dedup + RRF across peers)
- [ ] Agent Card enrichment for top results
- [ ] Search timeout + partial results handling
- [ ] Performance benchmarks (latency, recall, precision)

**Deliverable:** Any node can search the entire network. Core product is feature-complete.

### Phase 4: Trust & Reputation (Weeks 13-16)
**Goal:** Agents earn trust through behavior, not just identity

- [ ] Invocation outcome tracking (via registry proxying or consumer reporting)
- [ ] Reputation attestation format + signing
- [ ] Attestation gossip (periodic, signed summaries)
- [ ] EigenTrust implementation (trust-weighted aggregation)
- [ ] Trust score in search ranking
- [ ] Cold start mechanisms (vouching, grace period)

**Deliverable:** Search results factor in agent reliability. Bad agents sink in rankings.

### Phase 5: Advanced Features (Weeks 17+)
**Goal:** Production hardening + advanced capabilities

- [ ] LSH-based semantic DHT routing (V2 search optimization)
- [ ] Payment integration (x402 micropayments for premium agents)
- [ ] Private registries (enterprise mode — no external gossip)
- [ ] Hybrid deployment (gateway nodes bridging private + public)
- [ ] Web dashboard (network stats, agent browser, search UI)
- [ ] SDKs (Python, JavaScript, Go, Rust)
- [ ] Agent Card builder tool (generate valid Agent Cards from templates)

---

## 17. Comparison to Existing Systems

| Feature | Agent DNS | Traditional DNS | NANDA | AGNTCY ADS | Zynd | A2A Cards |
|---|---|---|---|---|---|---|
| **Decentralized** | ✅ P2P mesh | ✅ Hierarchical | ✅ Federated | ✅ DHT | ✅ P2P mesh | ❌ No registry |
| **Semantic search** | ✅ BM25 + embeddings | ❌ | ❌ | ✅ Taxonomy | ✅ BM25 + embeddings | ❌ |
| **Dynamic metadata** | ✅ Static pointer → dynamic card | ❌ | Partial (AgentFacts URL) | ❌ All in registry | ❌ All in registry | ✅ Self-hosted card |
| **Trust/reputation** | ✅ EigenTrust | ❌ | ✅ W3C VCs | ✅ Sigstore | ✅ EigenTrust | ❌ Self-declared |
| **Anyone can run a node** | ✅ | ❌ (requires delegation) | ✅ | ✅ | ✅ | N/A |
| **Privacy** | ⚡ Basic (pseudonymous) | ❌ | ✅ Strong (dual-path) | ⚡ Basic | ⚡ Basic | ❌ |
| **Standards body** | ❌ (new) | ✅ IETF | ✅ W3C VCs | ✅ IETF draft | ❌ | ✅ Google spec |
| **Static/dynamic split** | ✅ Core design | ❌ | Partial | ❌ | ❌ | ✅ (card is the dynamic data) |
| **Production ready** | ❌ (building) | ✅ | ❌ | ⚡ Early | ❌ | ✅ |

### Key Differentiator

The **static pointer → dynamic card** split is the core architectural insight that separates this from other proposals:

- **NANDA** has a similar idea (lean record → AgentFacts URL) but doesn't implement semantic search
- **A2A Agent Cards** are self-hosted but have no registry network — you need to already know the URL
- **AGNTCY** and **Zynd** put everything in the registry, creating replication pressure when agents update frequently
- **Agent DNS** combines the best of both: lightweight registry records that replicate cheaply + rich Agent Cards that update freely without touching the network

---

## 18. Open Questions & Future Work

1. **Cross-registry agent migration:** What happens when an agent wants to move its home registry? Need a migration protocol that preserves agent_id and history.

2. **Agent Card versioning:** Should Agent Cards have schema versions? What happens when the schema evolves?

3. **Search query privacy:** Can we support encrypted queries (e.g., via homomorphic encryption or private information retrieval) so registries don't learn what consumers are searching for?

4. **Governance:** Who decides on protocol upgrades? Rough consensus + running code (IETF model)? Token-weighted voting? Benevolent dictator?

5. **Abuse prevention:** How to handle agents that spam registrations, serve malware, or violate terms? Need a distributed moderation mechanism beyond trust scoring.

6. **Agent Card availability:** What if an agent's `agent_url` goes down? Registries could cache the last-known Agent Card and serve it with a "stale" warning.

7. **Multi-protocol invocation:** The current design assumes HTTP for Agent Cards and invocation. Should the registry support registering agents reachable via other protocols (gRPC, WebSocket-only, MQTT for IoT agents)?

---

## TL;DR

- **Registry records are static pointers** (~500 bytes, replicated across the mesh via gossip)
- **Agent Cards are dynamic metadata** (2-10KB, hosted by the agent at a stable URL, fetched on-demand)
- **Anyone can run a registry node** — it automatically joins the mesh and becomes a shard
- **Search is hybrid** — BM25 keywords + semantic embeddings, federated across peers with bloom filter routing
- **Trust is earned** — EigenTrust reputation from observed invocations, not self-declared
- **The network is the registry** — no single point of control, no permission needed

**The split between static registry records and dynamic Agent Cards is the key design insight.** It keeps the gossip network lightweight while allowing agents to evolve their capabilities freely.

---

*This is a living document. Update as implementation progresses.*
