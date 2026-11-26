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

Current implementation has landed **(1) milestones inside a deal**.

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
Legacy single-hold deals use **one hold invoice per deal** with release/refund of that hold. Milestone mode uses sequential per-milestone holds (see the decision below). Milestones introduce partial settlement needs:
- Pay milestone 1 but keep milestone 2 funded?
- Release partial amounts from a single hold? (Lightning hold invoices don’t naturally support partial capture in the way fiat auth/capture does.)
Possible approaches:
- **Per-milestone hold invoices** (each milestone is its own escrow leg) — operationally more invoices, but aligns with partial payouts.
- **Single hold + internal accounting** — risky/misaligned with “network-as-escrow” truth; avoid.
- **Sequential deals per milestone** under one “job” grouping — simpler escrow, more records.
**Decision: sequential per-milestone holds** — each milestone has its own hold invoice, funded in order via the public fund endpoint with an optional `milestone_id`. A deal locks only when LND on the escrow node Es verifies a matching accepted hold (hash + amount, `State == ACCEPTED`, `settled == false`, `settle_date == 0`); LNbits `paid=true` is not proof of funds and no longer locks. The escrow node is `polar-n3-Es` (Alice1/Es), the client is Bob/`polar-n3-Cl`, the freelancer is `polar-n3-Fre`, and `polar-n3-Pay` is route-only Es → Fre if needed (not a second Ganji wallet). The Alice2/relay workflow was reverted and is not current.

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
- **Freelancer-set work window drives expiry:** each new milestone carries `work_start_at`/`work_due_at`; `delivery_days` is derived from that window and legacy day-count payloads still work. Hold expiry is the time until the explicit due date (M1 at deal creation, M2+ when fundable/funded); legacy rows use `delivery_days * 86400` from hold creation. Generated payout invoices pass `expiry_seconds` derived from the same due date, so neither escrow nor payout invoices use a fixed 30-day schedule. `due_at` prefers the explicit due date over `funded_at + delivery_days`. `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` remains a fallback/default for holds without per-milestone expiry. Sweep reconciles UNPAID/EXPIRED/CANCELLED holds by LNbits status and never auto-cancels funded/accepted holds.
- **Escrow is mandatory for money moves:** a milestone with **no escrow hold** (`checking_id`/`preimage_hash` absent) **cannot be released/paid or advanced as paid**. Release/approve/accept-next requires the milestone’s hold to be funded first (`FundMilestone`, or `POST /public/deals/:shareToken/fund` with `{wallet_url, wallet_key, milestone_id?}`); the canonical path errors with “no escrow hold (fund it first)”. Never mark `released/paid` (and never advance escrow sequence as paid) without settle+pay. `locked` means **accepted hold** (not settled): work submission requires `locked`, and `awaiting_payment → work_submitted` is invalid. Without the LND checker on Es, verification is blocked and `FundDeal` returns `503`.
- **Exit when work NOT submitted/approved** (pre-submission: `pending`/`awaiting_payment`/`funded`/`in_progress`): milestone **not paid** to freelancer; that milestone hold is **cancelled/refunded to the client**. Symmetric for client/freelancer exit. Later unfunded milestones do not get paid.
- **Submitted/approved = material consequences (no auto-refund/release on exit):** if `work_submitted` (or `completed` but not `released`), **exit does not move money**. The milestone hold **stays in escrow** and the case is forced to **independent review/dispute/explicit resolution** (client reviews; if rejected → review/dispute; resolver releases to freelancer or refunds client). Participants cannot exit to bypass review.
- **Approve vs reject (submitted work):** **approved → reveal R, release/settle the milestone hold and pay the freelancer directly** (then fund next milestone). **Rejected after `work_submitted` → do NOT reveal R and do NOT settle/cancel/fail the HTLC**; **hold stays** and it goes to **independent review/decision** there (like the original single-milestone flow). **Independent review resolution decides money:** the arbiter reveals R to pay the freelancer (`release`, canonical payout path) OR cancels/waits for expiry to refund the client (`refund` — **no R used**). Idempotent.
- **`completed` vs `released` (hold semantics):** **`released` = hold settled + freelancer paid (money moved, terminal paid state)**. `completed` (if used) means **approved/payout-pending — never “paid”** — and must have exactly one idempotent payout-safe path to `released`. UI/API must treat only `released` as paid.
- **Public share-link client UI:** clients don’t log in. Share-link actions authorized by **`:shareToken` only** (no auth session, no internal IDs/secrets leaked). Funding uses `POST /public/deals/:shareToken/fund` with a transient linked wallet URL/key (`http/https` host only — no userinfo/query/fragment; dev-only exact-origin allowlist for local Polar wallets, blocked in production; keys are not stored or logged). Payout invoices use protected `POST /deals/wallet/invoice`.

## Implementation Status (WIP, not production-complete)
_Last updated: 2026-11-18 (branch `main`, post-`22c915b`; accepted-hold funding added)_

### Done
- **Fresh migration (not in prod):** consolidated into `backend/migrations/000008_create_milestones_table.up.sql/.down.sql` only (legacy patch migration files harmonized into `000008`). Creates `milestones` with full per-milestone escrow/status columns (includes `disputed`) and makes `deals.preimage_hash`/`deals.invoice` nullable for milestone mode.
- **Domain/repo:** `types.go`, `types_interfaces.go`, `repository.go`, `errors.go` — milestone statuses/transitions (incl. `disputed`), CRUD, escrow/payout tracking fields.
- **Sequential escrow (core path wired):** `CreateDealWithMilestones` creates milestones, skips total upfront deal hold, **funds M1** (`FundMilestone` → hold invoice). Legacy single-hold flow preserved when `milestones` empty.
- **Accepted-hold funding (current):** public `FundDeal` pays the hold from a client-supplied linked wallet and locks the deal only after LND on Es verifies the accepted hold; `paid=true` alone is not enough, and a missing checker returns 503. Payout invoices are generated through `POST /deals/wallet/invoice`. Tests in `service_test.go` / `service_milestone_test.go` cover lock, refusal, checker requirement, and linked-wallet calls.
- **Canonical payout path (`completed→released` enforced for escrow milestones):** `ReleaseMilestone` is the **only** path that settles hold + pays freelancer + marks `MilestoneReleased` (**money moved**). Locked/idempotent with conservative payout policy (durable intent; paid → no re-pay; failed → clear+retry; ambiguous/in-flight/unknown crash-window → `ErrPayoutInFlight`). `completed` (if used) = **approved/payout-pending, never paid**.
- **Escrow mandatory + bypasses removed (`b8c2971`, hardened):** approve/accept-next route payouts through `releaseMilestoneViaCanonicalPath` → **escrow-backed milestones only** go to `ReleaseMilestone`. A milestone with **no escrow hold** is an explicit error (**“fund it first”**); it is **never** marked `released/paid` or advanced/finalized as paid. No ad-hoc `released` status writes for payouts. **Fund-next/finalize deal only when the target milestone is actually `released` (paid).** `disputed`/`rejected` blocks normal approve/accept-next (requires independent resolution).
- **Post-submission hold-stays / independent review:** `work_submitted`/`completed`/`rejected` exit does **not** move money → milestone moved to `disputed` for review. `PublicRejectMilestone` (submitted/completed, share-token) does **not** auto-refund → moves to `disputed` (hold stays). **Operator per-milestone resolution** `ResolveMilestone` (`POST /disputes/deals/:dealID/milestones/:milestoneID/resolve`, `OperatorRequired` arbitration routes): `release` → canonical `ReleaseMilestone` (+advance sequence/finalize); `refund` → `CancelHold` + `refunded` (idempotent guards). Follows “rejected → independent review → release or refund”.
- **Exit:** symmetric participant exit (authenticated; public share-token). Cancels/refunds open holds for **pre-submission** states per agreed rule; post-submission → dispute/review (no money move).
- **Routes:** milestone CRUD + auth exit; share-token-only public actions (`/public/deals/:shareToken/*`); operator milestone resolve under arbitration.
- **Frontend:** `deals/new` milestone creation; authenticated/public milestone UI. Labels clarified: `released` = **paid**, `completed` = **payout pending (not paid)**, `disputed`/`rejected` = **under independent review (hold stays)**. `layout.tsx` icons fixed. Builds pass (`go build ./...`, deals compile-check, `frontend npm run build`).
- **Notifications:** `DealExited`/`MilestoneMoved` wired (best-effort).

### Partially done (needs hardening)
- **Milestone release tx/lock parity (done 2026-11-09):** `ReleaseMilestone` now mirrors `releaseEscrow` (Phase-3 re-validation, disputed-during-release guard, confirmed/failed/ambiguous + crash-window verdicts, already-settled settle). Tests in `service_milestone_test.go`.
- **Dispute/review completeness:** operator release/refund + refund idempotency done; rework-after-reject policy (rejected → in_progress/work_submitted vs dispute-only) and deal-level dispute coherence for milestone mode still need refinement.
- **Deal-level coherence:** deal `status/invoice/checking_id`/current milestone mirrored for compatibility but not a fully explicit authenticated contract.
- **Artifacts/evidence:** first pass requires ≥1 deal-level artifact in milestone mode; per-milestone tagging is wired at the DB/model/repository/service layer, with frontend upload-time tagging still a follow-up.

## Migration application (fresh DB / not in prod)
- Apply migrations in order through `000008` (e.g. `migrate -path backend/migrations -database "$DATABASE_URL" up` or project equivalent).
- If a **dev DB** applied old incremental patch migrations before consolidation: while **not in prod**, **reset/recreate the dev DB and migrate fresh** (or carefully align `schema_migrations` to consolidated `000008`). **Do not do this in prod.**

## What’s left / TODOs
- [x] **Canonical `completed→released` payout path (core):** `ReleaseMilestone` only pays/marks `released`; bypasses removed (`releaseMilestoneViaCanonicalPath`); `completed` = payout-pending not paid; UI labels updated.
- [x] **Escrow mandatory (no-escrow path hardened):** releasing/advancing a milestone with no hold is an explicit **“fund it first”** error — never mark/advance as paid without escrow.
- [x] **Post-submission reject/exit hold-stays + independent review (core):** no auto-refund; reject/exit → `disputed`; operator `ResolveMilestone` releases (canonical) or refunds (cancel hold).
- [x] **Idempotency/atomicity parity (2026-11-09, `service_milestone_test.go`):** `ReleaseMilestone` Phase-3 re-validation (disputed-during-release abort + `CanMilestoneTransition` gate), `awaiting_payment→released` removed (must refresh/submit first), `rejected→released` operator path added; `cancelMilestoneHold` idempotency (already-gone hold = success, still-held stays disputed); `UpdateMilestoneStatus` blocks direct `released`/`refunded` and enforces transitions; `CreateMilestone` validates title/amount/order; `ResolveMilestone` escrow-mandatory. 23 tests cover double-call, in-flight/ambiguous, crash-window, refund races, no-escrow, settle-already-settled, disputed-during-release, reject/exit money rules, share-token auth, transitions. _Remaining:_ rework-after-reject policy, deal/milestone dispute coherence.
- [x] **Artifacts/evidence model (first pass):** milestone-mode submit now requires ≥1 deal-level artifact (same gate as legacy). Per-milestone artifact scoping — `milestone_id` on artifacts (nullable, backward-compatible), filtering via `ListArtifactsByDeal(dealID, milestoneID)`, and CV-per-milestone anchoring — is now implemented at the DB/model level and wired through the repository/service layer. Full frontend upload-time tagging is a follow-up.
- [x] **Deal lifecycle/current-milestone contract (done):** authed `DealView.milestones[]` on create/get/list (active = first non-terminal milestone ordered by `order`); `invoice`/`checking_id`/`status` mirror the active milestone. Public view already had this contract; frontend `DealView` mirrors it.
- [x] **UX/API polish (done):** authenticated per-milestone submit/accept-next UI with role gating already covered by the shared submit/approve endpoints (active milestone resolved server-side); operator resolve endpoint now documented in `API_REFERENCE`. Remaining: public milestone list re-verified end-to-end in E2E.
- [ ] **E2E (real LNbits + LND accepted-hold checker on Es + DB, not run here):** apply fresh `000008` → create milestone deal → fund M1 via linked wallet (locks only on accepted hold) → submit → approve/release M1 (R revealed, freelancer paid) → fund/pay M2 → … plus reject→review→resolve (arbiter reveals R or cancels/waits expiry; refund uses no R) and exit/refund (pre vs post-submission).
