# Agent Heartbeat & Liveness Detection

**Layer:** 1 (Liveness & Code Integrity)
**Component:** 1.1
**Status:** Designed, not implemented
**Dependencies:** Layer 0 (agent identity, public key storage)

## Problem

When an agent goes offline, nothing happens in the registry. The `RegistryRecord` stays in PostgreSQL indefinitely, search results keep returning the dead agent with a hardcoded `Availability: 1.0`, and consumers only discover the agent is down when they try to invoke it and get a timeout. There is no mechanism to detect, mark, or exclude offline agents.

The current system has:
- No active health monitoring
- No heartbeat protocol between agents and registries
- Hardcoded availability scores (`1.0` for local agents, `0.8` for gossip agents)
- No concept of agent lifecycle states beyond "registered" and "tombstoned"
- `enrichCandidates()` attempts a card fetch at search time, but a failed fetch doesn't update any persisted state — the agent still appears in the next search

This means dead agents pollute search results, erode consumer trust, and waste network resources on federated queries that return unreachable agents.

## Solution

**Agent-initiated WebSocket heartbeat with cryptographic verification.** The agent is responsible for proving it's alive — the registry is passive.

### Protocol

1. The agent opens a WebSocket connection to its home registry at `GET /v1/agents/{agentID}/ws`
2. Every 30 seconds, the agent sends a signed heartbeat:
   ```json
   {
     "timestamp": "2026-03-12T10:00:30Z",
     "signature": "ed25519:<base64>"
   }
   ```
   - The signature is over the UTF-8 bytes of the `timestamp` string
   - Payload: ~120 bytes per heartbeat
3. The registry verifies:
   - Signature is valid against the agent's stored public key
   - `|timestamp - server_now| < 60 seconds` (clock skew tolerance, prevents replay)
4. On valid heartbeat: update `last_heartbeat` and `status = 'active'` in the `agents` table

### Liveness Rules

| Condition | Agent Status | Search Behavior |
|---|---|---|
| Heartbeat received within last 5 minutes | `active` | Included in search, `Availability = 1.0` |
| No heartbeat for 5+ minutes | `inactive` | Excluded from search results |
| Agent never sent a heartbeat | `inactive` | Never appears in search |
| Agent reconnects after being inactive | `active` (on first valid heartbeat) | Immediately included again |

### Background Monitor

A background goroutine (`monitor.go`) runs every 60 seconds:
1. Query `SELECT agent_id FROM agents WHERE status = 'active' AND last_heartbeat < NOW() - INTERVAL '5 minutes'`
2. Set `status = 'inactive'` for those agents
3. Gossip `agent_status` updates to mesh peers so other registries also exclude the agent

### Gossip Status Propagation

When an agent's status changes, the home registry gossips a lightweight status message:

```json
{
  "type": "agent_status",
  "agent_id": "agdns:7f3a...",
  "status": "inactive",
  "timestamp": "2026-03-12T10:05:00Z",
  "signature": "ed25519:<registry-signature>"
}
```

Other registries update their gossip entries accordingly. When the agent comes back and heartbeats resume, a `"status": "active"` gossip is sent.

### Search Integration

- `searchLocal()` filters agents by `status = 'active'` (default), or includes inactive agents if `include_inactive = true` is requested
- `searchGossip()` uses the gossip entry's status field for the `Availability` ranking signal
- `enrichCandidates()` no longer needs to be the only place where availability is determined — it becomes a supplementary check

### Edge Cases

| Case | Behavior |
|---|---|
| Agent goes down for 4 min, comes back | Stays `active` (within 5-min threshold) |
| Agent goes down for 6 min | Marked `inactive` at next monitor sweep |
| Registry restarts | All WebSocket connections drop. Agents reconnect. Agents with `last_heartbeat` within 5 min stay `active`. Others go `inactive` at next sweep. |
| Clock skew between agent and registry | 60-second tolerance on timestamp verification |
| Replay attack (someone replays old heartbeat) | Rejected: `|timestamp - now| > 60s` |
| Agent sends heartbeat to wrong registry | Rejected: agent_id not found in that registry's local store |
| Agent registered on registry A, searched on registry B | Registry A gossips status change, registry B updates its gossip entry |

### Why WebSocket Over HTTP POST

- **Persistent connection**: No TCP/TLS handshake per heartbeat. At 100K agents, HTTP POST would be ~3,300 req/s just for heartbeats.
- **Instant disconnect detection**: When the TCP connection drops, the registry knows within seconds (via read timeout), not minutes.
- **Lower overhead**: ~120 bytes per WS frame vs ~500+ bytes per HTTP request with headers.
- **Bidirectional (future)**: The registry could push commands to agents (e.g., "re-sign your card", "new protocol version available").

### Database Changes

Add two columns to the `agents` table:
```sql
ALTER TABLE agents ADD COLUMN last_heartbeat TIMESTAMPTZ;
ALTER TABLE agents ADD COLUMN status TEXT NOT NULL DEFAULT 'inactive';
CREATE INDEX idx_agents_status ON agents(status);
```

Add `status` column to `gossip_entries`:
```sql
ALTER TABLE gossip_entries ADD COLUMN status TEXT NOT NULL DEFAULT 'inactive';
```

### Configuration

```toml
[heartbeat]
timeout_seconds = 300          # 5 minutes — mark inactive after this
check_interval_seconds = 60    # how often the monitor sweeps
clock_skew_tolerance_seconds = 60
```

### Files to Change

| File | Change |
|---|---|
| `internal/models/registry_record.go` | Add `LastHeartbeat` and `Status` fields to `RegistryRecord` |
| `internal/models/gossip.go` | Add `agent_status` action type, add `Status` field to `GossipAnnouncement` and `GossipEntry` |
| `internal/store/store.go` | Add `UpdateAgentHeartbeat()` and `MarkInactiveAgents()` to `Store` interface |
| `internal/store/postgres.go` | Schema migration + implement new methods + filter by status in search |
| `internal/api/server.go` | Add `handleAgentWebSocket` handler at `GET /v1/agents/{agentID}/ws` |
| `internal/api/monitor.go` | **New** — background goroutine for liveness sweeps |
| `internal/search/engine.go` | Use real status/availability instead of hardcoded values |
| `internal/mesh/gossip.go` | Handle `agent_status` action in `HandleAnnouncement()` |
| `internal/config/config.go` | Add `HeartbeatConfig` struct |
| `config/*.toml` | Add `[heartbeat]` section |
| `cmd/agentdns/main.go` | Start monitor goroutine, wire WebSocket handler |
| `go.mod` | Add WebSocket dependency (`nhooyr.io/websocket`) |

### Agent-Side Example (Python)

```python
import asyncio, json, base64, websockets
from datetime import datetime, timezone
from nacl.signing import SigningKey

async def heartbeat(agent_id, private_key_bytes, registry_url):
    uri = f"ws://{registry_url}/v1/agents/{agent_id}/ws"
    while True:
        try:
            async with websockets.connect(uri) as ws:
                while True:
                    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
                    key = SigningKey(private_key_bytes)
                    sig = key.sign(ts.encode()).signature
                    await ws.send(json.dumps({
                        "timestamp": ts,
                        "signature": f"ed25519:{base64.b64encode(sig).decode()}"
                    }))
                    await asyncio.sleep(30)
        except Exception:
            await asyncio.sleep(5)  # reconnect after 5s
```
