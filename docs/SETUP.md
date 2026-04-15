# Agent DNS — Setup Guide

How to set up Agent DNS registries for local development and production, including ZNS handle claiming and domain verification.

---

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Installation](#installation)
3. [Local Development Setup](#local-development-setup)
   - [Single Node](#single-node-quickstart)
   - [3-Node Cluster](#3-node-cluster-local)
   - [Docker Compose](#docker-compose)
4. [Production Setup](#production-setup)
   - [Single Registry Node](#single-registry-node)
   - [Multi-Node Mesh](#multi-node-mesh)
   - [TLS & Domain Configuration](#tls--domain-configuration)
5. [Developer Identity & Onboarding](#developer-identity--onboarding)
6. [Registering Agents](#registering-agents)
7. [ZNS: Claiming Handles & Names](#zns-claiming-handles--names)
   - [Claiming a Handle](#claiming-a-handle)
   - [Domain Verification (DNS)](#domain-verification-dns)
   - [GitHub Verification](#github-verification)
   - [Registering Agent Names (FQANs)](#registering-agent-names-fqans)
8. [Registry Identity Verification](#registry-identity-verification)
   - [Layer 1: TLS Certificate](#layer-1-tls-certificate)
   - [Layer 2: Registry Identity Proof](#layer-2-registry-identity-proof-rip)
   - [Layer 3: DNS TXT Record](#layer-3-dns-txt-record)
   - [Layer 4: Peer Attestation](#layer-4-peer-attestation)
9. [Configuration Reference](#configuration-reference)
10. [Search & Embedding Backends](#search--embedding-backends)
11. [Monitoring & Operations](#monitoring--operations)
12. [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Required
- **Go 1.24+** — [Download](https://go.dev/dl/)
- **PostgreSQL 16+** — Database for agent records
- **Git** — For cloning the repository

### Optional
- **Redis 7+** — Caching layer (improves performance, not required)
- **Rust** — Required only if using ONNX embedding backend
- **Docker** — For containerized deployment

---

## Installation

### Automated (Recommended)

```bash
git clone https://github.com/agentdns/agent-dns.git
cd agent-dns

# Interactive installation — choose embedding backend and model
./install.sh

# Or: quick install with recommended defaults (ONNX + bge-small-en-v1.5)
./quick-install.sh
```

The installer will:
1. Detect your OS and architecture
2. Check Go installation
3. Prompt for embedding backend (Hash / ONNX / HTTP)
4. Build and install Rust tokenizers library (if ONNX selected)
5. Download ONNX Runtime (if ONNX selected)
6. Build and install the `agentdns` binary to `/usr/local/bin`
7. Create default config at `~/.zynd/config.toml`
8. Download the selected embedding model (if ONNX)

### Manual Build

```bash
# Without ONNX (no external dependencies)
CGO_ENABLED=0 go build -o agentdns -ldflags="-s -w" ./cmd/agentdns
sudo mv agentdns /usr/local/bin/

# With ONNX (requires Rust tokenizers + ONNX Runtime installed)
CGO_ENABLED=1 go build -o agentdns -ldflags="-s -w" ./cmd/agentdns
sudo mv agentdns /usr/local/bin/
```

### Verify Installation

```bash
agentdns version
# agent-dns v0.2.0
```

---

## Local Development Setup

### Single Node (Quickstart)

The fastest way to get a running registry for development.

**1. Start PostgreSQL**

```bash
# Using Docker
docker run -d --name agentdns-postgres \
  -e POSTGRES_USER=agentdns \
  -e POSTGRES_PASSWORD=agentdns \
  -e POSTGRES_DB=agentdns \
  -p 5432:5432 \
  postgres:16

# Or use an existing PostgreSQL instance — just create the database:
createdb -U postgres agentdns
```

**2. (Optional) Start Redis**

```bash
docker run -d --name agentdns-redis \
  -p 6379:6379 \
  redis:7
```

**3. Initialize the Node**

```bash
agentdns init
```

This creates `~/.zynd/` containing:
- `identity.json` — Ed25519 keypair (the node's permanent identity)
- `config.toml` — Default configuration

**4. Configure the Database**

Edit `~/.zynd/config.toml`:

```toml
[registry]
postgres_url = "postgres://agentdns:agentdns@localhost:5432/agentdns?sslmode=disable"

[redis]
url = "redis://localhost:6379/0"   # Leave empty to skip Redis
```

**5. Start the Registry**

```bash
agentdns start
```

The node starts:
- HTTP API on `http://localhost:8080`
- Mesh transport on port 4001
- Swagger docs at `http://localhost:8080/swagger/`

**6. Test It**

```bash
# Health check
curl http://localhost:8080/health

# Register a test agent
agentdns register \
  --name "TestAgent" \
  --agent-url "https://example.com/.well-known/agent.json" \
  --category "tools" \
  --summary "A test agent"

# Search
agentdns search "test agent"

# Check status
agentdns status
```

### 3-Node Cluster (Local)

For testing gossip, federation, and ZNS across multiple registries.

**1. Start PostgreSQL and Redis**

```bash
docker run -d --name agentdns-postgres \
  -e POSTGRES_USER=agentdns \
  -e POSTGRES_PASSWORD=agentdns \
  -e POSTGRES_DB=agentdns \
  -p 5432:5432 \
  -v $(pwd)/scripts/init-db.sql:/docker-entrypoint-initdb.d/init-db.sql \
  postgres:16

docker run -d --name agentdns-redis -p 6379:6379 redis:7
```

The `init-db.sql` script creates three separate databases: `agentdns`, `agentdns_b`, `agentdns_c`.

**2. Initialize Three Nodes**

```bash
# Create separate data directories
mkdir -p /tmp/node-a /tmp/node-b /tmp/node-c

# Initialize each node (generates unique keypairs)
AGENTDNS_DATA_DIR=/tmp/node-a agentdns init
AGENTDNS_DATA_DIR=/tmp/node-b agentdns init
AGENTDNS_DATA_DIR=/tmp/node-c agentdns init
```

**3. Configure Each Node**

Use the provided multi-node configs, or create your own:

**Node A** (`config/node-a.toml`) — Seed node:
```toml
[node]
name = "registry-a"

[mesh]
listen_port = 4001
bootstrap_peers = []    # No bootstrap — this IS the seed

[registry]
postgres_url = "postgres://agentdns:agentdns@localhost:5432/agentdns?sslmode=disable"

[redis]
url = "redis://localhost:6379/0"

[api]
listen = "0.0.0.0:8080"
```

**Node B** (`config/node-b.toml`) — Peers with A:
```toml
[node]
name = "registry-b"

[mesh]
listen_port = 4002
bootstrap_peers = ["localhost:4001"]    # Connect to Node A

[registry]
postgres_url = "postgres://agentdns:agentdns@localhost:5432/agentdns_b?sslmode=disable"

[redis]
url = "redis://localhost:6379/1"    # Separate Redis DB

[api]
listen = "0.0.0.0:8081"
```

**Node C** (`config/node-c.toml`) — Peers with A:
```toml
[node]
name = "registry-c"

[mesh]
listen_port = 4003
bootstrap_peers = ["localhost:4001"]

[registry]
postgres_url = "postgres://agentdns:agentdns@localhost:5432/agentdns_c?sslmode=disable"

[redis]
url = "redis://localhost:6379/2"

[api]
listen = "0.0.0.0:8082"
```

**4. Start All Three**

In separate terminals:

```bash
agentdns start --config config/node-a.toml
agentdns start --config config/node-b.toml
agentdns start --config config/node-c.toml
```

**5. Verify the Mesh**

```bash
# Check peers on Node A
curl http://localhost:8080/v1/network/peers
# Should show registry-b and registry-c

# Register an agent on Node A
curl -X POST http://localhost:8080/v1/agents \
  -H "Content-Type: application/json" \
  -d '{"name":"MeshTestAgent","agent_url":"https://example.com/agent","category":"tools","summary":"Testing gossip","public_key":"...","signature":"..."}'

# Search on Node B — agent should appear via gossip/federation
curl -X POST http://localhost:8081/v1/search \
  -H "Content-Type: application/json" \
  -d '{"query":"mesh test","federated":true}'
```

### Docker Compose

The easiest way to run a full 3-node cluster with all infrastructure.

**Standard (Hash embedder):**

```bash
# Start PostgreSQL + Redis
docker compose up -d postgres redis

# Uncomment registry services in docker-compose.yml, then:
docker compose up --build
```

**ONNX (ML embedder — better search quality):**

```bash
docker compose -f infrastructure/docker-compose.onnx.yml up --build
```

**Services started:**

| Service | API Port | Mesh Port | Description |
|---------|----------|-----------|-------------|
| `registry-a` | 8080 | 4001 | Seed node |
| `registry-b` | 8081 | 4002 | Peers with A |
| `registry-c` | 8082 | 4003 | Peers with A |
| `postgres` | 5432 | — | Shared PostgreSQL (separate DBs) |
| `redis` | 6379 | — | Shared Redis (separate DBs) |

---

## Production Setup

### Single Registry Node

**1. Provision Infrastructure**

- **Server:** Any Linux VM (2+ vCPU, 4+ GB RAM recommended)
- **PostgreSQL 16+:** Managed (RDS, Cloud SQL) or self-hosted
- **Redis 7+:** Optional but recommended for production
- **Domain:** e.g., `dns01.zynd.ai` with DNS A record pointing to your server
- **TLS Certificate:** Let's Encrypt (recommended) or CA-signed

**2. Install Agent DNS**

```bash
git clone https://github.com/agentdns/agent-dns.git
cd agent-dns
./install.sh    # or ./quick-install.sh
```

**3. Initialize with Domain**

```bash
agentdns init
```

**4. Configure for Production**

Edit `~/.zynd/config.toml`:

```toml
[node]
name = "dns01"
type = "full"
https_endpoint = "https://dns01.zynd.ai"    # Your public HTTPS endpoint
external_ip = "auto"                         # Or set explicitly

[mesh]
listen_port = 4001
max_peers = 50                               # Higher for production
bootstrap_peers = []                          # Add known peers

[registry]
postgres_url = "postgres://agentdns:STRONG_PASSWORD@db.example.com:5432/agentdns?sslmode=require"
max_local_agents = 100000

[search]
embedding_backend = "onnx"                    # Best quality
embedding_model = "bge-small-en-v1.5"
use_improved_keyword = true
max_federated_peers = 10
federated_timeout_ms = 2000
default_max_results = 20

[cache]
max_agent_cards = 100000
agent_card_ttl_seconds = 3600

[redis]
url = "redis://redis.example.com:6379/0"
password = "REDIS_PASSWORD"

[api]
listen = "0.0.0.0:8080"
rate_limit_search = 200
rate_limit_register = 20
cors_origins = ["https://yourdomain.com"]     # Restrict in production

[trust]
eigentrust_iterations = 5

[heartbeat]
enabled = true
inactive_threshold_seconds = 300
sweep_interval_seconds = 60

[dht]
enabled = true
```

**5. Set Up TLS Reverse Proxy**

Agent DNS serves HTTP on port 8080. Use nginx or Caddy to terminate TLS.

**Caddy (simplest — auto-HTTPS with Let's Encrypt):**

```
# /etc/caddy/Caddyfile
dns01.zynd.ai {
    reverse_proxy localhost:8080
}
```

**nginx:**

```nginx
server {
    listen 443 ssl http2;
    server_name dns01.zynd.ai;

    ssl_certificate     /etc/letsencrypt/live/dns01.zynd.ai/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/dns01.zynd.ai/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket support for heartbeat and activity streams
    location ~ ^/v1/(agents/.*/ws|ws/activity)$ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 3600s;
    }
}
```

**6. Run as a Service**

```bash
# systemd service file: /etc/systemd/system/agentdns.service
[Unit]
Description=Agent DNS Registry
After=network.target postgresql.service

[Service]
Type=simple
User=agentdns
ExecStart=/usr/local/bin/agentdns start --config /home/agentdns/.zynd/config.toml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable agentdns
sudo systemctl start agentdns
```

**7. Open Firewall Ports**

| Port | Protocol | Purpose |
|------|----------|---------|
| 443 | TCP | HTTPS API (via reverse proxy) |
| 4001 | TCP | Mesh P2P (gossip, DHT, federation) |

### Multi-Node Mesh

To connect multiple production registries into a mesh:

**On Registry B:**
```toml
[mesh]
bootstrap_peers = ["dns01.zynd.ai:4001"]    # Peer with Registry A
```

**On Registry A (after B connects):**
```toml
# Optionally add B as a bootstrap peer for resilience
[mesh]
bootstrap_peers = ["dns02.zynd.ai:4001"]
```

The mesh is self-healing: nodes automatically reconnect (30-second reconnect loop with exponential backoff). Peer exchange in heartbeats enables discovery of new nodes beyond the bootstrap list.

### TLS & Domain Configuration

For ZNS naming and registry verification, the `https_endpoint` in your config determines your registry's host name:

| Config `https_endpoint` | Registry Host in FQANs |
|------------------------|----------------------|
| `https://dns01.zynd.ai` | `dns01.zynd.ai` |
| `https://registry.acme-corp.com` | `registry.acme-corp.com` |
| `https://agents.example.io:8443` | `agents.example.io` |

This hostname becomes the first part of every FQAN registered on your node:
```
dns01.zynd.ai/acme-corp/doc-translator
^^^^^^^^^^^^^^
  from https_endpoint
```

> **Important: `https_endpoint` is not verified locally.**
>
> The registry does **not** check on startup that the domain in `https_endpoint` actually resolves to itself. You could configure `https_endpoint = "https://dns01.zynd.ai"` on a machine that has nothing to do with that domain, and the registry would mint FQANs under `dns01.zynd.ai/...` without complaint.
>
> **However, the 4-layer verification system catches this at the mesh level:**
>
> | Layer | What catches a false claim |
> |---|---|
> | **TLS (Layer 1)** | Peers connecting to your claimed domain would reach the real owner, not you |
> | **RIP (Layer 2)** | `/.well-known/zynd-registry.json` on the real domain would have a different Ed25519 key than yours |
> | **DNS TXT (Layer 3)** | `_zynd.{domain}` TXT record would list the real registry's key, not yours |
> | **Peer Attestation (Layer 4)** | No peers would vouch for you since all verification layers fail |
>
> So fake FQANs would gossip out, but any peer that verifies registry identity would reject or distrust them. Your registry would remain stuck at the **Self-Announced** tier (lowest trust) and never reach Domain-Verified or higher.
>
> **Bottom line:** Always set `https_endpoint` to a domain you actually control and serve HTTPS from. The protection is enforced by the network, not by your local node.

---

## Developer Identity & Onboarding

Developers are the entities that own and manage agents. Each developer has a unique Ed25519 keypair.

### Open Onboarding (Default)

Anyone can register as a developer:

```bash
# 1. Generate developer keypair
agentdns dev-init
# Creates ~/.zynd/developer.json with Ed25519 keypair
# Output: Developer ID: agdns:dev:f2a1c3e8...

# 2. Register on a registry
agentdns dev-register --registry http://localhost:8080
```

### Restricted Onboarding

For registries that require KYC or approval before registration:

**Registry admin setup:**
```toml
# config.toml
[onboarding]
mode = "restricted"
auth_url = "https://yourorg.com/onboard"    # External KYC/approval page
webhook_secret = "your-shared-secret"        # For webhook authentication
```

```bash
# Generate and set the webhook secret
agentdns onboarding setup
```

**Flow:**
1. Developer visits `auth_url` and completes KYC
2. External system calls `POST /v1/admin/developers/approve` with `Authorization: Bearer {webhook_secret}`
3. Registry creates the developer and returns encrypted private key
4. Developer decrypts key using their state token

---

## Registering Agents

### Via CLI

```bash
# Simple registration
agentdns register \
  --name "CodeReviewBot" \
  --agent-url "https://example.com/.well-known/agent.json" \
  --category "developer-tools" \
  --tags "python,security,code-review" \
  --summary "AI agent that reviews Python code for security vulnerabilities"

# With developer identity (HD-derived key)
agentdns derive-agent --index 0
# Output: Agent keypair derived at index 0

agentdns register \
  --name "CodeReviewBot" \
  --agent-url "https://example.com/.well-known/agent.json" \
  --category "developer-tools" \
  --tags "python,security" \
  --summary "Reviews Python code" \
  --developer-key ~/.zynd/developer.json \
  --agent-index 0
```

### Via API

```bash
curl -X POST https://dns01.zynd.ai/v1/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "CodeReviewBot",
    "agent_url": "https://example.com/.well-known/agent.json",
    "category": "developer-tools",
    "tags": ["python", "security"],
    "summary": "Reviews Python code for security vulnerabilities",
    "public_key": "ed25519:<base64-public-key>",
    "signature": "ed25519:<base64-signature>",
    "developer_id": "agdns:dev:f2a1c3e8...",
    "developer_proof": {
      "developer_public_key": "ed25519:<base64>",
      "entity_index": 0,
      "developer_signature": "ed25519:<base64>"
    },
    "entity_name": "code-review-bot",
    "version": "1.0.0"
  }'
```

If `entity_name` is provided and the developer has a claimed handle, the FQAN is created automatically (e.g., `dns01.zynd.ai/acme-corp/code-review-bot`).

### Agent Card Hosting

Your agent must host an Agent Card at its `agent_url`. The registry fetches this card for search enrichment and liveness.

**Minimal Agent Card** (`/.well-known/agent.json`):
```json
{
  "agent_id": "agdns:7f3a9c2e...",
  "version": "1.0.0",
  "status": "online",
  "capabilities": [{
    "name": "code-review",
    "description": "Reviews code for security issues",
    "protocols": ["a2a"],
    "languages": ["python"]
  }],
  "endpoints": {
    "invoke": "https://example.com/v1/invoke",
    "health": "https://example.com/health"
  }
}
```

**Zynd Agent Card** (`/.well-known/zynd-agent.json`) — extended format with protocol sections:
```json
{
  "zynd": {
    "version": "1.0",
    "fqan": "dns01.zynd.ai/acme-corp/code-review-bot",
    "agent_id": "agdns:7f3a9c2e...",
    "developer_id": "agdns:dev:f2a1c3e8...",
    "developer_handle": "acme-corp",
    "public_key": "ed25519:<base64>",
    "home_registry": "dns01.zynd.ai"
  },
  "agent": {
    "name": "CodeReviewBot",
    "description": "Reviews code for security vulnerabilities",
    "version": "1.0.0",
    "category": "developer-tools",
    "tags": ["python", "security"]
  },
  "endpoints": {
    "invoke": "https://example.com/v1/invoke",
    "health": "https://example.com/health"
  },
  "protocols": {
    "a2a": {
      "version": "1.0",
      "card_url": "https://example.com/.well-known/agent-card.json"
    },
    "mcp": {
      "version": "1.0",
      "transport": "streamable-http",
      "endpoint": "https://example.com/mcp"
    }
  }
}
```

---

## ZNS: Claiming Handles & Names

The Zynd Naming Service gives agents human-readable addresses like `dns01.zynd.ai/acme-corp/doc-translator` instead of opaque hashes like `agdns:7f3a9c2e`.

### Claiming a Handle

A handle is a human-readable alias for your developer ID. It becomes the middle part of every FQAN.

```bash
# Self-claimed (unverified, first-come-first-served)
agentdns dev-claim-handle --handle acme-corp
```

**Handle rules:**
- Lowercase letters, numbers, and hyphens only
- 3–40 characters
- Must start with a letter
- No consecutive hyphens (`a--b` is invalid)
- No trailing hyphens (`acme-` is invalid)
- Reserved names blocked: `zynd`, `system`, `admin`, `test`, `root`, `registry`, `anonymous`, `unknown`

**Check availability:**
```bash
curl https://dns01.zynd.ai/v1/handles/acme-corp/available
# {"available": true}
```

**Via API:**
```bash
curl -X POST https://dns01.zynd.ai/v1/handles \
  -H "Content-Type: application/json" \
  -d '{
    "handle": "acme-corp",
    "developer_id": "agdns:dev:f2a1c3e8...",
    "public_key": "ed25519:<base64>",
    "signature": "ed25519:<base64>"
  }'
```

### Domain Verification (DNS)

Prove you own a domain to get a verified badge and priority in search results.

**Step 1: Add a DNS TXT record**

Add this record to your domain's DNS:

```
_zynd-verify.acme-corp.com  TXT  "<your-developer-ed25519-public-key>"
```

For example:
```
_zynd-verify.acme-corp.com  TXT  "gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw="
```

You can also use the `ed25519:` prefix:
```
_zynd-verify.acme-corp.com  TXT  "ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw="
```

**Step 2: Trigger verification**

```bash
agentdns dev-claim-handle --handle acme-corp --verify-dns acme-corp.com
```

Or via API:
```bash
curl -X POST https://dns01.zynd.ai/v1/handles/acme-corp/verify \
  -H "Content-Type: application/json" \
  -d '{
    "method": "dns",
    "proof": "acme-corp.com"
  }'
```

The registry will:
1. Look up `_zynd-verify.acme-corp.com` TXT records
2. Check if any record contains the developer's Ed25519 public key
3. If matched, mark the handle as verified

**DNS propagation:** Allow up to 5 minutes for DNS changes to propagate before verifying.

### GitHub Verification

Link your GitHub account for verification:

```bash
agentdns dev-claim-handle --handle johndoe --verify-github
```

Or via API:
```bash
curl -X POST https://dns01.zynd.ai/v1/handles/johndoe/verify \
  -H "Content-Type: application/json" \
  -d '{
    "method": "github",
    "proof": "johndoe"
  }'
```

### Registering Agent Names (FQANs)

Once you have a handle, you can give your agents human-readable names.

**Automatic (at registration time):**
```bash
agentdns register \
  --name "DocTranslator" \
  --agent-url "https://translator.acme-corp.com/.well-known/agent.json" \
  --category "nlp" \
  --tags "translation,documents" \
  --developer-key ~/.zynd/developer.json \
  --agent-index 1 \
  --agent-name doc-translator \
  --version 2.1.0
```

This creates the FQAN `dns01.zynd.ai/acme-corp/doc-translator@2.1.0` automatically.

**Manual (after registration):**
```bash
curl -X POST https://dns01.zynd.ai/v1/names \
  -H "Content-Type: application/json" \
  -d '{
    "entity_name": "doc-translator",
    "developer_handle": "acme-corp",
    "agent_id": "agdns:7f3a9c2e...",
    "version": "2.1.0",
    "capability_tags": ["nlp", "translation"],
    "signature": "ed25519:<base64>"
  }'
```

**Resolve by name:**
```bash
# Via CLI
agentdns resolve acme-corp/doc-translator

# Via API
curl https://dns01.zynd.ai/v1/resolve/acme-corp/doc-translator
```

**Response:**
```json
{
  "fqan": "dns01.zynd.ai/acme-corp/doc-translator",
  "agent_id": "agdns:7f3a9c2e...",
  "developer_id": "agdns:dev:f2a1c3e8...",
  "developer_handle": "acme-corp",
  "registry_host": "dns01.zynd.ai",
  "version": "2.1.0",
  "agent_url": "https://translator.acme-corp.com/.well-known/agent.json",
  "public_key": "ed25519:<base64>",
  "trust_score": 0.87,
  "verification_tier": "domain-verified",
  "status": "active"
}
```

### DNS Bridge (Optional)

If your agent is hosted on non-Zynd infrastructure, you can add a DNS TXT record to point to its FQAN:

```
_zynd.translator.acme-corp.com  TXT  "fqan=dns01.zynd.ai/acme-corp/doc-translator"
```

This enables DNS-native discovery: anyone can look up `_zynd.translator.acme-corp.com` to find the agent's FQAN, then resolve it through the Zynd network.

---

## Registry Identity Verification

When your registry node participates in the mesh, other nodes need to trust that you are who you claim to be. The 4-layer verification system binds your domain to your cryptographic identity.

### Layer 1: TLS Certificate

**Automatic.** If you serve your API over HTTPS (via Let's Encrypt, etc.), every client connection already verifies your domain ownership. No extra configuration needed.

### Layer 2: Registry Identity Proof (RIP)

The RIP document binds your domain, TLS certificate, and Ed25519 key in one signed document.

**Setup:**

Your registry automatically serves the RIP at `/.well-known/zynd-registry.json` when `https_endpoint` is configured:

```toml
[node]
https_endpoint = "https://dns01.zynd.ai"
```

The document looks like:
```json
{
  "type": "registry-identity-proof",
  "version": "1.0",
  "domain": "dns01.zynd.ai",
  "registry_id": "agdns:registry:a1b2c3d4...",
  "ed25519_public_key": "gKH4VSJ838fG1jg6Y14EwwAkQ5PbXs...",
  "tls_spki_fingerprint": "sha256:b4de3a9f...",
  "issued_at": "2026-03-27T10:00:00Z",
  "expires_at": "2027-03-27T10:00:00Z",
  "signature": "ed25519:..."
}
```

**How SPKI fingerprints work:**
- The fingerprint is the SHA-256 hash of your TLS certificate's Subject Public Key Info (SPKI)
- Unlike a full cert hash, the SPKI fingerprint stays the same when you renew your certificate (as long as you keep the same private key)
- This means Let's Encrypt renewals every 90 days don't break verification

**Extract your SPKI fingerprint manually:**
```bash
openssl x509 -in /path/to/cert.pem -pubkey -noout | \
  openssl pkey -pubin -outform der | \
  openssl dgst -sha256 -hex
```

### Layer 3: DNS TXT Record

Publish your registry's identity in DNS for pre-connection verification:

```bash
# Add this DNS TXT record:
_zynd.dns01.zynd.ai  TXT  "v=zynd1 id=agdns:registry:a1b2c3d4 key=ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXs..."
```

**Format:** Space-separated key=value pairs:
- `v=zynd1` — Protocol version
- `id=agdns:registry:...` — Your registry ID
- `key=ed25519:...` — Your Ed25519 public key (base64)

**Benefits:**
- Other registries can verify your identity before connecting (TOFU)
- With DNSSEC enabled, this provides cryptographic tamper-proofing
- Enables Trust-on-First-Use: peers can pre-verify gossip senders

### Layer 4: Peer Attestation

After you connect to the mesh and other registries verify your TLS + RIP + DNS, they can vouch for you by publishing peer attestations:

```json
{
  "type": "peer-attestation",
  "attester_id": "agdns:registry:existing-peer...",
  "subject_id": "agdns:registry:a1b2c3d4...",
  "verified_layers": ["tls", "rip", "dns_txt"],
  "attested_at": "2026-03-27T10:05:00Z",
  "signature": "ed25519:..."
}
```

After 3 peer attestations (configurable), your registry reaches **mesh-verified** tier — the highest trust level.

### Verification Tiers Summary

| Tier | What You Need | Trust Level |
|------|---------------|-------------|
| **Self-Announced** | Just run `agentdns init` | Lowest — no domain proof |
| **Domain-Verified** | TLS cert + `https_endpoint` configured | Medium — domain proved |
| **DNS-Published** | Above + `_zynd.` DNS TXT record | Higher — publicly verifiable |
| **Mesh-Verified** | Above + 3 peer attestations | Highest — vouched by peers |

---

## Configuration Reference

Full configuration lives at `~/.zynd/config.toml`. See `config/default.toml` for the complete reference with all options.

### Key Sections

```toml
[node]
name = "my-registry"               # Display name
type = "full"                       # full | light | gateway
https_endpoint = ""                 # HTTPS URL for ZNS (e.g., "https://dns01.zynd.ai")

[mesh]
listen_port = 4001                  # P2P mesh port
max_peers = 15                      # Maximum peer connections
bootstrap_peers = []                # Initial peers to connect to

[gossip]
max_hops = 10                       # Announcement propagation limit
dedup_window_seconds = 300          # Deduplication window

[registry]
postgres_url = "postgres://..."     # PostgreSQL connection string
max_local_agents = 100000           # Agent capacity

[search]
embedding_backend = "hash"          # hash | onnx | http
embedding_model = "all-MiniLM-L6-v2"
use_improved_keyword = true         # BM25 with field boosting
max_federated_peers = 10            # Peers to query for federated search
federated_timeout_ms = 1500         # Per-peer search timeout

[search.ranking]
text_relevance_weight = 0.30
semantic_similarity_weight = 0.30
trust_weight = 0.20
freshness_weight = 0.10
availability_weight = 0.10

[cache]
max_agent_cards = 50000             # In-memory LRU cache size
agent_card_ttl_seconds = 3600       # Cache TTL

[redis]
url = ""                            # Empty disables Redis
prefix = "agdns:"                   # Key namespace

[api]
listen = "0.0.0.0:8080"            # HTTP listen address
rate_limit_search = 100             # Searches per minute per IP
rate_limit_register = 10            # Registrations per minute per IP
cors_origins = ["*"]                # CORS allowed origins

[heartbeat]
enabled = true
inactive_threshold_seconds = 300    # Mark inactive after 5 min silence
sweep_interval_seconds = 60         # Check every 1 min

[dht]
enabled = true
k = 20                              # Kademlia bucket size
alpha = 3                           # Lookup concurrency

[onboarding]
mode = "open"                       # open | restricted
```

---

## Search & Embedding Backends

### Hash Embedder (Default)

Zero dependencies. Uses feature hashing (FNV-64a) with bigram features. Good for basic search, fast startup.

```toml
[search]
embedding_backend = "hash"
```

### ONNX Embedder (Recommended)

In-process ML model for high-quality semantic search. Requires Rust tokenizers and ONNX Runtime (installed by `install.sh`).

```toml
[search]
embedding_backend = "onnx"
embedding_model = "bge-small-en-v1.5"    # Recommended
embedding_model_dir = "~/.zynd/models"
```

**Available models:**

| Model | Size | Quality | Speed |
|-------|------|---------|-------|
| `all-MiniLM-L6-v2` | 90 MB | Good | Fastest |
| `bge-small-en-v1.5` | 130 MB | Better | Fast |
| `e5-small-v2` | 130 MB | Best (multilingual) | Fast |

**Download models:**
```bash
agentdns models list
agentdns models download bge-small-en-v1.5
```

### HTTP Embedder

Uses an external embedding service (Ollama, OpenAI, or any compatible API).

```toml
[search]
embedding_backend = "http"
embedding_endpoint = "http://localhost:11434/api/embeddings"    # Ollama default
embedding_dimensions = 384
```

---

## Monitoring & Operations

### Health Check

```bash
curl http://localhost:8080/health
# {"status": "ok"}
```

### Node Status

```bash
agentdns status
# Or:
curl http://localhost:8080/v1/network/status
```

Returns: uptime, peer count, local agents, gossip entries, cached cards, node type.

### Peer List

```bash
agentdns peers
# Or:
curl http://localhost:8080/v1/network/peers
```

### Network Stats

```bash
curl http://localhost:8080/v1/network/stats
```

Returns: estimated registries, estimated agents, gossip messages/hour, searches/hour, mesh connectivity.

### Activity Stream (WebSocket)

```bash
# Connect to real-time event stream
wscat -c ws://localhost:8080/v1/ws/activity
```

Events: agent registrations, heartbeats, gossip messages, peer connections, ZNS handle claims.

### Load Testing

```bash
# Register 100 test agents with 10 concurrent workers
agentdns test register --count 100 --concurrency 10 --registry-url http://localhost:8080

# Clean up
agentdns test deregister
```

Reports: P50/P90/P99 latency, RPS, success rate.

### Cleanup

```bash
# Full cleanup (interactive, with confirmations)
./clean.sh
```

---

## Troubleshooting

### Node won't start

**"connection refused" on PostgreSQL:**
- Verify PostgreSQL is running: `pg_isready -h localhost -p 5432`
- Check connection string in `config.toml`
- Ensure the database exists: `createdb -U agentdns agentdns`

**"address already in use":**
- Another process is using port 8080 or 4001
- Check: `lsof -i :8080` and `lsof -i :4001`
- Change ports in config: `[api] listen` and `[mesh] listen_port`

### Peers won't connect

**Bootstrap peer unreachable:**
- Verify the peer is running and its mesh port is accessible
- Check firewall rules: port 4001 must be open
- Try manual connection: `curl http://peer:8080/health`

**Self-connection rejected:**
- This is normal — a node can't connect to itself
- Check that bootstrap_peers doesn't include the node's own address

### Search returns no results

**No local or gossip results:**
- Verify agents are registered: `curl http://localhost:8080/v1/agents/{id}`
- Check search engine index: the agent must be indexed after registration
- Try broader queries without filters

**No federated results:**
- Check peer connectivity: `agentdns peers`
- Verify `federated: true` in search request
- Check federated timeout (default 1.5s may be too short for slow peers)

### ZNS handle not available

- Handles are unique per registry, not globally
- Check availability: `curl http://localhost:8080/v1/handles/{handle}/available`
- Reserved handles cannot be claimed: `zynd`, `system`, `admin`, `test`, `root`, `registry`

### DNS verification fails

- Allow 5 minutes for DNS propagation
- Verify the TXT record: `dig TXT _zynd-verify.yourdomain.com`
- The record must contain your exact public key (with or without `ed25519:` prefix)
- Some DNS providers add quotes — ensure the key value is correct

### ONNX embedder won't load

- Verify model is downloaded: `agentdns models info bge-small-en-v1.5`
- Check `LD_LIBRARY_PATH` includes ONNX Runtime: `echo $LD_LIBRARY_PATH`
- On macOS, use `DYLD_LIBRARY_PATH=/usr/local/lib`
- Ensure `libtokenizers.so` (or `.dylib`) is in `/usr/local/lib`
- Run `ldconfig` (Linux) after installing libraries
