# Polar + LNbits Development Setup

This guide explains how our local Lightning Network test environment works: **Polar** (our Bitcoin/Lightning sandbox) + **LNbits** (our payment API) + **Ganji** (our app).

---

## Table of Contents

1. [The Big Picture](#the-big-picture)
2. [System Architecture](#system-architecture)
3. [What Is Polar?](#what-is-polar)
4. [What Is LNbits?](#what-is-lnbits)
5. [Docker Containers & Networks](#docker-containers--networks)
6. [How Hold Invoices Work (Escrow)](#how-hold-invoices-work-escrow)
7. [Step-by-Step Setup](#step-by-step-setup)
8. [Common Commands](#common-commands)
9. [Troubleshooting](#troubleshooting)
10. [File Reference](#file-reference)

---

## The Big Picture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         YOUR DEVELOPMENT MACHINE                      │
│                                                                       │
│  ┌──────────────┐    ┌──────────────┐    ┌───────────────────────┐  │
│  │   Browser    │    │   Backend    │    │    LNbits API         │  │
│  │  (Next.js)   │───▶│   (Go API)   │───▶│   (port 5000)         │  │
│  │  port 3000   │    │  port 8080   │    │                       │  │
│  └──────────────┘    └──────────────┘    └──────────┬────────────┘  │
│                                                       │               │
│         ┌─────────────────────────────────────────────┘               │
│         │  LNbits uses LND REST Wallet backend                       │
│         ▼                                                             │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │              POLAR NETWORK (Docker)                           │    │
│  │  ┌────────────────────────────────────────────────────────┐  │    │
│  │  │  bitcoind (regtest)  —  the fake Bitcoin blockchain     │  │    │
│  │  │  port 18446 (RPC)                                       │  │    │
│  │  └───────────────────────┬────────────────────────────────┘  │    │
│  │                          │ blockchain data                   │    │
│  │          ┌───────────────┴───────────────┐                   │    │
│  │          ▼                               ▼                   │    │
│  │  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐    │    │
│  │  │ LND "Cl"     │──▶│ LND "Es"     │──▶│ LND "Pay"    │    │    │
│  │  │ port 8083    │   │ port 8082    │   │ port 8084    │    │    │
│  │  └──────────────┘   └──────┬───────┘   └──────┬───────┘    │    │
│  │                           │                  │             │    │
│  │                           │                  ▼             │    │
│  │                           │          ┌──────────────┐     │    │
│  │                           └─────────▶│ LND "Fre"    │     │    │
│  │                                      │ port 8085    │     │    │
│  │                                      └──────────────┘     │    │
│  └──────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

**The flow:** User pays a Lightning invoice → LNbits creates it via Es LND → the sats are held in escrow → Ganji releases or refunds them via LNbits API calls.

---

## System Architecture

### Why This Architecture?

Ganji is a freelancer escrow platform. When a client pays for work, the money must be **held** until the work is approved, then **released** to the freelancer (or **refunded** if something goes wrong). This is possible because of **hold invoices** — a special Lightning feature that LNbits exposes through LND. Money moves go through LNbits; accepted-hold detection also reads Es's LND REST API directly.

```
Ganji Backend (Go)                 LNbits                    LND (Es)
     │                               │                            │
     │ POST /api/v1/payments         │                            │
     │ (with preimage_hash)          │ addholdinvoice             │
     │──────────────────────────────▶│───────────────────────────▶│
     │                               │                            │
     │ ◀─────────────────────────────│◀───────────────────────────│
     │   bolt11 invoice              │   invoice                  │
     │                               │                            │
     │ Client pays invoice ─────────────────────────────────────▶│ (held!)
     │                               │                            │
     │ POST /api/v1/payments/settle  │ settle via preimage        │
     │──────────────────────────────▶│───────────────────────────▶│
     │                               │                            │
     │                    freelancer gets paid                    │
```

### LND hold semantics: how `awaiting_payment → locked` is detected

With our **LND-backed LNbits**, a paid hold invoice does **not** become `paid: true`. LNbits reports:

```json
{ "paid": false, "status": "pending", "details": { "extra": { "hold_invoice": true } } }
```

until Ganji calls `/api/v1/payments/settle` with the preimage. The funds are already locked in an HTLC at Es (`state: ACCEPTED` in LND), so LNbits/CLN-style `paid` detection alone cannot see it.

Consequences:
- The LNbits webhook does not fire on payment acceptance; there is nothing to deliver yet.
- Ganji checks Es directly through `backend/internal/lnd/client.go` when these are set in `backend/.env`: `LND_REST_URL=https://localhost:8082`, `LND_REST_CERT_PATH` pointing at Es's `tls.cert`, and `LND_REST_MACAROON_PATH` pointing at Es's `admin.macaroon`.
- A matched payment hash, `state: ACCEPTED`, `settled: false`, `settle_date: 0`, and `amt_paid_sat >= deal.AmountSats` transition the deal to `locked`.
- Without those LND variables set, LND-backed deals intentionally remain `awaiting_payment`; this is a local config gap, not proof of funding.

Never force `locked` from a generic pending hold. Verify the hold at the LND layer, as shown in `DEMO_RUNBOOK.md` Part 4.

### Network Topology

| Network | Purpose | Who's on it |
|---------|---------|-------------|
| `polar-network-3_default` | Docker network Polar created for its containers | `polar-n3-backend1`, `polar-n3-Cl`, `polar-n3-Es`, `polar-n3-Pay`, `polar-n3-Fre`, **our `lnbits`** |
| Host (localhost) | Where our app runs | Frontend (3000), Backend (8080), browser |
| LNbits REST API | Bridge between our backend and Lightning | Backend → `localhost:5000` |

**Important:** LNbits runs *inside* Polar's Docker network so it can reach `polar-n3-Es:8080` using the container name as a hostname. From the host machine, we talk to LNbits at `localhost:5000`.

---

## What Is Polar?

**Polar** is a desktop app that runs a small Bitcoin + Lightning Network on your machine. It's a sandbox — the Bitcoin is fake (regtest), so you can mine blocks instantly and create as many sats as you need.

- **Where its config lives:** `~/.polar/networks/3/docker-compose.yml`
- **Where its data lives:** `~/.polar/networks/3/volumes/`

Polar created a network with five containers:

| Container | Role | Host Ports |
|-----------|------|------------|
| `polar-n3-backend1` | bitcoind (fake Bitcoin blockchain) | 18446 (RPC), 19444 (P2P) |
| `polar-n3-Cl` | LND node — client/demo payer | 8083 (REST), 10003 (gRPC), 9737 (P2P) |
| `polar-n3-Es` | LND node — **our escrow wallet** | 8082 (REST), 10002 (gRPC), 9736 (P2P) |
| `polar-n3-Pay` | LND node — payout relay | 8084 (REST), 10004 (gRPC), 9738 (P2P) |
| `polar-n3-Fre` | LND node — freelancer receiving node | 8085 (REST), 10005 (gRPC), 9739 (P2P) |

### Why "Cl", "Es", "Pay", and "Fre"?

These are role names, not tutorial placeholders:
- **Cl** = client/demo payer
- **Es** = escrow node backing LNbits; holds client funds until release or refund
- **Pay** = payout relay used during final release
- **Fre** = freelancer receiving node

---

## What Is LNbits?

**LNbits** is a Lightning wallet/server with a REST API. Our Go backend doesn't talk to LND directly — it talks to LNbits, and LNbits forwards requests to Es's LND node.

- **Runs on:** `http://localhost:5000`
- **API keys** (in `backend/.env`): `LNBITS_API_KEY` (create invoices) and `LNBITS_ADMIN_KEY` (settle/cancel holds)
- **Wallet data:** stored in Docker volume `ganji_lnbits-data`

### Why not talk to LND directly?

LNbits gives us:
- A simple HTTP API (`POST /api/v1/payments`, `/settle`, `/cancel`)
- Webhook support (notifies our backend when a payment arrives)
- User-facing wallet UI for debugging (visit `http://localhost:5000`)

---

## Docker Containers & Networks

### Starting the Stack

**1. Start Polar's containers** (from `~/.polar/networks/3/docker-compose.yml`):

```bash
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d
```

**2. Start LNbits** (wired to Polar's Es):

```bash
docker compose -f lnbits-docker-compose.yml up -d
```

Our `lnbits-docker-compose.yml` joins Polar's network using an **external network**:

```yaml
networks:
  polar-network:
    name: polar-network-3_default
    external: true   # ← Polar created it; we just join it
```

This is why LNbits can reach `https://polar-n3-Es:8080` — Docker's DNS resolves the container name to its IP within the network.

### TLS Certificates

LND REST API uses HTTPS with a self-signed certificate. We mount Polar's cert and macaroon into LNbits so it can authenticate:

```yaml
volumes:
  - /home/amonoff/.polar/networks/3/volumes/lnd/Es/tls.cert:/certs/tls.cert:ro
  - /home/amonoff/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon:/certs/admin.macaroon:ro
```

The cert includes `DNS:polar-n3-Es` in its SANs, so TLS verification succeeds.

---

## How Hold Invoices Work (Escrow)

This is the **core feature** Ganji depends on. A hold invoice is a BOLT11 Lightning invoice where payment is **accepted but not settled** until the receiver reveals a secret (the preimage).

### The Lifecycle

```
1. Ganji generates a random preimage (secret) and computes SHA256(preimage) = payment_hash
2. Ganji asks LNbits: "create hold invoice for this payment_hash"
3. Client pays the invoice → sats are LOCKED at LND (not yet settled)
4. Freelancer does the work
5a. Client approves → Ganji calls /settle with preimage → sats flow to escrow wallet
5b. Client disputes → Ganji calls /cancel with payment_hash → sats returned to client
```

### Backend Code Reference

| Function | File | What it does |
|----------|------|--------------|
| `CreateHoldInvoice` | `backend/internal/lnbits/client.go:131` | Creates the held payment via LNbits |
| `SettleHold` | `backend/internal/lnbits/client.go:159` | Releases escrow (reveals preimage) |
| `CancelHold` | `backend/internal/lnbits/client.go:175` | Refunds the client |

### Why Phoenixd Won't Work

Phoenixd (ACINQ's wallet) settles all invoices automatically — there's no "hold" state and no settle/cancel API. Ganji's escrow model requires hold invoices, so we **must** use LND (via LNbits' `LndRestWallet` backend).

---

## Step-by-Step Setup

### Fresh Setup (from scratch)

**1. Start Polar network:**
```bash
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d
```

**2. Verify Es is healthy:**
```bash
docker ps | grep polar
# All five Polar containers should say "Up"
```

**3. Mine a fresh block only if needed** (LND won't sync if the chain tip is older than ~1 hour). Polar GUI actions such as `Deposit` and `Open Channel` mine automatically; skip this for a GUI setup unless `synced_to_chain: false`:
```bash
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 1
```

**4. Verify Es is synced:**
```bash
docker exec polar-n3-Es lncli \
  --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  getinfo | grep synced
```
Expected: `"synced_to_chain": true`

**5. Start LNbits:**
```bash
docker compose -f lnbits-docker-compose.yml up -d
```

**6. Start our app:**
```bash
# Terminal 1 — Backend
cd backend && go run ./cmd/api

# Terminal 2 — Frontend
cd frontend && npm run dev
```

**7. Test:** Sign up at `http://localhost:3000/signup`, then create a deal.

---

## Common Commands

### Bitcoin (regtest)

```bash
# Check blockchain info
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 getblockchaininfo

# Mine blocks (note: -generate flag in Bitcoin Core 30+, not generate RPC)
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 10

# Get a fresh address
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 getnewaddress
```

### LND (Es)

```bash
# Shortcut — set this in your shell to avoid typing paths
LND="lncli --network=regtest --tlscertpath=/home/lnd/.lnd/tls.cert --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon"

docker exec polar-n3-Es sh -c "$LND getinfo"           # Node status
docker exec polar-n3-Es sh -c "$LND walletbalance"     # Balance in sats
docker exec polar-n3-Es sh -c "$LND listinvoices"      # All invoices
docker exec polar-n3-Es sh -c "$LND lookupinvoice HASH" # Specific hold
```

### LNbits

```bash
docker compose -f lnbits-docker-compose.yml logs -f      # Watch logs
docker compose -f lnbits-docker-compose.yml restart      # Restart
docker compose -f lnbits-docker-compose.yml down         # Stop (keeps data)
```

---

## Troubleshooting

### LNbits says "This backend does nothing... VoidWallet"

**Cause:** LNbits couldn't initialize `LndRestWallet`, so it silently fell back to a do-nothing wallet. Check the logs:
```bash
docker logs ganji-lnbits-1 2>&1 | grep -iE "error initializing|void"
```

**Common cause:** The DB (`system_settings`) has stale hardcoded LND paths that **override environment variables**. Check:
```bash
docker run --rm -v ganji_lnbits-data:/data python:3-alpine python3 -c "
import sqlite3
c=sqlite3.connect('/data/database.sqlite3')
for r in c.execute(\"SELECT id,value FROM system_settings WHERE id LIKE '%lnd%'\"): print(r)
"
```

**Fix:** Update the DB settings (note: values are JSON-encoded strings, so use `json.dumps`):
```bash
docker run --rm -v ganji_lnbits-data:/data python:3-alpine python3 -c "
import sqlite3,json
c=sqlite3.connect('/data/database.sqlite3')
updates = {
  'lnd_rest_endpoint': json.dumps('https://polar-n3-Es:8080'),
  'lnd_rest_cert': json.dumps('/certs/tls.cert'),
  'lnd_rest_macaroon': json.dumps('/certs/admin.macaroon'),
}
for k,v in updates.items():
    c.execute('UPDATE system_settings SET value=? WHERE id=?', (v,k))
c.commit()
"
docker restart ganji-lnbits-1
```
Expected log: `✔️ Backend LndRestWallet connected and with a balance of ~9991530000 msat`

### LNbits redirects all API calls to `/first_install`

**Cause:** The superuser account has `extra.provider == "env"` in the DB, which makes LNbits v1.7.1 think setup isn't complete.

**Fix:**
```bash
docker run --rm -v ganji_lnbits-data:/data python:3-alpine python3 -c "
import sqlite3,json
c=sqlite3.connect('/data/database.sqlite3')
row = c.execute('SELECT extra FROM accounts LIMIT 1').fetchone()
extra = json.loads(row[0])
extra['provider'] = 'lnbits'
c.execute('UPDATE accounts SET extra=? WHERE id=(SELECT id FROM accounts LIMIT 1)', (json.dumps(extra),))
c.execute(\"INSERT OR REPLACE INTO system_settings (id,value,tag) VALUES ('first_install','false','core')\")
c.commit()
"
docker restart ganji-lnbits-1
```

### Wallet API says "Wallet not found" despite correct keys

**Cause 1:** LNbits writes to `/app/data/database.sqlite3` by default, not `/data`. Our compose sets `LNBITS_DATA_FOLDER=/data` to fix this.

**Cause 2:** The wallet's account has `activated = false`, or the API key doesn't match `adminkey`/`inkey` exactly.

**Check:**
```bash
docker run --rm -v ganji_lnbits-data:/data python:3-alpine python3 -c "
import sqlite3
c=sqlite3.connect('/data/database.sqlite3')
print(c.execute('SELECT adminkey,inkey FROM wallets').fetchone())
"
```

### "Invoice (or Admin) key required" when creating invoices

**Cause:** In LNbits v1.7.1, `POST /api/v1/payments` defaults `out: true` (pay invoice). To **create** an invoice, you must send `"out": false`.

The Ganji backend already does this correctly (`Out: false` in `backend/internal/lnbits/models.go:8`). For manual curl testing:
```bash
curl -s -X POST -H "X-Api-Key: $API_KEY" -H "Content-Type: application/json" \
  -d '{"amount":1000,"unit":"sat","memo":"test","out":false}' \
  http://localhost:5000/api/v1/payments
```

### LND says `synced_to_chain: false` forever

**Cause:** The regtest chain tip is old (bitcoin-cli `getblockchaininfo` shows a timestamp from days ago). LND won't sync to a stale tip.

**Fix:** For CLI setup or a stale chain, mine one fresh block (Polar GUI actions normally auto-mine):
```bash
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 1
```
Wait ~5-10 seconds, then check `getinfo` again.

### `polar-n3-Es` is crash-looping: "parse address Es failed"

**Cause:** The container wasn't attached to Polar's Docker network (happens if Polar was closed and containers were orphaned).

**Fix:** Recreate it:
```bash
docker rm -f polar-n3-Es
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d Es
```

### LNbits returns 520 "unexpected backend error" on invoice creation

**Cause:** LND isn't synced, has no funds, or the TLS cert/macaroon paths are wrong.

**Check:**
1. LND synced? `getinfo` → `synced_to_chain: true`
2. LND funded? `walletbalance` → `total_balance > 0`
3. LNbits logs: `docker compose -f lnbits-docker-compose.yml logs lnbits`

### `generate` command not found in bitcoind

**Cause:** Bitcoin Core 30+ replaced the `generate` RPC with a CLI `-generate` flag.

**Fix:** Use `-generate N` instead of `generate N` (see commands above).

---

## File Reference

| File | Purpose |
|------|---------|
| `~/.polar/networks/3/docker-compose.yml` | Polar's own config (bitcoind, Cl, Es, Pay, Fre) |
| `~/.polar/networks/3/volumes/lnd/Es/tls.cert` | Es's TLS certificate |
| `~/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon` | Es's admin macaroon |
| `lnbits-docker-compose.yml` | Our LNbits container setup (wired to Polar) |
| `backend/.env` | LNbits URL + API keys, hold invoice settings |
| `backend/internal/lnbits/client.go` | Hold invoice API calls |
| `backend/internal/lnbits/models.go` | `CreateHoldInvoiceRequest` with `Out: false` |
| `frontend/src/app/signup/page.tsx` | Signup form (with password eye toggle) |
| `frontend/src/app/login/page.tsx` | Login form (with password eye toggle) |

---

## Quick Reference Card

```
Polar network:          ~/.polar/networks/3/docker-compose.yml
LNbits compose:         lnbits-docker-compose.yml (this repo)
Backend LNbits URL:     http://localhost:5000
LNbits in Docker:       polar-network-3_default network
Escrow node:            polar-n3-Es (LND REST :8080 in network, :8082 on host)
Bitcoin RPC:            polar-n3-backend1:18443 (host :18446)
Wallet balance:         check `walletbalance` on polar-n3-Es
Hold invoices:          Required for escrow — Phoenixd doesn't support them

LNbits v1.7.1 gotchas:
  - DB settings override env vars (use json.dumps for values)
  - first_install redirect caused by account.extra.provider == "env"
  - Data folder defaults to /app/data (we set LNBITS_DATA_FOLDER=/data)
  - POST /payments defaults out:true — must send out:false to create invoices
```
