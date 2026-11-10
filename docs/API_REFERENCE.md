# Ganji Backend — API Reference

**Base URL:** `http://localhost:8080`
**Auth:** JWT Bearer token in `Authorization` header (except where noted)

> **Milestone mode.** Deals may carry sequential milestones (`milestones` array on `POST /deals`).
> Each milestone has its own hold invoice (see `MILESTONE_REDESIGN_NOTES.md`): fund M1 → release M1 → fund M2, etc.
> `released` = hold settled + freelancer paid (terminal, paid). `completed` = approved/payout-pending, never paid.
> A milestone with no escrow hold cannot be released as paid ("fund it first").
> See §3 "Milestones" for endpoint details.

---

## Conventions

- All request/response bodies are JSON.
- Timestamps are ISO 8601 (`time.RFC3339`).
- IDs are UUIDs.
- Errors return `{"error": "message"}` with the appropriate HTTP status.
- Protected endpoints return `401` if the token is missing/invalid, `403` if the user doesn't own the resource.

---

## 1. Health

### `GET /health`

Public. Returns server and database status.

**Response `200`**
```json
{
  "status": "ok",
  "db": "ok"
}
```

---

## 2. Authentication

**Operator role.** Arbitration endpoints (`/disputes*`) require an operator.
Operators are promoted at boot from the comma-separated `OPERATOR_EMAILS`
environment variable (never self-serve). On login the role is embedded in the
access-token claims as `is_operator`; a promoted user picks it up on their next
login. Non-operators hitting an operator-only route get `403 Forbidden`.

### `POST /auth/signup`

Public. Register a new user.

**Request**
```json
{
  "email": "user@example.com",
  "password": "securepassword",
  "display_name": "Alice"
}
```

**Response `201`**
```json
{
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "display_name": "Alice",
    "slug": "alice",
    "trust_score": 100,
    "created_at": "2026-08-26T12:00:00Z"
  },
  "access_token": "...",
  "refresh_token": "..."
}
```

### `POST /auth/login`

Public. Authenticate with email and password.

**Request**
```json
{
  "email": "user@example.com",
  "password": "securepassword"
}
```

**Response `200`**
```json
{
  "user": { ... },
  "access_token": "...",
  "refresh_token": "..."
}
```

### `POST /auth/refresh-token`

Public. Rotate refresh token, get new access + refresh pair.

**Request**
```json
{
  "refresh_token": "..."
}
```

**Response `200`**
```json
{
  "access_token": "...",
  "refresh_token": "..."
}
```

### `POST /auth/logout`

Public. Revoke a refresh token.

**Request**
```json
{
  "refresh_token": "..."
}
```

**Response `200`**
```json
{
  "message": "logged out"
}
```

### `GET /auth/me`

Requires auth. Returns the authenticated user's profile (same `User` shape as signup/login, without tokens).

**Response `200`**
```json
{
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "display_name": "Alice",
    "slug": "alice",
    "trust_score": 100,
    "created_at": "2026-08-26T12:00:00Z"
  }
}
```

**Response `404`** — `{"error": "user not found"}` if the token's user no longer exists.

---

## 3. Deals

All deal endpoints require `Authorization: Bearer <access_token>` **except the public share-link endpoints** (`GET /public/deals/:shareToken`, `GET /public/deals/:shareToken/milestones`, `POST /public/deals/:shareToken/approve`, `POST /public/deals/:shareToken/milestones/:milestoneID/accept-next`, `POST /public/deals/:shareToken/milestones/:milestoneID/reject`, `POST /public/deals/:shareToken/exit`), which are intentionally unauthenticated so the freelancer can share a link the client can open without an account — to check payment status, pay, approve delivery, accept/reject a milestone, or exit.

Every deal carries a unique **`share_token`** — a high-entropy random value, separate from the deal's UUID. It is the only thing that identifies a deal on the public endpoint, so:

- a leaked link can be **revoked** by regenerating the token (`POST /deals/:dealID/share-link`) — the old link dies immediately;
- the internal deal UUID is **never exposed** unauthenticated;
- a freelancer can re-share (rotate) the link if a client loses it.

### `GET /public/deals/:shareToken`

Public. Returns a safe, limited view of the deal for the shareable payment link. Anyone with the link can view the deal title, amount, status, and the bolt11 invoice to pay. The internal deal id and all sensitive fields (preimage, preimage_hash, payee_invoice, freelancer_id, client_email, checking_id, share_token, verified_at) are never exposed.

The endpoint **refreshes the hold status with LNbits before answering**: if the client already paid but the webhook was delayed or lost, the deal is locked on the spot. LNbits being unreachable is not an error — the visitor gets the last known status.

**Response `200`**
```json
{
  "deal": {
    "title": "Landing page redesign",
    "amount_sats": 50000,
    "source_platform": "Telegram",
    "invoice": "lnbc50000n1...",
    "status": "locked",
    "created_at": "2026-08-26T12:00:00Z",
    "milestones": [
      {"id": "uuid", "title": "Design", "amount_sats": 20000, "order": 1, "status": "released", "invoice": "lnbc...", "created_at": "..."},
      {"id": "uuid", "title": "Build", "amount_sats": 30000, "order": 2, "status": "awaiting_payment", "invoice": "lnbc...", "created_at": "..."}
    ]
  }
}
```

In milestone mode the endpoint also refreshes each active milestone hold with LNbits before answering, so status and the active invoice reflect reality even if the webhook was delayed. `invoice`/`status` are mirrored from the active milestone for compatibility; `milestones[]` carries per-milestone truth (`released` = paid, `completed` = payout pending, `disputed`/`rejected` = hold stays in review).

**Response `404`**
```json
{
  "error": "deal not found"
}
```

### `POST /public/deals/:shareToken/approve`

Public. The client approves straight from the share link — no account and no JWT required; possession of the share token is the authorization. Money moves exactly like the authed `POST /deals/:dealID/approve`: settle the hold (preimage revealed, funds land in the Ganji wallet), then pay the freelancer's `payee_invoice`; on success the deal is `released` and the Live CV entry is anchored.

Legal only from `locked` or `work_submitted` — the hold status is refreshed from LNbits first, so a just-paid deal locks on the spot. Approving from `disputed` still requires the authenticated, email-matched endpoint (arbitration stays gated). Anything else is `400`. Idempotent like the authed path: re-approving an already-settled deal pays out without double-settling.

In milestone mode the endpoint releases the **active milestone's** hold via the canonical `ReleaseMilestone` path (settle + pay → `released`), then funds the next milestone. When the final milestone is released the deal itself becomes `released`. A milestone under `disputed`/`rejected` is not approvable here — it requires operator resolution. A milestone with no escrow hold errors with "fund it first" and is never marked paid.

> **Note:** anyone holding the link can release the freelancer's payment. Rotate the link (`POST /deals/:dealID/share-link`) to revoke a leaked one.

**Response `200`**
```json
{
  "message": "deal approved and escrow released",
  "deal": {
    "id": "uuid",
    "status": "released",
    ...
  }
}
```

**Responses:**

| Status | Condition |
|---|---|
| `400` | Blank token, or deal not in `locked`/`work_submitted` |
| `404` | No deal for the share token |
| `500` | Network leg failed (deal state unchanged by the DB's rules) |

### `POST /deals/:dealID/share-link`

Freelancer (owner) only. Regenerates the deal's `share_token`, immediately invalidating any previously shared link. The share link is frozen once the deal is `released` or `refunded`.

**Response `200`**
```json
{
  "message": "share link regenerated",
  "deal": {
    "id": "uuid",
    "title": "Landing page redesign",
    "share_token": "abc123...",
    "status": "awaiting_payment",
    "...": "..."
  }
}
```

The frontend renders the link from `share_token` (e.g. `FRONTEND_URL/deal/<share_token>`).

---

### `POST /deals`

Create a new deal. Generates a fresh escrow preimage, draws a **hold invoice** on LNbits (`payment_hash = sha256(preimage)`), and stores the deal. No money moves on creation — the funds stay held on the network until approve/dispute.

With `milestones` (≥1, each `title` + positive `amount_sats`) the deal is created in **sequential per-milestone mode**: no single upfront deal hold; `amount_sats` is derived as the sum; M1 is funded immediately (`FundMilestone` → per-milestone hold invoice); later milestones get their hold only after the previous milestone is `released` (paid). `deal.Invoice`/`deal.CheckingID` mirror the active (M1) hold for compatibility. Legacy single-hold flow is preserved when `milestones` is empty/omitted.

**Request (legacy single-hold)**
```json
{
  "title": "Landing page redesign",
  "amount_sats": 50000,
  "source_platform": "Telegram",
  "client_email": "client@example.com",
  "payee_invoice": "lnbc50000n1..."
}
```

**Request (milestone mode)**
```json
{
  "title": "Landing page redesign",
  "source_platform": "Telegram",
  "client_email": "client@example.com",
  "payee_invoice": "lnbc50000n1...",
  "milestones": [
    {"title": "Design", "amount_sats": 20000},
    {"title": "Build", "amount_sats": 30000}
  ]
}
```

- `client_email` — the client's email (who gets to approve/dispute).
- `payee_invoice` — the freelancer's Lightning invoice; it's where the settled escrow is forwarded on approve.

**Response `201`**
```json
{
  "deal": {
    "id": "uuid",
    "freelancer_id": "uuid",
    "client_email": "client@example.com",
    "title": "Landing page redesign",
    "amount_sats": 50000,
    "source_platform": "Telegram",
    "preimage_hash": "hex",
    "invoice": "lnbc...",
    "checking_id": "lnbits_checking_id",
    "share_token": "abc123...",
    "status": "awaiting_payment",
    "created_at": "2026-08-26T12:00:00Z",
    "verified_at": null
  }
}
```

The raw `preimage` (the network secret that can settle the escrow) and the freelancer's `payee_invoice` are stored server-side but intentionally **omitted** from API responses.

> ✅ **Fixed (2026-09-19).** The omission is now enforced: every authenticated
> deal endpoint (and the dispute queue) serializes the redacted `DealView`
> (`internal/deals/types.go`) instead of the raw `Deal` — `preimage`,
> `payee_invoice`, and the payout-tracking fields never leave the server.
> Regression-guarded by `internal/deals/deal_view_test.go`. See
> `explained.md` §10 #1.

`share_token` is the token for the public payment link (`GET /public/deals/:shareToken`) — there is one per deal from creation, rotatable via `POST /deals/:dealID/share-link`.

### `GET /deals`

List all deals owned by the authenticated user. Ordered by `created_at DESC`.

**Response `200`**
```json
{
  "deals": [ ... ]
}
```

### `GET /deals/:dealID`

Get a single deal. Ownership enforced. In milestone mode the response `deal` includes `milestones[]` (ordered by `order`) and `invoice`/`checking_id`/`status` mirror the **active milestone** — the first milestone whose status is not `released`/`refunded`/`exited`. Use `GET /deals/:dealID/milestones` (or this list) for exact per-milestone state; a nil active milestone means all are terminal. Same contract applies to `POST /deals` (201), `GET /deals` (list entries), and public `GET /public/deals/:shareToken`.

**Response `200`**
```json
{
  "deal": { ... }
}
```

### `GET /deals/:dealID/payment`

Poll LNbits for the hold invoice's status. If LNbits reports the payment `paid` and the deal is still `awaiting_payment`, it transitions to `locked`.

Backend autodetect: on a CLN-backed LNbits a held invoice already reports `paid`, so this locks as soon as the client pays. On an LND-backed LNbits a held invoice stays `paid=false` until it's settled, so the deal stays `awaiting_payment` — approve then settles and proves the funds were held.

**Response `200`**
```json
{
  "deal": {
    "id": "uuid",
    "status": "locked",
    ...
  }
}
```

### `POST /deals/:dealID/submit`

Freelancer marks their work as delivered (requires at least one artifact). Legal from `locked` and from `awaiting_payment` — the latter keeps LND-flow deals from deadlocking before the client can approve.

In milestone mode, submit targets the active milestone (`funded`/`in_progress` → `work_submitted`); no deal-level artifact is required (per-milestone artifacts are now enforced via `milestone_id` on the artifact model, backward-compatible — uploads through the existing handler have `milestone_id = nil` and are treated as deal-level). An `awaiting_payment` milestone blocks submit with "awaiting payment before work can be submitted". Per-milestone artifact scoping — artifacts may carry a `milestone_id` tag; listers can filter by it via `ListArtifactsByDeal(dealID, milestoneID)`.

**Response `200`**
```json
{
  "deal": { ... }
}
```

### `POST /deals/:dealID/approve`

Client approves → money moves on the network in two legs: `settle` the hold (reveal the stored preimage, funds land in the Ganji wallet) then `pay` the freelancer's `payee_invoice`. Idempotent: re-approving an already-settled deal pays out without double-settling. On success the deal is `released` and `verified_at` is stamped (Live CV entry).

In milestone mode this releases the active escrow-backed milestone through the canonical path only, then funds the next milestone; the deal becomes `released` only when its final milestone is `released` (paid). Disputed/rejected milestones cannot be approved here.

### `POST /deals/:dealID/exit`

Either participant (freelancer owner or client by email match) may exit — no silent exit; confirmation + reason semantics are enforced by the state rules. Pre-submission: open holds cancelled/refunded to the client, milestone not paid. Post-submission (`work_submitted`/`completed`/`rejected`): no money moves, hold stays, milestone → `disputed` for independent review. Legacy single-hold deals follow the same rule (post-submission → dispute; otherwise cancel → `refunded`).

**Response `200`**
```json
{
  "deal": {
    "id": "uuid",
    "status": "released",
    ...
  }
}
```

### `POST /deals/:dealID/dispute`

Client disputes the delivered work. The client must state **why** in writing;
the deal then freezes in the **`disputed`** arbitration state. This is a
freeze, **not a refund**: the hold stays held on the network (neither paying
the freelancer nor returning the sats) until an arbiter resolves the dispute
to `released` or `refunded`. A client who changes their mind can still
approve (`disputed → released`) to settle and pay.

**Request**
```json
{
  "reason": "deliverable does not match the agreement"
}
```

`reason` is required, trimmed, and capped at 2000 characters.

**Response `200`**
```json
{
  "message": "dispute raised — funds frozen pending arbitration",
  "deal": {
    "id": "uuid",
    "status": "disputed",
    "dispute_reason": "deliverable does not match the agreement",
    "disputed_at": "2026-09-17T12:00:00Z",
    ...
  }
}
```

### `GET /disputes`

**Operator only.** The arbitration queue — every deal frozen in the `disputed`
state, oldest dispute first.

**Response `200`**
```json
{
  "disputes": [
    {
      "id": "uuid",
      "status": "disputed",
      "dispute_reason": "deliverable does not match the agreement",
      "disputed_at": "2026-09-17T12:00:00Z",
      ...
    }
  ]
}
```

### `POST /disputes/:dealID/resolve`

**Operator only.** The arbiter's verdict on a frozen dispute. This is the only
path out of `disputed` that moves money:

- `release` — accept the work: settle the hold and pay the freelancer (the same
  two network legs as approve).
- `refund` — reject the work: cancel the hold so the sats return to the client.

The deal must actually be `disputed`; anything else is `400`. The resolution
and the deciding operator are recorded only **after** the network leg succeeds,
so the DB never claims a money move the network did not make.

**Request**
```json
{
  "resolution": "release"
}
```

`resolution` is `release` or `refund`.

**Response `200`**
```json
{
  "message": "dispute resolved — escrow released to the freelancer",
  "deal": {
    "id": "uuid",
    "status": "released",
    "resolved_by": "arbiter@example.com",
    "resolved_at": "2026-09-17T13:00:00Z",
    ...
  }
}
```

### `PATCH /deals/:dealID/payee-invoice`

Freelancer replaces the payout destination while the deal is still open. If the original `payee_invoice` expired mid-deal and the approve-time payout leg failed, this unsticks release: re-approving settles (already-settled is idempotent) and forwards the funds to the fresh invoice. Frozen once the deal is `released` or `refunded`.

**Request**
```json
{
  "payee_invoice": "lnbc50000n1..."
}
```

**Response `200`**
```json
{
  "message": "payee invoice updated",
  "deal": { ... }
}
```

### `PATCH /deals/:dealID/status`

Transition deal status (freelancer). Validated against the state machine.

**Request**
```json
{
  "status": "work_submitted"
}
```

**Response `200`**
```json
{
  "message": "deal status updated"
}
```

**Valid transitions (freelancer-driven workflow only):**
```
awaiting_payment → work_submitted
locked → work_submitted
work_submitted → reviewing
```

The **money states** (`locked`, `released`, `disputed`, `refunded`) are backend-only: they are set by payment detection, approve, and dispute, never by this endpoint.

### Milestones

In milestone mode each milestone is its own escrow leg with its own hold invoice. Milestone statuses: `pending → awaiting_payment → funded → in_progress → work_submitted → (completed →) released`, with `rejected`/`disputed` for work under review (hold stays), `refunded`/`exited` terminal. `released` = hold settled + freelancer paid (terminal, paid). `completed` = payout-pending, never paid. See `DEAL_LIFECYCLE.md` for the full transition table.

- `POST /deals/:dealID/milestones` (freelancer) — create an additional milestone on an existing deal. Body: `{title, amount_sats, order}` (order ≥ 1). Status starts `pending` (unfunded); it is funded only in sequence.
- `GET /deals/:dealID/milestones` (freelancer or client by email) — list milestones ordered by `order`. Response: `{milestones: [{id, title, amount_sats, order, status, invoice?, completed_at?, created_at}]}`.
- `PATCH /deals/:dealID/milestones/:milestoneID` (freelancer) — update milestone status. Body: `{status, completed_at? (RFC3339)}`. Thin status write; money-moving outcomes (`released`/`paid`) must still go through the canonical escrow path — a milestone with no hold cannot be marked paid via this endpoint alone.

Auth/portal behavior in milestone mode:

- `POST /deals/:dealID/submit` (freelancer) submits the **active milestone** (`funded`/`in_progress` → `work_submitted`) and sets the deal to `work_submitted`. Milestone-mode submit requires at least one deal-level artifact, same as the legacy flow. Per-milestone artifact scoping is a follow-up — see `MILESTONE_REDESIGN_NOTES.md` Outstanding.
- `POST /deals/:dealID/approve` (client by email) releases the active milestone via the canonical payout path and funds the next milestone; releasing the final milestone releases the deal. Disputed/rejected milestones cannot be approved (operator resolution required).
- `POST /deals/:dealID/exit` (freelancer **or** client by email) — symmetric participant exit. Pre-submission milestones (`pending`/`awaiting_payment`/`funded`/`in_progress`) are not paid: their holds are cancelled/refunded to the client (unfunded → `exited`). Post-submission milestones (`work_submitted`/`completed`/`rejected`) do **not** move money — they go to `disputed` for independent review. Legacy single-hold deals: post-submission → dispute review; otherwise the hold is cancelled → `refunded`.

Public (share-token-only, no JWT) milestone actions:

- `GET /public/deals/:shareToken/milestones` — list milestones for the share link.
- `POST /public/deals/:shareToken/milestones/:milestoneID/accept-next` — release that escrow-backed milestone (settle + pay), then fund the next; final milestone releases the deal. `disputed`/`rejected` → `400` (operator resolution required). No-hold → "fund it first" error, never marked paid.
- `POST /public/deals/:shareToken/milestones/:milestoneID/reject` — body `{reason}` (required, ≤2000 chars). Only from `work_submitted`/`completed`. **Does not auto-refund**: hold stays, milestone → `disputed` pending independent review. The reason also lands on the deal's `dispute_reason`.
- `POST /public/deals/:shareToken/exit` — public exit with the same pre/post-submission money rules as the authed exit.

Operator (arbitration):

- `POST /disputes/deals/:dealID/milestones/:milestoneID/resolve` (operator only, `OperatorRequired`). Body `{resolution: "release" | "refund"}`. Resolves a `disputed`/`rejected` milestone: `release` → canonical `ReleaseMilestone` (settle + pay) + advance sequence/finalize deal; `refund` → `CancelHold` + `refunded`. `release` is also accepted from `work_submitted`/`completed` for parity. Milestone `refund` does not auto-fund the next milestone.

---

## 4. Artifacts

All artifact endpoints require auth. Ownership is enforced via the parent deal.

### `POST /deals/:dealID/artifacts`

Upload an artifact (source code or file) as a **multipart form**. The file is
streamed to the storage backend (`STORAGE_PATH`, local disk by default); the DB
row records the generated storage key. Only the freelancer can upload, and only
while the deal is still open to work (not after `work_submitted` /
`reviewing` / `released` / `disputed` / `refunded`).

**Request** — `multipart/form-data`
| Fields |
|---|
| `kind` — `"source_code"` or `"source_file"` (required, must precede the file part) |
| `artifact` — the file itself (required) |

The body is **streamed** (not buffered). The `kind` field must be sent before
the file part — the file is consumed the moment it is seen, so there is no
reordering. Uploads larger than `MAX_UPLOAD_BYTES` (default 10 MB) are
rejected with `400` and no blob is committed; a request whose total body
exceeds the cap plus 1 MiB of multipart envelope is rejected with `413`.

**Response `201`**
```json
{
  "artifact": {
    "id": "uuid",
    "deal_id": "uuid",
    "kind": "source_code",
    "storage_key": "deals/<dealID>/<32-hex-random><sanitized-extension>",
    "uploaded_at": "2026-08-26T12:00:00Z"
  }
}
```

### `GET /deals/:dealID/artifacts`

List all artifacts for a deal.

**Response `200`**
```json
{
  "artifacts": [ ... ]
}
```

### `GET /deals/:dealID/artifacts/:artifactID`

Get a specific artifact.

**Response `200`**
```json
{
  "artifact": { ... }
}
```

### `GET /deals/:dealID/artifacts/:artifactID/download`

Stream the stored artifact content to the caller. Both the freelancer and the
client (matched by the deal's recorded `client_email`) may download; anyone
else gets `403`. The file is served with its content-type guessed from the
sanitized extension and an `attachment` `Content-Disposition`.

**Response `200`** — the raw file bytes (`Content-Length` = stored size).

**Response codes**: `403` if neither party, `404` if the artifact is unknown
or its blob is missing from storage.

---

## 5. Verifications

All verification endpoints require auth. Ownership is enforced via the parent deal.

### `POST /deals/:dealID/artifacts/:artifactID/verifications`

Create a verification record for an artifact.

**Request**
```json
{
  "method": "sandbox",
  "reference": "https://sandbox.example.com/preview/abc"
}
```

`method` must be one of: `"sandbox"`, `"preview_pdf"`, `"preview_image"`.

**Response `201`**
```json
{
  "verification": {
    "id": "uuid",
    "artifact_id": "uuid",
    "method": "sandbox",
    "reference": "https://sandbox.example.com/preview/abc",
    "status": "pending",
    "expires_at": null,
    "created_at": "2026-08-26T12:00:00Z"
  }
}
```

### `GET /deals/:dealID/artifacts/:artifactID/verifications`

List all verifications for an artifact.

**Response `200`**
```json
{
  "verifications": [ ... ]
}
```

### `GET /deals/:dealID/artifacts/:artifactID/verifications/:verificationID`

Get a specific verification.

**Response `200`**
```json
{
  "verification": { ... }
}
```

---

## 6. Live CV

Every released deal anchors its artifacts as hash-verified entries on the
freelancer's public **Live CV** (approve → `released` writes a SHA-256 anchor
per artifact; see the release flow in section 3), and submits each entry's
hash to the public OpenTimestamps calendars for a permanent Bitcoin
timestamp (`internal/ots`). Both endpoints are public — the slug is a
shareable handle, not a secret.

### `GET /cv/:slug`

Public. Returns the freelancer's identity plus all anchored entries, newest
verified first. Anchoring is self-healing: a released deal whose artifacts were
never anchored (hook failure, deals that predate Live CV) gets its entries
written on read, and the trust score is recalcuated from the number of released
deals (`100 + 25·released`, capped at 1000).

**Response `200`**
```json
{
  "profile": {
    "display_name": "Alice",
    "slug": "alice",
    "trust_score": 125,
    "entries": [
      {
        "id": "uuid",
        "deal_title": "Build a site",
        "amount_sats": 5000,
        "source_platform": "telegram",
        "artifact_kind": "source_code",
        "hash": "sha256 hex over the artifact's storage reference",
        "algorithm": "sha256",
        "verified_at": "2026-08-26T12:00:00Z",
        "created_at": "2026-08-26T12:00:00Z"
      }
    ]
  }
}
```

**Responses:**

| Status | Condition | Body |
|---|---|---|
| `200` | CV exists (may have empty `entries`) | `{"profile": {...}}` |
| `400` | Blank slug | `{"error": "invalid input: ..."}` |
| `404` | Slug does not resolve to a CV | `{"error": "cv not found"}` |

### `GET /cv/:slug/verify/:entryID`

Public. Recomputes the release-time SHA-256 anchor from the artifact's current
storage reference and compares it to the stored hash — proving the CV line is
intact, or detecting that the anchor no longer matches the artifact.
A slug/entry mismatch returns `404` exactly like a missing entry, so the
endpoint never confirms an entry's existence under a foreign slug.

When the entry carries a stored OpenTimestamps proof (a `.ots` file anchored
in `cv_entries`), the response also reports the **blockchain timestamp**:
`ots_verified` is true when the proof re-parses and its embedded digest commits
to the entry's hash (the merkle path is replayed against the Bitcoin
transaction the proof embeds), `ots_block_height` is the attested lowest block
height, and `ots_confirmed_at` is the block-header time when the proof was
checked against a live chain (otherwise the time the upgrade worker recorded
confirmation). Live block-header verification is optional — set
`OTS_ESPLORA_URL` to enable it; without it the offline proof check still runs.

**Response `200`**
```json
{
  "verification": {
    "valid": true,
    "entry_id": "uuid",
    "slug": "alice",
    "hash": "...",
    "algorithm": "sha256",
    "matches_current": true,
    "deal_title": "Build a site",
    "verified_at": "2026-08-26T12:00:00Z",
    "ots_verified": true,
    "ots_proof": "...",
    "ots_confirmed_at": "2026-08-26T12:00:00Z",
    "ots_block_height": 891686
  }
}
```

**Responses:**

| Status | Condition | Body |
|---|---|---|
| `200` | Entry resolves; `valid`/`matches_current` reflect hash comparison | `{"verification": {...}}` |
| `400` | Blank slug or entry id | `{"error": "invalid input: ..."}` |
| `404` | Unknown slug or entry not on that slug's CV | `{"error": "cv not found"}` |

---

## 7. Webhooks

### `POST /webhooks/lnbits`

Public endpoint called by LNbits when an invoice is paid. No JWT auth required.

Currently reconciles **deal-level** holds only (`awaiting_payment → locked` for legacy single-hold deals). Milestone holds (`awaiting_payment → funded → in_progress`) are reconciled by poll/refresh (`refreshMilestonePayment`, called from `SubmitWork`/public views) — there is no milestone webhook path yet.

**Headers:**
- `Content-Type: application/json`
- `LNbits-Signature: t=<unix_timestamp>,v1=<hmac_sha256_hex>` (if webhook signing is enabled)

**Request body (from LNbits):**
```json
{
  "checking_id": "lnbits_checking_id",
  "payment_hash": "hex",
  "amount": 50000,
  "fee": 1,
  "memo": "Landing page redesign",
  "status": "success",
  "time": 1700000000
}
```

**Responses:**

| Status | Condition | Body |
|---|---|---|
| `200` | Valid + processed | `{"status": "ok"}` |
| `200` | Payment not successful (valid webhook, nothing to do) | `{"status": "ignored", "reason": "payment not successful"}` |
| `400` | Malformed payload (missing `checking_id`, etc.) | `{"error": "malformed payload"}` |
| `401` | Invalid or missing HMAC signature | `{"error": "invalid signature"}` |
| `404` | No deal found for `checking_id` | `{"error": "no deal for checking_id"}` |
| `413` | Request body larger than 1 MiB (notifications are ~300 bytes) | `{"error": "request body too large"}` |
| `500` | Unexpected internal failure | `{"error": "internal failure"}` |

The request body is capped at **1 MiB** via `http.MaxBytesReader` and rejected
with `413` when exceeded, before any parsing or signature work — this public
endpoint must not be able to force unbounded buffering. Payment-not-successful
returns `200` because the webhook was received and understood — nothing for
Ganji to do. Returning non-2xx would cause LNbits to retry unnecessarily.

**Signature verification:**
- The signed payload is `"{timestamp}.{raw_body}"`
- HMAC-SHA256 with `LNBITS_WEBHOOK_SECRET` as key
- Timestamps older than 5 minutes are rejected (replay protection)
- Timestamps more than 5 minutes in the future are rejected (clock skew protection)
- If `LNBITS_WEBHOOK_SECRET` is empty, signature verification is skipped

---

## 8. Future Endpoints (Not Yet Built)

These are planned per the build spec but not yet implemented:

| Method | Path | Description |
|---|---|---|
| `WS` | `/ws/deals/:dealID` | Real-time deal state updates |

---

## Error Responses

All error responses follow this shape:

```json
{
  "error": "descriptive message"
}
```

| Status | Meaning |
|---|---|
| `400` | Bad request — invalid body, missing fields, invalid transition |
| `401` | Unauthorized — missing or invalid token |
| `403` | Forbidden — user doesn't own this resource, or lacks operator privileges |
| `404` | Not found — deal, artifact, or verification doesn't exist |
| `500` | Internal server error |

---

## State Machine

Network-as-escrow flow (hold invoices). `awaiting_payment → work_submitted` is allowed so LND-backed LNbits (which never reports a held payment as paid) can't deadlock: the freelancer submits, the client approves, and the settle atomically proves the funds were held. Disputing **freezes** the deal in `disputed` (funds stay held, awaiting arbitration); only an arbiter moves it to terminal `released` or `refunded`.

```
                ┌──────────────────────────────┐
                │  awaiting_payment            │  ← client pays hold invoice
                └──┬───────────────────────────┘  (funds held on the network)
        paid (CLN) │            │ freelancer submits
                   ▼            ▼
              ┌────────┐  ┌──────────────────┐
              │ locked │  │  work_submitted  │
              └───┬────┘  └───┬─────────┬────┘
   freelancer     │           │ client  │ client disputes
   submits        ▼           │ checks  ▼
              ┌────────────────┐   approve │
              │ work_submitted │◄─────────┘
              └───┬────────┬───┘
          dispute │        │ approve (settle hold → released)
                  ▼        ▼
             ┌──────────┐┌──────────┐
             │ disputed ││ released │
             └────┬─────┘└──────────┘
       arbiter    │  (terminal)
       resolves   ▼
             ┌────────┐
             │refunded│  terminal
             └────────┘

  * reviewing is an optional formal phase between submitted and approve.
  * disputed = money frozen pending arbitration. `POST /disputes/:id/resolve`
    (operator only) moves it to released (settle + pay) or refunded (cancel
    hold). A client who changes their mind can still approve from disputed.
  * released is reached by client approve or an operator release resolution.
  * refunded is reached by an operator refund resolution or by the
    hold-expiry sweep for deals that were never funded.
```

`released` and `refunded` are terminal — no transitions out.

### Milestone State Machine (sequential per-milestone escrow)

```
pending → awaiting_payment → funded → in_progress → work_submitted → (completed) → released
              │                │           │               │
              ▼                ▼           ▼               ▼
           refunded         disputed ──→ released | refunded
              │                ▲           ▲
              ▼                │           │
            exited           rejected ──→ disputed
```

Full transitions (`ValidMilestoneTransitions` in `backend/internal/deals/types.go`):

- `pending` → `awaiting_payment`, `in_progress`, `exited`
- `awaiting_payment` → `funded`, `in_progress`, `exited`, `refunded`
- `funded` → `in_progress`, `exited`, `refunded`, `disputed`
- `in_progress` → `work_submitted`, `released`, `exited`, `refunded`, `disputed`
- `work_submitted` → `released`, `completed`, `rejected`, `disputed`, `exited`
- `completed` → `released`, `disputed` (`completed` = payout-pending, never paid)
- `rejected` → `disputed`, `in_progress`, `work_submitted`
- `disputed` → `released`, `refunded`
- `released`, `refunded`, `exited` terminal.

Rules: `released` is set only by the canonical payout path (`ReleaseMilestone`: settle + pay, idempotent); no-hold milestones error "fund it first" and are never marked paid. `rejected`/`disputed`/`work_submitted` → exit does not move money (hold stays, review). `GET /disputes` lists **deal-level** disputes; milestone disputes are resolved via `POST /disputes/deals/:dealID/milestones/:milestoneID/resolve`.
