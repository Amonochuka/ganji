# Ganji Backend — Status Report

**Date:** 2026-08-26 (updated 2026-09-17)
**Author:** opencode

---

## Updates since 2026-08-26

This report was written early in the build. Since then the escrow core shipped
and the snapshot below is only history — **read [`explained.md`](./explained.md)
and [`API_REFERENCE.md`](./API_REFERENCE.md) for the current state.**

- **Network-as-escrow (hold invoices) is live.** Deals create LNbits hold
  invoices; approve = settle + payout, dispute = cancel → refunded. See
  `internal/deals/service.go`, `internal/lnbits/client.go`.
- **Webhook HMAC verification, hold-expiry sweep, and payee-invoice rotation**
  shipped (robustness batch).
- **Shareable payment link shipped.** Each deal has a revocable `share_token`;
  `GET /public/deals/:shareToken` shows a safe public view and refreshes the
  hold status (locks when paid) so the client sees the payment land without an
  account. `POST /deals/:dealID/share-link` rotates the token.
- **Live CV shipped.** Releasing a deal anchors its artifacts as SHA-256
  entries; the public `GET /cv/:slug` profile and `GET /cv/:slug/verify/:entryID`
  hash check are live, and `trust_score` is derived from released deals
  (`100 + 25·released`, capped at 1000).
- **Tests now exist.** `internal/deals/service_test.go`,
  `internal/cv/service_test.go`,
  `internal/webhook/service_test.go`+`handler_test.go`,
  `internal/lnbits/client_test.go`; `go test ./...` passes.
- Still stubbed (unchanged): `internal/websocket`,
  `internal/middleware/ratelimit.go`, `pkg/sanitize`.
  Frontend still does not compile.

---

## Summary

The Go backend (Gin + PostgreSQL) has a solid foundation with auth, deal CRUD, artifact/verification management, and LNbits payment integration all working. The webhook handler was added today. Frontend is blocked on missing component modules.

**Overall completion: ~45%**

---

## What's Built

### Phase 1 — Foundation (Week 1–2) ✅

| Component | Status | Files |
|---|---|---|
| DB schema (7 migrations) | ✅ Done | `backend/migrations/000001–000007` |
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
| Artifact CRUD (source_code, source_file) | ✅ Done | `internal/deals/artifact_handler.go` |
| Verification CRUD (sandbox, preview) | ✅ Done | `internal/deals/verification_handler.go` |
| Transactional deal creation | ✅ Done | `internal/deals/service.go` |

### Phase 3 — Lightning Integration (Week 6–7) ✅ Backend done

| Component | Status | Files |
|---|---|---|
| LNbits invoice creation | ✅ Done | `internal/lnbits/client.go` |
| LNbits payment status polling | ✅ Done | `internal/lnbits/client.go` |
| Payment check endpoint | ✅ Done | `internal/deals/handler.go` |
| **Webhook handler (NEW)** | ✅ Done | `internal/webhook/` |
| HMAC signature verification | ✅ Done | `internal/webhook/service.go` |
| Auto-transition on payment | ✅ Done | `internal/webhook/service.go` |

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
| Rate limiting middleware | ❌ Empty stub | `internal/middleware/ratelimit.go` |
| CORS middleware | ❌ Empty stub | `internal/middleware/cors.go` |
| Input sanitization hardening | ❌ Not done | — |
| ~~Tests~~ *(tests now exist — see deals/webhook/lnbits `_test.go`)* | ✅ Done | `backend/internal/*/*_test.go` |

---

## What's NOT Built (Backend)

### Critical (blocks Phase 4+)
1. **WebSocket server** (`internal/websocket/`) — empty stubs. Needed for real-time deal state updates to the frontend.
2. **File upload handling** — artifacts store a `storage_key` but there's no actual upload endpoint or storage backend (S3, local disk, etc.).

### Important (Phase 5)
4. **Sanitize package** — needed for output encoding, XSS prevention.
5. **Hash/preimage package** — preimage logic is currently inline in `deals/service.go`, should be extracted.
6. **Rate limiting middleware** — currently empty stub.
7. **CORS middleware** — currently inline in `router.go` via `gin-contrib/cors`.

### Nice to Have
8. ~~Tests~~ — *(outdated: now covered by service/handler/client `_test.go` files across deals, webhook, and lnbits — see the "Updates since 2026-08-26" section).*
9. **Webhook retry logic** — the current handler processes once; LNbits retries automatically but we could add idempotency tracking.
10. **Structured logging** — currently uses `log.Printf`. Should move to `slog` or `zerolog` for production.

---

## Webhook Handler — What Was Built Today

**New package:** `internal/webhook/`

**Endpoint:** `POST /webhooks/lnbits` (public — no JWT auth)

**Flow:**
1. LNbits POSTs a JSON payload when an invoice is paid
2. Handler reads the raw body and the `LNbits-Signature` header
3. Service verifies HMAC-SHA256 signature using `LNBITS_WEBHOOK_SECRET`
4. Looks up the deal by `checking_id`
5. If deal is `awaiting_payment` and payment status is `success`, transitions to `locked`
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
| `LNBITS_WEBHOOK_SECRET` | No* | HMAC secret for webhook verification (*recommended) |
| `WEBHOOK_URL` | No** | Public URL for webhook endpoint (**required for webhooks to work) |
| `LNBITS_ADMIN_KEY` | Yes | Router admin key — required for settle/cancel/payout money moves |
| `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` | No | Hold invoice lifetime (default: 30 days) |
| `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS` | No | Hold-expiry sweep cadence (default: 21600s = 6h) |
| `FRONTEND_URL` | No | CORS origin (default: `http://localhost:3000`) |
| `PORT` | No | Server port (default: `8080`) |

---

## Recommendations

1. **Create the missing frontend modules** — the frontend cannot compile without `Button`, `Input`, `FormBanner`, `AuthShell`, `AuthProvider`, and `ApiClient`. This is the single biggest blocker.
2. **Build the WebSocket server** — needed before any real-time deal UI works.
3. ~~**Build the Live CV package**~~ — *(done: released deals anchor hashed CV entries; public profile + verify endpoints are live).*
4. **Add tests** — start with auth and deal state machine. The repository interfaces make mocking straightforward.
5. **Set up a CI pipeline** — `go vet`, `go build`, `go test` on every push.
