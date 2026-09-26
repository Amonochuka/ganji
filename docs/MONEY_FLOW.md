# Money Flow: Network-as-Escrow (Hold Invoices)

LNbits hold invoices make the Lightning Network the escrow: an incoming payment
sits **held** under `payment_hash` until someone reveals the preimage
(settle → to the freelancer) or tears it down (cancel → back to the client).

## LNbits Endpoints Ganji Uses

| Action | Call | Auth |
|---|---|---|
| Create hold invoice (`out:false, amount, memo, payment_hash, webhook`) | `POST /api/v1/payments` | invoice key |
| Check status | `GET /api/v1/payments/{checking_id}` | invoice key |
| Settle (release escrow) — reveal stored preimage | `POST /api/v1/payments/settle` | **admin key** |
| Cancel (refund) — `{payment_hash}` | `POST /api/v1/payments/cancel` | **admin key** |
| Pay out — `out:true, bolt11` | `POST /api/v1/payments` | **admin key** |

`Deal` stores both sides of the secret: `preimage` (raw hex — needed later to
settle) and `preimage_hash = sha256(preimage)` (what LNbits locked the invoice
to). The preimage and the freelancer's `payee_invoice` are persisted but never
serialized into API responses: **every** authenticated deal endpoint serves the
redacted `DealView` (drops `preimage`, `payee_invoice`, and the payout-tracking
fields), and the public share link serves `PublicDeal`. The raw `*Deal` stays
internal to the service layer.

## CreateDeal (`service.go`)

1. Validate + trim input; require a valid `client_email` and a `payee_invoice`
   (the freelancer's payout destination).
2. Generate a 32-byte random **preimage**; store `preimage` + `preimage_hash`.
3. Generate the shareable-link **`share_token`** (see SHARE_LINK.md).
4. `lnbits.CreateHoldInvoice` → hold invoice + `checking_id`; `memo = title`
   (hold expiry: `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS`, default 30 days).
5. Insert the deal (transaction, `status = awaiting_payment`).

No money moves on creation.

## Orphan-Hold Protection (CreateDeal Failure Cleanup)

If `repo.CreateDeal` (or the transaction begin/commit) fails **after** the hold
invoice was created at LNbits, the invoice would otherwise remain on the network
with no deal row behind it — a client who pays it has their sats held for the
full 30-day expiry with no one able to settle or cancel (the preimage and
`preimage_hash` exist only in the uncommitted transaction).

To prevent this, `CreateDeal` now calls a best-effort `cancelOrphanHold(preimageHash)`
on **every failure path after the hold is drawn**:

| Failure point | What happens |
|---|---|
| `BeginTx` fails | Cancel hold → no orphan possible (no row could exist) |
| `repo.CreateDeal` fails (constraint, outage) | Cancel hold → prevents the exact orphan bug |
| `tx.Commit` fails | Cancel hold — **ambiguous**: the row may have persisted server-side. Erring toward cancellation is safer: a dead invoice on a live row is swept to `refunded` by the hold-expiry sweep; an orphaned hold strands a paying client for 30 days. |

`cancelOrphanHold` uses the LNbits **admin key** to call `POST /api/v1/payments/cancel`
with the `preimage_hash` (the `payment_hash` LNbits locked the invoice to). LNbits
tears down the HTLC, returning sats to the payer if they already paid, or making the
invoice unpayable if they haven't. A refused cancel (already settled/expired) is
logged, not returned — the caller's original error surfaces to the freelancer, who
can retry `CreateDeal` immediately with a fresh hold invoice.

The hold-expiry sweep (every 6 h) remains the ultimate backstop: it reconciles any
`UNPAID/EXPIRED/CANCELLED` holds to `refunded`.

## Paid ⇒ Locked Detection — Autodetect, Backend Chooses

Whether LNbits reports a *held-but-unsettled* invoice as `paid` depends on its
funding backend:

- **CLN-backed LNbits** reports `paid=true` while held → deal locks the moment
  the client pays (via webhook or poll).
- **LND-backed LNbits** keeps `paid=false` until settled → the deal stays
  `awaiting_payment`, and the **settle on approve** is what atomically proves
  the funds were held.

Hence the `awaiting_payment → work_submitted` edge (freelancer can submit
before confirmed payment without deadlocking), and all locking logic is gated
on LNbits actually reporting `paid`. No backend flags or hacks.

## Paths That Move Money

### Webhook (`POST /webhooks/lnbits`)
LNbits pings when an invoice is paid. The handler verifies the HMAC signature
(`LNbits-Signature`, ±5 min replay window, skipped if `LNBITS_WEBHOOK_SECRET`
is empty), then the service **re-queries LNbits** (the webhook body is never
trusted) and transitions `awaiting_payment → locked`. Idempotent for
already-locked deals.

### Poll (`GET /deals/:dealID/payment`, freelancer)
Same reconciliation via the shared `refreshPaymentStatus`, so locking works
even without webhooks.

### Approve (`POST /deals/:dealID/approve`, client-by-email)
1. `SettleHold(preimage)` — reveals the preimage, funds land in Ganji's
   LNbits wallet;
2. `PayInvoice(payee_invoice)` — forwards to the freelancer;
3. Only then `status = released` (+ `verified_at NOW()`).

Idempotent: a settle refused because it's *already settled* is treated as
success (verified via `CheckPayment`), so a retry after a crash between the
legs just pays out without double-settling. Payout is **never** attempted
unless LNbits confirms the escrow is settled.

### Dispute (`POST /deals/:dealID/dispute`, client-by-email)
**Moves no money.** The client must provide a written `reason` (trimmed, ≤ 2000 chars);
the deal enters `disputed` with `dispute_reason` + `disputed_at` and the
hold **stays held** on the network, frozen pending arbitration. Only an
arbiter resolves it to `released` (settle + payout) or `refunded` (cancel
hold). A client who changes their mind can still approve from `disputed`.
This is the safeguard against pay → take the work → cancel: the funds can't
be clawed back by the client alone.

### Arbitration (`GET /disputes`, `POST /disputes/:dealID/resolve`, operator-only)
- `release` → same two network legs as approve (`settleAndPayEscrow`): settle
  the hold, pay the freelancer, `released`. Anchors the CV like an approve.
- `refund` → `CancelHold(preimage_hash)`, sats return to the client,
  `refunded`. Idempotent: if the hold is already gone (retry, or already
  cancelled/expired on the network) LNbits refuses the cancel, and we
  confirm via `CheckPayment` that the funds are no longer committed
  (`UNPAID`/`EXPIRED`/`CANCELLED`) before recording the refund. A hold still
  committed that we cannot cancel stays `disputed` with an error.
- The deal must actually be `disputed` (else `ErrInvalidTransition`), and
  `resolved_by` + `resolved_at` are written **only after** the network leg
  succeeds — the DB never claims a money move the network did not make.

### Payee-Invoice Rotation (`PATCH /deals/:dealID/payee-invoice`, freelancer)
Swaps the payout destination while the deal is open. Unsticks a release where
the original invoice expired mid-deal. Frozen after `released`/`refunded`.

### Hold-Expiry Sweep (background, `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS`, default 6 h)
Reconciles stale open deals with LNbits. If LNbits reports
`UNPAID`/`EXPIRED`/`CANCELLED` — the network already returned any funds —
the deal becomes `refunded`. Holds still held/settled are left alone. This
is the backstop for an expired-but-never-refunded hold sitting in
`awaiting_payment` forever. Now also covers `locked`, `work_submitted`,
`reviewing`, and `disputed` states.