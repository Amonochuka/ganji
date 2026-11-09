# Milestone & Exit Flow Redesign Notes

Living document for the milestone/exit redesign discussion and implementation status. Update this whenever the idea changes.

## Problem
Current deals are single, all-or-nothing units:
- Client pays once and waits for the whole job.
- Freelancer submits once and gets paid once on release.
- Hard to assess progress step-by-step.
- If the relationship breaks mid-job, it’s unclear how to pay for partial work fairly.

## Proposed Direction
Two related options (user exploring both):
1. **Split a deal into milestones** (recommended baseline): one escrow relationship, multiple deliverable/payment checkpoints.
2. **One job broken into many deals**: simpler to model with current code, but heavier UX/admin and weaker “single job” coherence.

Current implementation work in this branch leans toward **(1) milestones inside a deal**.

## Core Ideas to Support
- Client can review work step-by-step, not just at the end.
- Freelancer can get paid for completed work even if the whole job isn’t finished.
- Client can exit after milestone 1 (and generally at milestone boundaries).
- Notifications:
  - freelancer notified when deal/milestone is approved
  - freelancer notified when client exits
  - freelancer notified when client accepts or rejects moving to next milestone (e.g., milestone 2)
- Notifications can be UI, email, or both.

## Design Questions / Critique (important)

### 1) Who can exit?
User suggested freelancers should also be able to exit.
- **Client exit**: makes sense (stops project). Must define what happens to funds for completed vs incomplete milestones.
- **Freelancer exit**: also reasonable (can’t continue, scope mismatch, etc.). Must define:
  - can they exit only before starting a milestone?
  - if mid-milestone, is work partial? who decides payout/refund?
  - does it trigger dispute/arbitration or a simpler partial-settlement flow?
**Recommendation:** allow both, but with different rules and explicit confirmation + reason. Avoid silent exits.

### 2) Client vs freelancer UI/UX must differ
Current system has an asymmetry:
- Freelancers are authenticated users (dashboard, deal management).
- Clients often interact via **public share link** (no login) to pay/approve.
Implications:
- Milestone review/accept/reject/exit for clients must work in **both** authenticated-client and public-link contexts (or we must deliberately require client accounts — product decision).
- Freelancer UI needs milestone creation, submission per milestone, progress tracking.
- Don’t reuse the same action set/components without role-aware states; permissions and copy differ.

### 3) Payment/esccrow model (biggest risk)
The current escrow implementation is **one hold invoice per deal** with release/refund of that hold. Milestones introduce partial settlement needs:
- Pay milestone 1 but keep milestone 2 funded?
- Release partial amounts from a single hold? (Lightning hold invoices don’t naturally support partial capture in the way fiat auth/capture does.)
Possible approaches:
- **Per-milestone hold invoices** (each milestone is its own escrow leg) — operationally more invoices, but aligns with partial payouts.
- **Single hold + internal accounting** — risky/misaligned with “network-as-escrow” truth; avoid.
- **Sequential deals per milestone** under one “job” grouping — simpler escrow, more records.
**This must be decided before calling the redesign “done”.** Current code changes add milestone records/statuses but do **not** fully solve partial escrow settlement.

### 4) State model complexity
Adding milestones creates two state machines (deal + milestone). Need clear rules:
- Can a deal be `released` while some milestones are pending/exited?
- What does `disputed` mean at milestone level vs deal level?
- Does client “reject milestone” block progress, request changes, or trigger dispute?
Keep transitions explicit and auditable.

### 5) Notifications
Must be reliable enough for UX but must not break money-path correctness (current code treats notifications as best-effort). Include:
- approval/release events
- client exit / freelancer exit
- milestone accepted (move to next) / milestone rejected
- in-app/UI indicators + email where configured

## Agreed Rules (captured)
- **Sequential per-milestone funding/invoices**: fund/release M1, then fund/start M2, etc. (good design; matches partial payout & limits exposure). Money-path wiring is separate from state/UX scaffolding.
- **Client exit mid-milestone (not submitted/approved)**: that milestone is not paid to freelancer; funds for that milestone refunded/cancelled per policy.
- **Freelancer exit**: symmetric/fair — can exit after a milestone or mid-milestone (including if they change mind). Mid-milestone (not submitted/approved) => no payout for that milestone; funds returned to client per same policy. Consequences become material when work is submitted/approved or disputed. Define explicitly per state: not started / in progress / submitted / approved.
- **Public share-link client UI**: required because clients don’t log in. Share-link actions are authorized by **`:shareToken` only** (no auth session, no internal IDs/secrets leaked), consistent with existing public deal flow.

## Implementation Status (WIP, not production-complete)
_Last updated: 2026-11-09 (branch `main`)_

### Done
- **Fresh migration (not in prod):** consolidated into `backend/migrations/000008_create_milestones_table.up.sql/.down.sql` only (no `000009/000010`). Creates `milestones` with full per-milestone escrow/status columns and makes `deals.preimage_hash`/`deals.invoice` nullable for milestone mode.
- **Domain/repo:** `types.go`, `interface_types.go`, `repository.go`, `errors.go` — milestone statuses/transitions (expanded), CRUD, escrow/payout tracking fields.
- **Sequential escrow (WIP, core path wired):** `CreateDealWithMilestones` creates milestones, skips total upfront deal hold, **funds M1** (`FundMilestone` → hold invoice). `ReleaseMilestone` settles hold + `PayInvoice` with conservative payout-attempt/idempotency checks (paid/in-flight/unknown/crash-window). `refreshMilestonePayment`. Authenticated + public **approve/submit/accept-next** release active funded milestone and fund next; final completes deal. Legacy single-hold flow preserved when `milestones` empty.
- **Exit:** symmetric participant exit (authenticated handler; public share-token exit). Cancels/refunds open milestone holds for **pre-submission** states (`pending/awaiting_payment/funded/in_progress`) per agreed rule. Leaves `work_submitted/completed` milestones for review/dispute resolution (not auto-refunded).
- **Routes:** milestone CRUD + auth exit; share-token-only public actions (`/public/deals/:shareToken/*`) — no auth session, no internal IDs leaked in public responses.
- **Frontend:** `deals/new` milestone creation UI (sequential, total derived); authenticated deal detail shows milestones + symmetric exit + milestone-aware approve/submit; client-facing public share-link milestone UI (fund invoice, approve/start-next, reject, exit); API client updated.
- **Build fixes:** `frontend/src/app/layout.tsx` Next `Metadata.icons` fixed. Backend `go build ./...` and frontend `npm run build` pass.
- **Notifications:** `DealExited`/`MilestoneMoved` wired (best-effort).

### Partially done (needs hardening)
- **Money consequences by state:** pre-submission exit/refund and normal approve/release/fund-next paths implemented; **not fully defined/implemented** for `work_submitted` + reject/dispute, post-submission exit, `completed` vs `released`, and operator resolution on per-milestone holds.
- **Idempotency/atomicity parity:** `ReleaseMilestone` has conservative checks but is **not yet at full `releaseEscrow` transactional/row-locking discipline**; retries/ambiguity need tests.
- **Deal-level coherence in milestone mode:** deal `status/invoice/checking_id` are partially mirrored for compatibility/current milestone; authenticated views don’t yet expose a first-class “current milestone/invoice” contract as clearly as public view.

## Migration application (fresh DB / not in prod)
- Apply migrations in order through `000008` (e.g. `migrate -path backend/migrations -database "$DATABASE_URL" up` or project equivalent).
- If a **dev DB** applied old incremental `000009/000010` before consolidation: while **not in prod**, **reset/recreate the dev DB and migrate fresh** (or carefully align `schema_migrations` to consolidated `000008`). **Do not do this in prod.** No incremental add-column migrations added.

## What’s left / TODOs
- [ ] **Money consequences by state (finish):** implement/decide hold resolution for `work_submitted`+rejected, disputed milestones, post-submission exit, and `completed` transitions. Ensure public responses remain non-sensitive.
- [ ] **Idempotency/atomicity parity:** align milestone release with deal `releaseEscrow` (locking/transaction boundaries) and cover retry/ambiguous-payout/crash-window cases with tests.
- [ ] **Artifacts/evidence model:** milestone-mode `SubmitWork` currently bypasses deal-level artifact requirement — decide per-milestone artifacts/submissions and enforce.
- [ ] **Deal lifecycle/current-milestone contract:** make deal-level `status/invoice/checking_id` and “active milestone” explicit/consistent for legacy + authenticated API consumers.
- [ ] **Reject/dispute wiring:** `PublicRejectMilestone` currently does state+notify; connect to milestone hold resolution (refund/cancel vs dispute) per agreed rules.
- [ ] **UX polish:** authenticated detail: show active milestone invoice/payment state, per-milestone actions (submit/accept-next) with role gating; clearer progress for sequential M1→M2.
- [ ] **Tests:** unit/integration tests for milestone transitions, share-token-only auth, and escrow edge cases.
- [ ] **E2E (real LNbits + DB, not run here):** create milestone deal → client pays M1 hold → freelancer submits → client approves/releases M1 (freelancer paid) → M2 funded → client pays M2 → … and exit/refund paths (pre- vs post-submission).
