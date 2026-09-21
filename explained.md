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
serialized into API responses: **every** authenticated deal endpoint serves the
redacted `DealView` (drops `preimage`, `payee_invoice`, and the payout-tracking
fields), and the public share link serves `PublicDeal`. The raw `*Deal` stays
internal to the service layer.

### CreateDeal (`service.go`)

1. Validate + trim input; require a valid `client_email` and a `payee_invoice`
   (the freelancer's payout destination).
2. Generate a 32-byte random **preimage**; store `preimage` + `preimage_hash`.
3. Generate the shareable-link **`share_token`** (see §5).
4. `lnbits.CreateHoldInvoice` → hold invoice + `checking_id`; `memo = title`
   (hold expiry: `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS`, default 30 days).
5. Insert the deal (transaction, `status = awaiting_payment`).

No money moves on creation.

### Orphan-hold protection (CreateDeal failure cleanup)

If `repo.CreateDeal` (or the transaction begin/commit) fails **after** the hold
invoice was created at LNbits, the invoice would otherwise remain on the network
with no deal row behind it — a client who pays it has their sats held for the
full 30-day expiry with no one able to settle or cancel (the preimage and
`preimage_hash` exist only in the uncommitted transaction).

To prevent this, `CreateDeal` now calls a best-effort `cancelOrphanHold(preimageHash)`
on **every failure path after the hold is drawn**:

| Failure point | What happens |
|---|---|
| `BeginTx` fails | Cancel hold → no orphan possible (no row could exist) |
| `repo.CreateDeal` fails (constraint, outage) | Cancel hold → prevents the exact orphan bug |
| `tx.Commit` fails | Cancel hold — **ambiguous**: the row may have persisted server-side. Erring toward cancellation is safer: a dead invoice on a live row is swept to `refunded` by the hold-expiry sweep; an orphaned hold strands a paying client for 30 days. |

`cancelOrphanHold` uses the LNbits **admin key** to call `POST /api/v1/payments/cancel`
with the `preimage_hash` (the `payment_hash` LNbits locked the invoice to). LNbits
tears down the HTLC, returning sats to the payer if they already paid, or making the
invoice unpayable if they haven't. A refused cancel (already settled/expired) is
logged, not returned — the caller's original error surfaces to the freelancer, who
can retry `CreateDeal` immediately with a fresh hold invoice.

The hold-expiry sweep (every 6 h) remains the ultimate backstop: it reconciles any
`UNPAID/EXPIRED/CANCELLED` holds to `refunded`.

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
- **Client-facing notifications.** Emails today go only to the freelancer
  (payment locked, disputed, released, refunded); the client — who is the one
  that paid and is waiting — hears nothing (e.g. no "work submitted — come
  review"). The notifier interface only has freelancer-scoped events.
- **Deletion endpoints.** There is no user-facing way to delete a deal, an
  artifact, or a verification — artifacts in particular accumulate forever.
- **Refresh-token GC.** `refresh_tokens` rows are never cleaned up after
  expiry, so the table grows without bound.
- **`GET /me`.** Your own profile is only returned at signup/login; there is no
  authenticated endpoint to fetch the current user.
- **Structured logging.** Everything is `log.Printf`; no `slog` / leveled
  loggers / request correlation IDs.
- **Hardening (future): `client_email` masking.** Already excluded from the
  public share-link view, but the authed deal payloads (`POST /deals`,
  `GET /deals`, `GET /deals/:id`) return the full email to both parties. If we
  want stricter contact privacy later: return a masked value
  (`c***@example.com`) outside approve/dispute contexts, or drop it from list
  views entirely.

Known bugs and doc-vs-code mismatches are tracked in **§10 Known Issues** below.

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

---

## 10. Known Issues & Bugs

Documented bugs and doc-vs-code mismatches, roughly ordered by severity. Fix
these before relying on the docs' claims. (Audited 2026-09-21.)

### Money / secrets

1. **Escrow preimage + payee invoice leak into authed API responses.**
   ~~`API_REFERENCE.md` §3 and §6 below claim the raw `preimage` (the settle
   secret) and the freelancer's `payee_invoice` are "intentionally omitted"
   from responses. They are not. The `Deal` struct tags them `omitempty`, but
   that only suppresses **empty** values — and the repository always loads both
   from the DB (see the `scanDeal` in `repository.go`). So every endpoint that
   returns the full `Deal` (`GET /deals`, `GET /deals/:id`, approve, submit,
   payee rotation, …) exposes the settle preimage and the payout invoice to
   **both parties, including the client** (authorized by `client_email`
   match). The public share link (`PublicDeal`) is genuinely safe; the authed
   views were not.~~ **Fixed 2026-09-19:** handlers now serialize the redacted
   `DealView` (`types.go`) instead of the raw `*Deal` — it drops `preimage`,
   `payee_invoice`, and the payout-tracking fields, and keeps everything the
   docs promise. All authed deal endpoints (`POST /deals`, `GET /deals`,
   `GET /deals/:id`, approve, submit, payee rotation, arbitration, reconcile)
   and the dispute queue are covered. Regression-guarded by
   `deal_view_test.go` (`TestDealResponsesRedactSecrets`,
   `TestListDealsResponsesRedactSecrets`). Note for future work: `*Deal`
   remains serializable via Go's reflection (e.g. if a new handler forgets the
   view), so keep routing leak-prone endpoints through `Deal.View()` /
   `dealsView()`. A lint rule (`mustusefuncs.dealview`) would enforce it.

2. **`CreateDeal` orphans the hold invoice if the DB insert fails.**
   ~~In `service.go`, `CreateDeal` draws the LNbits hold invoice **before** the
   row insert. If `repo.CreateDeal` fails (constraint, outage), the standing
   hold has no DB row behind it: nothing can settle or cancel it, and a client
   who pays it is stuck until the hold expires.~~ **Fixed 2026-09-19:**
   `service.go CreateDeal` now tears the hold back down on any failure after
   the invoice is drawn and before the row is durably committed — `BeginTx`
   error, `CreateDeal` error (constraint/outage), or `tx.Commit` error. A
   commit error is ambiguous (the row may have landed server-side either way),
   so the code errs toward cancellation: a dead invoice on a live row is swept
   to `refunded`, whereas an orphaned hold strands a paying client for the
   full expiry window. The cleanup goes through a small best-effort
   `cancelOrphanHold(preimageHash)` helper — a refused cancel is logged and the
   caller's original error is preserved (the hold-expiry sweep remains the
   backstop). Regression-tested by `TestCreateDealCancelsHoldOnInsertFailure`
   in `service_test.go` (hold created → insert fails → cancel called with the
   deal's `preimage_hash`, no row persisted).

3. **`cancelOrphanHold` passes a cancelled context on timeout or client disconnect.**
   In `deals/service.go CreateDeal`, if `BeginTx` or `repo.CreateDeal` fails
   because the client disconnected or the request context timed out
   (`ctx.Done()`), `cancelOrphanHold(ctx, deal.PreimageHash)` is called with that
   same cancelled context. `s.lnbits.CancelHold(ctx, ...)` immediately aborts
   with `context.Canceled`, meaning the orphaned hold is **never** cancelled on
   LNbits when the failure was caused by a client-side timeout or connection drop.
   It should tear down the hold using `context.WithoutCancel(ctx)` or
   `context.Background()` with a dedicated short timeout.

4. **Clients can dispute unpaid deals (`awaiting_payment -> disputed`).**
   In `deals/types.go`, `ValidTransitions[StatusAwaitingPayment]` includes
   `StatusDisputed`. A client who has not paid a single satoshi can call
   `POST /deals/:dealID/dispute` with a reason up to 2,000 characters and
   inject an unfunded deal into the operator's arbitration queue. If an operator
   resolves it with `release`, the backend attempts to settle an unpaid hold
   invoice on LNbits. `StatusDisputed` should only be legal from funded states
   (`locked`, `work_submitted`, `reviewing`).

5. **No expiry handling for `disputed` deals.**
   `ListOpenBefore` only selects `awaiting_payment` and `locked`. A hold that
   expires/cancels while the deal is `disputed` returns the sats to the client
   on the network, but the DB row stays `disputed` forever — the money truth
   and the DB diverge permanently. The sweep should also reconcile `disputed`
   rows whose hold the network reports as released (`UNPAID`/`EXPIRED`/
   `CANCELLED`) — this is the "automated resolution for never-funded
   disputes" noted in §8.

### Correctness / robustness

6. **Client is 403 Forbidden from listing or viewing deliverables (`ListArtifactsByDeal` and `GetArtifactByID`).**
   In `deals/artifact_handler.go` and `service.go`, `ListArtifactsByDeal` and
   `GetArtifactByID` only check `deal.FreelancerID != userID`. The client email
   (`c.GetString("email")`) is neither read nor verified. Consequently, a
   reviewing client who visits `GET /deals/:dealID/artifacts` receives a
   `403 Forbidden`. They cannot inspect deliverables or obtain the
   `artifactID`s needed to download them (even though `DownloadArtifact`
   correctly permits client access). Both `ListArtifactsByDeal` and
   `GetArtifactByID` must authorize by checking
   `deal.FreelancerID == userID || strings.EqualFold(deal.ClientEmail, email)`.

7. **Sweep ignores `work_submitted` deals when hold invoices expire.**
   `ListOpenBefore` in `repository.go` only queries `WHERE status IN ($1, $2)`
   (`awaiting_payment`, `locked`). If a freelancer submits work and the client
   becomes inactive or ghosts, when the hold invoice reaches its TTL and
   auto-expires on the Lightning Network, the deal is never swept to `refunded`.
   The deal sits in `work_submitted` indefinitely, permanently out of sync with
   the network.

8. **CV self-heal failure breaks the public CV (contradicts the docs).**
   `cv/service.go GetProfile` returns an error from `healAnchors`, so a
   transient DB problem makes `GET /cv/:slug` 500 — while the code comment and
   `API_REFERENCE.md` promise self-healing that "never blocks reading the CV."
   Trust-score refresh already degrades gracefully; `healAnchors` should too
   (log + serve the last-known state).

9. **Artifacts can be uploaded to `refunded` deals.**
   The upload gate in `service.go UploadArtifact` blocks
   `work_submitted`/`reviewing`/`released`/`disputed` but omits `refunded` — a
   terminal refunded deal should have a frozen deliverable set. The same block
   carries a stale `TEMP` comment about pre-escrow behavior.

10. **Uploads are buffered, not streamed; the size cap is checked late.**
    ~~gin's `FormFile` (`artifact_handler.go`) calls `ParseMultipartForm(32MB)`,
    buffering the whole file into memory (or a temp file) before
    `UploadArtifact` streams it to storage. The `MAX_UPLOAD_BYTES` cap is only
    enforced after that buffering.~~ **Fixed 2026-09-19:**
    `artifact_handler.go CreateArtifact` now streams the multipart body with
    `c.Request.MultipartReader()` — no whole-file buffering. The `kind` field is
    read with a 64-byte cap, the request body is bounded with
    `http.MaxBytesReader(maxUploadBytes + 1 MiB)`, the per-file cap is still
    enforced while streaming in `UploadArtifact`, and an oversized upload leaves
    no blob (regression-tested). Note: `kind` must now precede the `artifact`
    file part (the file is streamed the moment it is seen; there is no second
    pass). Oversized body is mapped to 413; oversized file to 400.

11. **CV anchor binds the storage key, not the file bytes.**
    `cv/service.go` anchors `sha256(storage_key)`. Replacing the stored blob
    under the same key passes `GET /cv/:slug/verify/:entryID`. The
    "cryptographically anchored reputation" is only as strong as key
    immutability; hashing the actual file bytes (size + content digest) at
    release would make verification meaningful.

12. **`approve` racing `dispute` during the release window can drop the
    dispute.**
    In `releaseEscrow`, phase 3 re-validates only `CanTransition(disputed,
    released)` (which returns true by design), so a dispute recorded while
    phase 2's network legs were in flight is silently overwritten by the
    release and the payout still happens. The `dispute_reason` is left behind
    on a `released` row.

### Minor / hardening

13. **Webhook body read is unbounded on a public endpoint.**
    ~~`webhook/handler.go` `io.ReadAll`s the request body before verifying the
    `LNbits-Signature` (which is also skipped entirely when
    `LNBITS_WEBHOOK_SECRET` is empty). No body cap → memory DoS vector and
    LNbits-retry spam.~~ **Fixed 2026-09-19:** the body is now read through
    `http.MaxBytesReader` (1 MiB cap — notifications are ~300 bytes); a body
    over the cap is rejected with 413 before any parsing or signature work.
    Regression-tested. The empty-secret skip remains a deployment concern:
    set `LNBITS_WEBHOOK_SECRET` (a public endpoint with no auth at all).

14. **`Local.Save` is non-atomic.** A crash mid-write leaves a partial blob
    that the download path will happily serve (and `Content-Length` comes from
    `os.Stat`, matching the partial file — so corrupt data is served with a
    valid-looking length). Write to a temp file + rename + fsync.

15. **`/health` Ping ignores context timeout.**
    In `health/handler.go`, a 3-second timeout context is created
    (`context.WithTimeout(c.Request.Context(), 3*time.Second)`), but the handler
    calls `dbConn.Ping()` (unbounded context) instead of `dbConn.PingContext(ctx)`.
    If Postgres hangs or the connection pool is starved, the health check
    blocks indefinitely instead of failing fast after 3 seconds.

16. **Notifications are freelancer-only, un-refunded to clients, and untracked.**
    `DealNotifier` emails go only to the freelancer, and each send spawns an
    un-bounded goroutine with no waitgroup/rate limit. Notably, on `DealRefunded`,
    the email is dispatched to the freelancer; the client—whose money was
    actually returned—receives no notification whatsoever.

17. **Dead code / stale stubs.** `pkg/hash/preimage.go` and `pkg/sanitize`
    are empty (preimage generation is inline in `deals/service.go`);
    `ErrPaymentNotPaid` (`deals/errors.go`) is never used.