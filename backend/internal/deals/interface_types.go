package deals

import (
	"context"
	"database/sql"
)

type DealRepository interface {
	// Deals
	CreateDeal(ctx context.Context, deal *Deal) error
	GetDealByID(ctx context.Context, id string) (*Deal, error)
	GetDealByCheckingID(ctx context.Context, checkingID string) (*Deal, error)
	ListByFreelancer(ctx context.Context, freelancerID string) ([]Deal, error)
	UpdateStatus(ctx context.Context, dealID string, status Status) error

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