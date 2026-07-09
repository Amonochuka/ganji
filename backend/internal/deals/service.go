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

func (s *Service) GetDealByID(ctx context.Context, dealID, userID string) (*Deal, error) {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	return deal, nil
}

func (s *Service) ListByFreelancer(ctx context.Context, userID string) ([]Deal, error) {
	if userID == "" {
		return nil, errors.New("freelancer id is required")
	}

	return s.repo.ListByFreelancer(ctx, userID)
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
// Artifacts

func (s *Service) CreateArtifact(ctx context.Context, userID string, artifact *Artifact) error {
	if artifact.DealID == "" {
		return errors.New("deal id is required")
	}
	if artifact.StorageKey == "" {
		return errors.New("storage key is required")
	}

	switch artifact.Kind {
	case ArtifactSourceCode, ArtifactSourceFile:
	default:
		return errors.New("invalid artifact kind")
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return err
	}

	if deal.FreelancerID != userID {
		return ErrForbidden
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

func (s *Service) GetArtifactByID(ctx context.Context, userID, artifactID string) (*Artifact, error) {
	if artifactID == "" {
		return nil, errors.New("artifact id is required")
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	return artifact, nil
}

func (s *Service) ListArtifactsByDeal(ctx context.Context, userID, dealID string) ([]Artifact, error) {
	if dealID == "" {
		return nil, errors.New("deal id is required")
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	return s.repo.ListArtifactsByDeal(ctx, dealID)
}

// Verifications

func (s *Service) CreateVerification(ctx context.Context, userID string, verification *Verification) error {
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

	artifact, err := s.repo.GetArtifactByID(ctx, verification.ArtifactID)
	if err != nil {
		return err
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return err
	}

	if deal.FreelancerID != userID {
		return ErrForbidden
	}

	if verification.Status == "" {
		verification.Status = VerificationPending
	}

	return s.repo.CreateVerification(ctx, verification)
}

func (s *Service) GetVerificationByID(ctx context.Context, userID, verificationID string) (*Verification, error) {
	if verificationID == "" {
		return nil, errors.New("verification id is required")
	}

	verification, err := s.repo.GetVerificationByID(ctx, verificationID)
	if err != nil {
		return nil, err
	}

	artifact, err := s.repo.GetArtifactByID(ctx, verification.ArtifactID)
	if err != nil {
		return nil, err
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	return verification, nil
}

func (s *Service) ListVerificationsByArtifact(ctx context.Context, userID, artifactID string) ([]Verification, error) {
	if artifactID == "" {
		return nil, errors.New("artifact id is required")
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	return s.repo.ListVerificationsByArtifact(ctx, artifactID)
}
