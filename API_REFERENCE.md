# Ganji Backend — API Reference

**Base URL:** `http://localhost:8080`
**Auth:** JWT Bearer token in `Authorization` header (except where noted)

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

---

## 3. Deals

All deal endpoints require `Authorization: Bearer <access_token>` **except the public shareable link endpoint** (`GET /public/deals/:shareToken`), which is intentionally unauthenticated so the freelancer can share a link the client can open without an account.

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
    "created_at": "2026-08-26T12:00:00Z"
  }
}
```

**Response `404`**
```json
{
  "error": "deal not found"
}
```

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

**Request**
```json
{
  "title": "Landing page redesign",
  "amount_sats": 50000,
  "source_platform": "Telegram",
  "client_email": "client@example.com",
  "payee_invoice": "lnbc50000n1..."
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

Get a single deal. Ownership enforced.

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

**Response `200`**
```json
{
  "deal": { ... }
}
```

### `POST /deals/:dealID/approve`

Client approves → money moves on the network in two legs: `settle` the hold (reveal the stored preimage, funds land in the Ganji wallet) then `pay` the freelancer's `payee_invoice`. Idempotent: re-approving an already-settled deal pays out without double-settling. On success the deal is `released` and `verified_at` is stamped (Live CV entry).

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

The **money states** (\`locked\`, \`released\`, \`disputed\`, \`refunded\`) are backend-only: they are set by payment detection, approve, and dispute, never by this endpoint.

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
| `kind` — `"source_code"` or `"source_file"` (required) |
| `artifact` — the file itself (required) |

Uploads larger than `MAX_UPLOAD_BYTES` (default 10 MB) are rejected with `400`
and no blob is committed.

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
per artifact; see the release flow in section 3). Both endpoints are public —
the slug is a shareable handle, not a secret.

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
    "verified_at": "2026-08-26T12:00:00Z"
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
| `500` | Unexpected internal failure | `{"error": "internal failure"}` |

Payment-not-successful returns `200` because the webhook was received and understood — nothing for Ganji to do. Returning non-2xx would cause LNbits to retry unnecessarily.

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
| `GET` | `/disputes` | Arbitration queue — list disputed deals (operator only) |
| `POST` | `/disputes/:dealID/resolve` | Arbiter resolves a dispute → `released` (settle+payout) or `refunded` (cancel hold); requires an operator/admin role (not built yet) |

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
| `403` | Forbidden — user doesn't own this resource |
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
  * disputed = money frozen pending arbitration; a client who changes their
    mind can still approve from disputed.
  * refunded is reached by arbitration (disputed → refunded) or by the
    hold-expiry sweep for deals that were never funded.
```

`released` and `refunded` are terminal — no transitions out.
