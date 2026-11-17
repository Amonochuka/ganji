# Tomorrow's Demo Runbook — Full Deal Flow

> Step-by-step from a cold start to a real hold invoice being paid.
> Follow top to bottom. Commands use the repo root `~/Projects/ganji` unless noted.

---

## Part 0 — Concepts (read once, 2 minutes)

```
Bob    = fake CLIENT   (pays the invoice)
Alice  = ESCROW vault  (holds the sats; backs LNbits)
Carol  = fake FREELANCER (optional — we can reuse Bob for now)
```

You mine blocks **only at setup**, not per deal. In production nobody mines — clients bring their own funded wallets.

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

### 2.2 Mine blocks to Bob (fund him with ~1 BTC of fake sats)

```bash
BOB_ADDRESS="paste_the_address_here"
for i in $(seq 1 10); do
  docker exec polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    generatetoaddress 1 "$BOB_ADDRESS"
done
```
Wait ~10s, then check Bob's balance:

```bash
docker exec polar-n3-bob lncli --network=regtest \
  --tlscertpath=/home/lnd/.lnd/tls.cert \
  --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  walletbalance
```
Expect: `total_balance` around 500,000,000 sats.

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
# LNbits logs — webhook fires, payment held
docker logs ganji-lnbits-1 --tail 20

# Deal status in Ganji (refresh the deal page)
# Should go: awaiting_payment → funded / work_submitted
```

The sats are now **held at Alice** — not settled yet. Bob's channel balance dropped, but Alice hasn't actually received spendable funds.

---

## Part 5 — Approve / Release (Escrow Settlement)

In the Ganji deal page, use the **approve/release** action. Behind the scenes:

1. Ganji calls LNbits `/api/v1/payments/settle` with the preimage → held sats settle into escrow
2. Ganji calls `PayInvoice(payee_invoice)` → pays out to Bob's payee invoice (freelancer)
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
