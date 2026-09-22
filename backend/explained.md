# Database Concepts & Design Decisions

This document explains the database patterns, concurrency controls, and idempotency strategies used in the Ganji escrow/arbitration system.

---

## 1. Transaction Boundaries

### The Simple Pattern (read → write, no payout leg)

Read → write operations need explicit transactions so a `SELECT` and a later
`UPDATE` can't be interleaved by a concurrent request:

```go
tx, _ := s.repo.BeginTx(ctx)
repo := s.repo.WithTx(tx)
defer tx.Rollback()

deal, _ := repo.GetDealForUpdate(ctx, dealID)  // SELECT FOR UPDATE
// ... decision + bookkeeping ...
repo.UpdateDisputeResolution(ctx, ...)          // same transaction
tx.Commit()                                     // releases lock
```

**Why not autocommit?** Each `ExecContext`/`QueryContext` in autocommit mode
is its own transaction. Without explicit `BEGIN`, a `SELECT` and subsequent
`UPDATE` are separate transactions — another request can sneak in between them.

### The Split Pattern: Two-Phase Release (`releaseEscrow`)

The *release* paths (approve → `released`, dispute-resolve → `released`) are
deliberately NOT a single lock-around-everything transaction. Holding a
`SELECT FOR UPDATE` lock for the seconds of LNbits network I/O pins both the
row and a pooled connection, so an LNbits stall hangs every deal write.
Instead `releaseEscrow` runs two short transactions AROUND the network legs:

```
Phase 1 (own tx, locked):        Phase 2 (network, NO lock):      Phase 3 (own tx, locked):
  SELECT FOR UPDATE                SettleHold(preimage)             SELECT FOR UPDATE
  authorize the release            PayInvoice(payee_invoice)        re-validate the transition
  MarkPayoutAttempted ──► COMMIT   (each call has a 15s HTTP        record payout_checking_id
                                   timeout)                          finalize ──► COMMIT
```

- The durable `payout_attempted_at` marker is committed in Phase 1, so a
  crash during Phase 2 is visible to any later request and blocks auto-resend
  (see §3).
- The row lock is free between Phase 1 and Phase 3, so another actor may
  dispute, sweep, or re-approve in the meantime — Phase 3 therefore re-locks
  and re-validates the transition before finalizing.
- The one path that still keeps its network call inside a single locked
  transaction is the refund (`refundDispute`): `cancelEscrowHold` is provably
  idempotent (a refused cancel is verified, and "already cancelled" counts as
  success), so holding the lock across it cannot create a second or partial
  cancel.
- The expiry sweep uses a single guarded statement,
  `UpdateStatusIfCurrent(dealID, expected, refunded)`, instead of a
  lock-and-verify transaction at all (see §4).

### Deferred Rollback Pattern

```go
defer func() {
    if err != nil {
        _ = tx.Rollback()
    }
}()
```

- Runs on **every return** (including panics)
- Only rolls back if `err != nil`
- On success: `tx.Commit()` runs, `err == nil`, defer does nothing
- Prevents connection leaks and stuck locks on error paths

---

## 2. Row Locking: `SELECT FOR UPDATE`

### The Problem: Lost Updates

```
Time →
Op A:  SELECT status=disputed
Op B:  SELECT status=disputed     (same snapshot!)
Op A:  settle + pay → UPDATE released
Op B:  settle + pay → UPDATE released  ← DOUBLE PAY
```

### The Fix: Pessimistic Locking

```sql
SELECT ... FROM deals WHERE id = $1 FOR UPDATE;
```

- Acquires **row-level write lock**
- Held until **transaction commits/rolls back**
- Concurrent `SELECT FOR UPDATE` on same row **blocks** until lock released
- Second operator reads committed state (`released`) → returns error

### When Lock Is Acquired/Released

```
BEGIN                          -- transaction starts
SELECT FOR UPDATE              -- LOCK ACQUIRED here
  ... network calls (seconds)  -- LOCK HELD
UPDATE ...                     -- still same transaction
COMMIT                         -- LOCK RELEASED here
```

**Critical**: The lock must span the **entire transaction**, and every read
and write of a single logical operation must live inside it — otherwise two
callers can read the same pre-state and both proceed. This is NOT the same as
saying the lock should also cover LNbits network calls. For the payout release
the network legs run deliberately outside the lock in two short transactions
(§1); refunds keep the cancel inside the lock only because `cancelEscrowHold`
is provably idempotent.

---

## 3. Idempotency: Preventing Double-Pay

### The Risk

```
settleAndPayEscrow():
  1. SettleHold(preimage)      -- idempotent on LNbits
  2. PayInvoice(payeeInvoice)  -- NOT idempotent! sends money again
  3. UPDATE status=released
     ↑ crash here → retry → step 2 runs AGAIN
```

### Payout recovery: durable intent before network I/O

**Schema addition:**
```sql
payout_attempted_at TIMESTAMPTZ,   -- durable "attempt started" marker
payout_checking_id   TEXT          -- LNbits outgoing payment ID
```

**Flow (two-phase release, in `releaseEscrow`):**
```go
// Phase 1 — commit durable intent BEFORE any money moves (own transaction):
deal := repo.GetDealForUpdate(dealID)   // SELECT FOR UPDATE
repo.MarkPayoutAttempted(dealID)        // payout_attempted_at = NOW()
tx.Commit()

// Phase 2 — the network legs, deliberately OUTSIDE any DB lock:
SettleHold(preimage)                    // idempotent: already-settled == success
checkingID := PayInvoice(payeeInvoice)  // 4xx refusal is classified (ErrPayoutRefused)

// Phase 3 — re-lock and finalize (second transaction):
repo.GetDealForUpdate(dealID)           // re-validate the transition
repo.UpdatePayoutCheckingID(dealID, checkingID)
repo.UpdateStatus(dealID, released)
tx.Commit()
```

**Guarantees (conservative — never a second payout without proof):**
- A retry that finds `payout_checking_id` set asks LNbits to verify it: confirmed
  paid → the deal is released without re-sending; confirmed failed/refused →
  the tracking is cleared and a fresh attempt is legal.
- If the attempt is durable (`payout_attempted_at` set, `payout_checking_id`
  missing) or LNbits cannot identify the outcome, Ganji returns
  `ErrPayoutInFlight` (reconciliation required) and **does not** call
  `PayInvoice` again. A crash between Phase 2 and Phase 3 therefore leaves a
  visible, stuck deal — not a duplicate payout.
- The human backstop is the operator-only `POST /deals/:dealID/reconcile`
  endpoint:
  - `{"action":"confirm_payout","payout_checking_id":"..."}` — the operator
    verified in the LNbits UI that the freelancer was paid; after re-verifying,
    the deal is released. No money moves.
  - `{"action":"reset_payout"}` — the operator verified the earlier attempt
    never moved money; the tracking is cleared so a normal approve/release can
    run the payout again.

---

## 4. Hold Invoice Lifecycle & Sweep Job

### States

| Deal Status | LNbits Hold State | Meaning |
|-------------|-------------------|---------|
| `awaiting_payment` | `PENDING`/`UNPAID` | Invoice created, client hasn't paid |
| `locked` | `SETTLED` (held) | Client paid, funds held on network |
| `disputed` | `SETTLED` (held) | Frozen, awaiting operator |
| `released` | `SETTLED` + paid out | Funds sent to freelancer |
| `refunded` | `CANCELLED`/`EXPIRED` | Funds returned to client |

### Sweep Job: `SweepExpiredHolds`

Reconciles DB with network for stale deals:

```go
func (s *Service) SweepExpiredHolds(ctx, cutoff) {
    open := repo.ListOpenBefore(ctx, cutoff)  // awaiting_payment/locked older than cutoff
    for deal in open {
        payment, err := lnbits.CheckPayment(deal.CheckingID)
        if err != nil { continue }            // LNbits unreachable; next sweep
        switch payment.Details.Status {
        case "UNPAID", "EXPIRED", "CANCELLED":
            // Guarded single statement, no lock-and-verify transaction: the
            // deal may have moved (e.g. just approved or disputed) while the
            // sweep was deciding. UpdateStatusIfCurrent only wins if the row
            // is still exactly what this sweep read.
            if repo.UpdateStatusIfCurrent(deal.ID, deal.Status, StatusRefunded) {
                notifyRefunded(deal)
            }
        }
    }
}
```

The guard prevents a sweep from refunding a deal that a concurrent approve
already released or a client already disputed after the funds moved. LNbits
holes (unreachable, pending) are skipped rather than forced.

**Triggers:** Client never paid, or paid after hold expired (LNbits auto-returns funds).

**Runs:** Periodic cron (e.g., hourly) via worker.

---

## 5. Key Column Reference

### `deals` Table

| Column | Purpose |
|--------|---------|
| `id` (UUID PK) | Internal Ganji deal ID |
| `checking_id` | LNbits hold invoice ID (for webhook/poll lookup) |
| `payout_checking_id` | LNbits **outgoing** payment ID (idempotency) |
| `payout_attempted_at` | Durable "payout started" marker, committed **before** money moves |
| `preimage` / `preimage_hash` | Escrow secret / hash (sha256) |
| `payee_invoice` | Freelancer's BOLT11 (payout destination) |
| `share_token` | Public payment link token (rotatable) |
| `status` | Lifecycle state (CHECK constraint) |
| `dispute_reason` | Client's written reason |
| `disputed_at` | When dispute raised (queue ordering) |
| `resolved_at` / `resolved_by` | Operator verdict audit trail |
| `verified_at` | Set on `released` → CV anchoring |

### Indexes

```sql
CREATE INDEX idx_deals_freelancer_id ON deals(freelancer_id);      -- user's deals
CREATE INDEX idx_deals_preimage_hash ON deals(preimage_hash);      -- settle lookup
CREATE INDEX idx_deals_status ON deals(status);                    -- status filters
CREATE INDEX idx_deals_client_email ON deals(client_email);        -- client's deals
CREATE INDEX idx_deals_disputed_at ON deals(disputed_at);          -- arbitration queue
```

---

## 6. Arbitration Flow Summary

```
Client pays hold invoice
        │
        ▼
   Webhook/Poll → status=locked
        │
Freelancer submits work
        │
Client: Approve → settle+pay → released ✓
   OR
Client: Dispute → status=disputed (money FROZEN)
        │
        ▼
Operator views queue (ListDisputes ORDER BY disputed_at)
        │
Operator: Release → settle+pay → released (CV anchor)
   OR
Operator: Refund  → cancel hold → refunded
        │
        ▼
DB records: resolved_at, resolved_by, payout_checking_id
```

**Invariants:**
- Money **never moves** without operator (disputed) or client (approve)
- `disputed` = funds frozen on Lightning network
- Only `release`/`refund` exit `disputed` (no direct client refund)
- All state changes recorded with actor + timestamp

---

## 7. Concurrency Guarantees

| Operation | Protection |
|-----------|------------|
| `ApproveDeal` / dispute-release | Two-phase `releaseEscrow` (§1): Phase-1 `SELECT FOR UPDATE` + durable `payout_attempted_at` (committed before money moves), network legs unlock, Phase-3 re-lock + re-validated transition. Concurrent approves share the payout tracking and cannot double-pay. |
| `DisputeDeal` | `UpdateDisputeIfCurrent` — guard loses if a concurrent release already moved the deal past the state this dispute read |
| `Refund` (`refundDispute`) | Single locked transaction; `cancelEscrowHold` is provably idempotent |
| `SweepExpiredHolds` | Reads open deals, then a guarded `UpdateStatusIfCurrent(dealID, expected, refunded)` per deal; LNbits-unreachable is skipped, never forced |
| `PayInvoice` retry | Status-token verdict (§23): confirmed → release w/o resend; failed/refused → clear + retry; ambiguous → `ErrPayoutInFlight`, operator reconcile only |
| `SettleHold` retry | LNbits native idempotency (preimage-based); already-settled verified and treated as success |

---

## 8. Webhook vs Polling

| Aspect | Webhook | Polling (`CheckPayment`) |
|--------|---------|--------------------------|
| Trigger | LNbits pushes on payment | Client/frontend calls |
| Auth | HMAC signature (`LNbits-Signature`) | JWT (freelancer) |
| Idempotency | Handled by status check | Same |
| Reliability | Can miss if down | Always works (fallback) |
| Latency | Instant | Poll interval |

Both converge on: `CheckPayment` → if paid + `awaiting_payment` → `UpdateStatus(locked)`.

---

## 9. Testing Strategy

- **Unit tests**: Fake repository + mocked LNbits client (`service_test.go`)
- **HTTP contract**: LNbits client against real HTTP semantics (`client_test.go`)
- **Concurrency tests**: Fake repo's `raceMoveTo` hook simulates another actor
  moving the row mid-flight; covers the dispute guard, the sweep guard, and
  payout-verification races
- **Idempotency tests**: Simulate an ambiguous/confirmed/failed prior payout →
  machine never re-sends without proof; the operator reconcile endpoint is
  covered via `reconcile_handler_test.go`

Not yet automated: an end-to-end DB test against a real Postgres. The schema
and repository SQL were validated once against Postgres 16 (migrations +
payout tracking + guarded updates) using a throwaway driver that is not part
of the suite.

Key test files:
- `arbitration_handler_test.go` — full auth+operator middleware chain
- `reconcile_handler_test.go` — operator reconcile endpoint (`confirm_payout` / `reset_payout`)
- `service_test.go` — business logic with fake repo
- `client_test.go` — LNbits HTTP contract (4xx refusal classification)

---

## 10. Future Considerations

- **Advisory locks** for cross-process coordination (if multiple API instances)
- **Outbox pattern** for reliable webhook delivery / event publishing
- **Saga orchestration** if adding more external dependencies
- **Partitioning** `deals` by `created_at` for large datasets

---

## 11. Hold Invoice Creation

### How the Escrow Hold Invoice Is Created

When a deal is created, Ganji generates a **cryptographic preimage** (32 random bytes) and derives its **payment hash** (SHA-256). This hash locks the incoming payment — only revealing the preimage can settle it.

```go
// 1. Generate preimage (32 random bytes)
preimage := make([]byte, 32)
rand.Read(preimage)

// 2. Derive payment hash (SHA-256)
hash := sha256.Sum256(preimage)
deal.Preimage = hex.EncodeToString(preimage)
deal.PreimageHash = hex.EncodeToString(hash[:])

// 3. Create hold invoice on LNbits
hold, err := lnbits.CreateHoldInvoice(CreateHoldInvoiceRequest{
    Out:         false,           // incoming payment
    Amount:      deal.AmountSats, // sats
    Memo:        deal.Title,
    PaymentHash: deal.PreimageHash, // locks to our preimage
    Webhook:     cfg.WebhookURL,    // LNbits posts here on payment
    Expiry:      cfg.HoldExpirySec, // auto-cancel after this many seconds
})
```

**Key Points:**

| Aspect | Detail |
|--------|--------|
| **Preimage** | 32 bytes (256 bits entropy), stored as hex in DB. Never logged. |
| **Payment hash** | `sha256(preimage)` — LNbits uses this to lock the HTLC. |
| **Hold invoice** | LNbits holds funds on Lightning network. Cannot be paid to anyone until settled with preimage. |
| **Webhook** | Attached per-invoice. LNbits POSTs to `WebhookURL` when payment arrives. |
| **Expiry** | Default 30 days (2,592,000 sec). After expiry, LNbits auto-cancels, funds return to payer. |

### Preimage Lifecycle

```
CREATE DEAL
    │
    ▼
Generate preimage (32 random bytes)
    │
    ▼
hash = sha256(preimage)  ──▶ sent to LNbits as payment_hash
    │
    ▼
Store preimage (raw hex) in DB
Store preimage_hash (hex) in DB
    │
    ▼
Client pays hold invoice (locked to preimage_hash)
    │
    ▼
LNbits holds funds (HTLC locked)
    │
    ▼
OPERATOR/CLIENT DECIDES:
    ├── RELEASE → SettleHold(preimage) → funds to wallet → PayInvoice(payee)
    └── REFUND  → CancelHold(preimage_hash) → funds return to client
```

**Security:** Preimage is the **single secret** that releases funds. If leaked before dispute resolution, anyone could settle the hold. That's why:
- Never logged
- Only used in `SettleHold` call
- Deal moves to `disputed` on client complaint — freezes money until operator decides

---

## 12. Webhook Signature Verification

### LNbits Webhook Security

LNbits signs each webhook payload with HMAC-SHA256 using the wallet's `webhook_secret`. This prevents spoofed payment notifications.

**Header Format:**
```
LNbits-Signature: t=<unix_timestamp>,v1=<hex_hmac_sha256>
```

**Verification (in `internal/webhook/signature.go`):**

```go
func VerifyLNbitsSignature(payload []byte, header, secret string) bool {
    // Parse: t=1699999999,v1=abc123...
    parts := strings.Split(header, ",")
    var timestamp, signature string
    for _, p := range parts {
        if strings.HasPrefix(p, "t=") {
            timestamp = strings.TrimPrefix(p, "t=")
        }
        if strings.HasPrefix(p, "v1=") {
            signature = strings.TrimPrefix(p, "v1=")
        }
    }

    // Reject old timestamps (replay protection) — 5 min window
    ts, _ := strconv.ParseInt(timestamp, 10, 64)
    if time.Now().Unix()-ts > 300 {
        return false
    }

    // Compute expected signature: HMAC-SHA256(secret, timestamp + "." + payload)
    msg := timestamp + "." + string(payload)
    expected := hmac.New(sha256.New, []byte(secret))
    expected.Write([]byte(msg))
    expectedSig := hex.EncodeToString(expected.Sum(nil))

    // Constant-time compare
    return hmac.Equal([]byte(signature), []byte(expectedSig))
}
```

**Why This Matters:**

| Threat | Mitigation |
|--------|------------|
| Attacker forges "payment received" | Can't generate valid HMAC without `webhook_secret` |
| Replay old webhook | Timestamp check rejects >5 min old |
| Timing attack on signature compare | `hmac.Equal` constant-time comparison |

**Configuration:**
- Set `LNBITS_WEBHOOK_SECRET` in env (from LNbits wallet settings)
- If empty, webhook endpoint accepts unsigned requests (dev only)

---

## 13. Share Token Rotation

### Why Separate Token from Deal UUID?

| Problem with UUID in URL | Solution: Share Token |
|-------------------------|----------------------|
| UUID is permanent — can't revoke leaked link | Token is rotatable — `RotateShareLink` generates new one |
| UUID leaks internal DB primary key | Token is random, no correlation to internal ID |
| Can't track which link was shared | Each rotation invalidates previous token |

**Implementation:**

```go
// Creation: 32 random bytes, base64url-encoded (no padding)
func generateShareToken() (string, error) {
    b := make([]byte, 32)
    rand.Read(b)
    return base64.RawURLEncoding.EncodeToString(b), nil
}

// Storage: UNIQUE index on share_token
CREATE UNIQUE INDEX idx_deals_share_token ON deals(share_token);

// Rotation: Freelancer-only, frozen after release/refund
func (s *Service) RotateShareLink(ctx, userID, dealID) {
    // Verify ownership + deal not terminal
    token, _ := generateShareToken()
    repo.UpdateShareToken(ctx, dealID, token)
    // Old token now returns 404
}
```

**Public Deal View (`GET /public/deals/:shareToken`):**
- No auth required
- Exposes only: title, amount, invoice, status, dispute_reason, created_at
- **Never exposes:** preimage, payee_invoice, client_email, checking_id, internal UUID
- Refreshes payment status from LNbits before responding (handles missed webhooks)

---

## 14. CV Anchoring

### Live CV: Verifiable Work History

When a deal is **released**, its artifacts are anchored as verified CV entries for the freelancer.

```go
// After successful release (client approve or operator release)
func (s *Service) anchorReleasedDeal(ctx, deal *Deal) {
    if s.cv != nil {
        s.cv.AnchorReleasedDeal(ctx, deal.FreelancerID, deal.ID)
    }
}
```

**Anchor Process (in `cv` package):**
1. Fetch all artifacts for the deal
2. For each artifact, create a `Verification` record:
   - `method`: `sandbox` / `preview_pdf` / `preview_image`
   - `reference`: URL or hash of the artifact
   - `status`: `ready`
   - `expires_at`: 90 days (renewable)
3. These become **verified CV entries** on freelancer's public profile

**Public CV (`GET /cv/:slug`):**
- Shows verified work with cryptographic proofs
- Anyone can verify: artifact hash matches on-chain anchor
- Self-healing: missing anchors re-created on next read

---

## 15. Artifact Storage

### Upload/Download Flow

**Upload (Freelancer only, before work_submitted):**
```
POST /deals/:dealID/artifacts
  ├── Validate: deal status allows upload (not submitted/released/disputed)
  ├── Validate: artifact kind (source_code | source_file)
  ├── Stream to storage (size-capped at MAX_UPLOAD_BYTES)
  │   └── CountingReader enforces limit before commit
  ├── Save metadata to DB (deal_id, kind, storage_key)
  │   └── On DB failure: delete blob (best-effort cleanup)
  └── Return artifact metadata
```

**Download (Freelancer OR Client):**
```
GET /deals/:dealID/artifacts/:artifactID
  ├── Verify artifact belongs to deal
  ├── Verify requester is freelancer OR client_email match
  ├── Open storage key → stream response
  └── Set Content-Type from sanitized extension
```

**Storage Key Format:**
```
deals/<dealID>/<32-random-hex><.ext>
```
- Random prefix prevents enumeration
- Extension preserved (sanitized: alphanumeric, 2-10 chars) for Content-Type

**Size Limit:** Configurable via `MAX_UPLOAD_BYTES` (default 10MB). Enforced during stream — oversized uploads rejected before blob committed.

---

## 16. Operator Promotion

### Startup-Time Role Assignment

Operators are **not self-serve**. They're promoted via `OPERATOR_EMAILS` env var at every boot:

```go
// In router setup, runs on every startup
authService.ApplyOperatorRole(ctx, cfg.OperatorEmails)
```

**`ApplyOperatorRole` logic:**
```go
func (s *Service) ApplyOperatorRole(ctx context.Context, emails []string) error {
    for _, email := range emails {
        user, err := s.repo.GetByEmail(ctx, email)
        if err != nil {
            if errors.Is(err, ErrUserNotFound) {
                continue // user hasn't registered yet
            }
            return err
        }
        if !user.IsOperator {
            user.IsOperator = true
            if err := s.repo.Update(ctx, user); err != nil {
                return err
            }
        }
    }
    return nil
}
```

**JWT Claims:**
- On login, access token includes `is_operator` claim
- `OperatorRequired` middleware checks this claim
- Token refresh picks up new claim automatically

**Why at startup?** Survives token rotation, no manual DB edits, config-driven.

---

## 17. Status Transition Matrix

### Valid Transitions (`types.go`)

```go
var ValidTransitions = map[Status][]Status{
    StatusAwaitingPayment: {StatusLocked, StatusWorkSubmitted, StatusDisputed, StatusRefunded},
    StatusLocked:          {StatusWorkSubmitted, StatusDisputed},
    StatusWorkSubmitted:   {StatusReviewing, StatusReleased, StatusDisputed},
    StatusReviewing:       {StatusReleased, StatusDisputed},
    StatusDisputed:        {StatusReleased, StatusRefunded},
    StatusReleased:        {}, // terminal
    StatusRefunded:        {}, // terminal
}
```

### Why Each Transition Exists

| From → To | Trigger | Why Allowed |
|-----------|---------|-------------|
| `awaiting_payment` → `locked` | Webhook/poll detects payment | LNbits confirms hold paid (CLN) |
| `awaiting_payment` → `work_submitted` | Freelancer submits without payment | LND holds never report "paid" — freelancer works on trust, client approves later |
| `awaiting_payment` → `disputed` | Client disputes before payment | Client refuses to pay — freezes (no money to freeze yet) |
| `awaiting_payment` → `refunded` | Sweep job (expired hold) | Hold expired/unpaid — network already returned funds |
| `locked` → `work_submitted` | Freelancer submits work | Normal flow after payment confirmed |
| `locked` → `disputed` | Client disputes after payment | Money frozen on network, arbitration starts |
| `work_submitted` → `reviewing` | Client starts formal review | Optional phase before approve/dispute |
| `work_submitted` → `released` | Client approves | Settle hold + pay freelancer |
| `work_submitted` → `disputed` | Client disputes work | Freeze money, operator decides |
| `reviewing` → `released` | Client approves after review | Same as above |
| `reviewing` → `disputed` | Client disputes after review | Same as above |
| `disputed` → `released` | Operator releases | Settle hold + pay freelancer (CV anchor) |
| `disputed` → `refunded` | Operator refunds | Cancel hold, funds return to client |

### LND vs CLN Difference

| Backend | Hold Invoice Behavior | Ganji Handling |
|---------|----------------------|----------------|
| **CLN** (Core Lightning) | `paid=true` immediately on payment | Webhook → `awaiting_payment` → `locked` instantly |
| **LND** | `paid=false` until **settle** | Poll/webhook sees unpaid → freelancer submits → client approves → `settle` proves funds were held all along |

**Design:** `awaiting_payment` → `work_submitted` allowed specifically for LND. Freelancer submits, client approves, settle atomically proves hold was funded.

---

## 18. LNbits Error Handling

### Common Error Patterns

**LNbits Response Format:**
```json
// Success
{"ok": true, "checking_id": "abc", "payment_hash": "def", ...}

// Failure
{"ok": false, "error_message": "payment already settled"}
```

**Client Wrapper (`internal/lnbits/client.go`):**

```go
func (c *Client) SettleHold(ctx, preimage) (*SimpleInvoiceResponse, error) {
    var out SimpleInvoiceResponse
    err := c.postJSON(ctx, "/api/v1/payments/settle", c.adminKey, 
        SettleHoldRequest{Preimage: preimage}, &out)
    if err != nil {
        return &out, err
    }
    if !out.OK {
        return &out, fmt.Errorf("lnbits refused to settle: %s", out.ErrorMessage)
    }
    return &out, nil
}
```

**Key Patterns:**

| Operation | Failure Handling |
|-----------|------------------|
| `SettleHold` | If `ok:false` + "already settled" → verify via `CheckPayment` → if `SETTLED`, treat as success |
| `CancelHold` | If `ok:false` → verify via `CheckPayment` → if `UNPAID/EXPIRED/CANCELLED`, treat as success |
| `PayInvoice` | Persist intent first (`payout_attempted_at`); 4xx → `ErrPayoutRefused` (provable non-send, safe to retry later); 5xx/timeout → ambiguous → `ErrPayoutInFlight`, never auto-resend. Operator reconciles a hung attempt via `POST /deals/:dealID/reconcile`. |
| `CreateHoldInvoice` | Retry with new preimage (idempotent at Ganji level) |

**Retry Logic:** All network calls use `context.Context` for timeout/cancellation. No automatic retry — caller decides (e.g., operator retries resolution).

---

## 19. Deploy & Operations

### Required Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | ✅ | — | Postgres connection string |
| `JWT_SECRET` | ✅ | — | Access token signing key (32+ chars) |
| `JWT_REFRESH_SECRET` | ✅ | — | Refresh token signing key (32+ chars) |
| `LNBITS_URL` | ✅ | — | LNbits instance URL (e.g., `https://lnbits.example.com`) |
| `LNBITS_API_KEY` | ✅ | — | Invoice key (read/create) |
| `LNBITS_ADMIN_KEY` | ✅ | — | Admin key (settle/cancel/pay) |
| `LNBITS_WEBHOOK_SECRET` | ⚠️ | "" | Wallet webhook secret (empty = dev mode) |
| `WEBHOOK_URL` | ⚠️ | "" | Public URL for LNbits webhooks (e.g., `https://api.ganji.com/webhooks/lnbits`) |
| `PORT` | ❌ | 8080 | HTTP listen port |
| `FRONTEND_URL` | ❌ | localhost:3000 | CORS origin |
| `LNBITS_HOLD_INVOICE_EXPIRY_SECONDS` | ❌ | 2,592,000 | Hold invoice expiry (30 days) |
| `LNBITS_HOLD_SWEEP_INTERVAL_SECONDS` | ❌ | 21,600 | Sweep interval (6 hours) |
| `STORAGE_PATH` | ❌ | ./uploads | Local artifact storage path |
| `MAX_UPLOAD_BYTES` | ❌ | 10,485,760 | Max artifact size (10MB) |
| `OPERATOR_EMAILS` | ❌ | [] | Comma-separated operator emails |
| `OTS_ESPLORA_URL` | ❌ | "" | Esplora API for live OTS block-header verification (empty = offline proof check only) |
| `OTS_ESPLORA_TIMEOUT_SECONDS` | ❌ | 10 | Per-request esplora timeout |
| `OTS_UPGRADE_INTERVAL_SECONDS` | ❌ | 21,600 | OTS proof-upgrade worker interval (6 hours) |

### Migrations

```bash
# Run all pending migrations
migrate -path backend/migrations -database "$DATABASE_URL" up

# Rollback last migration
migrate -path backend/migrations -database "$DATABASE_URL" down 1
```

**Migration Rules:**
- Never edit applied migrations — create new ones
- `up.sql` + `down.sql` pairs
- Consolidate before production (as done in v000002, v000003)

### Health Checks

```bash
# Liveness (k8s)
curl http://localhost:8080/health
# {"status":"ok","db":"connected","lnbits":"connected"}

# Readiness (includes LNbits)
curl http://localhost:8080/health
```

### Graceful Shutdown

```go
// main.go: 30s grace period for in-flight requests
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
srv.Shutdown(ctx)

// Workers cancelled via context
runCancel() // stops sweep ticker
```

### Scaling Considerations

| Component | Horizontal Scale? | Notes |
|-----------|-------------------|-------|
| API server | ✅ | Stateless, shared DB |
| Sweep worker | ❌ | Single instance (cron) — use advisory lock if multiple |
| Webhook handler | ✅ | Idempotent by `checking_id` + status check |
| LNbits | External | Single wallet per Ganji instance |

---

## 20. Security Checklist

| Area | Implementation |
|------|----------------|
| **Authentication** | JWT (RS256), short-lived access (15m), rotating refresh (7d) |
| **Authorization** | Role-based (`is_operator`), ownership checks on every endpoint |
| **Rate Limiting** | `middleware.RateLimit` (configurable per-route) |
| **CORS** | Restricted to `FRONTEND_URL`, credentials allowed |
| **Input Validation** | Server-side on all handlers (email, UUID, amounts, file types) |
| **File Upload** | Size limit, extension sanitization, random storage keys, no direct serving |
| **Secrets** | Env vars only, never in code/logs, `webhook_secret` for HMAC |
| **SQL Injection** | Parameterized queries everywhere (`$1`, `$2`...) |
| **Timing Attacks** | `hmac.Equal` for signatures, constant-time email compare |
| **Replay Protection** | Webhook timestamp window (5 min), JWT `jti` claim |
| **Audit Trail** | `resolved_by`, `resolved_at`, `disputed_at`, `verified_at` on all money moves |

---

## 21. Email Notifications

### Overview

Email notifications alert freelancers of key deal events without requiring an open dashboard tab. Implemented in `internal/email/service.go`.

### Configuration

```bash
SMTP_HOST=smtp.gmail.com        # SMTP server
SMTP_PORT=587                   # TLS port
SMTP_USER=ganji@example.com     # SMTP username
SMTP_PASS=app-password          # SMTP password (app-specific)
SMTP_FROM=noreply@ganji.local   # From address
```

Set `SMTP_ENCRYPTION` to one of `starttls` (default; port 587),
`implicit_tls` (normally port 465), or `none` (local development only). SMTP
is skipped when the host or sender is absent, or when only one of username and
password is configured.

### Events & Templates

| Event | Trigger | Template | Freelancer Action |
|-------|---------|----------|-------------------|
| **Payment Received** | First durable deal → `locked` transition | Green, "View Deal" button | Submit work |
| **Dispute Raised** | Client: `DisputeDeal` | Amber, client reason shown | Wait for operator |
| **Deal Released** | Operator/Client: `release` | Green, CV anchor mentioned | Funds in wallet |
| **Deal Refunded** | Operator: `refund` | Red, no further action | Move on |

### Implementation

Notifications are injected into the deal service and fired only after the
status update has completed. The webhook uses the same notifier when it wins
the locked transition. This keeps polling, public-link refreshes, webhooks,
approvals, disputes, arbitration, and expiry sweeps consistent.

**Key design choices:**

1. **Async and bounded** — a 15-second context prevents a slow SMTP server
   from blocking an escrow transition.
2. **Best effort** — delivery failures never affect payment processing.
3. **HTML + Text** — multipart/alternative with a random MIME boundary.
4. **Frontend URL** — links point to `FRONTEND_URL/deals/{dealID}`.
5. **First name** — extracted from `display_name` for personalization.

### SMTP Details

- Uses `STARTTLS` (port 587) by default; implicit TLS is explicitly supported
  for port 465.
- Uses `smtp.PlainAuth` only when both SMTP credentials are configured.
- Multipart/alternative: `text/plain` + `text/html`
- Rejects CR/LF in mail headers and MIME-encodes non-ASCII subjects.
- 15-second end-to-end send timeout (via context).

### Testing

```bash
# With MailHog (local dev)
SMTP_HOST=localhost SMTP_PORT=1025 SMTP_ENCRYPTION=none SMTP_USER= SMTP_PASS= SMTP_FROM=test@local

# With Gmail (app password required)
SMTP_HOST=smtp.gmail.com SMTP_PORT=587 SMTP_ENCRYPTION=starttls SMTP_USER=you@gmail.com SMTP_PASS=abcd1234
```

### Future Extensions

- **Retry queue** — Persist failed emails, retry with backoff
- **Preferences** — User opt-out per event type
- **Outbox** — persist intended notifications before dispatch so delivery can
  be retried across process restarts.

## 22. Fixes

- Corrected SMTP transport selection: the default port 587 now uses STARTTLS;
  implicit TLS (465) and plaintext local development are explicit modes.
- Added bounded SMTP operations, randomized MIME boundaries, encoded subjects,
  and header newline rejection to prevent hangs, malformed messages, and
  header injection through a deal title.
- Connected dispute, release, refund, payment polling, and expiry-sweep
  transitions to the same freelancer notification service. A conditional
  `awaiting_payment → locked` update ensures only the caller that performs the
  transition sends the payment-received message.
- Fixed transaction cleanup so failed deal creation, approval, or arbitration
  always rolls back; rollback after commit is intentionally ignored.
- Serialized client approval with `SELECT FOR UPDATE` through payout tracking
  and the final release status update, preventing concurrent approval requests
  from racing the payout path.
- Fixed `scanDeal` to read nullable deal columns (`preimage`, `payee_invoice`,
  `checking_id`, `payout_checking_id`, `resolved_by`) via `sql.NullString` so
  fresh deals — which start with those cells NULL — no longer crash reads.

---

## 23. LNbits: Payout Verification & Operator Reconciliation

Status of the LNbits integration as it stands, and the items still to
research on LNbits 1.5.6.

### What has been done

**Why Ganji cannot rely on LNbits for double-pay prevention.**
LNbits has no idempotency key for outgoing payments: every
`POST /api/v1/payments` with `out:true` creates a new attempt with a new
`checking_id`. Two identical `PayInvoice` calls → two payment attempts. The
protocol only shields against re-settling the *same* invoice (a payment_hash
settles once); a retry can still land twice if the freelancer rotated their
payee invoice or the first attempt was still routing. Dedup must therefore
live in Ganji — it does, in `payout_tracking`.

**Two-phase release (`releaseEscrow`, `service.go`).**
1. **Phase 1** — own transaction, before any money moves:
   `SELECT FOR UPDATE`, authorize, `MarkPayoutAttempted`
   (`payout_attempted_at = NOW()`), commit.
2. **Phase 2** — network legs run OUTSIDE any DB lock with a bounded HTTP
   client (15s): `SettleHold(preimage)` (already-settled treated as success),
   then `PayInvoice(payee_invoice)`.
3. **Phase 3** — re-lock, re-validate the transition, record
   `payout_checking_id`, finalize to `released`, commit, anchor, notify.

A crash can no longer roll back the evidence an attempt started, so an
auto-retry can never look like a fresh approve.

**Payout verdicts (`isPayoutConfirmed` / `isPayoutFailed`, `service.go`).**
On retry, Ganji asks LNbits to check the recorded `payout_checking_id`:

| CheckPayment outcome | Token set | Action |
|----------------------|-----------|--------|
| Confirmed paid | `SETTLED`, `COMPLETE`, `SUCCEEDED`, `PAID` | Release without re-sending |
| Confirmed not-paid | `FAILED`, `UNPAID`, `CANCELLED`, `EXPIRED` | Clear tracking, safe auto-retry |
| Ambiguous (pending, error, other token, or crash-window with no id) | anything else | `ErrPayoutInFlight` — stuck, operator only |

A 4xx response from `PayInvoice` is classified as a *provable non-send*
(`ErrPayoutRefused`) — LNbits rejects before enqueueing — so tracking is
cleared and a retry is legal; 5xx/timeout are treated as ambiguous.

**Operator reconcile endpoint.** The human backstop for the stuck state:
`POST /deals/:dealID/reconcile` (operator-only).
- `{"action":"confirm_payout","payout_checking_id":"..."}` — operator verified
  in the LNbits UI that the freelancer was paid; Ganji re-verifies, records the
  real id, and releases. No money moves.
- `{"action":"reset_payout"}` — operator verified the earlier attempt never
  moved money; tracking is cleared so a normal approve/release re-runs.

### What to research / confirm on LNbits 1.5.6

Open LNbits and report back the following (see §23 “Checking", below, for how):

1. **Successful outgoing payment status.** The exact `status` string a
   successful payout reports. Expected: `COMPLETE`, `PAID`, `SUCCEEDED`, or
   `SETTLED`. If it is anything else (e.g. `SUCCESS`), add that token to
   `isPayoutConfirmed` in `service.go` — until then such deals err on the safe
   side: `ErrPayoutInFlight`, reconcilable via the endpoint.
2. **Unsuccessful outgoing payment status.** The exact string for a
   failed/expired/cancelled payout — expected inside
   `FAILED`, `UNPAID`, `CANCELLED`, `EXPIRED`.
3. **`CheckPayment` reliability for outgoing payments.** Whether the
   `paid` field of `GET /api/v1/payments/{checking_id}` is trustworthy for
   outgoing payments on the backend Ganji runs against (LND vs CLN vs
   c-lightning-REST differ). If it is not, the confirm path stays operator-
   assisted (current design already tolerates that).
4. **History lookup by BOLT11.** Whether 1.5.6 offers a payments-history list
   that could locate an outgoing attempt by exact BOLT11. This is the fumble
   alternative to operator reconciliation that the docs originally assumed; it
   was intentionally NOT automated because matching by bolt11 is unreliable
   across backends. Research only — worth revisiting if it becomes trustworthy.
5. **Webhook for outgoing payments.** Whether 1.5.6 pushes a payment-status
   webhook for outgoing payments (the current webhook design targets incoming
   hold payments). If yes, the reconcile step could eventually be automated.

**Rule of thumb for any finding:** when in doubt, LNbits reports something
unexpected → the deal sticks (never auto-resend). Stuck is deliberate:
it is visible, reconcilable, and cannot double-spend. Only ever widen an
accepted-success token after observing a real successful payout report it.

### How to check status values on LNbits

From the LNbits UI (easiest): open the payout wallet → **Payments** tab → find
a payment that completed (the outgoing arrow) and read the status column. Same
for a failed one.

From the API (wallet `X-Api-Key` from the wallet's API info page):

```bash
curl -H "X-Api-Key: <wallet-api-key>" \
  "https://<lnbits-host>/lnbits/api/v1/payments/<checking_id>"
```

Report the `status` field verbatim for a successful and a failed payout.
