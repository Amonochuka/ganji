package deals

import (
	"context"
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
		return fmt.Errorf("%w: freelancer id is required", ErrInvalidInput)
	}

	if deal.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	}

	if deal.AmountSats <= 0 {
		return fmt.Errorf("%w: amount must be greater than zero", ErrInvalidInput)
	}

	if deal.SourcePlatform == "" {
		return fmt.Errorf("%w: source platform is required", ErrInvalidInput)
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
		return nil, fmt.Errorf("%w: freelancer id is required", ErrInvalidInput)
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
func (s *Service) CreateArtifact(ctx context.Context, userID string, artifact *Artifact) error {
	if artifact.DealID == "" {
		return fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	if artifact.StorageKey == "" {
		return fmt.Errorf("%w: storage key is required", ErrInvalidInput)
	}

	switch artifact.Kind {
	case ArtifactSourceCode, ArtifactSourceFile:
	default:
		return fmt.Errorf("%w: invalid artifact kind", ErrInvalidInput)
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
		return fmt.Errorf("%w: uploads are no longer allowed", ErrInvalidInput)
	default:
		return fmt.Errorf("%w: deal is not ready for uploads", ErrInvalidInput)
	}

	return s.repo.CreateArtifact(ctx, artifact)
}

func (s *Service) GetArtifactByID(ctx context.Context, userID, artifactID string) (*Artifact, error) {
	if artifactID == "" {
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
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
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
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
		return fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	if verification.Reference == "" {
		return fmt.Errorf("%w: reference is required", ErrInvalidInput)
	}

	switch verification.Method {
	case VerificationSandbox,
		VerificationPreviewPDF,
		VerificationPreviewImage:
	default:
		return fmt.Errorf("%w: invalid verification method", ErrInvalidInput)
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
		return nil, fmt.Errorf("%w: verification id is required", ErrInvalidInput)
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
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
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