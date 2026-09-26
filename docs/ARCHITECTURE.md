# Architecture

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

## Module Layout

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

## Key Files

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

## Design Constants

- Constructor-injected dependencies (`NewService(repo, lnbitsClient)`)
- Narrow consumer interfaces (webhook uses `DealReader`/`PaymentChecker`) so unit tests need no real DB or LNbits
- Sentinel errors + `errors.Is` for HTTP mapping
- Single `DBTX` for transactional operations