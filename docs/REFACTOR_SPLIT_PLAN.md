# Refactor Split Plan (behavior-preserving)

Status: **planning only**. No production-logic changes in this document; implementation starts only after explicit approval, step by step. Track progress in [Progress tracker](#progress-tracker).

## Progress tracker

Update this table after each approved refactor step (do not start steps without approval).

| Step | Scope | Status | Commit(s) | Verification | Deviations / notes |
|---|---|---|---|---|---|
| 0 | Baseline checks + split plan documented | done | `27543eb`, `3f51f74` | 2026-11-20: `go vet ./...` clean, `go test ./...` pass, `npx tsc --noEmit` clean | Baseline recorded; progress tracker added. No production code moved. |
| 1 | Split `backend/internal/deals/service.go` (same package, mechanical) | done | `aac1c94` (1a), `46fed96` (1b), `9f81c2e` (1c), `49042ad` (1d), `386bfab` (1e), `33b1572` (1f), `e3ba5d5` (1g), `e38c0d0` (1h), `b5d3a82` (1i), `7220690` (1j), `6c15b29` (1k), `ca64756` (1l), `2543b2a` (1m), `03972eb` (1n), `7a1ff7c` (1o), `2aa4efe` (1p), `b67046b` (1q), `32f3568` (1r), `1e81ab5` (1s), `c11b6fa` (1t), `85a3b9b` (1u), `8da51e4` (1v), `70e3b21` (1w/1x), `407a718` (1y), `63b253a` (1z), `ee42665` (1aa), `854f3a4` (1ab), `0ebf16e` (1ac), `5d96bf9` (1ad), `c69d7e6` (1ae), this commit (1af) | 2026-11-20: 1a-1af committed. 1af moved `releaseMilestoneViaCanonicalPath`/`ReleaseMilestone`/`PublicAcceptNextMilestone`/`ResolveMilestone` → `service_milestone_release.go`, leaving `service.go` as service/core definitions+options+`NewService` (~95 LOC). Verified after each step `cd backend && gofmt -w <changed> && go test ./internal/deals && go vet ./... && go test ./...` ok. | 1a artifacts/verif/storage (`canReset*` now in `service_payout_reset.go`). 1b notify, 1c share-token/email, 1d view helpers, 1e `RotateShareLink`, 1f stale comment cleanup, 1g milestone CRUD/read/status-guard, 1h `GetPublicDeal`, 1i read helpers, 1j deal creation/orphan-hold cleanup, 1k freelancer status/submit workflow, 1l payment/status/wallet-endpoint helpers, 1m dispute queue read, 1n payee-invoice update, 1o linked-wallet invoice generation, 1p dispute-raise and per-deal fund lock helper, 1q payout-reset predicates, 1r payout-tracking clear/CV-anchor helpers, 1s payment-check/refresh-status methods, 1t outgoing-payout status check helper, 1u hold settle/cancel guard primitives, 1v deal-hold cancel helper, 1w sweep, 1x exit flows, 1y operator dispute resolution/reconcile, 1z funding hold flows, 1aa public payout-attempt reset, 1ab public milestone reject, 1ac milestone hold prepare/fund/refresh, 1ad public approve, 1ae deal release escrow flow, 1af milestone release/accept/resolve. Fixed a mangled comment artifact in `service.go` (no logic change). Earlier bad cuts reverted then completed. Actual file boundaries differ from initial proposed grouping but remain mechanical same-package splits. |
| 2 | Split `backend/internal/deals/handler.go` (same package, mechanical) | done | `12a3c97` (2a), `acfad3a` (2b), `bff48a8` (2c), `8f8c7ef` (2d), this commit (2e) | 2026-11-20: 2a extracted public handlers/types → `handler_public.go`. 2b extracted arbitration/operator handlers, route registration, request types → `handler_arbitration.go`. 2c extracted authed milestone handlers/request/response/view helper types → `handler_milestones.go`. 2d extracted route registration (`RegisterRoutes`, `RegisterPublicRoutes`) → `handler_routes.go`. 2e extracted remaining authed deal handlers/request types and left `handler.go` as core `Handler` type/constructor only (~11 LOC). Verified each sub-step with `gofmt`, `go test ./internal/deals`, `go vet ./...`, `go test ./...` ok. | `handler_artifacts.go` (renamed from `artifact_handler.go`) remained separate. Actual boundaries: `handler_deals.go` (authed deal lifecycle/workflow/share/payee/invoice/exit), `handler_milestones.go`, `handler_public.go`, `handler_arbitration.go`, `handler_routes.go`, `handler.go` (core), `types_milestone_view.go` (renamed from `milestone_view.go`; Step 2 prep). Same package/exported API preserved. |
| 3 | Split `backend/internal/deals/repository.go` (same package, mechanical) | done | `b73d1c8` (3a), `e4d4f22` (3b), this commit (3c) | 2026-11-20: 3a extracted artifact/verification repository methods → `repository_artifacts.go`. 3b extracted milestone columns/scan/CRUD/payout methods → `repository_milestones.go`. 3c extracted deal columns/scan/create/query/mutation methods and `nullString` helper → `repository_deals.go`, leaving `repository.go` as core `DBTX`/`Repository`/constructor/tx helpers (~45 LOC). Verified each sub-step with `cd backend && gofmt -w <changed> && go test ./internal/deals && go vet ./... && go test ./...` ok. | Actual boundaries: `repository.go` (core+tx), `repository_deals.go`, `repository_milestones.go`, `repository_artifacts.go`. Same package/exported API preserved. |
| 4 | Split `frontend/src/lib/api/api-client.ts` (compatibility barrel) | done | this commit | 2026-11-20: split into `types.ts`, `session.ts` (ApiError/session refresh/fetch helpers/events), `client.ts` (api methods), kept `api-client.ts` as compatibility barrel re-exporting previous public surface; no consumer import changes. Verified `cd frontend && npx tsc --noEmit` clean. `npm run build` failed in this environment fetching Google Fonts (network), unrelated to this split. | `fetchWithSession`/`fetchJson` remain module-internal to session/client (not previously exported). |
| 5 | Extract frontend deal/public-deal page sections | done | `3b48dad` (5a), this commit (5b) | 2026-11-20: 5a extracted public deal milestone row/status helpers into colocated `frontend/src/app/public/deals/[shareToken]/deal-ui.tsx`. 5b extracted authed deal header/status/milestone helpers and timeline into colocated `frontend/src/app/deals/[dealId]/deal-ui.tsx`; page state/effects/actions/UI text unchanged. Verified each step with `cd frontend && npx tsc --noEmit` and `npm run build`. | Mechanical presentational/helper extraction only; large page bodies remain but duplicated/local UI helpers moved out. |
| 6 | Optional: split large test files | not started | — | — | — |
| F1 | Feature (separate): freelancer-configurable milestone build/funding days and per-milestone expiry/deadlines | planned | — | — | **Not a mechanical split** — involves schema/API/behavior changes; implement in dedicated step(s), not mixed into split commits. See [Planned feature: milestone funding/build timing](#planned-feature-milestone-fundingbuild-timing-f1). |

**How to use:**
- **Status:** `not started` → `in progress` → `done` (or `blocked`/`skipped`/`planned` with reason).
- **Commit(s):** record the commit hash(es) for that step.
- **Verification:** record commands run and outcome (e.g. `go test ./...`, `go vet ./...`, `npx tsc --noEmit`, `npm run build`, smoke checks) with date.
- **Deviations / notes:** record any file-boundary differences from the proposed grouping, unexpected issues, or decisions made during the step. For feature steps, note design decisions and migration/API changes.

## Baseline (verified 2026-11-20)
- `go vet ./...` clean
- `go test ./...` pass (`internal/deals` cached/passing)
- `npx tsc --noEmit` clean
- Known working tree noise before refactor: `M docs/DEAL_LIFECYCLE.md`, `M docs/KNOWN_ISSUES.md`, untracked `backend/uploads/` (runtime, do not commit)
- Untested wiring packages today: `cmd/api`, `internal/config`, `internal/db`, `internal/health`, `internal/websocket` (`[no test files]`)

## Goals
- Improve maintainability/readability of oversized files by splitting code by responsibility.
- Keep runtime behavior, API contracts, routes, DB behavior, escrow/payout semantics, and error mapping identical.
- Make each step independently verifiable and easy to review/revert.

## Non-goals
- No feature work, bug fixes, dependency changes, schema changes, or behavior changes mixed into refactor commits.
- No renaming of exported symbols, JSON fields, route paths, status strings, or package names unless a later approved step explicitly requires it.
- No “cleanup” of logic that looks odd without proof it is dead/incorrect and covered by tests.

## Invariants / safety rails
- **Escrow-critical paths must not change semantics:** accepted-hold verification, funding idempotency/race handling, settle/release/cancel flows, payout attempt tracking, dispute resolution, milestone sequential funding/release/exit rules.
- Prefer **same-package file splits** first (Go `package deals`) to avoid import/API churn.
- Move code, do not rewrite it. If a move requires minor adaptation (visibility/import), keep it mechanical and obvious.
- One logical step per commit/checkpoint; run verification after each step before proceeding.
- If a test/build fails, stop and fix the refactor regression before continuing (do not “fix forward” with behavior changes).
- Avoid touching untested wiring packages except when necessary for compilation; if touched, smoke-check manually.

## Verification checklist (run after every step)
Backend step:
1. `cd backend && gofmt -w <changed files>` (or `gofmt -w .` for touched package)
2. `cd backend && go test ./internal/deals ./internal/...` as relevant, then `go test ./...`
3. `cd backend && go vet ./...`

Frontend step:
1. `cd frontend && npx tsc --noEmit`
2. `cd frontend && npm run build` (when route/component structure changes)

Manual smoke (after risky/backend-funding or page-structure milestones): create/load deal, public fund, work submit, approve/release, dispute path as applicable; confirm no duplicate payout/funding and status transitions unchanged.

## Proposed split targets

### 1) `backend/internal/deals/service.go` (2844 LOC, 86 funcs) — highest value, highest risk
Split into multiple files in **same package** `deals`. Suggested grouping (adjust only if needed to keep helpers near callers; keep behavior identical):

| Proposed file | Responsibility | Indicative functions/areas |
|---|---|---|
| `service_core.go` (or keep `service.go`) | Service struct, interfaces used by service, options, constructor, shared locks, notify helpers, generic helpers | `With*`, `NewService`, `lockFund`, `validateWalletEndpoint`, `generateShareToken`, `isValidEmail`, `ActiveMilestone`, `dealViewWithMilestones`, `notify*`, small pure helpers |
| `service_deal_lifecycle.go` | Deal creation/read/list/basic transitions not tied to money settlement internals | `CreateDeal`, `CreateDealWithMilestones`, `cancelOrphanHold`, `GetDealByID`, `ListByUser`, `UpdateStatus`, `SubmitWork`, `UpdatePayeeInvoice`, `SweepExpiredHolds` |
| `service_funding.go` | Wallet/payment verification and funding entrypoints (**critical**) | `GenerateInvoice`, `FundDeal`, `fundDealHold`, `fundMilestoneHold`, `CheckPayment`, `refreshPaymentStatus`, `refreshMilestonePayment` |
| `service_release_payout.go` | Approve/release/settle/cancel/reconcile/payout tracking (**critical**) | `ApproveDeal`, `PublicApproveDeal`, `releaseEscrow`, `ReleaseMilestone`, `releaseMilestoneViaCanonicalPath`, `settleHoldGuarded`, `settleMilestoneHoldGuarded`, `cancelEscrowHold`, `cancelMilestoneHold`, `checkOutgoingPayout`, `clearPayoutAttempt`, payout/hold status helpers, `ReconcileMilestonePayout`, `PublicResetPayout`, reset-attempt guards, `anchorReleasedDeal` |
| `service_dispute_exit.go` | Disputes, arbitration outcomes, exits | `DisputeDeal`, `ListDisputes`, `ResolveDispute`, `refundDispute`, `ResolveMilestone`, `ExitDeal`, `PublicExitDeal`, `exitDeal`, `PublicRejectMilestone` |
| `service_milestone.go` | Milestone CRUD/state helpers and progression plumbing not covered above | `CreateMilestone`, `GetMilestonesByDeal`, `GetMilestoneByID`, `UpdateMilestoneStatus`, `prepareMilestoneHold`, `FundMilestone`, `PublicAcceptNextMilestone` (final placement decided to avoid cycles/duplication) |
| `service_artifact.go` | Artifacts, storage helpers, verifications | `MaxUploadBytes`, `UploadArtifact`, `DeleteArtifact`, `DownloadArtifact`, `openStoredArtifact`, `ListPublicArtifacts`, `DownloadPublicArtifact`, artifact key/sanitize/counting reader, `GetArtifactByID`, `ListArtifactsByDeal`, `CreateVerification`, `GetVerificationByID`, `ListVerificationsByArtifact` |
| `service_public.go` | Public/share views and token management (if not folded above) | `GetPublicDeal`, `RotateShareLink` |

Notes:
- Exact file boundaries may be tuned during implementation to keep related private helpers with their callers; do not split a function across concerns.
- `service_test.go` and `service_milestone_test.go` (renamed from `milestone_service_test.go`) stay as-is initially (same package tests should continue to compile). Splitting tests is optional later.

### 2) `backend/internal/deals/handler.go` (1059 LOC)
Same-package split by transport concern, preserving route registration and handler methods:
- `handler.go`: `Handler` struct, constructor, shared request/response helpers if extracted mechanically.
- `handler_deals.go`: deal CRUD/status/payment/work/approve/dispute/payee/invoice.
- `handler_public.go`: public deal/share/fund/approve/reset/accept/reject/exit/public milestones.
- `handler_milestones.go` and/or `handler_admin.go`: milestone CRUD, disputes list/resolve, reconcile, arbitration/milestone resolve.

Risk: low if methods/signatures unchanged and route registration still calls same methods.

### 3) `backend/internal/deals/repository.go` (863 LOC)
Same-package split:
- `repository.go`: struct, constructor, tx helpers, scan helpers.
- `repository_deals.go`: deal queries/mutations.
- `repository_milestones.go`: milestone queries/mutations.
- `repository_artifacts.go`: artifact/verification queries/mutations.

Risk: low-medium (SQL strings moved, must remain byte-for-byte in effect).

### 4) `frontend/src/lib/api/api-client.ts` (551 LOC)
Goal: modularize without breaking imports.
- Introduce domain modules (e.g. `lib/api/deals.ts`, `artifacts.ts`, `public.ts`, `auth-session.ts` or similar) and keep `api-client.ts` as a compatibility barrel re-exporting the existing API surface.
- Update imports only if necessary; prefer zero consumer changes in first pass.
- Verify types and runtime fetch behavior unchanged.

### 5) `frontend/src/app/deals/[dealId]/page.tsx` (697) and `frontend/src/app/public/deals/[shareToken]/page.tsx` (511)
- Extract presentational/section components and data-mapping/hooks into colocated files (same route folder or `components/deals/...`) while keeping page entrypoints and `"use client"` boundaries correct.
- Preserve state ownership, effects, API calls, error handling, and UI text/conditions.
- Do after API-client split or independently once verified.

## Execution order (recommended)
1. **Step 0 (done):** baseline checks + this plan.
2. **Step 1:** split `deals/service.go` into same-package files (mechanical move only), starting with lowest-risk clusters (artifacts/verifications or helpers), then funding/release in isolated commits with tests after each.
3. **Step 2:** split `deals/handler.go` (mechanical).
4. **Step 3:** split `deals/repository.go` (mechanical).
5. **Step 4:** split frontend `api-client.ts` via compatibility barrel.
6. **Step 5:** extract frontend deal page sections/components.
7. **Step 6 (optional, separate decision):** split large test files for readability only.

Stop after any step if verification fails or scope drifts.

## Out of scope for this refactor
- Adding tests for `cmd/api`, `config`, `db`, `health`, `websocket` (valuable, but separate task).
- Removing websocket stubs or changing documented milestone TODOs.
- Committing `backend/uploads/`.

## Milestone delivery timing (F1)

**Context/problem:** escrow hold invoices are currently created with a fixed `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` (default 30 days) from hold creation. For M1 this starts at deal/milestone creation, so different milestones cannot have different build/funding windows and the timer is not aligned to “when payable/funded” in the way desired. Goal: freelancer can set days/time per milestone (how long it will take to build / be fundable), and funded escrows must not be auto-cancelled by the app.

**Planned behavior (to refine during implementation):**
- Freelancer sets **days to build / funding window** per milestone at creation (e.g. `build_days` or equivalent, integer > 0). Different milestones may have different values.
- Hold invoice expiry/funding deadline for each milestone is based on that milestone’s configured days (instead of a single global 30-day-from-creation value for M1 semantics).
  - M1: hold created/fundable at deal creation with expiry derived from M1’s configured days.
  - M2+: hold created when milestone becomes payable/fundable (after previous released/accepted) with expiry derived from that milestone’s configured days.
- After a hold is funded/accepted (`HOLD/ACCEPTED/SETTLED`), Ganji must not auto-cancel/refund it via sweep; sweep continues to reconcile only unfunded/expired/cancelled holds as today.
- UI/API must expose and accept the per-milestone timing fields (create deal/milestone payloads, deal/milestone views).

**Implementation impact (separate from mechanical split):**
- DB migration(s): add timing fields to `milestones` (and legacy deal handling if needed).
- Types/models/repo/service/handler: plumb new fields and use them in `CreateHoldInvoice`/hold preparation and validation.
- Frontend: deal creation and milestone display/edit flows to set/show days/dates.
- Tests: update/add coverage for per-milestone expiry/funding windows and no auto-cancel after funded.
- Docs: update lifecycle/env/API docs to reflect per-milestone timing.

**Status (2026-11-22): implemented (per-milestone hold expiry).** `delivery_days` is collected per milestone (1–3650 days, default 7), stored in DB, and visible to both participants. Milestone hold invoices are created with `expiry = delivery_days * 86400` from that hold's creation time (M1 at deal creation, M2+ when fundable/funded). `due_at` is derived from `funded_at + delivery_days` after funding. `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` is fallback/default for holds without per-milestone expiry. Sweep reconciles open deals by LNbits status (`UNPAID/EXPIRED/CANCELLED`) without a global age cutoff, because hold expiries now vary. See API reference and migration `000008`.


## UI ideas / UX improvements (tracked)

Document UI changes/ideas here as they are implemented or planned, especially when they affect deal status visibility and escrow/payment workflows.

| Item | Scope | Status | Notes |
|---|---|---|---|
| UI-1 | Authed deal page (`frontend/src/app/deals/[dealId]/page.tsx`) status refresh controls | implemented | **Check Payment** (freelancer, `awaiting_payment`) calls `GET /deals/:dealID/payment` and verifies the accepted hold; **Check Escrow Status** refreshes deal/milestones/artifacts for the freelancer while `locked` and for the client while `awaiting_payment`. Both roles can exit; only the client gets approve/reject/clear-failed-payout after submission, and **Raise Dispute** / **Check Freelancer Status** were removed from client UI. Public deal page polls/fetches public status; no raw hold settle/cancel controls added. |
| BF-1 | Release hardening for settled hold + payout not confirmed | implemented | `releaseEscrow`/milestone release keep payout attempt markers when payout is refused/failed after settled hold (prevents stranded-escrow unsafe clear/retry). Crash-window/failed recorded payout retries only when hold is confirmed SETTLED; ambiguous/unsettled hold states return `ErrPayoutInFlight`/no resend. Added regression tests for refused/canceled payee, ambiguous/not-initiated payout, crash window, settled-hold retry (`f388b17`). |
| UI-2 | Operator/dashboard deal triage | implemented | `GET /deals` returns all deals for operators (`isOperator` claim) via `ListAllDeals`; non-operators keep freelancer/client scoping. Dashboard adds stuck badge (`payout_reset_available`), status filter (all/stuck/active/done/specific), and client-side pagination. Public share-link "Check Freelancer Status" now surfaces refresh errors directly. |
| UI-3 | Freelancer payee invoice recovery | implemented | Freelancer deal page offers **Update Payee Invoice** with an empty textarea, **Retry Current Invoice** (loads the stored invoice from the freelancer-scoped `GET /deals/:id` response), optional LNbits **Generate Invoice** dialog for the open milestone amount, and manual paste. Choosing Generate clears the textarea and successful generation replaces it with the new invoice. Updating the invoice rewrites both `deals.payee_invoice` and all non-terminal `milestones.payee_invoice`, so stale expired invoices are not retried after escrow is settled. Client is not charged again; approval/reconcile pays from held escrow. |
| UI-4 | Escrow/status copy and transient action errors | implemented | Work-submitted/reviewing deal pages no longer say "work can proceed"; they show waiting-for-approval/review copy and keep the held-escrow message accurate. Client approval copy states that approval releases already-held escrow and does not charge again. Public page background artifact polling no longer surfaces stale action errors, and successful reset/approve actions clear prior error messages. |
| UI-5 | Public payout error reporting | implemented | Public approve and accept-next use one error mapper: payout refusal returns `502` with the real reason, in-flight/ambiguous payout returns `409`, validation/not-found/forbidden keep their specific codes, and unexpected errors are logged server-side while the client still gets a redacted `500`. The public page surfaces the returned message instead of a generic console-only failure. |
| UI-6 | Client proof for public money actions | implemented | Public share links remain sufficient for viewing/funding/downloading artifacts. Approve/accept/reject/reset/exit require a logged-in account with the deal client's email; the public page shows a sign-in link that returns to the same deal. |
| UI-7 | Role-aware authed UI | implemented | Dashboard and deal detail use `?role=client|freelancer` hints plus deal relationships to select a role. Client view hides **Create Deal**, **My CV**, freelancer submit/invoice/share-link controls, **Raise Dispute**, and **Check Freelancer Status**; it offers **Approve & Release**, **Reject Submitted Work**, **Clear Failed Payout**, **Exit Deal**, and status. Freelancer view keeps funding/submission/invoice controls but no client-only reject/approve. Sign-in/signup preserve `role` and `next`. |


## Security hardening pass (2026-11-24)

- `PATCH /deals/:dealID/milestones/:milestoneID` now checks `milestone.deal_id == dealID`; a valid milestone id from another deal returns `404`.
- `POST /public/deals/:shareToken/milestones/:milestoneID/accept-next` now requires the caller-supplied milestone to be the active (first non-terminal) milestone; out-of-order accept/release is rejected before any settle or payout call.
- Added migration `000009_add_milestone_status_exited` so the `exited` status used by the exit flow exists in clean and already-migrated databases. Down migration is intentionally a no-op because PostgreSQL cannot safely remove an enum value.
- Deal and milestone views now serialize `payout_reset_available` from the stored payout-attempt state; the dashboard stuck filter/badge receives the flag in authed deal responses as well as public ones.
- Payee invoice validation now rejects expired invoices when LNbits decode data includes `date`/`timestamp` plus `expiry`, and rejects unverifiable expiry metadata. Validation remains best-effort when decode does not expose expiry data.
- Caller-supplied wallet URLs now reject unspecified (`0.0.0.0/8`), loopback, private, link-local, shared-address, and non-routable IPv6 destinations before any wallet API call; CIDR constants fail loudly at startup, and tests cover IPv4-mapped loopback.
- `DEV_ALLOWED_WALLET_URLS` permits exact local wallet origins (`http://localhost:5001`, `http://localhost:5002`) through the SSRF guard in development only; production refuses to start if the setting is present.
- Public money actions (approve, accept-next, reject, reset payout, exit) now require an authenticated account whose email matches the deal client; public views, artifact access, and funding remain share-link based.

## QA hardening pass (2026-11-24)

- `fundLocks` was replaced with context-short-circuited `singleflight` so per-deal funding keys are released after each call; no unbounded map remains.
- Accepted-hold polling now checks context at the top of each loop and adds jitter to retries, avoiding synchronized thundering-herd checks.
- `SubmitWork` fails closed when milestone payment refresh, reloaded milestone state, or deal status persistence fails.
- Hold sweep no longer accepts ignored `time.Time`/`time.Duration` cutoff parameters; it checks LNbits status for all open deals.
- Deal creation and milestone creation now cap milestone count (25) and title/source-platform lengths.
- Refresh tokens now live in a `Secure`-when-HTTPS, `HttpOnly`, `SameSite=Strict` cookie; access tokens stay in memory and are no longer written to `localStorage`.
- Logout now deletes the refresh token and revokes the current access token by `jti` via migration `000010_create_revoked_access_tokens_table`; `AuthRequired` rejects revoked access tokens.
- Auth and deal handlers now return fixed safe error messages instead of wrapped provider/internal error text.
- Email validation is shared between auth and deals; slugs transliterate accents and fall back to a deterministic hash for non-ASCII names.
- HTTP server now has header/read/write/idle timeouts and a 1 MiB header cap; responses set `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, and an API-safe CSP.
- Gin request logging redacts share-token path segments and uses route-style paths.
- `LNBITS_WEBHOOK_SECRET` is now required unless `ALLOW_INSECURE_WEBHOOKS=true` is explicitly set for local development; webhook signature verification fails closed and no longer warns per request.
- `PREIMAGE_ENCRYPTION_KEY` is required (32+ chars) in production; local dev logs a one-time warning when preimages remain plaintext.
- Frontend session restore now fails closed when refresh fails or the backend is unreachable, clearing stale local state without an unhandled promise rejection.
- Rate limiting remains in-process: this assumes a single backend replica; a shared Redis/SQL store is required before horizontal scaling.
- WebSocket support is still stubbed and no WebSocket route is mounted, so there is no socket auth surface yet.
- Deal-level `preimage_hash` remains a nullable legacy column; milestone mode stores the canonical hash on each milestone, and a drop migration/deployment is still pending.

## Legacy single-hold removal / migration harmonization (2026-11-22)

- `6c760c4` `refactor: remove legacy single-hold create/funding paths` — removed legacy single-hold create/funding/check/public-approve/release branches; omitted milestones normalized to 1 milestone (7-day default). `FundDeal`/`CheckPayment`/`PublicApproveDeal`/`ApproveDeal`/`refreshMilestonePayment` are milestone-only; `refreshMilestonePayment` requires accepted-hold verification and returns `ErrVerificationUnavailable` when verifier/hash unavailable (no `payment.Paid` fallback). `SubmitWork` updates milestone status with error handling; milestone release anchors CV. Verification: `cd backend && gofmt -w <changed> && go vet ./... && go test ./...` PASS.
- `af2c333` `refactor: remove legacy deal-level escrow paths` — removed legacy `releaseEscrow`/deal hold guards/payout helpers and `service_release_test.go`; `ResolveDispute` delegates to first disputed/rejected milestone resolution; `ReconcileMilestonePayout` returns explicit unsupported error in milestone-only mode; public/deal payout reset paths are milestone-only; sweep is milestone-aware (`UpdateMilestoneStatusIfCurrent`/`UpdateDealStatusFromMilestones`); exit requires milestones and recomputes deal status. Verification: `cd backend && gofmt -w <changed> && go vet ./... && go test ./...` PASS.
- `70ccf4d` `chore: harmonize milestone delivery timing into 000008` — folded the milestone delivery-timing patch migration into `000008_create_milestones_table` (not in prod) and removed the patch migration files.

## Deals filename harmonization (2026-11-25)

- Renamed same-package files to layer prefixes without moving code:
  - `interface_types.go` → `types_interfaces.go`, `milestone_view.go` → `types_milestone_view.go`, `preimage_crypto.go` → `crypto_preimage.go`
  - `payee_invoice_amount.go` → `service_payee_invoice_amount.go`, `sweep.go` → `worker_sweep.go`
  - `verification_handler.go` → `handler_verification.go`, `artifact_handler.go` → `handler_artifacts.go`
  - Tests now mirror sources: `deal_view_test.go` → `service_views_test.go`, `milestone_service_test.go` → `service_milestone_test.go`, `arbitration_handler_test.go` → `handler_arbitration_test.go`, `reconcile_handler_test.go` → `handler_arbitration_reconcile_test.go`, `artifact_handler_test.go` → `handler_artifacts_test.go`, `artifact_upload_test.go` → `service_artifacts_upload_test.go`, `handler_create_test.go` → `handler_deals_create_test.go`
- Base `service.go`, `handler.go`, `repository.go`, `types.go`, `errors.go` remain canonical anchors.
- Documentation references updated; no Go code or behavior changed. Verification: `cd backend && go test ./... && go vet ./...` PASS.

## Review notes for approver
- Highest-risk steps are **1 (funding/release code movement)** and **5 (React page extraction/state boundaries)**.
- Safest steps are **2, 3, 4 (barrel)** when done mechanically.
- Request: approve step-by-step; after each approved step I will implement only that step, run checks, report results, then wait before continuing.
