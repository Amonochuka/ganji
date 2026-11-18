# Deal Lifecycle (State Machine)

Defined in `backend/internal/deals/types.go` (`ValidTransitions`) and mirrored
by the DB `CHECK` constraint in
`backend/migrations/000003_create_deals_table.up.sql`.

## State Diagram

```
                ┌──────────────────────────────┐
                │  awaiting_payment            │  ← client funds hold invoice
                └──┬───────────────────────────┘  (on Es; verified via LND)
      accepted hold on Es
                   ▼
              ┌────────┐
              │ locked │  (Funds Held)
              └───┬────┘
   freelancer submits
                   ▼
              ┌────────────────┐
              │ work_submitted │
              └───┬────────┬───┘
          dispute │        │ approve (reveal R → pay Fre)
                  ▼        ▼
             ┌──────────┐┌──────────┐
             │ disputed ││ released │
             └────┬─────┘└──────────┘
       arbiter    │  (terminal)
       resolves   ▼
             ┌────────┐
             │refunded│  terminal
             └────────┘
```

- `locked` is displayed as **Funds Held**; it requires accepted-hold
  verification on `polar-n3-Es` (LNbits `paid=true` alone is not proof).
- `reviewing` is an optional formal phase between submitted and approve.
- `disputed` = money frozen pending arbitration. `DisputeDeal` requires a
  written reason and cancels nothing — the hold stays on the network. Rejecting
  work after submission does not reveal `R` and does not settle, cancel, or
  fail the HTLC. `ResolveDispute` (operator only) reveals `R` to pay Fre, or
  cancels/waits expiry to refund Bob (refund never uses `R`); a client who
  changes their mind can still approve from `disputed`.
- `released` is reached by client approve or an operator release resolution;
  both reveal `R` and pay `polar-n3-Fre` directly.
- `refunded` is reached by an operator refund resolution or by the
  hold-expiry sweep for deals that were never funded.

## Valid Transitions Table

| From | Allowed To | Set by | Money move? |
|---|---|---|---|
| `awaiting_payment` | `locked`, `refunded` | backend accepted-hold check / sweep | `locked` = Funds Held |
| `locked` | `work_submitted`, `disputed` | freelancer / client | — |
| `work_submitted` | `reviewing`, `released`, `disputed` | — | — |
| `reviewing` | `released`, `disputed` | — | — |
| `disputed` | `released`, `refunded` | client approve / arbiter | reveal `R` + pay Fre, or cancel/expiry refund |
| `released` | *(terminal)* | client approve / arbiter release | reveal `R` + pay Fre |
| `refunded` | *(terminal)* | arbiter / sweep | cancel hold |

`awaiting_payment → work_submitted` is not legal: work submission requires
`locked` (**Funds Held**), which requires accepted-hold verification on Es.

## Milestone Lifecycle (sequential per-milestone escrow)

Deals may instead (or additionally) carry `milestones` rows (migration `000008`, one escrow leg per milestone). In milestone mode the deal-level flow above is mirrored, not replaced: `deal.Invoice`/`deal.CheckingID` mirror the active milestone's hold for compatibility, and the deal becomes `released` only when its final milestone is `released`.

Milestone statuses (from `ValidMilestoneTransitions` in `backend/internal/deals/types.go`):

| From | Allowed To |
|---|---|
| `pending` | `awaiting_payment`, `in_progress`, `exited` |
| `awaiting_payment` | `funded`, `in_progress`, `exited`, `refunded` |
| `funded` | `in_progress`, `exited`, `refunded`, `disputed` |
| `in_progress` | `work_submitted`, `released`, `exited`, `refunded`, `disputed` |
| `work_submitted` | `released`, `completed`, `rejected`, `disputed`, `exited` |
| `completed` | `released`, `disputed` |
| `rejected` | `disputed`, `in_progress`, `work_submitted`, `released` (operator-only canonical release) |
| `disputed` | `released`, `refunded` |
| `released` / `refunded` / `exited` | *(terminal)* |

### Milestone money rules

- **Sequential holds:** `CreateDealWithMilestones` funds M1 on creation (`FundMilestone`); M2+ get a hold only after the previous milestone is `released` (paid). Approving/accepting a released milestone funds the next one. `POST /public/deals/:shareToken/fund` accepts an optional `milestone_id`; it pays that milestone's hold and requires accepted-hold verification on Es before `awaiting_payment → funded → in_progress` (LNbits `paid=true` alone is not proof).
- **Escrow mandatory:** a milestone with no hold (`checking_id`/`preimage_hash` absent) cannot be `released`/paid or advanced as paid — the canonical path errors with "no escrow hold (fund it first)".
- **Canonical payout:** `ReleaseMilestone` is the only path to `released` (settle with `R` on Es + pay Fre directly, locked/idempotent with the durable payout-attempt marker; paid → no re-pay, failed → clear + retry, ambiguous/in-flight → `ErrPayoutInFlight`). `completed` = payout-pending, never paid.
- **Approve vs reject:** approved → release; rejected (from `work_submitted`/`completed`) → **no auto-refund**, no `R` reveal, no settle/cancel/fail of the HTLC; hold stays → `disputed` for independent review. Operator `ResolveMilestone` reveals `R` to pay Fre (canonical) or cancels/waits expiry to refund Bob (`refund`, no `R`) idempotently.
- **Exit (symmetric, freelancer or client):** pre-submission (`pending`/`awaiting_payment`/`funded`/`in_progress`) → cancel/refund the open hold (or expiry returns it), not paid to Fre; unfunded `pending` → `exited`, funded → `refunded`. Post-submission (`work_submitted`/`completed`/`rejected`) → no money moves, hold stays, milestone → `disputed` for review. Legacy single-hold exit follows the same rule.

## Rules That Keep Money Honest

- The freelancer's generic `PATCH /deals/:dealID/status` **cannot** set
  `locked`, `released`, `disputed`, or `refunded`. Those money states are set
  only by the backend: payment detection, approve, dispute, or arbitration.
- `released` is only reachable through a **successful network settle** on Es
  (revealing `R` proves the client really funded the hold and pays Fre directly;
  there is no Ganji settlement wallet).
- `refunded` is **never** reachable directly from a client-facing state. A
  dispute freezes the funds in `disputed` instead; the sats only return via
  arbitration (cancel or expiry), or via the hold-expiry sweep for deals that
  were never funded (expired/cancelled/unpaid holds). Refund never uses `R`.
  This closes the pay → take the work → cancel loop.

If you add a status constant, update the DB `CHECK` constraint too or Postgres
rejects the writes.