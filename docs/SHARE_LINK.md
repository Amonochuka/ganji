# Shareable Payment Link (share_token)

This is how the payment-confirmation loop closes for the client. The goal:
the freelancer sends a link, the client opens it **without logging in**, sees
the invoice (and message at-a-glance), pays in their wallet, the page reflects
the money landing on the network — and they can approve delivery from the same
link, no account needed.

## Design — Why a Separate Token, Not the Deal UUID

The public share-link endpoints (`GET /public/deals/:shareToken`,
`GET /public/deals/:shareToken/milestones`,
`POST /public/deals/:shareToken/fund`,
`POST /public/deals/:shareToken/approve`,
`POST /public/deals/:shareToken/payout/reset`,
`POST /public/deals/:shareToken/milestones/:milestoneID/accept-next`,
`POST /public/deals/:shareToken/milestones/:milestoneID/reject`,
`POST /public/deals/:shareToken/exit`) are the only unauthenticated deal
endpoints. All are authorized by the share token alone — no JWT, no email match.
The share token is a **deliberately separate secret from the deal's DB UUID**:

- **Revocable.** `POST /deals/:dealID/share-link` (freelancer) mints a fresh
  token; the old link stops resolving immediately. A UUID-based link can't be
  retired without hiding the whole deal.
- **No internal-ID leak.** The public response never contains the deal UUID,
  so a shared link can't be used to correlate/guess other rows.
- **Unguessable but not secret-critical.** 32 random bytes
  (`crypto/rand`) base64url-encoded (~256 bits). Anyone with the link can see
  the title/amount/invoice and pay — that is the point — but they cannot read
  any sensitive field.

## Implementation

- **Schema** (`000003_create_deals_table.up.sql`):
  `share_token TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex')`
  with `UNIQUE` (which also serves as the lookup index). The `NOT NULL DEFAULT`
  backfilled every existing deal when the column was folded into the table, so
  no backfill migration is needed. New tokens are generated in
  `deals.Service.CreateDeal` too (service-side generation, DB default as a
  safety net).
- **Model**: `Deal.ShareToken` (`json:"share_token"` — visible to authed
  users so the freelancer can build the link) and a separate `PublicDeal`
   view that carries only `title`, `amount_sats`, `source_platform`, `invoice`,
   `status`, `created_at`, `dispute_reason`, and the optional `milestones[]` /
   `payout_reset_available` fields.
- **Lookup + refresh**: `Service.GetPublicDeal` resolves the token via
  `repo.GetDealByShareToken`, and before answering **proactively re-checks the
  hold with LNbits plus the LND accepted-hold checker on `polar-n3-Es`**
  (`refreshPaymentStatus` / `refreshMilestonePayment`). It locks only after a
  matching `payment_hash`/amount with `State == ACCEPTED`, `settled == false`,
  and `settle_date == 0`; LNbits `paid=true` alone does not lock. `locked` is
  shown as **Funds Held**. The client opening the link has no account and could
  never call the freelancer-only poll endpoint, so this self-refresh is what
  makes the page reflect reality even if the webhook was delayed/lost/never
  configured. Checker/provider errors are swallowed — the visitor gets the last
  known status rather than a 500.
- **Public fund**: `Service.FundDeal` accepts `{wallet_url, wallet_key, milestone_id?}`. Bob's linked LNbits wallet pays the hold invoice, then the backend requires accepted-hold proof on Es before `locked` (**Funds Held**) or milestone `funded → in_progress`; optional `milestone_id` selects which milestone hold to verify. `wallet_url` is `http`/`https` host only (no userinfo/query/fragment, no allowlist) and `wallet_key` is transient — never stored or logged. A missing checker returns `ErrVerificationUnavailable` (`503`).
- **Rotation**: `Service.RotateShareLink` (freelancer owner only) generates a
  new token and persists it via `repo.UpdateShareToken`. Mirrors the payee-
  invoice rule: frozen after `released`/`refunded` (nothing left to share).
- **Public approve**: `Service.PublicApproveDeal` resolves the token, refreshes
  the accepted hold on Es (same as the view), and — only from `locked`
  (**Funds Held**)/`work_submitted` — runs the same `releaseEscrow` two-leg
  settle + payout as the authed approve: reveal `R` and pay Fre directly (no
  Ganji settlement wallet). The token itself is the authorization, so no email
  match happens on this path; `disputed → released` deliberately stays
  authed-only (arbitration is email-gated).
- **Milestone mode**: `Service.PublicAcceptNextMilestone` / `PublicRejectMilestone`
  / `PublicExitDeal` / `GetPublicDeal(milestones)` follow the same token-only
  authorization. Accept-next releases via the canonical per-milestone path;
  reject/exit post-submission freeze the hold for operator resolution — no
  public money move outside the escrow rules.
- **Route wiring**: `GET /public/deals/:shareToken`,
  `POST /public/deals/:shareToken/fund`, and
  `POST /public/deals/:shareToken/approve` are mounted on the root `/public`
  group (unauthenticated, IP rate-limited), while
  `POST /deals/:dealID/share-link` and the payout-invoice helper
  `POST /deals/wallet/invoice` live in the protected group.

## Endpoints

| Method | Path | Auth | Effect |
|---|---|---|---|
| `GET` | `/public/deals/:shareToken` | none | safe public deal view; refreshes accepted hold on Es (locks only then, **Funds Held**); includes `milestones[]` in milestone mode |
| `GET` | `/public/deals/:shareToken/milestones` | none (share token) | list milestones for the share link |
| `POST` | `/public/deals/:shareToken/fund` | none (share token) | pay hold from linked wallet; accepted-hold proof on Es locks/funds (optional `milestone_id`); `503` if checker unavailable |
| `POST` | `/public/deals/:shareToken/approve` | none (share token) | reveal `R` + pay Fre directly — releases funds (milestone mode: releases active milestone, funds next) |
| `POST` | `/public/deals/:shareToken/payout/reset` | none (share token) | clear a definitively unconfirmed payout-attempt marker; no money move |
| `POST` | `/public/deals/:shareToken/milestones/:milestoneID/accept-next` | none (share token) | release that escrow-backed milestone (canonical path: reveal `R` + pay Fre), fund next |
| `POST` | `/public/deals/:shareToken/milestones/:milestoneID/reject` | none (share token) | milestone → `disputed`; no `R` reveal, no HTLC change, no auto-refund pending operator resolution |
| `POST` | `/public/deals/:shareToken/exit` | none (share token) | symmetric exit; pre-submission refunds, post-submission → review |
| `POST` | `/deals/:dealID/share-link` | freelancer | rotate token → revoke old link, return new one |
| `POST` | `/deals/wallet/invoice` | authenticated | create a payout invoice from a transient linked wallet URL/key |

## Threat Model / Trade-offs

- Link is bearer: whoever has it can see the deal, fund the hold from a linked
  wallet (transient key, never stored/logged), **and approve it — revealing `R`
  and paying Fre directly** (in milestone mode, whoever has it can also
  accept/reject the active milestone or exit — reject/exit post-submission
  freeze for review rather than paying or changing the HTLC). Funding and
  approving are both the point, and the token is unguessable. It is effectively
  a bearer payment authorization — treat it like one, and rotate to revoke a
  leaked link before it's abused.
- Dispute stays payment-gated: even with the link, a stranger can't dispute
  (locked to `client_email` matching) and can't see the preimage, payee
  invoice, or the freelancer's payout details. Authed approve also remains
  available (including from `disputed`, which public approve rejects).
- After `released`/`refunded` the public page still renders the (terminal)
  status but the invoice is spent; rotation is disabled because there is
  nothing to re-share.