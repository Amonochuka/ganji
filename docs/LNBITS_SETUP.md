# LNbits Setup for Ganji

Ganji uses **LNbits** as the Lightning wallet backend. You need a wallet with two API keys:

Users get these values from their own LNbits instance: the base URL (for example,
`http://localhost:5000`) and the key shown in that LNbits wallet's API keys screen.
Exchange wallets such as Binance are not LNbits wallets and cannot be used for the
linked-wallet buttons because they do not expose these endpoints or keys.

| Key | Purpose | Ganji Config |
|---|---|---|
| **Invoice Key** | Creates hold invoices (read-only) | `LNBITS_API_KEY` |
| **Admin Key** | Settles, cancels, pays out (money moves) | `LNBITS_ADMIN_KEY` |

> ⚠️ **The admin key is required.** The invoice key alone cannot settle/cancel holds or pay out.

> **Escrow node.** LNbits must be backed by `polar-n3-Es` (Alice1/Es). The client is Bob/`polar-n3-Cl`; the freelancer is `polar-n3-Fre`. `polar-n3-Pay` is route-only Es → Fre if needed, not a second Ganji wallet. The Alice2/relay workflow was reverted and is not current.
>
> `locked` (shown as **Funds Held**) requires direct LND accepted-hold verification on Es — LNbits `paid=true` is not proof of funds and no longer locks a deal. The backend also needs `LND_REST_URL` / `LND_REST_CERT_PATH` / `LND_REST_MACAROON_PATH` pointing at Es; without them `FundDeal` returns `503`.

---

## Prerequisites

### PostgreSQL Database
```bash
# Quick Docker PostgreSQL
docker run -d --name ganji-db \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=ganji \
  -p 5432:5432 \
  postgres:16
```

Or use any PostgreSQL 14+ instance. Update `DATABASE_URL` in `.env` accordingly.

---

## Option 1: LNbits Demo (Unsupported for Escrow)

Generic demo LNbits (or an LNbits/ngrok setup) is **unsupported** for the
current escrow flow unless it can provide the Es LND REST accepted-hold
verification described below. A public demo has no access to the Es TLS
certificate/macaroon, and LNbits `paid=true` or a webhook alone cannot lock a
deal.

**Caveats:**
- Demo is on testnet/mainnet — real sats move
- Webhooks need a public URL (use ngrok: `ngrok http 8080`), but a URL is not proof of held funds
- No Es LND REST checker means deals stay `awaiting_payment` and `FundDeal` returns `503`
- Shared instance — don't put real funds

---

## Option 2: Polar Es + Self-Hosted LNbits (Supported Dev Setup)

### Prerequisites
- Docker
- Polar network 3 running `polar-n3-Es` (Alice1/Es), `polar-n3-Cl` (Bob/client), `polar-n3-Fre` (freelancer), and optionally `polar-n3-Pay` for Es → Fre routing
- The `lnbits-docker-compose.yml` in this repo

### 1. Start Polar and Es
```bash
docker compose -f ~/.polar/networks/3/docker-compose.yml up -d
# Verify in the Polar GUI or with: docker ps | grep polar-n3-Es
```

### 2. Start LNbits (Backed by Es)
```bash
docker compose -f lnbits-docker-compose.yml up -d
```

Relevant wiring from `lnbits-docker-compose.yml`:
```yaml
services:
  lnbits:
    image: lnbits/lnbits:latest
    environment:
      - LNBITS_BACKEND_WALLET_CLASS=LndRestWallet
      - LND_REST_ENDPOINT=https://polar-n3-Es:8080
      - LND_REST_CERT=/certs/tls.cert
      - LND_REST_MACAROON=/certs/admin.macaroon
    volumes:
      - /home/amonoff/.polar/networks/3/volumes/lnd/Es/tls.cert:/certs/tls.cert:ro
      - /home/amonoff/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon:/certs/admin.macaroon:ro
    networks:
      - polar-network
```

Do not point LNbits at Alice2/relay or add a second Ganji wallet; that workflow
was reverted. `polar-n3-Pay`, if used, is only a route hop Es → Fre.

### 3. Get Keys
1. Open LNbits at `http://localhost:5000`
2. Create wallet
3. Settings → API Keys → copy both keys
4. Set in `.env` (values from `backend/.env.example`):
   ```env
   LNBITS_URL=http://localhost:5000
   LNBITS_API_KEY=change-me-lnbits-invoice-key
   LNBITS_ADMIN_KEY=change-me-lnbits-admin-key
   LNBITS_WEBHOOK_SECRET=
   WEBHOOK_URL=http://localhost:8080/webhooks/lnbits
   # Direct backend checker on the same Es node
   LND_REST_URL=https://localhost:8082
   LND_REST_CERT_PATH=/home/amonoff/.polar/networks/3/volumes/lnd/Es/tls.cert
   LND_REST_MACAROON_PATH=/home/amonoff/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon
   ```
   Set all three `LND_REST_*` values or none. Without them `FundDeal` returns `503`.

### 4. Fund the Wallet (Regtest)
```bash
# Generate a block if the chain tip is stale (Polar GUI actions mine automatically)
docker exec polar-n3-backend1 bitcoin-cli -regtest \
  -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 -generate 1
```

---

## Option 3: Your Own LNbits Instance (Conditional)

If you already run LNbits (VPS, Umbrel, Start9, etc.), it is supported only if
it is backed by the same `polar-n3-Es` LND REST node and you can provide the Es
TLS/macaroon paths to the backend checker. Generic demo/remote instances without
that verification are unsupported for the current escrow flow.

1. Create dedicated wallet for Ganji
2. Copy Invoice Key + Admin Key
3. Set `LNBITS_URL` to your instance and `LND_REST_ENDPOINT` to `https://polar-n3-Es:8080`
4. Set `LND_REST_URL` / `LND_REST_CERT_PATH` / `LND_REST_MACAROON_PATH` to the Es values from `backend/.env.example`
5. Ensure webhook URL is publicly accessible — but remember it is not proof of funds

---

## Webhook Configuration

Ganji receives payment notifications via LNbits webhooks. Webhooks are
best-effort only: both the webhook and `CheckPayment` require LND accepted-hold
verification on Es (matching hash/amount, `State == ACCEPTED`, `settled == false`,
`settle_date == 0`). LNbits `paid=true` alone does not lock a deal.

### Required
```env
WEBHOOK_URL=https://your-domain.com/webhooks/lnbits
LNBITS_WEBHOOK_SECRET=your-webhook-secret  # from LNbits wallet settings
```

### Local Development with ngrok
```bash
# Terminal 1: Start backend
cd backend && go run ./cmd/api

# Terminal 2: Expose port 8080
ngrok http 8080
# Copy the https URL (e.g., https://abc123.ngrok.io)
# Set WEBHOOK_URL=https://abc123.ngrok.io/webhooks/lnbits
```
ngrok only makes the webhook reachable; it does not provide accepted-hold
verification. Without the Es LND checker the deal stays `awaiting_payment`.

### In LNbits Wallet Settings
1. Extensions → **Webhooks** → Enable
2. Add webhook:
   - URL: `https://your-ngrok-url/webhooks/lnbits`
   - Secret: generate random string → put in `LNBITS_WEBHOOK_SECRET`

---

## Payee Invoice (Freelancer Payout)

The freelancer generates a payout invoice through the protected
`POST /deals/wallet/invoice` endpoint with
`{wallet_url, wallet_key, amount_sats, memo?}`; the backend fills the deal's
`payee_invoice` field. This is the freelancer's own wallet (for example
`polar-n3-Fre`), not the escrow wallet. On approve/release, Ganji reveals `R`
on Es and pays this invoice directly; `polar-n3-Pay`, if used, is only a route
hop Es → Fre.

**For testing:** Use the freelancer's linked Lightning wallet (e.g., the
`polar-n3-Fre` node) to create the invoice. Wallet URLs must be `http`/`https`
with a host and no userinfo/query/fragment; there is no allowlist. The wallet
key is used transiently and is never stored or logged.

```bash
# Example payee_invoice format
lnbc50000u1p3xyz...  # bolt11 invoice from freelancer's wallet
```

---

## Funding a Deal (Client Wallet)

The client funds from their linked wallet (Bob/`polar-n3-Cl`) through public
`POST /public/deals/:shareToken/fund` with
`{wallet_url, wallet_key, milestone_id?}`. No JWT is required — the share token
is the authorization. `milestone_id` is optional; omit it to fund the first
open milestone (or the legacy deal-level hold).

The backend pays the hold invoice transiently using the wallet admin key, then
requires the Es LND checker to confirm an accepted hold. Only after that does
the deal become `locked` (**Funds Held**). Work submission requires `locked`;
`awaiting_payment → work_submitted` is invalid. If the checker is missing, the
funding request returns `503` and the deal stays `awaiting_payment`.

Wallet URLs must be `http`/`https` with a host, no userinfo/query/fragment, and
there is no allowlist. Wallet keys are used for that request only — never
stored or logged. Rejecting work after `work_submitted` does not reveal `R` and
does not settle/cancel/fail the HTLC; the arbiter reveals `R` to pay Fre or
cancels/waits for expiry to refund Bob. A refund does not use `R`.

---

## Complete `.env` Example

```env
# Database
DATABASE_URL=postgres://postgres:postgres@localhost:5432/ganji?sslmode=disable

# Auth
JWT_SECRET=super-secret-access-key-change-in-production
JWT_REFRESH_SECRET=super-secret-refresh-key-change-in-production

# LNbits (backed by Es in Polar)
LNBITS_URL=http://localhost:5000
LNBITS_API_KEY=change-me-lnbits-invoice-key
LNBITS_ADMIN_KEY=change-me-lnbits-admin-key
LNBITS_WEBHOOK_SECRET=
WEBHOOK_URL=http://localhost:8080/webhooks/lnbits

# Direct Es LND REST checker (required to lock deals)
LND_REST_URL=https://localhost:8082
LND_REST_CERT_PATH=/home/amonoff/.polar/networks/3/volumes/lnd/Es/tls.cert
LND_REST_MACAROON_PATH=/home/amonoff/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon

# Storage
STORAGE_PATH=./uploads
MAX_UPLOAD_BYTES=10485760

# Frontend
FRONTEND_URLS=http://localhost:3000

# Email (optional)
SMTP_HOST=
SMTP_PORT=587
SMTP_USER=
SMTP_PASS=
SMTP_FROM=noreply@ganji.local
SMTP_ENCRYPTION=starttls

# OTS (optional)
OTS_ESPLORA_URL=https://blockstream.info/api
OTS_UPGRADE_INTERVAL_SECONDS=21600

# Operators (optional)
OPERATOR_EMAILS=admin@yourdomain.com
```

---

## Quick Test Flow

```bash
# 1. Start backend (with the Es LND_REST_* vars set)
cd backend && go run ./cmd/api

# 2. Signup freelancer
curl -X POST http://localhost:8080/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"freelancer@test.com","password":"password123","display_name":"Test Freelancer"}'

# 3. Login → get access_token
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"freelancer@test.com","password":"password123"}'

# 4. Generate the freelancer payout invoice (protected); backend fills payee_invoice
curl -X POST http://localhost:8080/deals/wallet/invoice \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{"wallet_url":"http://localhost:5000","wallet_key":"<fre-wallet-admin-key>","amount_sats":50000,"memo":"Test Deal"}'

# 5. Create deal (use the bolt11 from step 4)
curl -X POST http://localhost:8080/deals \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{"title":"Test Deal","amount_sats":50000,"source_platform":"telegram","client_email":"client@test.com","payee_invoice":"lnbc50000u1p3xyz..."}'

# 6. Get public link from response → open in browser
#    http://localhost:3000/public/deals/<share_token>

# 7. Client funds from linked wallet (public; no JWT)
curl -X POST http://localhost:8080/public/deals/<share_token>/fund \
  -H "Content-Type: application/json" \
  -d '{"wallet_url":"http://localhost:5000","wallet_key":"<client-wallet-admin-key>"}'

# 8. Backend pays the hold invoice from that wallet, verifies the accepted hold
#    on Es, then locks the deal (shown as "Funds Held"). Work submission
#    requires locked; awaiting_payment → work_submitted is invalid.

# 9. Freelancer submits work → client approves → R is revealed and Fre is paid
```

---

## Troubleshooting

| Issue | Fix |
|---|---|
| `LNBITS_ADMIN_KEY` invalid | Must be **admin key**, not invoice key |
| Webhook not received | Check `WEBHOOK_URL` is reachable; check `LNBITS_WEBHOOK_SECRET` matches. A webhook alone still cannot lock the deal |
| Deal stays `awaiting_payment` after payment | Expected unless Es reports an accepted hold; `paid=true` is not proof |
| `FundDeal` returns `503` | Set all three `LND_REST_*` values for Es; verification is unavailable without them |
| Hold invoice not created | Verify `LNBITS_API_KEY` and `LNBITS_URL`, and that LNbits uses `https://polar-n3-Es:8080` |
| Funding rejected as invalid URL/key | Wallet URL must be `http`/`https` with host, no userinfo/query/fragment; no allowlist; key must be non-empty |
| Payout fails | Escrow wallet admin key needs balance; check route Es → Fre (`polar-n3-Pay` is route-only if needed) |
| `context deadline exceeded` | Increase `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` or check LNbits/LND REST connectivity |

---

## Security Notes

- **Never commit `.env`** — it's in `.gitignore`
- **Admin key = full wallet control** — treat like a password
- **Linked wallet keys are transient** — used for one request, never stored or logged; no URL allowlist, so validate the URL yourself
- **Use dedicated wallet** for Ganji, not your personal wallet
- **Regtest for development** — no real funds at risk
- **Rotate keys** if exposed