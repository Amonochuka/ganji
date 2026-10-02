# Ganji Frontend — Implementation Report

**Last updated:** 2026-10-01  
**Branch:** main  
**Commit:** 6095ff6

---

## ✅ Completed Pages

| Page | Route | Description | Backend Endpoints |
|------|-------|-------------|-------------------|
| **Home** | `/` | Marketing landing with GNJ stopwatch-logo, hero, feature cards | — |
| **Login** | `/login` | Email/password auth, redirects to dashboard | `POST /auth/login` |
| **Signup** | `/signup` | Registration with validation, redirects to dashboard | `POST /auth/signup` |
| **Dashboard** | `/dashboard` | Authenticated deal list with status cards, empty state, create CTA | `GET /deals` |
| **Create Deal** | `/deals/new` | Form: title, amount (sats), platform dropdown, client email, payee invoice (lnbc) | `POST /deals` |
| **Deal Detail** | `/deals/[id]` | Full deal view, timeline, actions (submit/dispute/rotate), share link, verification status | `GET /deals/:id`, `POST /submit`, `POST /dispute`, `POST /share-link`, `POST /approve` |
| **Public Deal** | `/public/deals/[shareToken]` | Client-facing view: invoice to pay, approve button (requires auth fix), dispute status | `GET /public/deals/:shareToken` |

---

## 🔧 Core Components

| Component | Location | Purpose |
|-----------|----------|---------|
| `Button` | `components/ui/button.tsx` | Primary/ghost/outline/danger variants, sm/md/lg sizes, loading state |
| `Input` | `components/ui/input.tsx` | Form input with label, error state, focus gold glow |
| `FormBanner` | `components/ui/form-banner.tsx` | Error/success toast banner |
| `AuthShell` | `components/auth/auth-shell.tsx` | Centered auth card with vault-seam styling |
| `DealCard` | `components/deals/deal-card.tsx` | Dashboard deal row with status badge, amount, client, copy share link |
| `format.ts` | `lib/utils/format.ts` | Currency (sats → ₿/k sats), relative time |

---

## 🔌 API Client (`lib/api/api-client.ts`)

**Auth:**
- `login(credentials)` → `POST /auth/login`
- `signup(data)` → `POST /auth/signup` (fixed from `/register`)
- `logout()` → client-side only

**Deals:**
- `listDeals(token)` → `GET /deals`
- `createDeal(data, token)` → `POST /deals`
- `getDeal(id, token)` → `GET /deals/:id`
- `submitWork(id, token)` → `POST /deals/:id/submit`
- `approveDeal(id, token)` → `POST /deals/:id/approve`
- `disputeDeal(id, reason, token)` → `POST /deals/:id/dispute`
- `updatePayeeInvoice(id, data, token)` → `PATCH /deals/:id/payee-invoice`
- `rotateShareLink(id, token)` → `POST /deals/:id/share-link`
- `getPublicDeal(shareToken)` → `GET /public/deals/:shareToken`

**Types:** Full TypeScript interfaces for all requests/responses (DealView, DealStatus, PublicDeal, etc.)

---

## 🎨 Design System

**Fonts:**
- Display: Space Grotesk
- Body: Inter
- Mono: IBM Plex Mono
- **Digital/Stopwatch:** Orbitron + Share Tech Mono (GNJ logo)

**Colors (CSS variables):**
- Vault surfaces: `#0a0c0e` → `#181b1f`
- Gold: `#c49632` (500) → `#d4a843` (400)
- Ink: `#faf9f6` (100) → `#5a5852` (700)
- Status colors per deal state

**Utilities:**
- `.logo-gnj` / `.logo-gnj-sm` — gold-glow stopwatch logo
- `.vault-card` — hover gold border glow
- `.gold-accent` — gradient underline
- `.font-digital` — tabular-nums, wide tracking
- `.hero-gradient` — ink → gold → ink text gradient

---

## ⚠️ Known Issues / Backend Gaps

| Issue | Impact | Fix Needed |
|-------|--------|------------|
| **Public approve requires auth** | Client cannot approve from public link without login | Add `POST /public/deals/:shareToken/approve` (no auth, uses share token) or magic-link email flow |
| **No `GET /auth/me`** | Settings page cannot fetch current user profile | Add backend endpoint returning User + default payee invoice |
| **No CV types in API client** | Live CV pages can't be typed | Add `Profile`, `CVEntry`, `VerificationResult` types + API methods |
| **Payee invoice update not in Deal Detail** | Freelancer can't change payout destination after create | Add inline edit in Deal Detail (endpoint exists: `PATCH /deals/:id/payee-invoice`) |

---

## 📋 Remaining Pages to Build

| Priority | Page | Route | Backend Endpoint | Notes |
|----------|------|-------|------------------|-------|
| **High** | Live CV (Public Profile) | `/cv/[slug]` | `GET /cv/:slug` | Core differentiator — hash-verified reputation |
| **High** | Verify Entry | `/cv/[slug]/verify/[entryId]` | `GET /cv/:slug/verify/:entryID` | Cryptographic proof viewer |
| **Medium** | Settings / Profile | `/settings` | `GET /auth/me` (needs backend) | Update display name, default payee invoice, password |
| **Low** | Operator Dispute Queue | `/operator/disputes` | `GET /disputes`, `POST /disputes/:id/resolve` | Admin only |
| **Low** | Operator Reconcile | `/operator/deals/[id]/reconcile` | `POST /deals/:id/reconcile` | Admin only |

---

## 🧪 Testing Checklist (Local)

```bash
# Start backend (requires .env with DATABASE_URL, JWT_SECRET, LNBITS_*)
cd backend && go run ./cmd/api

# Start frontend
cd frontend && npm run dev
# → http://localhost:3000
```

**Flows to verify:**
1. **Signup → Login → Dashboard** — tokens persist in localStorage
2. **Create Deal** — form validation (sats > 0, lnbc invoice), redirects to detail
3. **Deal Detail (freelancer)** — Submit Work, Dispute, Rotate Link buttons work
4. **Public Deal** — loads via share token, shows invoice, status updates after payment
5. **Approve flow** — currently blocked (see issue above)

---

## 📁 File Structure (New/Modified)

```
frontend/
├── src/
│   ├── app/
│   │   ├── page.tsx                    # Home (redesigned)
│   │   ├── layout.tsx                  # Added Orbitron/Share Tech Mono fonts
│   │   ├── globals.css                 # Digital font, logo-gnj, hero-gradient, gold-glow
│   │   ├── login/page.tsx              # Updated AuthShell, gold links
│   │   ├── signup/page.tsx             # Updated AuthShell, gold links
│   │   ├── dashboard/page.tsx          # NEW: Deal list + empty state
│   │   ├── deals/
│   │   │   ├── new/page.tsx            # NEW: Create deal form
│   │   │   └── [dealId]/page.tsx       # NEW: Detail + actions + timeline
│   │   └── public/deals/[shareToken]/page.tsx  # NEW: Client view
│   ├── components/
│   │   ├── ui/
│   │   │   ├── button.tsx              # Variants, sizes, loading
│   │   │   ├── input.tsx
│   │   │   └── form-banner.tsx
│   │   ├── auth/
│   │   │   └── auth-shell.tsx          # NEW: Vault-styled auth card
│   │   └── deals/
│   │       └── deal-card.tsx           # NEW: Dashboard deal row
│   ├── lib/
│   │   ├── api/
│   │   │   └── api-client.ts           # Full deals API + types
│   │   ├── auth/
│   │   │   └── auth-context.tsx        # Fixed /auth/signup endpoint
│   │   └── utils/
│   │       └── format.ts               # NEW: Currency, relative time
```

---

## 📝 Documentation Updates Needed

- [ ] `README.md` — Add frontend setup, env vars, page overview
- [ ] `frontend/README.md` — Component library, design tokens, API client usage
- [ ] `docs/FRONTEND_ARCHITECTURE.md` — New file: routing, auth flow, state management
- [ ] `docs/DEAL_LIFECYCLE.md` — Map UI states to backend status transitions
- [ ] `AGENTS.md` — Update with new component patterns

---

## 🚀 Next Steps (Suggested Order)

1. **Fix public approve** — Add backend `POST /public/deals/:shareToken/approve` (no auth)
2. **Add `GET /auth/me`** — Enable Settings page
3. **Build Live CV** — `/cv/[slug]` + `/cv/[slug]/verify/[entryId]`
4. **Settings page** — Profile, default payee invoice, password change
5. **Operator pages** — Dispute queue, reconcile (if needed for MVP)
6. **E2E test** — Full flow: signup → create deal → pay → submit → approve → verify on CV