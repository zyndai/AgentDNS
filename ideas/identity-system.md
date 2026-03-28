# Zynd Identity System — How It Works

> How developer and agent identities are created, stored, and verified across the registry, dashboard, and CLI.

---

## 1. Identity Formats

Every identity in Zynd is derived from an Ed25519 public key. The public key is hashed to produce a compact, deterministic identifier.

### Developer ID

```
agdns:dev:<first 16 bytes of SHA-256(public_key) as hex>
```

Example: `agdns:dev:a1b2c3d4e5f67890f1e2d3c4b5a69870`

- 40 characters total
- Deterministic: same key always produces same ID
- Generated in Go (`models.GenerateDeveloperID`) and Python (`ed25519_identity.generate_agent_id`)

### Developer Username (Handle)

```
acme-corp
```

- 3-40 lowercase alphanumeric + hyphens, starts with letter
- Unique within the home registry (two registries can each have `acme-corp`)
- Set once during developer registration, cannot be changed
- Reserved names blocked: `zynd`, `system`, `admin`, `test`, `root`, `registry`, `anonymous`, `unknown`

### Agent ID

```
agdns:<first 16 bytes of SHA-256(public_key) as hex>
```

Example: `agdns:7f3a9c2e4b1d5e8af0123456789abcde`

- Same formula as developer ID but without the `dev:` prefix
- Derived from the agent's own Ed25519 public key (not the developer's)

### FQAN (Fully Qualified Agent Name)

```
{registry-host}/{developer-username}/{agent-name}
```

Example: `dns01.zynd.ai/acme-corp/doc-translator`

- Globally unique because: registry host is TLS-verified, username is unique within registry, agent name is unique within developer
- Optional `@version` and `#capability` qualifiers: `dns01.zynd.ai/acme-corp/doc-translator@2.1.0`

### Registry ID

```
agdns:registry:<first 16 bytes of SHA-256(public_key) as hex>
```

- Identifies the registry node itself in the mesh network

---

## 2. Key Generation and Encryption

### Ed25519 Keypair

Every identity starts with an Ed25519 keypair:

```
Private Key: 64 bytes (32-byte seed + 32-byte public key)
Public Key:  32 bytes
```

Serialized as: `ed25519:<base64(raw_bytes)>`

### Encryption Layers

Private keys are never stored in plaintext on the server. The system uses a two-layer encryption scheme:

**Layer 1 — Transit encryption (state-based)**:
- A random `state` is generated per registration session
- Key: `SHA-256(state)` → 32-byte AES key
- Algorithm: AES-256-GCM (12-byte nonce, 16-byte auth tag)
- Format: `base64(nonce || ciphertext || authTag)`
- Purpose: Protect the key in transit between registry and dashboard/CLI

**Layer 2 — Storage encryption (master key)**:
- Key: `PKI_ENCRYPTION_KEY` environment variable (64-char hex = 32 bytes)
- Algorithm: AES-256-GCM (12-byte IV, 16-byte auth tag)
- Format: `base64(iv || authTag || ciphertext)`
- Purpose: Protect the key at rest in the dashboard database

---

## 3. Developer Registration — Dashboard Flow

This is the primary registration path. A developer signs in with Google/GitHub, fills out a profile, and the system creates their cryptographic identity.

```
Browser                     Dashboard Server              Registry
  │                              │                           │
  │ 1. Google/GitHub OAuth       │                           │
  │─────────────────────────────>│                           │
  │                              │                           │
  │ 2. Redirect to /onboard/setup│                           │
  │<─────────────────────────────│                           │
  │                              │                           │
  │ 3. Fill name, username, role │                           │
  │─────────────────────────────>│                           │
  │                              │                           │
  │ 4. Check username            │                           │
  │ GET /api/developer/          │                           │
  │   username-check?username=x  │                           │
  │─────────────────────────────>│                           │
  │                              │ 5. Check local DB         │
  │                              │    + GET /v1/handles/     │
  │                              │      x/available          │
  │                              │──────────────────────────>│
  │                              │<──────────────────────────│
  │  {available: true}           │                           │
  │<─────────────────────────────│                           │
  │                              │                           │
  │ 6. Submit form               │                           │
  │ POST /api/developer/register │                           │
  │ {name, username, role}       │                           │
  │─────────────────────────────>│                           │
  │                              │ 7. Generate state         │
  │                              │    (random 16 bytes)      │
  │                              │                           │
  │                              │ 8. POST /v1/admin/        │
  │                              │    developers/approve     │
  │                              │    {name, state, handle}  │
  │                              │──────────────────────────>│
  │                              │                           │ 9. Generate Ed25519 keypair
  │                              │                           │ 10. developer_id = SHA-256(pubkey)[:16]
  │                              │                           │ 11. Encrypt privkey with SHA-256(state)
  │                              │                           │ 12. Store developer record (with handle)
  │                              │                           │ 13. Gossip to mesh peers
  │                              │  {developer_id,           │
  │                              │   private_key_enc,        │
  │                              │   public_key}             │
  │                              │<──────────────────────────│
  │                              │                           │
  │                              │ 14. Decrypt with state    │
  │                              │ 15. Re-encrypt with       │
  │                              │     master key            │
  │                              │ 16. Store in Prisma DB    │
  │                              │     (userId, developerId, │
  │                              │      publicKey,           │
  │                              │      privateKeyEnc,       │
  │                              │      username, role)      │
  │                              │                           │
  │  {developer_id, public_key,  │                           │
  │   username}                  │                           │
  │<─────────────────────────────│                           │
  │                              │                           │
  │ 17. Redirect to /dashboard   │                           │
```

### What gets stored where

| Location | Data | Encryption |
|----------|------|------------|
| Registry DB (`developers` table) | developer_id, name, public_key, dev_handle, home_registry, signature | None (public data) |
| Dashboard DB (`developer_keys` table) | userId, developerId, publicKey, privateKeyEnc, name, username, role | Private key encrypted with master key |
| Gossip mesh (other registries) | developer_id, name, public_key, dev_handle, home_registry | None (public data) |

---

## 4. Developer Registration — CLI Flow

The CLI uses a browser-based authentication flow where the dashboard acts as an intermediary. The developer's private key is transferred encrypted.

```
CLI                          Browser/Dashboard              Registry
 │                                │                           │
 │ 1. zynd auth login             │                           │
 │    --registry <url>            │                           │
 │                                │                           │
 │ 2. GET /v1/info                │                           │
 │    (check onboarding mode)     │                           │
 │───────────────────────────────────────────────────────────>│
 │  {mode: "restricted",          │                           │
 │   auth_url: "https://..."}     │                           │
 │<───────────────────────────────────────────────────────────│
 │                                │                           │
 │ 3. Generate state              │                           │
 │    = urlsafe(32)               │                           │
 │ 4. Start local HTTP server     │                           │
 │    on ephemeral port           │                           │
 │                                │                           │
 │ 5. Open browser to:            │                           │
 │    auth_url?callback_port=     │                           │
 │    54321&state=<state>         │                           │
 │ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─>│                           │
 │                                │                           │
 │                                │ 6. Google/GitHub login    │
 │                                │ 7. If new: create         │
 │                                │    developer via registry │
 │                                │    (same as dashboard     │
 │                                │     flow steps 7-16)      │
 │                                │                           │
 │                                │ 8. POST /api/onboard/     │
 │                                │    approve                │
 │                                │    {state, callback_port} │
 │                                │                           │
 │                                │ 9. Decrypt stored key     │
 │                                │    with master key        │
 │                                │ 10. Re-encrypt for CLI    │
 │                                │     with SHA-256(state)   │
 │                                │                           │
 │ 11. Callback to localhost:     │                           │
 │     54321/callback?            │                           │
 │     developer_id=...&          │                           │
 │     private_key_enc=...        │                           │
 │<─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ │                           │
 │                                │                           │
 │ 12. Decrypt with               │                           │
 │     SHA-256(state)             │                           │
 │ 13. Save to ~/.zynd/           │                           │
 │     developer.json             │                           │
 │     {public_key, private_key}  │                           │
```

### Key re-encryption path

```
Registry                    Dashboard                    CLI
generates key ──[state]──> decrypts ──[master]──> stores encrypted
                                     ──[state]──> decrypts ──> stores plaintext
```

The private key is encrypted differently at each hop:
1. Registry → Dashboard: AES-GCM with SHA-256(state)
2. Dashboard DB: AES-GCM with master key
3. Dashboard → CLI: AES-GCM with SHA-256(state) (new state)
4. CLI disk: Plaintext JSON (protected by filesystem permissions)

---

## 5. Agent Keypair Derivation

Agent keypairs are deterministically derived from the developer's private key using HD (Hierarchical Deterministic) derivation. This means:
- The developer's single key can produce unlimited agent keys
- If an agent keypair is lost, it can be re-derived from the developer key + index
- Each agent has its own independent keypair for signing

### Derivation Formula

```
input = developer_private_seed (32 bytes)
      || "agdns:agent:"                   (domain separator)
      || big_endian_uint32(index)          (4 bytes)

agent_seed = SHA-512(input)[:32]           (first 32 bytes)
agent_keypair = Ed25519.from_seed(agent_seed)
```

### Index Management

```
~/.zynd/agents/
  ├── my-agent/
  │   └── keypair.json     (index: 0)
  ├── code-reviewer/
  │   └── keypair.json     (index: 1)
  └── doc-translator/
      └── keypair.json     (index: 2)
```

Each `keypair.json` stores the derivation metadata:
```json
{
  "public_key": "base64...",
  "private_key": "base64...",
  "derived_from": {
    "developer_public_key": "base64...",
    "index": 0
  }
}
```

When creating a new agent, the CLI scans all existing agents to find the next unused index.

### Derivation Proof

To prove a developer authorized a specific agent, a cryptographic proof is created:

```
message = agent_public_key_bytes (32 bytes) || big_endian_uint32(index)
signature = developer_private_key.sign(message)

proof = {
  developer_public_key: "ed25519:...",
  agent_index: 0,
  developer_signature: "ed25519:..."
}
```

This proof is verified by the registry during agent registration. Anyone with the developer's public key can verify the proof offline.

---

## 6. Agent Registration with ZNS Name

When an agent registers on the network, it can optionally claim a human-readable name under its developer's handle.

```
CLI                                         Registry
 │                                            │
 │ 1. Load agent keypair                      │
 │    Load developer keypair                  │
 │    Load agent.config.json                  │
 │    (includes agent_name)                   │
 │                                            │
 │ 2. Check name availability                 │
 │    GET /v1/developers/{dev_id}             │
 │    → get dev_handle                        │
 │─────────────────────────────────────────> │
 │                                            │
 │    GET /v1/names/{handle}/                 │
 │    {agent_name}/available                  │
 │─────────────────────────────────────────> │
 │    {available: true}                       │
 │<─────────────────────────────────────────  │
 │                                            │
 │ 3. Create derivation proof                 │
 │    msg = agent_pubkey || uint32(index)     │
 │    sig = dev_key.sign(msg)                 │
 │                                            │
 │ 4. Sign registration payload               │
 │    body = {name, agent_url, category,      │
 │            tags, summary, public_key}      │
 │    sig = agent_key.sign(body)              │
 │                                            │
 │ 5. POST /v1/agents                         │
 │    {name, agent_url, category, tags,       │
 │     summary, public_key, signature,        │
 │     developer_id, developer_proof,         │
 │     agent_name, version}                   │
 │─────────────────────────────────────────> │
 │                                            │ 6. Verify agent signature
 │                                            │ 7. Verify developer exists
 │                                            │ 8. Verify derivation proof
 │                                            │ 9. Generate agent_id from pubkey
 │                                            │ 10. Store RegistryRecord
 │                                            │
 │                                            │ 11. If agent_name provided:
 │                                            │     - Look up developer's handle
 │                                            │     - Check name not taken by
 │                                            │       different key
 │                                            │     - Build FQAN:
 │                                            │       host/handle/agent_name
 │                                            │     - Create ZNS name binding
 │                                            │     - Create version record
 │                                            │
 │                                            │ 12. Gossip agent + name binding
 │                                            │
 │  {agent_id: "agdns:...",                   │
 │   fqan: "dns01.zynd.ai/acme-corp/         │
 │          doc-translator"}                  │
 │<─────────────────────────────────────────  │
```

### Duplicate Name Protection

If an agent name already exists under the same developer:
- **Same public key**: Allowed (idempotent re-registration)
- **Different public key**: Rejected with error "agent name is already registered under {handle} with a different key; choose a different name"

This prevents name squatting while allowing re-registration of the same agent.

---

## 7. Signature Verification Summary

The registry verifies multiple signatures during agent registration:

| What | Signer | Message | When |
|------|--------|---------|------|
| Registration payload | Agent key | `{name, agent_url, category, tags, summary, public_key}` | Every agent registration |
| Derivation proof | Developer key | `agent_public_key_bytes \|\| uint32_be(index)` | When developer_proof provided |
| Update/Delete auth | Agent or Developer key | Request body bytes | Authorization header on PUT/DELETE |
| Gossip announcements | Registry key | Full announcement (minus signature field) | When gossiping to peers |
| ZNS name bindings | Developer key | `{agent_name, developer_handle, agent_id, ...}` | When binding a name |

---

## 8. Identity Resolution

Given any identifier, you can resolve it through the registry:

| Input | API | Returns |
|-------|-----|---------|
| `agdns:7f3a9c2e...` (agent ID) | `GET /v1/agents/{id}` | Registry record |
| `agdns:dev:a1b2c3d4...` (developer ID) | `GET /v1/developers/{id}` | Developer record |
| `acme-corp/doc-translator` (short name) | `GET /v1/resolve/acme-corp/doc-translator` | Full resolution with agent_url, trust, status |
| `acme-corp` (handle) | `GET /v1/handles/acme-corp` | Developer info |

Resolution chain for names: local ZNS table → gossip ZNS entries → DHT fallback.

---

## 9. Where Keys Live

| System | File/Table | Contains | Protection |
|--------|-----------|----------|------------|
| Registry | `developers` table | public_key, developer_id, dev_handle | Public data |
| Registry | `agents` table | public_key, agent_id, developer_proof | Public data |
| Dashboard | `developer_keys` table | privateKeyEnc, publicKey, username | AES-256-GCM (master key) |
| CLI | `~/.zynd/developer.json` | private_key, public_key | Filesystem permissions |
| CLI | `~/.zynd/agents/*/keypair.json` | private_key, public_key, derivation metadata | Filesystem permissions |
