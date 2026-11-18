# Demo Runbook — Full Deal Flow

> Step-by-step from a cold start to a real hold invoice being paid.
> Follow top to bottom. Commands use the repo root `~/Projects/ganji` unless noted.

---

## Part 0 — Concepts (read once, 2 minutes)

```
Bob    = fake CLIENT     (pays the invoice — sends sats out)
Alice  = ESCROW vault    (holds the sats; backs LNbits)
Carol  = fake FREELANCER (receives the payout — gets sats in)
```

**Two demo modes:**
- **Simple mode** (Parts 1–5): Bob plays BOTH client and freelancer — he pays in, then gets paid back. Fine for proving escrow works, but the money just returns to the same wallet.
- **Realistic mode** (Part 6): Add a third node **Carol** as the freelancer, so the flow is truly `Bob → Alice → Carol` with three separate wallets.

You mine blocks **only at setup**, not per deal. In production nobody mines — clients bring their own funded wallets.

> 🎯 **Long-term goal:** automate this whole demo (spin up Polar, mine, fund, open channels, create deal, pay, settle) with a single script. See [Part 7 — Automation](#part-7--automation-roadmap).

---

## Part 0.5 — Demo Credentials & UI Checkpoints

### Logins

| UI | URL / App | Login | Notes |
|----|-----------|-------|-------|
| **LNbits** | `http://localhost:5000` | `admin` / `lnbits_local_dev_2026` | This is Ganji's escrow wallet, backed by Polar's Alice node. Local demo only. |
| **Polar** | Polar desktop app | No login/password by default | It is a local desktop app. If you set a password, use your own; nothing is stored in this repo. |
| **Ganji frontend** | `http://localhost:3000` | Signup/login with a local test user | Demo deal uses freelancer `polar@test.com` and client `local@test.com`. |

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

### Where to see escrow in each UI

**LNbits UI** (`http://localhost:5000`, login above):
- Wallet balance shows `0 sats` while the hold is unsettled — expected.
- **Transactions** shows `Test Deal Polar — 50,000 — pending` with a pause icon.
- That row is the escrow/hold invoice. LNbits does **not** show LND internals like `ACCEPTED` or pending HTLC.

**Polar UI** (Polar desktop app):
1. Select network **3**.
2. Click the **alice** node (`polar-n3-alice` — this is the escrow vault backing LNbits).
3. Open **Invoices** and find the 50,000-sat invoice with hash prefix `94cc8d6f`.
4. While held, it should show `ACCEPTED` (not `SETTLED`) and `settle_date: 0`.
5. Open **Channels** and select Bob's 500,000-sat channel with Alice; look for the pending incoming HTLC of **50,000 sats**.

CLI equivalent (authoritative):

```bash
docker exec polar-n3-alice lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices
```

---

## Part 1 — Start Everything (Terminal)

### 1.1 Start Polar (bitcoind + Alice + Bob)

```bash
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d
docker ps --format '{{.Names}}\t{{.Status}}'
```

Expect: `polar-n3-backend1`, `polar-n3-alice`, `polar-n3-bob` all **Up**.

> If `polar-n3-alice` is crash-looping ("parse address alice failed"):
> ```bash
> docker rm -f polar-n3-alice
> docker compose -f ~/.polar/networks/3/docker-compose.yml up -d alice
> ```

### 1.2 Mine a fresh block (so LND syncs)

```bash
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 1
```

Verify Alice is synced (wait ~10s after mining):

```bash
docker exec polar-n3-alice lncli --network=regtest \
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
setsid nohup npm run dev > /tmp/ganji-frontend.log 2>&1 < /dev/null & disown
sleep 10
curl -s -o /dev/null -w "%{http_code}" http://localhost:3000
```
Expect: `200`

---

## Part 2 — Fund Bob & Open Channel (ONE-TIME setup)

Bob currently has ~5,000 sats. We give him a big budget once.

### 2.1 Get Bob's Bitcoin address

```bash
docker exec polar-n3-bob lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  newaddress p2wkh
```
Copy the `address` value.

### 2.2 Mine blocks to Bob (fund him with fake sats)

```bash
BOB_ADDRESS="paste_the_address_here"
# Mine 10 blocks to Bob — each block reward goes to his address
for i in $(seq 1 10); do
  docker exec polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    generatetoaddress 1 "$BOB_ADDRESS"
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
1. `generatetoaddress 1 "$BOB_ADDRESS"` → creates a block with a 50 BTC coinbase reward to Bob's address
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

**Alternatives to mining 100 blocks:**
- Use Polar GUI's "Deposit" button — it handles this automatically
- Get coins from Alice (she has mature funds) via `sendtoaddress` — no maturity wait

</details>

Wait ~10s, then check Bob's balance:

```bash
docker exec polar-n3-bob lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  walletbalance
```
Expect: `total_balance` around 500,000,000,000 sats (500 BTC).

### 2.3 Open a channel Bob → Alice (needed to pay Lightning invoices)

Get Alice's pubkey:

```bash
docker exec polar-n3-alice lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  getinfo | grep identity_pubkey
```

Open channel from Bob (replace `ALICE_PUBKEY`):

```bash
docker exec polar-n3-bob lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  openchannel ALICE_PUBKEY 500000
```

Mine 6 confirmations so the channel becomes active:

```bash
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 6
sleep 10
docker exec polar-n3-bob lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listchannels
```
Expect the channel with `active: true`.

> **Alternative (easier):** do steps 2.1–2.3 in the **Polar desktop GUI**:
> - Click `bob` → **Deposit** to fund
> - Click `bob` → **Open Channel** → select `alice`

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
  - **Your Payee Invoice**: generate one from Bob's node (he'll be paid back):
    ```bash
    docker exec polar-n3-bob lncli --network=regtest \
      --tlscertpath=/home/lnd/.lnd/tls.cert \
      --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
      addinvoice --amt=50000 --memo="freelancer payout"
    ```
    Copy the `payment_request` (starts with `lnbcrt...`) and paste it.
- Click **Create Deal**

### 3.3 Where to see the created invoice

| Place | How |
|-------|-----|
| **Ganji** | Deal detail page — shows the invoice + share link |
| **LNbits** | `http://localhost:5000` → login `admin` / `lnbits_local_dev_2026` → Transactions → pending hold invoice |
| **Polar** | Click `alice` → Invoices tab |
| **CLI** | See below |

**CLI check (from Alice's node):**

```bash
docker exec polar-n3-alice lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices
```
Look for the invoice with `amt: 50000` and `state: OPEN` (hold invoice accepted but unsettled).

---

## Part 4 — Pay the Invoice (Bob = Client)

Copy the **hold invoice** (`lnbcrt...`) from the Ganji deal page, then:

```bash
docker exec polar-n3-bob lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  payinvoice <paste_hold_invoice_here>
```

> **Important:** pay the **deal's hold invoice** (from the deal page), NOT the payee invoice you pasted into the form.

### Watch what happens

```bash
# LNbits logs — no webhook on LND holds (see Part 5.0)
docker logs ganji-lnbits-1 --tail 20

# Deal status in Ganji (refresh the deal page)
# On LND-backed LNbits the status STAYS awaiting_payment here — that is expected (see Part 5.0)
```

The sats are now **held at Alice** — not settled yet. Bob's channel balance dropped, but Alice hasn't actually received spendable funds.

**LNbits UI shows the hold, but not the LND state:**
- Wallet balance stays `0 sats` (hold not settled).
- Transactions shows `Test Deal Polar — 50,000 — pending`.
- There is no `ACCEPTED` / HTLC label in the LNbits UI. `pending` there means "not settled yet"; the definitive proof of Alice holding the HTLC is Polar/LND below.

**Verify Alice is holding it (this is the definitive check):**

```bash
docker exec polar-n3-alice lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices | grep -A 8 '"r_hash": "94cc8d6f'
```

You want `state: ACCEPTED`, `amt_paid_sat: 50000`, `settle_date: 0`. That means Alice accepted the HTLC but has not revealed the preimage. In Polar GUI: **network 3 → alice → Invoices**, and **Channels → Bob's 500k channel → pending incoming HTLC 50,000**.

---

## Part 5 — Approve / Release (Escrow Settlement)

### 5.0 Why the deal still says `awaiting_payment` (and no webhook fired)

This is **intentional for LND-backed LNbits**, not a bug:

- LNbits keeps a held invoice at `paid: false`, `status: pending` until the preimage is revealed by `settle`.
- Ganji's webhook/poll only transitions `awaiting_payment → locked` when `paid: true` (that's CLN behaviour; CLN reports held invoices as paid).
- With LND, the deal therefore stays `awaiting_payment` while the money is genuinely held at Alice. The **settle on approve** is what atomically proves the funds were held.

So do **not** change code to force `locked` from a pending hold. Follow the normal flow instead:

### 5.1 Freelancer submits work (`awaiting_payment → work_submitted`)

`SubmitWork` is allowed from `awaiting_payment`, but it requires at least one artifact. In the frontend, log in as the freelancer (`polar@test.com`), open the deal, and use the **Deliverables** panel:

1. Paste a work link into **Work link or delivery note** and click **Attach Work Link**, or upload a file with **Upload File**.
2. Confirm the artifact appears in the deliverables list.
3. Click **Submit Work**. The button unlocks only after at least one artifact is attached.

Check the deal page: status should now be `work_submitted`. The client share page shows the same single-step deal as one “Final delivery” milestone, so the submitted work is visible even though no milestone rows were created.

### 5.2 Client approves (`work_submitted → released`)

Log in as the client (`local@test.com`) and use the **approve/release** action on the deal page, or call:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $CLIENT_TOKEN" \
  http://localhost:8080/deals/$DEAL_ID/approve
```

Behind the scenes:

1. Ganji calls LNbits `/api/v1/payments/settle` with the preimage → held sats settle into escrow
2. Ganji calls `PayInvoice(payee_invoice)` → pays out to the payee (Bob in simple mode, Carol in Part 6)
3. Deal status → `released`

Verify:

```bash
docker exec polar-n3-alice lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices
```
The hold invoice should now show `state: SETTLED`.

---

## Part 6 — Realistic 3-Wallet Experiment (Bob → Alice → Carol)

This mode gives a clean, realistic escrow demo: **Bob pays, Alice holds, Carol receives** — three separate wallets, no money looping back to the same node.

### 6.1 What We're Building

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│    BOB      │  pays   │   ALICE     │  pays   │   CAROL     │
│  (client)   │────────▶│  (escrow)   │────────▶│ (freelancer)│
└─────────────┘         └─────────────┘         └─────────────┘
   sends sats            holds sats              receives sats
```

### 6.2 Add Carol to Polar

**Using Polar GUI (recommended):**
1. Open the Polar desktop app
2. Select network 3
3. Click **"Add Node"** → choose **LND**
4. Name it `carol`
5. Connect it to the existing bitcoind backend (`polar-n3-backend1`)
6. Click **Start**

**Or via CLI** — add a `carol` service to Polar's compose file:

```bash
# See the existing alice/bob services in ~/.polar/networks/3/docker-compose.yml
# Copy the bob service block, rename: bob → carol, ports 8082→8083, 10002→10003, 9736→9737
# Then:
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d carol
```

> ⚠️ Editing Polar's compose file while Polar GUI is open may cause conflicts. Prefer the GUI method.

### 6.3 Fund Carol (same maturity rules apply!)

```bash
# Get Carol's address
docker exec polar-n3-carol lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  newaddress p2wkh

# Mine to it + mature
CAROL_ADDRESS="paste_carols_address"
for i in $(seq 1 5); do
  docker exec polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    generatetoaddress 1 "$CAROL_ADDRESS"
done
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 100
```

### 6.4 Open a channel Carol → Alice (so Alice can pay Carol)

Bob→Alice channel already exists (Part 2.3). Now we need **Alice→Carol** so escrow can pay out:

```bash
# Get Carol's pubkey
CAROL_PUBKEY=$(docker exec polar-n3-carol lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  getinfo | grep identity_pubkey | cut -d'"' -f4)

# Open channel Alice → Carol (Alice has plenty of funds)
docker exec polar-n3-alice lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  openchannel "$CAROL_PUBKEY" 500000

# Mine 6 confirmations
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 6
```

### 6.5 Generate the Payee Invoice from Carol

```bash
docker exec polar-n3-carol lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  addinvoice --amt=50000 --memo="freelancer payout to Carol"
```
Copy the `payment_request` (starts `lnbcrt...`) — paste this into Ganji's `payee_invoice` field.

### 6.6 Run the Flow

1. **Create deal** in Ganji with Carol's payee invoice
2. **Pay the hold invoice** from Bob (same as Part 4)
3. **Approve** in Ganji → escrow settles → Alice pays Carol's invoice
4. **Verify Carol received:**
```bash
docker exec polar-n3-carol lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  listinvoices | grep -A 5 "settled"
```

**Result:** Bob's balance went down, Carol's went up, Alice acted as pure intermediary. Clean three-way escrow demo. ✅

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
| 2. Wait for LND sync | Poll `getinfo` until `synced_to_chain: true` | ✅ |
| 3. Mine fresh block | `bitcoin-cli -generate 1` | ✅ |
| 4. Start LNbits | `docker compose -f lnbits-docker-compose.yml up -d` | ✅ |
| 5. Start backend | `go run ./cmd/api &` + poll `/health` | ✅ |
| 6. Start frontend | `npm run dev &` + poll port 3000 | ✅ |
| 7. Fund Bob | `newaddress` → `generatetoaddress` → mine 100 → check balance | ✅ |
| 8. Open channels | `openchannel` Bob→Alice, Alice→Carol → mine 6 | ✅ |
| 9. Create deal via API | `curl POST /auth/signup` + `curl POST /deals` | ✅ |
| 10. Parse hold invoice | `jq` the response | ✅ |
| 11. Pay from Bob | `lncli payinvoice` | ✅ |
| 12. Approve deal | `curl POST /deals/:id/approve` | ✅ |
| 13. Verify | Check all balances | ✅ |

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
- **Error handling:** fail fast with clear messages (e.g., "Alice not synced after 60s — check chain tip")
- **Third wallet:** add Carol only when realism matters; skip for quick smoke tests

### Why automate?

- Reproducible demos (same result every time)
- Fast regression testing after code changes
- Easy onboarding for new devs ("just run `make demo`")
- Can later become a CI smoke test

---

## Quick Troubleshooting

| Symptom | Fix |
|---------|-----|
| Alice crash-loops | `docker rm -f polar-n3-alice && docker compose -f ~/.polar/networks/3/docker-compose.yml up -d alice` |
| `synced_to_chain: false` | Mine 1 fresh block (Part 1.2) |
| LNbits says VoidWallet | See doc → "Troubleshooting" — DB settings override env |
| `/first_install` redirect | See doc → "Troubleshooting" |
| `Wallet not found` | Check API keys in DB match `backend/.env` |
| `Invoice key required` | Backend sends `out: false`; manual curls must too |
| Bob can't pay | Check channel active (`listchannels`) and Bob has sats (`walletbalance`) |
| Bob funded but balance unchanged | Coinbase maturity — mine 100 more blocks (see Part 2.2 explanation) |
| Deal still `awaiting_payment` after Bob paid | Expected on LND: hold stays `paid:false`. Check Alice invoice `ACCEPTED`; then freelancer submits work, client approves (Part 5.0–5.2) |
| Approve returns `cannot be approved from status awaiting_payment` | Run Part 5.1 first (`SubmitWork` moves it to `work_submitted`) |
| Carol can't receive payout | Need Alice→Carol channel active, and Carol's payee invoice amount must be ≤ channel capacity |

---

## Cheat Sheet — LND CLI Shortcut

To avoid typing the long flags every time, set a shell variable first:

```bash
BOB="lncli --network=regtest --tlscertpath=/home/lnd/.lnd/tls.cert --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon"
docker exec polar-n3-bob sh -c "$BOB walletbalance"
docker exec polar-n3-bob sh -c "$BOB payinvoice <bolt11>"
```

Same works for `polar-n3-alice` by swapping the container name.

---

**Full technical background:** `docs/POLAR_LNBITS_SETUP.md`
