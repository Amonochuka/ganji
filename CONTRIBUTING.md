# Contributing to Ganji

This document covers team ownership, build order, and git conventions. For the full product spec — user flows, dispute handling, API contract, database schema — read [`Ganji-Build-Documentation.docx`](./Ganji-Build-Documentation.docx) first. Nothing here overrides that doc; this is the day-to-day operating guide for the 4 of us building it.

---

## Team Ownership

| Dev | Owns |
|---|---|
| **Backend A** | Auth, user accounts, deal CRUD, PostgreSQL schema & migrations |
| **Backend B** | Lightning integration, webhook handling, WebSocket server, CV verification endpoints |
| **Frontend A** | Create Deal, Freelancer Dashboard, auth pages, Provider Hub, Settings (all freelancer-facing screens) |
| **Frontend B** | Live CV page, client escrow state machine, sandbox viewer, CV verification UI (all client-facing screens) |

The split runs along natural seams: backend splits between pure CRUD (predictable, testable alone) and external-system integration (Lightning, webhooks — more moving parts, needs a dedicated owner). Frontend splits along user role, matching the product itself — freelancer screens barely share state with client screens, so this avoids merge conflicts.

**Before writing any code**, all 4 devs must agree on the API contract (Section 10 of the build doc — request/response shapes for `/deals`, `/cv/:slug`, and the WebSocket event format). Once that's locked, all 4 people build in parallel without blocking each other.

---

## Build Order

Follow Section 8 of the build doc in order. Do not skip ahead or work out of sequence — each phase depends on the previous one being functional:

1. **Foundation** — DB schema, auth, basic layout (Week 1–2)
2. **Core Escrow Flow** — deal CRUD, freelancer submit, client state machine, WebSocket (Week 3–5)
3. **Lightning Integration** — real LNbits invoices, webhook, payment detection (Week 6–7)
4. **Live CV & Verification** — public CV endpoint, hash verification, trust score (Week 8–9)
5. **Polish & Deploy** — sanitization hardening, mobile responsiveness, production deploy (Week 10–12)

Items in Section 9 of the build doc ("Roadmap Stage 2 / Stage 3" — M-Pesa, multisig arbiter, file uploads, etc.) are **not part of this build**. They come after Phase 5 ships and the product is live. Do not build them now, and do not ignore them either — they're committed roadmap, just sequenced for after launch.

---

## Git Workflow

- Feature branches off `main`, named `<dev-initials>/<short-description>` (e.g. `ao/lightning-webhook`)
- PRs require one review before merge
- Never force-push a shared branch
- Conventional commit messages (`feat:`, `fix:`, `chore:`, `docs:`)

---

## Reference Files

- [`FOLDER_STRUCTURE.txt`](./FOLDER_STRUCTURE.txt) — exact repo tree
- [`SCAFFOLD_COMMANDS.md`](./SCAFFOLD_COMMANDS.md) — terminal commands to recreate the structure from scratch
