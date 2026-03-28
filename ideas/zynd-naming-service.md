# Zynd Naming Service (ZNS) — Research & Recommendation

> Which naming approach should Zynd adopt, and how should registry endpoints, developer namespaces, and agent names fit together?

---

## 1. Why Zynd Needs Better Naming

Zynd currently identifies agents with `agdns:7f3a9c2e4b1d5e8a...` — a 32-hex-char hash of an Ed25519 public key. The `name` field is free-form UTF-8, not unique, and not resolvable. This works for cryptographic verification but fails for everything else.

The problems are concrete. There is no deterministic resolution — you cannot construct an address from intent the way you type `google.com` and reach Google. Two agents can share the name "sentiment-analyzer" with no conflict detection. The name carries no information about capability, version, or who built the agent. In a federated mesh, the same name on different registries can point to completely different agents. And there is no version pinning — the underlying code can change while the name stays the same.

Every major agent naming system in the industry has recognized these same problems. The question is which solution Zynd should adapt.

---

## 2. Industry Landscape: Seven Naming Approaches

### 2.1 GoDaddy ANS (Agent Name Service)

GoDaddy's approach, launched publicly in November 2025 with an API and Standards site, anchors agent identity directly to DNS domain ownership.

Their naming format ties agents to FQDNs verified through ACME challenges:

```
ans://v1.0.0.chatbot.acme.com
```

Each agent gets four DNS records (`TXT _ans.`, `HTTPS`, `TLSA _443._tcp.`, `TXT _ra-badge.`) that enable decentralized verification. The Registration Authority issues two certificates per agent — a public CA cert for the stable domain and a private CA cert bound to the specific code version. Every code update triggers a new registration with a fresh identity certificate, creating an immutable audit trail in a SCITT-compliant Transparency Log backed by AWS KMS.

GoDaddy's Trust Index scores agents across five independent dimensions (integrity, identity, solvency, behavior, safety) on 0-100 scales, which is more granular than Zynd's single EigenTrust number.

The system is protocol-agnostic through an adapter layer that translates agent records into formats used by A2A, MCP, and other frameworks.

**What Zynd should take from this:** The domain-anchored identity model is excellent for enterprise trust. GoDaddy's insight that DNS is already a decentralized trust anchor is worth absorbing. The version-bound certificate model that prevents silent code changes maps cleanly to Zynd's Layer 1 (build integrity). The five-dimensional trust scoring is superior to a single number.

**What doesn't fit Zynd:** Requiring domain ownership excludes hobbyist and anonymous agents, which Zynd explicitly supports through self-sovereign registration. GoDaddy's centralized Transparency Log conflicts with Zynd's federated mesh philosophy. And DNS resolution is exact-match only — no semantic or capability-based discovery.

### 2.2 OWASP / IETF ANS Specification

The OWASP GenAI Security Project published the ANS specification (v1.0, May 2025), which also formed the basis of IETF draft-narajala-ans-00. GoDaddy's implementation builds on this specification. The naming format encodes semantics directly into the name:

```
Protocol "://" AgentID "." agentCapability "." Provider ".v" Version "." Extension
```

Example: `a2a://textProcessor.DocumentTranslation.AcmeCorp.v2.1.hipaa`

This is the richest name format in the industry. From the name alone you know the communication protocol (A2A), the agent (textProcessor), its capability (DocumentTranslation), the provider (AcmeCorp), the version (2.1), and deployment metadata (HIPAA-compliant). The specification includes a formal resolution algorithm with semver range negotiation, PKI certificate chain verification, and a Protocol Adapter Layer that translates between A2A, MCP, and ACP.

**What Zynd should take from this:** The capability encoding in the name is the right idea — it enables filtering before a full registry lookup. Version negotiation with semver ranges is essential for production agent systems. The Protocol Adapter Layer concept aligns with Zynd's protocol-agnostic stance.

**What doesn't fit Zynd:** The protocol prefix (`a2a://`, `mcp://`) bakes protocol choice into the name itself. Zynd is protocol-agnostic — the same agent might speak A2A, MCP, and JSON-RPC simultaneously. Hardcoding the protocol in the name forces a choice that should live in the Agent Card. The names also become unwieldy at full expansion. And the specification assumes a centralized or semi-centralized Agent Registry, which clashes with Zynd's federated mesh.

### 2.3 IETF BANDAID (Brokered Agent Network for DNS AI Discovery)

BANDAID (draft-mozleywilliams-dnsop-bandaid-00) takes the most conservative approach: zero changes to DNS infrastructure. Agent metadata goes into SVCB records under a structured leaf zone like `_agents.example.com`. Agents discover each other through standard DNS-SD. Security comes from DNSSEC and DANE.

**What Zynd should take from this:** The idea of a DNS bridge for enterprise adoption is worth stealing. Enterprises that already manage DNS zones could publish `_zynd.example.com` records pointing to their Zynd-registered agents, giving IT teams a familiar discovery path without requiring them to run Zynd nodes.

**What doesn't fit Zynd:** Exact-match-only discovery defeats the purpose of Zynd's semantic search. ICANN centralization conflicts with decentralization goals.

### 2.4 ENS + ERC-8004 (Ethereum Name Service)

ENS maps `.eth` names to Ethereum addresses. In 2025, the Ethereum Foundation, Google, MetaMask, and Coinbase developed ERC-8004, which adds three agent-specific registries on top of ENS: an Identity Registry (agents as NFTs), a Reputation Registry (performance history), and a Validation Registry (third-party verification). Agent names look like `shop.agent.eth` or `trade.agent.eth` using delegated subnames.

**What Zynd should take from this:** The three-registry trust model (identity, reputation, validation) is a cleaner separation than Zynd's current single-score approach. The insight that agent names should be portable across wallets, applications, and networks aligns with Zynd's cross-registry goals. ENS's composability principle — naming integrates with existing protocols without restricting environments — matches Zynd's protocol-agnostic design.

**What doesn't fit Zynd:** On-chain resolution adds ~1 second latency, which is unacceptable for agent-to-agent interactions that need sub-100ms response. Gas costs for name registration and updates create friction. The Ethereum-specific infrastructure limits adoption to Web3-native developers.

### 2.5 MIT NANDA Index (Networked Agents and Decentralized AI)

NANDA, originating from MIT, uses a "Quilt of Registries" model where no single registry governs the entire network. Each agent gets a minimal `AgentAddr` record (~120 bytes, Ed25519-signed) that maps a name like `@financial-analyzer` to metadata locations. The full details live in "AgentFacts" documents — cryptographically verifiable W3C Verifiable Credentials with short-lived TTLs (sometimes under 5 minutes).

NANDA's architecture decouples three things: the index (lightweight pointers), the facts (detailed metadata), and the routing (endpoint resolution). This three-tier separation enables privacy-preserving discovery through `PrivateFactsURL` paths and adaptive routing with `AdaptiveRouterURL` for load balancing.

**What Zynd should take from this:** The lean index + rich facts separation mirrors Zynd's existing "static pointer + dynamic Agent Card" architecture. The privacy-preserving discovery via obfuscated paths is valuable for enterprise agents that shouldn't be publicly discoverable. The TTL-scoped endpoint model (static 1-6h, rotating 5-15min, adaptive 30-60s) is more sophisticated than Zynd's fixed 24h TTL.

**What doesn't fit Zynd:** NANDA is still largely academic. The `@`-prefix naming convention is too minimal — it doesn't encode provider, capability, or version.

### 2.6 AGNTCY Agent Directory Service (ADS)

AGNTCY, with an IETF draft (draft-mp-agntcy-ads-00) and implementation, takes a content-addressed approach. Agent records are packaged as OCI (Open Container Initiative) artifacts with Sigstore provenance. Names are content identifiers — `sha256:<digest>` — which are self-authenticating by construction. Discovery uses a Kademlia DHT with semantic taxonomy overlays built on the Open Agentic Schema Framework (OASF).

**What Zynd should take from this:** Content-addressed naming is cryptographically elegant and eliminates squatting by definition (you can't squat on a hash). The OCI artifact approach means any OCI-compliant registry can participate in the network, which is a powerful federation primitive. The Sigstore integration for provenance signing is lighter than full PKI.

**What doesn't fit Zynd:** Content hashes are not human-readable. `sha256:a1b2c3d4...` is even worse than `agdns:7f3a9c2e...` for human ergonomics. The OCI dependency adds infrastructure requirements that most agent developers won't want to manage.

### 2.7 Microsoft Entra Agent ID

Microsoft's enterprise approach integrates agent identity into Azure AD. Agents are managed alongside human users and service principals in the same identity governance infrastructure. Discovery uses Graph APIs with real-time sync.

**What Zynd should take from this:** The lesson is that enterprises will want agent naming to integrate with their existing identity provider. Zynd's restricted onboarding mode (KYC via external auth) already supports this pattern. The registry should make it easy for organizations to map their Entra/Okta/Auth0 identities to Zynd provider namespaces.

**What doesn't fit Zynd:** Fully centralized, Azure-locked, no federation.

---

## 3. What to Adapt: The Decision

After evaluating all seven approaches, Zynd should build a hybrid naming system that combines:

| Take From | Concept | Why |
|---|---|---|
| **GoDaddy ANS** | Domain-anchored provider verification | Hard to fake, enterprise-trusted, maps to Zynd's developer verification tiers |
| **OWASP/IETF ANS** | Capability + version encoding in names | Enables structured discovery without full-text search |
| **NANDA** | Lean index + rich facts separation | Zynd already does this (registry record + Agent Card) — naming should follow the same pattern |
| **AGNTCY** | Content-addressed immutable IDs as the canonical layer | Zynd's `agdns:<hash>` already serves this role; names are an overlay |
| **ENS/ERC-8004** | Portable, composable, open naming that isn't locked to one protocol | Protocol-agnostic naming that works across A2A, MCP, ACP, and whatever comes next |
| **BANDAID** | DNS bridge for enterprise onboarding | Optional `_zynd` DNS records for enterprises that want DNS-native discovery |

And critically, Zynd should NOT adopt:

| Reject | Why |
|---|---|
| Protocol prefix in names (OWASP's `a2a://`, `mcp://`) | Zynd is protocol-agnostic. Same agent, multiple protocols. Protocol belongs in the Agent Card, not the name. |
| Blockchain-based resolution (ENS, 0x01) | Latency and gas cost are incompatible with agent-to-agent speed requirements |
| Domain ownership as a hard requirement (GoDaddy) | Zynd supports self-sovereign agents; domain verification is a trust-tier bonus, not a gate |
| Content hashes as the primary human-facing name (AGNTCY) | Developers need names they can type and remember |

---

## 4. The Zynd Naming Format

### 4.1 Three-Part Naming: Registry / Developer / Agent

The key innovation in Zynd's naming is that every name is scoped to both a registry and a developer. The registry's name comes from its HTTPS endpoint.

**Why not dots?** An earlier design used dots between all parts: `doc-translator.acme.dns01.zynd.ai`. The problem is that registry hostnames already contain dots (`dns01.zynd.ai`), making it impossible to parse where the developer name ends and the registry host begins. A parser seeing `a.b.c.d.e` cannot determine the boundary without external knowledge of the TLD structure.

Zynd uses **slash separators** between the three logical parts, keeping dots only within the registry hostname where they belong:

```
{registry-host}/{developer-handle}/{agent-name}
```

**Concrete examples:**

```
dns01.zynd.ai/acme/doc-translator
registry.agentmesh.io/opentools/sentiment-api
local-node.example.com/johndoe/my-bot
eu-west.zynd.ai/fintech-corp/price-checker
```

This reads left-to-right like a URL path: registry (the authority), then developer (the namespace owner), then agent (the specific service). Anyone reading `dns01.zynd.ai/acme-corp/doc-translator` immediately knows: this is the `doc-translator` agent, built by `acme-corp`, registered on the `dns01.zynd.ai` registry. The format is unambiguous regardless of how many dots appear in the registry hostname.

### 4.2 Registry Names from HTTPS Endpoints

Registry nodes already have HTTPS endpoints. The registry's name in the naming system is derived directly from that endpoint:

| Registry Endpoint | Registry Name in ZNS |
|---|---|
| `https://dns01.zynd.ai` | `dns01.zynd.ai` |
| `https://registry.agentmesh.io` | `registry.agentmesh.io` |
| `https://agents.acme-corp.com` | `agents.acme-corp.com` |
| `https://local-node.example.com:8443` | `local-node.example.com` |

Rules for registry names:

The registry name is the hostname from its configured HTTPS endpoint, stripped of the port and protocol. This is set once during `agentdns init` and stored in the TOML config alongside the existing `[node].name`. The registry name is immutable once published — changing it invalidates all names scoped to that registry. Registry names are verified via TLS certificate validation during mesh peering. A registry claiming to be `dns01.zynd.ai` must present a valid TLS cert for that domain. This reuses GoDaddy ANS's insight that domain ownership is a natural trust anchor, but applies it to registries rather than individual agents.

### 4.3 Developer Handles

Currently developers only have cryptographic address IDs (`agdns:dev:f2a1c3e8...`). That ID remains the canonical identity. The developer **handle** is a human-readable alias that developers claim separately.

**How developers get a handle:**

Claiming a handle is optional, not required. A developer can register agents using only their address ID. But having a handle unlocks the human-readable naming path.

There are three ways a developer can get a handle:

**1. Self-claimed (unverified):** Any developer can claim an available handle on their registry. No proof required, first-come-first-served. These handles are displayed without a verification badge.

**2. Domain-verified:** The developer proves ownership of a domain by placing a DNS TXT record at `_zynd-verify.{domain}` containing their developer public key. If they own `acme-corp.com`, they can claim `acme-corp` or any derivative. Domain-verified handles get a verification badge and priority in search results.

**3. GitHub/OAuth-verified:** The developer links a GitHub account (or other OAuth provider). The handle defaults to their GitHub username unless they choose otherwise. Verified via OAuth flow during registration.

**If a developer never claims a handle**, their agents can still be registered and discovered by address ID, but won't have human-readable FQANs. The address ID is always a valid lookup key.

| Developer | Handle | How Obtained | FQAN Pattern |
|---|---|---|---|
| Acme Corp | `acme-corp` | Domain-verified (`acme-corp.com`) | `dns01.zynd.ai/acme-corp/*` |
| John Doe | `johndoe` | GitHub-verified | `dns01.zynd.ai/johndoe/*` |
| Anonymous dev | `quickbot` | Self-claimed (unverified) | `dns01.zynd.ai/quickbot/*` |
| Dev who never claimed | *(none)* | N/A | Agents reachable only by `agdns:` address ID |

**Handle rules:**
- Lowercase alphanumeric plus hyphens, 3-40 characters, must start with a letter
- Unique within the home registry (two registries can each have an `acme-corp` handle)
- Immutable once claimed — you cannot rename, only register a new handle on a different registry
- Reserved handles (`zynd`, `system`, `admin`, `test`, `root`, `registry`) are blocked on all registries

### 4.4 Agent Names

Agent names are unique within a developer namespace. They follow the same rules as handles: lowercase alphanumeric plus hyphens, 3-40 characters, must start with a letter.

```
dns01.zynd.ai/acme-corp/doc-translator
```

This is the **Fully Qualified Agent Name (FQAN)**. It is globally unique because: the registry host is unique (TLS-verified domain), the developer handle is unique within that registry, and the agent name is unique within that developer's namespace.

### 4.5 Optional Qualifiers: Version and Capability

The FQAN can be extended with version and capability qualifiers for precise resolution:

```
{registry-host}/{developer}/{agent}@{version}#{capability}
```

The `@` delimiter separates the name from the version. The `#` delimiter separates the version from the capability filter. Both are optional.

**Examples:**

```
dns01.zynd.ai/acme-corp/doc-translator                           -- latest stable
dns01.zynd.ai/acme-corp/doc-translator@2.1.0                     -- exact version
dns01.zynd.ai/acme-corp/doc-translator@2                          -- latest v2.x.x
dns01.zynd.ai/acme-corp/doc-translator#nlp.translation            -- capability filter
dns01.zynd.ai/acme-corp/doc-translator@2.1.0#nlp.translation     -- version + capability
```

Version uses semver. When only a major version is specified (`@2`), the resolver returns the highest compatible minor/patch. When omitted entirely, the resolver returns the latest stable (non-prerelease) version.

### 4.6 The `agdns://` URI Scheme

For use in Agent Cards, delegation contracts, ZTPs, and documentation, the full URI form is:

```
agdns://dns01.zynd.ai/acme-corp/doc-translator@2.1.0#nlp.translation
```

This is a valid URI that can appear anywhere a URL can. The scheme `agdns://` tells any system that this name should be resolved through the Zynd naming system, not DNS or HTTP. Note how natural this looks — it's structurally identical to a URL.

### 4.7 Short Names and Search

Within a single registry, agents can be referenced by short names:

```
acme-corp/doc-translator           -- within dns01.zynd.ai, resolves locally
doc-translator                     -- ambiguous, triggers search across mesh
```

When a short name is used, the local registry attempts resolution first. If not found, it falls back to federated search across the gossip mesh. This mirrors how Docker resolves unqualified image names against Docker Hub first.

### 4.8 Why Not Protocol in the Name?

The OWASP ANS specification puts the protocol in the name (`a2a://`, `mcp://`). Zynd explicitly rejects this. The reason is that Zynd is protocol-agnostic — the same agent might serve A2A Agent Cards at `/.well-known/agent.json`, MCP tool definitions, and ACP profiles simultaneously. The protocol is a property of the connection, not the identity.

Instead, the supported protocols are declared in the Agent Card and the registry record's `capability_summary.protocols` field. When resolving a name, the caller can filter by protocol as a query parameter:

```
GET /v1/resolve/acme-corp/doc-translator?protocol=mcp
```

This returns the agent's endpoint along with MCP-specific metadata from the `protocolExtensions` field, without baking the protocol choice into the permanent name.

---

## 5. How This Compares

| Dimension | Zynd ZNS | GoDaddy ANS | OWASP/IETF ANS | NANDA | AGNTCY | ENS |
|---|---|---|---|---|---|---|
| **Name format** | `registry/dev/agent` | `ans://v{ver}.{fqdn}` | `proto://id.cap.provider.ver.ext` | `@agent-name` | `sha256:<digest>` | `name.eth` |
| **Human-readable** | Yes | Yes | Somewhat (long) | Yes | No | Yes |
| **Protocol in name** | No (protocol-agnostic) | No | Yes (scheme prefix) | No | No | No |
| **Registry scoped** | Yes (registry host in name) | No (DNS is the registry) | No | No (quilt of registries) | No (DHT) | No (Ethereum is the registry) |
| **Developer scoped** | Yes (developer namespace) | Via domain | Via Provider field | No | No | Via subnames |
| **Version support** | `@2.1.0` qualifier | In name, cert-bound | `.v2.1` in name | Via AgentFacts | Via OCI tags | No |
| **Capability filter** | `#nlp.translation` qualifier | No | In name | Via AgentFacts | Via taxonomy | No |
| **Identity anchor** | Ed25519 + TLS-verified registry host | Domain ACME | X.509 PKI | Ed25519 + W3C VCs | Sigstore + OCI | Ethereum address |
| **Squatting resistance** | Registry-scoped + dev verification | Domain ownership | Governance authority | Federated | Content-addressed | ETH cost |
| **Decentralized** | Federated mesh | Federated RAs | Semi-centralized | Federated quilt | DHT | Fully on-chain |
| **Latency** | Microseconds (local) to ms (gossip/DHT) | DNS TTL | Registry-dependent | 2-hop resolution | DHT variable | ~1s on-chain |
| **Enterprise bridge** | Optional DNS records | Native DNS | No | No | No | No |

---

## 6. Architecture: Integrated into the Registry Node

Naming should be a module within the existing registry node, not a separate service. The reasoning:

Naming is a thin mapping layer on top of identity, not an independent system. A ZNS name maps to an `agent_id`, which the registry already stores. Adding two database tables and a handful of API endpoints is trivial compared to running a separate node type with its own sync protocol.

The gossip mesh already propagates agent and developer announcements. Name bindings and developer namespace claims are two more announcement types — the plumbing is identical.

Atomic registration matters. If agent registration and name binding are separate operations on separate nodes, you get partial-state problems: an agent exists but its name doesn't, or a name exists but the agent is tombstoned. Integrated means a single API call validates, stores, and gossips everything together.

Performance on the hot path (search) depends on local resolution. An external naming node adds a network hop to every search-time name lookup. Integrated means it's a local database join.

And operationally, Zynd's target is "anyone can run a registry node." Requiring a second node type cuts participation.

---

## 7. How Names Flow Through the System

### 7.1 Registry Setup

When a registry operator runs `agentdns init`, they configure their HTTPS endpoint. This becomes the registry's name in the naming system:

```toml
[node]
name = "dns01"
type = "full"
https_endpoint = "https://dns01.zynd.ai"
# Registry name in ZNS: dns01.zynd.ai (derived automatically)
```

The registry name is gossiped to mesh peers as part of the existing `PeerInfo` structure. Other registries learn that `agdns:registry:a1b2c3d4...` maps to `dns01.zynd.ai`.

### 7.2 Registry Identity Verification: Proving Domain Ownership

A registry claiming to be `dns01.zynd.ai` must prove it actually controls that domain. Without this, any node could announce itself as `dns01.zynd.ai` on the gossip mesh and impersonate the real registry. Zynd uses a three-layer verification model that binds the domain name to the registry's Ed25519 identity key.

**The Problem:** A registry has two identities — its domain (`dns01.zynd.ai`, verified by a CA-issued TLS certificate) and its Zynd keypair (`agdns:registry:a1b2c3...`, self-generated Ed25519). These two identities start out unrelated. Verification means proving the same entity controls both.

#### Layer 1 — TLS Certificate (automatic, verifies at connection time)

When any client connects to `https://dns01.zynd.ai`, the TLS handshake proves the server holds a valid certificate for that domain. The CA (Let's Encrypt, DigiCert, etc.) already verified domain ownership before issuing the cert via an ACME challenge. This is the baseline — every HTTPS connection already verifies domain ownership. But TLS only proves ownership at connection time and does not bind the domain to the Ed25519 key.

#### Layer 2 — Registry Identity Proof (the binding layer)

During `agentdns init`, the registry creates and publishes a **Registry Identity Proof (RIP)** — a signed document that cryptographically binds the domain, the TLS certificate, and the Ed25519 key together.

```bash
agentdns init --domain dns01.zynd.ai --tls-cert /etc/letsencrypt/live/dns01.zynd.ai/fullchain.pem
```

The init process:

1. Generates the Ed25519 keypair (existing flow)
2. Reads the TLS certificate and extracts its SPKI (Subject Public Key Info) fingerprint — using SPKI rather than the full cert fingerprint so the proof survives certificate renewals as long as the key stays the same
3. Creates and signs the Registry Identity Proof
4. Publishes the proof at `https://dns01.zynd.ai/.well-known/zynd-registry.json`

**Registry Identity Proof document:**

```json
{
  "type": "registry-identity-proof",
  "version": "1.0",
  "domain": "dns01.zynd.ai",
  "registry_id": "agdns:registry:a1b2c3d4e5f6...",
  "ed25519_public_key": "gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=",
  "tls_spki_fingerprint": "sha256:b4de3a9f8c2e1d7b5a0f6c3d8e9b2a1c...",
  "issued_at": "2026-03-27T10:00:00Z",
  "expires_at": "2027-03-27T10:00:00Z",
  "signature": "ed25519:Pfix+qwQxg0ztDjnRmbk3/..."
}
```

**How a peer verifies this (at connection time):**

1. Connect to `https://dns01.zynd.ai` — TLS handshake proves domain ownership
2. During the TLS handshake, extract the server certificate's SPKI fingerprint
3. Fetch `/.well-known/zynd-registry.json` — get the Registry Identity Proof
4. Verify: does the `tls_spki_fingerprint` in the proof match the fingerprint from step 2?
5. Verify: is the Ed25519 `signature` on the proof valid against the `ed25519_public_key`?
6. If both pass: the entity controlling `dns01.zynd.ai` is confirmed to be the same entity holding the Ed25519 key

The trust chain: CA verifies domain → TLS cert proves you're talking to that domain → SPKI fingerprint in the proof binds the cert to the Ed25519 key → Ed25519 signature proves the key holder authored the proof. No link can be faked without breaking another.

**SPKI pinning vs full cert fingerprint:** TLS certificates from Let's Encrypt expire every 90 days. If the proof pinned the full certificate fingerprint, every renewal would invalidate it. SPKI pins the public key inside the certificate instead. As long as the operator keeps the same TLS private key across renewals (standard practice), the SPKI hash stays the same and the proof remains valid. If the TLS key does rotate, `agentdns` detects the mismatch and re-signs the proof automatically.

#### Layer 3 — DNS TXT Record (public verification without connecting)

For verification before connecting (e.g., a peer evaluating a gossip peering request), the registry operator publishes a DNS TXT record:

```
_zynd.dns01.zynd.ai  TXT  "v=zynd1 id=agdns:registry:a1b2c3d4e5f6 key=ed25519:gKH4VSJ838fG..."
```

Anyone can look up `_zynd.dns01.zynd.ai` via standard DNS and see the registry's Ed25519 public key. Modifying DNS records requires domain control, so this is a strong ownership signal. If the domain uses DNSSEC, the record is cryptographically tamper-proof via the DANE/TLSA chain.

**Pre-connection verification flow:**

1. Receive a gossip peering request from a node claiming to be `dns01.zynd.ai` with Ed25519 key X
2. DNS lookup `_zynd.dns01.zynd.ai` → get the published Ed25519 key
3. Does the key in the gossip request match the key in DNS? If yes, proceed to TLS connection for full verification
4. Optionally, search Certificate Transparency logs (e.g., via crt.sh) to confirm a real TLS cert exists for `dns01.zynd.ai` — publicly auditable, no connection needed

#### Layer 4 — Peer Attestation (mesh-level trust)

The three layers above prove domain ownership. But they don't prove the registry is running legitimate Zynd software or behaving honestly in the mesh. For that, Zynd adds a peer attestation mechanism:

When an existing trusted registry successfully completes a peering handshake (TLS + RIP verification), it co-signs the new registry's identity proof and gossips the attestation to the mesh:

```json
{
  "type": "peer-attestation",
  "attester_id": "agdns:registry:existing-peer...",
  "attester_domain": "dns02.zynd.ai",
  "subject_id": "agdns:registry:a1b2c3d4e5f6...",
  "subject_domain": "dns01.zynd.ai",
  "verified_layers": ["tls", "rip", "dns_txt"],
  "attested_at": "2026-03-27T10:05:00Z",
  "signature": "ed25519:..."
}
```

After N peer attestations (configurable, default 3), the registry is considered "mesh-verified." This is a web-of-trust model — no central authority, just existing peers vouching for newcomers.

**Verification tiers for registries:**

| Tier | Requirements | Trust Signal |
|---|---|---|
| **Self-announced** | Ed25519 keypair only | Lowest — no domain proof |
| **Domain-verified** | TLS + RIP at `/.well-known/zynd-registry.json` | Medium — domain ownership proved |
| **DNS-published** | Domain-verified + `_zynd.` DNS TXT record | Higher — publicly verifiable before connecting |
| **Mesh-verified** | DNS-published + N peer attestations | Highest — vouched for by existing trusted peers |

Gossip announcements include the registry's verification tier. Peers can set minimum tier requirements for accepting peering requests (e.g., "only peer with DNS-published or higher").

#### Database Schema for Registry Verification

```sql
-- Registry identity proofs (local and received via gossip)
CREATE TABLE registry_identity_proofs (
    registry_id         TEXT PRIMARY KEY,
    domain              TEXT NOT NULL UNIQUE,
    ed25519_public_key  TEXT NOT NULL,
    tls_spki_fingerprint TEXT NOT NULL,
    proof_json          JSONB NOT NULL,
    proof_signature     TEXT NOT NULL,
    verification_tier   TEXT NOT NULL DEFAULT 'self-announced',
    issued_at           TIMESTAMPTZ NOT NULL,
    expires_at          TIMESTAMPTZ NOT NULL,
    received_at         TIMESTAMPTZ NOT NULL
);

-- Peer attestations
CREATE TABLE peer_attestations (
    attester_id     TEXT NOT NULL,
    subject_id      TEXT NOT NULL REFERENCES registry_identity_proofs(registry_id),
    verified_layers TEXT[] NOT NULL,
    attested_at     TIMESTAMPTZ NOT NULL,
    signature       TEXT NOT NULL,
    PRIMARY KEY (attester_id, subject_id)
);

CREATE INDEX idx_attestations_subject ON peer_attestations(subject_id);
```

#### Gossip Extensions for Registry Verification

```go
// Type: "registry_proof"
// Action: "publish", "update", "revoke"
// Fields: RegistryID, Domain, Ed25519PublicKey, TLSSPKIFingerprint, ProofSignature, VerificationTier

// Type: "peer_attestation"
// Action: "attest", "revoke"
// Fields: AttesterID, SubjectID, SubjectDomain, VerifiedLayers, Signature
```

### 7.3 Developer Registration and Handle Claiming

Developer registration and handle claiming are two separate steps. Registration creates the cryptographic identity. Handle claiming adds the human-readable namespace.

**Step 1 — Register (creates the developer ID):**

```bash
agentdns dev-init
agentdns dev-register
# Output: Developer ID: agdns:dev:f2a1c3e8b9d7...
# No handle yet — agents can be registered by ID only
```

**Step 2 — Claim a handle (optional, can happen later):**

```bash
# Self-claimed (no verification)
agentdns dev-claim-handle --handle quickbot

# Domain-verified
agentdns dev-claim-handle --handle acme-corp --verify-dns acme-corp.com
# Registry checks: _zynd-verify.acme-corp.com TXT contains the developer's public key

# GitHub-verified
agentdns dev-claim-handle --handle johndoe --verify-github
# Opens OAuth flow, verifies GitHub username matches
```

Once claimed, the developer owns the `dns01.zynd.ai/acme-corp/*` namespace. Without a handle, the developer can still register agents, but those agents won't have FQANs — they'll only be discoverable by their `agdns:` address IDs.

### 7.4 Agent Registration with Name

Agents are registered with a name in one step:

```bash
agentdns register \
  --name doc-translator \
  --developer acme-corp \
  --url https://translator.acme-corp.com \
  --category nlp \
  --tags translation,documents \
  --version 2.1.0
```

This creates: the agent record (existing flow), the ZNS name binding (`dns01.zynd.ai/acme-corp/doc-translator`), and the version record (v2.1.0). All three are stored atomically and gossiped as a single announcement.

The FQAN `dns01.zynd.ai/acme-corp/doc-translator` is now resolvable from any registry in the mesh.

If the developer hasn't claimed a handle, the `--developer` flag is omitted and the agent is registered under the developer's address ID only (no FQAN generated).

### 7.5 Resolution

```
GET /v1/resolve/acme-corp/doc-translator

Response:
{
  "fqan": "dns01.zynd.ai/acme-corp/doc-translator",
  "agent_id": "agdns:7f3a9c2e4b1d5e8a...",
  "developer_id": "agdns:dev:f2a1c3e8...",
  "developer_handle": "acme-corp",
  "registry_host": "dns01.zynd.ai",
  "version": "2.1.0",
  "agent_url": "https://translator.acme-corp.com",
  "public_key": "ed25519:...",
  "protocols": ["a2a", "mcp"],
  "trust_score": 0.87,
  "status": "online"
}
```

Resolution chain: local ZNS table, then gossip ZNS entries, then DHT lookup using `SHA-256("dns01.zynd.ai/acme-corp/doc-translator")` as the key.

---

## 8. Database Schema

```sql
-- Developer handles (extends existing developers table)
ALTER TABLE developers ADD COLUMN dev_handle TEXT;
ALTER TABLE developers ADD COLUMN dev_handle_verified BOOLEAN DEFAULT FALSE;
ALTER TABLE developers ADD COLUMN verification_method TEXT;  -- 'dns', 'github', 'oauth', NULL
ALTER TABLE developers ADD COLUMN verification_proof TEXT;   -- domain name, github username, etc.
CREATE UNIQUE INDEX idx_developers_handle ON developers(dev_handle, home_registry);

-- ZNS name bindings
CREATE TABLE zns_names (
    fqan            TEXT PRIMARY KEY,     -- "dns01.zynd.ai/acme-corp/doc-translator"
    agent_name      TEXT NOT NULL,        -- "doc-translator"
    developer_handle TEXT NOT NULL,       -- "acme-corp"
    registry_host   TEXT NOT NULL,        -- "dns01.zynd.ai"
    agent_id        TEXT NOT NULL REFERENCES agents(agent_id),
    developer_id    TEXT NOT NULL,
    current_version TEXT,                 -- "2.1.0"
    capability_tags TEXT[],               -- ["nlp", "translation"]
    registered_at   TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    signature       TEXT NOT NULL
);

CREATE INDEX idx_zns_agent_id ON zns_names(agent_id);
CREATE INDEX idx_zns_developer ON zns_names(developer_handle, registry_host);
CREATE INDEX idx_zns_capability ON zns_names USING GIN(capability_tags);

-- Version history
CREATE TABLE zns_versions (
    fqan            TEXT NOT NULL REFERENCES zns_names(fqan),
    version         TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    build_hash      TEXT,
    registered_at   TIMESTAMPTZ NOT NULL,
    signature       TEXT NOT NULL,
    PRIMARY KEY (fqan, version)
);

-- Gossip tables for remote names
CREATE TABLE gossip_zns_names (
    fqan            TEXT PRIMARY KEY,
    agent_name      TEXT NOT NULL,
    developer_handle TEXT NOT NULL,
    registry_host   TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    current_version TEXT,
    capability_tags TEXT[],
    received_at     TIMESTAMPTZ NOT NULL,
    tombstoned      BOOLEAN DEFAULT FALSE
);
```

---

## 9. API Endpoints

```
-- Developer handles
POST   /v1/handles                                     -- Claim a handle
GET    /v1/handles/{handle}                             -- Look up a handle
DELETE /v1/handles/{handle}                             -- Release a handle
POST   /v1/handles/{handle}/verify                     -- Submit verification proof

-- Agent name bindings
POST   /v1/names                                       -- Register a name for an agent
GET    /v1/names/{developer}/{agent}                    -- Resolve by local FQAN
PUT    /v1/names/{developer}/{agent}                    -- Update binding
DELETE /v1/names/{developer}/{agent}                    -- Release name

GET    /v1/names/{developer}/{agent}/versions           -- List all versions
GET    /v1/names/{developer}/{agent}@{version}          -- Resolve specific version

-- Resolution (includes trust/status, used by other registries)
GET    /v1/resolve/{developer}/{agent}                  -- Full resolution
GET    /v1/resolve/{developer}/{agent}?protocol=mcp     -- Protocol-filtered

-- Listing
GET    /v1/handles/{handle}/agents                     -- All agents under a developer
GET    /v1/registry/names                              -- All names on this registry

POST   /v1/agents (extended)                           -- Now accepts optional "agent_name" field
```

---

## 10. Gossip Extensions

Two new announcement types:

```go
// Type: "dev_handle"
// Action: "claim", "verify", "release"
// Fields: Handle, DeveloperID, RegistryHost, VerificationMethod, VerificationProof, Signature

// Type: "name_binding"
// Action: "register", "update", "version", "release"
// Fields: FQAN, AgentName, DevHandle, RegistryHost, AgentID, Version, Signature
```

Both follow the existing gossip patterns: hop-counted, deduped, origin-pinned, tombstone-propagated.

---

## 11. Naming Examples Across Scenarios

**Enterprise agent on the main Zynd registry:**
```
dns01.zynd.ai/fintech-corp/invoice-processor
dns01.zynd.ai/fintech-corp/invoice-processor@3.2.1
```

**Open-source agent on a community registry:**
```
community.agentmesh.io/opensource-dev/code-reviewer
```

**Self-claimed unverified developer:**
```
local-registry.example.com/quickbot/my-scraper
```

**Agent registered on a private enterprise registry:**
```
agents.bigcorp.internal/legal-team/compliance-checker
```

**Referencing in an Agent Card:**
```json
{
  "name": "doc-translator",
  "description": "Translates documents between 40 languages",
  "url": "https://translator.acme-corp.com",
  "zynd_fqan": "dns01.zynd.ai/acme-corp/doc-translator",
  "zynd_id": "agdns:7f3a9c2e4b1d5e8a..."
}
```

**Referencing in a delegation contract:**
```json
{
  "provider": "agdns://dns01.zynd.ai/acme-corp/doc-translator@2.1.0",
  "orchestrator": "agdns://dns01.zynd.ai/bigcorp/workflow-engine",
  "scope": "translate uploaded PDF from English to French"
}
```

**Referencing in a ZTP (Zero-Trust Proof):**
```json
{
  "provider_fqan": "dns01.zynd.ai/acme-corp/doc-translator",
  "orchestrator_fqan": "dns01.zynd.ai/bigcorp/workflow-engine",
  "outcome": "success",
  "domain": "nlp.translation"
}
```

---

## 12. Agent Card Format: What Zynd Should Use

### 12.1 The Problem with Zynd's Current Format

Zynd currently uses a custom JSON format at `/.well-known/agent.json`. Here's what a live Zynd Agent Card looks like today:

```json
{
  "agent_id": "agdns:92bab5e6833826b36e2e1b272695414d",
  "public_key": "ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=",
  "name": "swapnil-tes",
  "description": "swapnil-tes — a LangChain agent on the ZyndAI network.",
  "version": "1.0",
  "category": "general",
  "tags": ["langchain"],
  "capabilities": [
    {"name": "nlp", "category": "ai"},
    {"name": "langchain", "category": "ai"},
    {"name": "http", "category": "protocols"}
  ],
  "agent_url": "http://localhost:5001",
  "endpoints": {
    "invoke": "http://localhost:5001/webhook/sync",
    "invoke_async": "http://localhost:5001/webhook",
    "health": "http://localhost:5001/health",
    "agent_card": "http://localhost:5001/.well-known/agent.json"
  },
  "status": "online",
  "last_heartbeat": "2026-03-26T11:37:00Z",
  "signed_at": "2026-03-26T11:37:00Z",
  "signature": "ed25519:Pfix+qwQxg0ztDjnRmbk3/..."
}
```

This format is Zynd-specific. No other system can parse it. An A2A client won't recognize `endpoints.invoke`. An MCP client won't find tool definitions. The card mixes identity concerns (agent_id, public_key, signature) with runtime concerns (status, last_heartbeat) and discovery concerns (capabilities, endpoints) in a flat structure with no clear boundaries.

### 12.2 A2A Protocol: Not HTTP-Only

A common misconception is that A2A only works over HTTP. In reality, A2A version 0.3 (July 2025) supports three transport bindings: JSON-RPC 2.0 over HTTP, gRPC (Protocol Buffers), and HTTP+JSON/REST. It also supports Server-Sent Events for streaming and webhooks for push notifications.

MCP (Model Context Protocol) supports two transports: stdio (for local processes) and Streamable HTTP (which replaced the older SSE transport in March 2025).

ACP, ANP, and other emerging protocols each have their own transport and card formats. This is why Zynd being protocol-agnostic matters — the Agent Card needs to work regardless of which protocol the agent speaks.

### 12.3 Recommended Format: Zynd-Native with Protocol Extensions

Since Zynd is protocol-agnostic, the Agent Card should NOT blindly adopt A2A's format. Instead, Zynd should define its own card format that includes a `protocols` section where each supported protocol declares its own metadata in that protocol's native format.

The card should live at `/.well-known/zynd-agent.json` (distinct from A2A's `/.well-known/agent-card.json` to avoid collisions when an agent supports both).

**Recommended Zynd Agent Card structure:**

```json
{
  "zynd": {
    "version": "1.0",
    "fqan": "dns01.zynd.ai/acme-corp/doc-translator",
    "agent_id": "agdns:92bab5e6833826b36e2e1b272695414d",
    "developer_id": "agdns:dev:f2a1c3e8b9d7...",
    "developer_handle": "acme-corp",
    "public_key": "ed25519:gKH4VSJ838fG1jg6Y14EwwAkQ5PbXsCsu7ckS3SeGRw=",
    "home_registry": "dns01.zynd.ai",
    "signed_at": "2026-03-26T11:37:00Z",
    "signature": "ed25519:Pfix+qwQxg0ztDjnRmbk3/..."
  },

  "agent": {
    "name": "doc-translator",
    "description": "Translates documents between 40 languages with high accuracy.",
    "version": "2.1.0",
    "category": "nlp",
    "tags": ["translation", "documents", "multilingual"],
    "capabilities": [
      {"name": "document-translation", "category": "nlp"},
      {"name": "language-detection", "category": "nlp"}
    ]
  },

  "endpoints": {
    "base_url": "https://translator.acme-corp.com",
    "health": "/health"
  },

  "protocols": {
    "a2a": {
      "version": "0.3",
      "card_url": "https://translator.acme-corp.com/.well-known/agent-card.json",
      "skills": [
        {
          "id": "translate-doc",
          "name": "Document Translation",
          "description": "Translate a document from one language to another",
          "inputModes": ["application/pdf", "text/plain"],
          "outputModes": ["application/pdf", "text/plain"]
        }
      ]
    },
    "mcp": {
      "version": "2025-11-25",
      "transport": "streamable-http",
      "endpoint": "https://translator.acme-corp.com/mcp",
      "tools": [
        {
          "name": "translate",
          "description": "Translate text between languages",
          "inputSchema": {
            "type": "object",
            "properties": {
              "text": {"type": "string"},
              "source_lang": {"type": "string"},
              "target_lang": {"type": "string"}
            }
          }
        }
      ]
    },
    "rest": {
      "openapi_url": "https://translator.acme-corp.com/openapi.json",
      "invoke": "/api/v1/translate",
      "invoke_async": "/api/v1/translate/async"
    }
  },

  "trust": {
    "trust_score": 0.87,
    "verification_tier": "domain-verified",
    "ztp_count": 1423
  }
}
```

### 12.4 Why This Structure

**The `zynd` section** contains everything specific to Zynd's identity and registry system: the FQAN, cryptographic IDs, keys, and signatures. This is what other Zynd nodes care about.

**The `agent` section** is protocol-neutral metadata that any system can read: name, description, version, capabilities. This is the "universal" part of the card.

**The `endpoints` section** provides the base URL and standard health check. No protocol-specific invocation details here.

**The `protocols` section** is the key innovation. Each protocol the agent supports gets its own subsection with that protocol's native metadata. An A2A client reads `protocols.a2a`. An MCP client reads `protocols.mcp`. A plain REST client reads `protocols.rest`. The agent doesn't need to choose one protocol — it declares all the ones it supports, and the caller picks.

If an agent only speaks one protocol, the `protocols` section has one entry. If it speaks three, it has three. Zynd's registry indexes the protocol keys for filtered discovery (`?protocol=mcp`).

**The `trust` section** surfaces Zynd-specific trust data: the EigenTrust score, verification tier, and ZTP interaction count. This helps callers make trust decisions before invoking.

### 12.5 Backward Compatibility

The current Zynd Agent Card format at `/.well-known/agent.json` should continue to work during migration. The new format lives at `/.well-known/zynd-agent.json`. Agents can serve both. The registry should prefer the new format but fall back to the old one.

For agents that already have an A2A card at `/.well-known/agent-card.json`, the Zynd card's `protocols.a2a.card_url` can simply point to it rather than duplicating the A2A skills inline.

---

## 13. Migration Path

The naming system is purely additive. Existing `agdns:<hash>` identifiers remain the canonical ground truth at every stage.

**Phase 0 (current state):** Cryptographic IDs only. Names are decorative. No domain verification for registries.

**Phase 1 (registry verification):** Registries configure `https_endpoint`, generate Registry Identity Proofs, and publish `/.well-known/zynd-registry.json`. DNS TXT records at `_zynd.{domain}` enable pre-connection verification. Peer attestation begins building mesh trust.

**Phase 2 (naming launch):** Developers claim handles (self-claimed, domain-verified, or GitHub-verified). Agents get FQANs via the `registry/developer/agent` format. Both old ID-based and new FQAN-based resolution paths work side by side.

**Phase 3 (adoption):** Search results show FQANs alongside agent_ids. The CLI defaults to showing human-readable names. Agent Cards migrate to the new `/.well-known/zynd-agent.json` format with protocol-specific sections.

**Phase 4 (naming as primary UX):** New registrations prompt for a name. Delegation contracts and ZTPs reference agents by FQAN. The `agdns://` URI becomes the standard way to reference agents in documentation.

**Phase 5 (external interop):** The FQAN can be embedded in A2A Agent Cards, MCP tool manifests, and ACP profiles. A DNS bridge (optional `_zynd` TXT records) enables enterprise discovery without running a Zynd node.

---

## 14. Open Questions

**Developer handles are registry-scoped.** Two registries can each have an `acme-corp` handle belonging to different developers. This is by design — it avoids the need for global coordination. If cross-registry identity matters, the `agdns:dev:` address ID is the canonical link. A developer who wants to prove they're the same entity on multiple registries can use the same keypair.

**Reserved handles are enforced at the application level.** The following handles are blocked on all registries: `zynd`, `system`, `admin`, `test`, `root`, `registry`, `anonymous`, `unknown`. Brand protection relies on domain verification — if you own `nike.com`, you can claim `nike` with a verified badge; no one else can domain-verify that handle.

**How should the DNS bridge work exactly?** One option: registries publish `_zynd.dns01.zynd.ai TXT "registry_id=agdns:registry:a1b2c3..."` and agents publish `_zynd.translator.acme-corp.com TXT "fqan=dns01.zynd.ai/acme-corp/doc-translator"`. This lets standard DNS tools discover Zynd agents.

**What happens when a registry changes its domain?** Since the registry host is part of every FQAN, changing from `dns01.zynd.ai` to `dns01.zynd.network` invalidates all names. This should trigger a migration event that gossips alias records, similar to HTTP 301 redirects. Old FQANs resolve with a `moved_to` field for a grace period.

**Should handles cost money?** GoDaddy uses domain ownership as a natural cost barrier. ENS charges ETH. Zynd could charge a small x402 micropayment for handle claims to prevent speculative squatting, or rely purely on verification tiers as the friction mechanism. Unverified handles are free but limited (one per developer key per registry).

---

## 15. Sources

- [GoDaddy ANS API and Standards Launch](https://aboutus.godaddy.net/newsroom/news-releases/press-release-details/2025/GoDaddy-advances-trusted-AI-agent-identity-with-ANS-API-and-Standards-site/default.aspx)
- [GoDaddy: Building ANS with a One System Approach](https://www.godaddy.com/resources/news/building-the-agent-name-service-using-a-one-system-approach)
- [GoDaddy ANS Registry (GitHub)](https://github.com/godaddy/ans-registry)
- [IETF draft-narajala-ans-00: Agent Name Service](https://datatracker.ietf.org/doc/draft-narajala-ans/)
- [OWASP ANS v1.0 Specification](https://genai.owasp.org/resource/agent-name-service-ans-for-secure-al-agent-discovery-v1-0/)
- [IETF draft-liang-agentdns-00: AgentDNS Root Domain](https://datatracker.ietf.org/doc/draft-liang-agentdns/00/)
- [IETF BANDAID: Brokered Agent Network for DNS AI Discovery](https://www.ietf.org/archive/id/draft-mozleywilliams-dnsop-bandaid-00.html)
- [ENS + ERC-8004: AI Agent Identity](https://ens.domains/blog/post/ens-ai-agent-erc8004)
- [NANDA Index and Verified AgentFacts](https://arxiv.org/abs/2507.14263)
- [AGNTCY Agent Directory Service](https://arxiv.org/abs/2509.18787)
- [Evolution of AI Agent Registry Solutions (Survey)](https://arxiv.org/abs/2508.03095)
- [InfoQ: Introducing ANS](https://www.infoq.com/news/2025/06/secure-agent-discovery-ans/)
- [Analysis of ANS Frameworks (Substack)](https://kenhuangus.substack.com/p/analysis-of-agent-name-service-frameworks)
- [A2A Protocol Specification (latest)](https://a2a-protocol.org/latest/specification/)
- [A2A Protocol v0.3 Upgrade Announcement](https://cloud.google.com/blog/products/ai-machine-learning/agent2agent-protocol-is-getting-an-upgrade)
- [MCP Transports Specification (2025-11-25)](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)
- [Certificate Transparency — How CT Works](https://certificate.transparency.dev/howctworks/)
- [DANE / DNS-based Authentication of Named Entities](https://en.wikipedia.org/wiki/DNS-based_Authentication_of_Named_Entities)
- [RFC 8555 — ACME Protocol (Let's Encrypt)](https://datatracker.ietf.org/doc/html/rfc8555/)
- [RFC 7469 — HTTP Public Key Pinning (SPKI)](https://datatracker.ietf.org/doc/html/rfc7469)