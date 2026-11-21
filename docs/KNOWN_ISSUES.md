# Known Issues & Bugs

Documented bugs and doc-vs-code mismatches, roughly ordered by severity.
Fix these before relying on the docs' claims. (Originally audited 2026-09-26;
**superseded** by the current accepted-hold verification and linked-wallet
funding changes — several notes below are historical and reflect the older
`paid=true`/`awaiting_payment` assumptions.)

## Fixed Issues ✅

| # | Issue | Fixed In |
|---|---|---|
| 1 | Escrow preimage + payee invoice leak into authed API responses | 2026-09-19 (`DealView` redaction) |
| 2 | `CreateDeal` orphans hold invoice if DB insert fails | 2026-09-19 (orphan hold cleanup) |
| 3 | `cancelOrphanHold` passes cancelled context on client timeout | 2026-09-26 (commit ee15232) |
| 4 | Clients can dispute unpaid deals (`awaiting_payment → disputed`) | 2026-09-26 (commit 5b414bf) |
| 5 | No expiry handling for `disputed` deals | 2026-09-26 (commit 27f7a07) |
| 6 | Client 403 from listing/viewing artifacts | 2026-09-26 (commit c49f96f) |
| 7 | Sweep ignores `work_submitted` deals | 2026-09-26 (commit af393a6) |
| 8 | CV self-heal failure breaks public CV | 2026-09-21 (commit 9c47b75) |
| 9 | Artifacts can be uploaded to `refunded` deals | 2026-09-26 (commit f8767aa) |
| 10 | Uploads buffered, not streamed; size cap checked late | 2026-09-19 (streaming multipart) |
| 11 | CV anchor binds storage key, not file bytes | 2026-09-21 (commit 9c47b75) |
| 12 | `approve` racing `dispute` can drop dispute | 2026-09-26 (commit 7f8b7b3) |
| 13 | Webhook body read unbounded on public endpoint | 2026-09-19 (1 MiB cap) |
| 14 | `Local.Save` non-atomic | 2026-09-26 (temp file + rename + fsync) |
| 18 | OTS proof pipeline non-functional end-to-end | 2026-09-22 (fixed protocol) |
| 19 | `GET /me` missing — profile only at signup/login | 2026-10-20 (commit 6ca4dc1) |
| 20 | Refresh tokens issued in the same second were identical (`iat` has second granularity) → UNIQUE `token_hash` violation → 500 on concurrent login | 2026-10-20 (commit 57409bf, random `jti` per issuance) |
| 21 | Rate limiting middleware was an empty stub (public/auth/webhook endpoints unthrottled) | 2026-11-02 (in-memory per-IP limiter; auth 5/min, public 60/min, webhooks 100/min) |
| 22 | JWT secrets had no minimum length validation | 2026-11-02 (config.Load fails fast below 32 chars) |
| 23 | Refresh-token GC never scheduled | 2026-11-02 (hourly cleanup worker via auth.RunRefreshTokenCleanup) |
| 24 | `/health` ping ignored context timeout in `db.Open` retry | 2026-11-02 (PingContext with 3s timeout in pingWithRetry) |
| 25 | Background workers serialized: `runWorkers` called `RunHoldSweep` (blocking) before OTS upgrade and refresh-token cleanup, so those never started | 2026-11-04 (launched each worker in its own goroutine) |
| 26 | Refresh-token cleanup query referenced nonexistent `revoked` column → hourly cleanup failed | 2026-11-04 (query now filters only `expires_at < now()`) |
| 27 | `Repository.WithTx` dropped `preimageKey`, disabling AES-GCM preimage encryption on the create-deal path | 2026-11-04 (key propagated through `WithTx`) |
| 28 | Rate limiter could divide by zero when `window/rate` underflowed | 2026-11-04 (guard `perToken <= 0`) |
| 29 | Funding reported `invalid status transition: awaiting_payment → locked` when the webhook/status poll accepted the hold first | 2026-11-20 (commit `261a364`) |

## Remaining Issues

### Minor / Hardening

The following items in the original audit are fixed; they are retained here as
history rather than active blockers:

- `/health` and DB startup use context-bounded `PingContext` checks.
- Notifications are sent to both parties and bounded by a semaphore (capacity 10).
- Removed dead `pkg/hash`, `pkg/sanitize`, and `ErrPaymentNotPaid` code.
- Artifacts can be deleted with `DELETE /deals/:dealID/artifacts/:artifactID`;
  there are no user-facing deal or verification deletion endpoints.
- Preimages support optional AES-GCM encryption at rest. When
  `PREIMAGE_ENCRYPTION_KEY` is unset, development deployments retain plaintext.
- Slug generation checks the base slug then uses a random suffix (up to five
  attempts).

## Not Built / Next

### Backend

- **Arbitration (next): operator workflow polish.** The endpoints exist but
  there is no request–response round between arbiter and parties (evidence,
  counter-claims) and no audit history beyond the single
  `resolved_by`/`resolved_at`. Notifications are sent on dispute, release, and
  refund transitions.
- **WebSocket** (`internal/websocket/`): stub — real-time deal updates planned.
- **Deletion endpoints.** There is no user-facing way to delete a deal or a
  verification. Users can delete artifacts, which also removes the stored blob.
- **Structured logging.** Everything is `log.Printf`; no `slog` / leveled
  loggers / request correlation IDs.
- **Hardening (future): `client_email` masking.** Already excluded from the
  public share-link view, but the authed deal payloads (`POST /deals`,
  `GET /deals`, `GET /deals/:id`) return the full email to both parties. If we
  want stricter contact privacy later: return a masked value
  (`c***@example.com`) outside approve/dispute contexts, or drop it from list
  views entirely.

### Frontend

See [`FRONTEND_REPORT.md`](./FRONTEND_REPORT.md) for the full picture. The
public-link page funds the hold through `POST /public/deals/:shareToken/fund`
with a linked wallet and approves via `POST /public/deals/:shareToken/approve`;
the client needs no Ganji account. A `locked` deal is labelled "Funds Held"
(an LND-accepted hold, not a settled payment). Settings (`/settings`, on
`GET /auth/me`) and Live CV pages are built. Status icons are inline SVGs from
`components/ui/status-icon.tsx`, not an emoji map. Manual-pay funding is still
deferred: the current fund button requires an LNbits URL/admin key, while the
planned flow will show the `bolt11` and verify the same Es accepted hold after
the client pays from any Lightning wallet.
