# Ganji Frontend — Implementation Report

**Last updated:** 2026-11-22<br>
**Branch:** main  
**Commit:** post-`22c915b` (milestone redesign + accepted-hold funding)

---

## ✅ Completed Pages

| Page | Route | Description | Backend Endpoints |
|------|-------|-------------|-------------------|
| **Home** | `/` | Marketing landing with GNJ stopwatch-logo, hero, feature cards | — |
| **Login** | `/login` | Email/password auth, redirects to dashboard | `POST /auth/login` |
| **Signup** | `/signup` | Registration with validation, redirects to dashboard | `POST /auth/signup` |
| **Dashboard** | `/dashboard` | Authenticated deal list with status cards, empty state, create CTA | `GET /deals` |
| **Create Deal** | `/deals/new` | Form: title, amount (sats), platform dropdown, client email, payee invoice (lnbc); milestone mode: add milestones (title + sats each); generate the freelancer payout invoice from a linked wallet | `POST /deals` (with `milestones[]`), `POST /deals/wallet/invoice` |
| **Deal Detail** | `/deals/[id]` | Full deal view, timeline, deliverables, share link, milestone actions and stage-specific payment/escrow/payout status refresh controls | `GET /deals/:id`, `GET /deals/:id/payment`, submit/dispute/share/approve/exit, artifact and milestone endpoints |
| **Public Deal** | `frontend/src/app/public/deals/[shareToken]/page.tsx` | Client-facing view: fund buttons on the deal and each milestone using a linked wallet, invoice to pay, approve/reset/accept/reject/exit actions wired to public endpoints; `locked` shown as "Funds Held" | `GET /public/deals/:shareToken`, `POST /public/deals/:shareToken/fund`, `POST /public/deals/:shareToken/approve`, `POST /public/deals/:shareToken/payout/reset`, public milestone actions |

---

## 🔧 Core Components

| Component | Location | Purpose |
|-----------|----------|---------|
| `Button` | `components/ui/button.tsx` | Primary/ghost/outline/danger variants, sm/md/lg sizes, loading state |
| `Input` | `components/ui/input.tsx` | Form input with label, error state, focus gold glow |
| `PasswordInput` | `components/ui/password-input.tsx` | Password field with working show/hide toggle |
| `FormBanner` | `components/ui/form-banner.tsx` | Error/success toast banner |
| `AuthShell` | `components/auth/auth-shell.tsx` | Centered auth card with vault-seam styling |
| `DealCard` | `components/deals/deal-card.tsx` | Dashboard deal row with status badge, amount, client, copy share link |
| `DealDetailPage` | `app/deals/[dealId]/page.tsx` | Authed deal view with artifact deliverables panel and synthetic single-step milestone |
| `PublicDealPage` | `app/public/deals/[shareToken]/page.tsx` | Client fund/approve link with linked-wallet fund buttons and synthetic single-step milestone |
| `StatusIcon` | `components/ui/status-icon.tsx` | Inline SVG icons per status (including "Funds Held" for `locked`); no emoji status map |
| `WalletLinkDialog` | `components/deals/wallet-link-dialog.tsx` | Shared LNbits URL/key prompt used by client funding and freelancer payout invoice generation |
| `format.ts` | `lib/utils/format.ts` | Currency (sats → ₿/k sats), relative time |

---

## 🔌 API Client (`lib/api/api-client.ts`)

**Auth:**
- `login(credentials)` → `POST /auth/login`
- `signup(data)` → `POST /auth/signup` (fixed from `/register`)
- `logout()` → clears client-side auth state and local storage; the frontend does not call backend `POST /auth/logout`

**Deals:**
- `listDeals(token)` → `GET /deals`
- `createDeal(data, token)` → `POST /deals` (`data.milestones?` supported)
- `getDeal(id, token)` → `GET /deals/:id`
- `listArtifacts(id, token)` → `GET /deals/:id/artifacts`
- `uploadArtifact(id, kind, file, token)` → `POST /deals/:id/artifacts` (multipart)
- `downloadArtifact(id, artifactId, token)` → `GET /deals/:id/artifacts/:artifactID/download`
- `deleteArtifact(id, artifactId, token)` → `DELETE /deals/:id/artifacts/:artifactID`
- `submitWork(id, token)` → `POST /deals/:id/submit` (milestone mode: submits the active milestone via the same endpoint)
- `approveDeal(id, token)` → `POST /deals/:id/approve` (milestone mode: releases the active milestone)
- `disputeDeal(id, reason, token)` → `POST /deals/:id/dispute`
- `updatePayeeInvoice(id, data, token)` → `PATCH /deals/:id/payee-invoice`
- `rotateShareLink(id, token)` → `POST /deals/:id/share-link`
- `getPublicDeal(shareToken)` → `GET /public/deals/:shareToken`
- `getPublicMilestones(shareToken)` → `GET /public/deals/:shareToken/milestones`
- `fundPublicDeal(shareToken, data)` → `POST /public/deals/:shareToken/fund` (no auth; `data` = `{wallet_url, wallet_key, milestone_id?}`)
- `generateInvoice(data, token)` → `POST /deals/wallet/invoice` (protected; `data` = `{wallet_url, wallet_key, amount_sats, memo?}`)
- `publicApproveDeal(shareToken)` → `POST /public/deals/:shareToken/approve` (no auth)
- `publicResetPayout(shareToken, milestoneId?)` → `POST /public/deals/:shareToken/payout/reset` (no auth)
- `publicAcceptNextMilestone(shareToken, milestoneId)` → `POST /public/deals/:shareToken/milestones/:milestoneID/accept-next` (no auth)
- `publicRejectMilestone(shareToken, milestoneId, reason)` → `POST /public/deals/:shareToken/milestones/:milestoneID/reject` (no auth)
- `publicExitDeal(shareToken)` → `POST /public/deals/:shareToken/exit` (no auth)

**Milestones (authed):**
- `createMilestone(id, title, amountSats, order, token)` → `POST /deals/:id/milestones`
- `listMilestones(id, token)` → `GET /deals/:id/milestones`
- `updateMilestone(id, milestoneId, status, completedAt, token)` → `PATCH /deals/:id/milestones/:milestoneID`
- `exitDeal(id, token)` → `POST /deals/:id/exit` (symmetric participant exit)

**Types:** Full TypeScript interfaces for all requests/responses (DealView, DealStatus, PublicDeal, MilestoneView, FundDealRequest, GenerateInvoiceRequest, etc.). Labels: `locked` = **"Funds Held"** (LND-accepted hold on the escrow node, not settled), `released` = paid, `completed` = payout pending (not paid), `disputed`/`rejected` = under review (hold stays). Work submission requires `locked`; `awaiting_payment → work_submitted` is invalid.

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
| **Payee invoice update not in Deal Detail** | Freelancer can't change the payout destination from the deal page | Settings already edits the default payee invoice; add inline edit in Deal Detail (`PATCH /deals/:id/payee-invoice`) |
| **No dedicated funding-status UI test** | Fund/lock and "Funds Held" label regressions could slip through | Add frontend tests for `fundPublicDeal`, `generateInvoice`, and the status icon map |

---

## 📋 Additional Pages (built)

| Page | Route | Backend Endpoint | Notes |
|------|-------|------------------|-------|
| Live CV (Public Profile) | `/cv/[slug]` | `GET /cv/:slug` | Hash-verified reputation |
| Verify Entry | `/cv/[slug]/verify/[entryId]` | `GET /cv/:slug/verify/:entryID` | Cryptographic proof viewer |
| Settings / Profile | `/settings` | `GET /auth/me` | Display name, default payee invoice, password |
| Operator Dispute Queue | `/operator/disputes` | `GET /disputes`, `POST /disputes/:id/resolve` | Admin only |
| Operator Reconcile | `/operator/deals/[id]/reconcile` | `POST /deals/:id/reconcile` | Admin only |

---

## 🧪 Testing Checklist (Local)

```bash
# Start backend (requires .env with DATABASE_URL, JWT_SECRET, LNBITS_*, LND_REST_*)
cd backend && go run ./cmd/api

# Start frontend
cd frontend && npm run dev
# → http://localhost:3000
```

**Flows to verify:**
1. **Signup → Login → Dashboard** — tokens persist in localStorage
2. **Create Deal** — form validation (sats > 0, lnbc invoice), optional payout invoice generation from a linked wallet, redirects to detail
3. **Deal Detail (freelancer)** — Submit Work (only from `locked`), Dispute, Rotate Link buttons work
4. **Public Deal** — loads via share token, fund button on the deal/milestones pays the hold via a linked wallet; status becomes "Funds Held" only after LND accepts the hold
5. **Approve flow** — authed approve and public-link approve reveal R and pay the freelancer directly; reject after `work_submitted` leaves the hold untouched

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
│   │   │   ├── form-banner.tsx
│   │   │   └── status-icon.tsx         # Inline SVG status icons (no emoji map)
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

- [x] `docs/FRONTEND_ARCHITECTURE.md` — Routing, auth flow, state management, and fund/invoice endpoints are documented
- [x] `docs/FRONTEND_REPORT.md` — This report is current with accepted-hold funding and status icons
- [x] `README.md` — Frontend routes and major component overview
- [x] `frontend/README.md` — Component library, design tokens, API client modules and local setup
- [x] `docs/DEAL_LIFECYCLE.md` — UI state transitions are mapped to backend lifecycle rules
- [x] `frontend/AGENTS.md` — Component and API client patterns are documented

---

## 🚀 Next Steps (Suggested Order)

1. ~~**Fix public approve**~~ — backend `POST /public/deals/:shareToken/approve` shipped 2026-10-20; frontend wired same day (`publicApproveDeal` + public page button)
2. ~~**Add `GET /auth/me`**~~ — backend shipped 2026-10-20; `getMe` + Settings page are live
3. ~~**Build Live CV / Settings / Operator pages**~~ — all routes are implemented
4. **Accepted-hold funding E2E** — signup → create deal (optional linked-wallet payout invoice) → public `fundPublicDeal` → verify "Funds Held" only after LND accepts the hold → submit (requires `locked`) → approve reveals R and pays the freelancer → verify on CV
5. **Frontend tests** — fund/invoice methods and status icon map
