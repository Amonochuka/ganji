# Ganji Frontend

Next.js 16 (App Router) + TypeScript + Tailwind v4 frontend for the Ganji Lightning escrow platform.

## Stack

- **Framework:** Next.js 16 with App Router, React 19
- **Language:** TypeScript (strict mode)
- **Styling:** Tailwind CSS v4 (CSS-first config via `@theme` in `globals.css`)
- **Fonts:** Space Grotesk (display), Inter (body), IBM Plex Mono (mono), Orbitron + Share Tech Mono (digital/stopwatch GNJ logo)
- **Auth:** JWT in localStorage (access + refresh tokens), `AuthContext` provider
- **API:** `lib/api/api-client.ts` — typed fetch wrapper with credentials

## Design System

**Colors (CSS variables in `globals.css`):**
- Vault surfaces: `#0a0c0e` → `#181b1f` (near-black matte, cool)
- Gold: `#c49632` (500) → `#d4a843` (400) — matte, sophisticated
- Ink: `#faf9f6` (100) → `#5a5852` (700)
- Status colors per deal state

**Utilities:**
- `.vault-card` — hover gold border glow
- `.logo-gnj` / `.logo-gnj-sm` — stopwatch-style GNJ logo with gold glow pulse
- `.font-digital` — Orbitron, tabular-nums, wide tracking
- `.hero-gradient` — ink → gold → ink text gradient
- `.gold-accent` — gradient underline
- Focus rings: gold, always visible

## Pages

| Route | File | Description |
|---|---|---|
| `/` | `app/page.tsx` | Marketing home |
| `/login` | `app/login/page.tsx` | Email/password login |
| `/signup` | `app/signup/page.tsx` | Registration with validation |
| `/dashboard` | `app/dashboard/page.tsx` | Authenticated deal list |
| `/deals/new` | `app/deals/new/page.tsx` | Create deal form |
| `/deals/[id]` | `app/deals/[dealId]/page.tsx` | Deal detail + actions |
| `/public/deals/[shareToken]` | `app/public/deals/[shareToken]/page.tsx` | Client-facing view |

## Components

| Component | Path | Props |
|---|---|---|
| `Button` | `components/ui/button.tsx` | `variant: primary\|ghost\|outline\|danger`, `size: sm\|md\|lg`, `isLoading` |
| `Input` | `components/ui/input.tsx` | `label`, `error`, standard HTML attrs |
| `FormBanner` | `components/ui/form-banner.tsx` | `message`, `type: error\|success` |
| `AuthShell` | `components/auth/auth-shell.tsx` | `eyebrow`, `title`, `subtitle`, `children`, `footer` |
| `DealCard` | `components/deals/deal-card.tsx` | `deal: DealView`, `onClick?` |

## API Client

`lib/api/api-client.ts` — central fetch wrapper with:

```typescript
api.listDeals(token)
api.createDeal(data, token)
api.getDeal(id, token)
api.submitWork(id, token)
api.approveDeal(id, token)
api.disputeDeal(id, reason, token)
api.updatePayeeInvoice(id, data, token)
api.rotateShareLink(id, token)
api.getPublicDeal(shareToken)
```

Full TypeScript interfaces for all requests/responses: `DealView`, `DealStatus`, `PublicDeal`, `CreateDealRequest`, etc.

## Local Development

```bash
cd frontend
cp .env.example .env.local   # NEXT_PUBLIC_API_URL=http://localhost:8080
npm install
npm run dev
```

Runs on `http://localhost:3000`.

## Build

```bash
npm run build   # production build (Turbopack)
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` | Backend API base URL |
| `NEXT_PUBLIC_WS_URL` | — | WebSocket URL (not implemented yet) |

## Known Issues

- **Public approve requires auth** — `POST /deals/:id/approve` is behind auth middleware. Need backend `POST /public/deals/:shareToken/approve` for client approval from public link.
- **No `GET /auth/me`** — Settings page needs this backend endpoint.
- **CV types missing** — Live CV pages need `Profile`, `CVEntry` types + API methods in client.

## File Structure

```
frontend/
├── src/
│   ├── app/
│   │   ├── page.tsx                    # Home
│   │   ├── layout.tsx                  # Fonts, AuthProvider
│   │   ├── globals.css                 # Tailwind v4 @theme, design tokens
│   │   ├── login/page.tsx
│   │   ├── signup/page.tsx
│   │   ├── dashboard/page.tsx
│   │   ├── deals/
│   │   │   ├── new/page.tsx
│   │   │   └── [dealId]/page.tsx
│   │   └── public/deals/[shareToken]/page.tsx
│   ├── components/
│   │   ├── ui/                         # Button, Input, FormBanner
│   │   ├── auth/                       # AuthShell
│   │   └── deals/                      # DealCard
│   └── lib/
│       ├── api/api-client.ts           # API client + types
│       ├── auth/auth-context.tsx       # AuthContext + useAuth
│       └── utils/format.ts             # Currency, relative time
```

## Documentation

- [`FRONTEND_REPORT.md`](../FRONTEND_REPORT.md) — Full implementation status
- [`../README.md`](../README.md) — Project overview
- [`../docs/`](../docs/) — Backend architecture, deal lifecycle, money flow, etc.