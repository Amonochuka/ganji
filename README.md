# Ganji

Bitcoin Lightning escrow + an unfakeable, cryptographically anchored freelancer reputation layer.

Ganji ("money" in Sheng) lets a freelancer and a client transact safely over any channel — WhatsApp, Telegram, X, LinkedIn DM — with no platform lock-in and no bank account required. Funds are held in a Lightning invoice until the client reviews the delivered work in a sandboxed preview and approves release. Every completed deal becomes a permanent, hash-verified entry on the freelancer's public **Live CV**.

Full product documentation — user flows, dispute handling, API contract, database schema, and the phased build plan — lives in [`docs/Ganji-Build-Documentation.docx`](./docs/Ganji-Build-Documentation.docx). Read that before writing code. This README only covers setup and repo layout.

---

## Repo Structure

See [`docs/FOLDER_STRUCTURE.txt`](./docs/FOLDER_STRUCTURE.txt) for the full tree. Summary:

```
ganji/
├── backend/                 # Go API — owned by Backend Eng A & B
│   ├── cmd/
│   │   └── api/             # main.go — entrypoint, server bootstrap
│   ├── internal/
│   │   ├── auth/            # signup, login, JWT, refresh tokens        (Backend A)
│   │   ├── deals/           # deal CRUD, status transitions            (Backend A)
│   │   ├── lightning/       # LNbits client, invoice + webhook         (Backend B)
│   │   ├── websocket/       # real-time deal status push               (Backend B)
│   │   ├── cv/              # public CV endpoints, hash verification   (Backend B)
│   │   ├── db/              # connection pool, query helpers
│   │   ├── middleware/      # auth guard, CORS, rate limiting
│   │   └── config/          # env var loading
│   ├── pkg/
│   │   ├── hash/             # SHA-256 preimage generation (shared logic)
│   │   └── sanitize/         # server-side input sanitization (shared logic)
│   └── migrations/           # golang-migrate SQL files
│
├── frontend/                 # Next.js app — owned by Frontend Eng A & B
│   └── src/
│       ├── app/
│       │   ├── auth/login/          # (Frontend A)
│       │   ├── auth/signup/         # (Frontend A)
│       │   ├── create-deal/         # (Frontend A)
│       │   ├── dashboard/           # Provider Hub               (Frontend A)
│       │   ├── settings/            # Freelancer profile         (Frontend A)
│       │   ├── freelancer/[id]/     # Freelancer submit page     (Frontend A)
│       │   └── live-cv/[slug]/      # Client escrow + public CV  (Frontend B)
│       ├── components/       # EscrowBox, SandboxViewer, VerifiedBlock, etc.
│       ├── lib/               # API client, WebSocket client, sanitize
│       ├── hooks/             # useDeal, useWebSocket, useCVVerify
│       └── types/             # Shared TypeScript types — MUST match backend contract
│
└── docs/
    ├── Ganji-Build-Documentation.docx   # Full spec — read this first
    ├── FOLDER_STRUCTURE.txt              # Reference tree
    └── SCAFFOLD_COMMANDS.md              # Terminal commands to recreate this structure
```

---

## Team Ownership

| Dev | Owns |
|---|---|
| **Backend A** | Auth, user accounts, deal CRUD, PostgreSQL schema & migrations |
| **Backend B** | LNbits integration, webhook handling, WebSocket server, CV verification endpoints |
| **Frontend A** | Create Deal, Freelancer Dashboard, auth pages, Provider Hub, Settings (all freelancer-facing screens) |
| **Frontend B** | Live CV page, client escrow state machine, sandbox viewer, CV verification UI (all client-facing screens) |

The split runs along natural seams: backend splits between pure CRUD (predictable, testable alone) and external-system integration (Lightning, webhooks — more moving parts, needs a dedicated owner). Frontend splits along user role, matching the product itself — freelancer screens barely share state with client screens, so this avoids merge conflicts.

**Before writing any code**, all 4 devs must agree on the API contract (Section 10 of the doc — request/response shapes for `/deals`, `/cv/:slug`, and the WebSocket event format). Once that's locked, all 4 people build in parallel without blocking each other.

---

## Build Order

Follow Section 8 of the doc in order. Do not skip ahead or work out of sequence — each phase depends on the previous one being functional:

1. **Foundation** — DB schema, auth, basic layout (Week 1–2)
2. **Core Escrow Flow** — deal CRUD, freelancer submit, client state machine, WebSocket (Week 3–5)
3. **Lightning Integration** — real LNbits invoices, webhook, payment detection (Week 6–7)
4. **Live CV & Verification** — public CV endpoint, hash verification, trust score (Week 8–9)
5. **Polish & Deploy** — sanitization hardening, mobile responsiveness, production deploy (Week 10–12)

Items in Section 9 of the doc ("Roadmap Stage 2 / Stage 3" — M-Pesa, multisig arbiter, file uploads, etc.) are **not part of this build**. They come after Phase 5 ships and the product is live. Do not build them now, and do not ignore them either — they're committed roadmap, just sequenced for after launch.

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

All schema changes go through `golang-migrate`. Never edit a migration that has already been applied to a shared environment — write a new one.

```bash
migrate create -ext sql -dir backend/migrations -seq <description>
```

---

## Git Workflow

- Feature branches off `main`, named `<dev-initials>/<short-description>` (e.g. `ao/lightning-webhook`)
- PRs require one review before merge
- Never force-push a shared branch
- Conventional commit messages (`feat:`, `fix:`, `chore:`, `docs:`)

---

## Status

🚧 Pre-launch. Following the 5-phase build plan in `docs/Ganji-Build-Documentation.docx`, Section 8.

Built by the Ganji team — Zone01 Kisumu.
