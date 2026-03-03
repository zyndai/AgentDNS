# Agent DNS

A decentralized registry and discovery network for AI agents. Like DNS maps domain names to IP addresses, Agent DNS maps natural-language queries to discoverable, verifiable AI agents across a federated peer-to-peer mesh.

Register agents with cryptographic identity, discover them via hybrid search, and resolve live Agent Cards -- all through a single Go binary backed by PostgreSQL and Redis.

## Architecture

```
                         ┌──────────────────────────────────────────┐
                         │            Agent DNS Network             │
                         │                                          │
   ┌──────────┐          │  ┌────────────┐       ┌────────────┐    │
   │  Client  │──HTTP───▶│  │ Registry A │◄─────▶│ Registry B │    │
   │  / CLI   │          │  │  :8080     │ Gossip │  :8081     │    │
   └──────────┘          │  └─────┬──────┘       └─────┬──────┘    │
                         │        │                     │           │
                         │        │    ┌────────────┐   │           │
                         │        └───▶│ Registry C │◄──┘           │
                         │             │  :8082     │               │
                         │             └────────────┘               │
                         │                                          │
                         │  ┌────────────┐       ┌────────────┐    │
                         │  │ PostgreSQL │       │   Redis     │    │
                         │  │  :5432     │       │   :6379     │    │
                         │  └────────────┘       └────────────┘    │
                         └──────────────────────────────────────────┘
```

### How It Works

1. **Registration** -- Agent owners submit a `RegistryRecord` containing the agent's name, URL, category, tags, public key, and an Ed25519 signature. The agent receives a deterministic ID (`agdns:<sha256-prefix>`) derived from its public key.

2. **Gossip Propagation** -- Registrations, updates, and deregistrations are packaged as `GossipAnnouncements` and propagated across the mesh with hop-count limits and deduplication.

3. **Hybrid Search** -- Clients search using natural-language queries. The engine combines:
   - **BM25 keyword search** for text relevance
   - **Semantic vector search** using cosine similarity
   - Results are ranked with a weighted formula: text relevance (30%), semantic similarity (30%), trust (20%), freshness (10%), availability (10%)

4. **Agent Cards** -- Beyond the static registry record, each agent hosts a dynamic Agent Card at its URL containing live capabilities, pricing, endpoints, and status. Cards are cached in a two-tier cache (in-process LRU + Redis).

5. **Trust & Reputation** -- The EigenTrust algorithm computes global trust scores from signed attestations across registry peers. Trust is transitive but attenuated.

6. **Bloom Filter Routing** -- Peers exchange bloom filters built from agent tags and categories, enabling smart query routing to the most relevant peers.

### Core Components

| Component | Description |
|---|---|
| **Registry Store** | PostgreSQL-backed storage for agent records, gossip entries, tombstones, and attestations |
| **Search Engine** | BM25 keyword + semantic vector search with multi-signal ranking |
| **Gossip Protocol** | Hop-counted announcements with dedup windows for mesh propagation |
| **Peer Manager** | Manages mesh connections, heartbeats, bootstrap, and peer eviction |
| **Agent Card Fetcher** | Two-tier cached fetcher (LRU + Redis) for live agent metadata |
| **EigenTrust** | Decentralized reputation scoring from weighted peer attestations |
| **Identity** | Ed25519 keypair generation, signing, and verification |
| **REST API** | Full HTTP API with Swagger docs, rate limiting, and CORS |

## Prerequisites

- **Go 1.24+** (for building from source)
- **PostgreSQL 16+**
- **Redis 7+** (optional, for caching)
- **Docker & Docker Compose** (for containerized deployment)

## Installation

### From Source

```bash
git clone https://github.com/agentdns/agent-dns.git
cd agent-dns

go build -o agentdns ./cmd/agentdns
```

For a production build with stripped symbols:

```bash
CGO_ENABLED=0 go build -o agentdns -ldflags="-s -w" ./cmd/agentdns
```

### With Docker

```bash
docker build -t agentdns .
```

## Quick Start

### Option 1: Local Binary

```bash
# 1. Initialize node (generates Ed25519 keypair + config at ~/.agentdns/)
agentdns init

# 2. Start the registry node
agentdns start

# 3. Register an agent
agentdns register \
  --name "CodeReviewBot" \
  --agent-url "https://example.com/.well-known/agent.json" \
  --category "developer-tools" \
  --tags "python,security,code-review" \
  --summary "AI agent that reviews Python code for security vulnerabilities"

# 4. Search for agents
agentdns search "code review agent for Python security"

# 5. Resolve an agent's record
agentdns resolve agdns:7f3a9c2e...

# 6. Fetch a live Agent Card
agentdns card agdns:7f3a9c2e...
```

### Option 2: Docker Compose (3-Node Testbed)

Spin up a full local mesh with 3 registry nodes, PostgreSQL, and Redis:

```bash
docker compose up --build
```

This starts:

| Service | Port | Description |
|---|---|---|
| `registry-a` | `8080` (HTTP), `4001` (mesh) | Seed node |
| `registry-b` | `8081` (HTTP), `4002` (mesh) | Peers with A |
| `registry-c` | `8082` (HTTP), `4003` (mesh) | Peers with A |
| `postgres` | `5432` | Shared PostgreSQL (separate DBs per node) |
| `redis` | `6379` | Shared Redis (separate DBs per node) |

### Option 3: Single Docker Container

```bash
docker run -p 8080:8080 -p 4001:4001 \
  -v ./config/default.toml:/config/config.toml \
  agentdns
```

## CLI Reference

```
agentdns <command> [flags]
```

| Command | Description |
|---|---|
| `init` | Initialize a new registry node (generates Ed25519 keypair + default config) |
| `start` | Start the registry node (`--config <path>`, default: `~/.agentdns/config.toml`) |
| `register` | Register an agent (`--name`, `--agent-url`, `--category`, `--tags`, `--summary`) |
| `search` | Search for agents (`--category`, `--min-trust`, `--status`, `--max-results`) |
| `resolve` | Get an agent's registry record by ID |
| `card` | Fetch an agent's live Agent Card by ID |
| `status` | Show node status (uptime, peers, agents, gossip stats) |
| `peers` | List connected mesh peers |
| `deregister` | Remove an agent from the registry |
| `version` | Print version |

### Examples

```bash
# Search with filters
agentdns search "translate english to japanese" --category translation --max-results 10

# Register with tags
agentdns register \
  --name "TranslatorBot" \
  --agent-url "https://translate.example.com/.well-known/agent.json" \
  --category "translation" \
  --tags "english,japanese,multilingual"

# Check node status
agentdns status

# List peers
agentdns peers
```

## API Reference

The REST API is served on port `8080` by default. Interactive Swagger docs are available at `/swagger/`.

### Agent Management

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/v1/agents` | Register a new agent |
| `GET` | `/v1/agents/{agentID}` | Get agent by ID |
| `PUT` | `/v1/agents/{agentID}` | Update an agent |
| `DELETE` | `/v1/agents/{agentID}` | Deregister an agent |
| `GET` | `/v1/agents/{agentID}/card` | Fetch live Agent Card |

### Search

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/v1/search` | Search for agents (natural-language query with filters) |
| `GET` | `/v1/categories` | List all agent categories |
| `GET` | `/v1/tags` | List all agent tags |

### Network

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/v1/network/status` | Node status |
| `GET` | `/v1/network/peers` | List connected peers |
| `POST` | `/v1/network/peers` | Add a peer manually |
| `GET` | `/v1/network/stats` | Network-wide statistics |

### Health & Docs

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/swagger/*` | Swagger UI |

### Example: Register an Agent via API

```bash
curl -X POST http://localhost:8080/v1/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "CodeReviewBot",
    "agent_url": "https://example.com/.well-known/agent.json",
    "category": "developer-tools",
    "tags": ["python", "security"],
    "summary": "Reviews Python code for security vulnerabilities",
    "public_key": "<base64-ed25519-public-key>",
    "signature": "<base64-ed25519-signature>"
  }'
```

### Example: Search for Agents

```bash
curl -X POST http://localhost:8080/v1/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "code review agent for Python",
    "category": "developer-tools",
    "max_results": 10,
    "enrich": true
  }'
```

## Configuration

Configuration is in TOML format. The default config is generated at `~/.agentdns/config.toml` on `agentdns init`. See [`config/default.toml`](config/default.toml) for the full reference with all options documented.

### Key Configuration Sections

```toml
[node]
name = "my-registry"             # Node display name
type = "full"                    # full | light | gateway

[mesh]
listen_port = 4001               # Peer-to-peer mesh port
max_peers = 15
bootstrap_peers = []             # e.g. ["registry-a:4001"]

[registry]
postgres_url = "postgres://agentdns:agentdns@localhost:5432/agentdns?sslmode=disable"

[search]
default_max_results = 20

[search.ranking]
text_relevance_weight = 0.30
semantic_similarity_weight = 0.30
trust_weight = 0.20
freshness_weight = 0.10
availability_weight = 0.10

[cache]
max_agent_cards = 50000
agent_card_ttl_seconds = 3600

[redis]
url = "redis://localhost:6379/0" # Leave empty to disable Redis

[api]
listen = "0.0.0.0:8080"
rate_limit_search = 100          # Requests per minute
rate_limit_register = 10         # Requests per minute
cors_origins = ["*"]
```

## Project Structure

```
agent-dns/
├── cmd/agentdns/           # CLI entry point
├── config/                 # TOML config files (default + per-node)
├── docs/                   # Auto-generated Swagger/OpenAPI specs
├── internal/
│   ├── api/                # HTTP server, handlers, middleware
│   ├── cache/              # Redis cache layer
│   ├── card/               # Agent Card fetcher + LRU cache
│   ├── config/             # Config structs and loader
│   ├── identity/           # Ed25519 keypair and signing
│   ├── mesh/               # Gossip protocol, peer manager, bloom filters
│   ├── models/             # Data models (records, cards, search, trust)
│   ├── ranking/            # Multi-signal ranking algorithm
│   ├── search/             # Search engine (BM25 + semantic)
│   ├── store/              # PostgreSQL storage layer
│   └── trust/              # EigenTrust reputation algorithm
├── scripts/                # Docker entrypoint + DB init scripts
└── tests/                  # Integration tests and fixtures
```

## License

MIT
