<!-- BEGIN:nextjs-agent-rules -->
# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.

## Component Patterns

### Button (`components/ui/button.tsx`)
- Variants: `primary`, `ghost`, `outline`, `danger`
- Sizes: `sm`, `md`, `lg`
- `isLoading` state — disables button and shows spinner
- Class conventions: `btn-primary`, `btn-ghost`, `btn-outline`, `btn-danger` mapped to Tailwind classes

### Input (`components/ui/input.tsx`)
- Label + input with gold focus glow
- Error state — red border + FormBanner above
- `type` supports: text, password, email
- CSS class: `input-input` for the input, `input-label` for label

### FormBanner (`components/ui/form-banner.tsx`)
- `type="success"` — green background, checkmark
- `type="error"` — red background, exclamation
- Auto-dismiss after 5 seconds (optional)
- Consistent padding and rounded corners matching vault theme

### AuthShell (`components/auth/auth-shell.tsx`)
- Centered card with vault-seam background
- Gold GNJ logo at top
- Consistent width constraints (max-w-md, max-w-lg)
- Footer slot for extra actions (terms, links)

### DealCard (`components/deals/deal-card.tsx`)
- Row in dashboard deal list
- Status badge with color coding per state
- Amount display with `formatCurrency` utility
- Share link copy action
- Click opens deal detail page

### format (`lib/utils/format.ts`)
- `formatCurrency(sats)` — converts sats to human readable (e.g., `50000` → `0.05 BTC` or `50,000 sats`)
- `relativeTime(date)` — formatted like "2 days ago", "Jan 15, 2024"
- Both use CSS variables for gold/ink coloring

## API Client Patterns (`lib/api/api-client.ts`)

All methods follow consistent pattern:

```typescript
// Basic call
api.listDeals(token)
api.createDeal(data, token)

// With body
api.submitWork(dealId, token)
api.approveDeal(dealId, token)
api.disputeDeal(dealId, reason, token)
api.updatePayeeInvoice(dealId, data, token)

// Public (no auth)
api.getPublicDeal(shareToken)
api.getPublicMilestones(shareToken)
api.publicApproveDeal(shareToken)
api.publicAcceptNextMilestone(shareToken, milestoneId)
api.publicRejectMilestone(shareToken, milestoneId, reason)
api.publicExitDeal(shareToken)

// Milestones (authed)
api.createMilestone(dealId, title, amountSats, order, token)
api.listMilestones(dealId, token)
api.updateMilestone(dealId, milestoneId, status, completedAt, token)
api.exitDeal(dealId, token)

// Auth methods
api.getMe(token)

// CV methods
api.getCVProfile(slug)
api.verifyEntry(slug, entryId)

// Operator methods (authed, operator-only)
api.listDisputes(token)
api.resolveDispute(dealId, resolution, token)
api.reconcileDeal(dealId, action, payoutCheckingId, token)
```

Error handling — all methods throw `ApiError` on non-2xx:
```typescript
try {
  const res = await api.listDeals(token)
} catch (err) {
  if (err instanceof ApiError) {
    // err.status → HTTP status
    // err.data → parsed error body
  }
}
```

## Routing Conventions (App Router)

- `app/page.tsx` — home, unauthenticated
- `app/login/page.tsx` — auth form
- `app/signup/page.tsx` — registration form
- `app/dashboard/page.tsx` — protected, requires auth token
- `app/deals/new/page.tsx` — create form, protected
- `app/deals/[dealId]/page.tsx` — detail, protected, shows actions based on status
- `app/public/deals/[shareToken]/page.tsx` — unauthenticated, client pays/approves
- `app/settings/page.tsx` — protected, profile + payee invoice + password
- `app/cv/[slug]/page.tsx` — public, Live CV profile
- `app/cv/[slug]/verify/[entryId]/page.tsx` — public, entry verification
- `app/operator/disputes/page.tsx` — operator-only, dispute queue
- `app/operator/deals/[dealId]/reconcile/page.tsx` — operator-only, reconcile payout

## State Management

- **AuthContext** — provides `tokens` (access + refresh) and `user` (profile) to entire app
- **Local state** — `useState` for form inputs, loading spinners, error messages
- **No global store** — Redux/Zustand not used; URL params drive data fetching
- **useEffect** — each page fetches data on mount, depends on `tokens` and route params

## Styling Conventions (Tailwind v4)

- All colors via CSS variables in `globals.css` under `:root`
- Dark/vault mode: `bg-vault-bg`, `bg-vault-card`, `bg-vault-raised`
- Gold accent: `text-gold-400`, `border-gold-500`, `gold-accent`
- Ink text: `text-ink-100` (lightest), `text-ink-500`, `text-ink-300`
- Status colors: `border-green-600/30`, `border-red-600/30`, `border-yellow-500/30`
- Focus rings: `focus-ring-gold` — gold outline on focus
- Responsive: mobile-first breakpoints `sm`, `md`, `lg`
- Custom `font-digital` — Orbitron, tabular-nums, wide tracking for digital/stopwatch display
- `.logo-gnj` — stopwatch logo with gold glow pulse animation
- `.vault-card` — card with gold border glow on hover
- `.hero-gradient` — ink → gold → ink gradient for hero sections

## Documentation Updates

When adding new pages or components:

1. Update `frontend/README.md` — pages table, components table, file structure
2. Update `docs/FRONTEND_REPORT.md` — ✅ Completed items, ⏳ remaining pages
3. Update `docs/FRONTEND_ARCHITECTURE.md` — routing, auth flow, state management section
4. Add/type new API client methods in `lib/api/api-client.ts`
5. Add new types in `lib/api/api-client.ts` if new request/response shapes introduced
6. Run `npm run lint` and `npm run typecheck` if available