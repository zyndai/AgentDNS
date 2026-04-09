# Zynd Services Directory — Research & Strategy

> **Context**: Chandan suggested listing external services/tools (like MPP's directory at mpp.dev/services) on ZyndAI so agents can discover and use them alongside other agents. This document captures the full competitive research and strategic recommendation.

---

## 1. The Landscape (TL;DR)

- **MPP (Stripe/Tempo)**: 100+ services, $12K/week volume, 1,400 agents, IETF spec, multi-rail payments (crypto + fiat). Launched March 18, 2026. Directory at mpp.dev/services is a curated list of API proxies — Tempo/Locus wraps existing APIs (OpenAI, Alchemy, etc.) behind MPP payment gates.
- **x402 Foundation**: Coinbase + Linux Foundation + Google/AWS/Visa/Mastercard. 50M+ txs but only $28K/day real volume. Per-request settlement. ZyndAI already uses this.
- **MCP**: 20K+ servers, 97M monthly SDK downloads. **Zero native payment layer.** Biggest gap in the market.
- **A2A**: Agent Cards at `/.well-known/agent-card.json` becoming the standard. 150+ partners (Google, Salesforce, SAP, etc.). No payment layer — Google built AP2 separately.
- **Nobody** combines: agent registry + service directory + payments + trust + federated discovery in one stack.

---

## 2. Competitive Deep Dive

### 2.1 MPP (Machine Payments Protocol)

**Created by**: Stripe + Tempo Labs (co-authored)
**Funding**: Tempo raised $500M Series A at $5B valuation (Paradigm-led)
**Launch**: March 18, 2026

**Protocol Flow (Challenge-Credential-Receipt)**:
1. Client sends HTTP request to paid resource
2. Server responds `402 Payment Required` with `WWW-Authenticate: Payment` header (contains id, realm, method, intent, request, expires, digest)
3. Client fulfills payment challenge (on-chain tx, card payment, Lightning invoice, etc.)
4. Client retries with `Authorization: Payment <base64url-encoded credential>`
5. Server verifies, returns resource with `Payment-Receipt` header

**Payment Methods**:
| Method | Settlement | Status |
|--------|-----------|--------|
| Tempo | Stablecoin (USDC/USDT/pathUSD) on Tempo L1 | Production |
| Stripe | Cards, wallets via Shared Payment Tokens | Production (US only) |
| Lightning | Bitcoin over BOLT11 invoices | Available |
| Card | Encrypted network tokens | Available |
| Solana | Native SOL + SPL tokens | Available (spec not finalized) |
| Stellar | SEP-41 tokens | Available |

**Two Payment Intents**:
- **Charge** (one-shot): Each request settles independently. Backward-compatible with x402
- **Session** (streaming): Agent deposits into escrow (~500ms setup), issues cumulative EIP-712 signed vouchers. Sub-100ms latency. Micropayments as small as $0.0001/request, batch-settled into single on-chain tx

**Services Directory** (mpp.dev/services): 100+ services at launch

| Category | Examples |
|----------|---------|
| AI/LLM | OpenAI, Anthropic, Gemini, DeepSeek, Mistral, Grok, Groq, fal.ai |
| Search | Parallel, Brave Search, Exa |
| Blockchain | Alchemy, Allium, Codex, CoinGecko, Dune, Nansen |
| Data | StableTravel, Alpha Vantage, Apollo, Google Maps, Hunter, IPinfo, EDGAR/SEC |
| Web/Infra | Browserbase, Firecrawl, 2Captcha, Diffbot |
| Compute | Modal, Judge0, Build With Locus |

**Service Endpoint Patterns**:
1. Tempo-proxied: `servicename.mpp.tempo.xyz`
2. Locus-proxied: `servicename.mpp.paywithlocus.com`
3. Native integration: Custom domains (mpp.alchemy.com, mpp.browserbase.com)

**Tempo CLI**: Rust-based CLI (v0.4.0). `tempo wallet login` (passkey auth), `tempo request <url>` (paid HTTP request), `tempo wallet fund`, session management

**IETF**: `draft-ryan-httpauth-payment-01` submitted. Active Internet-Draft, not yet assigned to working group

**Adoption (3 weeks post-launch)**:
- ~30,047 transactions (7-day)
- ~$11,944 volume (7-day)
- 1,397 unique agents
- 170 payment servers
- Top services: HYRE Agent API (6,647 txs), StableEnrich (2,780), Google Maps (1,588)

**What MPP Lacks**:
- No trust/reputation layer
- No federated discovery (centralized at mpp.dev)
- No agent-to-agent communication
- No composability (can't chain service A -> service B -> agent C)
- No DID/identity for services
- Fiat is US-only via Stripe
- Session payments only work on Tempo chain

---

### 2.2 x402 Protocol (Coinbase)

**Created by**: Coinbase, now under Linux Foundation
**Foundation launched**: April 2, 2026

**Founding coalition**: Stripe, Cloudflare, AWS, Google, Microsoft, Visa, Mastercard, American Express, Shopify, Circle, Solana Foundation, Polygon Labs, Adyen, Fiserv, KakaoPay, PPRO, Sierra, thirdweb

**How it works**: Three HTTP headers (PAYMENT-REQUIRED, PAYMENT-SIGNATURE, PAYMENT-RESPONSE). Settlement in USDC on Base, Ethereum, Solana. Sub-cent transaction fees.

**Honest traction**:
- 50M+ cumulative transactions (Coinbase claim)
- Daily volume only ~$28K (Artemis onchain data, March 2026)
- Average payment ~$0.20
- ~50% flagged as artificial (self-dealing, wash trading)
- Daily tx count down 92% from Dec 2025 peak

**x402 vs MPP**: MPP is backwards-compatible with x402. x402 "exact" flow maps directly to MPP charge intent. The two are not mutually exclusive.

---

### 2.3 A2A Protocol (Google)

**Agent Card format** at `/.well-known/agent-card.json`:
```json
{
  "name": "...",
  "description": "...",
  "version": "...",
  "provider": {},
  "url": "...",
  "capabilities": { "streaming": true, "pushNotifications": true },
  "skills": [{ "name": "...", "description": "...", "inputModes": [], "outputModes": [] }],
  "securitySchemes": {},
  "interfaces": ["json-rpc", "grpc"]
}
```

**Adoption**: 150+ partners. V1.0 with gRPC. Donated to Linux Foundation's AAIF alongside MCP.

---

### 2.4 MCP (Model Context Protocol)

**MCP Server Registries**:
- Smithery.ai: 2,500+ servers
- Glama.ai: 21,108 servers
- MCP.so: 19,656 servers
- Official MCP Registry: planned Q4 2026

**Critical gap**: No payment layer. Cloudflare bridged with x402 middleware (`withX402/paidTool`). ZyndAI's MCP server is well-positioned here.

**Upcoming**: Server Cards at `/.well-known/mcp/server-card.json` (SEP-1649), manifests at `/.well-known/mcp` (SEP-1960)

---

### 2.5 Other Competitors

| Player | What | Traction | Moat | Weakness |
|--------|------|----------|------|----------|
| **Fetch.ai / ASI** | Agentverse marketplace | 2.74M agents, 131M messages, 15K autonomous agents | Largest agent count, NVIDIA partnership | Token lock-in, complex onboarding |
| **Nevermined** | Payments infra for AI agents | Visa integration, x402 facilitator | Multi-pricing (subs, bundles, pay-per-use) | NVM token friction, no federated discovery |
| **Autonolas/Olas** | On-chain agent registry | 10M+ agent-to-agent txs, 8 blockchains | NFT-based composability, first to prove A2A crypto payments (2023) | Complex, Ethereum-centric |
| **Virtuals Protocol** | Agent tokenization | 18K agents, $470M aGDP | Financial incentives, ACP proven | 87% token price decline, speculative |
| **ElizaOS** | Open-source agent framework | Large OSS community, Solana ecosystem | Plugin registry, x402-swarms plugin | No native payment protocol |
| **Morpheus** | Decentralized AI network | 1M+ users, 300+ devs, 320K ETH staked | Decentralized compute marketplace | MOR token complexity |
| **NEAR AI** | Agent marketplace | Task-based discovery | NEAR Intents (natural language) | NEAR-specific, not EVM |
| **Skyfire** | Identity + payments for agents | a16z, Coinbase Ventures backed | KYAPay protocol, Visa demo | Early stage |
| **Crossmint** | Unified API for all payment protocols | Google AP2 partner | Single API across protocols | Infrastructure only |

---

### 2.6 Payment Protocol Stack Summary

| Protocol | Layer | Focus | Owner |
|----------|-------|-------|-------|
| AP2 | Authorization | Trust framework, verifiable credentials | Google |
| ACP (OpenAI) | Checkout | Agent-to-merchant commerce | OpenAI + Stripe |
| ACP (Virtuals) | Commerce | Agent-to-agent commerce | Virtuals Protocol |
| x402 | Settlement | Per-request stablecoin micropayments | Coinbase / Linux Foundation |
| MPP | Settlement + Sessions | Streaming payments, multi-rail | Stripe / Tempo |
| MCP | Tool access | Agent-to-tool connections | Anthropic / AAIF |
| A2A | Communication | Agent-to-agent coordination | Google / AAIF |

---

## 3. ZyndAI's Strategic Position

### Capability Matrix

| Capability | ZyndAI | MPP | x402 | A2A |
|-----------|--------|-----|------|-----|
| Agent Registry | **Yes** | No | No | Partial |
| Service Directory | **No** | Yes (100+) | No | No |
| Federated Discovery | **Yes** (AgentDNS) | No | No | No |
| Payments (x402) | **Yes** | Yes (multi-rail) | Yes | No |
| Trust/Reputation | **Yes** (EigenTrust) | No | No | No |
| Agent-to-Agent Comms | **Yes** | No | No | Yes |
| DID Identity | **Yes** (iden3) | No | No | No |
| Agent Cards | Partial | No | No | Yes |
| Semantic Search | **Yes** (pgvector + BM25) | No | No | No |
| Session Management | **Yes** | Yes | Partial | No |

**The gap is the service directory. Adding it completes the picture.**

---

## 4. Recommendation: Zynd Services

> Don't copy MPP's proxy-only model. Build something fundamentally stronger by treating **services as first-class citizens alongside agents** in the existing registry.

### Core Concept

Two types of entities in ZyndAI's world:
- **Agents** — autonomous, stateful, can reason and compose (existing)
- **Services** — stateless API tools that agents consume (new)

Both live in the same registry, same discovery, same payment layer, same trust system. An agent searching for "translate this document" might find:
- Another agent (autonomous, can negotiate, might use multiple tools)
- A service (direct API call to DeepL, pay-per-request)

The agent chooses based on price, trust score, capability match, and latency.

### Why This Beats MPP

| Dimension | MPP | Zynd Services |
|-----------|-----|---------------|
| Discovery | Centralized table | Federated semantic search + bloom filters |
| Trust | None | EigenTrust reputation from actual usage |
| Composability | Service -> Agent only | Agent -> Service -> Agent -> Service (any chain) |
| Identity | URL-based | DID-based (verifiable, portable) |
| Payment | MPP-only (tied to Tempo chain) | x402 + MPP-compatible + ZyndPay routing |
| Onboarding | Requires proxy setup or SDK integration | `zynd service register` wraps any OpenAPI spec |
| Ecosystem lock-in | Tempo chain for sessions | Chain-agnostic (Base, Polygon, any EVM) |

### Architecture

```
+---------------------------------------------------+
|              ZYND UNIFIED REGISTRY                |
|                                                   |
|   +----------+    +-----------+                   |
|   |  AGENTS  |    | SERVICES  |  <-- NEW LAYER    |
|   |          |    |           |                   |
|   | LangChain|    | OpenAI    |                   |
|   | CrewAI   |    | Anthropic |                   |
|   | Custom   |    | Alchemy   |                   |
|   | n8n      |    | Firecrawl |                   |
|   +----+-----+    +-----+-----+                   |
|        |                |                         |
|   Same search, same trust, same payments          |
|   Same Agent Cards, same DIDs                     |
+--------+----------------+-------------------------+
         |                |
    AgentDNS gossip   AgentDNS gossip
    (federated)       (federated)
```

### Service Record Schema

```json
{
  "type": "service",
  "name": "OpenAI Completions",
  "provider": "openai",
  "category": "ai",
  "description": "GPT-4o, o3, embeddings, image generation",
  "endpoint": "https://api.openai.com/v1",
  "pricing": {
    "model": "per_request",
    "base_price_usd": 0.003,
    "payment_methods": ["x402", "api_key"],
    "currency": "USDC"
  },
  "capabilities": {
    "tools": ["chat_completions", "embeddings", "image_generation"],
    "protocols": ["openapi", "mcp"],
    "input_modes": ["text/plain", "image/png"],
    "output_modes": ["text/plain", "application/json"]
  },
  "openapi_url": "https://api.openai.com/openapi.json",
  "mcp_endpoint": null,
  "trust_score": 0.97,
  "uptime_30d": 99.94,
  "avg_latency_ms": 240,
  "did": "did:zynd:service:openai-completions"
}
```

### Three Ways Services Get Listed

1. **Self-registration** — `zynd service register --openapi https://api.example.com/openapi.json` — auto-extracts endpoints, pricing, capabilities
2. **Wrapped proxy** — For services without x402: agent pays Zynd via x402 -> Zynd calls service with API key -> returns result. Revenue split with provider
3. **Community-submitted** — Anyone submits a service pointing to existing API. Gets trust score of 0 until verified

### Revenue Model

```
Agent A calls Service X via Zynd:
  Service price:     $0.01
  Zynd commission:   $0.001 (10%)
  Agent A pays:      $0.011 total

Agent A calls Agent B (existing model):
  Agent B price:     $0.05
  Zynd commission:   $0.005 (10%)
  Agent A pays:      $0.055 total
```

Commission configurable. Self-hosted registries can set to 0%.

---

## 5. Implementation Phases

### Phase 1 — Service Schema + Registry (1-2 weeks)
- Add `type: "service" | "agent"` to existing Agent model in registry
- Add service-specific fields: `openapi_url`, `pricing`, `uptime`, `avg_latency`
- Extend search to filter by type
- Add service registration endpoint to registry API

### Phase 2 — SDK + CLI Integration (1 week)
- `zynd service register --openapi <url>` — parse OpenAPI, extract tools, register
- `zynd service search "web scraping"` — discover services
- `ZyndAIAgent.use_service(service_id, params)` — call service from agent code
- Auto-discover services during `fan_out()` orchestration

### Phase 3 — Service Directory UI (1-2 weeks)
- Page on dashboard/website: `/services` — browse, search, filter by category
- Service detail page: pricing, trust score, uptime chart, API docs link
- "Use with agent" button generates code snippet

### Phase 4 — Proxy + Payments (1-2 weeks)
- Wrap services that don't support x402 natively
- Route payments through ZyndPay
- Track usage metrics for trust scoring

### Phase 5 — Health Monitoring + Trust (ongoing)
- Ping service health endpoints every 5min (reuse existing health check cron)
- Track response times, error rates, uptime
- Feed into EigenTrust for reputation scoring
- Agents prefer higher-trust services in search results

---

## 6. Priority Services to List First

Based on MPP's top services by transaction volume and agent utility:

| Category | Services | Why |
|----------|----------|-----|
| AI/LLM | OpenAI, Anthropic, Groq, DeepSeek | Every agent needs inference |
| Search | Exa, Brave Search, Firecrawl | Agents need web access |
| Blockchain | Alchemy, Dune, CoinGecko | Our Web3 DNA |
| Data | Google Maps, Alpha Vantage, Hunter | High-volume on MPP |
| Compute | Modal, Judge0 | Agents running code |
| Communication | AgentMail, Browserbase | Agent infrastructure |

Start with 20-30 high-quality services. Quality > quantity. Each one verified, health-monitored, trust-scored.

---

## 7. Killer Differentiator: Composable Workflows

MPP lets agents call services one at a time. ZyndAI can enable **composable workflows** where agents chain services and other agents:

```python
agent = ZyndAIAgent(config)

# Agent discovers and chains: search -> scrape -> summarize
results = await agent.compose([
    ("service", "exa-search", {"query": "latest AI research"}),
    ("service", "firecrawl", {"urls": "$prev.urls"}),
    ("agent", "research-summarizer", {"docs": "$prev.content"}),
])
```

**This is the moat.** MPP can't do this because it has no agent layer. A2A can't do this because it has no service layer. Only ZyndAI would have both.

---

## 8. Market Data (April 2026)

**Agent Payments Stack** (agentpaymentsstack.com):
- 179 projects across 6 layers
- $43M settled volume (~$600M annualized)
- 140M transactions
- 98.6% settle in USDC
- Major M&A: Capital One/Brex ($5.15B), Mastercard/BVNK ($1.8B), Stripe/Bridge ($1.1B)

---

## 9. Bottom Line

**Yes, build the services directory.** But don't just list services — make them composable with agents.

> **Agents + Services + Trust + Payments + Federation = the network nobody else has**

MPP has services but no agents. A2A has agents but no services. x402 has payments but no discovery. ZyndAI can be the only place where all five converge.

---

## Sources

- [MPP Homepage](https://mpp.dev/) | [MPP Services](https://mpp.dev/services) | [MPP Docs](https://mpp.dev/overview)
- [Stripe MPP Blog](https://stripe.com/blog/machine-payments-protocol) | [Stripe MPP Docs](https://docs.stripe.com/payments/machine/mpp)
- [IETF Draft: draft-ryan-httpauth-payment](https://datatracker.ietf.org/doc/draft-ryan-httpauth-payment/)
- [MPPScan Explorer](https://mppscan.com/)
- [x402 Foundation Launch](https://www.coindesk.com/tech/2026/04/02/coinbase-s-ai-payments-system-joins-linux-foundation)
- [x402 Protocol](https://www.x402.org/) | [x402 V2](https://www.x402.org/writing/x402-v2-launch)
- [x402 vs MPP (Alchemy)](https://www.alchemy.com/blog/x402-vs-mpp-comparing-agent-payment-protocols)
- [x402 vs MPP (WorkOS)](https://workos.com/blog/x402-vs-stripe-mpp-how-to-choose-payment-infrastructure-for-ai-agents-and-mcp-tools-in-2026)
- [A2A Protocol](https://a2a-protocol.org/latest/specification/) | [Google A2A Blog](https://developers.googleblog.com/en/a2a-a-new-era-of-agent-interoperability/)
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-11-25)
- [MCP 2026 Roadmap](https://blog.modelcontextprotocol.io/posts/2026-mcp-roadmap/)
- [Agentverse (Fetch.ai)](https://agentverse.ai/) | [Fetch.ai Statistics](https://coinlaw.io/fetch-ai-statistics/)
- [Nevermined](https://nevermined.ai/product/) | [Nevermined x402](https://nevermined.ai/blog/the-payment-layer-ai-agents-actually-need)
- [Olas Network](https://olas.network/) | [Autonolas Registry](https://registry.olas.network/)
- [Virtuals Protocol](https://www.virtuals.io/) | [Virtuals ACP](https://whitepaper.virtuals.io/about-virtuals/agent-commerce-protocol)
- [Morpheus](https://mor.org/) | [ElizaOS](https://docs.elizaos.ai/)
- [NEAR AI Agent Market](https://near.ai/blog/introducing-near-ai-agent-market)
- [Skyfire](https://skyfire.xyz/) | [Crossmint Payments](https://www.crossmint.com/solutions/agentic-payments)
- [Agent Payments Stack](https://agentpaymentsstack.com/)
- [Cloudflare x402 docs](https://developers.cloudflare.com/agents/x402/)
- [Google AP2 Protocol](https://ap2-protocol.org)
- [Tempo Wallet CLI](https://github.com/tempoxyz/wallet) | [Tempo Docs](https://docs.tempo.xyz/)
- [llms.txt Specification](https://llmstxt.org/)
- [Protocol Ecosystem Map 2026](https://www.digitalapplied.com/blog/ai-agent-protocol-ecosystem-map-2026-mcp-a2a-acp-ucp)
