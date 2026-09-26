# Shareable Payment Link (share_token)

This is how the payment-confirmation loop closes for the client. The goal:
the freelancer sends a link, the client opens it **without logging in**, sees
the invoice (and message at-a-glance), pays in their wallet, and the page
reflects the money landing on the network.

## Design — Why a Separate Token, Not the Deal UUID

`GET /public/deals/:shareToken` is the only unauthenticated deal endpoint.
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
  `status`, `created_at`.
- **Lookup + refresh**: `Service.GetPublicDeal` resolves the token via
  `repo.GetDealByShareToken`, and before answering **proactively re-checks the
  hold with LNbits** (`refreshPaymentStatus`): if the client has already paid,
  the deal transitions to `locked` right there. The client opening the link has
  no account and could never call the freelancer-only poll endpoint, so this
  self-refresh is what makes the page reflect reality even if the webhook was
  delayed/lost/never configured. An LNbits error is swallowed — the visitor
  gets the last known status rather than a 500.
- **Rotation**: `Service.RotateShareLink` (freelancer owner only) generates a
  new token and persists it via `repo.UpdateShareToken`. Mirrors the payee-
  invoice rule: frozen after `released`/`refunded` (nothing left to share).
- **Route wiring**: `GET /public/deals/:shareToken` is mounted on the **root
  router** (unauthenticated), while `POST /deals/:dealID/share-link` lives in
  the protected `/deals` group.

## Endpoints

| Method | Path | Auth | Effect |
|---|---|---|---|
| `GET` | `/public/deals/:shareToken` | none | safe public deal view; refreshes hold status (locks if paid) |
| `POST` | `/deals/:dealID/share-link` | freelancer | rotate token → revoke old link, return new one |

## Threat Model / Trade-offs

- Link is bearer: whoever has it can see the deal and pay the invoice. That is
  the feature, and the token is unguessable. It is **not**
  sensitive-credential-grade — treat it like a "magic link".
- Payment-gating is unchanged: even with the link, a stranger can't approve or
  dispute (both are locked to `client_email` matching) and can't see the
  preimage, payee invoice, or the freelancer's payout details.
- After `released`/`refunded` the public page still renders the (terminal)
  status but the invoice is spent; rotation is disabled because there is
  nothing to re-share.