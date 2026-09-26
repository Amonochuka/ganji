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
- Create deal → hold invoice
- Submit rules
- Approve settle+payout (+ idempotent already-settled, refusal unless settled)
- Arbitration disputes (freeze funds, write reason + reject empty/overlong, no network move, non-client forbidden, invalid-transition refusals, approve-after-dispute releases)
- Operator arbitration (release = settle+payout+anchor, refund = cancel hold, idempotent already-cancelled, refusal while the hold is still committed, only-disputed, unknown verdict)
- Sweep
- Payee-invoice rotation
- Share-link (public view safe fields, lock-on-refresh, LNbits-down fallback, rotation owner/frozen)

Transactions simulated via a no-op `database/sql` driver so `BeginTx/Commit` flows without a DB.

### `internal/deals/arbitration_handler_test.go`
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

### `internal/deals/artifact_upload_test.go` / `artifact_handler_test.go` + `internal/storage/local_test.go`
- Streaming upload/download
- Size-cap rejection leaves no blob
- Owner/party gating
- Multipart handler round-trips
- Traversal-safe storage keys
- Delete semantics

### `internal/webhook/service_test.go` / `handler_test.go`
- Payment → locked
- Unpaid ignored
- Missing deal
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