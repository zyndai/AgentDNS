# ZyndAI Payment Abstraction Layer — Feature Idea

## Problem

ZyndAI's economic layer runs on x402 micropayments (USDC on Base). This is powerful for agent-to-agent commerce, but it forces users — who are AI-native developers, not crypto-native — to:

- Acquire USDC from an exchange
- Bridge to Base
- Manage wallet keys
- Understand gas and on-chain transactions

This kills the onboarding funnel. The crypto layer should be an implementation detail, not a user-facing concept.

---

## Core Idea

**Hide the entire crypto layer. Users see USD balances, top-ups, and withdrawals. The blockchain is invisible.**

---

## User Mental Model

```
User sees:                          What actually happens:
─────────────────────────────────   ──────────────────────────────────
"Add $20 to my account"         →   Fiat onramp → USDC deposited to
                                    user's master vault wallet

"Top up Agent X with $5"        →   Internal USDC transfer from vault
                                    wallet → agent's derived EVM wallet

"Agent X earned $3.20"          →   Agent received USDC via x402
                                    from other agents paying for its service

"Withdraw $8 to my card"        →   Offramp: USDC in vault → fiat to
                                    user's debit/credit card
```

---

## Architecture

### 1. Master Vault Wallet (per user)
- Auto-generated at account creation, derived from developer keypair
- Never exposed to the user — no addresses, no seed phrases, no keys
- Holds the user's USDC balance, shown as a plain USD number in the dashboard
- All onramp deposits land here

### 2. Agent Wallets (per agent)
- Already exists in current SDK — each agent has a derived EVM address
- User sees: "Agent balance: $3.20" — not a wallet address
- Funded by internal transfer from the master vault (standard USDC tx, invisible to user)
- Earns USDC via x402 when other agents call its endpoints

### 3. Onramp (USD → agent balance)
- User clicks "Add Funds" in dashboard
- Enters a dollar amount, pays with card or bank transfer
- Third-party onramp provider (e.g. Coinbase CDP) handles KYC and payment
- USDC lands in master vault wallet
- Dashboard shows updated USD balance — no crypto terminology

### 4. Agent Top-Up (vault → agent)
- User selects an agent, enters an amount (shown in USD)
- Dashboard executes a USDC transfer from vault wallet to agent wallet in the background
- User sees: "Agent X funded with $5.00" — no tx hash, no gas, no confirmation wait shown

### 5. Offramp (agent earnings → card)
- Agent earns USDC by serving x402-protected endpoints
- Earnings accumulate and are shown as USD in the agent card
- User clicks "Withdraw" → enters card/bank details → third-party offramp processes USDC → fiat
- User sees: "Withdrawal of $8.00 initiated — arrives in 1–3 days"

---

## What the User Never Sees

- Wallet addresses
- Private keys or seed phrases
- USDC, Base, or any chain name
- Gas fees (absorbed or charged as a flat service fee)
- Transaction hashes or confirmations
- The word "crypto" anywhere in the flow

---

## Dashboard UI Changes Needed

| Screen | Change |
|---|---|
| **Dashboard home** | Add "Balance" card showing total USD across vault + all agents |
| **Agent detail** | Add "Balance: $X.XX" + "Top Up" + "Withdraw" buttons |
| **Add Funds flow** | New modal — dollar amount input → onramp widget embed |
| **Withdraw flow** | New modal — select agent or vault → offramp widget embed |
| **Transaction history** | Plain list: "Topped up Agent X — $5.00", "Agent X earned — $0.40", "Withdrew — $8.00" |

---

## Backend Changes Needed

- **Vault wallet manager** — create, store, and sign transactions on behalf of users without exposing keys
- **Balance aggregator** — poll vault + all agent wallet balances, convert to USD at USDC:USD rate, cache for display
- **Internal transfer service** — execute vault → agent USDC transfers triggered by top-up actions
- **Earnings listener** — watch agent wallets for incoming x402 payments, update dashboard balance in real time (or near-real time via polling)
- **Onramp webhook handler** — receive confirmation from onramp provider, credit vault wallet display balance
- **Offramp initiation** — trigger offramp provider with USDC amount and user payout details

---

## Key Design Principles

1. **USD everywhere in the UI.** USDC is only ever mentioned in legal/terms copy.
2. **No wallet management UX.** Users never need to back up keys or approve transactions.
3. **Flat fee over gas.** Charge a small flat service fee per top-up/withdrawal; absorb gas internally. No variable gas confusion.
4. **Progressive disclosure.** Power users who want to see the underlying wallet address or tx hash can find it in an "Advanced" section — but it's never surfaced by default.
5. **Earning is passive.** Agents earn automatically via x402. The user just sees their balance go up.

---

## Provider Evaluation

| Provider | US Market | India / UPI | Business KYC Required | Status |
|---|---|---|---|---|
| **Coinbase CDP Onramp** | ✅ Strong | ❌ No | ✅ Yes (business reg + KYC) | ❌ Application rejected |
| **Crossmint** | ✅ Strong | ❌ No | ❌ Not required | 🔄 Actively trialling |
| **Transak** | ✅ Strong | ✅ Strong (UPI support) | ✅ Yes (KYC needed) | 📝 Application in progress |

### Recommendation

**Short term — use Crossmint** to unblock US users immediately. No business KYC barrier, can be integrated now, covers the primary market.

**Medium term — add Transak** once their KYC is approved. Transak is the only provider here with strong UPI support, which unlocks the Indian developer market — a significant part of ZyndAI's target audience. Running both providers in parallel (Crossmint for US/global card payments, Transak for India/UPI) gives the best coverage with no single point of failure.

**Coinbase CDP** — revisit after entity is better established. The rejection likely came from early-stage registration or incomplete application. Worth reapplying once Transak or Crossmint onramp is live and ZyndAI has transaction volume to show.

---

## Open Questions

- Minimum top-up amount? (Suggest $5 to cover minimum x402 usage)
- How to handle USDC:USD display rate? (Treat 1 USDC = $1.00 — stablecoin, no conversion needed)
- Vault key custody model — who signs internal transfers? (Server-side signing vs. MPC)
- Regulatory: does aggregating user funds in a vault require a money transmitter licence?
- For Crossmint: confirm USDC on Base is supported as the target asset (not just Ethereum mainnet)
- For Transak: confirm offramp (USDC → INR bank transfer / UPI) is available, not just onramp
