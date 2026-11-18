# Full LNbits Cycle Testing Guide

This guide walks through testing the complete Ganji escrow cycle: **CV → Deal → Invoice → Payment → Work → Approve → Payout → CV Anchor**.

## Prerequisites Checklist

- [ ] **Go 1.22+** installed
- [ ] **PostgreSQL 14+** (or Docker)
- [ ] **LNbits instance** with both keys:
  - Invoice Key (read-only) → `LNBITS_API_KEY`
  - Admin Key (money moves) → `LNBITS_ADMIN_KEY`
- [ ] **LND accepted-hold checker on the escrow node** (`polar-n3-Es` / Alice1/Es): set `LND_REST_URL`, `LND_REST_CERT_PATH`, `LND_REST_MACAROON_PATH`. Without it funding verification is blocked and `FundDeal` returns `503`. The escrow wallet is backed by Es; `polar-n3-Pay` is route-only Es → Fre if needed, not a second Ganji wallet. The Alice2/relay workflow was reverted.
- [ ] **ngrok** (for local webhook testing)
- [ ] **Linked Lightning wallet** (LNbits URL + key) for:
  - Generating the freelancer `payee_invoice` (payout destination, via `POST /deals/wallet/invoice`)
  - Paying the hold invoice as the client (via `POST /public/deals/:shareToken/fund`)
  - Wallet URLs are http/https host only (no userinfo/query/fragment, no allowlist); keys are transient and are never stored or logged.

---

## Option 1: Quick Test with LNbits Demo (Testnet/Mainnet)

### 1. Get LNbits Keys
1. Go to https://demo.lnbits.com
2. Create wallet → Settings → API Keys
3. Copy **Invoice key** → `LNBITS_API_KEY`
4. Copy **Admin key** → `LNBITS_ADMIN_KEY`

### 2. Configure Environment
```bash
make setup
# Edit backend/.env with your keys:
LNBITS_URL=https://demo.lnbits.com
LNBITS_API_KEY=your_invoice_key
LNBITS_ADMIN_KEY=your_admin_key
LNBITS_WEBHOOK_SECRET=random-secret-here
WEBHOOK_URL=https://your-ngrok-url/webhooks/lnbits  # Update after ngrok starts
LND_REST_URL=https://escrow-node:8080  # LND on polar-n3-Es; accepted-hold checker
LND_REST_CERT_PATH=/path/to/es/tls.cert
LND_REST_MACAROON_PATH=/path/to/es/admin.macaroon
DATABASE_URL=postgres://postgres:postgres@localhost:5432/ganji?sslmode=disable
JWT_SECRET=dev-secret-change-me
JWT_REFRESH_SECRET=dev-refresh-secret-change-me
```

### 3. Start Infrastructure
```bash
# Terminal 1: Database
make db-up

# Terminal 2: Backend
make run

# Terminal 3: ngrok (expose port 8080)
make ngrok

# Terminal 4: Update webhook URL in .env
make update-webhook-url
```

### 4. Configure LNbits Webhook
1. In LNbits wallet: Extensions → **Webhooks** → Enable
2. Add webhook:
   - URL: `https://your-ngrok-url/webhooks/lnbits`
   - Secret: same as `LNBITS_WEBHOOK_SECRET`

### 5. Run Full Cycle Test
```bash
make test-cycle
```

This interactive script will:
1. Register freelancer + client accounts
2. Create a deal (prompts for your freelancer `payee_invoice`, optionally generated via `POST /deals/wallet/invoice`)
3. Show the hold invoice (bolt11) to pay
4. Fund via `POST /public/deals/:shareToken/fund` with the client's linked wallet and poll until LND on Es confirms the accepted hold → `locked` ("Funds Held")
5. Submit work as freelancer (requires `locked` + artifact)
6. Approve as client (reveals R, settles + pays the freelancer directly)
7. Verify final state

---

## Option 2: Full Regtest (No Real Funds)

### 1. Start Bitcoin Core + LND + LNbits (Docker Compose)
Create `docker-compose.yml`:
```yaml
version: '3.8'
services:
  bitcoind:
    image: ruimarinho/bitcoin-core:26.0
    ports: ["18443:18443", "18444:18444"]
    volumes: [bitcoind-data:/bitcoin]
    command: -regtest -fallbackfee=0.0001 -server -rpcuser=user -rpcpassword=pass -rpcallowip=0.0.0.0/0

  lnd:
    image: lightninglabs/lnd:v0.18.0-beta
    ports: ["10009:10009", "8080:8080"]
    volumes: [lnd-data:/lnd, ./lnd.conf:/etc/lnd/lnd.conf]
    depends_on: [bitcoind]

  lnbits:
    image: lnbits/lnbits:latest
    ports: ["5000:5000"]
    environment:
      - LNBITS_NETWORK=regtest
      - LNBITS_BACKEND_WALLET_CLASS=LndRestWallet
      - LND_REST_ENDPOINT=http://lnd:8080
      - LND_REST_CERT=/lnd/tls.cert
      - LND_REST_MACAROON=/lnd/admin.macaroon
    volumes: [lnbits-data:/data]
    depends_on: [lnd]

volumes:
  bitcoind-data:
  lnd-data:
  lnbits-data:
```

Create `lnd.conf`:
```
[Application Options]
debuglevel=info
[Bitcoin]
bitcoin.regtest=1
[Bitcoind]
bitcoind.rpchost=bitcoind:18443
bitcoind.rpcuser=user
bitcoind.rpcpass=pass
bitcoind.zmqpubrawblock=tcp://bitcoind:28332
bitcoind.zmqpubrawtx=tcp://bitcoind:28333
```

Start:
```bash
docker-compose up -d
# Wait for all services healthy (check docker-compose logs -f)
# Fund wallet:
make lnbits-fund-regtest
```

### 2. Configure Environment
```bash
LNBITS_URL=http://localhost:5000
LNBITS_API_KEY=your_invoice_key_from_lnbits_ui
LNBITS_ADMIN_KEY=your_admin_key_from_lnbits_ui
LNBITS_WEBHOOK_SECRET=random-secret
WEBHOOK_URL=http://localhost:8080/webhooks/lnbits  # No ngrok needed on same host
LND_REST_URL=https://escrow-node:8080  # LND on polar-n3-Es; accepted-hold checker
LND_REST_CERT_PATH=/path/to/es/tls.cert
LND_REST_MACAROON_PATH=/path/to/es/admin.macaroon
```

### 3. Test
```bash
make db-up && make run
# In another terminal:
make test-cycle
```

---

## API Flow Reference

| Step | Endpoint | Auth | Description |
|------|----------|------|-------------|
| 1 | `POST /auth/signup` | — | Register freelancer |
| 2 | `POST /auth/login` | — | Get access_token |
| 3 | `POST /deals/wallet/invoice` | Freelancer | Optional: generate the payout `payee_invoice` from a linked wallet |
| 4 | `POST /deals` | Freelancer | Create deal with `payee_invoice` (with `milestones[]` for milestone mode) |
| 5 | `GET /public/deals/:shareToken` | — | Public deal page (client funds here) |
| 6 | `POST /public/deals/:shareToken/fund` | — (share token) | Client's linked wallet pays the hold; LND on Es verifies the accepted hold → `locked` (503 without checker) |
| 7 | `GET /deals/:id/payment` | Freelancer | Poll payment status |
| 8 | `POST /deals/:id/submit` | Freelancer | Submit work (requires `locked` + at least one artifact; milestone mode submits active milestone) |
| 9 | `POST /deals/:id/approve` | Client (email) | Approve → reveal R, settle hold + pay freelancer directly (milestone mode: release active milestone, fund next) |
| 10 | `GET /cv/:slug` | — | View freelancer CV (auto-anchored on release) |

Milestone E2E (not yet run against real LNbits/LND — see `MILESTONE_REDESIGN_NOTES.md`): fresh `000008` → create milestone deal → fund/pay M1 via linked wallet (locks on accepted hold) → submit → approve/release M1 (R revealed, freelancer paid) → fund/pay M2 → … plus reject → review → operator `ResolveMilestone` (release reveals R; refund cancels/waits expiry, no R) and pre/post-submission exit/refund. Public milestone actions: `fund` (with optional `milestone_id`), `accept-next`, `reject`, `exit`.

### Deal States
```
awaiting_payment → locked → work_submitted → released
                        ↓
                    disputed → (arbitration) → released | refunded
```
`locked` means the LND accepted hold was verified on Es — **not** settled. Work submission requires `locked`; `awaiting_payment → work_submitted` is invalid.

### Money Moves
| Action | LNbits Call | Key |
|--------|-------------|-----|
| Create hold invoice | `POST /api/v1/payments` (out:false) | Invoice key |
| Client pays hold (public fund) | `POST /api/v1/payments` (out:true, hold bolt11) on the client's linked wallet | Client wallet key (transient) |
| Verify accepted hold | LND REST on Es: matching hash/amount, `ACCEPTED`, `settled=false`, `settle_date=0` | LND macaroon (`LND_REST_*`) |
| Approve/release | `POST /api/v1/payments/settle` with R, then `POST /api/v1/payments` (out:true, payee bolt11) → pays Fre directly | **Admin key** |
| Arbiter refund | `POST /api/v1/payments/cancel` (no R; or wait for expiry) | **Admin key** |

Reject after `work_submitted` reveals no R and does not settle, cancel, or fail the HTLC.

---

## Troubleshooting

| Issue | Fix |
|-------|-----|
| `LNBITS_ADMIN_KEY` invalid | Must be **admin key**, not invoice key |
| Webhook not received | Check `WEBHOOK_URL` is public HTTPS; verify `LNBITS_WEBHOOK_SECRET` matches |
| Hold invoice not created | Verify `LNBITS_API_KEY` and `LNBITS_URL` reachable |
| Payout fails | Admin key needs wallet balance; fund LNbits wallet first |
| `context deadline exceeded` | Increase `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` or check LNbits connectivity |
| Deal stuck in `awaiting_payment` after funding | LNbits `paid=true` is not proof of funds. Check LND on Es for the matching accepted hold (`ACCEPTED`, not settled); ensure `LND_REST_*` is configured — without the checker `FundDeal` returns 503. Retry `POST /public/deals/:shareToken/fund` after the hold is accepted. |

---

## Automated Tests

Run unit tests (no external dependencies):
```bash
make backend-test-deals
# or, from backend/:
go test ./internal/deals/... ./internal/webhook/...
```

Tests cover:
- Create deal → hold invoice
- Accepted-hold lock, refusal of unaccepted/`paid=true`-only holds, checker requirement, and checker errors (`internal/deals/service_test.go`)
- Linked-wallet `FundDeal` and `GenerateInvoice` request shapes/keys (`internal/deals/service_test.go`)
- Submit work (requires `locked` + artifact)
- Approve (settle + payout + idempotent)
- Dispute + arbitration (release/refund)
- Sweep (hold expiry)
- Payee-invoice rotation
- Share link rotation
- Webhook accepted/rejected/unavailable (`internal/webhook/service_test.go`: locks only on accepted hold, rejects paid-but-unaccepted, missing checker → `ErrVerificationUnavailable`)
- Dedicated milestone tests (`internal/deals/milestone_service_test.go`): sequential funding, canonical release idempotency/crash-window/ambiguous payouts, reject/exit → dispute, operator release/refund, share-token auth, transitions

---

## CV Integration

When a deal reaches `released`:
1. CV entry is created for the freelancer
2. Entry is anchored to Bitcoin via OpenTimestamps
3. Verify at: `GET /cv/:slug/verify/:entryID`

The CV proves: **this work was paid for on Lightning at block height X**.

---

## Key Files

| File | Purpose |
|------|---------|
| `docs/LNBITS_SETUP.md` | LNbits configuration details |
| `docs/MONEY_FLOW.md` | Network-as-escrow mechanics |
| `docs/DEAL_LIFECYCLE.md` | State machine + transitions |
| `backend/internal/lnbits/client.go` | LNbits API client |
| `backend/internal/deals/service.go` | Core escrow logic |
| `backend/internal/deals/service_test.go` | Full cycle unit tests (accepted-hold, linked wallet) |
| `backend/internal/deals/milestone_service_test.go` | Dedicated milestone funding/release/resolve tests |
| `backend/internal/webhook/service_test.go` | Webhook accepted/rejected/unavailable tests |