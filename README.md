# Ganji

Bitcoin Lightning escrow + an unfakeable, cryptographically anchored freelancer reputation layer.

Ganji ("money" in Sheng) lets a freelancer and a client transact safely over any channel — WhatsApp, Telegram, X, LinkedIn DM — with no platform lock-in and no bank account required. Funds are held in a Lightning invoice until the client reviews the delivered work in a sandboxed preview and approves release. Every completed deal becomes a permanent, hash-verified entry on the freelancer's public **Live CV**.

Full product spec lives in [`docs/Ganji-Build-Documentation.docx`](./docs/Ganji-Build-Documentation.docx). Team workflow and build order live in [`docs/CONTRIBUTING.md`](./docs/CONTRIBUTING.md).

---

## Repo Structure

```
ganji/
├── backend/      # Go API
├── frontend/     # Next.js app
└── docs/         # Spec, folder reference, contributing guide
```

See [`docs/FOLDER_STRUCTURE.txt`](./docs/FOLDER_STRUCTURE.txt) for the full tree.

---

## Local Development

### Backend

```bash
cd backend
cp .env.example .env        # fill in DATABASE_URL, JWT secrets, LNBITS_* vars
go mod download
migrate -path migrations -database "$DATABASE_URL" up
go run cmd/api/main.go
```

Backend runs on `http://localhost:8080` by default.

### Frontend

```bash
cd frontend
cp .env.local.example .env.local   # set NEXT_PUBLIC_API_URL and NEXT_PUBLIC_WS_URL
npm install
npm run dev
```

Frontend runs on `http://localhost:3000`.

### Required environment variables

See Section 11 of the build documentation for the full list. At minimum, backend needs `DATABASE_URL`, `JWT_SECRET`, `JWT_REFRESH_SECRET`, `LNBITS_URL`, `LNBITS_API_KEY`. Frontend needs `NEXT_PUBLIC_API_URL` pointed at the running backend.

For Lightning testing, run LNbits against a Bitcoin Core regtest node — do not point dev environments at mainnet.

---

## Database Migrations

```bash
migrate create -ext sql -dir backend/migrations -seq <description>
```

Never edit a migration already applied to a shared environment — write a new one.

---

## Status

🚧 Pre-launch. See `docs/CONTRIBUTING.md` for build order and team ownership.
