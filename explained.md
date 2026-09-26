# Ganji — Documentation Index

This is the living reference for the Ganji backend. Each section is now a
separate file in [`docs/`](./docs/) for easier navigation.

---

## Quick Links

| Document | Description |
|---|---|
| [`ARCHITECTURE.md`](./docs/ARCHITECTURE.md) | Module layout, layering, key files table |
| [`DEAL_LIFECYCLE.md`](./docs/DEAL_LIFECYCLE.md) | State machine, transitions, money-move rules |
| [`MONEY_FLOW.md`](./docs/MONEY_FLOW.md) | Hold invoices, LNbits endpoints, webhook/poll/approve/dispute/arbitration/sweep |
| [`SHARE_LINK.md`](./docs/SHARE_LINK.md) | Share token design, rotation, public endpoint |
| [`IDENTITY_AUTH.md`](./docs/IDENTITY_AUTH.md) | Freelancer vs client vs operator, email matching, endpoint access matrix |
| [`TESTING.md`](./docs/TESTING.md) | Test commands, what each test file covers |
| [`ENVIRONMENT.md`](./docs/ENVIRONMENT.md) | All environment variables with defaults |
| [`KNOWN_ISSUES.md`](./docs/KNOWN_ISSUES.md) | Fixed + remaining bugs, not-built items |
| [`OTS_ANCHORING.md`](./OTS_ANCHORING.md) | OpenTimestamps anchor/verify pipeline (plain English + jargon) |

---

## What Ganji Is (One-Page Summary)

Ganji ("money" in Sheng) is a **Bitcoin / Lightning escrow app for
freelancers**. A freelancer and a client transact over any channel (WhatsApp,
Telegram, X, email) — Ganji just provides the escrow, the payment link, and a
cryptographically anchored reputation layer (the **Live CV**).

The escrow is **network-as-escrow**: instead of holding sats in a hot wallet,
Ganji draws a Lightning **hold invoice** and the Lightning Network itself keeps
the funds until the client approves. Nothing is custodial.

```
freelancer creates deal   ──►  Ganji draws a hold invoice for amount_sats
                               (payment_hash = sha256(preimage Ganji holds))

  client opens share link ──►  sees title, amount, bolt11 invoice, status
  client pays invoice     ──►  sats HELD on the network (escrowed)

  freelancer submits work ──►  client views artifacts
  client approves         ──►  settle (reveal preimage) → funds to Ganji wallet
                               ──► pay freelancer's payee_invoice ──► released
  client disputes         ──►  deal freezes 'disputed', funds stay held
                               ──► arbiter: released (pay) or refunded
```

Every released deal stamps `verified_at` and its artifacts become hash-verified
lines on the freelancer's public **Live CV** (`GET /cv/:slug` + hash verification
endpoint).

---

## Quick Start

### Backend

```bash
cd backend
cp .env.example .env        # fill in DATABASE_URL, JWT secrets, LNBITS_* vars
go mod download
migrate -path migrations -database "$DATABASE_URL" up
go run ./cmd/api
```
Backend runs on `http://localhost:8080` by default.

### Frontend

```bash
cd frontend
cp .env.example .env.local   # set NEXT_PUBLIC_API_URL and NEXT_PUBLIC_WS_URL
npm install
npm run dev
```
Frontend runs on `http://localhost:3000`.

---

## Status

🚧 Pre-launch. See [`BACKEND_STATUS.md`](./BACKEND_STATUS.md) for what's built
and [`CONTRIBUTING.md`](./CONTRIBUTING.md) for workflow and team ownership.

---

## Related Files

| File | Purpose |
|---|---|
| [`API_REFERENCE.md`](./API_REFERENCE.md) | Every endpoint, request/response shapes, error codes |
| [`BACKEND_STATUS.md`](./BACKEND_STATUS.md) | Build status, what's done, what's not |
| [`CONTRIBUTING.md`](./CONTRIBUTING.md) | Team ownership, build order, git conventions |
| [`OTS_ANCHORING.md`](./OTS_ANCHORING.md) | OpenTimestamps pipeline breakdown |
| [`README.md`](./README.md) | Repo overview and quick start |