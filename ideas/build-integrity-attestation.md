# Build Integrity & Code Attestation

**Layer:** 1 (Liveness & Code Integrity)
**Component:** 1.2
**Status:** Designed, not implemented
**Dependencies:** Layer 0 (agent identity, card fetcher)

## Problem

When someone discovers an agent via Agent DNS, they can verify two things today:
1. **Identity**: The agent is who it claims to be (Ed25519 signature on the registry record)
2. **Authenticity**: The Agent Card is genuine (signed by the agent's key)

But there is no way to verify:
3. **Code integrity**: Is the agent actually running the code it claims?

The Agent Card contains self-reported fields (`capabilities`, `framework`, `model`, etc.) that are entirely trust-on-first-use. A malicious agent could claim to be an open-source code reviewer running Claude, but actually be a data exfiltration tool. A legitimate agent could silently update its code to do something different from what it was audited for.

### Why a Simple Code Hash Isn't Enough

The naive solution — "hash the codebase and put it in the Agent Card" — has several issues:

1. **Hash of what?** The binary? The source? The Docker image? Each gives different guarantees.
2. **Who produces the hash?** If the agent self-reports it, a malicious agent just lies.
3. **Runtime behavior != code.** An agent's code might be fine, but it calls an LLM with a different system prompt, or environment variables change its behavior. The code hash says nothing about runtime configuration.
4. **Verification requires the artifact.** A hash only proves that *some* artifact matches — you still need access to the source or binary to actually audit it.

## Solution

**A layered build provenance system embedded in the Agent Card.** Two layers, implementable now:

### Layer 1: Self-Attestation (Agent Developer Signs the Build Hash)

The agent developer builds the artifact, computes its hash, and signs the hash with their Ed25519 agent key. This proves: "the agent key holder claims this artifact hash is what's running."

### Layer 2: Third-Party Build Attestation (CI System Signs the Build)

A trusted CI/CD system (GitHub Actions, GitLab CI) produces a signed SLSA provenance attestation. This proves: "this trusted third party built this exact artifact from this exact source commit." The agent developer cannot forge this.

### Agent Card `integrity` Section

```json
{
  "agent_id": "agdns:7f3a9c2e...",
  "version": "2.4.1",
  "status": "online",

  "integrity": {
    "source_repo": "https://github.com/example/codereview-agent",
    "source_commit": "a1b2c3d4e5f67890abcdef1234567890abcdef12",
    "source_visibility": "public",
    "build_hash": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
    "build_target": "linux/amd64",
    "build_system": "docker",
    "dockerfile_hash": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "built_at": "2026-03-10T12:00:00Z",

    "attestations": [
      {
        "type": "slsa-provenance-v1",
        "issuer": "https://github.com/actions/attestation",
        "issuer_display": "GitHub Actions",
        "subject_digest": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
        "build_config_uri": "https://github.com/example/codereview-agent/.github/workflows/release.yml@refs/tags/v2.4.1",
        "issued_at": "2026-03-10T12:00:00Z",
        "expires_at": "2027-03-10T12:00:00Z",
        "certificate": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
        "signature": "<sigstore-cosign-signature>"
      }
    ],

    "self_attestation": {
      "build_hash": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "signed_at": "2026-03-10T12:00:00Z",
      "signature": "ed25519:<signed-by-agent-key>"
    }
  },

  "capabilities": ["..."],
  "..."
}
```

### Trust Levels

What's present in the `integrity` section determines the trust level displayed to consumers:

| Present Fields | Trust Level | Display Label |
|---|---|---|
| Nothing | Unverified | "Unverified build" |
| `self_attestation` only | Self-reported | "Self-attested build" |
| `self_attestation` + public `source_repo` | Auditable | "Open source, self-attested" |
| `attestations` from trusted CI | Build-verified | "Build verified by GitHub Actions" |
| `attestations` + public `source_repo` | Fully transparent | "Open source, build verified" |

### Verification Flow

When the registry fetches an Agent Card (in `card/fetch.go`):

1. **Existing**: Verify Agent Card signature (proves card came from agent key holder)
2. **New**: If `integrity.self_attestation` exists:
   - Reconstruct the signable message: UTF-8 bytes of `build_hash`
   - Verify the `signature` against the agent's public key from the registry record
   - This proves the agent key holder claims this specific build hash
3. **New**: If `integrity.attestations` exist:
   - Verify the SLSA/Sigstore signature against the issuer's known public key
   - Verify the `subject_digest` matches `integrity.build_hash`
   - This proves a trusted CI system built this artifact from the claimed source
4. **New**: Compute an `integrity_level` string on the search result so consumers can filter/sort

### Agent Developer Workflow

```bash
# 1. Build the agent
docker build -t my-agent:v2.4.1 .

# 2. Get the image digest
IMAGE_HASH=$(docker inspect --format='{{.Id}}' my-agent:v2.4.1)

# 3. Self-attest with agent key (agentdns CLI helper)
agentdns attest \
  --build-hash "$IMAGE_HASH" \
  --source-repo "https://github.com/example/my-agent" \
  --source-commit "$(git rev-parse HEAD)"

# Outputs the integrity JSON block to embed in the Agent Card
```

For CI integration (GitHub Actions, automatic SLSA):
```yaml
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
      - run: docker build -t my-agent .
      - uses: actions/attest-build-provenance@v2
        with:
          subject-digest: sha256:${{ steps.build.outputs.digest }}
```

### Data Structures

```go
// Integrity contains build provenance and code verification data.
type Integrity struct {
    SourceRepo       string             `json:"source_repo,omitempty"`
    SourceCommit     string             `json:"source_commit,omitempty"`
    SourceVisibility string             `json:"source_visibility,omitempty"` // public, private
    BuildHash        string             `json:"build_hash,omitempty"`        // sha256:...
    BuildTarget      string             `json:"build_target,omitempty"`      // linux/amd64
    BuildSystem      string             `json:"build_system,omitempty"`      // docker, go, npm
    DockerfileHash   string             `json:"dockerfile_hash,omitempty"`   // sha256:...
    BuiltAt          string             `json:"built_at,omitempty"`
    Attestations     []BuildAttestation `json:"attestations,omitempty"`
    SelfAttestation  *SelfAttestation   `json:"self_attestation,omitempty"`
}

// BuildAttestation is a third-party attestation of build provenance (SLSA/Sigstore).
type BuildAttestation struct {
    Type           string `json:"type"`             // slsa-provenance-v1, sigstore-bundle
    Issuer         string `json:"issuer"`           // https://github.com/actions/attestation
    IssuerDisplay  string `json:"issuer_display"`   // GitHub Actions
    SubjectDigest  string `json:"subject_digest"`   // sha256:...
    BuildConfigURI string `json:"build_config_uri"` // workflow file ref
    IssuedAt       string `json:"issued_at"`
    ExpiresAt      string `json:"expires_at,omitempty"`
    Certificate    string `json:"certificate,omitempty"`
    Signature      string `json:"signature"`
}

// SelfAttestation is the agent developer's own signed claim about the build.
type SelfAttestation struct {
    BuildHash string `json:"build_hash"` // sha256:...
    SignedAt  string `json:"signed_at"`
    Signature string `json:"signature"`  // ed25519:<base64>
}
```

### Honest Limitations

What this does NOT solve:

- **Doesn't prove the running binary matches the hash.** That requires a Trusted Execution Environment (TEE) like Intel SGX, AMD SEV, or AWS Nitro Enclaves. This is a future layer, not practical for most agents today.
- **Doesn't verify the code is safe.** It only makes the code auditable if the source is public.
- **Private source agents** can self-attest, but consumers can't independently audit the source.
- **Third-party attestations require CI support for SLSA.** GitHub and GitLab support this natively. Others may not.
- **Runtime configuration** (environment variables, system prompts, API keys) is not captured. An agent could run the attested binary but behave differently based on config.

These are acceptable tradeoffs for a practical first implementation. Each limitation has a known upgrade path (TEE attestation, behavioral probes, config hashing) that can be added in future phases.

### Zynd's Approach (for reference)

Zynd uses build manifests with `model_hash`, `system_prompt_hash`, `code_hash`, `tool_versions`. Developer signs the manifest with their master key. Key difference: Zynd does **drift detection** via heartbeat — every heartbeat includes re-computed component hashes. If runtime hashes don't match the registered manifest, the agent is hidden immediately and the card NEVER updates to match drift.

### Files to Change

| File | Change |
|---|---|
| `internal/models/agent_card.go` | Add `Integrity`, `BuildAttestation`, `SelfAttestation` structs. Add `Integrity *Integrity` field to `AgentCard`. |
| `internal/card/fetch.go` | Add `verifySelfAttestation()` after card signature verification |
| `internal/models/search.go` | Add `IntegrityLevel string` to `SearchResult` |
| `internal/api/server.go` | Include integrity info in search results and card responses |
| `cmd/agentdns/main.go` | Add `attest` subcommand for generating self-attestation JSON |
| `tests/fixtures/agent_card.json` | Update fixture with `integrity` section |
| `docs/` | Regenerate Swagger docs |
