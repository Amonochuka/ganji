# Demo Runbook — Full Deal Flow

> Step-by-step from a cold start to a real hold invoice being paid.
> Follow top to bottom. Commands use the repo root `~/Projects/ganji` unless noted.

---

## Part 0 — Concepts (read once, 2 minutes)

```
Bob = CLIENT      (pays the hold from a linked wallet — polar-n3-Cl)
Es  = ESCROW vault (Alice1 / polar-n3-Es; backs Ganji's LNbits)
Fre = FREELANCER  (receives the payout directly — polar-n3-Fre)
```

`polar-n3-Pay` is only a route-only hop (Es → Fre) if routing needs it; it is
**not** a second Ganji wallet. The earlier Alice2/relay workflow was reverted
and is not current.

**Demo mode:** the flow below is the three-wallet `Bob (Cl) → Es → Fre` path,
with three separate wallets. Ganji's escrow node is Es; Bob funds the hold from
his linked LNbits wallet, Es holds it, and Fre is paid on release.

You mine blocks **only at setup**, not per deal. Polar's GUI handles regtest mining automatically for `Deposit` and `Open Channel`; with CLI `bitcoin-cli`/`lncli`, mine manually when the runbook says to. The source of truth is `walletbalance` and `active: true`, not a fixed manual step. In production nobody mines — clients bring their own funded wallets.

> 🎯 **Long-term goal:** automate this whole demo (spin up Polar, mine, fund, open channels, create deal, pay, settle) with a single script. See [Part 7 — Automation](#part-7--automation-roadmap).
>
> Demo LNbits config disables the fee reserve (`LNBITS_RESERVE_FEE_MIN=0`, `LNBITS_RESERVE_FEE_PERCENT=0`) so the escrow wallet can fully pay out in local regtest. Existing wallets also need `lnbits_reserve_fee_min=0` and `lnbits_reserve_fee_percent=0.0` in LNbits Admin → Server Settings, because saved DB settings override env on startup. Do not copy this to real-money deployments.

---

## Part 0.5 — Demo Credentials & UI Checkpoints

### Logins

| UI | URL / App | Login | Notes |
|----|-----------|-------|-------|
| **LNbits** | `http://localhost:5000` | `admin` / `lnbits_local_dev_2026` | This is Ganji's escrow wallet, backed by Polar's Es node (Alice1 / `polar-n3-Es`). Local demo only. |
| **Polar** | Polar desktop app | No login/password by default | It is a local desktop app. If you set a password, use your own; nothing is stored in this repo. |
| **Ganji frontend** | `http://localhost:3000` | Signup/login with a local test user | Demo deal uses freelancer `polar@test.com` (node `polar-n3-Fre`) and client `local@test.com` (node `polar-n3-Cl`). |

### Polar install on Ubuntu (user-local, no sudo)

Polar was installed from the official GitHub release as an AppImage:

```bash
mkdir -p ~/Applications ~/.local/share/applications
curl -fL -o ~/Applications/polar.AppImage \
  https://github.com/jamaljsr/polar/releases/download/v4.0.0/polar-linux-x86_64-v4.0.0.AppImage
chmod +x ~/Applications/polar.AppImage
```

Expected SHA-256 for `polar-linux-x86_64-v4.0.0.AppImage`:

```text
b59657fd9c9b0b0a69395e055f7c9c1468ec8f874cfb5597da8fdfa64345fc9c
```

This machine has no `libfuse2`, so launch with AppImage extraction mode and Electron sandbox flags:

```bash
DISPLAY=:0 ~/Applications/polar.AppImage \
  --appimage-extract-and-run --no-sandbox --disable-setuid-sandbox
```

A desktop launcher is installed at `~/.local/share/applications/polar.desktop` with the same flags. Polar has no default login/password; it is a local desktop app.

### Containers this app owns

These are the containers created by this repo's Compose files. The backend and frontend are currently run from the host, so only LNbits is running in Docker locally.

| Container | Service | Compose file | Port / storage | Purpose |
|-----------|---------|--------------|----------------|---------|
| `ganji-lnbits-1` | `lnbits` | `lnbits-docker-compose.yml` | `5000:5000`, volume `ganji_lnbits-data` | Ganji escrow wallet, backed by Polar's Es node (Alice1). |
| `ganji-db-1` | `db` | `docker-compose.yml` | `5432:5432`, volume `ganji_pgdata` | Ganji Postgres. Defined locally; currently not created. |
| `ganji-api-1` | `api` | `docker-compose.yml` | `8080:8080`, volume `ganji_uploads` | Ganji Go API. Defined locally; currently not created. |
| `ganji-frontend-1` | `frontend` | `docker-compose.yml` | `3000:3000` | Ganji Next.js frontend. Defined locally; currently not created. |

Polar network 3 is an external dependency, not created by Ganji: `polar-n3-backend1` (regtest Bitcoin), `polar-n3-es` (Alice1, escrow LND node), `polar-n3-cl` (Bob, demo client node), `polar-n3-fre` (freelancer node), and optionally `polar-n3-pay` (route-only Es → Fre hop). Ganji's LNbits joins `polar-network-3_default` and mounts Es's TLS cert and admin macaroon read-only.

List all Ganji-owned containers:

```bash
docker ps -a --filter label=com.docker.compose.project=ganji \
  --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker compose -f docker-compose.yml ps -a
docker compose -f lnbits-docker-compose.yml ps -a
```

Both Compose files share the project name `ganji`. Avoid `docker compose -f docker-compose.yml down --remove-orphans` while LNbits is needed — it also removes `ganji-lnbits-1`. Use `docker compose -f lnbits-docker-compose.yml down` to stop LNbits while preserving its external volume.

### Where to see escrow in each UI

**LNbits UI** (`http://localhost:5000`, login above):
- Wallet balance shows `0 sats` while the hold is unsettled — expected.
- **Transactions** shows `Test Deal Polar — 50,000 — pending` with a pause icon.
- That row is the escrow/hold invoice. LNbits does **not** show LND internals like `ACCEPTED` or pending HTLC.

**Polar UI** (Polar desktop app):
1. Select network **3**.
2. Click the **es** node (`polar-n3-es` / Alice1 — this is the escrow vault backing LNbits).
3. Open **Invoices** and find the 50,000-sat invoice with hash prefix `94cc8d6f`.
4. While held, it should show `ACCEPTED` (not `SETTLED`) and `settle_date: 0`.
5. Open **Channels** and select Bob's (Cl's) 500,000-sat channel with Es; look for the pending incoming HTLC of **50,000 sats**.

CLI equivalent (authoritative):

```bash
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices
```

---

## Part 1 — Start Everything (Terminal)

### 1.1 Start Polar (bitcoind + Es + Cl + Fre)

```bash
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d
docker ps --format '{{.Names}}\t{{.Status}}'
```

Expect: `polar-n3-backend1`, `polar-n3-es`, `polar-n3-cl`, and `polar-n3-fre` all **Up** (`polar-n3-pay`, if present, is route-only).

> If `polar-n3-es` is crash-looping ("parse address es failed"):
> ```bash
> docker rm -f polar-n3-es
> docker compose -f ~/.polar/networks/3/docker-compose.yml up -d es
> ```

### 1.2 Mine a fresh block only if needed (so LND syncs)

Polar GUI actions normally mine automatically. Use this command only for CLI setup or if LND reports `synced_to_chain: false`.

```bash
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 1
```

Verify Es is synced (wait ~10s after mining):

```bash
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  getinfo | grep synced
```
Expect `"synced_to_chain": true`.

### 1.3 Start LNbits

```bash
cd ~/Projects/ganji
docker compose -f lnbits-docker-compose.yml up -d
sleep 20
docker logs ganji-lnbits-1 --tail 8 2>&1 | grep -iE "LndRestWallet|connected|VoidWallet"
```
Expect: `✔️ Backend LndRestWallet connected and with a balance of ~9991530000 msat`
If you see `VoidWallet`, see `docs/POLAR_LNBITS_SETUP.md` → "Troubleshooting".

For a full topology test, also start the client and freelancer LNbits instances:

```bash
docker compose -f lnbits-docker-compose.yml up -d
```

Use `http://localhost:5001` in the client fund dialog and
`http://localhost:5002` in the freelancer invoice dialog. Keep the backend on
`http://localhost:5000`.

### 1.4 Start Backend (Go API, port 8080)

```bash
cd ~/Projects/ganji/backend
setsid nohup go run ./cmd/api > /tmp/ganji-backend.log 2>&1 < /dev/null & disown
sleep 15
curl -s http://localhost:8080/health
```
Expect: `{"db":"connected","lnbits":"connected","status":"ok"}`

### 1.5 Start Frontend (Next.js, port 3000)

```bash
cd ~/Projects/ganji/frontend
fuser -k 3000/tcp 2>/dev/null
setsid nohup npm run dev:lan > /tmp/ganji-frontend.log 2>&1 < /dev/null & disown
sleep 10
curl -s -o /dev/null -w "%{http_code}" http://localhost:3000
```
Expect: `200`

### 1.6 Three-Wallet Full Flow Check

1. Open `http://localhost:5000`, `http://localhost:5001`, and
   `http://localhost:5002`. Keep the backend's `LNBITS_*` keys on `:5000`.
2. Create one client wallet in `:5001` and copy its **admin key**. Create one
   freelancer wallet in `:5002` and copy its **invoice key**.
3. Log in as the freelancer, use **Generate payout invoice** with `:5002` and
   the freelancer invoice key, then create the deal with that `lnbc...` invoice.
4. Open the public deal link as the client, choose **Pay hold invoice**, and use
   `:5001` with the client admin key.
5. Confirm the deal shows **Funds Held** only after Es accepts the hold.
6. Submit work as the freelancer, then approve as the client; confirm payout to
   the freelancer invoice.

---

## Part 2 — Fund Bob (Cl) & Open Channel to Es (ONE-TIME setup)

Bob's client node (`polar-n3-cl`) currently has ~5,000 sats. We give it a big budget once.

### 2.1 Get Bob's Bitcoin address

```bash
docker exec polar-n3-cl lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  newaddress p2wkh
```
Copy the `address` value.

### 2.2 Fund Bob (Polar GUI auto-mines; CLI steps mine manually)

**Polar GUI path (recommended):** click `bob` → **Deposit**. Polar mines and matures the regtest coins automatically; skip the manual mining below and just verify `walletbalance`.

**CLI path:** use the commands below.

```bash
CL_ADDRESS="paste_the_address_here"
# Mine 10 blocks to Bob — each block reward goes to his address
for i in $(seq 1 10); do
  docker exec polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    generatetoaddress 1 "$CL_ADDRESS"
done

# IMPORTANT: coinbase rewards need 100 confirmations before they're spendable!
# Mine 100 more blocks (to any address) to mature them.
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 100
```

> ⚠️ **Coinbase maturity:** Regtest requires 100 confirmations for mined block rewards. If you skip the second step, Bob's balance stays at 5,000 sats even though the blocks were mined — the rewards are "immature".

<details>
<summary><strong>📚 Why 100 blocks? (Coinbase Maturity Explained)</strong> — click to expand</summary>

When you "mine a block," the miner gets a reward (50 BTC in regtest). That reward is paid via a special transaction called a **coinbase transaction**. Bitcoin has a security rule:

> **Coinbase rewards can't be spent until 100 blocks have been mined on top of them.**

This exists in real Bitcoin too — it prevents a miner from spending rewards from a block that might get reorged (orphaned) out of the chain.

**In our demo:**
1. `generatetoaddress 1 "$CL_ADDRESS"` → creates a block with a 50 BTC coinbase reward to Bob's address
2. That reward is `immature` — it exists but can't be used
3. Each subsequent block adds one confirmation
4. After 100 more blocks, the reward becomes `mature` and spendable → LND finally counts it in `walletbalance`

**What we observed (the bug that cost us 20 minutes):**
```
Mined 10 blocks to Bob's address
→ Bob's walletbalance: 5,000 sats (unchanged!)
→ Blocks WERE mined, but rewards were immature

Mined 100 more blocks
→ Bob's walletbalance: 100,000,005,000 sats ✅
```

**Key insight:** `bitcoin-cli` shows the blocks exist, but LND's `walletbalance` only counts **mature, spendable** UTXOs. Immature coinbase outputs are excluded.

**Alternatives to mining 100 blocks (CLI path):**
- Use Polar GUI's "Deposit" button — it handles mining and maturity automatically
- Get coins from Es (it has mature funds) via `sendtoaddress` — no maturity wait

</details>

Wait ~10s, then check Bob's balance:

```bash
docker exec polar-n3-cl lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  walletbalance
```
Expect: `total_balance` around 500,000,000,000 sats (500 BTC).

### 2.3 Open a channel Bob (Cl) → Es (needed to pay Lightning invoices)

Get Es's pubkey:

```bash
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  getinfo | grep identity_pubkey
```

**CLI path** — open channel from Bob (replace `ES_PUBKEY`):

```bash
docker exec polar-n3-cl lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  openchannel ES_PUBKEY 500000
```

Mine 6 confirmations **only for the CLI path** so the channel becomes active:

```bash
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 6
sleep 10
docker exec polar-n3-cl lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listchannels
```
Expect the channel with `active: true`.

**Polar GUI path (recommended):** do steps 2.1–2.3 in the **Polar desktop GUI**:
- Click `cl` (Bob) → **Deposit** to fund (Polar mines and matures automatically)
- Click `cl` (Bob) → **Open Channel** → select `es`

Polar mines the confirmation blocks automatically; **do not run the `-generate 6` step** for a UI-created channel. Verified in this environment: channels opened from the Polar UI were `active: true` with no manual mining.

**Go/no-go:** `active: true`. If a UI-created channel is still pending after a few seconds, mine 6 blocks via `backend1` → **Actions** → **Mine** (or the CLI command above).

---

## Part 3 — Create a Deal & Get a Real Invoice

### 3.1 Sign up / Log in in the browser

Open `http://localhost:3000`:
- Go to `/signup` → create an account (remember the password)
- Password eye toggle works now — click it to verify

### 3.2 Create a deal

- Go to `/deals/new`
- Fill in:
  - **Deal Title**: `Tomorrow's Test Deal`
  - **Amount (sats)**: `50000`
  - **Source Platform**: `Upwork`
  - **Client Email**: `client@example.com`
  - **Your Payee Invoice**: generate one from the freelancer node Fre (the payout goes to Fre directly):
    ```bash
    docker exec polar-n3-fre lncli --network=regtest \
      --tlscertpath=/home/lnd/.lnd/tls.cert \
      --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
      addinvoice --amt=50000 --memo="freelancer payout"
    ```
    Copy the `payment_request` (starts with `lnbcrt...`) and paste it. Alternatively use the **Generate invoice** button on `/deals/new` with a linked LNbits wallet URL/key (`POST /deals/wallet/invoice`); wallet keys are transient and never stored or logged.
- Click **Create Deal**

### 3.3 Where to see the created invoice

| Place | How |
|-------|-----|
| **Ganji** | Deal detail page — shows the invoice + share link |
| **LNbits** | `http://localhost:5000` → login `admin` / `lnbits_local_dev_2026` → Transactions → pending hold invoice |
| **Polar** | Click `es` → Invoices tab |
| **CLI** | See below |

**CLI check (from Es's node):**

```bash
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices
```
Look for the invoice with `amt: 50000` and `state: OPEN` (hold invoice accepted but unsettled).

---

## Part 4 — Fund the Deal (Bob = Client)

The client funds the hold from a **linked LNbits wallet** through Ganji's public
fund endpoint. That wallet must be backed by Bob's node (`polar-n3-Cl`), and
Ganji pays the deal's hold invoice from it:

```bash
# From the public deal page, copy the share token (the UUID in the share URL)
SHARE_TOKEN=<share_token>

curl -s -X POST \
  http://localhost:8080/public/deals/$SHARE_TOKEN/fund \
  -H 'Content-Type: application/json' \
  -d '{
    "wallet_url": "http://localhost:5001",
    "wallet_key": "<client-wallet-admin-key>"
  }'
```

`wallet_url` must be an `http`/`https` host only (no userinfo, query, or
fragment; no allowlist here). `wallet_key` is used transiently and is never
stored or logged. For milestones, add `"milestone_id": "<uuid>"` to fund a
specific milestone. The frontend does the same thing through its **Fund** /
**Pay hold** buttons; it also labels the resulting status **"Funds Held"**.

> **Important:** the client pays the **deal's hold invoice**, not the payee
> invoice pasted into the form. A manual alternative for debugging is
> `lncli payinvoice <hold_invoice>` from `polar-n3-cl`, then reloading the
> public link — the GET also verifies the accepted hold.

### Watch what happens

```bash
# Ganji logs — accepted-hold verification happens on Es via LND REST
docker logs ganji-api-1 --tail 20

# LND state on the escrow node
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices
```

The deal locks only after LND on Es confirms a matching accepted hold: same
hash and amount, `State == ACCEPTED`, `settled == false`, `settle_date == 0`.
LNbits `paid=true` alone is **not** proof of funds. Without the LND checker
configured (`LND_REST_URL`, `LND_REST_CERT_PATH`, `LND_REST_MACAROON_PATH`),
`FundDeal` returns `503` and the deal stays in `awaiting_payment`.

**LNbits UI shows the hold, but not the LND state:**
- Wallet balance stays `0 sats` (hold not settled).
- Transactions shows `Test Deal Polar — 50,000 — pending`.
- There is no `ACCEPTED` / HTLC label in the LNbits UI. `pending` there means "not settled yet"; the definitive proof of Es holding the HTLC is Polar/LND below.

**Verify Es is holding it (this is the definitive check):**

```bash
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices | grep -A 8 '"r_hash": "94cc8d6f'
```

You want `state: ACCEPTED`, `amt_paid_sat: 50000`, `settle_date: 0`. That means
Es accepted the HTLC but has not revealed the preimage. The Ganji deal should
now read `locked` / **"Funds Held"**. In Polar GUI: **network 3 → es →
Invoices**, and **Channels → Bob's 500k channel → pending incoming HTLC
50,000**.

---

## Part 5 — Approve / Release (Escrow Settlement)

### 5.0 Why the deal is `locked` (accepted hold, not settled)

With the current accepted-hold model, LNbits `paid=true` is not proof of funds
and no longer locks the deal. Ganji verifies the hold directly against LND on
Es: matching hash/amount, `State == ACCEPTED`, `settled == false`,
`settle_date == 0`. The webhook path requires that checker as well — a
`paid=true` webhook without an accepted hold is rejected, and a missing checker
returns `ErrVerificationUnavailable` / `503` from `FundDeal`. `locked` therefore
means **"Funds Held"**, not "money already moved".

### 5.1 Freelancer submits work (`locked → work_submitted`)

`SubmitWork` is allowed from `locked` (not `awaiting_payment`) and requires at
least one artifact. In the frontend, log in as the freelancer
(`polar@test.com`, node `polar-n3-Fre`), open the deal, and use the
**Deliverables** panel:

1. Paste a work link into **Work link or delivery note** and click **Attach Work Link**, or upload a file with **Upload File**.
2. Confirm the artifact appears in the deliverables list.
3. Click **Submit Work**. The button unlocks only after at least one artifact is attached and the deal is `locked`.

Check the deal page: status should now be `work_submitted`. The client share page shows the same single-step deal as one “Final delivery” milestone, so the submitted work is visible even though no milestone rows were created. If the client rejects at this point, no R is revealed and the hold is not settled, cancelled, or failed — it goes to dispute/review.

### 5.2 Client approves (`work_submitted → released`)

Log in as the client (`local@test.com`, node `polar-n3-Cl`) and use the
**approve/release** action on the deal page (or the public share link), or call:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $CLIENT_TOKEN" \
  http://localhost:8080/deals/$DEAL_ID/approve
```

Behind the scenes:

1. Ganji reveals R and calls LNbits `/api/v1/payments/settle` → held sats settle into escrow
2. Ganji calls `PayInvoice(payee_invoice)` → pays the freelancer directly (`polar-n3-Fre`; `polar-n3-Pay` is only a route-only hop if needed)
3. Deal status → `released`

Verify:

```bash
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices
```
The hold invoice should now show `state: SETTLED`; the freelancer's invoice on
Fre should show it was paid.

### 5.3 Dispute / arbitration

If the client disputes, the deal moves to `disputed` with the hold still
accepted and unsettled. The arbiter can reveal R to pay the freelancer
(`released`), or cancel / wait for hold expiry to refund the client
(`refunded`). Refunds never use R.

---

## Part 6 — Realistic 3-Wallet Experiment (Bob/Cl → Es → Fre)

This mode gives a clean, realistic escrow demo: **Bob (client, `polar-n3-Cl`) funds the hold, Es (`polar-n3-Es` / Alice1) holds it, and Fre (`polar-n3-Fre`) receives the payout** — three separate wallets, no money looping back to the same node. `polar-n3-Pay`, if present, is only a route-only hop Es → Fre when needed, not a second Ganji wallet.

### 6.1 What We're Building

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│    BOB      │  pays   │     ES      │  pays   │    FRE      │
│  (client)   │────────▶│  (escrow)   │────────▶│ (freelancer)│
│ polar-n3-Cl │         │ polar-n3-Es │         │ polar-n3-Fre│
└─────────────┘         └─────────────┘         └─────────────┘
   sends sats            holds sats              receives sats
```

### 6.2 Add Fre to Polar (if not present)

**Using Polar GUI (recommended):**
1. Open the Polar desktop app
2. Select network 3
3. Click **"Add Node"** → choose **LND**
4. Name it `fre`
5. Connect it to the existing bitcoind backend (`polar-n3-backend1`)
6. Click **Start**

**Or via CLI** — add a `fre` service to Polar's compose file:

```bash
# See the existing es/cl services in ~/.polar/networks/3/docker-compose.yml
# Copy the cl service block, rename: cl → fre, ports 8082→8083, 10002→10003, 9736→9737
# Then:
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d fre
```

> ⚠️ Editing Polar's compose file while Polar GUI is open may cause conflicts. Prefer the GUI method.

### 6.3 Fund Fre (Polar GUI auto-mines; CLI steps mine manually)

**Polar GUI path (recommended):** click `fre` → **Deposit**. Polar mines and matures the regtest coins automatically; skip the manual mining below and just verify `walletbalance`.

**CLI path:** use the commands below.

```bash
# Get Fre's address
docker exec polar-n3-fre lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  newaddress p2wkh

# Mine to it + mature
FRE_ADDRESS="paste_fre_address"
for i in $(seq 1 5); do
  docker exec polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    generatetoaddress 1 "$FRE_ADDRESS"
done
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 100
```

### 6.4 Open a channel Es → Fre (so Es can pay Fre)

Bob→Es channel already exists (Part 2.3). Now we need **Es→Fre** so escrow can pay out:

```bash
# Get Fre's pubkey
FRE_PUBKEY=$(docker exec polar-n3-fre lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  getinfo | grep identity_pubkey | cut -d'"' -f4)

# Open channel Es → Fre (Es has plenty of funds) — CLI path
docker exec polar-n3-es lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  openchannel "$FRE_PUBKEY" 500000

# Mine 6 confirmations only for the CLI path
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 6
```

**Polar GUI path (recommended):** click `es` → **Open Channel** → select `fre`. Polar mines the confirmation blocks automatically; do not run `-generate 6`. Verify with `listchannels` on Es that the channel is `active: true`.

**Go/no-go:** `active: true` on both Bob↔Es and Es↔Fre. If a UI-created channel is still pending after a few seconds, mine 6 blocks via `backend1` → **Actions** → **Mine**.

### 6.5 Generate the Payee Invoice from Fre

```bash
docker exec polar-n3-fre lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  addinvoice --amt=50000 --memo="freelancer payout to Fre"
```
Copy the `payment_request` (starts `lnbcrt...`) — paste this into Ganji's `payee_invoice` field. Alternatively use the frontend's **Generate invoice** button / `POST /deals/wallet/invoice` with a linked wallet backed by Fre.

### 6.6 Run the Flow

1. **Create deal** in Ganji with Fre's payee invoice
2. **Fund the hold invoice** from Bob's linked wallet (same as Part 4) → status `locked` / **"Funds Held"** only after LND on Es accepts the hold
3. **Submit work** from the freelancer account (`locked → work_submitted`)
4. **Approve** in Ganji → R revealed, escrow settles → Es pays Fre's invoice directly (route via `polar-n3-Pay` only if needed)
5. **Verify Fre received:**
```bash
docker exec polar-n3-fre lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices | grep -A 5 "settled"
```

**Result:** Bob's balance went down, Fre's went up, Es acted as escrow. Clean three-way escrow demo. ✅

---

## Part 7 — Automation Roadmap

The goal is to replace all the manual terminal work above with **one command**:

```bash
make demo          # or: ./scripts/demo-full-flow.sh
```

### What such a script would do

| Step | Command/action | Automatable? |
|------|---------------|--------------|
| 1. Start Polar | `docker compose -f ~/.polar/networks/3/docker-compose.yml up -d` | ✅ |
| 2. Wait for LND sync | Poll `getinfo` until `synced_to_chain: true` on Es, Cl, and Fre | ✅ |
| 3. Mine fresh block only if needed | CLI/stale chain: `bitcoin-cli -generate 1`; Polar GUI normally auto-mines | ✅ |
| 4. Start LNbits | `docker compose -f lnbits-docker-compose.yml up -d` | ✅ |
| 5. Start backend | `go run ./cmd/api &` + poll `/health` | ✅ |
| 6. Start frontend | `npm run dev &` + poll port 3000 | ✅ |
| 7. Fund Bob (Cl) | Polar GUI `Deposit` (auto-mines/matures), or CLI: `newaddress` → `generatetoaddress` → mine 100 → check balance | ✅ |
| 8. Fund Fre (if used) | Polar GUI `Deposit` on `fre`; same CLI mining pattern | ✅ |
| 9. Open channels | `openchannel` Bob→Es, Es→Fre; CLI: mine 6 until `active: true` (Polar GUI auto-mines) | ✅ |
| 10. Create deal via API | `curl POST /auth/signup` + optional `curl POST /deals/wallet/invoice` + `curl POST /deals` | ✅ |
| 11. Parse share token | `jq` the share link / deal response | ✅ |
| 12. Fund from Bob's linked wallet | `curl POST /public/deals/:shareToken/fund` with `wallet_url`/`wallet_key`; poll until `locked` (503 without the LND checker) | ✅ |
| 13. Submit work | `curl POST /deals/:id/submit` with an artifact (requires `locked`) | ✅ |
| 14. Approve deal | `curl POST /deals/:id/approve` (reveals R, pays Fre directly) | ✅ |
| 15. Verify | Check Es invoice `SETTLED`, Fre invoice paid, all balances | ✅ |

### Suggested structure

```
scripts/
├── demo-full-flow.sh      # orchestrator — runs all steps
├── lib/
│   ├── lnd.sh             # lndcli wrapper with cert/macaroon paths
│   ├── bitcoind.sh        # bitcoin-cli wrapper
│   └── wait.sh            # polling helpers (wait_for_sync, wait_for_balance)
├── demo-config.sh         # configurable: amounts, emails, container names
└── demo-reset.sh          # tear down + fresh start (wipe volumes)
```

### Key design decisions to make later

- **Idempotency:** script should skip steps already done (e.g., don't re-fund Bob if he has enough)
- **Cleanup:** reset script that wipes LNbits DB + Polar volumes for a truly fresh run
- **Output:** pretty-printed status at each step ("✅ Bob funded: 1000 BTC", "⏳ waiting for sync...")
- **Error handling:** fail fast with clear messages (e.g., "Es not synced after 60s — check chain tip"; "funding blocked: LND accepted-hold checker not configured")
- **Third wallet:** add Fre only when realism matters; `polar-n3-Pay` is route-only and not a second Ganji wallet. For a quick smoke test, Bob can receive the payout himself, but the standard demo pays Fre directly.

### Why automate?

- Reproducible demos (same result every time)
- Fast regression testing after code changes
- Easy onboarding for new devs ("just run `make demo`")
- Can later become a CI smoke test

---

## Quick Troubleshooting

| Symptom | Fix |
|---------|-----|
| Es crash-loops | `docker rm -f polar-n3-es && docker compose -f ~/.polar/networks/3/docker-compose.yml up -d es` |
| `synced_to_chain: false` | CLI/stale chain only: mine 1 fresh block (Part 1.2). Polar GUI actions normally auto-mine. |
| LNbits says VoidWallet | See doc → "Troubleshooting" — DB settings override env |
| `/first_install` redirect | See doc → "Troubleshooting" |
| `Wallet not found` | Check API keys in DB match `backend/.env` |
| `Invoice key required` | Ganji creates the hold with `out: false`; public funding pays it with `out: true` from the client's linked wallet. Manual curls must send the matching flag. |
| Bob can't pay | Check `listchannels` shows `active: true` and `walletbalance` has sats. If pending, mine 6 blocks (CLI or `backend1` → **Actions** → **Mine**). |
| Channel created in Polar UI shows pending | Wait a few seconds; Polar auto-mines. If still pending, mine 6 blocks via `backend1` → **Actions** → **Mine**; `active: true` is the go/no-go |
| Bob funded but balance unchanged | Coinbase maturity — CLI path: mine 100 more blocks (see Part 2.2 explanation). GUI `Deposit` auto-mines/matures. |
| Deal still `awaiting_payment` after Bob paid | `paid=true` is not enough. Check the Es invoice is accepted (hash/amount match, `ACCEPTED`, `settled=false`, `settle_date=0`), then retry `POST /public/deals/:shareToken/fund` or reload the public link. |
| `FundDeal` returns 503 | LND checker not configured on Es — set `LND_REST_URL`, `LND_REST_CERT_PATH`, `LND_REST_MACAROON_PATH`. Without it the deal cannot lock. |
| Approve returns `cannot be approved from status awaiting_payment` | The deal must first lock on an accepted hold (Part 4), then freelancer submits work (Part 5.1) to reach `work_submitted`. |
| Approve says `You must reserve at least ...` | Set LNbits Admin → Server Settings reserve min/percent to `0`, or update `system_settings` in `ganji_lnbits-data`, then restart LNbits. Env vars alone do not override saved admin settings. |
| Reset button disappeared after approve failed | Expected when LNbits definitively refused (`status: "failed"`): the marker auto-clears so no reset is needed. Fix the reserve, then approve again. |
| Fre can't receive payout | Need Es→Fre `active: true` (route via `polar-n3-Pay` only if needed), and Fre's payee invoice amount must be ≤ Es's local balance on that channel. If pending, mine 6 blocks or wait for Polar UI auto-mining. |

---

## Cheat Sheet — LND CLI Shortcut

To avoid typing the long flags every time, set a shell variable first:

```bash
CL="lncli --network=regtest --tlscertpath=/home/lnd/.lnd/tls.cert --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon"
docker exec polar-n3-cl sh -c "$CL walletbalance"
docker exec polar-n3-cl sh -c "$CL payinvoice <bolt11>"
```

Same works for `polar-n3-es` by swapping the container name.

---

**Full technical background:** `docs/POLAR_LNBITS_SETUP.md`
