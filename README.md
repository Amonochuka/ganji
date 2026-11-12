# Ganji

Bitcoin Lightning escrow + an unfakeable, cryptographically anchored freelancer reputation layer.

Ganji ("money" in Sheng) lets a freelancer and a client transact safely over any channel — WhatsApp, Telegram, X, LinkedIn DM — with no platform lock-in and no bank account required. Escrow is drawn directly on Lightning: Ganji creates a `hold invoice` the client pays into, funds stay held on the network, and only the client's approval settles + forwards them to the freelancer. If the client disputes the work, the funds **freeze** and an operator resolves the dispute (release to the freelancer, or refund to the client). Every completed deal becomes a permanent, hash-verified entry on the freelancer's public **Live CV**.

**Milestones (sequential per-milestone escrow):** a deal can instead split the job into milestones, each with its own hold invoice — fund M1 → release M1 → fund M2, etc. `released` means paid; `completed` means payout-pending. Post-submission reject/exit freezes the hold for independent review. See [`docs/MILESTONE_REDESIGN_NOTES.md`](./docs/MILESTONE_REDESIGN_NOTES.md).

---

## Documentation

The living docs are the source of truth. The original product build document (a `.docx`) is **not** part of this repo.

| Doc | What's in it |
|---|---|
| [`explained.md`](./explained.md) | Architecture walkthrough: money paths, state machine, escrow design, tests |
| [`docs/API_REFERENCE.md`](./docs/API_REFERENCE.md) | Every endpoint, request/response shapes, error codes |
| [`docs/BACKEND_STATUS.md`](./docs/BACKEND_STATUS.md) | Build status, what's done, what's not |
| [`docs/OTS_ANCHORING.md`](./docs/OTS_ANCHORING.md) | The "permanent hash" explained — plain English + jargon breakdown of the OpenTimestamps anchor/verify pipeline |
| [`docs/CONTRIBUTING.md`](./docs/CONTRIBUTING.md) | Team ownership, build order, git conventions |
| [`docs/FRONTEND_REPORT.md`](./docs/FRONTEND_REPORT.md) | Frontend implementation status, pages, components, API client, remaining work |

### Detailed Guides (`docs/`)

| Guide | What's in it |
|---|---|
| [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md) | Module layout, layering, key files table |
| [`docs/DEAL_LIFECYCLE.md`](./docs/DEAL_LIFECYCLE.md) | State machine, transitions, money-move rules |
| [`docs/MONEY_FLOW.md`](./docs/MONEY_FLOW.md) | Hold invoices, LNbits endpoints, webhook/poll/approve/dispute/arbitration/sweep |
| [`docs/SHARE_LINK.md`](./docs/SHARE_LINK.md) | Share token design, rotation, public endpoint |
| [`docs/IDENTITY_AUTH.md`](./docs/IDENTITY_AUTH.md) | Freelancer vs client vs operator, email matching, endpoint access matrix |
| [`docs/TESTING.md`](./docs/TESTING.md) | Test commands, what each test file covers |
| [`docs/ENVIRONMENT.md`](./docs/ENVIRONMENT.md) | All environment variables with defaults |
| [`docs/KNOWN_ISSUES.md`](./docs/KNOWN_ISSUES.md) | Fixed + remaining bugs, not-built items |
| [`docs/LNBITS_SETUP.md`](./docs/LNBITS_SETUP.md) | Complete LNbits setup (Demo, regtest, own instance), webhook config, curl flow |
| [`docs/MILESTONE_REDESIGN_NOTES.md`](./docs/MILESTONE_REDESIGN_NOTES.md) | Milestone/exit redesign: agreed money rules, implementation status, TODOs |

---

## Repo Structure

```
ganji/
├── backend/      # Go API (Gin + PostgreSQL + LNbits)
├── frontend/     # Next.js app (App Router, TypeScript, Tailwind v4)
├── docs/         # detailed documentation
└── *.md          # root docs (README, CONTRIBUTING, explained, FRONTEND_REPORT)
```

Each package's purpose is documented in [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md).

---

## Frontend Pages (Implemented)

| Route | Page | Description |
|---|---|---|
| `/` | Home | Marketing landing with GNJ stopwatch-logo, hero, feature cards |
| `/login` | Login | Email/password auth, JWT tokens in localStorage |
| `/signup` | Signup | Registration with validation |
| `/dashboard` | Dashboard | Authenticated deal list with status cards, empty state, create CTA |
| `/deals/new` | Create Deal | Form: title, amount (sats), platform dropdown, client email, payee invoice (lnbc) |
| `/deals/[id]` | Deal Detail | Full view, timeline, actions (submit/dispute/rotate/approve), share link, verification |
| `/public/deals/[shareToken]` | Public Deal | Client-facing: invoice to pay, approve flow (no account needed) |

**Core Components:** `Button` (4 variants, 3 sizes), `Input`, `FormBanner`, `AuthShell`, `DealCard`, `format` utils

**API Client:** `lib/api/api-client.ts` — full TypeScript coverage for auth + all deal endpoints

**Design System:** Vault (near-black matte) + Gold accents, Orbitron/Share Tech Mono for GNJ logo, CSS variables for all colors/fonts

---

## Pivot: Offline work and non-software jobs

Ganji does not need to stay limited to software gigs. The same escrow + milestone model can be repurposed for any job where trust is needed, the outcome is visible, and a client wants proof before releasing funds.

### Why this is a natural sideways pivot

The current system already models the right primitives:

- A client pays into escrow and funds stay locked until approval.
- A freelancer/client deal can be split into milestones.
- Each milestone can have artifacts: files, images, notes, receipts, checklists, before/after shots.
- A public history of completed work becomes an anchored reputation layer.

That is enough for jobs such as:

- Construction and renovation work
- Plumbing, electrical, HVAC, repairs, painting
- Delivery, sourcing, and supply runs
- Photography, filming, and event coverage
- Cleaning, moving, and home setup services
- Furniture assembly, installation, and custom fabrication

### Example: image-backed milestone workflow

A home renovation deal could look like this:

1. Client and contractor agree on a total contract.
2. Deal creates a hold invoice for the full amount or for the first milestone.
3. Milestone 1: "Demo + site prep".
   - Contractor uploads 3–5 photos of the site before/after
   - A short note records the exact work done
   - Client approves after reviewing the evidence
4. Milestone 2: "Plumbing + electrical rough-in".
   - Photos + invoice/proof of materials
   - Funds unlock only after client accepts the checkpoint
5. Final milestone: "Final finish + handover".
   - Before/after images, final checklist, and completion notes
   - Final payout settles after approval

This keeps the trust model intact while shifting the subject matter from code to physical, visible work.

### How the product should evolve for offline jobs

The current architecture already supports the key behaviors; it mostly needs broader job metadata and artifact handling:

- `title` and `description` remain the same, but add categories like `construction`, `supply`, `installation`, `delivery`, `repair`
- Add a milestone content model with:
  - `title`
  - `amount_sats`
  - `deliverable_summary`
  - `proof_images[]`
  - `completion_note`
  - `location` or `site_address` (optional)
- Allow uploaded image evidence instead of just text or code-submission artifacts
- Allow client acceptance based on visual proof, not only a software “done” state
- Keep the dispute path: if the client says the milestone is not complete, the hold remains frozen for review
- Keep the public CV or work history, but attach asset proof to the record instead of just text summaries

### Product framing

The product should be positioned as a wider trust layer for work that is hard to verify remotely:

- not just software delivery
- not just remote freelancers
- but any job where progress is visible and money should only move as proof appears

In other words, Ganji becomes a generic escrow marketplace for real-world work, while the software market remains the most obvious initial wedge.

### Compatibility with the current stack

This fits the existing app without a full rewrite:

- The same deals and milestones model still works
- Artifact uploads already exist in the storage layer
- The hold-invoice escrow pattern still applies
- The approval and dispute workflow remains valid
- The public reputation layer can show completed jobs plus proof snapshots and hashes

The main differences are not on the money side; they are in the evidence model. For software, the proof is a commit or ticket closure. For offline work, the proof is a photo, a checklist, a delivery receipt, or a before/after completion record.

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

At minimum the backend needs `DATABASE_URL`, `JWT_SECRET`, `JWT_REFRESH_SECRET`, `LNBITS_URL`, `LNBITS_API_KEY`, and `LNBITS_ADMIN_KEY`. `LNBITS_ADMIN_KEY` is the router's **admin** key — it is required for the escrow money moves (settle/cancel/payout); the invoice key alone can only create the hold invoice. `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` (default 30 days) must comfortably exceed a deal's lifetime. `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS` (default 6 hours) controls how often a background job reconciles stale open deals against LNbits. If webhook signing is enabled in LNbits, set `LNBITS_WEBHOOK_SECRET` to the wallet's webhook secret so the backend verifies the `LNbits-Signature` header. `OPERATOR_EMAILS` (comma-separated) promotes those accounts to arbitration operators at boot. Frontend needs `NEXT_PUBLIC_API_URL` pointed at the running backend; `NEXT_PUBLIC_WS_URL` is for real-time updates (the WebSocket server is not built yet). OpenTimestamps anchoring of CV entries is on by default against the public calendar pools; set `OTS_ESPLORA_URL` (e.g. `https://blockstream.info/api`) to also verify proofs against the live Bitcoin chain, and `OTS_UPGRADE_INTERVAL_SECONDS` to change the proof-upgrade worker cadence (default 6 hours). See [`docs/OTS_ANCHORING.md`](./docs/OTS_ANCHORING.md) for a plain-English + jargon breakdown of the whole anchor/verify pipeline.

See `backend/.env.example` and `frontend/.env.example` for the full annotated list. For Lightning testing, run LNbits against a Bitcoin Core regtest node — do not point dev environments at mainnet. Any hold invoice drawn to a given wallet belongs to that wallet's pool, so use the dedicated Ganji wallet/keys and never paste an admin key somewhere public.

---

## Database Migrations

```bash
migrate create -ext sql -dir backend/migrations -seq <description>
```

Never edit a migration already applied to a shared environment — write a new one.

---

## Status

🚧 Pre-launch. See [`docs/BACKEND_STATUS.md`](./docs/BACKEND_STATUS.md) for what's built and [`docs/CONTRIBUTING.md`](./docs/CONTRIBUTING.md) for workflow and team ownership. See [`docs/FRONTEND_REPORT.md`](./docs/FRONTEND_REPORT.md) for frontend implementation status.
