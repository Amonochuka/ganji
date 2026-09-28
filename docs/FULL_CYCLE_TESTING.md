# Full LNbits Cycle Testing Guide

This guide walks through testing the complete Ganji escrow cycle: **CV → Deal → Invoice → Payment → Work → Approve → Payout → CV Anchor**.

## Prerequisites Checklist

- [ ] **Go 1.22+** installed
- [ ] **PostgreSQL 14+** (or Docker)
- [ ] **LNbits instance** with both keys:
  - Invoice Key (read-only) → `LNBITS_API_KEY`
  - Admin Key (money moves) → `LNBITS_ADMIN_KEY`
- [ ] **ngrok** (for local webhook testing)
- [ ] **Lightning wallet** (Phoenix, Breez, Wallet of Satoshi, BlueWallet) for:
  - Generating freelancer `payee_invoice` (payout destination)
  - Paying the hold invoice as the client

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
2. Create a deal (prompts for your freelancer `payee_invoice`)
3. Show the hold invoice (bolt11) to pay
4. Poll payment status after you pay
5. Submit work as freelancer
6. Approve as client (triggers settle + payout)
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
| 1 | `POST /auth/register` | — | Register freelancer |
| 2 | `POST /auth/login` | — | Get access_token |
| 3 | `POST /deals` | Freelancer | Create deal with `payee_invoice` |
| 4 | `GET /public/deals/:shareToken` | — | Public deal page (client pays here) |
| 5 | `GET /deals/:id/payment` | Freelancer | Poll payment status |
| 6 | `POST /deals/:id/submit` | Freelancer | Submit work |
| 7 | `POST /deals/:id/approve` | Client (email) | Approve → settle hold + payout |
| 8 | `GET /cv/:slug` | — | View freelancer CV (auto-anchored on release) |

### Deal States
```
awaiting_payment → locked → work_submitted → released
                        ↓
                    disputed → (arbitration) → released | refunded
```

### Money Moves
| Action | LNbits Call | Key |
|--------|-------------|-----|
| Create hold invoice | `POST /api/v1/payments` (out:false) | Invoice key |
| Settle hold | `POST /api/v1/payments/settle` | **Admin key** |
| Cancel hold (refund) | `POST /api/v1/payments/cancel` | **Admin key** |
| Pay freelancer | `POST /api/v1/payments` (out:true, bolt11) | **Admin key** |

---

## Troubleshooting

| Issue | Fix |
|-------|-----|
| `LNBITS_ADMIN_KEY` invalid | Must be **admin key**, not invoice key |
| Webhook not received | Check `WEBHOOK_URL` is public HTTPS; verify `LNBITS_WEBHOOK_SECRET` matches |
| Hold invoice not created | Verify `LNBITS_API_KEY` and `LNBITS_URL` reachable |
| Payout fails | Admin key needs wallet balance; fund LNbits wallet first |
| `context deadline exceeded` | Increase `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` or check LNbits connectivity |
| Deal stuck in `awaiting_payment` | Run sweep manually: `curl -X POST http://localhost:8080/deals/:id/reconcile -H "Authorization: Bearer <operator_token>" -d '{"action":"confirm_payout"}'` |

---

## Automated Tests

Run unit tests (no external dependencies):
```bash
make backend-test-deals
```

Tests cover:
- Create deal → hold invoice
- Submit work
- Approve (settle + payout + idempotent)
- Dispute + arbitration (release/refund)
- Sweep (hold expiry)
- Payee invoice rotation
- Share link rotation
- Payment status reconciliation

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
| `backend/internal/deals/service_test.go` | Full cycle unit tests |