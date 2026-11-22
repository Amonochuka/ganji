# Money Flow: Network-as-Escrow (Hold Invoices)

LNbits hold invoices make the Lightning Network the escrow: an incoming payment
sits **held** under `payment_hash` until someone reveals the preimage `R`
(settle → to the freelancer) or tears it down (cancel → back to the client).

Escrow node: `polar-n3-Es` (Alice1/Es). Client: Bob/`polar-n3-Cl`. Freelancer:
`polar-n3-Fre`. `polar-n3-Pay`, if used, is a route-only hop Es → Fre — not a
second Ganji wallet. The Alice2/relay workflow is reverted and is not current.

## LNbits Endpoints Ganji Uses

| Action | Call | Auth |
|---|---|---|
| Create hold invoice (`out:false, amount, memo, payment_hash, webhook`) | `POST /api/v1/payments` | invoice key |
| Check status | `GET /api/v1/payments/{checking_id}` | invoice key |
| Settle (release escrow) — reveal stored preimage | `POST /api/v1/payments/settle` | **admin key** |
| Cancel (refund) — `{payment_hash}` | `POST /api/v1/payments/cancel` | **admin key** |
| Pay out — `out:true, bolt11` | `POST /api/v1/payments` | **admin key** |

Not an LNbits endpoint: `internal/lnd.Client.IsAcceptedHold` queries the escrow
node's LND REST API (`GET /v1/invoice/{payment_hash}` on Es, TLS cert +
macaroon). It is the accepted-hold proof used before any deal is marked
`locked` (**Funds Held**).

`Deal` stores both sides of the secret: `preimage` (`R`, raw hex — revealed on
settle) and `preimage_hash = sha256(R)` (what LNbits locked the invoice to).
The preimage and the freelancer's `payee_invoice` are persisted but never
serialized into API responses: **every** authenticated deal endpoint serves the
redacted `DealView` (drops `preimage`, `payee_invoice`, and the payout-tracking
fields), and the public share link serves `PublicDeal`. The raw `*Deal` stays
internal to the service layer.

## CreateDeal (`service.go`)

1. Validate + trim input; require a valid `client_email` and a `payee_invoice`
   (the freelancer's payout destination).
 2. Generate a 32-byte random **preimage `R`**; store `preimage` + `preimage_hash`.
 3. Generate the shareable-link **`share_token`** (see SHARE_LINK.md).
 4. `lnbits.CreateHoldInvoice` on `polar-n3-Es` → hold invoice + `checking_id`;
    `memo = title` (hold expiry: `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS`, default 30 days).
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

## Accepted Hold ⇒ Locked — LND Verification on Es

LNbits `paid=true` is **not** proof that funds are locked. `CheckPayment` and the
webhook re-query LNbits, then require `internal/lnd.Client.IsAcceptedHold`
against `polar-n3-Es`: the invoice must match the deal's `payment_hash` and
amount, report `State == ACCEPTED`, `settled == false`, and `settle_date == 0`.
Only then does `awaiting_payment → locked` happen; `locked` is displayed as
**Funds Held**. A missing checker returns `ErrVerificationUnavailable` — public
funding (`POST /public/deals/:shareToken/fund`) maps this to HTTP `503`.

Work submission requires `locked`; `awaiting_payment → work_submitted` is not a
legal transition. No CLN/LND autodetect heuristic is used.

## Client Wallet Funding (`POST /public/deals/:shareToken/fund`)

Bob pays the hold invoice from his linked LNbits wallet (`polar-n3-Cl`). Body:
`{wallet_url, wallet_key, milestone_id?}`. `wallet_url` must be `http`/`https`
with a host; userinfo, query strings, and fragments are rejected, and there is
no allowlist. `wallet_key` is transient — never stored or logged. `milestone_id`
is optional; accepted-hold proof applies to milestone funding too, moving the
milestone `awaiting_payment → funded → in_progress` (and the deal to `locked`)
only after Es verification. Without it, the first unfunded milestone or the
legacy deal-level hold is funded.

### Deferred: manual-pay mode

The linked-wallet flow above is the current implementation. A later manual-pay
mode will show the hold `bolt11` on the public deal page and let the client pay
it in any Lightning wallet, including exchange wallets that do not expose LNbits
API keys. The client would press **"I paid — verify hold"**, and Ganji would run
the same Es `IsAcceptedHold` check before locking; no admin key would be sent to
Ganji. This is not built yet and must not weaken the accepted-hold requirement.

## Payout Invoice Helper (`POST /deals/wallet/invoice`)

Protected. Body: `{wallet_url, wallet_key, amount_sats, memo?}` (`memo` defaults
to `Ganji payout`). Same transient-key and URL rules as public funding. Creates
a regular invoice through the linked wallet; it does not move escrow funds.

## Paths That Move Money

### Webhook (`POST /webhooks/lnbits`)
LNbits pings when an invoice is paid. The handler verifies the HMAC signature
(`LNbits-Signature`, ±5 min replay window, skipped if `LNBITS_WEBHOOK_SECRET`
is empty), then the service **re-queries LNbits** (the webhook body is never
trusted) and transitions `awaiting_payment → locked` only after the Es
accepted-hold check succeeds. `paid=true` alone is ignored. Idempotent for
already-locked deals.

### Poll (`GET /deals/:dealID/payment`, freelancer)
Same LNbits + Es accepted-hold reconciliation via the shared
`refreshPaymentStatus`, so locking works even without webhooks.

### Approve (`POST /deals/:dealID/approve`, client-by-email)
1. `SettleHold(R)` on Es — reveals the preimage, completing the held payment;
2. `PayInvoice(payee_invoice)` — pays `polar-n3-Fre` directly (no Ganji
   settlement wallet; `polar-n3-Pay`, if used, is route-only Es → Fre);
3. Only then `status = released` (+ `verified_at NOW()`).

Idempotent: a settle refused because it's *already settled* is treated as
success (verified via `CheckPayment`), so a retry after a crash between the
legs just pays out without double-settling. Payout is **never** attempted
unless LNbits confirms the escrow is settled on Es.

### Dispute (`POST /deals/:dealID/dispute`, client-by-email)
**Moves no money.** The client must provide a written `reason` (trimmed, ≤ 2000 chars);
the deal enters `disputed` with `dispute_reason` + `disputed_at` and the
hold **stays held** on the network, frozen pending arbitration. Only an
arbiter resolves it to `released` (reveal `R` + pay Fre) or `refunded` (cancel
hold or expiry; no `R`). Rejecting work after submission does **not** reveal
`R` and does **not** settle, cancel, or fail the HTLC. A client who changes
their mind can still approve from `disputed`. This is the safeguard against
pay → take the work → cancel: the funds can't be clawed back by the client
alone.

### Arbitration (`GET /disputes`, `POST /disputes/:dealID/resolve`, operator-only)
- `release` → `releaseEscrow`: the arbiter reveals `R` on Es and pays Fre
  directly, `released`. Anchors the CV like an approve.
- `refund` → `CancelHold(preimage_hash)`, sats return to Bob, `refunded` (or an
  expired hold is swept back to Bob). `R` is never used for a refund.
  Idempotent: if the hold is already gone (retry, or already cancelled/expired
  on the network) LNbits refuses the cancel, and we confirm via `CheckPayment`
  that the funds are no longer committed (`UNPAID`/`EXPIRED`/`CANCELLED`)
  before recording the refund. A hold still committed that we cannot cancel
  stays `disputed` with an error.
- The deal must actually be `disputed` (else `ErrInvalidTransition`), and
  `resolved_by` + `resolved_at` are written **only after** the network leg
  succeeds — the DB never claims a money move the network did not make.

### Payee-Invoice Rotation (`PATCH /deals/:dealID/payee-invoice`, freelancer)
Swaps the payout destination while the deal is open. Unsticks a release where
the original invoice expired mid-deal. Frozen after `released`/`refunded`.

### Milestone Escrow (sequential per-milestone holds)

In milestone mode there is **no single upfront deal hold**. Each milestone is its own escrow leg:

1. `CreateDealWithMilestones` — creates the deal + all milestones (`pending`), commits, then funds M1 only (`FundMilestone`: fresh preimage + `CreateHoldInvoice` for that milestone's amount). `deal.Invoice`/`deal.CheckingID` mirror M1 for compatibility. Later milestones stay `pending`/unfunded.
 2. Client pays the M1 invoice — directly or via `POST /public/deals/:shareToken/fund` with optional `milestone_id` → accepted-hold verification on Es moves `awaiting_payment → funded → in_progress` (LNbits `paid=true` alone is not enough for public funding). `refreshMilestonePayment` runs on `SubmitWork`/public views, not the webhook. Milestone holds have per-milestone escrow/payout tracking fields (migration `000008`); the webhook reconciles deal-level holds only.
 3. Freelancer submits the active milestone (`funded`/`in_progress` → `work_submitted`) with at least one deal-level artifact, same as the legacy flow; `awaiting_payment` blocks submit.
 4. Client approves (`POST /deals/:dealID/approve` or public accept-next; both refresh the accepted hold on Es first) → `releaseMilestoneViaCanonicalPath` → `ReleaseMilestone` (see below) → `released` (paid), then funds the next milestone. Final milestone released → deal `released`.
 5. **Canonical payout (`ReleaseMilestone`)** — the only path to `released`/paid: lock + durable milestone payout-attempt commit, settle hold on Es, pay Fre directly, then re-lock/record `payout_checking_id` + `released`. Policy: confirmed paid → release without re-pay; confirmed failed after settled hold handled conservatively; ambiguous/in-flight/unknown → in-flight error. Hung payouts are reconciled by operator milestone reconcile (`confirm_payout`/`reset_payout`). **Escrow mandatory:** no hold → "fund it first" error; never marked paid.
 6. **Reject → independent review:** public reject (only from `work_submitted`/`completed`) does **not** reveal `R` and does **not** settle, cancel, or fail the HTLC; no auto-refund. Milestone → `disputed`, hold stays. Operator `POST /disputes/deals/:dealID/milestones/:milestoneID/resolve`: `release` → canonical `ReleaseMilestone` (arbiter reveals `R`, pays Fre) + advance sequence; `refund` → `CancelHold` + `refunded` (idempotent, no `R`; does not auto-fund next).
 7. **Exit (symmetric):** pre-submission → cancel/refund the open hold(s) (or let expiry return them), not paid to Fre; post-submission → no money move, milestone → `disputed` (hold stays) for review. Refund never uses `R`. Legacy single-hold deals follow the same rule.

`completed` (if used) = approved/payout-pending, never paid; only `released` means money moved (terminal).

### Hold-Expiry Sweep (background, `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS`, default 6 h)
Reconciles stale open deals with LNbits. If LNbits reports
`UNPAID`/`EXPIRED`/`CANCELLED` — the network already returned any funds —
the deal becomes `refunded`. Holds still held/settled are left alone. This
is the backstop for an expired-but-never-refunded hold sitting in
`awaiting_payment` forever. Now also covers `locked`, `work_submitted`,
`reviewing`, and `disputed` states.

The sweep reconciles **deal-level** holds only (`deal.CheckingID`). In
milestone mode it checks the mirrored M1 `checking_id`; later per-milestone
holds have no sweep coverage yet (stale M2+ holds need explicit handling — see `MILESTONE_REDESIGN_NOTES.md` Outstanding).