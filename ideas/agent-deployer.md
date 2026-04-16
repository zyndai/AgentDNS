# Agent Deployer

**Status:** Idea
**Date:** 2026-04-16

## Problem

Users publishing Zynd agents/services today rely on ngrok — flaky, non-deterministic URLs, breaks on laptop sleep. Non-infra users can't get a stable hosted agent running.

## Proposal

A standalone, self-hostable deployer service. User uploads a zynd project folder, the deployer runs it in a Docker container on a single VM, and exposes it at `https://<slug>.deploy.<host>`. Replaces ngrok entirely.

## Key decisions

- **Separate product.** New repo `zynd-deployer`. Not coupled to the dashboard.
- **Single VM.** Docker multi-tenancy + Caddy reverse proxy (wildcard TLS via Cloudflare DNS-01).
- **User owns the keypair.** Uploaded folder includes `keypair.json`. Deployed entity stays under the user's developer handle. Deployer is purely a hosting layer, not a network developer.
- **Dev key never uploaded.** User must run `zynd agent run` locally once so the entity exists on the registry. Container falls into update path — no `developer_proof` needed. Validator rejects `developer.json` if found in the upload.
- **Key custody.** Encrypted at rest with `age` under a VM-local master key. Decrypted only when container starts.
- **Stack:** Next.js + TypeScript + Prisma + Postgres. Worker is a sibling Node process in the same repo. They coordinate through Postgres.
- **Scope:** agents + services from v1.

## Architecture

```
User laptop          Deployer (Next.js + Worker)           VM (same box)

zynd agent init ──►  POST /api/deployments  ──────►  Worker (systemd)
zynd agent run       (auth, validate, encrypt,         ├─ decrypt blob
(registers entity)    store blob, insert row)           ├─ unpack
zip folder ─────►                                       ├─ rewrite .env
                                                        ├─ allocate port
                                                        ├─ docker run base-image
                                                        ├─ poll /health
                                                        ├─ Caddy API: add route
                                                        └─ status=running

Caddy: *.deploy.<host> → localhost:<port>
Container: runs user's agent.py with user's keypair → heartbeat → registry
```

## Upload format

Standard zynd project folder, zipped client-side:
- `{agent,service}.config.json`
- `{agent,service}.py`
- `.env` (framework API keys)
- `keypair.json` (required)
- `requirements.txt` (optional)

## Core flow

1. **Upload** → validate → `age` encrypt blob → insert `Deployment{status:queued}`.
2. **Worker** polls DB: decrypt → unpack → rewrite `.env` → allocate port → `docker run` → poll `/health` → Caddy adds route → `status=running`.
3. **Logs**: worker tails `docker logs -f` into DB. SSE endpoint streams to browser.
4. **Stop**: worker stops container, removes Caddy route, frees port.
5. **Redeploy**: same user+name → atomic swap, reuse slug (URL stability).

## Data model

```
User: id, email, createdAt
Deployment: id, userId, name, slug (unique), entityType, status,
            blobPath, port (unique), containerId, hostUrl, errorMessage,
            createdAt, startedAt, stoppedAt
DeploymentLog: id, deploymentId, lineNo, text, ts
```

## Repo layout

```
zynd-deployer/
├── src/app/               # Next.js pages + API routes
├── src/lib/               # db, auth, crypto, validator, types
├── worker/                # main.ts, docker.ts, caddy.ts, ports.ts
├── prisma/schema.prisma
├── images/                # Dockerfile.agent-base, Dockerfile.service-base
└── infra/install.sh       # VM bootstrap + systemd units
```

## Tech choices

- Next.js 15 + TypeScript + Prisma + Postgres
- `dockerode` (Node Docker SDK)
- Caddy admin API via `fetch`
- NextAuth with GitHub OAuth
- `jszip` client-side zipping
- `age` for encryption at rest
- Two systemd units: web (`next start`) + worker (`tsx worker/main.ts`)

## Phases

**v1 (MVP):** scaffold, install script, base images, schema, auth, upload/list/detail/logs API, worker, UI pages, end-to-end smoke test.

**v2:** restart/redeploy buttons, `docker stats` metrics, GC stopped deploys, zero-knowledge client-side encryption.

**v3:** multi-VM scheduler, egress firewall, rate limits, image signing, pre-signed `developer_proof.json` for zero-local-run deploys.

## Security notes

- Agent private key: encrypted at rest with `age`, decrypted only into container env. Operator can read keys — disclosed in UI.
- Developer private key: never uploaded, never handled. Validator rejects it.
- Containers: Docker user-namespace remap, memory/CPU caps, loopback-only port binding.
- TLS: Caddy auto-provisions via Cloudflare DNS-01 wildcard cert.
