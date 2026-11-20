# Refactor Split Plan (behavior-preserving)

Status: **planning only**. No production-logic changes in this document; implementation starts only after explicit approval, step by step.

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

## Review notes for approver
- Highest-risk steps are **1 (funding/release code movement)** and **5 (React page extraction/state boundaries)**.
- Safest steps are **2, 3, 4 (barrel)** when done mechanically.
- Request: approve step-by-step; after each approved step I will implement only that step, run checks, report results, then wait before continuing.
