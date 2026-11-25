# Architecture

A classic layering: **Gin router → Handler → Service → Repository → Postgres**,
with the `Service` talking to both LNbits (money moves) and an LND accepted-hold
checker on the escrow node. It is not LNbits-only.

```
 Request → gin router → handler (HTTP concerns, auth context, status mapping)
                       → service   (business rules, ownership, state machine,
                                    LNbits money moves, accepted-hold checks,
                                    share-link logic)
                       → repository (pure SQL: one struct per row)
                       → PostgreSQL

 service ──► lnbits.Client (hold create/check/settle/cancel, payout)
         ──► lnd.Client (accepted-hold verification on Es via LND REST)
 webhook.HandlePayment ──► deals repo (lookup by checking_id, lock)
                        ──► lnd.Client (accepted hold before locking)
```

Escrow node: `polar-n3-Es` (Alice1/Es). Client: Bob/`polar-n3-Cl`.
Freelancer: `polar-n3-Fre`. `polar-n3-Pay`, if used, is route-only Es → Fre, not
a second Ganji wallet; the Alice2/relay workflow is reverted and not current.

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
                      accepted-hold gate, public fund / wallet invoice,
                      milestones (sequential per-milestone holds, canonical
                      ReleaseMilestone, exit, operator resolve),
                      artifacts (streamed upload/download), verifications,
                      expiry sweep
    health/           GET /health
    lnbits/           LNbits HTTP client (hold invoices, check/settle/cancel,
                      pay out, webhook-url attachment)
    lnd/              LND REST client (TLS cert + macaroon) implementing
                      IsAcceptedHold for the accepted-hold checker
    middleware/       auth (done); cors via gin-contrib; ratelimit implemented (in-memory)
    storage/          artifact blob backend (Storage iface + Local disk impl,
                      traversal-safe keys, S3-ready)
    webhook/          LNbits payment webhook + HMAC signature verification
    websocket/        STUB (real-time updates planned)
  migrations/         numbered SQL schema (golang-migrate, run at boot; 000001–000008 incl. `000008` milestones)
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
| Artifact upload/download (streamed to storage) | `backend/internal/deals/service.go` + `handler_artifacts.go` |
| Artifact blob backend (`Storage` iface + `Local`) | `backend/internal/storage/` |
| Deal SQL (repos, transactions) | `backend/internal/deals/repository.go` |
| Deal repository interface | `backend/internal/deals/types_interfaces.go` |
| Deal sentinel errors | `backend/internal/deals/errors.go` |
| Deal HTTP handlers + route registration | `backend/internal/deals/handler.go` |
| Hold-expiry sweep worker logic | `backend/internal/deals/worker_sweep.go` |
| LNbits client | `backend/internal/lnbits/client.go` + `models.go` |
| LND accepted-hold checker | `backend/internal/lnd/client.go` (`IsAcceptedHold`) |
| Accepted-hold checker wiring | `backend/internal/deals/service.go` (`AcceptedHoldChecker`, `WithAcceptedHoldChecker`), `backend/internal/webhook/service.go`, `backend/cmd/api/router.go` (`LND_REST_*` from `.env.example`) |
| Webhook endpoint + HMAC | `backend/internal/webhook/` |
| Deal service tests | `backend/internal/deals/service_test.go` |
| Milestone types/transitions | `backend/internal/deals/types.go` (`Milestone`, `ValidMilestoneTransitions`) |
| Milestones migration (000008) | `backend/migrations/000008_create_milestones_table.up.sql` |

Milestone mode is a second, sequential escrow inside the same `deals` module:
`Service.CreateDealWithMilestones` → `FundMilestone` (M1) → `ReleaseMilestone` (canonical, idempotent) → `FundMilestone` (next). The webhook reconciles deal-level holds only; milestone holds are refreshed by public views and freelancer poll (`refreshMilestonePayment`) using the accepted-hold checker.

## Design Constants

- Constructor-injected dependencies with options (`NewService(repo, lnbitsClient, opts...)`, `WithAcceptedHoldChecker`)
- Narrow consumer interfaces (webhook uses `DealReader`/`PaymentChecker`) so unit tests need no real DB or LNbits
- Sentinel errors + `errors.Is` for HTTP mapping; `ErrVerificationUnavailable` maps to `503`
- Single `DBTX` for transactional operations
- LNbits `paid=true` is not proof of held funds: `locked`/`funded` require the LND accepted-hold checker on `polar-n3-Es`
- Linked-wallet URLs are `http`/`https` host only (no userinfo/query/fragment); private/loopback hosts are blocked except for exact dev-only origins in `DEV_ALLOWED_WALLET_URLS`, which production refuses to load; wallet keys are transient and never stored or logged
