# Ganji Backend — Status Report

**Date:** 2026-08-26 (updated 2026-11-18)
**Author:** opencode

---

## Updates since 2026-08-26

This report was written early in the build. Since then the escrow core shipped
and the snapshot below is only history — **read [`explained.md`](../backend/explained.md)
and [`API_REFERENCE.md`](./API_REFERENCE.md) for the current state.**

- **Accepted-hold escrow model (current).** The escrow node is `polar-n3-Es`
  (Alice1/Es), the client is Bob/`polar-n3-Cl`, and the freelancer is
  `polar-n3-Fre`; `polar-n3-Pay` is route-only (Es → Fre when needed), not a
  second Ganji wallet. The Alice2/relay workflow was reverted and is not
  current. A deal locks only after LND on Es verifies a matching hold
  (hash + amount, `State == ACCEPTED`, `settled == false`, `settle_date == 0`);
  LNbits `paid=true` is not proof of funds and no longer locks. The checker is
  configured via `LND_REST_URL` / `LND_REST_CERT_PATH` /
  `LND_REST_MACAROON_PATH`; without it verification is blocked and `FundDeal`
  returns `503`. `locked` means **accepted hold**, not settled. Work submission
  requires `locked` (`awaiting_payment → work_submitted` is invalid). Approve
  reveals R and pays Fre directly; reject after `work_submitted` reveals no R
  and does not settle/cancel/fail the HTLC; the arbiter reveals R to pay Fre or
  cancels/waits for expiry to refund Bob; refund does not use R.
- **Linked-wallet funding endpoints.** Public
  `POST /public/deals/:shareToken/fund` takes `{wallet_url, wallet_key, milestone_id?}`
  and protected `POST /deals/wallet/invoice` takes
  `{wallet_url, wallet_key, amount_sats, memo?}`. Wallet URLs are http/https
  host only (no userinfo/query/fragment, no allowlist); keys are transient and
  are never stored or logged. The frontend exposes `api.fundPublicDeal()` and
  `api.generateInvoice()`, fund buttons on the public deal and milestones,
  payout invoice generation on `/deals/new`, "Funds Held" status labels, and
  inline SVG status icons (`components/ui/status-icon.tsx`).

- **Network-as-escrow (hold invoices) is live.** Deals create LNbits hold
  invoices; approve = settle + payout, dispute = **freeze + arbitration** (no
  more instant cancel → refunded). See `internal/deals/service.go`,
  `internal/lnbits/client.go`.
- **Webhook HMAC verification, hold-expiry sweep, and payee-invoice rotation**
  shipped (robustness batch).
- **Shareable payment link shipped.** Each deal has a revocable `share_token`;
  `GET /public/deals/:shareToken` shows a safe public view and refreshes the
  hold status (locks when the accepted hold is verified) so the client sees the
  payment land without an account. `POST /public/deals/:shareToken/fund` pays
  the hold from the client's linked wallet. `POST /deals/:dealID/share-link`
  rotates the token.
- **Live CV shipped.** Releasing a deal anchors its artifacts as SHA-256
  entries; the public `GET /cv/:slug` profile and `GET /cv/:slug/verify/:entryID`
  hash check are live, and `trust_score` is derived from released deals
  (`100 + 25·released`, capped at 1000).
- **File upload / download shipped.** Artifacts are now real files, not just a
  `storage_key` string: `POST /deals/:dealID/artifacts` streams a multipart
  upload to a disk backend (`internal/storage/` — `Storage` interface, `Local`
  impl, traversal-safe keys, S3-ready), size-capped by `MAX_UPLOAD_BYTES` with
  oversized blobs rejected before commit, and
  `GET /deals/:dealID/artifacts/:artifactID/download` streams them back to the
  freelancer or client.
- **Arbitration shipped.** Disputes freeze the funds (no network move); an
  operator resolves them via `GET /disputes` (queue) and
  `POST /disputes/:dealID/resolve` (`release` = settle + payout, `refund` =
  cancel hold). Operators are promoted from `OPERATOR_EMAILS` at boot and the
  role rides in the access token as `is_operator`; the routes sit behind
  `middleware.OperatorRequired`. Resolutions record `resolved_by` /
  `resolved_at` only after the network leg succeeds.
- **Tests now exist.** `internal/deals/service_test.go`,
  `internal/cv/service_test.go`,
  `internal/webhook/service_test.go`+`handler_test.go`,
  `internal/lnbits/client_test.go`,
  `internal/storage/local_test.go`, and the artifact upload/download handler
  and service tests; `go test ./...` passes.
- **Share-link approval + current-user profile shipped (2026-10-20).**
  `POST /public/deals/:shareToken/approve` lets the client settle + release
  straight from the share link with no account (the token is the
  authorization; `disputed → released` stays authed/email-gated), and
  `GET /auth/me` returns the caller's profile. Refresh tokens now carry a
  random `jti` so two issuals in the same second can't collide on the UNIQUE
  `token_hash` constraint. The frontend public page is wired to the approve
  endpoint; `GET /auth/me` still awaits a Settings page — see
  [`FRONTEND_REPORT.md`](./FRONTEND_REPORT.md).
- **Frontend compiles and has 12 pages implemented** — see [`FRONTEND_REPORT.md`](./FRONTEND_REPORT.md).
- Still stubbed (unchanged): `internal/websocket`, `pkg/sanitize`.
  Rate limiting now implemented (in-memory) in `internal/middleware/ratelimit.go`.
- **Milestone redesign (2026-11-09, `main`)** — sequential per-milestone escrow shipped as WIP (see [`MILESTONE_REDESIGN_NOTES.md`](./MILESTONE_REDESIGN_NOTES.md) for canonical status). Consolidated migration `000008` (`milestones` + nullable deal hold); `CreateDealWithMilestones` funds M1; canonical `ReleaseMilestone` (idempotent two-phase payout); escrow-mandatory ("fund it first"); post-submission reject/exit → `disputed` (hold stays); operator per-milestone `ResolveMilestone`; symmetric exit; public milestone actions (`accept-next`/`reject`/`exit`). Idempotency/atomicity parity, per-milestone artifacts/evidence, deal-level current-milestone contract, and UX/API polish are done (with `service_milestone_test.go`); remaining: rework-after-reject policy, deal/milestone dispute coherence, and real-LNbits + accepted-hold E2E (tracked in the redesign notes TODOs).

---

## Summary

The Go backend (Gin + PostgreSQL) has a solid foundation with auth, deal CRUD, artifact/verification management, and LNbits payment integration all working. The webhook handler was added, and funding is verified against LND accepted holds on Es. Frontend now compiles with 12 pages.

**Overall completion: ~65%**

---

## What's Built

### Phase 1 — Foundation (Week 1–2) ✅

| Component | Status | Files |
|---|---|---|
| DB schema (8 migrations) | ✅ Done | `backend/migrations/000001–000008` |
| Auth (signup, login, logout, refresh) | ✅ Done | `internal/auth/` |
| JWT access + refresh tokens | ✅ Done | `internal/auth/token.go` |
| Password hashing (bcrypt) | ✅ Done | `internal/auth/service.go` |
| Slug generation from display name | ✅ Done | `internal/auth/service.go` |
| DB connection + auto-migration on boot | ✅ Done | `internal/db/` |
| Config loading from env | ✅ Done | `internal/config/config.go` |

### Phase 2 — Core Escrow Flow (Week 3–5) ✅ Backend done

| Component | Status | Files |
|---|---|---|
| Deal CRUD (create, get, list) | ✅ Done | `internal/deals/` |
| Deal status state machine | ✅ Done | `internal/deals/types.go` |
| Ownership enforcement | ✅ Done | `internal/deals/service.go` |
| Artifact CRUD (source_code, source_file) | ✅ Done | `internal/deals/handler_artifacts.go` |
| Verification CRUD (sandbox, preview) | ✅ Done | `internal/deals/handler_verification.go` |
| Transactional deal creation | ✅ Done | `internal/deals/service.go` |

### Phase 3 — Lightning Integration (Week 6–7) ✅ Backend done

| Component | Status | Files |
|---|---|---|
| LNbits invoice creation | ✅ Done | `internal/lnbits/client.go` |
| LNbits payment status polling | ✅ Done | `internal/lnbits/client.go` |
| Payment check endpoint | ✅ Done | `internal/deals/handler.go` |
| **Webhook handler** | ✅ Done | `internal/webhook/` |
| HMAC signature verification | ✅ Done | `internal/webhook/service.go` |
| Auto-transition on accepted hold | ✅ Done (requires LND checker) | `internal/webhook/service.go` |

### Phase 4 — Live CV & Verification (Week 8–9) ✅ Backend done

| Component | Status | Files |
|---|---|---|
| CV entries table (migration) | ✅ Done | `migrations/000005` (UNIQUE per artifact) |
| Release-time SHA-256 anchoring | ✅ Done | `internal/cv/service.go`, `internal/deals/service.go` |
| Public CV endpoint (`GET /cv/:slug`) | ✅ Done | `internal/cv/handler.go` |
| Hash verification (`/cv/:slug/verify/:entryID`) | ✅ Done | `internal/cv/service.go` |
| Trust score calculation | ✅ Done | `internal/cv/service.go` |

### Phase 5 — Polish & Deploy (Week 10–12) ❌ Not started

| Component | Status | Files |
|---|---|---|
| Sanitize package | ❌ Empty stub | `pkg/sanitize/` |
| Hash/preimage package | ❌ Empty stub | `pkg/hash/` |
| Rate limiting middleware | ✅ Done (in-memory) | `internal/middleware/ratelimit.go` |
| CORS middleware | ❌ Empty stub | `internal/middleware/cors.go` |
| Input sanitization hardening | ❌ Not done | — |
| ~~Tests~~ *(tests now exist — see deals/webhook/lnbits `_test.go`)* | ✅ Done | `backend/internal/*/*_test.go` |

---

## What's NOT Built (Backend)

### Critical (blocks Phase 4+)
1. **WebSocket server** (`internal/websocket/`) — empty stubs. Needed for real-time deal state updates to the frontend.
2. ~~**File upload handling**~~ — *(done: multipart upload + download streamed through `internal/storage/`; see "Updates since 2026-08-26".)*

### Important (Phase 5)
4. **Sanitize package** — needed for output encoding, XSS prevention.
5. **Hash/preimage package** — preimage logic is currently inline in `deals/service.go`, should be extracted.
6. **Rate limiting middleware** — implemented (in-memory, per-IP; auth 5/min, public 60/min, webhooks 100/min).
7. **CORS middleware** — currently inline in `router.go` via `gin-contrib/cors`.

### Nice to Have
8. ~~Tests~~ — *(outdated: now covered by service/handler/client `_test.go` files across deals, webhook, and lnbits — see the "Updates since 2026-08-26" section).*
9. **Webhook retry logic** — the current handler processes once; LNbits retries automatically but we could add idempotency tracking.
10. **Structured logging** — currently uses `log.Printf`. Should move to `slog` or `zerolog` for production.

---

## Webhook Handler — What Was Built

**New package:** `internal/webhook/`

**Endpoint:** `POST /webhooks/lnbits` (public — no JWT auth)

**Flow:**
1. LNbits POSTs a JSON payload for a payment event on the escrow wallet
2. Handler reads the raw body and the `LNbits-Signature` header
3. Service verifies HMAC-SHA256 signature using `LNBITS_WEBHOOK_SECRET`
4. Looks up the deal by `checking_id`
5. If the deal is `awaiting_payment`, the LND accepted-hold checker on Es
   confirms a matching hold (`ACCEPTED`, not settled) → transitions to `locked`;
   `paid=true` alone is not enough, and a missing checker blocks the transition
6. Returns appropriate HTTP status code based on the outcome

**Response codes:**
| Condition | Status | Body |
|---|---|---|
| Valid + processed | `200` | `{"status": "ok"}` |
| Invalid signature | `401` | `{"error": "invalid signature"}` |
| Malformed payload | `400` | `{"error": "malformed payload"}` |
| Payment not successful (valid webhook) | `200` | `{"status": "ignored", "reason": "payment not successful"}` |
| No deal for checking_id | `404` | `{"error": "no deal for checking_id"}` |
| Internal failure | `500` | `{"error": "internal failure"}` |

Payment-not-successful returns `200` because the webhook was received and understood — nothing for Ganji to do. Returning non-2xx would cause LNbits to retry unnecessarily.

**Signature format:** `t=<unix_timestamp>,v1=<hmac_sha256_hex>`
**Signed payload:** `"{timestamp}.{raw_body}"`
**Max age:** 5 minutes (rejects replayed webhooks)
**Future skew:** 5 minutes (rejects timestamps too far ahead)

**New config vars:**
- `WEBHOOK_URL` — public URL for this endpoint (e.g., `https://yourdomain.com/webhooks/lnbits`)
- `LNBITS_WEBHOOK_SECRET` — shared secret from LNbits wallet settings

**New repository method:** `GetDealByCheckingID` — looks up a deal by its LNbits checking ID.

---

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `JWT_SECRET` | Yes | Access token signing key |
| `JWT_REFRESH_SECRET` | Yes | Refresh token signing key |
| `LNBITS_URL` | Yes | LNbits instance base URL |
| `LNBITS_API_KEY` | Yes | LNbits invoice/admin key |
| `LNBITS_WEBHOOK_SECRET` | Yes* | HMAC secret for webhook verification (*local `ALLOW_INSECURE_WEBHOOKS=true` bypass only) |
| `WEBHOOK_URL` | No** | Public URL for webhook endpoint (**required for webhooks to work) |
| `LNBITS_ADMIN_KEY` | Yes | Router admin key — required for settle/cancel/payout money moves |
| `LND_REST_URL` | Yes for funding | LND REST endpoint on the escrow node Es; enables the accepted-hold checker (missing checker → `FundDeal` 503) |
| `LND_REST_CERT_PATH` | No | TLS cert path for the Es LND REST client |
| `LND_REST_MACAROON_PATH` | No | Macaroon path for the Es LND REST client |
| `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` | No | Hold invoice lifetime (default: 30 days) |
| `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS` | No | Hold-expiry sweep cadence (default: 21600s = 6h) |
| `FRONTEND_URLS` | No | CORS origins, comma-separated (default: `http://localhost:3000`) |
| `STORAGE_PATH` | No | Artifact blob directory (default: `./uploads`) |
| `MAX_UPLOAD_BYTES` | No | Per-artifact upload cap (default: 10 MB) |
| `OPERATOR_EMAILS` | No | Comma-separated emails promoted to arbitration operators at boot |
| `OTS_ESPLORA_URL` | No | Esplora API endpoint for live Bitcoin block-header verification of OTS proofs (empty = offline proof check only) |
| `OTS_ESPLORA_TIMEOUT_SECONDS` | No | Per-request esplora timeout (default: 10) |
| `OTS_UPGRADE_INTERVAL_SECONDS` | No | OTS proof-upgrade worker cadence (default: 21600s = 6h) |
| `PORT` | No | Server port (default: `8080`) |

---

## Recommendations

1. ~~**Add missing backend endpoints for frontend:** `GET /auth/me`, `POST /public/deals/:shareToken/approve`~~ — *(done 2026-10-20; frontend wiring still pending — see [`FRONTEND_REPORT.md`](./FRONTEND_REPORT.md)).*
2. **Build the WebSocket server** — needed before any real-time deal UI works.
3. ~~**Build the Live CV package**~~ — *(done: released deals anchor hashed CV entries; public profile + verify endpoints are live).*
4. **Add tests** — start with auth and deal state machine. The repository interfaces make mocking straightforward.
5. **Set up a CI pipeline** — `go vet`, `go build`, `go test` on every push.
