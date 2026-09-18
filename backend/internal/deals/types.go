package deals

import (
	"database/sql"
	"time"
)

// Status represents the deal's position in the escrow lifecycle. These
// values must exactly match the CHECK constraint on the deals table
// (see migrations/000003_create_deals_table.up.sql) — if you add a new
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
	StatusRefunded        Status = "refunded"
)

// Deal mirrors the deals table. A deal is created before its Lightning
// hold invoice exists — CheckingID starts NULL and gets filled in by
// creation durable even if LNbits is briefly unavailable. Artifacts
// (source code, sandboxes, previews) live in their own table — a Deal
// only describes the transaction itself. Preimage holds the raw hex
// preimage (needed to settle the hold); PreimageHash is sha256(preimage)
// for verification and CV anchoring. PayeeInvoice is the freelancer's
// Lightning destination for the payout leg. PayoutCheckingID tracks the
// outgoing payout payment for idempotency (double-pay prevention).
type Deal struct {
	ID                string       `json:"id"`
	FreelancerID      string       `json:"freelancer_id"`
	ClientEmail       string       `json:"client_email"`
	Title             string       `json:"title"`
	AmountSats        int64        `json:"amount_sats"`
	SourcePlatform    string       `json:"source_platform"`
	PreimageHash      string       `json:"preimage_hash"`
	Preimage          string       `json:"preimage,omitempty"`
	PayeeInvoice      string       `json:"payee_invoice,omitempty"`
	Invoice           string       `json:"invoice"`
	CheckingID        string       `json:"checking_id"`
	PayoutCheckingID  string       `json:"payout_checking_id,omitempty"`
	ShareToken        string       `json:"share_token"`
	Status            Status       `json:"status"`
	DisputeReason     string       `json:"dispute_reason"`
	DisputedAt        sql.NullTime `json:"disputed_at"`
	ResolvedAt        sql.NullTime `json:"resolved_at"`
	ResolvedBy        string       `json:"resolved_by,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	VerifiedAt        sql.NullTime `json:"verified_at"`
}

// DisputeResolution is the operator's verdict on a disputed deal. It is the
// only thing that moves money out of the frozen 'disputed' state: "release"
// accepts the work (settle + payout to the freelancer), "refund" sends the
// held funds back to the client (cancel the hold). Neither happens without an
// operator.
type DisputeResolution string

const (
	DisputeResolutionRelease DisputeResolution = "release"
	DisputeResolutionRefund  DisputeResolution = "refund"
)

// PublicDeal is the safe view of a deal exposed on the public shareable
// link (GET /public/deals/:shareToken). It carries only what anyone with the
// link needs to pay and track the deal: the bolt11 invoice, title, amount,
// platform and status. Everything else — the internal deal id, preimage,
// preimage_hash, payee invoice, freelancer id, client email, LNbits checking
// id and the share token itself — stays private. The internal id is kept out
// of the response on purpose so a shared link never leaks the DB row handle
// (rotation would otherwise be pointless if the UUID leaked inside the body).
type PublicDeal struct {
	Title          string    `json:"title"`
	AmountSats     int64     `json:"amount_sats"`
	SourcePlatform string    `json:"source_platform"`
	Invoice        string    `json:"invoice"`
	Status         Status    `json:"status"`
	DisputeReason  string    `json:"dispute_reason"`
	CreatedAt      time.Time `json:"created_at"`
}

// ValidTransitions defines which status transitions are allowed. This is
// the enforcement point for the dispute flow design from Section 3.3 —
// nothing can jump straight from awaiting_payment to released, for
// example, and released is a terminal state with no transitions out.
//
// Network-as-escrow notes:
//   - awaiting_payment -> work_submitted is allowed so LND-backed hold
//     invoices (which never report "paid" while held) cannot deadlock:
//     the freelancer submits, the client approves, and settle atomically
//     proves the funds were held all along.
//   - dispute freezes the money: a client who disputes sends the deal to
//     'disputed' (funds stay held on the network, awaiting arbitration),
//     it does NOT refund. The client-facing states have no direct path to
//     refunded — that terminal state is reached only by arbitration
//     (disputed -> refunded) or by the hold-expiry sweep for deals that
//     were never funded.
//   - a client who changes their mind after disputing can still approve:
//     disputed -> released settles the hold and pays the freelancer.
var ValidTransitions = map[Status][]Status{
	StatusAwaitingPayment: {StatusLocked, StatusWorkSubmitted, StatusDisputed, StatusRefunded},
	// refunded stays reachable from awaiting_payment for the hold-expiry
	// sweep (expired/cancelled/unfunded holds). Clients cannot reach it.
	StatusLocked: {StatusWorkSubmitted, StatusDisputed},
	// work_submitted -> released/disputed is allowed because the client can
	// approve or dispute immediately on submission; reviewing is an optional
	// formal phase before approve/dispute.
	StatusWorkSubmitted: {StatusReviewing, StatusReleased, StatusDisputed},
	StatusReviewing:     {StatusReleased, StatusDisputed},
	StatusDisputed:      {StatusReleased, StatusRefunded},
	StatusReleased:      {}, // terminal — no transitions out
	StatusRefunded:      {}, // terminal — no transitions out
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

type Artifact struct {
	ID         string       `json:"id"`
	DealID     string       `json:"deal_id"`
	Kind       ArtifactKind `json:"kind"`
	StorageKey string       `json:"storage_key"`
	UploadedAt time.Time    `json:"uploaded_at"`
}

// Artifact types
type ArtifactKind string

const (
	ArtifactSourceCode ArtifactKind = "source_code"
	ArtifactSourceFile ArtifactKind = "source_file"
)

// Verification types
type VerificationMethod string

const (
	VerificationSandbox      VerificationMethod = "sandbox"
	VerificationPreviewPDF   VerificationMethod = "preview_pdf"
	VerificationPreviewImage VerificationMethod = "preview_image"
)

type VerificationStatus string

const (
	VerificationPending VerificationStatus = "pending"
	VerificationReady   VerificationStatus = "ready"
	VerificationExpired VerificationStatus = "expired"
)

type Verification struct {
	ID         string             `json:"id"`
	ArtifactID string             `json:"artifact_id"`
	Method     VerificationMethod `json:"method"`
	Reference  string             `json:"reference"`
	Status     VerificationStatus `json:"status"`
	ExpiresAt  sql.NullTime       `json:"expires_at"`
	CreatedAt  time.Time          `json:"created_at"`
}
