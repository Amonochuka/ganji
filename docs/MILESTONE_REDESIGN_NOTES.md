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
- **Sequential per-milestone funding/invoices**: fund/release M1, then fund/start M2, etc. Money-path wiring is sequential per-milestone holds.
- **Exit when work NOT submitted/approved** (pre-submission: `pending`/`awaiting_payment`/`funded`/`in_progress`): milestone **not paid** to freelancer; that milestone hold is **cancelled/refunded to the client**. Symmetric for client/freelancer exit. Later unfunded milestones do not get paid.
- **Submitted/approved = material consequences (no auto-refund/release on exit):** if `work_submitted` (or `completed` but not `released`), **exit does not move money**. The milestone hold **stays in escrow** and the case is forced to **independent review/dispute/explicit resolution** (client reviews; if rejected → review/dispute; resolver releases to freelancer or refunds client). Participants cannot exit to bypass review.
- **Approve vs reject (submitted work):** **approved → release/settle milestone hold + pay freelancer** (then fund next milestone). **Rejected → do NOT auto-refund**; **hold stays** and it goes to **independent review/decision** there (like the original single-milestone flow). **Independent review resolution decides money:** `release` → pay freelancer (canonical payout path) OR `refund` → cancel hold/refund client. Idempotent.
- **`completed` vs `released` (hold semantics):** **`released` = hold settled + freelancer paid (money moved, terminal paid state)**. `completed` (if used) means **approved/payout-pending — never “paid”** — and must have exactly one idempotent payout-safe path to `released`. UI/API must treat only `released` as paid.
- **Public share-link client UI:** clients don’t log in. Share-link actions authorized by **`:shareToken` only** (no auth session, no internal IDs/secrets leaked).

## Implementation Status (WIP, not production-complete)
_Last updated: 2026-11-09 (branch `main`, post-`22c915b`)_

### Done
- **Fresh migration (not in prod):** consolidated into `backend/migrations/000008_create_milestones_table.up.sql/.down.sql` only (no `000009/000010`). Creates `milestones` with full per-milestone escrow/status columns (includes `disputed`) and makes `deals.preimage_hash`/`deals.invoice` nullable for milestone mode.
- **Domain/repo:** `types.go`, `interface_types.go`, `repository.go`, `errors.go` — milestone statuses/transitions (incl. `disputed`), CRUD, escrow/payout tracking fields.
- **Sequential escrow (core path wired):** `CreateDealWithMilestones` creates milestones, skips total upfront deal hold, **funds M1** (`FundMilestone` → hold invoice). Legacy single-hold flow preserved when `milestones` empty.
- **Canonical payout path (`completed→released` enforced for escrow milestones):** `ReleaseMilestone` is the **only** path that settles hold + pays freelancer + marks `MilestoneReleased` (**money moved**). Locked/idempotent with conservative payout policy (durable intent; paid → no re-pay; failed → clear+retry; ambiguous/in-flight/unknown crash-window → `ErrPayoutInFlight`). `completed` (if used) = **approved/payout-pending, never paid**.
- **Bypasses removed (`b8c2971`):** approve/accept-next (authenticated + public) route payouts through `releaseMilestoneViaCanonicalPath`: escrow-backed milestones → `ReleaseMilestone`; **no-escrow/state-only** milestones mark `completed` (not `released/paid`). No ad-hoc `released`/paid status writes for escrow payouts. **Fund-next/finalize deal only when the target milestone is actually `released` (paid).** `disputed`/`rejected` blocks normal approve/accept-next (requires independent resolution).
- **Post-submission hold-stays / independent review:** `work_submitted`/`completed`/`rejected` exit does **not** move money → milestone moved to `disputed` for review. `PublicRejectMilestone` (submitted/completed, share-token) does **not** auto-refund → moves to `disputed` (hold stays). **Operator per-milestone resolution** `ResolveMilestone` (`POST /disputes/deals/:dealID/milestones/:milestoneID/resolve`, `OperatorRequired` arbitration routes): `release` → canonical `ReleaseMilestone` (+advance sequence/finalize); `refund` → `CancelHold` + `refunded` (idempotent guards). Follows “rejected → independent review → release or refund”.
- **Exit:** symmetric participant exit (authenticated; public share-token). Cancels/refunds open holds for **pre-submission** states per agreed rule; post-submission → dispute/review (no money move).
- **Routes:** milestone CRUD + auth exit; share-token-only public actions (`/public/deals/:shareToken/*`); operator milestone resolve under arbitration.
- **Frontend:** `deals/new` milestone creation; authenticated/public milestone UI. Labels clarified: `released` = **paid**, `completed` = **payout pending (not paid)**, `disputed`/`rejected` = **under independent review (hold stays)**. `layout.tsx` icons fixed. Builds pass (`go build ./...`, deals compile-check, `frontend npm run build`).
- **Notifications:** `DealExited`/`MilestoneMoved` wired (best-effort).

### Partially done (needs hardening)
- **Milestone release tx/lock parity:** `ReleaseMilestone` is locked/idempotent but not yet at full deal `releaseEscrow` parity across every edge/race; needs tests (double-call, ambiguous payout, crash-window).
- **Dispute/review completeness:** operator release/refund exists per milestone; refund idempotency on “hold already gone/ambiguous cancel” needs parity with `cancelEscrowHold`; rework-after-reject policy/state transitions and deal-level dispute coherence for milestone mode need refinement.
- **Deal-level coherence:** deal `status/invoice/checking_id`/current milestone mirrored for compatibility but not a fully explicit authenticated contract.
- **Artifacts/evidence:** milestone-mode submit bypasses deal-level artifact requirement.

## Migration application (fresh DB / not in prod)
- Apply migrations in order through `000008` (e.g. `migrate -path backend/migrations -database "$DATABASE_URL" up` or project equivalent).
- If a **dev DB** applied old incremental `000009/000010` before consolidation: while **not in prod**, **reset/recreate the dev DB and migrate fresh** (or carefully align `schema_migrations` to consolidated `000008`). **Do not do this in prod.** No incremental add-column migrations added.

## What’s left / TODOs
- [x] **Canonical `completed→released` payout path (core):** `ReleaseMilestone` only pays/marks `released`; bypasses removed (`releaseMilestoneViaCanonicalPath`); `completed` = payout-pending not paid; UI labels updated. _Follow-up:_ enforce everywhere incl. all edge/legacy cases + full tx/lock parity + tests.
- [x] **Post-submission reject/exit hold-stays + independent review (core):** no auto-refund; reject/exit → `disputed`; operator `ResolveMilestone` releases (canonical) or refunds (cancel hold). _Follow-up:_ refund idempotency for ambiguous/“hold gone” cancel, rework/resubmit-after-review policy, deal/milestone dispute coherence.
- [ ] **Idempotency/atomicity parity (finish):** full `releaseEscrow`-level discipline for milestone release/resolve; tests for double-call, in-flight/ambiguous payout, crash-window, and refund/cancel races.
- [ ] **Artifacts/evidence model:** milestone-mode submit bypasses deal-level artifact requirement — decide per-milestone artifacts/submissions and enforce.
- [ ] **Deal lifecycle/current-milestone contract:** explicit deal-level `status/invoice/checking_id` + “active milestone” for legacy/authenticated consumers.
- [ ] **UX/API polish:** authenticated per-milestone submit/accept-next with role gating and active milestone invoice/payment state; document operator resolve endpoint in `API_REFERENCE`/ops docs.
- [ ] **Tests:** transitions, share-token-only auth, escrow edge cases, dispute resolve release/refund.
- [ ] **E2E (real LNbits + DB, not run here):** apply fresh `000008` → create milestone deal → pay M1 → submit → approve/release M1 (freelancer paid) → fund/pay M2 → … plus reject→review→resolve and exit/refund (pre vs post-submission).
