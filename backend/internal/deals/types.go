package deals

import (
	"database/sql"
	"time"
)

// Status represents the deal's position in the escrow lifecycle. These
// values must exactly match the CHECK constraint on the deals table
// (see migrations/000002_create_deals_table.up.sql) — if you add a new
// status here, you must also update that constraint, or inserts using
// the new status will be rejected by Postgres.
type Status string

const (
	StatusAwaitingPayment Status = "awaiting_payment"
	StatusLocked          Status = "locked"
	StatusWorkSubmitted   Status = "work_submitted"
	StatusReviewing       Status = "reviewing"
	StatusReleased        Status = "released"
	StatusDisputed        Status = "disputed"
)

// Deal mirrors the deals table. A deal is created before its Lightning
// invoice exists — CheckingID starts NULL and gets filled in by
// Repository.UpdateCheckingID once LNbits responds. This keeps deal
// creation durable even if LNbits is briefly unavailable. Artifacts
// (source code, sandboxes, previews) live in their own table — a Deal
// only describes the transaction itself.
type Deal struct {
	ID             string         `json:"id"`
	FreelancerID   string         `json:"freelancer_id"`
	Title          string         `json:"title"`
	AmountSats     int64          `json:"amount_sats"`
	SourcePlatform string         `json:"source_platform"`
	PreimageHash   string         `json:"preimage_hash"`
	Invoice        string         `json:"invoice"`
	CheckingID     sql.NullString `json:"checking_id"`
	Status         Status         `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	VerifiedAt     sql.NullTime   `json:"verified_at"`
}

// ValidTransitions defines which status transitions are allowed. This is
// the enforcement point for the dispute flow design from Section 3.3 —
// nothing can jump straight from awaiting_payment to released, for
// example, and released is a terminal state with no transitions out.
var ValidTransitions = map[Status][]Status{
	StatusAwaitingPayment: {StatusLocked},
	StatusLocked:          {StatusWorkSubmitted},
	StatusWorkSubmitted:   {StatusReviewing},
	StatusReviewing:       {StatusReleased, StatusDisputed},
	StatusDisputed:        {StatusReleased},
	StatusReleased:        {}, // terminal — no transitions out
}

// CanTransition checks whether moving from one status to another is a
// legal transition according to the deal lifecycle.
func CanTransition(from, to Status) bool {
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
