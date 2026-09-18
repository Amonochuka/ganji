# Database Concepts & Design Decisions

This document explains the database patterns, concurrency controls, and idempotency strategies used in the Ganji escrow/arbitration system.

---

## 1. Transaction Boundaries

### Explicit Transactions for Multi-Step Operations

Operations that span **read → network call → write** must run in a single transaction to prevent races:

```go
func (s *Service) ResolveDispute(...) {
    tx, _ := s.repo.BeginTx(ctx)
    repo := s.repo.WithTx(tx)
    defer func() { if err != nil { _ = tx.Rollback() } }()

    deal, _ := repo.GetDealForUpdate(ctx, dealID)  // SELECT FOR UPDATE
    // ... network calls (LNbits) ...
    repo.UpdateDisputeResolution(ctx, ...)          // same transaction
    tx.Commit()                                     // releases lock
}
```

**Why not autocommit?** Each `ExecContext`/`QueryContext` in autocommit mode is its own transaction. Without explicit `BEGIN`, a `SELECT` and subsequent `UPDATE` are separate transactions — another request can sneak in between them.

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

**Critical**: The lock must span the **entire operation**, including external API calls. That's why we use explicit `BEGIN`/`COMMIT` wrapping everything.

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

### Solution: Track Payout with `payout_checking_id`

**Schema addition:**
```sql
ALTER TABLE deals ADD COLUMN payout_checking_id TEXT;
CREATE INDEX idx_deals_disputed_at ON deals(disputed_at);
```

**Flow:**
```go
func settleAndPayEscrow(ctx, deal, repo) {
    // 1. Settle hold (idempotent)
    SettleHold(preimage)

    // 2. Check if payout already recorded
    if deal.PayoutCheckingID != "" {
        payment := CheckPayment(deal.PayoutCheckingID)
        if payment.Status == "SETTLED" {
            return nil  // already paid, skip
        }
        // else: stale ID, clear and retry below
        repo.UpdatePayoutCheckingID(deal.ID, "")
    }

    // 3. Send payout, IMMEDIATELY record checking_id
    payoutCheckingID := PayInvoice(payeeInvoice)
    repo.UpdatePayoutCheckingID(deal.ID, payoutCheckingID)  // part of same tx
}
```

**Guarantees:**
- If crash after `PayInvoice` but before DB update: `payout_checking_id` exists in LNbits, next retry verifies it
- If crash before `PayInvoice`: no `checking_id` recorded, safe to retry
- `UpdatePayoutCheckingID` uses the **transaction repo** → atomic with final status update

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
        payment := lnbits.CheckPayment(deal.CheckingID)
        switch payment.Status {
        case "UNPAID", "EXPIRED", "CANCELLED":
            repo.UpdateStatus(deal.ID, StatusRefunded)  // network already returned funds
        }
    }
}
```

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
| `ResolveDispute` | `SELECT FOR UPDATE` in explicit transaction |
| `ApproveDeal` | Single-threaded per deal (client-only, no concurrent approve) |
| `DisputeDeal` | Client-only, status check prevents double-dispute |
| `SweepExpiredHolds` | Idempotent: only touches `awaiting_payment`/`locked` |
| `PayInvoice` retry | `payout_checking_id` verification |
| `SettleHold` retry | LNbits native idempotency (preimage-based) |

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

- **Unit tests**: Fake repository + mocked LNbits client
- **Integration tests**: Real Postgres (testcontainers) + real LNbits (testnet)
- **Concurrency tests**: Goroutine races on `ResolveDispute` to verify locking
- **Idempotency tests**: Simulate crash after `PayInvoice` → verify no double-pay

Key test files:
- `arbitration_handler_test.go` — full auth+operator middleware chain
- `service_test.go` — business logic with fake repo
- `client_test.go` — LNbits HTTP contract

---

## 10. Future Considerations

- **Advisory locks** for cross-process coordination (if multiple API instances)
- **Outbox pattern** for reliable webhook delivery / event publishing
- **Saga orchestration** if adding more external dependencies
- **Partitioning** `deals` by `created_at` for large datasets