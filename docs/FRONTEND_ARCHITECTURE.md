# Frontend Architecture

## Routing (App Router)

```
app/
├── page.tsx                  # Home (marketing landing)
├── layout.tsx               # Root layout, AuthProvider, fonts, CSS vars
├── globals.css              # Tailwind v4 @theme, design tokens, CSS variables
├── login/
│   └── page.tsx             # Email/password auth
├── signup/
│   └── page.tsx             # Registration with validation
├── dashboard/
│   └── page.tsx            # Authenticated deal list + empty state
├── deals/
│   ├── new/
│   │   └── page.tsx        # Create deal form (payout invoice generation)
│   └── [dealId]/
│       └── page.tsx        # Deal detail + timeline + actions
├── public/
│   └── deals/
│       └── [shareToken]/
│           └── page.tsx    # Client-facing fund/approve link
├── settings/
│   └── page.tsx            # Profile, payee invoice, password
├── cv/
│   └── [slug]/
│       ├── page.tsx        # Live CV profile + entries
│       └── verify/
│           └── [entryId]/page.tsx  # Entry verification + OTS proof
└── operator/
    ├── disputes/
    │   └── page.tsx        # Admin dispute arbitration queue
    └── deals/
        └── [dealId]/
            └── reconcile/
                └── page.tsx  # Operator reconcile payout
```

## Auth Flow

1. **Signup** → `POST /auth/signup` → user created, `is_operator` flag from `OPERATOR_EMAILS` at boot
2. **Login** → `POST /auth/login` → access token returned in the response body; refresh token set as an `HttpOnly` cookie scoped to `/auth`
3. **AuthContext** wraps the app and provides `tokens.access_token` plus `user`
4. **Token persistence** — access token kept only in memory; refresh is replayed from the cookie on reload and rotated silently when access expires
5. **API client** — all protected endpoints require `Authorization: Bearer <token>` header; public view/artifact/funding endpoints are share-link based, while approve/reset/milestone actions and exit additionally require a logged-in client whose email matches the deal

## State Management

- **Local state** — `useState` for form inputs, loading states, UI flags
- **Auth state** — `AuthContext` provides `tokens` and `user` to all components
- **URL routing** — Next.js App Router files determine routes; dynamic segments (`[id]`, `[slug]`, `[entryId]`) pass parameters via `useParams`
- **No global store** — Redux/Zustand not used; UI state managed locally or via context

## Design System (Tailwind v4)

- **Color palette** defined as CSS variables in `globals.css` under `:root`
- **Vault theme** — near-black matte backgrounds (`#0a0c0e`), gold accents (`#c49632`, `#d4a843`)
- **Ink text** — `#faf9f6` (light) / `#5a5852` (readable on vault)
- **Font families** — Space Grotesk (display/headings), Inter (body), IBM Plex Mono (monospace), Orbitron + Share Tech Mono (digital/stopwatch style for GNJ logo)
- **Components** — all UI components in `components/ui/` follow the vault/gold visual language
- **Responsive** — mobile-first Tailwind breakpoints (sm: ≥640px, md: ≥768px, lg: ≥1024px)

## API Client (`lib/api/api-client.ts`)

Compatibility barrel re-exporting `types.ts`, `session.ts`, and `client.ts`.
The typed API wrapper around `fetch` supports two import styles:

```typescript
// Functional API
api.listDeals(token)
api.createDeal(data, token)          // data may include milestones[]
api.getDeal(id, token)
api.checkPayment(id, token)          // GET /deals/:dealID/payment; freelancer accepted-hold check
api.listArtifacts(id, token)
api.uploadArtifact(id, kind, file, token)
api.downloadArtifact(id, artifactId, token)
api.deleteArtifact(id, artifactId, token)
api.submitWork(id, token)
api.approveDeal(id, token)
api.disputeDeal(id, reason, token)
api.updatePayeeInvoice(id, data, token)
api.rotateShareLink(id, token)
api.getPublicDeal(shareToken)
api.getPublicMilestones(shareToken)
api.fundPublicDeal(shareToken, { wallet_url, wallet_key, milestone_id? })  // POST /public/deals/:shareToken/fund
api.generateInvoice({ wallet_url, wallet_key, amount_sats, memo? }, token)  // POST /deals/wallet/invoice
api.publicApproveDeal(shareToken)
api.publicResetPayout(shareToken, milestoneId?)
api.publicAcceptNextMilestone(shareToken, milestoneId)
api.publicRejectMilestone(shareToken, milestoneId, reason)
api.publicExitDeal(shareToken)
api.createMilestone(id, title, amountSats, order, token)
api.listMilestones(id, token)
api.updateMilestone(id, milestoneId, status, completedAt, token)
api.exitDeal(id, token)
api.getMe(token)           // GET /auth/me
api.getCVProfile(slug)     // GET /cv/:slug
api.verifyEntry(slug, entryId)  // GET /cv/:slug/verify/:entryId
api.listDisputes(token)    // GET /disputes
api.resolveDispute(id, resolution, token)  // POST /disputes/:id/resolve
api.reconcileMilestonePayout(id, action, payoutCheckingId, token)  // POST /deals/:dealID/milestones/:milestoneID/reconcile
```

All methods return `Promise<T>` and throw `ApiError` on non-2xx responses.

### Auth Methods

- `login(credentials)` → `POST /auth/login`
- `signup(data)` → `POST /auth/signup`
- `logout()` → clears local auth state and calls `POST /auth/logout` with credentials so the refresh cookie is cleared and both refresh/access tokens are revoked
- `getMe(token)` → `GET /auth/me` — returns user profile without tokens

### Deal Methods

- `listDeals(token)` → `GET /deals`
- `createDeal(data, token)` → `POST /deals`
- `getDeal(id, token)` → `GET /deals/:id`
- `submitWork(id, token)` → `POST /deals/:id/submit`
- `approveDeal(id, token)` → `POST /deals/:id/approve`
- `disputeDeal(id, reason, token)` → `POST /deals/:id/dispute`
- `updatePayeeInvoice(id, data, token)` → `PATCH /deals/:id/payee-invoice`
- `rotateShareLink(id, token)` → `POST /deals/:id/share-link`
- `getPublicDeal(shareToken)` → `GET /public/deals/:shareToken`
- `getPublicMilestones(shareToken)` → `GET /public/deals/:shareToken/milestones`
- `fundPublicDeal(shareToken, data)` → `POST /public/deals/:shareToken/fund` (no auth; `data` = `{wallet_url, wallet_key, milestone_id?}`, wallet key is transient)
- `generateInvoice(data, token)` → `POST /deals/wallet/invoice` (protected; `data` = `{wallet_url, wallet_key, amount_sats, memo?}`, used for the freelancer payout invoice)
- `publicApproveDeal(shareToken)` → `POST /public/deals/:shareToken/approve` (no auth)
- `publicResetPayout(shareToken, milestoneId?)` → `POST /public/deals/:shareToken/payout/reset` (no auth)
- `publicAcceptNextMilestone(shareToken, milestoneId)` → `POST /public/deals/:shareToken/milestones/:milestoneID/accept-next` (no auth)
- `publicRejectMilestone(shareToken, milestoneId, reason)` → `POST /public/deals/:shareToken/milestones/:milestoneID/reject` (no auth)
- `publicExitDeal(shareToken)` → `POST /public/deals/:shareToken/exit` (no auth)
- `createMilestone(id, title, amountSats, order, token)` → `POST /deals/:id/milestones`
- `listMilestones(id, token)` → `GET /deals/:id/milestones`
- `updateMilestone(id, milestoneId, status, completedAt, token)` → `PATCH /deals/:id/milestones/:milestoneID`
- `exitDeal(id, token)` → `POST /deals/:id/exit`

### Operator Methods (authed, operator-only)

- `listDisputes(token)` → `GET /disputes`
- `resolveDispute(id, resolution, token)` → `POST /disputes/:id/resolve`
- `resolveMilestone(dealId, milestoneId, resolution, token)` → `POST /disputes/deals/:dealID/milestones/:milestoneID/resolve` (frontend wiring complete; operator resolves disputed/rejected milestone via release/refund)
- `reconcileMilestonePayout(id, action, payoutCheckingId, token)` → `POST /deals/:dealID/milestones/:milestoneID/reconcile`

### Types (selected)

- `User` — `id`, `email`, `display_name`, `slug`, `bitcoin_address`, `trust_score`, `is_operator`, `created_at`
- `DealStatus` — `"awaiting_payment" | "locked" | "work_submitted" | "reviewing" | "released" | "disputed" | "refunded"`
- `MilestoneStatus` — `"pending" | "awaiting_payment" | "funded" | "in_progress" | "work_submitted" | "completed" | "released" | "exited" | "rejected" | "refunded" | "disputed"` (`released` = paid, `completed` = payout pending, `disputed`/`rejected` = hold stays in review)
- `MilestoneView` — `id`, `title`, `amount_sats`, `order`, `status`, `invoice?`, `completed_at`, `created_at`
- `DealView` — authenticated safe view. It includes `preimage_hash` and `verified_at`, but omits the raw `preimage`, `payee_invoice`, and internal payout tracking fields. It includes `payout_reset_available`; in milestone mode `invoice`/`checking_id`/`status` mirror the active milestone, so use `listMilestones` for per-milestone truth.
- `PublicDeal` — share-link view: `title`, `amount_sats`, `source_platform`, `invoice`, `status`, `dispute_reason`, `created_at`, optional `milestones`, and optional `payout_reset_available`. It omits internal IDs, client/freelancer identity, preimage and payout destination.
- `FundDealRequest` — `{wallet_url, wallet_key, milestone_id?}`; `GenerateInvoiceRequest` — `{wallet_url, wallet_key, amount_sats, memo?}`. Wallet URLs must be http/https hosts only (no userinfo/query/fragment, no allowlist); keys are never stored or logged.
- `CreateDealRequest.milestones?` — `{title, amount_sats}[]`; when present, `amount_sats` on the deal is derived as the sum
- `CVProfile` — `display_name`, `slug`, `trust_score`, `entries: CVEntry[]`
- `CVEntry` — `id`, `deal_title`, `amount_sats`, `source_platform`, `artifact_kind`, `hash`, `algorithm`, `verified_at`, `created_at`, OTS fields
- `VerifyResult` — hash comparison + OTS verification results

## Component Library

| Component | Purpose | Key Props |
|---|---|---|
| `Button` | Primary/ghost/outline/danger buttons, sm/md/lg sizes, loading state | `variant`, `size`, `isLoading` |
| `Input` | Form input with label, error state, focus gold glow | `label`, `error`, type |
| `PasswordInput` | Password field with show/hide toggle used by login/signup | `label`, `error`, standard input attrs |
| `FormBanner` | Error/success toast banner | `message`, `type` |
| `AuthShell` | Centered auth card with vault-styling | `eyebrow`, `title`, `subtitle`, `children`, `footer` |
| `DealCard` | Dashboard deal row with status badge, amount, client, share link | `deal: DealView`, `onClick?` |
| `StatusIcon` | Inline SVG icons per deal status (`components/ui/status-icon.tsx`); no emoji status map | `status`, `className?` |
| `WalletLinkDialog` | Shared LNbits URL/key dialog for funding and payout invoice generation (`components/deals/wallet-link-dialog.tsx`) | `title`, `walletURL`, `walletKey`, `keyLabel`, `submitLabel`, callbacks |
| `format` | Currency (sats → ₿/k sats), relative time | — |

## Data Fetching

- All API calls go through `api` client from `lib/api/api-client.ts`
- Credentials (cookies) sent via `credentials: "include"` so the `ganji_refresh` cookie reaches `/auth`
- In-memory access tokens passed via `Authorization: Bearer <token>` header
- Error handling unified — `ApiError` with `status` and `data` properties
- Loading states — each page manages its own `isLoading` flag

## Error Handling

- `ApiError` class — wraps fetch errors with `message`, `status`, and `data`
- Protected endpoints can return `401` for missing/invalid/revoked authentication and `403` for an authenticated user without access; public view/funding endpoints use share links, while public money-changing actions still require matching client proof
- `404` for not-found deals, artifacts, verifications
- `500` for unexpected server errors
- UI shows vault-styled error banners inline

## State Machine Integration

- Deal status transitions mirrored from backend `DEAL_LIFECYCLE.md`
- `locked` is labelled **"Funds Held"** in the UI: it means the LND accepted hold on the escrow node Es was verified, **not** that the payment settled. Status icons are inline SVGs from `components/ui/status-icon.tsx`.
- Milestone status transitions mirrored from the same doc's milestone table (`released` = paid; `completed` = payout pending, never paid; `disputed`/`rejected` = hold stays in review)
- UI disables actions invalid per state (e.g., freelancer cannot set `locked`/`released`/`disputed`/`refunded` status directly)
- Freelancer-driven transitions: `locked → work_submitted`, `work_submitted → reviewing`. Work submission requires a `locked` deal; `awaiting_payment → work_submitted` is invalid.
- Deal detail refresh controls are stage- and role-specific: the freelancer gets **Check Payment** while `awaiting_payment`, then **Check Escrow Status** while `locked`; after submission, **Check Freelancer Status** refreshes the recorded payout/deal state. The client gets **Check Escrow Status** while `awaiting_payment` and **Check Freelancer Status** after submission. These refresh controls do not resend payments.
- Backend-only money states (`locked`, `released`, `disputed`, `refunded`) set by accepted-hold verification, approve, dispute, and arbitration; milestone `released` is set only by the canonical payout path and can never be set without escrow ("fund it first")
- Approve/release reveals R and pays the freelancer directly; rejecting after `work_submitted` reveals no R and leaves the hold untouched; arbiter reveals R to pay or cancels/waits for expiry to refund (refund never uses R).
