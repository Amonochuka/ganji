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

## Remaining Issues

### Minor / Hardening

**15. `/health` Ping ignores context timeout.**
In `health/handler.go`, a 3-second timeout context is created
(`context.WithTimeout(c.Request.Context(), 3*time.Second)`), but the handler
calls `dbConn.Ping()` (unbounded context) instead of `dbConn.PingContext(ctx)`.
If Postgres hangs or the connection pool is starved, the health check
blocks indefinitely instead of failing fast after 3 seconds.

**16. Notifications are freelancer-only, un-refunded to clients, and untracked.**
`DealNotifier` emails go only to the freelancer, and each send spawns an
un-bounded goroutine with no waitgroup/rate limit. Notably, on `DealRefunded`,
the email is dispatched to the freelancer; the client—whose money was
actually returned—receives no notification whatsoever.

**17. Dead code / stale stubs.**
`pkg/hash/preimage.go` and `pkg/sanitize` are empty (preimage generation is
inline in `deals/service.go`); `ErrPaymentNotPaid` (`deals/errors.go`) is
never used.

## Not Built / Next

### Backend

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

### Frontend (out of scope this session)

`frontend/` exists but the shared deal/public-link UI is not implemented.