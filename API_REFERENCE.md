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

Create a new deal. Generates a preimage hash, creates a LNbits invoice, and stores the deal.

**Request**
```json
{
  "title": "Landing page redesign",
  "amount_sats": 50000,
  "source_platform": "Telegram"
}
```

**Response `201`**
```json
{
  "deal": {
    "id": "uuid",
    "freelancer_id": "uuid",
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

Poll LNbits for payment status. If the invoice has been paid and the deal is still `awaiting_payment`, it transitions to `locked`.

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

### `PATCH /deals/:dealID/status`

Transition deal status. Validates against the state machine.

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

**Valid transitions:**
```
awaiting_payment → locked
locked → work_submitted
work_submitted → reviewing
reviewing → released | disputed
disputed → released
released → (terminal)
```

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
| `POST` | `/deals/:dealID/submit` | Freelancer submits work (transitions to `work_submitted`) |
| `POST` | `/deals/:dealID/approve` | Client approves release |
| `POST` | `/deals/:dealID/dispute` | Client raises dispute |

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

```
                  ┌──────────────────┐
                  │ awaiting_payment │
                  └────────┬─────────┘
                           │ payment received (webhook or poll)
                           ▼
                  ┌────────────────┐
                  │     locked     │
                  └────────┬───────┘
                           │ freelancer submits work
                           ▼
                  ┌──────────────────┐
                  │  work_submitted  │
                  └────────┬─────────┘
                           │ client begins review
                           ▼
                  ┌────────────────┐
                  │    reviewing    │
                  └───┬────────┬───┘
                      │        │
               approve│        │dispute
                      ▼        ▼
            ┌──────────┐  ┌──────────┐
            │ released │  │ disputed │
            └──────────┘  └────┬─────┘
                               │ arbiter resolves
                               ▼
                        ┌──────────┐
                        │ released │
                        └──────────┘
```

`released` is terminal — no transitions out.
