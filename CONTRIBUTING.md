# Contributing to Ganji

This document covers team ownership, build order, and git conventions. For the current architecture, money flows, and API contract, read [`explained.md`](./explained.md) and [`API_REFERENCE.md`](./API_REFERENCE.md) first. Those, plus [`BACKEND_STATUS.md`](./BACKEND_STATUS.md), are the source of truth — the original build document (a `.docx`) is not part of this repo.

---

## Team Ownership

| Dev | Owns |
|---|---|
| **Backend A** | Auth, user accounts, deal CRUD, PostgreSQL schema & migrations |
| **Backend B** | Lightning integration, webhook handling, WebSocket server, CV verification endpoints |
| **Frontend A** | Create Deal, Freelancer Dashboard, auth pages, Provider Hub, Settings (all freelancer-facing screens) |
| **Frontend B** | Live CV page, client escrow state machine, sandbox viewer, CV verification UI (all client-facing screens) |

The split runs along natural seams: backend splits between pure CRUD (predictable, testable alone) and external-system integration (Lightning, webhooks — more moving parts, needs a dedicated owner). Frontend splits along user role, matching the product itself — freelancer screens barely share state with client screens, so this avoids merge conflicts.

**Before writing any code**, all 4 devs must agree on the API contract — request/response shapes for `/deals`, `/cv/:slug`, and the WebSocket event format — which is recorded in [`API_REFERENCE.md`](./API_REFERENCE.md). Once that's locked, all 4 people build in parallel without blocking each other.

---

## Build Order

Follow the phases in order. Do not skip ahead or work out of sequence — each phase depends on the previous one being functional:

1. **Foundation** — DB schema, auth, basic layout (Week 1–2)
2. **Core Escrow Flow** — deal CRUD, freelancer submit, client state machine, WebSocket (Week 3–5)
3. **Lightning Integration** — real LNbits invoices, webhook, payment detection (Week 6–7)
4. **Live CV & Verification** — public CV endpoint, hash verification, trust score (Week 8–9)
5. **Polish & Deploy** — sanitization hardening, mobile responsiveness, production deploy (Week 10–12)

Items from the Stage 2 / Stage 3 roadmap (M-Pesa, multisig arbiter, file uploads, etc.) are **not part of this build**. They come after Phase 5 ships and the product is live. Do not build them now, and do not ignore them either — they're committed roadmap, just sequenced for after launch.

---

## Git Workflow

- Feature branches off `main`, named `<dev-initials>/<short-description>` (e.g. `ao/lightning-webhook`)
- PRs require one review before merge
- Never force-push a shared branch
- Conventional commit messages (`feat:`, `fix:`, `chore:`, `docs:`)

---

## Reference Files

- [`explained.md`](./explained.md) — architecture, escrow/state machine, package walkthrough
- [`API_REFERENCE.md`](./API_REFERENCE.md) — endpoint contract, error codes, state machine
- [`BACKEND_STATUS.md`](./BACKEND_STATUS.md) — what's built vs. not
