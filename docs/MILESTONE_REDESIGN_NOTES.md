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
Backend:
- `backend/migrations/000008_create_milestones_table.up.sql` added.
- `deals` types include `Milestone`, `MilestoneStatus`, transitions; `PublicDeal` includes `milestones`.
- `DealRepository` + impl: milestone CRUD.
- Authenticated routes: create/list/update milestones, `POST /deals/:dealID/exit` (client).
- **Public (share-token) routes**: `GET /public/deals/:shareToken/milestones`, `POST /public/deals/:shareToken/milestones/:milestoneID/accept-next`, `POST /public/deals/:shareToken/milestones/:milestoneID/reject`, `POST /public/deals/:shareToken/exit` (alongside existing public deal/approve).
- Exit logic (state scaffolding): marks open milestones exited, best-effort cancels legacy hold for non-completed open milestone, notifies. `PublicAcceptNextMilestone`/`PublicRejectMilestone` added (state + notify; money settlement separate).
- `DealNotifier`: `DealExited`, `MilestoneMoved` (email best-effort). `go build ./...` passes.

Frontend:
- API client: `getPublicMilestones`, `publicAcceptNextMilestone`, `publicRejectMilestone`, `publicExitDeal`; `PublicDeal.milestones`.
- `frontend/src/app/public/deals/[shareToken]/page.tsx`: client-facing share-link UI shows milestones and actions (approve/start next, reject with reason, exit) — **state/UX scaffolding; not wired to per-milestone escrow settlement yet**.
- Authenticated deal detail still shows milestones and exit (WIP).

## Sequential Per-Milestone Holds (implemented slice, WIP)
- `000009` adds per-milestone escrow columns (`preimage_hash/preimage/payee_invoice/invoice/checking_id/payout_*`) and expanded milestone statuses.
- Backend service: `FundMilestone` creates a hold invoice for a milestone (escrow leg) and stores it; `ReleaseMilestone` settles hold + pays freelancer (idempotent-ish payout attempt marker); `refreshMilestonePayment` locks/funds on payment detection. `PublicAcceptNextMilestone` (share-token) **releases** the target milestone’s escrow if it has a hold, then **funds/creates the next milestone’s hold** (M1→M2 sequential). `exitDeal` cancels the open milestone’s hold (per-milestone) or falls back to legacy deal hold when not completed/released.
- Public deal view refreshes milestone payment state and returns milestone `invoice`.
- Public UI shows “Fund this milestone” invoice when `awaiting_payment`, and actions for approve/release/start-next.

## Known Gaps / TODO
- **Legacy single full-deal hold still created in `CreateDeal`** and legacy `ApproveDeal`/public approve release that single hold. To be fully sequential: stop creating a total upfront hold for milestone-based deals; fund M1 on demand (and define payee/amounts), then M2 after M1 release. Requires flow/product decision + careful backward compatibility.
- Add authenticated endpoints for fund/release milestone and freelancer-exit parity; enforce submitted/approved state transitions.
- Tighten idempotency/race handling for milestone release (mirror deal `releaseEscrow`’s full two-phase/confirm logic).
- Public reject semantics (request changes vs dispute/refund) and `000008.down.sql`/tests/rate limiting.

## Open Decisions
- Milestones-in-deal vs many-deals-per-job as the primary model.
- Per-milestone escrow vs grouped job escrow strategy.
- Whether clients must authenticate for milestone decisions, or public-link actions are sufficient (with security considerations).
- Exact payout/refund rules on client/freelancer exit at each milestone state.
