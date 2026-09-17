package deals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/mail"
	"path/filepath"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
	"github.com/Amonochuka/ganji-backend/internal/storage"
)

// CVAnchorer persists Live CV hash anchors for delivered work. It is
// implemented by the cv package and injected through WithCVAnchorer so the
// deals service never depends on cv directly (and cv never depends on deals).
type CVAnchorer interface {
	// AnchorReleasedDeal writes CV entries for a released deal's artifacts.
	// It is called after the escrow is released and must be safe to fail:
	// CV anchoring is derived data, not on the money path.
	AnchorReleasedDeal(ctx context.Context, freelancerID, dealID string) error
}

type Service struct {
	repo           DealRepository
	lnbits         *lnbits.Client
	cv             CVAnchorer
	storage        storage.Storage
	maxUploadBytes int64
}

type Option func(*Service)

// WithCVAnchorer injects the Live CV anchoring hook. Once wired, approving a
// deal anchors its artifacts as verified entries on the freelancer's CV.
func WithCVAnchorer(a CVAnchorer) Option {
	return func(s *Service) { s.cv = a }
}

// WithStorage wires the artifact blob backend and the per-upload size cap.
// Uploaded files are streamed into storage; DownloadArtifact reads them back.
func WithStorage(st storage.Storage, maxUploadBytes int64) Option {
	return func(s *Service) {
		s.storage = st
		s.maxUploadBytes = maxUploadBytes
	}
}

func NewService(repo DealRepository, lnbits *lnbits.Client, opts ...Option) *Service {
	s := &Service{
		repo:   repo,
		lnbits: lnbits,
	}
	for _, o := range opts {
		o(s)
	}
	return s
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

	// High-entropy, revocable share token for the public payment link. Kept
	// separate from the deal's DB id so a leaked link can be rotated without
	// exposing (or changing) the internal row handle.
	shareToken, err := generateShareToken()
	if err != nil {
		return fmt.Errorf("generating share token: %w", err)
	}
	deal.ShareToken = shareToken

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

// ApproveDeal settles the escrow on the network and forwards the funds to
// the freelancer. Only the client (matched by the client_email recorded on
// the deal) can approve. Because released is a terminal state, approving
// also stamps verified_at for the Live CV.
//
// The two network legs are deliberate and non-atomic:
//  1. SettleHold reveals the preimage, completing the held payment so the
//     sats land in Ganji's LNbits wallet.
//  2. PayInvoice forwards those sats onward to the freelancer's invoice.
//
// Idempotency: approving twice (or a crash between the two legs) re-settles
// nothing and re-pays the freelancer's invoice. A settle that fails because
// the hold is already settled is treated as success and we move straight to
// the payout. Payout is only ever attempted after LNbits confirms the escrow
// is actually settled.
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

	if deal.Preimage == "" {
		return nil, fmt.Errorf("%w: cannot approve a deal without an escrow preimage", ErrInvalidInput)
	}

	if _, err := s.lnbits.SettleHold(ctx, deal.Preimage); err != nil {
		// A previous approve may already have settled this hold (for
		// example if we crashed between settle and the DB update). Confirm
		// with LNbits that the funds really are settled before paying out.
		payment, checkErr := s.lnbits.CheckPayment(ctx, deal.CheckingID)
		if checkErr != nil || payment.Details.Status != "SETTLED" {
			return nil, fmt.Errorf("settling escrow for deal %s: %w", dealID, err)
		}
	}

	if err := s.lnbits.PayInvoice(ctx, deal.PayeeInvoice); err != nil {
		return nil, fmt.Errorf("paying freelancer for deal %s: %w", dealID, err)
	}

	if err := s.repo.UpdateStatus(ctx, dealID, StatusReleased); err != nil {
		return nil, fmt.Errorf("releasing escrow for deal %s: %w", dealID, err)
	}

	deal.Status = StatusReleased

	// Live CV anchoring. Best-effort and deliberately off the money path:
	// a failed anchor must never roll back a completed release, and the
	// public CV self-heals any missing anchors on its next read anyway.
	if s.cv != nil {
		if err := s.cv.AnchorReleasedDeal(ctx, deal.FreelancerID, dealID); err != nil {
			log.Printf("cv: anchoring released deal %s: %v", dealID, err)
		}
	}

	return deal, nil
}

// DisputeDeal cancels the hold on the network (refunding the client) and
// records the deal as refunded. Only the client can dispute.
//
// Cancelling a hold nobody ever funded succeeds trivially — if the client
// disputes before paying, or the hold already expired/cancelled, there is
// nothing held to return. But if LNbits still holds the funds and the cancel
// fails, we refuse to mark the deal refunded: the escrow stays committed.
// A hold that was already settled can no longer be cancelled (the sats are
// in the wallet) and requires admin handling, so DisputeDeal refuses rather
// than lying about the escrow.
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

	if !CanTransition(deal.Status, StatusRefunded) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusRefunded)
	}

	if deal.PreimageHash == "" {
		return nil, fmt.Errorf("%w: cannot dispute a deal without a payment hash", ErrInvalidInput)
	}

	if _, err := s.lnbits.CancelHold(ctx, deal.PreimageHash); err != nil {
		payment, checkErr := s.lnbits.CheckPayment(ctx, deal.CheckingID)
		if checkErr != nil {
			return nil, fmt.Errorf("cancelling escrow for deal %s (status check failed): %w", dealID, err)
		}

		switch payment.Details.Status {
		case "SETTLED":
			return nil, fmt.Errorf("%w: escrow already settled, refund must be handled by an operator", ErrInvalidTransition)
		case "UNPAID", "EXPIRED", "CANCELLED":
			// Nothing held — the refund already happened trivially.
		default:
			return nil, fmt.Errorf("cancelling escrow for deal %s: %w", dealID, err)
		}
	}

	if err := s.repo.UpdateStatus(ctx, dealID, StatusRefunded); err != nil {
		return nil, fmt.Errorf("refunding deal %s: %w", dealID, err)
	}

	deal.Status = StatusRefunded
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

	if err := s.refreshPaymentStatus(ctx, deal); err != nil {
		return nil, fmt.Errorf("checking payment with lnbits: %w", err)
	}

	return deal, nil
}

// refreshPaymentStatus asks LNbits for the hold's current state and moves an
// awaiting_payment deal to locked once the payment is confirmed. It is the
// single "paid => locked" reconciliation shared by the authed poll endpoint
// and the public share link. LNbits errors are returned so the authed caller
// can surface them; the public link treats them as best-effort.
func (s *Service) refreshPaymentStatus(ctx context.Context, deal *Deal) error {
	payment, err := s.lnbits.CheckPayment(ctx, deal.CheckingID)
	if err != nil {
		return err
	}

	if !payment.Paid {
		return nil
	}

	if deal.Status == StatusAwaitingPayment {
		if err := s.repo.UpdateStatus(ctx, deal.ID, StatusLocked); err != nil {
			return err
		}
		deal.Status = StatusLocked
	}

	return nil
}

// SweepExpiredHolds reconciles DB state with LNbits for old open deals.
// When a hold invoice expires (or was never funded and is now UNPAID /
// EXPIRED / CANCELLED), any committed funds have already returned to the
// client on the Lightning network — so the deal should be recorded as
// refunded instead of sitting in awaiting_payment/locked forever. Holds that
// LNbits still reports as held (or settled) are left untouched. Returns the
// number of deals reconciled to refunded.
func (s *Service) SweepExpiredHolds(ctx context.Context, cutoff time.Time) (int, error) {
	open, err := s.repo.ListOpenBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}

	swept := 0
	for i := range open {
		deal := &open[i]
		if deal.CheckingID == "" {
			continue
		}

		payment, err := s.lnbits.CheckPayment(ctx, deal.CheckingID)
		if err != nil {
			// LNbits unreachable — leave the deal for the next sweep.
			continue
		}

		switch payment.Details.Status {
		case "UNPAID", "EXPIRED", "CANCELLED":
			if err := s.repo.UpdateStatus(ctx, deal.ID, StatusRefunded); err != nil {
				continue
			}
			swept++
		default:
			// HOLD / ACCEPTED / SETTLED — funds still committed.
		}
	}

	return swept, nil
}

// UpdatePayeeInvoice lets the freelancer replace the payout destination while
// the deal is still open. If the original payee invoice expired mid-deal the
// approve-time payout leg would fail and leave the escrow settled but the
// deal unreleased; a fresh invoice here unsticks it — re-approving then
// forwards the funds to the new invoice. Frozen once the deal is released or
// refunded.
func (s *Service) UpdatePayeeInvoice(ctx context.Context, userID, dealID, bolt11 string) (*Deal, error) {
	dealID = strings.TrimSpace(dealID)
	bolt11 = strings.TrimSpace(bolt11)

	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	if bolt11 == "" {
		return nil, fmt.Errorf("%w: payee invoice is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	switch deal.Status {
	case StatusReleased, StatusRefunded:
		return nil, fmt.Errorf("%w: payee invoice is frozen once the deal is %s", ErrInvalidInput, deal.Status)
	}

	if deal.PayeeInvoice == bolt11 {
		return deal, nil
	}

	if err := s.repo.UpdatePayeeInvoice(ctx, dealID, bolt11); err != nil {
		return nil, fmt.Errorf("updating payee invoice: %w", err)
	}

	deal.PayeeInvoice = bolt11
	return deal, nil
}

// Artifacts
//
// UploadArtifact persists a deliverable (the file) for a deal and records its
// storage reference. Only the freelancer can upload, and only while the deal
// is still open to the work — once submitted (or released/disputed/refunded)
// the deliverable set is frozen. The file is streamed straight into storage:
// the reader is size-capped so an oversized upload is rejected without a byte
// being committed. If the DB record cannot be written afterwards, the
// just-saved blob is best-effort deleted so storage never accumulates orphans.
func (s *Service) UploadArtifact(ctx context.Context, userID, dealID string, kind ArtifactKind, filename string, r io.Reader) (*Artifact, error) {
	if s.storage == nil {
		return nil, fmt.Errorf("%w: artifact storage is not configured", ErrInvalidInput)
	}
	if s.maxUploadBytes <= 0 {
		return nil, fmt.Errorf("%w: artifact upload limit is not configured", ErrInvalidInput)
	}
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	switch kind {
	case ArtifactSourceCode, ArtifactSourceFile:
	default:
		return nil, fmt.Errorf("%w: invalid artifact kind", ErrInvalidInput)
	}
	if r == nil {
		return nil, fmt.Errorf("%w: artifact content is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	// TEMP: Allow uploads before LNBits escrow integration.
	// Remove this once deals automatically transition to StatusLocked after payment.
	switch deal.Status {
	case StatusWorkSubmitted,
		StatusReviewing,
		StatusReleased,
		StatusDisputed:
		return nil, fmt.Errorf("%w: uploads are no longer allowed", ErrInvalidInput)
	}

	key, err := artifactStorageKey(dealID, filename)
	if err != nil {
		return nil, fmt.Errorf("generating artifact storage key: %w", err)
	}

	// Size cap: read at most max+1 bytes so we can tell an oversized upload
	// apart from a legal one (io.Reader yields nil after the cap), and reject
	// before the blob is committed.
	counted := &countingReader{r: r}
	if err := s.storage.Save(ctx, key, io.LimitReader(counted, s.maxUploadBytes+1)); err != nil {
		return nil, fmt.Errorf("storing artifact: %w", err)
	}
	if counted.n > s.maxUploadBytes {
		_ = s.storage.Delete(ctx, key)
		return nil, fmt.Errorf("%w: upload exceeds %d bytes", ErrInvalidInput, s.maxUploadBytes)
	}

	artifact := &Artifact{DealID: dealID, Kind: kind, StorageKey: key}
	if err := s.repo.CreateArtifact(ctx, artifact); err != nil {
		_ = s.storage.Delete(ctx, key) // best-effort cleanup on DB failure
		return nil, err
	}
	return artifact, nil
}

// DownloadArtifact opens a deal artifact's stored content for streaming to the
// caller. Both the freelancer and the client (matched by the recorded
// client_email) may download — the client is precisely who has to review the
// work. Anyone else is forbidden. Returns the artifact metadata plus a reader
// and its size so the handler can send accurate headers.
func (s *Service) DownloadArtifact(ctx context.Context, userID, email, dealID, artifactID string) (*Artifact, io.ReadCloser, int64, error) {
	if s.storage == nil {
		return nil, nil, 0, fmt.Errorf("%w: artifact storage is not configured", ErrInvalidInput)
	}
	if dealID == "" {
		return nil, nil, 0, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	if artifactID == "" {
		return nil, nil, 0, fmt.Errorf("%w: artifact id is required", ErrInvalidInput)
	}

	artifact, err := s.repo.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return nil, nil, 0, err
	}

	if artifact.DealID != dealID {
		return nil, nil, 0, ErrArtifactNotFound
	}

	deal, err := s.repo.GetDealByID(ctx, artifact.DealID)
	if err != nil {
		return nil, nil, 0, err
	}

	if deal.FreelancerID != userID && !strings.EqualFold(deal.ClientEmail, email) {
		return nil, nil, 0, ErrForbidden
	}

	reader, err := s.storage.Open(ctx, artifact.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil, 0, ErrArtifactNotFound
		}
		return nil, nil, 0, fmt.Errorf("opening stored artifact: %w", err)
	}

	size, err := s.storage.Size(ctx, artifact.StorageKey)
	if err != nil {
		_ = reader.Close()
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil, 0, ErrArtifactNotFound
		}
		return nil, nil, 0, fmt.Errorf("sizing stored artifact: %w", err)
	}

	return artifact, reader, size, nil
}

// artifactStorageKey mints a unique key for an uploaded file:
// deals/<dealID>/<32-random-hex><safe-extension>. The extension is preserved
// (sanitized) so download responses can pick a usable Content-Type; the
// original filename is never carried into the key.
func artifactStorageKey(dealID, filename string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := "deals/" + dealID + "/" + hex.EncodeToString(b)
	if ext := sanitizeExt(filename); ext != "" {
		key += ext
	}
	return key, nil
}

// sanitizeExt returns the lowercase ".ext" of a filename only when it is a
// safe-looking alphanumeric extension (2–10 chars); otherwise "".
func sanitizeExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) < 2 || len(ext) > 10 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

// countingReader counts bytes as they are streamed so the size cap can be
// enforced after Save without buffering the whole payload in memory.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
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

// GetPublicDeal returns a safe, public view of the deal for the shareable
// link. No auth required — anyone holding the (unguessable, rotatable)
// share_token can see it. We expose only the fields needed to pay and track
// the deal, never secrets (preimage, payee_invoice, client_email,
// checking_id, the internal deal id, or the share token itself).
//
// Before answering, GetPublicDeal re-checks the hold's status with LNbits and
// locks the deal if the payment landed. The client who opens this link has no
// account, so they can never hit the freelancer-only poll endpoint — without
// this refresh the page would stay "awaiting_payment" if the webhook was
// delayed, lost, or never configured. LNbits being down is not a visitor
// error: we just fall back to the last known status.
func (s *Service) GetPublicDeal(ctx context.Context, shareToken string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}
	if deal.CheckingID != "" && deal.Status == StatusAwaitingPayment {
		_ = s.refreshPaymentStatus(ctx, deal)
	}
	return &PublicDeal{
		Title:          deal.Title,
		AmountSats:     deal.AmountSats,
		SourcePlatform: deal.SourcePlatform,
		Invoice:        deal.Invoice,
		Status:         deal.Status,
		CreatedAt:      deal.CreatedAt,
	}, nil
}

// RotateShareLink mints a fresh share token for a deal, immediately killing
// any previously shared link (the old token no longer resolves). Freelancer
// only, and frozen once the deal is released/refunded — after the money has
// moved there is nothing left to share. Returns the updated deal so the
// caller can rebuild the link from deal.ShareToken.
func (s *Service) RotateShareLink(ctx context.Context, userID, dealID string) (*Deal, error) {
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

	switch deal.Status {
	case StatusReleased, StatusRefunded:
		return nil, fmt.Errorf("%w: share link is frozen once the deal is %s", ErrInvalidInput, deal.Status)
	}

	token, err := generateShareToken()
	if err != nil {
		return nil, fmt.Errorf("generating share token: %w", err)
	}

	if err := s.repo.UpdateShareToken(ctx, dealID, token); err != nil {
		return nil, fmt.Errorf("rotating share token: %w", err)
	}

	deal.ShareToken = token
	return deal, nil
}

// generateShareToken returns a URL-safe random token for the public payment
// link. 32 random bytes (~256 bits of entropy) base64url-encoded without
// padding. It is independent of the preimage and the deal id: rotating it
// revokes the old link with no effect on the escrow.
func generateShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// isValidEmail does a light syntactic check using the standard library. The
// authoritative validation happens when the client actually registers with
// this email (auth.Service lowercases + enforces a stricter pattern).
func isValidEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && strings.Contains(s, "@")
}
