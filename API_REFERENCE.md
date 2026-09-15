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

All deal endpoints require `Authorization: Bearer <access_token>`.

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
    "status": "awaiting_payment",
    "created_at": "2026-08-26T12:00:00Z",
    "verified_at": null
  }
}
```

The raw `preimage` (the network secret that can settle the escrow) and the freelancer's `payee_invoice` are stored server-side but intentionally **omitted** from API responses.

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

Client disputes → the hold is cancelled on the network (sats return to the client) and the deal is recorded **`refunded`** (terminal). Disputing before payment, or a hold that already expired/cancelled, still records the refunded state (nothing was held). If the cancel fails while funds are still held — or the escrow was already settled — the deal is **not** marked refunded and the money needs operator handling.

**Response `200`**
```json
{
  "deal": {
    "id": "uuid",
    "status": "refunded",
    ...
  }
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

Register an artifact (source code or file) with a storage key.

**Request**
```json
{
  "kind": "source_code",
  "storage_key": "s3://bucket/key"
}
```

`kind` must be one of: `"source_code"`, `"source_file"`.

**Response `201`**
```json
{
  "artifact": {
    "id": "uuid",
    "deal_id": "uuid",
    "kind": "source_code",
    "storage_key": "s3://bucket/key",
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

## 6. Webhooks

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

## 7. Future Endpoints (Not Yet Built)

These are planned per the build spec but not yet implemented:

| Method | Path | Description |
|---|---|---|
| `GET` | `/cv/:slug` | Public freelancer Live CV page |
| `GET` | `/cv/:slug/verify/:entryID` | Verify a CV entry's hash |
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
| `403` | Forbidden — user doesn't own this resource |
| `404` | Not found — deal, artifact, or verification doesn't exist |
| `500` | Internal server error |

---

## State Machine

Network-as-escrow flow (hold invoices). `awaiting_payment → work_submitted` is allowed so LND-backed LNbits (which never reports a held payment as paid) can't deadlock: the freelancer submits, the client approves, and the settle atomically proves the funds were held. `refunded` — reached by dispute cancelling the hold — is terminal, as is `released`.

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
             ┌────────┐┌──────────┐
             │refunded││ released │   terminal ×2
             └────────┘└──────────┘

  * reviewing is an optional formal phase between submitted and approve.
  * disputed is a reserved arbitration state (future); today DisputeDeal
    goes straight to refunded via a network cancel.
```

`released` and `refunded` are terminal — no transitions out.
