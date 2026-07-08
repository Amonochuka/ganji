package interfaces

import (
	"context"

	"github.com/Amonochuka/ganji-backend/internal/deals"
)

type Repository interface {
	// Deals
	CreateDeal(ctx context.Context, deal *deals.Deal) error
	GetDealByID(ctx context.Context, id string) (*deals.Deal, error)
	ListByFreelancer(ctx context.Context, freelancerID string) ([]deals.Deal, error)
	UpdateCheckingID(ctx context.Context, dealID string, checkingID string) error
	UpdateStatus(ctx context.Context, dealID string, status deals.Status) error

	// Artifacts
	CreateArtifact(ctx context.Context, artifact *deals.Artifact) error
	GetArtifactByID(ctx context.Context, id string) (*deals.Artifact, error)
	ListArtifactsByDeal(ctx context.Context, dealID string) ([]deals.Artifact, error)
}
