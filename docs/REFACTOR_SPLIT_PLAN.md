# Refactor Split Plan (behavior-preserving)

Status: **planning only**. No production-logic changes in this document; implementation starts only after explicit approval, step by step. Track progress in [Progress tracker](#progress-tracker).

## Progress tracker

Update this table after each approved refactor step (do not start steps without approval).

| Step | Scope | Status | Commit(s) | Verification | Deviations / notes |
|---|---|---|---|---|---|
| 0 | Baseline checks + split plan documented | done | `27543eb`, `3f51f74` | 2026-11-20: `go vet ./...` clean, `go test ./...` pass, `npx tsc --noEmit` clean | Baseline recorded; progress tracker added. No production code moved. |
| 1 | Split `backend/internal/deals/service.go` (same package, mechanical) | done | `aac1c94` (1a), `46fed96` (1b), `9f81c2e` (1c), `49042ad` (1d), `386bfab` (1e), `33b1572` (1f), `e3ba5d5` (1g), `e38c0d0` (1h), `b5d3a82` (1i), `7220690` (1j), `6c15b29` (1k), `ca64756` (1l), `2543b2a` (1m), `03972eb` (1n), `7a1ff7c` (1o), `2aa4efe` (1p), `b67046b` (1q), `32f3568` (1r), `1e81ab5` (1s), `c11b6fa` (1t), `85a3b9b` (1u), `8da51e4` (1v), `70e3b21` (1w/1x), `407a718` (1y), `63b253a` (1z), `ee42665` (1aa), `854f3a4` (1ab), `0ebf16e` (1ac), `5d96bf9` (1ad), `c69d7e6` (1ae), this commit (1af) | 2026-11-20: 1a-1af committed. 1af moved `releaseMilestoneViaCanonicalPath`/`ReleaseMilestone`/`PublicAcceptNextMilestone`/`ResolveMilestone` → `service_milestone_release.go`, leaving `service.go` as service/core definitions+options+`NewService` (~95 LOC). Verified after each step `cd backend && gofmt -w <changed> && go test ./internal/deals && go vet ./... && go test ./...` ok. | 1a artifacts/verif/storage (`canReset*` now in `service_payout_reset.go`). 1b notify, 1c share-token/email, 1d view helpers, 1e `RotateShareLink`, 1f stale comment cleanup, 1g milestone CRUD/read/status-guard, 1h `GetPublicDeal`, 1i read helpers, 1j deal creation/orphan-hold cleanup, 1k freelancer status/submit workflow, 1l payment/status/wallet-endpoint helpers, 1m dispute queue read, 1n payee-invoice update, 1o linked-wallet invoice generation, 1p dispute-raise and per-deal fund lock helper, 1q payout-reset predicates, 1r payout-tracking clear/CV-anchor helpers, 1s payment-check/refresh-status methods, 1t outgoing-payout status check helper, 1u hold settle/cancel guard primitives, 1v deal-hold cancel helper, 1w sweep, 1x exit flows, 1y operator dispute resolution/reconcile, 1z funding hold flows, 1aa public payout-attempt reset, 1ab public milestone reject, 1ac milestone hold prepare/fund/refresh, 1ad public approve, 1ae deal release escrow flow, 1af milestone release/accept/resolve. Fixed a mangled comment artifact in `service.go` (no logic change). Earlier bad cuts reverted then completed. Actual file boundaries differ from initial proposed grouping but remain mechanical same-package splits. |
| 2 | Split `backend/internal/deals/handler.go` (same package, mechanical) | done | `12a3c97` (2a), `acfad3a` (2b), `bff48a8` (2c), `8f8c7ef` (2d), this commit (2e) | 2026-11-20: 2a extracted public handlers/types → `handler_public.go`. 2b extracted arbitration/operator handlers, route registration, request types → `handler_arbitration.go`. 2c extracted authed milestone handlers/request/response/view helper types → `handler_milestones.go`. 2d extracted route registration (`RegisterRoutes`, `RegisterPublicRoutes`) → `handler_routes.go`. 2e extracted remaining authed deal handlers/request types and left `handler.go` as core `Handler` type/constructor only (~11 LOC). Verified each sub-step with `gofmt`, `go test ./internal/deals`, `go vet ./...`, `go test ./...` ok. | `artifact_handler.go` remained separate. Actual boundaries: `handler_deals.go` (authed deal lifecycle/workflow/share/payee/invoice/exit), `handler_milestones.go`, `handler_public.go`, `handler_arbitration.go`, `handler_routes.go`, `handler.go` (core), `milestone_view.go` (Step 2 prep). Same package/exported API preserved. |
| 3 | Split `backend/internal/deals/repository.go` (same package, mechanical) | in progress | this commit (3a) | 2026-11-20: 3a extracted artifact/verification repository methods (`CreateArtifact`, `DeleteArtifact`, `GetArtifactByID`, `ListArtifactsByDeal`, `CreateVerification`, `GetVerificationByID`, `ListVerificationsByArtifact`) → `repository_artifacts.go`. Verified `cd backend && gofmt -w internal/deals/repository.go internal/deals/repository_artifacts.go && go test ./internal/deals && go vet ./... && go test ./...` ok. | Core deal/milestone/tx/scan helpers remain in `repository.go`. |
| 4 | Split `frontend/src/lib/api/api-client.ts` (compatibility barrel) | not started | — | — | — |
| 5 | Extract frontend deal/public-deal page sections | not started | — | — | — |
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
| `service_release_payout.go` | Approve/release/settle/cancel/reconcile/payout tracking (**critical**) | `ApproveDeal`, `PublicApproveDeal`, `releaseEscrow`, `ReleaseMilestone`, `releaseMilestoneViaCanonicalPath`, `settleHoldGuarded`, `settleMilestoneHoldGuarded`, `cancelEscrowHold`, `cancelMilestoneHold`, `checkOutgoingPayout`, `clearPayoutAttempt`, payout/hold status helpers, `ReconcilePayout`, `PublicResetPayout`, reset-attempt guards, `anchorReleasedDeal` |
| `service_dispute_exit.go` | Disputes, arbitration outcomes, exits | `DisputeDeal`, `ListDisputes`, `ResolveDispute`, `refundDispute`, `ResolveMilestone`, `ExitDeal`, `PublicExitDeal`, `exitDeal`, `PublicRejectMilestone` |
| `service_milestone.go` | Milestone CRUD/state helpers and progression plumbing not covered above | `CreateMilestone`, `GetMilestonesByDeal`, `GetMilestoneByID`, `UpdateMilestoneStatus`, `prepareMilestoneHold`, `FundMilestone`, `PublicAcceptNextMilestone` (final placement decided to avoid cycles/duplication) |
| `service_artifact.go` | Artifacts, storage helpers, verifications | `MaxUploadBytes`, `UploadArtifact`, `DeleteArtifact`, `DownloadArtifact`, `openStoredArtifact`, `ListPublicArtifacts`, `DownloadPublicArtifact`, artifact key/sanitize/counting reader, `GetArtifactByID`, `ListArtifactsByDeal`, `CreateVerification`, `GetVerificationByID`, `ListVerificationsByArtifact` |
| `service_public.go` | Public/share views and token management (if not folded above) | `GetPublicDeal`, `RotateShareLink` |

Notes:
- Exact file boundaries may be tuned during implementation to keep related private helpers with their callers; do not split a function across concerns.
- `service_test.go` and `milestone_service_test.go` stay as-is initially (same package tests should continue to compile). Splitting tests is optional later.

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

## Planned feature: milestone funding/build timing (F1)

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

**Execution:** do as dedicated step(s) after or interleaved with approved refactor steps, but never mixed into mechanical split commits. Record design decisions and deviations in the progress tracker.


## UI ideas / UX improvements (tracked)

Document UI changes/ideas here as they are implemented or planned, especially when they affect deal status visibility and escrow/payment workflows.

| Item | Scope | Status | Notes |
|---|---|---|---|
| UI-1 | Authed deal page (`frontend/src/app/deals/[dealId]/page.tsx`) status refresh controls | implemented | Added two explicit actions instead of requiring full page refresh: **Check Payment** (freelancer, shown when deal is `awaiting_payment`) calls `GET /deals/:dealID/payment` via `api.checkPayment` to re-check LNbits/accepted-hold state and refresh milestones/artifacts; **Check Escrow Status** (freelancer and client) re-fetches deal, milestones, and artifacts from backend (`GET /deals/:dealID` + list endpoints) for current escrow/workflow state. `frontend/src/lib/api/api-client.ts` now exposes `checkPayment(dealId, token)`. Verified `cd frontend && npx tsc --noEmit` clean (2026-11-20). Public deal page already polls/fetches deal status periodically; no raw hold settle/cancel controls added. |

## Review notes for approver
- Highest-risk steps are **1 (funding/release code movement)** and **5 (React page extraction/state boundaries)**.
- Safest steps are **2, 3, 4 (barrel)** when done mechanically.
- Request: approve step-by-step; after each approved step I will implement only that step, run checks, report results, then wait before continuing.
