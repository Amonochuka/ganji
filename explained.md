# Ganji — What's Been Built and Where (Session Reference)

This document is a working reference for the Ganji backend: what the code does,
which files implement it, what was recently changed and why, and what comes next.
Read this first before touching code so you can find your way around.

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

## 5. Client Role & Submit / Approve / Dispute Flow

The `deals` table originally only recorded `freelancer_id`. This milestone adds
the **client role** so the escrow can be completed by the person who actually
owns the money side of the deal.

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