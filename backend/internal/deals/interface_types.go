package deals

import (
	"context"
	"database/sql"
	"time"
)

type DealRepository interface {
	// Deals
	CreateDeal(ctx context.Context, deal *Deal) error
	GetDealByID(ctx context.Context, id string) (*Deal, error)
	GetDealByCheckingID(ctx context.Context, checkingID string) (*Deal, error)
	GetDealByShareToken(ctx context.Context, shareToken string) (*Deal, error)
	ListByFreelancer(ctx context.Context, freelancerID string) ([]Deal, error)
	ListForUser(ctx context.Context, userID, email string) ([]Deal, error)
	UpdateStatus(ctx context.Context, dealID string, status Status) error
	// UpdateStatusIfCurrent atomically changes a status only when the row is
	// still in expected. The boolean reports whether this caller won the
	// transition, which is essential for one-time side effects such as email.
	UpdateStatusIfCurrent(ctx context.Context, dealID string, expected, status Status) (bool, error)
	UpdateDispute(ctx context.Context, dealID, reason string) error
	UpdateDisputeResolution(ctx context.Context, dealID string, status Status, resolvedBy string) error
	GetDealForUpdate(ctx context.Context, dealID string) (*Deal, error)
	UpdatePayoutCheckingID(ctx context.Context, dealID, payoutCheckingID string) error
	UpdatePayeeInvoice(ctx context.Context, dealID, payeeInvoice string) error
	UpdateShareToken(ctx context.Context, dealID, shareToken string) error
	ListOpenBefore(ctx context.Context, cutoff time.Time) ([]Deal, error)
	ListDisputed(ctx context.Context) ([]Deal, error)

	// Artifacts
	CreateArtifact(ctx context.Context, artifact *Artifact) error
	GetArtifactByID(ctx context.Context, id string) (*Artifact, error)
	ListArtifactsByDeal(ctx context.Context, dealID string) ([]Artifact, error)

	// Verifications
	CreateVerification(ctx context.Context, verification *Verification) error
	GetVerificationByID(ctx context.Context, id string) (*Verification, error)
	ListVerificationsByArtifact(ctx context.Context, artifactID string) ([]Verification, error)

	// Transactions
	BeginTx(ctx context.Context) (*sql.Tx, error)
	WithTx(tx *sql.Tx) DealRepository
}
