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
- **Fresh migration (not in prod):** consolidated into `backend/migrations/000008_create_milestones_table.up.sql/.down.sql` (milestones table + full escrow/status columns + `deals.preimage_hash/invoice` nullable for milestone mode). Incremental `000009/000010` removed.
- Added milestone domain types/statuses and repo helpers: `backend/internal/deals/types.go`, `interface_types.go`, `repository.go`, `errors.go`.
- Sequential escrow (WIP): `CreateDealWithMilestones` stores milestones, skips total upfront hold, funds **M1**. `FundMilestone`/`ReleaseMilestone` (settle+pay; conservative payout-attempt/idempotency checks) / `refreshMilestonePayment`. Public+authenticated approve/submit/accept-next release active milestone and fund next; final completes deal. Exit cancels/refunds open milestone holds (agreed: mid-milestone not submitted/approved => not paid). Symmetric participant exit (auth handler).
- Handlers/routes: milestone CRUD + exit and share-token-only public actions.
- Frontend: milestone inputs on `deals/new`; authenticated deal detail milestone/symmetric actions; client-facing public share link; API client updated. `layout.tsx` Metadata icons fixed.
- Email notifications: `DealExited`/`MilestoneMoved`.

## Migration application (fresh DB / not in prod)
- Apply migrations in order through `000008` (`migrate -path backend/migrations -database "$DATABASE_URL" up` or project equivalent).
- If a **dev DB** already applied old incremental `000009/000010` before consolidation: while not in prod, **reset/recreate the dev DB** and migrate fresh (or carefully down+migrate to align `schema_migrations` with consolidated `000008`). Do **not** do this in prod.

## Important gaps / TODOs
1. **Money consequences by state (complete):** awaiting_payment/funded/in_progress/work_submitted/approved/disputed/rejected/exited — especially submitted-but-rejected/disputed (hold/resolve) vs pre-submission exit. Public responses non-sensitive.
2. **Idempotency/atomicity parity:** align milestone release with deal `releaseEscrow` transaction/locking discipline; add tests.
3. **Artifacts/evidence:** milestone-mode submit currently bypasses deal-level artifact requirement; decide per-milestone artifacts.
4. **Deal lifecycle coherence:** keep deal-level status/invoice/checking_id/current-milestone clear across sequential milestones; legacy compatibility.
5. **E2E with real LNbits+DB:** create milestone deal → pay M1 hold → submit → approve/release M1 → fund/pay M2 → exit/refund; add integration tests.
