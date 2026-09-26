# Environment Variables

See `backend/.env.example`-style vars in `API_REFERENCE.md` / `README.md`; the
critical ones:

## Backend (Required)

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string (e.g., `postgres://postgres:postgres@localhost:5432/ganji?sslmode=disable`) |
| `JWT_SECRET` | Access token signing secret (HS256) |
| `JWT_REFRESH_SECRET` | Refresh token signing secret (HS256) |
| `LNBITS_URL` | LNbits instance URL (e.g., `https://lnbits.example.com`) |
| `LNBITS_API_KEY` | LNbits **invoice key** (read/create invoices) |
| `LNBITS_ADMIN_KEY` | LNbits **admin key** — required for settle/cancel/payout (money moves) |
| `LNBITS_WEBHOOK_SECRET` | Wallet webhook secret for `LNbits-Signature` verification |
| `WEBHOOK_URL` | Public URL for LNbits to POST payment notifications (e.g., `https://api.ganji.app/webhooks/lnbits`) |
| `FRONTEND_URL` | CORS origin (e.g., `http://localhost:3000`) |
| `OPERATOR_EMAILS` | Comma-separated emails promoted to arbitration operators at boot (`is_operator` claim). Empty by default; no operator means disputes can be raised but not resolved. |

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

## Frontend (Required)

| Variable | Description |
|---|---|
| `NEXT_PUBLIC_API_URL` | Backend API base URL (e.g., `http://localhost:8080`) |
| `NEXT_PUBLIC_WS_URL` | WebSocket URL for real-time updates (not built yet) |

## Notes

- **For Lightning testing**: Run LNbits against a Bitcoin Core regtest node — do not point dev environments at mainnet.
- Any hold invoice drawn to a given wallet belongs to that wallet's pool, so use the dedicated Ganji wallet/keys and never paste an admin key somewhere public.
- OpenTimestamps anchoring of CV entries is on by default against the public calendar pools; set `OTS_ESPLORA_URL` to also verify proofs against the live Bitcoin chain.