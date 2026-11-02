# Known Issues & Bugs

Documented bugs and doc-vs-code mismatches, roughly ordered by severity.
Fix these before relying on the docs' claims. (Audited 2026-09-26.)

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

## Remaining Issues

### Minor / Hardening

**15. `/health` Ping ignores context timeout.**
Fixed in `db.Open`'s `pingWithRetry` (now uses `PingContext` with a 3s timeout).
The health handler already used `PingContext`.

**16. Notifications.** Now sent to both freelancer and client via `notifyBoth`.
Goroutines bounded by a semaphore (capacity 10).

**17. Dead code / stale stubs.**
`pkg/hash` and `pkg/sanitize` removed; `ErrPaymentNotPaid` removed.

### Minor / Hardening (continued)

**18. Orphaned files in storage.**
Artifacts and verifications have no deletion endpoints; uploaded blobs accumulate
forever (disk exhaustion risk). No cleanup strategy exists.

**19. Preimage storage security.**
Fixed: optional AES-GCM encryption at rest via `PREIMAGE_ENCRYPTION_KEY`
(env). Plaintext retained when key unset (dev).

**20. Slug generation collision handling.**
Fixed: base slug checked first, then random 6-hex suffix (5 attempts) instead of
100 sequential retries.

**21. Unbounded notification goroutines.**
Fixed: bounded by semaphore (capacity 10).

## Not Built / Next

### Backend

- **Arbitration (next): operator workflow polish.** The endpoints exist but
  there is no request–response round between arbiter and parties (evidence,
  counter-claims), no notifications on dispute/resolution, and no audit
  history beyond the single `resolved_by`/`resolved_at`. Also consider an
  automated resolution for `awaiting_payment` disputes where nothing was ever
  funded (the sweep already covers stale open deals).
- **WebSocket** (`internal/websocket/`): stub — real-time deal updates planned.
- **Deletion endpoints.** There is no user-facing way to delete a deal, an
  artifact, or a verification — artifacts in particular accumulate forever.
- **Refresh-token GC.** Hourly cleanup worker now removes expired/revoked rows;
  table no longer grows unbounded.
- **Structured logging.** Everything is `log.Printf`; no `slog` / leveled
  loggers / request correlation IDs.
- **Hardening (future): `client_email` masking.** Already excluded from the
  public share-link view, but the authed deal payloads (`POST /deals`,
  `GET /deals`, `GET /deals/:id`) return the full email to both parties. If we
  want stricter contact privacy later: return a masked value
  (`c***@example.com`) outside approve/dispute contexts, or drop it from list
  views entirely.

### Frontend (out of scope this session)

See [`FRONTEND_REPORT.md`](./FRONTEND_REPORT.md) for the full picture. The
public-link page is wired to `POST /public/deals/:shareToken/approve` (client
can pay and approve with no account). Settings (`/settings`, on `GET /auth/me`)
and Live CV pages are not built.