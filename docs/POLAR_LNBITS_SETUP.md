# Polar + LNbits Development Setup

This guide explains how our local Lightning Network test environment works: **Polar** (our Bitcoin/Lightning sandbox) + **LNbits** (our payment API) + **Ganji** (our app).

> **Current escrow model.** Holds live on `polar-n3-Es` (Alice1/Es); the client is Bob/`polar-n3-Cl`, the freelancer is `polar-n3-Fre`. `polar-n3-Pay` is route-only Es → Fre if needed, not a second Ganji wallet. The Alice2/relay workflow was reverted and is not current. `locked` (shown as **Funds Held**) requires accepted-hold verification on Es — LNbits `paid=true` alone is not proof of funds.

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
│  └──────────────┘    └──────┬───────┘    └──────────┬────────────┘  │
│                             │ LND REST checker       │ LND REST backend│
│                             │ (accepted hold)        │ (hold/settle/pay)│
└─────────────────────────────┼────────────────────────┼─────────────────┘
                              ▼                        ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      POLAR NETWORK (Docker)                          │
│  bitcoind (regtest) — the fake Bitcoin blockchain                    │
│                                                                       │
│  polar-n3-Es (Alice1/Es) — escrow LND node (REST :8080 in network)    │
│  polar-n3-Cl (Bob) — client wallet    polar-n3-Fre — freelancer      │
│  polar-n3-Pay — route-only Es → Fre if needed, not a Ganji wallet    │
└─────────────────────────────────────────────────────────────────────┘
```

**The flow:** Client funds the hold from the linked wallet (Bob/`polar-n3-Cl`) → the backend pays the Es hold invoice transiently with that wallet's admin key → LND on Es reports an accepted hold → Ganji locks the deal (**Funds Held**). On approve, Ganji reveals `R` on Es and pays `polar-n3-Fre` directly.

---

## System Architecture

### Why This Architecture?

Ganji is a freelancer escrow platform. When a client funds work, the money must be **held** until the work is approved, then **released** to the freelancer (or **refunded** if something goes wrong). This is possible because of **hold invoices** — a special Lightning feature that LNbits exposes through LND.

```
Ganji Backend (Go)                 LNbits                LND Es (escrow)
     │                               │                            │
     │ POST /api/v1/payments         │                            │
     │ (with payment_hash)           │ addholdinvoice             │
     │──────────────────────────────▶│───────────────────────────▶│
     │                               │                            │
     │ LND REST lookup on Es         │                            │
     │ (accepted-hold checker)       │                            │
     │───────────────────────────────────────────────────────────▶│
     │                               │                            │
     │ Client wallet (Cl/Bob) pays hold invoice ─────────────────▶│ (ACCEPTED)
     │                               │                            │
     │ verify hash+amount+ACCEPTED+unsettled → locked (Funds Held) │
     │                               │                            │
     │ approve: SettleHold(R) + PayInvoice(Fre)                   │
     │──────────────────────────────▶│───────────────────────────▶│
     │                               │                            │
     │                    freelancer (Fre) gets paid              │
```

### LND hold semantics: why accepted-hold verification is required

With our **LND-backed LNbits**, a funded hold invoice does **not** become `paid: true`. LNbits reports:

```json
{ "paid": false, "status": "pending", "details": { "extra": { "hold_invoice": true } } }
```

until Ganji settles it with `R`. The funds are already committed in an HTLC on Es, but LNbits `paid` detection cannot see it.

Consequences:
- The LNbits webhook is best-effort and is not proof that funds are held.
- `CheckPayment` and the webhook both ask Es directly through LND REST before locking a deal.
- A hold counts only when LND reports a matching `payment_hash`/`r_hash`, the matching amount, `State == ACCEPTED`, `settled == false`, and `settle_date == 0`.
- Only then does an `awaiting_payment` deal move to `locked` (**Funds Held**). Work submission requires `locked`; `awaiting_payment → work_submitted` is invalid.
- If the checker is not configured, verification is unavailable and `FundDeal` returns `503`.

This is enforced in `backend/internal/lnd/client.go` (`IsAcceptedHold`), `backend/internal/deals/service.go` (`refreshPaymentStatus`/`FundDeal`), and `backend/internal/webhook/service.go`. Do not force `locked` from `paid=true` or a pending hold.

### Network Topology

| Network | Purpose | Who's on it |
|---------|---------|-------------|
| `polar-network-3_default` | Docker network Polar created for its containers | `polar-n3-backend1`, `polar-n3-Es`, `polar-n3-Cl`, `polar-n3-Fre`, `polar-n3-Pay`, **our `lnbits`** |
| Host (localhost) | Where our app runs | Frontend (3000), Backend (8080), browser |
| LNbits REST API | Bridge for money moves (hold/settle/cancel/pay) | Backend → `localhost:5000` |
| LND REST on Es | Direct accepted-hold verification | Backend → `LND_REST_URL` (`https://localhost:8082` in `backend/.env.example`) |

**Important:** LNbits runs *inside* Polar's Docker network so it can reach `https://polar-n3-Es:8080` using the container name as a hostname. From the host machine, we talk to LNbits at `localhost:5000` and to Es directly via the `LND_REST_*` values in `backend/.env.example`.

---

## What Is Polar?

**Polar** is a desktop app that runs a small Bitcoin + Lightning Network on your machine. It's a sandbox — the Bitcoin is fake (regtest), so you can mine blocks instantly and create as many sats as you need.

- **Where its config lives:** `~/.polar/networks/3/docker-compose.yml`
- **Where its data lives:** `~/.polar/networks/3/volumes/`

Polar network 3 contains the regtest backend and the LND nodes used by the current escrow flow:

| Container | Role | Host Ports |
|-----------|------|------------|
| `polar-n3-backend1` | bitcoind (fake Bitcoin blockchain) | 18446 (RPC), 19444 (P2P) |
| `polar-n3-Es` | LND node — **our escrow wallet (Alice1/Es)** | REST :8080 in network, host port from `backend/.env.example` (`:8082`) |
| `polar-n3-Cl` | LND client node — **Bob, the client** | See Polar GUI |
| `polar-n3-Fre` | LND node — **freelancer payout destination** | See Polar GUI |
| `polar-n3-Pay` | Route-only hop Es → Fre if needed | See Polar GUI |

### Node roles

- **Es (Alice1)** = the node LNbits uses and the node the backend verifies directly → holds escrow money
- **Cl (Bob)** = the client; funds the hold from a linked wallet
- **Fre** = the freelancer; generates the payout invoice Ganji pays after approval
- **Pay** = route-only Es → Fre if needed, **not** a second Ganji wallet
- **Alice2/relay** = reverted; do not describe it as part of the current setup

---

## What Is LNbits?

**LNbits** is a Lightning wallet/server with a REST API. Our Go backend uses it for money moves (create hold, settle, cancel, payout), and talks directly to Es LND REST for accepted-hold verification.

- **Runs on:** `http://localhost:5000`
- **LNbits backend:** `LND_REST_ENDPOINT=https://polar-n3-Es:8080`
- **API keys** (in `backend/.env`): `LNBITS_API_KEY` (create invoices) and `LNBITS_ADMIN_KEY` (settle/cancel holds, payouts)
- **Backend checker** (in `backend/.env`): `LND_REST_URL` / `LND_REST_CERT_PATH` / `LND_REST_MACAROON_PATH` on Es
- **Wallet data:** stored in Docker volume `ganji_lnbits-data`

### Why LNbits plus a direct LND checker?

LNbits gives us:
- A simple HTTP API (`POST /api/v1/payments`, `/settle`, `/cancel`)
- Webhook support (best-effort notification, not proof of funds)
- User-facing wallet UI for debugging (visit `http://localhost:5000`)

LND REST on Es gives us the accepted-hold proof that `paid=true` cannot provide.

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

**3. Optional: full three-wallet topology test.** Start two more LNbits
instances, backed by `Cl` and `Fre`:

```bash
docker compose -f lnbits-docker-compose.yml up -d
```

| URL | LND backend | Use |
|---|---|---|
| `http://localhost:5000` | `polar-n3-Es` | Ganji escrow wallet; backend `LNBITS_URL` |
| `http://localhost:5001` | `polar-n3-Cl` | Client wallet; fund dialog URL |
| `http://localhost:5002` | `polar-n3-Fre` | Freelancer wallet; invoice dialog URL |

With only `:5000`, extra wallets are sub-accounts on Es and do not test real
channel routing.

Open each URL, log in, and create one test wallet where needed. Use the client
wallet **admin key** from `:5001` in the fund dialog and the freelancer wallet
**invoice key** from `:5002` in the generate-invoice dialog. Leave the backend's
`LNBITS_*` keys pointing at the `:5000` escrow wallet.

Our `lnbits-docker-compose.yml` joins Polar's network using an **external network**:

```yaml
networks:
  polar-network:
    name: polar-network-3_default
    external: true   # ← Polar created it; we just join it
```

This is why LNbits can reach `https://polar-n3-Es:8080` — Docker's DNS resolves the container name to its IP within the network. The backend uses `LND_REST_URL=https://localhost:8082` with the Es cert/macaroon paths from `backend/.env.example`.

### TLS Certificates

LND REST API uses HTTPS with a self-signed certificate. We mount Polar's cert and macaroon into LNbits so it can authenticate:

```yaml
volumes:
  - /home/amonoff/.polar/networks/3/volumes/lnd/Es/tls.cert:/certs/tls.cert:ro
  - /home/amonoff/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon:/certs/admin.macaroon:ro
```

The cert includes `DNS:polar-n3-Es` in its SANs, so TLS verification succeeds. Do not mount Alice2/relay credentials; that workflow was reverted.

---

## How Hold Invoices Work (Escrow)

This is the **core feature** Ganji depends on. A hold invoice is a BOLT11 Lightning invoice where payment is **accepted but not settled** until the receiver reveals a secret (the preimage).

### The Lifecycle

```
1. Ganji generates a random preimage R and computes SHA256(R) = payment_hash
2. Ganji asks LNbits (backed by Es): "create hold invoice for this payment_hash"
3. Client funds from linked wallet; backend pays that hold invoice transiently with the wallet admin key
4. LND on Es reports an ACCEPTED, unsettled hold → deal locks (Funds Held)
5. Freelancer does the work (submission requires locked)
6a. Client approves → Ganji reveals R on Es and pays polar-n3-Fre directly
6b. Client disputes/rejects after submission → no R revealed; hold stays for the arbiter
6c. Arbiter reveals R to pay Fre, or cancels/waits for expiry to refund Bob (no R)
```

### Backend Code Reference

| Function | File | What it does |
|----------|------|--------------|
| `CreateHoldInvoice` | `backend/internal/lnbits/client.go:131` | Creates the held payment via LNbits |
| `SettleHold` | `backend/internal/lnbits/client.go:174` | Reveals R on Es and releases escrow |
| `CancelHold` | `backend/internal/lnbits/client.go:190` | Cancels the hold to refund the client |
| `IsAcceptedHold` | `backend/internal/lnd/client.go:88` | Verifies hash/amount/`ACCEPTED`/unsettled on Es |
| `FundDeal` | `backend/internal/deals/service.go:1215` | Pays hold from linked wallet, verifies, locks |

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
# All three containers should say "Up"
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

# Terminal 2 — Frontend (use dev:lan when accessing from another device)
cd frontend && npm run dev:lan
```

**7. Test:** Sign up at `http://localhost:3000/signup`, generate the freelancer payout invoice, create a deal, and fund the hold from the client's linked wallet. The deal becomes `locked` (**Funds Held**) only after Es accepts the hold; work submission before that is invalid.

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

### LND (Es / Alice1)

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

### `polar-n3-Es` is crash-looping: "parse address es failed"

**Cause:** The container wasn't attached to Polar's Docker network (happens if Polar was closed and containers were orphaned).

**Fix:** Recreate it:
```bash
docker rm -f polar-n3-Es
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d es
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

### `FundDeal` returns `503` or the deal stays `awaiting_payment`

**Cause:** The backend accepted-hold checker is not configured, or Es does not
report a matching `ACCEPTED` hold. LNbits `paid=true` and webhooks are not proof
of funds.

**Fix:** Set all three `LND_REST_URL` / `LND_REST_CERT_PATH` /
`LND_REST_MACAROON_PATH` values from `backend/.env.example`, then confirm with
`lookupinvoice HASH` on `polar-n3-Es`: matching hash/amount, `state=ACCEPTED`,
`settled=false`, `settle_date=0`.

### Funding rejected as an invalid wallet URL or key

**Cause:** Wallet URLs must be `http`/`https` with a host and no
userinfo/query/fragment; there is no allowlist. The key must be non-empty and
is used transiently (never stored or logged).

**Fix:** Pass the linked wallet's URL and admin key in
`POST /public/deals/:shareToken/fund` (or `POST /deals/wallet/invoice` for the
freelancer payout invoice).

---

## File Reference

| File | Purpose |
|------|---------|
| `~/.polar/networks/3/docker-compose.yml` | Polar's own config (bitcoind, Es, Cl, Fre, Pay) |
| `~/.polar/networks/3/volumes/lnd/Es/tls.cert` | Es TLS certificate |
| `~/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon` | Es admin macaroon |
| `lnbits-docker-compose.yml` | Our LNbits container setup (wired to Es) |
| `backend/.env` | LNbits URL/keys, Es LND REST checker, hold invoice settings |
| `backend/internal/lnbits/client.go` | Hold invoice money-move API calls |
| `backend/internal/lnd/client.go` | Accepted-hold verification on Es |
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
LNbits LND backend:     https://polar-n3-Es:8080
Backend LND checker:    LND_REST_URL=https://localhost:8082 + Es cert/macaroon paths
Escrow node:            polar-n3-Es (Alice1/Es; REST :8080 in network)
Client:                 polar-n3-Cl (Bob)
Freelancer:             polar-n3-Fre
Route-only:             polar-n3-Pay (Es → Fre if needed, not a Ganji wallet)
Not current:            Alice2 / relay forwarding
Bitcoin RPC:            polar-n3-backend1:18443 (host :18446)
Locked status:          "Funds Held" — ACCEPTED hold on Es, not settled
Hold invoices:          Required for escrow — Phoenixd doesn't support them

LNbits v1.7.1 gotchas:
  - DB settings override env vars (use json.dumps for values)
  - first_install redirect caused by account.extra.provider == "env"
  - Data folder defaults to /app/data (we set LNBITS_DATA_FOLDER=/data)
  - POST /payments defaults out:true — must send out:false to create invoices
```
