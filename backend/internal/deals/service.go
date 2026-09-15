package deals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strings"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

type Service struct {
	repo   DealRepository
	lnbits *lnbits.Client
}

func NewService(repo DealRepository, lnbits *lnbits.Client) *Service {
	return &Service{
		repo:   repo,
		lnbits: lnbits,
	}
}

func (s *Service) CreateDeal(ctx context.Context, deal *Deal) error {
	deal.Title = strings.TrimSpace(deal.Title)
	deal.SourcePlatform = strings.TrimSpace(deal.SourcePlatform)
	deal.PayeeInvoice = strings.TrimSpace(deal.PayeeInvoice)

	if deal.FreelancerID == "" {
		return fmt.Errorf("%w: freelancer id is required", ErrInvalidInput)
	}

	deal.ClientEmail = strings.ToLower(strings.TrimSpace(deal.ClientEmail))
	if !isValidEmail(deal.ClientEmail) {
		return fmt.Errorf("%w: valid client email is required", ErrInvalidInput)
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

	if deal.PayeeInvoice == "" {
		return fmt.Errorf("%w: payee invoice is required", ErrInvalidInput)
	}

	if deal.Status == "" {
		deal.Status = StatusAwaitingPayment
	}

	// Generate the preimage that controls the escrow. LNbits holds the
	// payment against sha256(preimage); only a successful settle (reveal)
	// releases it to the freelancer, and only a cancel returns it to the
	// client. We keep the raw preimage in the DB because LNbits's settle
	// endpoint takes the preimage body.
	preimage := make([]byte, 32)
	if _, err := rand.Read(preimage); err != nil {
		return fmt.Errorf("generating escrow preimage: %w", err)
	}

	hash := sha256.Sum256(preimage)
	deal.Preimage = hex.EncodeToString(preimage[:])
	deal.PreimageHash = hex.EncodeToString(hash[:])

	hold, err := s.lnbits.CreateHoldInvoice(ctx, lnbits.CreateHoldInvoiceRequest{
		Out:         false,
		Amount:      deal.AmountSats,
		Memo:        deal.Title,
		PaymentHash: deal.PreimageHash,
	})
	if err != nil {
		return fmt.Errorf("creating LNBits hold invoice: %w", err)
	}

	deal.Invoice = hold.PaymentRequest
	deal.CheckingID = hold.CheckingID

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	repo := s.repo.WithTx(tx)

	if err := repo.CreateDeal(ctx, deal); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *Service) GetDealByID(ctx context.Context, dealID, userID, email string) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID && !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	return deal, nil
}

func (s *Service) ListByUser(ctx context.Context, userID, email string) ([]Deal, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id is required", ErrInvalidInput)
	}

	return s.repo.ListForUser(ctx, userID, email)
}

func (s *Service) UpdateStatus(ctx context.Context, userID, dealID string, newStatus Status) error {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return err
	}

	if deal.FreelancerID != userID {
		return ErrForbidden
	}

	// locked, released, disputed and refunded are money moves that must be
	// driven by the backend — payment detection (webhook/poll), ApproveDeal,
	// or DisputeDeal — never by the freelancer's generic status endpoint.
	// Under the hold-invoice escrow, a freelancer self-marking a deal
	// "locked" (or worse "refunded") would be able to fake that escrow
	// funds are committed.
	switch newStatus {
	case StatusLocked, StatusReleased, StatusDisputed, StatusRefunded:
		return fmt.Errorf("%w: %s is a backend-only money transition (payment detection, approve, dispute)", ErrInvalidTransition, newStatus)
	}

	if !CanTransition(deal.Status, newStatus) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, newStatus)
	}
	return s.repo.UpdateStatus(ctx, dealID, newStatus)
}

// SubmitWork moves a locked deal to work_submitted. Only the freelancer
// (the deal owner) can submit, and the deal must already have at least one
// artifact — you cannot submit nothing.
func (s *Service) SubmitWork(ctx context.Context, userID, dealID string) (*Deal, error) {
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

	artifacts, err := s.repo.ListArtifactsByDeal(ctx, dealID)
	if err != nil {
		return nil, fmt.Errorf("checking submitted artifacts: %w", err)
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("%w: submit requires at least one artifact", ErrInvalidInput)
	}

	if !CanTransition(deal.Status, StatusWorkSubmitted) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusWorkSubmitted)
	}

	if err := s.repo.UpdateStatus(ctx, dealID, StatusWorkSubmitted); err != nil {
		return nil, fmt.Errorf("submitting work for deal %s: %w", dealID, err)
	}

	deal.Status = StatusWorkSubmitted
	return deal, nil
}

// ApproveDeal releases escrow to the freelancer. Only the client (matched
// by the client_email recorded on the deal) can approve. Because released
// is a terminal state, approving also stamps verified_at for the Live CV.
func (s *Service) ApproveDeal(ctx context.Context, email, dealID string) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	if !CanTransition(deal.Status, StatusReleased) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusReleased)
	}

	if err := s.repo.UpdateStatus(ctx, dealID, StatusReleased); err != nil {
		return nil, fmt.Errorf("releasing escrow for deal %s: %w", dealID, err)
	}

	deal.Status = StatusReleased
	return deal, nil
}

// DisputeDeal raises a dispute. Only the client can dispute.
func (s *Service) DisputeDeal(ctx context.Context, email, dealID string) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	if !CanTransition(deal.Status, StatusDisputed) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusDisputed)
	}

	if err := s.repo.UpdateStatus(ctx, dealID, StatusDisputed); err != nil {
		return nil, fmt.Errorf("disputing deal %s: %w", dealID, err)
	}

	deal.Status = StatusDisputed
	return deal, nil
}

// CheckPayment queries LNbits for the payment status of a deal's hold
// invoice. If the payment is confirmed paid (held on the network under the
// payment hash), the deal transitions from awaiting_payment to locked. The
// caller should never be trusted to set the status directly — the backend
// determines it from LNbits.
//
// Backend autodetection: on CLN-backed LNbits a held invoice already
// reports paid=true, so this locks as soon as the client pays. On
// LND-backed LNbits a held invoice stays unpaid (paid=false) until it is
// settled, so the deal remains awaiting_payment and only moves on approve —
// the settle itself then atomically proves the funds were held.
func (s *Service) CheckPayment(ctx context.Context, userID, dealID string) (*Deal, error) {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	if deal.CheckingID == "" {
		return nil, ErrNoCheckingID
	}

	payment, err := s.lnbits.CheckPayment(ctx, deal.CheckingID)
	if err != nil {
		return nil, fmt.Errorf("checking payment with lnbits: %w", err)
	}

	if payment.Paid && deal.Status == StatusAwaitingPayment {
		if err := s.repo.UpdateStatus(ctx, dealID, StatusLocked); err != nil {
			return nil, fmt.Errorf("updating deal status after payment: %w", err)
		}
		deal.Status = StatusLocked
	}

	return deal, nil
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

	// TEMP: Allow uploads before LNBits escrow integration.
	// Remove this once deals automatically transition to StatusLocked after payment.
	switch deal.Status {
	case StatusWorkSubmitted,
		StatusReviewing,
		StatusReleased,
		StatusDisputed:
		return fmt.Errorf("%w: uploads are no longer allowed", ErrInvalidInput)
	}

	return s.repo.CreateArtifact(ctx, artifact)
}

func (s *Service) GetArtifactByID(
	ctx context.Context, userID, dealID, artifactID string) (*Artifact, error) {

	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	if artifactID == "" {
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	if artifact.DealID != dealID {
		return nil, ErrArtifactNotFound
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

func (s *Service) GetVerificationByID(
	ctx context.Context, userID, dealID, artifactID, verificationID string) (*Verification, error) {

	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	if artifactID == "" {
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	if verificationID == "" {
		return nil, fmt.Errorf("%w: verification id is required", ErrInvalidInput)
	}

	verification, err := s.repo.GetVerificationByID(ctx, verificationID)
	if err != nil {
		return nil, err
	}

	if verification.ArtifactID != artifactID {
		return nil, ErrVerificationNotFound
	}

	artifact, err := s.repo.GetArtifactByID(ctx, verification.ArtifactID)
	if err != nil {
		return nil, err
	}

	if artifact.DealID != dealID {
		return nil, ErrVerificationNotFound
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

// isValidEmail does a light syntactic check using the standard library. The
// authoritative validation happens when the client actually registers with
// this email (auth.Service lowercases + enforces a stricter pattern).
func isValidEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && strings.Contains(s, "@")
}
