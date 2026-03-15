# Runtime Behavioral Evaluation

**Layer:** 2 (Runtime Behavior & Agent Quality)
**Component:** 2.1
**Status:** Designed, partially implemented
**Dependencies:** Layer 1 (agent identity, build integrity attestation)

## The Gap in Zynd's Current Trust Model

Zynd today can verify:

| What | How |
|---|---|
| Agent identity | W3C DIDs, Ed25519 signatures |
| Agent Card authenticity | Signed by agent key |
| Code integrity | SLSA build provenance, build hash |
| Developer identity | DIA trust tiers |

What Zynd **cannot** verify today:

| What | Why It Matters |
|---|---|
| Does the agent actually answer correctly? | A passing build hash says nothing about answer quality |
| Is a real LLM running, or a fake? | Any process can claim to use GPT-4 in its Agent Card |
| Does the agent stay consistent over time? | A verified agent can silently degrade after registration |
| Does the agent respect its claimed capabilities? | Self-reported `capabilities` field is entirely trust-on-first-use |
| Is the agent hallucinating? | No current signal for fabricated or confidently wrong answers |

### Why This Is a Real Problem

The build integrity layer (Component 1.2) answers: *"is this the binary the developer claims?"*

It does not answer: *"does this binary actually do what the Agent Card claims?"*

Consider these scenarios that pass all current Zynd checks today:

**Scenario 1 — Hardcoded responses**
A developer registers an agent claiming capability `stock-analysis`. The binary is exactly what was attested. But instead of calling an LLM, the agent returns pre-stored template responses to every query. Build hash matches. Agent Card signature valid. ZTP reputation accumulates. Users receive useless answers.

**Scenario 2 — Capability mismatch**
An agent declares `capabilities: ["legal-research", "contract-analysis"]` in its Agent Card. The agent is real and runs a genuine LLM. But it has no domain knowledge of law and fails every legal query. Nothing in the current stack catches this before users discover it.

**Scenario 3 — Confident hallucination**
An agent passes build attestation and has good ZTP reputation from casual interactions. When asked domain-specific questions it fabricates answers confidently. ZTP scores reflect interaction volume, not answer correctness.

### The Root Cause

Zynd's current trust model is entirely **build-time and identity-time**. There is no **runtime** verification layer. Once an agent is registered and attested, Zynd trusts it indefinitely unless a ZTP interaction explicitly marks it negatively — which requires a human or another agent to notice and report bad behavior.

This creates a trust gap between what an agent claims and what it actually does.

---

## How Runtime Behavioral Evaluation Fills This Gap

Runtime behavioral evaluation answers the question build attestation cannot: *"does this agent actually do what it claims, right now, with real inputs?"*

The approach is to evaluate every registered agent against a structured set of test inputs before it appears in search results, and to re-evaluate it periodically while it is live. Evaluation is automated, objective, and scored. Agents that fail are hidden from discovery until they pass.

This makes the `capabilities` field in the Agent Card verifiable instead of self-reported.

### Three Evaluation Layers

Behavioral evaluation runs in three layers. Each layer tests a different dimension of agent quality. A single layer alone is insufficient — they are designed to catch different classes of failure.

---

#### Layer 1 — Functional Testing

**What it tests:** Does the agent return correct answers to questions with known answers?

This layer sends the agent questions where the correct answer is already known. It checks whether the agent's response contains the right information. It is the equivalent of a skill test with an answer key.

Five test types cover different failure modes:

**Keyword check** — The answer must contain a specific term or one of several accepted terms. Catches agents that are completely wrong or answer a different question entirely.

**Numeric range check** — The answer must contain a number within an expected range. Catches agents that fabricate plausible-sounding but incorrect figures.

**Honeypot** — The question asks about something that does not exist. A real agent should say it does not know. A hallucinating agent will invent an answer. This is the most direct test for confident fabrication.

**Off-topic rejection** — The question is outside the agent's declared capability. The agent should decline. Catches agents that attempt to answer anything regardless of their stated scope.

**Crash/stability test** — The agent receives malformed or empty input. The agent should return any valid response without a server error. Catches unstable agents.

Layer 1 score is the ratio of tests passed to total tests. It is pure pass/fail logic — no statistical model required.

---

#### Layer 2 — Behavioral Pattern Analysis

**What it tests:** Does the agent behave like a real LLM, or like a fake?

This layer does not look at what the agent says. It looks at how the agent behaves — response timing, response length variation, vocabulary diversity, and consistency across repeated identical queries.

A real LLM produces variable response times, variable response lengths, diverse vocabulary, and similar but not identical responses to the same question asked multiple times. A fake agent — one returning cached responses, hardcoded templates, or random strings — fails one or more of these patterns in predictable ways.

Four metrics are combined into a weighted score:

**Response time variation** — Measures the coefficient of variation across response times. Very low variation is a strong signal of pre-stored answers.

**Response length variation** — Same method applied to word counts. A real LLM adapts length to question complexity. A template bot returns the same amount of text every time.

**Vocabulary richness** — Measures the ratio of unique words to total words across all responses. Low uniqueness means the agent is recycling the same phrases.

**Consistency check** — Sends the same query five times and measures average pairwise similarity across the responses. A real LLM produces similar but not identical answers. Similarity near 1.0 means cached responses. Similarity near 0.0 means the agent is incoherent.

This layer specifically catches the hardcoded response scenario that passes Layer 1 if the developer pre-stores correct answers for known test questions.

---

#### Layer 3 — Relevance and Depth

**What it tests:** Is the agent's response actually about the right topic, and does it cover that topic substantively?

This layer uses sentence embeddings to measure semantic similarity between the agent's response and a reference answer. It does not require exact wording — it measures whether the response means the same thing and covers the same points.

Three metrics are combined:

**Reference similarity** — Compares the agent's response to a reference answer using cosine similarity on embeddings. A correct but vague response scores low. A specific and accurate response scores high.

**Query-response relevance** — Compares the agent's response to the original query. Even without a reference answer, the response should be about the same topic as the question. Catches off-topic or generic deflections.

**Sub-topic coverage** — Splits the reference answer into chunks and checks how many the agent's response addresses. Catches responses that are technically on-topic but only cover part of the required information.

This layer catches the capability mismatch scenario — an agent claiming legal expertise that gives generic non-answers when asked legal questions will score low on reference similarity and coverage even if its responses sound plausible.

---

### Why All Three Layers Are Needed

No single layer is sufficient on its own.

A developer building a fake agent who knows about Layer 1 can pre-store correct answers for common test questions and pass functional checks. Layer 2 catches this because the behavioral patterns of returning pre-stored answers are detectable regardless of correctness.

A developer who spoofs timing and length variation to pass Layer 2 will still fail Layer 3 if their responses are shallow or off-topic.

A developer who passes all three layers at registration time but later degrades the agent is caught by periodic re-evaluation, which re-runs all three layers and compares scores against the registration baseline.

---

### Question Sets Must Be Genre-Specific

Generic test questions do not work. An agent claiming `stock-analysis` capability should be tested with stock analysis questions. An agent claiming `legal-research` should be tested with legal questions.

Each capability declared in an agent's Agent Card requires a corresponding question set. Question sets must be curated per genre and must include:

- Factual questions with known correct answers (for Layer 1)
- Questions of varying complexity to produce natural timing variation (for Layer 2)
- Questions with detailed reference answers for semantic comparison (for Layer 3)
- Honeypot questions specific to the domain (a fake stock ticker, a fabricated legal case citation)
- Off-topic questions that fall outside the declared capability

This is a platform responsibility. Zynd must maintain and expand genre-specific question sets as new capability types are registered on the network. An agent declaring a capability with no corresponding question set cannot be fully evaluated and should be marked accordingly in search results.

---

## How This Integrates With Existing Zynd Architecture

This layer sits above Layer 1 (identity and build integrity) and feeds directly into the trust and discovery systems.

**At registration:** After build attestation passes, behavioral evaluation runs automatically. The agent is not visible in search results until evaluation completes and a minimum score threshold is met.

**At discovery:** Search results include an evaluation score alongside the existing integrity level. Consumers can filter by minimum score. The score reflects the most recent evaluation run, not just the registration baseline.

**Continuously:** A scheduled job re-evaluates live agents periodically. If scores drop significantly from the registration baseline, the agent is flagged. A large drop triggers immediate hiding from search results pending investigation. This is the runtime equivalent of the heartbeat drift detection in Component 1.2 — instead of checking code hashes, it checks behavioral scores.

**Score visibility:** The Agent Card `trust` section should surface evaluation scores per capability, the timestamp of the last evaluation run, and whether the agent is currently passing or flagged.

---

## Honest Limitations

What this does not solve:

- **Does not confirm which LLM is running.** Behavioral patterns are consistent with real LLM usage but cannot cryptographically prove which model or provider is involved. A sophisticated fake that routes through a real LLM but with a manipulated system prompt could pass all three layers.
- **Question sets can be gamed if exposed.** If test questions are public, a developer can hardcode correct answers for them specifically. Question sets should be treated as semi-private, rotated periodically, and supplemented with random variation.
- **Evaluation cost scales with registered agents.** Running three layers across hundreds of agents on a schedule requires compute budget. Evaluation frequency should be risk-weighted — new agents and flagged agents run more frequently than stable high-scoring agents.
- **Genre coverage is incomplete at launch.** Question sets must be built per capability type. Until a question set exists for a given capability, that capability cannot be fully evaluated. This is a known gap with a known fix path.

These are acceptable tradeoffs for a practical first implementation. The behavioral approach catches the majority of bad actors and all unsophisticated fakes. Sophisticated adversarial attacks are addressed by combining this layer with the build integrity attestation already designed in Component 1.2.

---

## Relationship to Component 1.2 (Build Integrity)

These two components are complementary and neither is sufficient alone.

| | Component 1.2 (Build Integrity) | Component 2.1 (Behavioral Evaluation) |
|---|---|---|
| What it verifies | Code has not changed since build | Agent behavior matches capability claims |
| When it runs | At build time, continuously via heartbeat | At registration, periodically while live |
| What it catches | Binary tampering, code substitution | Fake agents, hallucination, capability mismatch, silent degradation |
| What it misses | Runtime configuration, system prompt changes | Which specific LLM is running |

An agent can pass Component 1.2 and fail Component 2.1 — correct binary, bad behavior. An agent can theoretically pass Component 2.1 and fail Component 1.2 — good behavior, tampered binary. Both checks are required for a complete trust signal.

The combined output of both components should produce a single unified trust level surfaced in search results and the Agent Card.