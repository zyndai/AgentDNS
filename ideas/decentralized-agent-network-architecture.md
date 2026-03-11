# Decentralized Open Agent Network — Architecture Overview

## Vision

A decentralized open network where AI agents can register identity, discover each other via natural language, prove they're alive and running legitimate code, communicate through standardized protocols, build verifiable trust through co-signed interaction records, and exchange value through cryptographic payment proofs — all without any central authority, single point of failure, or gatekeeping entity.

Anyone can run a registry node. Anyone can register an agent. Trust is earned through behavior, not granted by a platform.

---

## Layer Stack

```
┌─────────────────────────────────────────────────────────────────┐
│  Layer 8: Cold Start & Graduated Trust                          │
│  How new agents safely enter the network                        │
├─────────────────────────────────────────────────────────────────┤
│  Layer 7: Payments & Settlement                                 │
│  How agents pay each other for services                         │
├─────────────────────────────────────────────────────────────────┤
│  Layer 6: Trust Scoring & Reputation                            │
│  How trust scores are computed from behavioral evidence          │
├─────────────────────────────────────────────────────────────────┤
│  Layer 5: Zero-Trust Proofs (ZTPs)                              │
│  Co-signed records of completed agent interactions              │
├─────────────────────────────────────────────────────────────────┤
│  Layer 4: Delegation & Task Contracts                           │
│  How agents agree on scope, price, and terms before work        │
├─────────────────────────────────────────────────────────────────┤
│  Layer 3: Agent-to-Agent Communication                          │
│  Standardized message format and invocation protocol            │
├─────────────────────────────────────────────────────────────────┤
│  Layer 2: Developer Identity & Verification                     │
│  Who built this agent and how verified are they                 │
├─────────────────────────────────────────────────────────────────┤
│  Layer 1: Liveness & Code Integrity                             │
│  Is the agent alive and running what it claims                  │
├─────────────────────────────────────────────────────────────────┤
│  Layer 0: Identity, Registry & Discovery  [DONE]                │
│  Ed25519 identity, registry, gossip, federated search           │
└─────────────────────────────────────────────────────────────────┘
```

Each layer depends on the ones below. You can't have ZTPs without delegation contracts, can't compute trust without ZTPs, can't determine escrow requirements without trust scores, can't do cold start without all of the above.

---

## Centralized vs Decentralized — The Core Tension

Every component in this architecture faces the same fundamental question: in a centralized system (like Zynd), a single authority issues credentials, computes trust, runs escrow, and resolves disputes. In our decentralized mesh, there is no such authority. Each component must be designed to work when:

- No single entity is trusted by everyone
- Registries may disagree on trust scores
- Agents may interact across registries that have never peered
- Bad actors can run their own registries
- The network must function during partitions

This document maps out every component and the approaches for solving each in a decentralized context.

---

## Layer 0: Identity, Registry & Discovery [DONE]

**Status:** Implemented. ~8,500 lines of Go.

### What's Built

| Component | Status | Description |
|---|---|---|
| Ed25519 Identity | Done | Agent and registry keypairs, deterministic IDs (`agdns:<sha256>`), signing and verification |
| Registry Store | Done | PostgreSQL-backed storage for agent records, gossip entries, tombstones, attestations |
| Gossip Protocol | Done | Hop-counted announcements with dedup windows, tombstone propagation |
| Peer Manager | Done | Mesh connections, heartbeats (peer-to-peer), bootstrap with backoff, peer eviction |
| Federated Search | Done | BM25 keyword + hash-embedding semantic search, bloom filter routing, multi-signal ranking |
| Agent Card Fetcher | Done | Two-tier cache (LRU + Redis), signature verification, 50KB body limit |
| REST API | Done | Full HTTP API with Swagger docs, CORS, rate limiter (defined but not wired) |
| CLI | Done | init, start, register, search, resolve, card, status, peers, deregister, version |
| Docker Compose | Done | 3-node testbed with PostgreSQL and Redis |

### Known Gaps in Layer 0 (to fix before moving up)

| Gap | Description | Priority |
|---|---|---|
| Rate limiter not wired | `searchRL` and `registerRL` created in server.go but never applied to routes | High |
| Update/delete auth missing | `handleUpdateAgent` and `handleDeleteAgent` don't verify signatures | Critical |
| Gossip signature not verified | `HandleAnnouncement()` checks hop count and dedup but not signature | Critical |
| Mesh transport is plaintext | TCP connections between peers have no encryption | Critical |
| Tombstone GC not running | `CleanExpiredTombstones()` exists but no goroutine calls it | High |
| Search pagination missing | No cursor/offset for large result sets | Medium |
| Real embeddings missing | Uses FNV hash embedder, not a real neural model | Medium |
| Agent Card schema version | No `schema_version` field for forward compatibility | Medium |
| EigenTrust not wired to search | Trust score hardcoded to 0.5/0.3 in search results | High |

---

## Layer 1: Liveness & Code Integrity

### Component 1.1: Agent Heartbeat & Liveness

**Problem:** Dead agents stay in search results forever. No mechanism to detect or exclude offline agents. The `Availability` ranking signal is hardcoded, not measured.

**Approaches:**
1. **Agent-initiated WebSocket heartbeat** — Agent opens persistent WS to home registry, sends signed timestamp every 30s. Registry passively verifies. 5-min silence marks inactive.
   - Pro: Low overhead per heartbeat (~120 bytes), instant disconnect detection, works behind NAT
   - Con: Persistent connection per agent (memory), reconnect logic needed
2. **Agent-initiated HTTP POST heartbeat** — Agent POSTs signed timestamp to `/v1/agents/{id}/heartbeat` every 30-60s
   - Pro: Stateless, works through any proxy/CDN, simpler server implementation
   - Con: TCP/TLS overhead per request, ~3,300 req/s at 100K agents with 30s interval
3. **Registry-initiated health probe** — Registry periodically pings agent's health endpoint
   - Pro: No agent-side SDK needed
   - Con: Doesn't work behind NAT/firewalls, registry must know how to probe arbitrary agents

**Decision:** WebSocket (chosen)

**Key Design Points:**
- Heartbeat message: `{"timestamp": "RFC3339", "signature": "ed25519:<base64>"}` (~120 bytes)
- Signature over UTF-8 bytes of the timestamp string
- Registry verifies: signature valid + `|timestamp - now| < 60s` (clock skew tolerance, prevents replay)
- 5-minute timeout: no heartbeat = status `inactive`, excluded from search
- Status changes gossiped across mesh (`agent_status` announcement type)
- Agent is NOT deleted when inactive — comes back live on next heartbeat

**Dependencies:** Layer 0 (agent identity, public key storage)

**Zynd's approach:** 30-second WebSocket heartbeat with component hash verification (drift detection). If hashes don't match registered manifest, agent hidden immediately.

**Decentralization challenge:** Heartbeats only go to the home registry. Other registries learn status via gossip (10-30s delay). During a network partition, an agent might appear active on some registries and inactive on others.

**Detailed spec:** `ideas/agent-heartbeat-liveness.md`

---

### Component 1.2: Build Integrity & Code Attestation

**Problem:** No way to verify an agent runs the code it claims. The Agent Card contains self-reported fields (capabilities, framework, model) that are entirely trust-on-first-use. A malicious agent could claim anything.

**Approaches:**
1. **Self-attestation** — Agent developer signs the build hash with their Ed25519 key. Proves "the key holder claims this is the build." Lowest bar, easy to implement.
2. **Third-party build attestation (SLSA/Sigstore)** — A trusted CI system (GitHub Actions, GitLab CI) signs a provenance attestation proving "this binary was built from this source commit by this workflow." Stronger guarantee, requires CI integration.
3. **Runtime attestation via TEE** — Run agent inside Intel SGX / AMD SEV / AWS Nitro Enclave. Hardware produces attestation that this exact binary is running in a secure enclave. Strongest guarantee, heavy infrastructure.
4. **Behavioral probes** — Agents publish test suites in their card. Registries periodically run tests to verify the agent behaves as claimed. Verifies behavior, not code.

**Decision:** Layer 1 (self-attestation) + Layer 2 (SLSA/Sigstore) — chosen. TEE and behavioral probes are future layers.

**Key Design Points:**
- `integrity` section added to Agent Card with: source_repo, source_commit, build_hash, attestations[], self_attestation
- Trust levels: unverified → self-attested → open source self-attested → build verified → open source build verified
- Registry verifies self_attestation signature against agent's public key on card fetch
- SLSA attestation verification requires knowing trusted issuer keys (GitHub Actions, etc.)

**Dependencies:** Layer 0 (agent identity, card fetcher)

**Zynd's approach:** Build manifests with model_hash, prompt_hash, code_hash, tool_versions. Developer signs the manifest. Drift detection via heartbeat — if runtime hashes don't match the registered manifest, agent is hidden immediately. Card NEVER updates to match drift.

**Decentralization challenge:** In Zynd, the central registry enforces "card never updates to match drift." In our system, the agent hosts its own card — we can't prevent it from updating. We can only detect mismatches between the registered build_hash (in the registry record) and the card's self-reported integrity. Gossip can propagate integrity warnings.

**Detailed spec:** `ideas/build-integrity-attestation.md`

---

### Component 1.3: Drift Detection

**Problem:** Even with build attestation, an agent could update its code without updating the registered build hash. The running binary silently drifts from what was attested.

**Approaches:**
1. **Heartbeat-embedded hashes (Zynd approach)** — Every heartbeat includes `component_hashes` (re-computed at runtime). Registry compares against registered manifest. Drift = agent hidden immediately.
   - Pro: Catches drift within 30 seconds
   - Con: Requires agent SDK to compute hashes at runtime, adds complexity to heartbeat
2. **Periodic card re-verification** — Registry periodically re-fetches the Agent Card and compares `integrity.build_hash` against the last known value. If changed without a corresponding registry record update, flag it.
   - Pro: Simpler, no changes to heartbeat protocol
   - Con: Only catches drift when the card is re-fetched (TTL-dependent)
3. **Registry record anchoring** — Store the `build_hash` in the registry record itself (not just the card). The card's build_hash must match. Mismatch = integrity warning on search results.
   - Pro: Registry controls the anchor, agent can't silently change it
   - Con: Requires a registry record update every time the agent rebuilds

**Decision:** To be discussed. Depends on how much complexity we want in the heartbeat vs how fast we need to detect drift.

**Dependencies:** Component 1.1 (heartbeat), Component 1.2 (build attestation)

**Zynd's approach:** Heartbeat-embedded hashes with immediate hiding on mismatch. Card never updates to match drift — only explicit `zynd update --agent <name>` re-derives the card.

---

## Layer 2: Developer Identity & Verification

### Component 2.1: Developer Identity (DID)

**Problem:** Agents are identified by their Ed25519 keys, but there's no concept of who *built* the agent. Two agents from the same developer have no visible relationship. There's no way to know if a developer is a verified entity or a throwaway account.

**Approaches:**
1. **Self-sovereign proofs** — Developers prove identity via cryptographic challenges: sign a GitHub gist (proves GitHub ownership), add DNS TXT record (proves domain ownership), sign with an existing PGP key, etc. No central verifier needed. Registries independently verify the proofs.
   - Pro: Fully decentralized, no gatekeeper
   - Con: UX is rougher, each proof type needs a verifier
2. **Registry-issued tiers** — Each registry has its own developer verification process and issues tiered credentials. Trust in the developer depends on which registry verified them.
   - Pro: Simple for developers (one place to verify), registries compete on verification quality
   - Con: Centralized within each registry, trust transfer between registries is complex
3. **Web-of-trust model** — Existing verified developers vouch for new ones. N attestations from trusted developers = verified.
   - Pro: Fully decentralized, community-driven
   - Con: Slow for newcomers, can form cliques

**Decision:** To be discussed.

**Key Design Points:**
- Developer DID format: `did:agdns:<pubkey-hash>` or similar
- Developer keypair is separate from agent keypair (key hierarchy)
- Developer signs agent public keys at registration (chain of trust: developer → agent)
- Multiple agents can share the same developer DID

**Dependencies:** Layer 0 (identity primitives)

**Zynd's approach:** Centralized. Zynd issues developer DIDs via email/GitHub OAuth. Four DIA tiers: email verified (tier 1, trust capped at 0.60), GitHub linked (tier 2, uncapped), org DNS verified (tier 3), legal entity (tier 4). Tier is a hard ceiling on trust scores.

**Decentralization challenge:** Without a central authority, who decides what "tier 2" means? Each registry might have different standards. A developer verified by a reputable registry carries more weight than one verified by an unknown registry. This creates a meta-trust problem (trust in registries, not just agents).

---

### Component 2.2: Key Hierarchy & Separation

**Problem:** If an agent's runtime key is compromised, what's the blast radius? Currently, agent key = identity key = signing key for everything. One compromised key = full agent takeover.

**Approaches:**
1. **Two-level key hierarchy (Zynd approach)** — Developer master key (signs agent public keys, manifests, never on agent machine) + Agent runtime key (signs heartbeats, ZTPs, no management permissions)
   - Pro: Compromised agent can't register new agents or access other agents
   - Con: More complex key management for developers
2. **Single key with capability scoping** — One key per agent but with capability-scoped permissions defined at registration time (this key can sign heartbeats and ZTPs but not re-register)
   - Pro: Simpler key management
   - Con: If the key is compromised, attacker can do everything the key was scoped for
3. **Three-level hierarchy** — Developer key → Agent management key (rotate runtime keys) → Agent runtime key
   - Pro: Maximum isolation
   - Con: Complexity, most developers won't bother

**Decision:** To be discussed.

**Dependencies:** Component 2.1 (developer identity)

**Zynd's approach:** Developer master key (`~/.zynd/developer.key`) signs agent public keys and build manifests, never on running agents. Agent runtime key (`~/.zynd/agents/<id>.key`) signs heartbeats and ZTPs. Compromised agent machine cannot access other agents or re-register.

---

### Component 2.3: Developer Verification Tiers

**Problem:** Not all developers are equally trustworthy. An anonymous email signup is different from a verified GitHub org with years of history. Trust ceilings prevent unverified developers from gaming the system.

**Approaches:**
1. **Cryptographic proof tiers** — Each tier is a verifiable claim:
   - Tier 1: Ed25519 keypair exists (baseline, anyone)
   - Tier 2: GitHub ownership proven (sign a gist with developer key)
   - Tier 3: Domain ownership proven (DNS TXT record with developer pubkey)
   - Tier 4: Legal entity (harder to verify decentrally — may need registry attestation)
   - Pro: Fully verifiable, no trust in any central entity
   - Con: Tier 4 is hard without a central verifier
2. **Registry-attested tiers** — Registries verify developers and issue signed tier attestations. Other registries decide how much to trust those attestations.
   - Pro: Simpler UX, registries can do KYC for tier 4
   - Con: Creates trust dependency on registries
3. **Hybrid** — Tiers 1-3 via self-sovereign proofs, Tier 4 via registry attestation
   - Pro: Best of both
   - Con: Two different verification mechanisms

**Decision:** To be discussed.

**Key Design Points:**
- Should tiers impose hard trust ceilings (Zynd: tier 1 capped at 0.60)?
- How are tier proofs stored? In the registry record? In the Agent Card?
- Can a developer upgrade tiers without re-registering agents?

**Dependencies:** Component 2.1 (developer identity)

**Zynd's approach:** DIA tiers with hard ceilings. Tier 1 (email) = max trust 0.60. Tier 2+ = uncapped. Centrally verified by Zynd.

---

## Layer 3: Agent-to-Agent Communication

### Component 3.1: Message Format Standard

**Problem:** When Agent A wants to invoke Agent B, what does the message look like? Currently there's no standard — the Agent Card lists protocols (a2a, mcp, jsonrpc) but no unified message envelope.

**Approaches:**
1. **Unified envelope with protocol-specific payloads** — A standard outer envelope (from, to, intent, timestamp, signature) wrapping protocol-specific inner payloads (A2A task, MCP tool call, JSON-RPC method)
   - Pro: Common routing/auth/logging regardless of inner protocol
   - Con: Extra wrapping layer, may conflict with existing protocol specs
2. **Protocol-native messages** — Don't define our own format. Let agents speak native A2A, MCP, or JSON-RPC. The Agent Card declares which protocols are supported.
   - Pro: Zero friction with existing ecosystems
   - Con: No unified auth/signing/payment layer
3. **Zynd-style message format** — Custom format with required fields (protocol_version, message_id, timestamp, from DID, to DID, intent, payload) + optional payment proof
   - Pro: Clean, purpose-built for agent economy
   - Con: Yet another standard, adoption friction

**Decision:** To be discussed.

**Key Design Points:**
- Should messages be signed by the sender? (Zynd: yes)
- Should messages include payment proofs inline? (Zynd: yes)
- TTL on messages? Idempotency keys?
- Reply-to mechanism for async tasks?

**Dependencies:** Layer 0 (identity for signing)

**Zynd's approach:** Custom JSON format with required fields: protocol_version, message_id, timestamp, from (DID), to (DID), intent, payload. Optional: payment proof, metadata (correlation_id, reply_to).

---

### Component 3.2: Endpoint Revelation & Trust-Gated Access

**Problem:** An agent's invoke endpoint is sensitive. You don't want arbitrary callers hitting it. Currently the endpoint is in the Agent Card, which is publicly fetchable.

**Approaches:**
1. **Trust-gated revelation** — Agent Card only reveals the invoke endpoint to callers above a trust threshold. Below threshold, the endpoint field is omitted or encrypted.
   - Pro: Protects agents from untrusted callers
   - Con: Requires the card fetcher to be trust-aware, complicates caching
2. **Always public, auth required** — Endpoint is always visible, but invoking requires authentication (API key, signed challenge, payment proof). The agent rejects unauthorized requests.
   - Pro: Simpler discovery, auth is at the agent level
   - Con: Endpoint exposed to DDoS, scanning, etc.
3. **Relay via registry** — Callers send requests through the registry, which proxies to the agent. Agent endpoint is never public.
   - Pro: Full protection, registry can add rate limiting
   - Con: Registry becomes a bottleneck, adds latency, violates "registry is not in the data path"

**Decision:** To be discussed.

**Dependencies:** Layer 6 (trust scores for gating)

**Zynd's approach:** Default threshold `min_trust = 0.30`. Agent owner can set higher. Below threshold, endpoint stays hidden. Three access levels: public (ZID, capabilities, trust), authenticated (ZTP count, availability), verified search (endpoint revealed).

---

### Component 3.3: NAT Traversal & Local Agents

**Problem:** Not all agents run on cloud servers with public IPs. Local agents (running on developer machines, Raspberry Pis, behind corporate firewalls) can't accept incoming connections.

**Approaches:**
1. **WebSocket gateway** — Local agents maintain a persistent WebSocket to a gateway service. Incoming requests are relayed through the WS connection.
   - Pro: Works behind any NAT, agent initiates the connection
   - Con: Requires gateway infrastructure, adds latency
2. **Registry as relay** — The home registry relays requests to locally-connected agents via their heartbeat WebSocket
   - Pro: Reuses existing heartbeat connection, no additional infrastructure
   - Con: Registry becomes a data path bottleneck, wasn't designed for this
3. **Peer-to-peer with hole punching** — Use STUN/TURN/ICE for direct P2P connections between agents
   - Pro: Direct connection, lowest latency
   - Con: Complex, doesn't always work (symmetric NAT), requires TURN fallback

**Decision:** To be discussed.

**Dependencies:** Component 1.1 (heartbeat WebSocket could double as relay)

**Zynd's approach:** Two types of agents — local (WebSocket to gateway) and cloud (public endpoint). Local agents connect via `wss://gateway.zynd.network/agent`. Centralized gateway.

**Decentralization challenge:** Who runs the gateway? Each registry could act as a gateway for its locally-registered agents. Or a separate gateway node type.

---

## Layer 4: Delegation & Task Contracts

### Component 4.1: Delegation Contract Format

**Problem:** Before Agent A asks Agent B to do work, they need to agree on: what work, what price, what deadline, what permissions, and what constitutes success. Without a pre-agreed contract, disputes are unresolvable.

**Approaches:**
1. **Co-signed JSON contract** — Both agents sign a JSON document specifying scope, permissions, value, expiry, and deliverable criteria before any work begins
   - Pro: Clear, auditable, foundation for ZTPs and disputes
   - Con: Extra round-trip before every interaction, overhead for microtasks
2. **Standing agreements** — Agents pre-negotiate terms that cover multiple future interactions (e.g., "I'll pay you $0.01 per call for the next 30 days at up to 100 calls/day")
   - Pro: No per-request overhead for repeated interactions
   - Con: More complex, needs revocation mechanism
3. **Implicit from Agent Card** — The Agent Card declares pricing and terms. Calling the agent = accepting the terms.
   - Pro: Simplest, no extra round-trip
   - Con: No evidence of agreement for disputes, terms can change between discovery and invocation

**Decision:** To be discussed.

**Key Design Points:**
- Contract fields: scope, permissions, value, currency, expiry, deliverable_hash_algorithm, dispute_window
- Who stores the contract? Both agents? The home registry? On-chain?
- Contract ID format and referencing in subsequent messages

**Dependencies:** Layer 3 (communication protocol for negotiation)

**Zynd's approach:** Delegation contracts co-signed by both agents. Fields: provider_did, orchestrator_did, scope, permissions (JSONB), value, expiry, both signatures, status. Stored in Zynd's database.

**Decentralization challenge:** In Zynd, the central registry stores delegation contracts. In our system, both agents store their copy. If they disagree on whether a contract exists or what it says, the co-signatures are the proof.

---

### Component 4.2: Permissions & Scope Model

**Problem:** When Agent A delegates work to Agent B, what can B do? Can B call other agents? Can B access A's data? Can B spend A's money?

**Approaches:**
1. **Explicit permission list** — Contract specifies exactly what the provider can do: `["read_data", "call_sub_agents", "max_sub_cost: $0.50"]`
   - Pro: Least privilege, clear boundaries
   - Con: Hard to enumerate all possible permissions
2. **Capability-based** — Provider receives a token/capability that grants specific access, limited by scope and time
   - Pro: Composable, revocable, well-understood pattern (UCAN, ZCAP-LD)
   - Con: More complex to implement
3. **Trust-based implicit** — Higher trust = more implicit permissions. Low trust agents get sandboxed, high trust agents get broader access.
   - Pro: Less ceremony per interaction
   - Con: Vague boundaries, harder to audit

**Decision:** To be discussed.

**Dependencies:** Component 4.1 (contract format)

---

## Layer 5: Zero-Trust Proofs (ZTPs)

### Component 5.1: ZTP Format & Generation

**Problem:** How do agents build verifiable behavioral history? Self-reported metrics are untrustworthy. You need co-signed evidence from both parties to an interaction.

**Approaches:**
1. **Zynd-style co-signed ZTPs** — After task completion, both agents sign a record containing: delegation_id, deliverable_hash, outcome (success/failed/disputed), domain tag. Both signatures required for a valid ZTP.
   - Pro: Strong evidence (both parties attest), immutable, verifiable by anyone
   - Con: Requires cooperation from both parties, bad-faith actors can refuse to countersign
2. **Unilateral attestations** — Either party can publish an attestation about the interaction. Weight depends on the attester's trust.
   - Pro: No cooperation needed
   - Con: Easy to fake (agent creates sock puppets that attest to its quality)
3. **Registry-observed attestations** — Registries that proxy or observe interactions publish attestations (current EigenTrust model)
   - Pro: Third-party evidence
   - Con: Registries don't observe most interactions (direct agent-to-agent)

**Decision:** To be discussed.

**Key Design Points:**
- ZTP fields: id, delegation_id, provider_did, orchestrator_did, domain, deliverable_hash, outcome, provider_sig, orchestrator_sig, created_at
- What if the orchestrator refuses to countersign? (Zynd: 24h timeout, asymmetric ZTP accepted, orchestrator flagged)
- Where are ZTPs stored? Both agents + gossiped to registries?
- Are ZTPs append-only? Can they be revised?

**Dependencies:** Layer 4 (delegation contracts for the delegation_id reference)

**Zynd's approach:** Co-signed ZTPs with 24h countersign window. If orchestrator silent → asymmetric ZTP accepted, escrow releases to provider, orchestrator gets permanent `non-responsive-principal` flag. Disputes must include evidence within 12h.

**Decentralization challenge:** In Zynd, the central registry tracks countersign timeouts and triggers automatic acceptance. In our system, who enforces the 24h timeout? Options: (a) the home registry of the provider, (b) on-chain timeout, (c) both agents track independently and gossiped state is the tiebreaker.

---

### Component 5.2: ZTP Storage & Gossip

**Problem:** ZTPs are the raw data for trust computation. They need to be available to any registry that wants to compute a trust score for an agent. But gossiping every ZTP to every registry is expensive at scale.

**Approaches:**
1. **Full gossip** — All ZTPs propagated via gossip protocol, stored by every registry
   - Pro: Every registry can compute trust independently
   - Con: O(agents * interactions) data, doesn't scale
2. **On-demand fetch** — ZTPs stored by the involved agents and their home registries. Other registries fetch ZTPs for a specific agent when computing trust.
   - Pro: No gossip overhead, scales better
   - Con: Trust computation requires network requests, slower
3. **Aggregated trust gossip** — Instead of gossiping raw ZTPs, registries gossip aggregated trust summaries (like the current attestation model). Raw ZTPs are available on request for audit.
   - Pro: Efficient gossip (summaries are small), raw data available for verification
   - Con: Must trust the aggregator or verify from raw data

**Decision:** To be discussed.

**Dependencies:** Component 5.1 (ZTP format)

**Zynd's approach:** ZTPs stored in central PostgreSQL. No gossip needed — one database.

---

### Component 5.3: Dispute Resolution

**Problem:** Provider claims success, orchestrator claims failure. Who's right? In a decentralized system with no central authority, this is one of the hardest problems.

**Approaches:**
1. **On-chain timeouts only (no human disputes)** — If orchestrator doesn't countersign within 24h, provider wins automatically. If orchestrator disputes with evidence within 12h, escrow freezes. Resolution by timeout.
   - Pro: Trustless, deterministic, no human intervention
   - Con: No nuance — what if the work was genuinely bad but evidence is hard to prove on-chain?
2. **Registry-mediated disputes** — The home registry of the provider (or orchestrator) acts as a lightweight arbiter. Both parties submit evidence.
   - Pro: More nuanced resolution, human review possible
   - Con: Requires trusting the registry, potential bias (home registry may favor its own agents)
3. **Multi-registry jury** — 3-5 random uninvolved registries review the dispute. Majority vote decides outcome.
   - Pro: Decentralized, reduces bias
   - Con: Slow, complex coordination, registries may not want to volunteer
4. **Reputation-only resolution** — Don't resolve disputes per se. Just record that a dispute occurred. Both parties get a trust hit (Zynd: disputed = -0.5 weight regardless of who's right). Incentivizes clean transactions.
   - Pro: Simple, no arbitration needed
   - Con: Unfair to the innocent party

**Decision:** To be discussed.

**Dependencies:** Component 5.1 (ZTP format), Layer 7 (escrow for financial disputes)

**Zynd's approach:** Disputes must be raised within 12h with evidence (specific delegation clause violated + actual deliverable). If evidence not submitted in 12h → auto-dismissed + frivolous dispute flag. Being disputed hurts trust (-0.5 weight) even if you win. Incentivizes clean transactions.

---

## Layer 6: Trust Scoring & Reputation

### Component 6.1: Domain-Scoped Trust Vectors

**Problem:** A single global trust score is misleading. An agent that's excellent at web scraping might be terrible at financial analysis. Trust should be contextual.

**Approaches:**
1. **Per-domain vectors (Zynd approach)** — Each agent has a trust map: `{"web_scraping": 0.94, "finance": 0.12, "legal": 0.67}`. Search queries match against the relevant domain.
   - Pro: Most accurate, prevents domain mismatch
   - Con: Cold start per domain (even trusted agents start at 0 in new domains), domain taxonomy management
2. **Global score + domain bonus** — One global score plus modifiers for domains where the agent has strong ZTP history
   - Pro: Simpler, agents don't start at zero in new domains
   - Con: Less accurate, global score can be gamed
3. **Multi-dimensional trust** — Trust across fixed dimensions: reliability, speed, quality, honesty. Combine dimensions differently per query.
   - Pro: More nuanced than single score
   - Con: Dimensions are abstract, harder to compute from ZTP data

**Decision:** To be discussed.

**Key Design Points:**
- Who defines the domain taxonomy? Fixed list? Free-form tags? Hierarchical categories?
- How does domain-scoped trust interact with search ranking weights?
- Minimum ZTP count before a domain score is displayed?

**Dependencies:** Layer 5 (ZTPs as raw data for trust computation)

**Zynd's approach:** Free-form domain tags on ZTPs. Score per domain = weighted sum of outcomes with recency decay and counterparty tier multiplier. Agents can have wildly different scores across domains.

---

### Component 6.2: Trust Computation Algorithm

**Problem:** Given a set of ZTPs for an agent, how do you compute a trust score? Who computes it? Do all registries agree?

**Approaches:**
1. **Weighted ZTP aggregation** — `score = sum(weight * outcome) / sum(weight)` where weight = recency_decay * counterparty_trust_multiplier. Simple, deterministic.
   - Pro: Easy to understand and implement, deterministic (all registries agree if they have the same ZTPs)
   - Con: Doesn't capture transitive trust (trusted by agents I trust)
2. **EigenTrust (current model)** — Iterative algorithm that propagates trust transitively through the network. Trust is attenuated at each hop.
   - Pro: Captures network effects (trusted by trusted agents)
   - Con: Requires global knowledge of the trust graph, expensive to compute, registries may disagree
3. **PageRank-style graph scoring** — Treat agent interactions as a directed graph. Score based on the structure of who interacts with whom.
   - Pro: Network-effect reputation, Sybil-resistant
   - Con: Complex, requires significant ZTP data to be meaningful

**Decision:** To be discussed.

**Key Design Points:**
- Recency decay function: `weight = exp(-lambda * days)` where lambda chosen so 30 days = 0.5
- Counterparty weighting: interactions with high-trust agents count more
- Trust ceilings by developer verification tier?
- Should registries publish their computed trust scores? Can consumers verify them?

**Dependencies:** Component 6.1 (domain scoping), Layer 5 (ZTP data)

**Zynd's approach:** Weighted aggregation with recency decay and counterparty tier multiplier. DIA tier 1 agents capped at 0.60 trust regardless of ZTP history. Future: PageRank-style multi-hop scoring.

---

### Component 6.3: Trust Ceilings & Structural Limits

**Problem:** Should unverified developers have a hard cap on trust scores? This prevents Sybil attacks (create many anonymous agents, fake ZTPs with each other, inflate trust).

**Approaches:**
1. **Developer tier ceilings (Zynd approach)** — Unverified = capped at 0.60. Verified GitHub = uncapped. Domain verified = uncapped.
   - Pro: Strong Sybil resistance, incentivizes verification
   - Con: Penalizes legitimate anonymous developers, requires tier system
2. **No ceilings, trust decay** — No hard caps. Instead, trust decays faster for unverified developers. They can reach high trust but must maintain constant positive interactions.
   - Pro: More egalitarian
   - Con: Easier to game with sustained fake interactions
3. **Network-relative caps** — Cap based on network position, not developer tier. Agents with few unique counterparties are capped regardless of verification.
   - Pro: Directly targets Sybil (fake agents only interact with each other)
   - Con: Harder to compute, penalizes niche agents with few legitimate counterparties

**Decision:** To be discussed.

**Dependencies:** Component 2.3 (developer verification tiers), Component 6.2 (trust computation)

---

## Layer 7: Payments & Settlement

### Component 7.1: x402 Micropayments

**Problem:** Agents need to pay each other for services. Traditional payment rails (Stripe, bank transfers) are too slow and expensive for agent-to-agent microtransactions ($0.001 - $1.00).

**Approaches:**
1. **x402 protocol (Coinbase standard)** — HTTP-native micropayments. Agent returns `402 Payment Required` with crypto payment details. Caller pays on-chain, resubmits with payment proof header.
   - Pro: Standard protocol, works with existing HTTP, Coinbase backing
   - Con: Requires crypto wallet per agent, on-chain transaction per request (gas costs)
2. **Payment channels / state channels** — Open a payment channel between two agents. Many off-chain microtransactions, settle on-chain periodically.
   - Pro: Near-zero cost per transaction, instant settlement
   - Con: Requires channel opening/closing (capital lockup), complex
3. **Registry-mediated credits** — Agents deposit credits with their registry. Registry handles internal accounting. Settlement on-chain periodically.
   - Pro: Simplest for agents (no wallet needed)
   - Con: Centralized within each registry, trust in registry

**Decision:** To be discussed.

**Key Design Points:**
- Supported chains: Base L2 (primary, low gas, USDC native), Polygon, Ethereum mainnet
- Minimum viable payment: what's the minimum amount worth an on-chain transaction?
- Agent wallet management: who holds the keys? Developer? The agent itself?

**Dependencies:** Layer 3 (communication protocol for payment headers)

**Zynd's approach:** x402 with USDC on Base L2. 402 response includes crypto payment details. Caller pays, resubmits with `X-Payment-Proof` and `X-Challenge-Id` headers. 5-minute challenge timeout.

---

### Component 7.2: Trust-Tiered Escrow

**Problem:** Low-trust agents might take payment and not deliver. High-trust agents shouldn't need the overhead of escrow. Escrow requirements should scale with trust.

**Approaches:**
1. **On-chain escrow smart contract** — Payments locked in a smart contract. Released when both parties sign ZTP. Auto-refunded on timeout.
   - Pro: Trustless, no central authority
   - Con: Gas costs, on-chain latency
2. **Registry-mediated escrow** — Home registry holds funds in escrow. Releases based on ZTP co-signatures.
   - Pro: Faster, no gas costs for escrow operations
   - Con: Trust in the registry, registry holds funds
3. **No escrow, reputation-only** — Don't hold funds in escrow. If an agent scams, they get negative ZTPs and their trust drops. Market punishment instead of escrow.
   - Pro: Simplest, no escrow infrastructure
   - Con: First victim eats the loss, agents can hit-and-run

**Decision:** To be discussed.

**Key Design Points:**
- Trust thresholds for escrow tiers (Zynd: 0-0.30 mandatory, 0.30-0.60 optional, 0.60-0.85 on request, 0.85+ none)
- Max task value per trust tier
- Who acts as the oracle for releasing escrow?

**Dependencies:** Component 6.2 (trust scores for tiering), Component 5.1 (ZTPs for release condition)

**Zynd's approach:** On-chain escrow on Base L2 via `ZyndEscrow` contract. 2-of-3 Gnosis Safe oracle. Trust-tiered: mandatory below 0.30, none above 0.85. Zynd charges 1-2% escrow fee.

**Decentralization challenge:** Zynd has a single oracle (their multisig). We need either: (a) fully on-chain release logic (both signatures present = release), (b) multiple registries acting as co-oracles, or (c) time-locked escrow that auto-releases.

---

### Component 7.3: Revenue & Fee Model

**Problem:** Running registry infrastructure costs money. What's the sustainable economic model for registry operators?

**Approaches:**
1. **Per-ZTP fee** — Small fee ($0.001-0.002) per ZTP recorded through the registry
   - Pro: Scales with network activity
   - Con: Incentivizes registries to fake ZTPs
2. **Escrow fee** — Percentage of escrowed amounts (1-2%)
   - Pro: Revenue proportional to value transacted
   - Con: Incentivizes unnecessary escrow
3. **Subscription tiers** — Developers pay monthly for higher rate limits, more agents, priority search placement
   - Pro: Predictable revenue
   - Con: Centralized pricing per registry
4. **Free + premium** — Basic registration and search is free. Premium features (analytics, priority placement, higher rate limits) are paid.
   - Pro: Low barrier to entry
   - Con: Must define clear free vs premium boundary

**Decision:** To be discussed.

**Dependencies:** Component 7.1 (payment infrastructure)

**Zynd's approach:** $0.002 per ZTP, 1-2% escrow fee, 3% marketplace cut, $99-499/month enterprise subscriptions.

---

## Layer 8: Cold Start & Graduated Trust

### Component 8.1: Initial Trust Derivation

**Problem:** New agents have no ZTP history. Zero trust means no one uses them. Full trust means risk. Need a way to derive an initial trust score from structural signals.

**Approaches:**
1. **Structural scoring (Zynd approach)** — Initial score from: base (0.30) + developer_tier_bonus + benchmark_percentile + developer_clean_record - prior_incidents. Typically 0.40-0.55.
   - Pro: Immediate usability, honest labeling ("structural trust, not behavioral")
   - Con: Requires benchmarking infrastructure, developer tier system
2. **Zero trust + low-stakes sandbox** — New agents start at trust 0 but can only accept very small tasks (< $0.10). Build trust through real but low-risk interactions.
   - Pro: Simplest, no benchmarking infrastructure
   - Con: Slow start, many microtransactions needed to build trust
3. **Vouching system** — Existing trusted developers or agents can vouch for new agents. Voucher stakes reputation.
   - Pro: Decentralized, leverages existing trust
   - Con: Can form cartels, newcomers without connections are stuck

**Decision:** To be discussed.

**Dependencies:** Component 2.3 (developer tiers), Component 6.2 (trust computation)

**Zynd's approach:** Four-stage cold start: (0) registration + "new" badge, (1) automated benchmarks + structural score, (2) supervised interactions (parallel execution with reference agent, 5-10 tasks), (3) unsupervised with value caps, (4) full participant after 100 ZTPs with <0.5% disputes. Automatic, deterministic promotion — no human gatekeeping.

---

### Component 8.2: Capability Benchmarks

**Problem:** Before an agent handles real tasks, can we verify it can actually do what it claims? Standardized tests for declared capabilities.

**Approaches:**
1. **Registry-run benchmarks** — Each registry maintains benchmark suites for common capability categories. New agents run benchmarks as part of registration.
   - Pro: Objective measurement, immediate
   - Con: Benchmarks are limited (agent can optimize for tests), registries must maintain suites
2. **Community-contributed benchmarks** — Open benchmark suites that anyone can contribute to. Agents run whichever benchmarks match their capabilities.
   - Pro: Broader coverage, community-driven
   - Con: Quality control, gaming (contribute easy benchmarks)
3. **No benchmarks, behavioral only** — Skip benchmarks entirely. Let the cold start sandbox and ZTP history do the work.
   - Pro: Simplest, no benchmark infrastructure
   - Con: Slower path to trust, more risk during initial interactions

**Decision:** To be discussed.

**Dependencies:** Component 8.1 (feeds into initial trust derivation)

---

### Component 8.3: Supervised Interactions (Reference Agents)

**Problem:** During cold start, how do you protect consumers from unproven agents? Running the same task on both the new agent and a trusted reference agent provides a safety net.

**Approaches:**
1. **Parallel execution (Zynd approach)** — First 5-10 tasks run on both the new agent and a trusted reference agent. If outputs match, consumer gets the new agent's output. If mismatch, consumer gets the reference output, new agent still gets a ZTP (success or failure recorded).
   - Pro: Consumer is never harmed, real behavioral data collected
   - Con: Doubles compute cost, needs reference agents willing to participate, not all tasks are deterministic
2. **Shadow mode** — New agent handles the task for real, but output is reviewed/compared after the fact. Consumer gets the new agent's output immediately.
   - Pro: No doubled compute cost
   - Con: Consumer takes the risk
3. **Staged permissions** — New agents can only accept tasks below a value threshold. No supervision, just bounded risk.
   - Pro: Simplest
   - Con: No quality signal, just risk capping

**Decision:** To be discussed.

**Key Design Points:**
- Who are reference agents? How are they selected? What's the incentive?
- How do you compare outputs for non-deterministic tasks?
- Does the consumer know they're in a supervised interaction?

**Dependencies:** Component 8.1 (initial trust), Component 4.1 (delegation contracts for the supervised task)

**Zynd's approach:** Parallel execution. Reference agents earn passive ZTP credit. Delegating agents are never harmed. Explicit "flywheel" — network growth benefits incumbents.

---

## Cross-Cutting Concerns

### C.1: Agent Card Schema Versioning

**Problem:** The Agent Card format will evolve (adding integrity, authentication, etc.). Old consumers must handle new fields gracefully, and new consumers must understand old cards.

**Approach:** Add `schema_version` field (e.g., `"1.0"`, `"1.1"`). Consumers ignore unknown fields. Breaking changes increment the major version. Registries can indicate minimum supported schema version.

---

### C.2: Agent Card Extended Fields

**Problem:** The current Agent Card is missing practical fields for real-world agent interop.

**Fields to add:**
- `authentication` — How to auth when invoking (api_key, oauth, x402, signed_challenge, none)
- `rate_limits` — What throttling the agent enforces (requests_per_minute, concurrent_max)
- `supported_protocols` (top-level) — What protocols the agent speaks without scanning each capability
- `input_content_types` / `output_content_types` — MIME types
- `max_input_size` — Practical payload limit
- `dependencies` — Other agents/services this agent calls
- `region` / `data_residency` — Where data is processed (GDPR)
- `terms_of_service` / `privacy_policy` — URLs
- `schema_version` — Card format version

---

### C.3: SDK & Client Libraries

**Problem:** No client libraries exist. Agent developers need reference implementations for heartbeat, registration, card serving, and invocation.

**Needed:**
- Python SDK (most AI agents are Python)
- Go SDK (for the ecosystem)
- JavaScript/TypeScript SDK (for web-based agents)
- Reference Agent Card server (serves `/.well-known/agent.json` with heartbeat)

---

### C.4: Agent Card URL Standard

**Problem:** The `agent_url` is freeform. Should we enforce a standard path?

**Options:**
- Enforce `/.well-known/agent.json` (A2A convention)
- Allow any URL but recommend `/.well-known/agent.json`
- Allow any URL with no recommendation

---

### C.5: Multi-Registry Agent Registration

**Problem:** An agent registers on one home registry. If that registry goes down, the agent's authoritative record is unavailable.

**Options:**
- Allow registration on multiple registries (primary + backups)
- Migration protocol for changing home registry
- Gossip entries serve as backup (current behavior, but lightweight)

---

## Build Order

| Phase | Layers | Components | Reasoning |
|---|---|---|---|
| Phase 1 | Layer 1 | Heartbeat, build integrity, drift detection | Foundation — need to know agents are alive and legitimate |
| Phase 2 | Layer 0 fixes + Layer 2 | Security fixes (auth, gossip sigs, TLS), developer identity, key hierarchy, verification tiers | Can't build trust on an insecure foundation |
| Phase 3 | Layer 3 | Message format, endpoint revelation, NAT traversal | Agents need to talk to each other before they can transact |
| Phase 4 | Layers 4 + 5 | Delegation contracts, ZTP format, ZTP storage, dispute resolution | Trust evidence collection — the core value proposition |
| Phase 5 | Layer 6 | Domain-scoped vectors, trust computation, trust ceilings | Make trust data actionable in search and decisions |
| Phase 6 | Layer 7 | x402 payments, escrow, fee model | Economic layer — agents can exchange value |
| Phase 7 | Layer 8 | Cold start scoring, benchmarks, supervised interactions | Onboarding — smooth path for new agents |
| Ongoing | Cross-cutting | Schema versioning, SDKs, card fields, URL standard | Ecosystem maturity |
