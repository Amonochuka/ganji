package deals

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{
		repo: repo,
	}
}

// Deals
func (s *Service) CreateDeal(ctx context.Context, deal *Deal) error {
	deal.Title = strings.TrimSpace(deal.Title)
	deal.SourcePlatform = strings.TrimSpace(deal.SourcePlatform)

	if deal.FreelancerID == "" {
		return errors.New("freelancer id is required")
	}

	if deal.Title == "" {
		return errors.New("title is required")
	}

	if deal.AmountSats <= 0 {
		return errors.New("amount must be greater than zero")
	}

	if deal.SourcePlatform == "" {
		return errors.New("source platform is required")
	}

	if deal.Status == "" {
		deal.Status = StatusAwaitingPayment
	}

	return s.repo.CreateDeal(ctx, deal)
}

func (s *Service) GetDealByID(ctx context.Context, id string) (*Deal, error) {
	if id == "" {
		return nil, errors.New("deal id is required")
	}
	return s.repo.GetDealByID(ctx, id)
}

func (s *Service) ListByFreelancer(ctx context.Context, freelancerID string) ([]Deal, error) {
	if freelancerID == "" {
		return nil, errors.New("freelancer id is required")
	}
	return s.repo.ListByFreelancer(ctx, freelancerID)
}

func (s *Service) UpdateStatus(ctx context.Context, dealID string, newStatus Status) error {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return err
	}
	if !CanTransition(deal.Status, newStatus) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, newStatus)
	}
	return s.repo.UpdateStatus(ctx, dealID, newStatus)
}

// Artifacts
func (s *Service) CreateArtifact(ctx context.Context, artifact *Artifact) error {
	if artifact.DealID == "" {
		return errors.New("deal id is required")
	}

	if artifact.StorageKey == "" {
		return errors.New("storage key is required")
	}

	switch artifact.Kind {
	case ArtifactSourceCode,
		ArtifactSourceFile:

	default:
		return errors.New("invalid artifact kind")
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return err
	}

	switch deal.Status {
	case StatusLocked:
		// uploads allowed
	case StatusWorkSubmitted,
		StatusReviewing,
		StatusReleased,
		StatusDisputed:
		return errors.New("uploads are no longer allowed")
	default:
		return errors.New("deal is not ready for uploads")
	}
	return s.repo.CreateArtifact(ctx, artifact)
}

func (s *Service) GetArtifactByID(ctx context.Context, id string) (*Artifact, error) {
	if id == "" {
		return nil, errors.New("artifact id is required")
	}
	return s.repo.GetArtifactByID(ctx, id)
}

func (s *Service) ListArtifactsByDeal(ctx context.Context, dealID string) ([]Artifact, error) {
	if dealID == "" {
		return nil, errors.New("deal id is required")
	}
	return s.repo.ListArtifactsByDeal(ctx, dealID)
}

//verifications
func (s *Service) CreateVerification(ctx context.Context, verification *Verification) error {
	if verification.ArtifactID == "" {
		return errors.New("artifact id is required")
	}
	if verification.Reference == "" {
		return errors.New("reference is required")
	}
	
	switch verification.Method {
	case VerificationSandbox,
		VerificationPreviewPDF,
		VerificationPreviewImage:

	default:
		return errors.New("invalid verification method")
	}

	if verification.Status == "" {
		verification.Status = VerificationPending
	}
	return s.repo.CreateVerification(ctx, verification)
}

func (s *Service) GetVerificationByID(ctx context.Context, id string) (*Verification, error) {
	if id == "" {
		return nil, errors.New("verification id is required")
	}
	return s.repo.GetVerificationByID(ctx, id)
}

func (s *Service) ListVerificationsByArtifact(ctx context.Context, artifactID string) ([]Verification, error) {
	if artifactID == "" {
		return nil, errors.New("artifact id is required")
	}
	return s.repo.ListVerificationsByArtifact(ctx, artifactID)
}