# Testing & Verification

Run everything from `backend/`:

```bash
go build ./...   # compiles
go vet ./...     # static checks
gofmt -l .       # should print nothing
go test ./...    # all packages
```

## Test Files

### `internal/deals/service_test.go`
Service-level tests using an in-memory fake `DealRepository` plus `httptest`
LNbits servers:
- Create deal → hold invoice (and orphan-hold cancel on insert failure)
- Accepted-hold verification on the escrow node: matching hash/amount,
  `State == ACCEPTED`, `settled == false`, `settle_date == 0`; a locked deal
  locks only on the accepted hold, `paid=true` alone is refused, an open hold
  stays `awaiting_payment`, checker errors surface, and a missing checker
  returns `ErrVerificationUnavailable` (503 at the handler)
- Linked-wallet funding (`FundDeal`): client wallet pays the hold, deal locks
  after the hold is accepted; unaccepted hold stays `awaiting_payment` and a
  missing checker blocks verification
- `GenerateInvoice` builds the payout invoice through the linked wallet
- Submit rules — at least one artifact and a `locked` deal; `awaiting_payment → work_submitted` is invalid
- Approve settle+payout (+ idempotent already-settled, refusal unless settled)
- Arbitration disputes (freeze funds, write reason + reject empty/overlong, no network move, non-client forbidden, invalid-transition refusals, approve-after-dispute releases)
- Operator arbitration (release = reveal R, settle + payout + anchor, refund = cancel hold with no R, idempotent already-cancelled, refusal while the hold is still committed, only-disputed, unknown verdict)
- Sweep (hold expiry refunds to the client)
- Payee-invoice rotation
- Share-link (public view safe fields, lock-on-refresh via the accepted-hold checker, LNbits-down fallback, rotation owner/frozen)

Transactions simulated via a no-op `database/sql` driver so `BeginTx/Commit` flows without a DB.

### `internal/deals/service_milestone_test.go`
Dedicated milestone tests with an `httptest` LNbits server:
- `CreateDealWithMilestones` funds M1 only; no deal/milestones persisted on
  hold-invoice or DB failure; first hold cancelled on DB failure
- Canonical `ReleaseMilestone`: settle + payout, double-call idempotency,
  confirmed payout does not re-pay, failed payout clears + retries, ambiguous
  and crash-window payouts block with `ErrPayoutInFlight`, refused payout
  clears tracking, no-escrow "fund it first", already-settled counts as success,
  disputed-during-release aborts, invalid status refused
- `ResolveMilestone`: release advances the sequence and funds the next
  milestone, refund is idempotent when the hold is gone and stays disputed
  while still held, no-escrow fails fund-first
- Exit money rules (pre-submission refund, post-submission dispute)
- Public reject → disputed without moving money, accept-next on disputed
  blocked, share-token-only auth
- `UpdateMilestoneStatus` cannot bypass money moves; transition table,
  artifact-gated submit, active-milestone mirroring

### `internal/deals/handler_arbitration_test.go`
Operator-only routes behind the **real** auth + operator middleware:
- Queue contents
- Release round-trip
- 400 on unknown verdict
- 403 for a non-operator
- 401 without a token

### `internal/auth/jwt_test.go` + `internal/middleware/auth_test.go`
- `is_operator` claim is issued and verified
- `AuthRequired` surfaces it
- `OperatorRequired` allows operators / 403s everyone else

### `internal/deals/service_artifacts_upload_test.go` / `handler_artifacts_test.go` + `internal/storage/local_test.go`
- Streaming upload/download
- Size-cap rejection leaves no blob
- Owner/party gating
- Multipart handler round-trips
- Traversal-safe storage keys
- Delete semantics

### `internal/webhook/service_test.go` / `handler_test.go`
- Payment → locked only when the LND accepted-hold checker confirms the hold
  (matching hash/amount, `ACCEPTED`, not settled)
- `paid=true` with no accepted hold is rejected; unpaid stays unlocked
- Missing accepted-hold checker → `ErrVerificationUnavailable`; LNbits errors
  are wrapped; missing checking ID / missing deal / already-locked cases
- Bad signature
- HMAC verification

### `internal/cv/service_test.go`
- Profile read self-heals missing anchors and derives trust score
- Verify matches / detects tampered hashes
- Foreign-slug lookup is hidden (404)
- Handler status codes
- Approve→anchor hook (including anchoring failing without blocking the release)

### `internal/lnbits/client_test.go`
- Client request shapes and key selection

### `internal/ots/verify_test.go` + `client_test.go`
- OTS proof verification against real test fixture (Bitcoin block 891686)
- Client submission and upgrade protocol