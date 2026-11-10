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
│   │   └── page.tsx        # Create deal form
│   │   └── [dealId]/
│   │       └── page.tsx    # Deal detail + timeline + actions
│   └── public/
│       └── [shareToken]/
│           └── page.tsx    # Client-facing payment page
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
2. **Login** → `POST /auth/login` → JWT access + refresh tokens stored in localStorage
3. **AuthContext** wraps the app provides `tokens` (access/refresh) and `user` (profile from `GET /auth/me`)
4. **Token persistence** — access token in localStorage, refresh used silently when access expires
5. **API client** — all protected endpoints require `Authorization: Bearer <token>` header; two public endpoints (`GET /public/deals/:shareToken`, `POST /public/deals/:shareToken/approve`) are unauthenticated

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

Typed wrapper around `fetch` with two export styles:

```typescript
// Functional API
api.listDeals(token)
api.createDeal(data, token)          // data may include milestones[]
api.getDeal(id, token)
api.submitWork(id, token)
api.approveDeal(id, token)
api.disputeDeal(id, reason, token)
api.updatePayeeInvoice(id, data, token)
api.rotateShareLink(id, token)
api.getPublicDeal(shareToken)
api.getPublicMilestones(shareToken)
api.publicApproveDeal(shareToken)
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
api.reconcileDeal(id, action, payoutCheckingId, token)  // POST /deals/:id/reconcile
```

All methods return `Promise<T>` and throw `ApiError` on non-2xx responses.

### Auth Methods

- `login(credentials)` → `POST /auth/login`
- `signup(data)` → `POST /auth/signup`
- `logout()` → client-side only (revokes refresh token)
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
- `publicApproveDeal(shareToken)` → `POST /public/deals/:shareToken/approve` (no auth)
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
- `reconcileDeal(id, action, payoutCheckingId, token)` → `POST /deals/:id/reconcile`

### Types (selected)

- `User` — `id`, `email`, `display_name`, `slug`, `bitcoin_address`, `trust_score`, `is_operator`, `created_at`
- `DealStatus` — `"awaiting_payment" | "locked" | "work_submitted" | "reviewing" | "released" | "disputed" | "refunded"`
- `MilestoneStatus` — `"pending" | "awaiting_payment" | "funded" | "in_progress" | "work_submitted" | "completed" | "released" | "exited" | "rejected" | "refunded" | "disputed"` (`released` = paid, `completed` = payout pending, `disputed`/`rejected` = hold stays in review)
- `MilestoneView` — `id`, `title`, `amount_sats`, `order`, `status`, `invoice?`, `completed_at`, `created_at`
- `DealView` — full deal with all fields except `preimage`, `payee_invoice`, `verified_at` (redacted); in milestone mode `invoice`/`checking_id`/`status` mirror the active milestone — use `listMilestones` for per-milestone truth
- `PublicDeal` — limited view: `title`, `amount_sats`, `source_platform`, `invoice`, `status`, `dispute_reason`, `created_at`, `milestones?`
- `CreateDealRequest.milestones?` — `{title, amount_sats}[]`; when present, `amount_sats` on the deal is derived as the sum
- `CVProfile` — `display_name`, `slug`, `trust_score`, `entries: CVEntry[]`
- `CVEntry` — `id`, `deal_title`, `amount_sats`, `source_platform`, `artifact_kind`, `hash`, `algorithm`, `verified_at`, `created_at`, OTS fields
- `VerifyResult` — hash comparison + OTS verification results

## Component Library

| Component | Purpose | Key Props |
|---|---|---|
| `Button` | Primary/ghost/outline/danger buttons, sm/md/lg sizes, loading state | `variant`, `size`, `isLoading` |
| `Input` | Form input with label, error state, focus gold glow | `label`, `error`, type |
| `FormBanner` | Error/success toast banner | `message`, `type` |
| `AuthShell` | Centered auth card with vault-styling | `eyebrow`, `title`, `subtitle`, `children`, `footer` |
| `DealCard` | Dashboard deal row with status badge, amount, client, share link | `deal: DealView`, `onClick?` |
| `format` | Currency (sats → ₿/k sats), relative time | — |

## Data Fetching

- All API calls go through `api` client from `lib/api/api-client.ts`
- Credentials (cookies) sent via `credentials: "include"` for sessions
- Bearer tokens passed via `Authorization: Bearer <token>` header
- Error handling unified — `ApiError` with `status` and `data` properties
- Loading states — each page manages its own `isLoading` flag

## Error Handling

- `ApiError` class — wraps fetch errors with `message`, `status`, and `data`
- All endpoints return `401` (unauth) or `403` (forbidden) for auth issues
- `404` for not-found deals, artifacts, verifications
- `500` for unexpected server errors
- UI shows vault-styled error banners inline

## State Machine Integration

- Deal status transitions mirrored from backend `DEAL_LIFECYCLE.md`
- Milestone status transitions mirrored from the same doc's milestone table (`released` = paid; `completed` = payout pending, never paid; `disputed`/`rejected` = hold stays in review)
- UI disables actions invalid per state (e.g., freelancer cannot set `locked`/`released`/`disputed`/`refunded` status directly)
- Freelancer-driven transitions: `awaiting_payment → work_submitted`, `locked → work_submitted`, `work_submitted → reviewing`
- Backend-only money states (`locked`, `released`, `disputed`, `refunded`) set by payment detection, approve, dispute, and arbitration; milestone `released` is set only by the canonical payout path and can never be set without escrow ("fund it first")