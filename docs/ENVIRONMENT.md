# Environment Variables

See `backend/.env.example` for the authoritative list; the critical ones:

## Backend (Required)

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string (e.g., `postgres://postgres:postgres@localhost:5432/ganji?sslmode=disable`) |
| `JWT_SECRET` | Access token signing secret (HS256); must be at least 32 characters |
| `JWT_REFRESH_SECRET` | Refresh token signing secret (HS256); must be at least 32 characters |
| `LNBITS_URL` | LNbits instance URL, backed by the Es LND node (e.g., `http://localhost:5000`) |
| `LNBITS_API_KEY` | LNbits **invoice key** (read/create invoices) |
| `LNBITS_ADMIN_KEY` | LNbits **admin key** — required for settle/cancel/payout (money moves) |
| `LNBITS_WEBHOOK_SECRET` | Wallet webhook secret for `LNbits-Signature` verification |
| `WEBHOOK_URL` | Public URL for LNbits to POST payment notifications (e.g., `http://localhost:8080/webhooks/lnbits`) |
| `LND_REST_URL` | Direct LND REST endpoint on the escrow node Es (e.g., `https://localhost:8082`) |
| `LND_REST_CERT_PATH` | Es TLS certificate path (e.g., `/home/amonoff/.polar/networks/3/volumes/lnd/Es/tls.cert`) |
| `LND_REST_MACAROON_PATH` | Es admin macaroon path (e.g., `/home/amonoff/.polar/networks/3/volumes/lnd/Es/data/chain/bitcoin/regtest/admin.macaroon`) |
| `FRONTEND_URLS` | CORS origins, comma-separated (e.g., `http://localhost:3000`). Singular `FRONTEND_URL` still accepted as a fallback. |
| `OPERATOR_EMAILS` | Comma-separated emails promoted to arbitration operators at boot (`is_operator` claim). Empty by default; no operator means disputes can be raised but not resolved. |

Escrow holds live on `polar-n3-Es` (Alice1/Es). The client is Bob/`polar-n3-Cl`,
the freelancer is `polar-n3-Fre`, and `polar-n3-Pay` is route-only Es → Fre if
needed, not a second Ganji wallet. The Alice2/relay workflow was reverted and is
not current. Set all three `LND_REST_*` values or none; the backend uses them to
verify accepted holds directly on Es, while LNbits is configured with
`LND_REST_ENDPOINT=https://polar-n3-Es:8080`. Without the checker, deals cannot
be verified/locked through the new funding flow and `FundDeal` returns `503`.
`locked` (shown as **Funds Held**) requires matching hash/amount, `State == ACCEPTED`,
`settled == false`, and `settle_date == 0`; LNbits `paid=true` is not proof of
funds. Both the webhook and `CheckPayment` require accepted-hold verification.

Linked-wallet endpoints are public `POST /public/deals/:shareToken/fund`
(`{wallet_url, wallet_key, milestone_id?}`) and protected
`POST /deals/wallet/invoice` (`{wallet_url, wallet_key, amount_sats, memo?}`).
Wallet URLs must be `http`/`https` with a host and no userinfo/query/fragment;
there is no allowlist. Wallet keys are used transiently, never stored or logged.

### Quick PostgreSQL (Docker)
```bash
docker run -d --name ganji-db \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=ganji \
  -p 5432:5432 \
  postgres:16
```
Then use `DATABASE_URL=postgres://postgres:postgres@localhost:5432/ganji?sslmode=disable`

## Backend (Optional / Tuning)

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP server port |
| `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` | `2592000` (30 days) | Hold invoice expiry; must comfortably exceed a deal's lifetime |
| `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS` | `21600` (6 hours) | Background job interval to reconcile stale open deals against LNbits |
| `OTS_ESPLORA_URL` | (empty) | Esplora API URL for live Bitcoin chain verification (e.g., `https://blockstream.info/api`) |
| `OTS_UPGRADE_INTERVAL_SECONDS` | `21600` (6 hours) | OTS proof upgrade worker cadence |
| `STORAGE_PATH` | `./uploads` | Where artifact blobs live on disk |
| `MAX_UPLOAD_BYTES` | `10485760` (10 MB) | Per-artifact size cap |
| `PREIMAGE_ENCRYPTION_KEY` | (empty) | Optional secret used to AES-GCM encrypt escrow preimages at rest; unset disables encryption (dev) |

## Frontend (Required)

| Variable | Description |
|---|---|
| `NEXT_PUBLIC_API_URL` | Backend API base URL (e.g., `http://localhost:8080`) |
| `NEXT_PUBLIC_WS_URL` | WebSocket URL for real-time updates (not built yet) |
| `GANJI_ALLOWED_DEV_ORIGINS` | Optional comma-separated LAN hostnames/IPs allowed to request Next.js dev resources; set when accessing `npm run dev` from another device, then restart the dev server |

## Notes

- **For Lightning testing**: Run LNbits against the Es LND regtest node — do not point dev environments at mainnet.
- Generic demo LNbits/ngrok setups are unsupported unless they can provide Es LND REST accepted-hold verification; a webhook URL or `paid=true` is not enough.
- Any hold invoice drawn to a given wallet belongs to that wallet's pool, so use the dedicated Ganji wallet/keys and never paste an admin key somewhere public.
- OpenTimestamps anchoring of CV entries is on by default against the public calendar pools; set `OTS_ESPLORA_URL` to also verify proofs against the live Bitcoin chain.