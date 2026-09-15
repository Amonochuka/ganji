# Ganji — What's Been Built and Where (Session Reference)

This document is a working reference for the Ganji backend: what the code does,
which files implement it, what was recently changed and why, and what comes next.
Read this first before touching code so you can find your way around.

> **Current milestone: migrating from a custodial LNbits escrow to a
> network-as-escrow (hold invoice) design.** Section 4 describes the old flow;
> Section 4b documents the new hold-invoice flow and how far the migration has
> progressed.

---

## 1. What Ganji Is

Ganji ("money" in Sheng) is a **Bitcoin / Lightning Network escrow platform for
freelancers** with a cryptographically-anchored reputation layer ("Live CV").

The flow that has been built so far (the escrow core):

```
CreateDeal()
      │  posts to POST /deals
      ▼
deals.Service.CreateDeal()
      │  generates a 32-byte preimage, hashes it (SHA-256)
      │  calls LNbits CreateInvoice()  -> creates a Lightning invoice
      ▼
LNbits returns invoice + checking_id
      │  deal is INSERTed transactionally, status = awaiting_payment
      ▼
User pays the invoice in their wallet
      │  (or explicitly polls GET /deals/:dealID/payment)
      ▼
LNbits POSTs a payment webhook  ->  POST /webhooks/lnbits
      │  webhook.Service.HandlePayment()
      │    → CheckPayment() with LNbits (authoritative confirmation)
      │    → GetDealByCheckingID()
      │    → transition awaiting_payment → locked
      ▼
Deal is LOCKED — escrow secured, freelancer can deliver work
```

Key point: **the webhook does not trust LNbits' webhook body.** It re-verifies
the payment with `CheckPayment()` before locking the deal. The webhook is just
a trigger; LNbits is the source of truth.

---

## 2. Where Each Piece Lives

### Modules in the repo

```
backend/
  cmd/api/                 entry point + router wiring
  internal/
    auth/                  JWT + bcrypt auth (complete)
    config/                env/config loading
    cv/                    Live CV — EMPTY STUB (next big feature)
    db/                    connection pool + auto-migrations
    deals/                 deal CRUD, state machine, artifacts, verifications
    health/                GET /health
    lnbits/                LNbits HTTP client (CreateInvoice, CheckPayment)
    middleware/            auth (done); cors/ratelimit (empty)
    webhook/               LNbits payment webhook (complete + tested)
    websocket/             EMPTY STUB
  migrations/              SQL schema (golang-migrate)
  pkg/
    hash/                  preimage hashing — EMPTY STUB
    sanitize/              input sanitizing — EMPTY STUB
frontend/                  Next.js app — currently does NOT compile
```

### Key files

| Concern | File |
|---|---|
| Server entry + graceful shutdown | `backend/cmd/api/main.go` |
| Route wiring, CORS | `backend/cmd/api/router.go` |
| Deal struct + status constants + valid transitions | `backend/internal/deals/types.go` |
| Deal business logic (preimage, LNbits invoice, ownership) | `backend/internal/deals/service.go` |
| Deal DB queries (repos, transactions) | `backend/internal/deals/repository.go` |
| Deal repository interface | `backend/internal/deals/interface_types.go` |
| Deal sentinel errors | `backend/internal/deals/errors.go` |
| Deal Gin handlers + routes | `backend/internal/deals/handler.go` |
| Deal service tests (submit/approve/dispute) | `backend/internal/deals/service_test.go` |
| Client role schema (client_email) | `backend/migrations/000007_add_client_email_to_deals.up.sql` |
| LNbits client (create invoice, check payment) | `backend/internal/lnbits/client.go` |
| LNbits request/response models | `backend/internal/lnbits/models.go` |
| Webhook handler (`POST /webhooks/lnbits`) | `backend/internal/webhook/handler.go` |
| Webhook service (verify + lock deal) | `backend/internal/webhook/service.go` |
| LNbits webhook payload model | `backend/internal/webhook/models.go` |
| Webhook service tests | `backend/internal/webhook/service_test.go` |
| Webhook handler tests | `backend/internal/webhook/handler_test.go` |

---

## 3. The Deal Lifecycle (State Machine)

Defined in `backend/internal/deals/types.go` (`ValidTransitions`) and mirrored
in the DB CHECK constraint (`backend/migrations/000002_create_deals_table.up.sql`).

```
awaiting_payment ──payment confirmed──► locked ──► work_submitted ──► reviewing
                                                                    │
                                          reviewing ──┬───────► released (terminal)
                                                      └───────► disputed ──► released
```

Note: `work_submitted` may also go **directly** to `released` or `disputed`
(the client can approve or dispute right after submission; `reviewing` is an
optional formal phase reached via `PATCH /deals/:dealID/status`).

| From | Allowed To |
|---|---|
| `awaiting_payment` | `locked` |
| `locked` | `work_submitted` |
| `work_submitted` | `reviewing`, `released`, `disputed` |
| `reviewing` | `released`, `disputed` |
| `disputed` | `released` |
| `released` | *(terminal — nothing)* |

If you add a status constant, you must update the DB CHECK constraint too, or
Postgres will reject inserts/updates.

---

## 4. Payment Flow — Implemented Details

### LNbits client (`backend/internal/lnbits/client.go`)

- `CreateInvoice(ctx, req)` — `POST {url}/api/v1/payments` with header
  `X-Api-Key`. If no per-request webhook URL is set, it attaches the configured
  `webhookURL`. Non-2xx responses are read and returned as error text.
- `CheckPayment(ctx, checkingID)` — `GET {url}/api/v1/payments/{checkingID}`,
  returns `CheckPaymentResponse{Paid, Details}`.

### Deal creation (`backend/internal/deals/service.go::CreateDeal`)

1. Trim + validate input (`ErrInvalidInput` on missing/bad fields).
2. Generate 32 random bytes as the **preimage**; store `SHA-256(preimage)` hex
   as `PreimageHash`.
3. Call LNbits to create the invoice (memo = deal title).
4. In a DB transaction, insert the deal with `status = awaiting_payment`.
   Rollback if anything fails.

### Payment polling (`deals/service.go::CheckPayment`)

`GET /deals/:dealID/payment` (authed, ownership-checked): asks LNbits if the
invoice is paid; if `Paid` and status is `awaiting_payment`, updates to `locked`.

### Webhook (`backend/internal/webhook/`)

- `Handler.HandleLNbitsWebhook` (`handler.go`) — public endpoint, no JWT.
  Reads raw body → unmarshals into `PaymentNotification` → calls service.
  Maps errors to status codes:
  | Error | HTTP |
  |---|---|
  | `ErrMalformedPayload` | 400 |
  | `ErrPaymentFailed` | 200 `{"status":"ignored","reason":"payment not successful"}` |
  | `ErrDealNotFound` | 404 |
  | anything else | 500 |
  | success | 200 `{"status":"ok"}` |

- `Service.HandlePayment` (`service.go`):
  1. Requires `checking_id`.
  2. `CheckPayment()` against LNbits — if it errors or `Paid == false`, stop.
  3. `GetDealByCheckingID()` — if not found, `ErrDealNotFound`.
  4. Only transitions a deal currently in `awaiting_payment` → `locked`.
     Already-locked deals are **idempotent** (returns nil, 200) so LNbits
     retries don't corrupt state.

- Narrow interfaces for easy testing: `DealReader` and `PaymentChecker`
  (`service.go`). This is why the tests need no real DB or LNbits.

---

## 4b. Network-As-Escrow Migration (Hold Invoices) — IN PROGRESS

**What we are replacing:** Section 4 above is the **custodial** design. Ganji
generates a preimage but never uses it (LNbits mints its own), the client pays a
regular invoice and the sats land **immediately in Ganji's LNbits wallet
balance**, and `ApproveDeal` only flips a DB column — there is no payout leg and
no way for the client to be refunded automatically. Ganji is the custodian of
every escrowed sat.

**Target:** use **hold invoices** so the Lightning Network itself is the escrow.

### How a hold invoice works (vetted against LNbits source, v1.5.6/v1.6.1/[dev])

Current LNbits endpoints (differ from old docs — the code is authoritative):

| Action | Endpoint | Auth |
|---|---|---|
| Create hold invoice | `POST /api/v1/payments` `{out:false, amount, memo, payment_hash, webhook}` | invoice/admin |
| Check status | `GET /api/v1/payments/{payment_hash}` | invoice/admin |
| **Settle** (release) | `POST /api/v1/payments/settle` `{preimage}` | **admin** |
| **Cancel** (refund) | `POST /api/v1/payments/cancel` `{payment_hash}` | **admin** |
| Pay out | `POST /api/v1/payments` `{out:true, bolt11}` | **admin** |

To create a hold invoice you pass `payment_hash = sha256(preimage)`. LNbits
stores the preimage internally and the incoming HTLC sits **held** — it cannot
complete until LNbits reveals the preimage (settle) or tears it down (cancel).

### Backend-dependent "held" detection — IMPORTANT

Whether LNbits reports a held-but-unsettled invoice as `paid` depends on the
funding source (`lnbits/wallets/*.py` `get_invoice_status`):

- **CLN / corelightning / clnrest**: maps invoice status `"paid"` → `paid=true`
  while held. So a webhook/poll can lock a deal the moment the client pays.
- **LND / lndrest / lndgrpc**: only `SETTLED` returns success; a held invoice
  stays `ACCEPTED` → LNbits reports `paid=false` until **settled**, and LND's
  `paid_invoices_stream` skips anything not `SETTLED` (so **no webhook fires
  while held**).

**Decision (with product):** autodetect — no backend config, no hacks.
Locking is always gated on `paid == true`. On CLN the deal locks immediately on
payment; on LND it stays `awaiting_payment` until the client approves, at which
point `SettleHold` atomically proves the funds were really held. To avoid a
deadlock on LND, `awaiting_payment → work_submitted` is an allowed transition
(submit before confirmed payment is safe: `released` is only reachable via a
successful settle, which cannot happen if the client never funded the hold).

### Progress so far

- [x] **LNbits hold-invoice client** (`internal/lnbits/client.go`,
  `models.go`, `client_test.go`):
  - `Client.adminKey` added; `Config{AdminKey}` wired from `LNBITS_ADMIN_KEY`
    (new env var, required for settle/cancel/send).
  - `CreateHoldInvoice` — POST `/api/v1/payments` with `payment_hash`,
    attaches the configured webhook, defaults `unit` to `sat`.
  - `SettleHold(preimage)` — POST `/api/v1/payments/settle`.
  - `CancelHold(payment_hash)` — POST `/api/v1/payments/cancel`.
  - `PayInvoice(bolt11)` — POST `/api/v1/payments` `out:true` (payout leg).
  - `postJSON` helper for the new POST methods.
  - Tests cover happy path, key selection (invoice vs admin), and error bodies.
- [x] **Deal model + migration** (`migrations/000008*`, `deals/types.go`,
  `deals/repository.go`):
  - `deals.preimage TEXT` — raw hex preimage Ganji generated, needed on LNbits
    to settle later. `deals.payee_invoice TEXT` — freelancer's Lightning
    destination for the payout leg.
  - Status enum + DB CHECK gain **`refunded`** (terminal): dispute cancels the
    hold on the network and refunds the client.
  - `ValidTransitions` updated for the LND case: `awaiting_payment →
    work_submitted` allowed (see the autodetect decision above); `refunded`
    reachable from `awaiting_payment`, `locked`, `work_submitted`, `reviewing`,
    `disputed`.
  - `Deal` gains `Preimage` and `PayeeInvoice` (JSON `omitempty` so the secret
    never leaks in API responses by default).
  - Repository inserts/scans all new columns via one shared `dealColumns` +
    `scanDeal` helper.
- [x] **`CreateDeal` → hold invoice** (`deals/service.go`, `handler.go`,
  `lnbits/client.go`, `config.go`):
  - `deals.CreateDeal` now requires `PayeeInvoice` (freelancer's payout
    destination) and no longer uses the custodial `CreateInvoice` path. It
    generates a fresh 32-byte **preimage** per deal, hashes it with sha256,
    and creates an LNBits hold invoice: `payment_hash = sha256(preimage)`,
    `out=false`, amount = `deal.amount_sats`, memo = title.
  - The raw preimage is stored (`deals.preimage`) because LNBits settles a
    hold with the preimage in the request body; only a money path (approve)
    or cancel (dispute) may later move the stuck funds.
  - Hold invoices are created with a **long expiry**
    (`LNBITS_HOLD_INVOICE_EXPIRY_SECONDS`, default 30 days) — LNBits' default
    1h invoice lifetime is far too short for escrow deals.
  - API: `POST /deals` now accepts `"payee_invoice"` (required).
  - New test `TestCreateDealCreatesHoldInvoice` verifies against an
    `httptest` LNBits server: `out=false`, correct amount/memo, a 64-hex
    `payment_hash`, the stored hex preimage matches, and the deal is saved as
    `awaiting_payment`. (Unit tests get a working transaction via a small
    no-op `database/sql` driver so `BeginTx/Commit` flow works without a DB.)
- [ ] Lock detection + transition changes.
- [ ] Approve = settle + payout; Dispute = cancel → refunded.
- [ ] More tests + docs.

## 5. Client Role & Submit / Approve / Dispute Flow

### Identity model (email link, no forced sign-up)

- Migration `000007` adds `client_email TEXT` to `deals`.
- At creation the freelancer names the client by **email** — the client does
  **not** need an account yet (product concept: no platform lock-in, deals
  happen over WhatsApp/Telegram/X/etc.).
- When the client is ready to review, they sign up with that same email. The
  JWT access token carries their email (`middleware/auth.go` sets
  `c.Set("email", claims.Email)`), which authorizes them for approve/dispute.
- Emails are stored lowercased (matching `auth` signup normalization) and
  compared with `strings.EqualFold`.

### New endpoints (all in `internal/deals/handler.go`)

| Method | Path | Who | Effect |
|---|---|---|---|
| `POST` | `/deals/:dealID/submit` | freelancer (owner) | `locked` → `work_submitted`; **requires ≥ 1 artifact** |
| `POST` | `/deals/:dealID/approve` | client (email match) | `work_submitted`/`reviewing` → `released` (stamps `verified_at`) |
| `POST` | `/deals/:dealID/dispute` | client (email match) | `work_submitted`/`reviewing` → `disputed` |

### Authorization rules (`internal/deals/service.go`)

- `SubmitWork(userID, dealID)` — `deal.FreelancerID != userID` → `ErrForbidden`.
  Returns `ErrInvalidInput` if no artifacts exist yet.
- `ApproveDeal(email, dealID)` / `DisputeDeal(email, dealID)` —
  `strings.EqualFold(deal.ClientEmail, email)` fails → `ErrForbidden`.
- The generic `PATCH /deals/:dealID/status` is **freelancer-only** and is
  blocked from reaching `released`/`disputed` (returning `ErrInvalidTransition`)
  so it can never be used to bypass the client. Those two states are reachable
  only through the client's approve/dispute endpoints.
- Releasing a deal via approve (`repository.go::UpdateStatus`) is what stamps
  `verified_at = NOW()`; dispute resolution later reuses the same single path.
- `GET /deals/:dealID` and `GET /deals` now also serve the **client**: a user
  who matches `client_email` can view the deal, and `ListForUser` returns deals
  where the user is freelancer **or** client.

Tests: `internal/deals/service_test.go` (fake in-memory `DealRepository`) covers
submit success/forbidden/no-artifact/bad-transition, approve as client (from
`reviewing` and from `work_submitted`), approve/forbidden/pre-payment, dispute
as client/forbidden, and the PATCH release/dispute bypass block.

---

## 5. Tests — What Passes and Why

All tests live in `backend/internal/webhook/`. Run with:

```bash
cd backend
go test ./internal/webhook
go test ./...     # whole module
```

- `service_test.go` — `Service.HandlePayment` unit tests with fake
  `DealReader`/`PaymentChecker` (payment confirmed → locked, unpaid → ignored,
  missing deal → error, LNbits failure → error, idempotency).
- `handler_test.go` — full HTTP tests through Gin:
  - paid payment → 200 `{"status":"ok"}` and deal updated
  - invalid JSON → 400
  - unpaid payment → 200 ignored
  - missing deal → 404
  - LNbits failure → 500

### JSON key-order gotcha (why the fix this session was needed)

`gin.H{...}` marshals map keys in **sorted order**, so
`{"reason": "payment not successful", "status": "ignored"}` is the actual wire
order even when the code writes `gin.H{"status": "ignored", "reason": ...}`.
The tests originally compared the response body as a raw string, so the
unpaid-payment test failed on key ordering even though the handler was correct.

Fix: compare responses as decoded JSON maps instead of strings. The pattern
used in `handler_test.go`:

```go
var expectedJSON map[string]string
var actualJSON map[string]string
json.Unmarshal([]byte(expectedBody), &expectedJSON)  // etc.
// compare len + each key/value
```

`TestHandleLNbitsWebhookReturnsNotFoundForMissingDeal` also initially had the
wrong expected body (`{"status":"ignored",...}`), which produced the misleading
`expected body X, got {"error":"no deal for checking_id"}` output. It now
asserts `{"error":"no deal for checking_id"}`.

---

## 6. Recent Commit Log (the worked milestones)

```
d63a907 fix webhook handler tests for JSON key order    <- most recent
938dbdc add webhook handler tests
dcc5b44 add webhook payment verification tests
9b38ebe verify LNbits payments before locking deals
1ccc45a wire and clean up LNbits webhook
c7d7365 fix: formatting
e495b44 webhook: return proper HTTP status codes and reject future timestamps
aeeca34 docs: add backend status report and API reference
49698d8 feat(webhook): add LNbits webhook handler
08551af feat(deals): add GET /deals/:dealID/payment endpoint
13ec9f6 feat: Add LNbits payment status client
c5567fe refactor: migrate to LNBits client and introduce transaction support for deals
```

---

## 7. Project State Overview

**Working:** auth (JWT + refresh rotation), deals CRUD + state machine, **client
role with submit/approve/dispute flow**, artifacts + verifications, LNbits
invoice creation, payment polling, webhook payment verification → lock, health,
migrations, graceful shutdown. `go build ./...`, `go vet ./...`, and the deals +
webhook tests all pass.

**Empty stubs (next features):** `internal/cv`, `internal/websocket`,
`internal/middleware/cors.go`, `internal/middleware/ratelimit.go`,
`pkg/hash`, `pkg/sanitize`.

**Not built / gaps:**

- Frontend (`frontend/`) does **not compile** — 22 TS errors from missing
  `@/components/ui/*`, `@/components/auth/auth-shell`,
  `@/lib/auth/auth-context`, `@/lib/api-client`.
- No dispute resolution endpoint (arbiter `disputed → released` is only
  reachable via the freelancer's `PATCH /status`, though the DB accepts it).
- `trust_score` is persisted but never calculated.
- No file upload backend (artifacts just record a `storage_key` string).
- No rate limiting, no structured logging (`log.Printf` everywhere).
- No CI, Dockerfile, Makefile, or `.env.example`.
- `backend/.env` (with real credentials) is **committed to git** — rotate and
  untrack it.
- The root `.gitignore` is **malformed** (contains the heredoc command text
  instead of rules).

### Architecture at a glance

```
Requests → Gin router → Handler → Service → Repository → PostgreSQL
                           │          │
                           │          └─→ lnbits.Client (X-Api-Key)
                           └─(webhook: narrow interfaces → easy tests)
```

Handler → service → repository layering, constructor-injected dependencies
(`NewService(repo, lnbitsClient)`), narrow consumer interfaces, sentinel errors
+ `errors.Is` for HTTP mapping, `DBTX` for transactional operations.

---

## 8. Next Logical Steps (in priority order)

1. **Deal client role + submit/approve/dispute flow** — the escrow product flow
   is documented (`submit`, `approve`, `dispute`) but not buildable yet because
   the model is freelancer-only. Add `client_id` to deals, then implement the
   transition endpoints. (Chosen as the next milestone.)
2. **Frontend build fix** — create the 6 missing `lib`/`components` modules so
   `next build` / `tsc` passes.
3. **`internal/cv` (Live CV)** — product differentiator; the `cv_entries`
   migration already exists. Needs public `GET /cv/:slug` + hash verification;
   extract the inline preimage logic into `pkg/hash`.
4. **WebSocket layer** — real-time deal updates over `/ws/deals/:dealID`
   (needs a websocket library — none is a dependency yet).
5. **Hygiene** — fix `.gitignore`, untrack+rotate `.env`, add CI
   (`go vet` + `go test` + `next build`), Makefile/docker-compose, `.env.example`.

---

## 9. Useful Commands

```bash
# Build / verify
cd backend
go build ./...
go vet ./...
gofmt -w internal/webhook/handler.go   # format a file
gofmt -l .                             # list unformatted files

# Tests
go test ./internal/webhook
go test ./...

# Migrations run automatically at boot (golang-migrate, file://migrations)
# Config required: DATABASE_URL, JWT_SECRET, JWT_REFRESH_SECRET
# Optional: PORT (8080), LNBITS_URL, LNBITS_API_KEY, WEBHOOK_URL, FRONTEND_URL

# Git (single main branch, conventional commits)
git status
git log --oneline -15
```