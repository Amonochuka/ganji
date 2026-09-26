# Deal Lifecycle (State Machine)

Defined in `backend/internal/deals/types.go` (`ValidTransitions`) and mirrored
by the DB `CHECK` constraint in
`backend/migrations/000003_create_deals_table.up.sql`.

## State Diagram

```
                ┌──────────────────────────────┐
                │  awaiting_payment            │  ← client pays hold invoice
                └──┬───────────────────────────┘  (funds held on the network)
        paid (CLN) │            │ freelancer submits
                   ▼            ▼
              ┌────────┐  ┌──────────────────┐
              │ locked │  │  work_submitted  │
              └───┬────┘  └───┬─────────┬────┘
   freelancer     │           │ client  │ client disputes
   submits        ▼           │ checks  ▼
              ┌────────────────┐   approve │
              │ work_submitted │◄─────────┘
              └───┬────────┬───┘
          dispute │        │ approve (settle hold → released)
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

- `reviewing` is an optional formal phase between submitted and approve.
- `disputed` = money frozen pending arbitration. `DisputeDeal` requires a
  written reason and cancels nothing — the hold stays on the network.
  `ResolveDispute` (operator only) moves it to `released` or `refunded`; a client
  who changes their mind can still approve from `disputed`.
- `released` is reached by client approve or an operator release resolution.
- `refunded` is reached by an operator refund resolution or by the
  hold-expiry sweep for deals that were never funded.

## Valid Transitions Table

| From | Allowed To | Set by | Money move? |
|---|---|---|---|
| `awaiting_payment` | `locked`, `work_submitted`, `refunded` | backend / freelancer / client | locked = funds held |
| `locked` | `work_submitted`, `disputed` | — | — |
| `work_submitted` | `reviewing`, `released`, `disputed` | — | — |
| `reviewing` | `released`, `disputed` | — | — |
| `disputed` | `released`, `refunded` | client approve / arbiter | settle + payout, or cancel hold |
| `released` | *(terminal)* | client approve | settle + payout |
| `refunded` | *(terminal)* | arbiter / sweep | cancel hold |

## Rules That Keep Money Honest

- The freelancer's generic `PATCH /deals/:dealID/status` **cannot** set
  `locked`, `released`, `disputed`, or `refunded`. Those money states are set
  only by the backend: payment detection, approve, dispute, or arbitration.
- `released` is only reachable through a **successful network settle**
  (revealing the preimage proves the client really funded the hold).
- `refunded` is **never** reachable directly from a client-facing state. A
  dispute freezes the funds in `disputed` instead; the sats only return via
  arbitration, or via the hold-expiry sweep for deals that were never funded
  (expired/cancelled/unpaid holds). This closes the pay → take the work →
  cancel loop.

If you add a status constant, update the DB `CHECK` constraint too or Postgres
rejects the writes.