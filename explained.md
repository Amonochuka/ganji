# Ganji — How It Works (Working Reference)

A living reference for the Ganji backend: what the product does, how the money
and deal flows work, where each concern lives, and what is still missing.
Read this before touching code — it is kept in sync with `API_REFERENCE.md`
(the authoritative endpoint contract) and the migrations.

---

## 1. What Ganji Is

Ganji ("money" in Sheng) is a **Bitcoin / Lightning escrow app for
freelancers**. A freelancer and a client transact over any channel (WhatsApp,
Telegram, X, email) — Ganji just provides the escrow, the payment link, and a
cryptographically anchored reputation layer (the **Live CV**).

The escrow is **network-as-escrow**: instead of holding sats in a hot wallet,
Ganji draws a Lightning **hold invoice** and the Lightning Network itself keeps
the funds until the client approves. Nothing is custodial.

```
freelancer creates deal   ──►  Ganji draws a hold invoice for amount_sats
                               (payment_hash = sha256(preimage Ganji holds))

  client opens share link ──►  sees title, amount, bolt11 invoice, status
  client pays invoice     ──►  sats HELD on the network (escrowed)

  freelancer submits work ──►  client views artifacts
  client approves         ──►  settle (reveal preimage) → funds to Ganji wallet
                               ──► pay freelancer's payee_invoice ──► released
  client disputes         ──►  deal freezes 'disputed', funds stay held
                               ──► arbiter: released (pay) or refunded
```

Every released deal stamps `verified_at` and its artifacts become hash-verified
lines on the freelancer's public **Live CV** (`GET /cv/:slug` + hash verification
endpoint — see §6 of API_REFERENCE).

---

## 2. Architecture

A classic layering: **Gin router → Handler → Service → Repository → Postgres**,
with the `Service` also talking to LNbits for anything network-related.

```
 Request → gin router → handler (HTTP concerns, auth context, status mapping)
                       → service   (business rules, ownership, state machine,
                                    LNbits money moves, share-link logic)
                       → repository (pure SQL: one struct per row)
                       → PostgreSQL

 service ──► lnbits.Client (hold create/check/settle/cancel, payout)
 webhook.HandlePayment ──► deals repo (lookup by checking_id, lock)
```

### Module layout

```
backend/
  cmd/api/            entry point, router wiring, background workers
  internal/
    auth/             JWT + refresh rotation + bcrypt (complete)
    config/           env/config loading (godotenv)
    cv/               Live CV — public profile, hash anchoring + verification,
                      trust-score derivation
    db/               pool + auto-migration on boot
    deals/            deal model, state machine, share link, escrow service,
                      artifacts (streamed upload/download), verifications,
                      expiry sweep
    health/           GET /health
    lnbits/           LNbits HTTP client (hold invoices, check/settle/cancel,
                      pay out, webhook-url attachment)
    middleware/       auth (done); cors via gin-contrib; ratelimit is a stub
    storage/          artifact blob backend (Storage iface + Local disk impl,
                      traversal-safe keys, S3-ready)
    webhook/          LNbits payment webhook + HMAC signature verification
    websocket/        STUB (real-time updates planned)
  migrations/         numbered SQL schema (golang-migrate, run at boot)
  pkg/                hash/ (sha256 anchor helper), sanitize/ — mostly empty
frontend/             Next.js app (out of scope here)
```

### Key files

| Concern | File |
|---|---|
| Server entry + graceful shutdown | `backend/cmd/api/main.go` |
| Route wiring + CORS + dependency construction | `backend/cmd/api/router.go` |
| Background workers (hold-expiry sweep) | `backend/cmd/api/workers.go` |
| Deal struct, statuses, valid transitions | `backend/internal/deals/types.go` |
| Deal business logic (escrow, ownership, share link) | `backend/internal/deals/service.go` |
| Artifact upload/download (streamed to storage) | `backend/internal/deals/service.go` + `artifact_handler.go` |
| Artifact blob backend (`Storage` iface + `Local`) | `backend/internal/storage/` |
| Deal SQL (repos, transactions) | `backend/internal/deals/repository.go` |
| Deal repository interface | `backend/internal/deals/interface_types.go` |
| Deal sentinel errors | `backend/internal/deals/errors.go` |
| Deal HTTP handlers + route registration | `backend/internal/deals/handler.go` |
| Hold-expiry sweep worker logic | `backend/internal/deals/sweep.go` |
| LNbits client | `backend/internal/lnbits/client.go` + `models.go` |
| Webhook endpoint + HMAC | `backend/internal/webhook/` |
| Deal service tests | `backend/internal/deals/service_test.go` |

Design constants to keep: constructor-injected dependencies
(`NewService(repo, lnbitsClient)`), narrow consumer interfaces (webhook uses
`DealReader`/`PaymentChecker`) so unit tests need no real DB or LNbits,
sentinel errors + `errors.Is` for HTTP mapping, and a single `DBTX` for
transactional operations.

---

## 3. The Deal Lifecycle (State Machine)

Defined in `backend/internal/deals/types.go` (`ValidTransitions`) and mirrored
by the DB `CHECK` constraint in
`backend/migrations/000003_create_deals_table.up.sql`.

```
                ┌──────────────────────────────┐
                │  awaiting_payment            │  ← client pays hold invoice
                └──┬───────────────────────────┘  (funds held on the network)
        paid (CLN) │            │ freelancer submits
                   ▼            ▼
              ┌────────┐  ┌──────────────────┐
              │ locked │  │  work_submitted  │
              └───┬────┘  └───┬─────────┬────┘
   freelancer     │           │ client  │ client disputes
   submits        ▼           │ checks  ▼
              ┌────────────────┐   approve │
              │ work_submitted │◄─────────┘
              └───┬────────┬───┘
          dispute │        │ approve (settle hold → released)
                  ▼        ▼
             ┌──────────┐┌──────────┐
             │ disputed ││ released │
             └────┬─────┘└──────────┘
       arbiter    │  (terminal)
       resolves   ▼
             ┌────────┐
             │refunded│  terminal
             └────────┘

  * reviewing is an optional formal phase between submitted and approve.
  * disputed = money frozen pending arbitration. DisputeDeal requires a
    written reason and cancels nothing — the hold stays on the network.
    ResolveDispute (operator only) moves it to released or refunded; a client
    who changes their mind can still approve from disputed.
  * released is reached by client approve or an operator release resolution.
  * refunded is reached by an operator refund resolution or by the
    hold-expiry sweep for deals that were never funded.
```

| From | Allowed To | Set by | Money move? |
|---|---|---|---|
| `awaiting_payment` | `locked`, `work_submitted`, `disputed`, `refunded` | backend / freelancer / client | locked = funds held |
| `locked` | `work_submitted`, `disputed` | — | — |
| `work_submitted` | `reviewing`, `released`, `disputed` | — | — |
| `reviewing` | `released`, `disputed` | — | — |
| `disputed` | `released`, `refunded` | client approve / arbiter | settle + payout, or cancel hold |
| `released` | *(terminal)* | client approve | settle + payout |
| `refunded` | *(terminal)* | arbiter / sweep | cancel hold |

Rules that keep the money honest:

- The freelancer's generic `PATCH /deals/:dealID/status` **cannot** set
  `locked`, `released`, `disputed`, or `refunded`. Those money states are set
  only by the backend: payment detection, approve, dispute, or arbitration.
- `released` is only reachable through a **successful network settle**
  (revealing the preimage proves the client really funded the hold).
- `refunded` is **never** reachable directly from a client-facing state. A
  dispute freezes the funds in `disputed` instead; the sats only return via
  arbitration, or via the hold-expiry sweep for deals that were never funded
  (expired/cancelled/unpaid holds). This closes the pay → take the work →
  cancel loop.

If you add a status constant, update the DB `CHECK` constraint too or Postgres
rejects the writes.

---

## 4. Money: Network-as-Escrow (Hold Invoices)

This is the current model. LNbits hold invoices make the Lightning Network the
escrow: an incoming payment sits **held** under `payment_hash` until someone
reveals the preimage (settle → to the freelancer) or tears it down (cancel →
back to the client).

### LNbits endpoints Ganji uses

| Action | Call | Auth |
|---|---|---|
| Create hold invoice (`out:false, amount, memo, payment_hash, webhook`) | `POST /api/v1/payments` | invoice key |
| Check status | `GET /api/v1/payments/{checking_id}` | invoice key |
| Settle (release escrow) — reveal stored preimage | `POST /api/v1/payments/settle` | **admin key** |
| Cancel (refund) — `{payment_hash}` | `POST /api/v1/payments/cancel` | **admin key** |
| Pay out — `out:true, bolt11` | `POST /api/v1/payments` | **admin key** |

`Deal` stores both sides of the secret: `preimage` (raw hex — needed later to
settle) and `preimage_hash = sha256(preimage)` (what LNbits locked the invoice
to). The preimage and the freelancer's `payee_invoice` are persisted but never
serialized into API responses (`omitempty` + the public view drops them).

### CreateDeal (`service.go`)

1. Validate + trim input; require a valid `client_email` and a `payee_invoice`
   (the freelancer's payout destination).
2. Generate a 32-byte random **preimage**; store `preimage` + `preimage_hash`.
3. Generate the shareable-link **`share_token`** (see §5).
4. `lnbits.CreateHoldInvoice` → hold invoice + `checking_id`; `memo = title`
   (hold expiry: `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS`, default 30 days).
5. Insert the deal (transaction, `status = awaiting_payment`).

No money moves on creation.

### Paid ⇒ locked detection — autodetect, backend chooses

Whether LNbits reports a *held-but-unsettled* invoice as `paid` depends on its
funding backend:

- **CLN-backed LNbits** reports `paid=true` while held → deal locks the moment
  the client pays (via webhook or poll).
- **LND-backed LNbits** keeps `paid=false` until settled → the deal stays
  `awaiting_payment`, and the **settle on approve** is what atomically proves
  the funds were held.

Hence the `awaiting_payment → work_submitted` edge (freelancer can submit
before confirmed payment without deadlocking), and all locking logic is gated
on LNbits actually reporting `paid`. No backend flags or hacks.

### Paths that move money

- **Webhook** (`POST /webhooks/lnbits`): LNbits pings when an invoice is paid.
  The handler verifies the HMAC signature (`LNbits-Signature`, ±5 min replay
  window, skipped if `LNBITS_WEBHOOK_SECRET` is empty), then the service
  **re-queries LNbits** (the webhook body is never trusted) and transitions
  `awaiting_payment → locked`. Idempotent for already-locked deals.
- **Poll** (`GET /deals/:dealID/payment`, freelancer): same reconciliation via
  the shared `refreshPaymentStatus`, so locking works even without webhooks.
- **Approve** (`POST /deals/:dealID/approve`, client-by-email):
  1. `SettleHold(preimage)` — reveals the preimage, funds land in Ganji's
     LNbits wallet;
  2. `PayInvoice(payee_invoice)` — forwards to the freelancer;
  3. only then `status = released` (+ `verified_at NOW()`).
  Idempotent: a settle refused because it's *already settled* is treated as
  success (verified via `CheckPayment`), so a retry after a crash between the
  legs just pays out without double-settling. Payout is **never** attempted
  unless LNbits confirms the escrow is settled.
- **Dispute** (`POST /deals/:dealID/dispute`, client-by-email): **moves no
  money.** The client must provide a written `reason` (trimmed, ≤ 2000 chars);
  the deal enters `disputed` with `dispute_reason` + `disputed_at` and the
  hold **stays held** on the network, frozen pending arbitration. Only an
  arbiter resolves it to `released` (settle + payout) or `refunded` (cancel
  hold). A client who changes their mind can still approve from `disputed`.
  This is the safeguard against pay → take the work → cancel: the funds can't
  be clawed back by the client alone.
- **Arbitration** (`GET /disputes`, `POST /disputes/:dealID/resolve`,
  operator-only via the `is_operator` claim):
  - `release` → same two network legs as approve (`settleAndPayEscrow`): settle
    the hold, pay the freelancer, `released`. Anchors the CV like an approve.
  - `refund` → `CancelHold(preimage_hash)`, sats return to the client,
    `refunded`. Idempotent: if the hold is already gone (retry, or already
    cancelled/expired on the network) LNbits refuses the cancel, and we
    confirm via `CheckPayment` that the funds are no longer committed
    (`UNPAID`/`EXPIRED`/`CANCELLED`) before recording the refund. A hold still
    committed that we cannot cancel stays `disputed` with an error.
  - The deal must actually be `disputed` (else `ErrInvalidTransition`), and
    `resolved_by` + `resolved_at` are written **only after** the network leg
    succeeds — the DB never claims a money move the network did not make.
- **Payee-invoice rotation** (`PATCH /deals/:dealID/payee-invoice`,
  freelancer): swaps the payout destination while the deal is open. Unsticks a
  release where the original invoice expired mid-deal. Frozen after
  `released`/`refunded`.
- **Hold-expiry sweep** (background, `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS`,
  default 6 h): reconciles stale open deals with LNbits. If LNbits reports
  `UNPAID`/`EXPIRED`/`CANCELLED` — the network already returned any funds —
  the deal becomes `refunded`. Holds still held/settled are left alone. This
  is the backstop for an expired-but-never-refunded hold sitting in
  `awaiting_payment` forever.

---

## 5. The Shareable Payment Link (share_token)

This is how the payment-confirmation loop closes for the client. The goal:
the freelancer sends a link, the client opens it **without logging in**, sees
the invoice (and message at-a-glance), pays in their wallet, and the page
reflects the money landing on the network.

### Design — why a separate token, not the deal UUID

`GET /public/deals/:shareToken` is the only unauthenticated deal endpoint.
The share token is a **deliberately separate secret from the deal's DB UUID**:

- **Revocable.** `POST /deals/:dealID/share-link` (freelancer) mints a fresh
  token; the old link stops resolving immediately. A UUID-based link can't be
  retired without hiding the whole deal.
- **No internal-ID leak.** The public response never contains the deal UUID,
  so a shared link can't be used to correlate/guess other rows.
- **Unguessable but not secret-critical.** 32 random bytes
  (`crypto/rand`) base64url-encoded (~256 bits). Anyone with the link can see
  the title/amount/invoice and pay — that is the point — but they cannot read
  any sensitive field.

### Implementation

- **Schema** (`000003_create_deals_table.up.sql`):
  `share_token TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex')`
  with `UNIQUE` (which also serves as the lookup index). The `NOT NULL DEFAULT`
  backfilled every existing deal when the column was folded into the table, so
  no backfill migration is needed. New tokens are generated in
  `deals.Service.CreateDeal` too (service-side generation, DB default as a
  safety net).
- **Model**: `Deal.ShareToken` (`json:"share_token"` — visible to authed
  users so the freelancer can build the link) and a separate `PublicDeal`
  view that carries only `title`, `amount_sats`, `source_platform`, `invoice`,
  `status`, `created_at`.
- **Lookup + refresh**: `Service.GetPublicDeal` resolves the token via
  `repo.GetDealByShareToken`, and before answering **proactively re-checks the
  hold with LNbits** (`refreshPaymentStatus`): if the client has already paid,
  the deal transitions to `locked` right there. The client opening the link has
  no account and could never call the freelancer-only poll endpoint, so this
  self-refresh is what makes the page reflect reality even if the webhook was
  delayed/lost/never configured. An LNbits error is swallowed — the visitor
  gets the last known status rather than a 500.
- **Rotation**: `Service.RotateShareLink` (freelancer owner only) generates a
  new token and persists it via `repo.UpdateShareToken`. Mirrors the payee-
  invoice rule: frozen after `released`/`refunded` (nothing left to share).
- **Route wiring**: `GET /public/deals/:shareToken` is mounted on the **root
  router** (unauthenticated), while `POST /deals/:dealID/share-link` lives in
  the protected `/deals` group.

### Endpoints

| Method | Path | Auth | Effect |
|---|---|---|---|
| `GET` | `/public/deals/:shareToken` | none | safe public deal view; refreshes hold status (locks if paid) |
| `POST` | `/deals/:dealID/share-link` | freelancer | rotate token → revoke old link, return new one |

### Threat model / trade-offs

- Link is bearer: whoever has it can see the deal and pay the invoice. That is
  the feature, and the token is unguessable. It is **not**
  sensitive-credential-grade — treat it like a "magic link".
- Payment-gating is unchanged: even with the link, a stranger can't approve or
  dispute (both are locked to `client_email` matching) and can't see the
  preimage, payee invoice, or the freelancer's payout details.
- After `released`/`refunded` the public page still renders the (terminal)
  status but the invoice is spent; rotation is disabled because there is
  nothing to re-share.

---

## 6. Identity & Authorization

- **Freelancer** = authenticated user. `deals.freelancer_id` points at `users`.
  Deal creation, artifact/verification upload, submit, payee-invoice and share-
  link rotation are all freelancer-owner-gated.
- **Client** = just an email address. `deals.client_email` (lowercased) is
  captured at deal creation; the client needs **no account** to receive the
  link and pay. When they're ready to review, they sign up with that same
  email; the JWT carries `email` (set by `middleware/auth.go`), and
  `strings.EqualFold(deal.ClientEmail, email)` authorizes approve/dispute and
  deal viewing.
- `GET /deals` and `GET /deals/:dealID` serve both roles (freelancer by id,
  client by email match via `ListForUser`).
- Money-state endpoints are contrasted: approve/dispute are **client**-scoped,
  submit/payee-invoice/share-link are **freelancer**-scoped, and the generic
  status PATCH is freelancer-only but blocked from money states.

---

## 7. Tests & Verification

Run everything from `backend/`:

```bash
go build ./...   # compiles
go vet ./...     # static checks
gofmt -l .       # should print nothing
go test ./...    # all packages
```

- `internal/deals/service_test.go` — service-level tests using an in-memory
  fake `DealRepository` plus `httptest` LNbits servers: create→hold-invoice,
  submit rules, approve settle+payout (+ idempotent already-settled, refusal
  unless settled), arbitration disputes (freeze funds, write reason + reject
  empty/overlong, no network move, non-client forbidden, invalid-transition
  refusals, approve-after-dispute releases), operator arbitration (release =
  settle+payout+anchor, refund = cancel hold, idempotent already-cancelled,
  refusal while the hold is still committed, only-disputed, unknown verdict),
  sweep, payee-invoice rotation, share-link (public view safe fields, lock-on-
  refresh, LNbits-down fallback, rotation owner/frozen). Transactions are
  simulated via a no-op `database/sql` driver so `BeginTx/Commit` flows without
  a DB.
- `internal/deals/arbitration_handler_test.go` — the operator-only routes
  behind the **real** auth + operator middleware: queue contents, release
  round-trip, 400 on unknown verdict, 403 for a non-operator, 401 without a
  token.
- `internal/auth/jwt_test.go` + `internal/middleware/auth_test.go` — the
  `is_operator` claim is issued and verified, `AuthRequired` surfaces it, and
  `OperatorRequired` allows operators / 403s everyone else.
- `internal/deals/artifact_upload_test.go` / `artifact_handler_test.go` +
  `internal/storage/local_test.go` — streaming upload/download, size-cap
  rejection leaves no blob, owner/party gating, multipart handler round-trips,
  traversal-safe storage keys, delete semantics.
- `internal/webhook/service_test.go` / `handler_test.go` — payment → locked,
  unpaid ignored, missing deal, bad signature, HMAC verification.
- `internal/cv/service_test.go` — profile read self-heals missing anchors and
  derives trust score, verify matches / detects tampered hashes, foreign-slug
  lookup is hidden (404), handler status codes. The deals tests also cover
  the approve→anchor hook (including anchoring failing without blocking the
  release).
- `internal/lnbits/client_test.go` — client request shapes and key selection.

---

## 8. Not Built / Next

Backend:

- **Live CV** (`internal/cv/`): public `GET /cv/:slug` and
  `GET /cv/:slug/verify/:entryID` are implemented. Approving a deal anchors
  its artifacts as SHA-256 entries (`verified_at` → release), and the CV
  self-heals any anchors that were missed on its next read. `trust_score`
  is now derived on CV read (`100 + 25·released`, capped at 1000).
- **Artifact storage** (`internal/storage/`): done. `Storage` is an interface
  (S3-ready) with a `Local` disk backend; `POST /deals/:dealID/artifacts`
  streams a multipart upload to disk (size-capped by `MAX_UPLOAD_BYTES`,
  oversized rejected with no blob left behind, DB-failure rollback deletes the
  blob) and `GET /deals/:dealID/artifacts/:artifactID/download` streams it
  back to the freelancer or client. Keys are `deals/<dealID>/<random-hex><sanitized-ext>`.
- **Arbitration: done.** Disputes freeze the funds with the client's written
  reason (`disputed`; §3/§4) and an operator resolves them via
  `POST /disputes/:dealID/resolve` (release = settle+payout, refund = cancel
  hold) with the queue at `GET /disputes`. Operators come from the
  `OPERATOR_EMAILS` env var, promoted at boot; the role rides in the access
  token as `is_operator` and the routes sit behind `OperatorRequired`.
- **Arbitration (next): operator workflow polish.** The endpoints exist but
  there is no request–response round between arbiter and parties (evidence,
  counter-claims), no notifications on dispute/resolution, and no audit
  history beyond the single `resolved_by`/`resolved_at`. Also consider an
  automated resolution for `awaiting_payment` disputes where nothing was ever
  funded (the sweep already covers stale open deals).
- **WebSocket** (`internal/websocket/`): stub — real-time deal updates planned.
- **Rate limiting** middleware: empty stub (public endpoints are unthrottled).
- **Hardening (future): `client_email` masking.** Already excluded from the
  public share-link view, but the authed deal payloads (`POST /deals`,
  `GET /deals`, `GET /deals/:id`) return the full email to both parties. If we
  want stricter contact privacy later: return a masked value
  (`c***@example.com`) outside approve/dispute contexts, or drop it from list
  views entirely.

Frontend (out of scope this session): `frontend/` exists but the shared deal/
public-link UI is not implemented.

---

## 9. Environment

See `backend/.env.example`-style vars in `API_REFERENCE.md` / `README.md`; the
critical ones:

- `LNBITS_ADMIN_KEY` — router **admin** key; required for settle/cancel/payout
  (money moves). The invoice key alone can only create the hold invoice.
- `LNBITS_WEBHOOK_SECRET` + `WEBHOOK_URL` — webhook HMAC verification and the
  public webhook URL.
- `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` (30 d) / `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS` (6 h).
- `FRONTEND_URL` — CORS origin.
- `STORAGE_PATH` — where artifact blobs live on disk (default `./uploads`).
- `MAX_UPLOAD_BYTES` — per-artifact size cap (default 10 MB).
- `OPERATOR_EMAILS` — comma-separated emails promoted to arbitration
  operators at boot (`is_operator` claim). Empty by default; no operator means
  disputes can be raised but not resolved.