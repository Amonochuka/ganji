# Ganji

Bitcoin Lightning escrow + an unfakeable, cryptographically anchored freelancer reputation layer.

Ganji ("money" in Sheng) lets a freelancer and a client transact safely over any channel — WhatsApp, Telegram, X, LinkedIn DM — with no platform lock-in and no bank account required. Escrow is drawn directly on Lightning: Ganji creates a `hold invoice` the client pays into, funds stay held on the network, and only the client's approval settles + forwards them to the freelancer. If the client disputes the work, the funds **freeze** and an operator resolves the dispute (release to the freelancer, or refund to the client). Every completed deal becomes a permanent, hash-verified entry on the freelancer's public **Live CV**.

---

## Documentation

The living docs are the source of truth. The original product build document (a `.docx`) is **not** part of this repo.

| Doc | What's in it |
|---|---|
| [`explained.md`](./explained.md) | Architecture walkthrough: money paths, state machine, escrow design, tests |
| [`API_REFERENCE.md`](./API_REFERENCE.md) | Every endpoint, request/response shapes, error codes |
| [`BACKEND_STATUS.md`](./BACKEND_STATUS.md) | Build status, what's done, what's not |
| [`CONTRIBUTING.md`](./CONTRIBUTING.md) | Team ownership, build order, git conventions |

---

## Repo Structure

```
ganji/
├── backend/      # Go API (Gin + PostgreSQL + LNbits)
├── frontend/     # Next.js app
└── *.md          # docs live at the repo root
```

Each package's purpose is documented in [`explained.md`](./explained.md).

---

## Local Development

### Backend

```bash
cd backend
cp .env.example .env        # fill in DATABASE_URL, JWT secrets, LNBITS_* vars
go mod download
migrate -path migrations -database "$DATABASE_URL" up
go run ./cmd/api
```

Backend runs on `http://localhost:8080` by default.

### Frontend

```bash
cd frontend
cp .env.example .env.local   # set NEXT_PUBLIC_API_URL and NEXT_PUBLIC_WS_URL
npm install
npm run dev
```

Frontend runs on `http://localhost:3000`.

### Required environment variables

At minimum the backend needs `DATABASE_URL`, `JWT_SECRET`, `JWT_REFRESH_SECRET`, `LNBITS_URL`, `LNBITS_API_KEY`, and `LNBITS_ADMIN_KEY`. `LNBITS_ADMIN_KEY` is the router's **admin** key — it is required for the escrow money moves (settle/cancel/payout); the invoice key alone can only create the hold invoice. `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` (default 30 days) must comfortably exceed a deal's lifetime. `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS` (default 6 hours) controls how often a background job reconciles stale open deals against LNbits. If webhook signing is enabled in LNbits, set `LNBITS_WEBHOOK_SECRET` to the wallet's webhook secret so the backend verifies the `LNbits-Signature` header. `OPERATOR_EMAILS` (comma-separated) promotes those accounts to arbitration operators at boot. Frontend needs `NEXT_PUBLIC_API_URL` pointed at the running backend; `NEXT_PUBLIC_WS_URL` is for real-time updates (the WebSocket server is not built yet).

See `backend/.env.example` and `frontend/.env.example` for the full annotated list. For Lightning testing, run LNbits against a Bitcoin Core regtest node — do not point dev environments at mainnet. Any hold invoice drawn to a given wallet belongs to that wallet's pool, so use the dedicated Ganji wallet/keys and never paste an admin key somewhere public.

---

## Database Migrations

```bash
migrate create -ext sql -dir backend/migrations -seq <description>
```

Never edit a migration already applied to a shared environment — write a new one.

---

## Status

🚧 Pre-launch. See [`BACKEND_STATUS.md`](./BACKEND_STATUS.md) for what's built and [`CONTRIBUTING.md`](./CONTRIBUTING.md) for workflow and team ownership.
