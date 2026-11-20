package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
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

type AcceptedHoldChecker interface {
	IsAcceptedHold(ctx context.Context, paymentHash string, amountSats int64) (bool, error)
}

// DealNotifier delivers best-effort notifications after a durable state
// transition. Implementations must never make escrow correctness depend on
// notification delivery.
// Both freelancer and client are notified where applicable.
type DealNotifier interface {
	PaymentLocked(ctx context.Context, deal *Deal)
	DealDisputed(ctx context.Context, deal *Deal)
	DealReleased(ctx context.Context, deal *Deal)
	DealRefunded(ctx context.Context, deal *Deal)
	DealExited(ctx context.Context, deal *Deal)                             // client exited the deal
	MilestoneMoved(ctx context.Context, deal *Deal, fromOrder, toOrder int) // client moved to next milestone
}

// maxDisputeReasonRunes caps how much the client can write when raising a
// dispute. Enough to explain the problem, short enough to not become an
// attachments dump.
const maxDisputeReasonRunes = 2000

type Service struct {
	repo           DealRepository
	lnbits         *lnbits.Client
	acceptedHolds  AcceptedHoldChecker
	cv             CVAnchorer
	storage        storage.Storage
	maxUploadBytes int64
	notifier       DealNotifier
	fundMu         sync.Mutex
	fundLocks      map[string]*sync.Mutex
}

type Option func(*Service)

// WithCVAnchorer injects the Live CV anchoring hook. Once wired, approving a
// deal anchors its artifacts as verified entries on the freelancer's CV.
func WithCVAnchorer(a CVAnchorer) Option {
	return func(s *Service) { s.cv = a }
}

func WithAcceptedHoldChecker(checker AcceptedHoldChecker) Option {
	return func(s *Service) { s.acceptedHolds = checker }
}

// WithStorage wires the artifact blob backend and the per-upload size cap.
// Uploaded files are streamed into storage; DownloadArtifact reads them back.
func WithStorage(st storage.Storage, maxUploadBytes int64) Option {
	return func(s *Service) {
		s.storage = st
		s.maxUploadBytes = maxUploadBytes
	}
}

// WithNotifier wires best-effort deal-status notifications.
func WithNotifier(n DealNotifier) Option {
	return func(s *Service) { s.notifier = n }
}

func NewService(repo DealRepository, lnbits *lnbits.Client, opts ...Option) *Service {
	s := &Service{
		repo:      repo,
		lnbits:    lnbits,
		fundLocks: make(map[string]*sync.Mutex),
	}
	for _, o := range opts {
		o(s)
	}
	return s
} // ApproveDeal settles the escrow on the network and forwards the funds to
// the freelancer. Only the client (matched by the client_email recorded on
// the deal) can approve. Because released is a terminal state, approving
// also stamps verified_at for the Live CV.
//
// The two network legs are deliberate and non-atomic:
//  1. SettleHold reveals the preimage, completing the held payment so the
//     sats land in Ganji's LNbits wallet.
//  2. PayInvoice forwards those sats onward to the freelancer's invoice.
//
// The release is two-phase and conservative about payouts (see releaseEscrow
// and explained.md §3). In a nutshell: a durable payout_attempted_at marker
// is committed BEFORE any money moves, the network legs run outside any DB
// lock, and a retry never auto-resends — a verified payout releases without
// re-paying, a provably-failed one is cleared and retried, and anything
// ambiguous surfaces as ErrPayoutInFlight for manual reconciliation.
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

	ms, _ := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if len(ms) > 0 {
		for i := range ms {
			_ = s.refreshMilestonePayment(ctx, &ms[i])
		}
		ms, _ = s.repo.GetMilestonesByDeal(ctx, deal.ID)
		idx := -1
		for i := range ms {
			st := ms[i].Status
			if st == MilestoneWorkSubmitted || st == MilestoneInProgress || st == MilestoneFunded || st == MilestoneCompleted {
				idx = i
				break
			}
		}
		if idx < 0 {
			for i := range ms {
				if ms[i].Status == MilestoneDisputed || ms[i].Status == MilestoneRejected {
					return nil, fmt.Errorf("%w: milestone %d is under review/dispute; independent resolution required", ErrInvalidTransition, ms[i].Order)
				}
			}
			return nil, fmt.Errorf("%w: no milestone available for approval", ErrInvalidTransition)
		}
		target := ms[idx]
		if _, err := s.releaseMilestoneViaCanonicalPath(ctx, deal, &target); err != nil {
			return nil, err
		}
		// re-read target status after canonical release
		rel, _ := s.repo.GetMilestoneByID(ctx, target.ID)
		if idx+1 < len(ms) {
			next := ms[idx+1]
			if rel != nil && rel.Status == MilestoneReleased && next.CheckingID == "" && next.AmountSats > 0 && next.Status != MilestoneReleased && next.Status != MilestoneExited && next.Status != MilestoneRefunded {
				_ = s.FundMilestone(ctx, deal, &next)
			} else if next.Status == MilestonePending {
				_ = s.repo.UpdateMilestoneStatus(ctx, next.ID, MilestoneInProgress, nil)
			}
			s.notifyMilestoneMoved(deal, target.Order, next.Order)
			deal.Status = StatusWorkSubmitted
		} else {
			if rel != nil && rel.Status == MilestoneReleased {
				_ = s.repo.UpdateStatus(ctx, deal.ID, StatusReleased)
				deal.Status = StatusReleased
				s.notifyReleased(deal)
			}
		}
		fresh, _ := s.repo.GetDealByID(ctx, deal.ID)
		if fresh != nil {
			return fresh, nil
		}
		return deal, nil
	}

	return s.releaseEscrow(ctx, dealID, releaseEscrowOptions{
		authorize: func(d *Deal) error {
			if !strings.EqualFold(d.ClientEmail, email) {
				return ErrForbidden
			}
			if !CanTransition(d.Status, StatusReleased) {
				return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, d.Status, StatusReleased)
			}
			return nil
		},
		finalize: func(repo DealRepository, id string) error {
			return repo.UpdateStatus(ctx, id, StatusReleased)
		},
	})
}

// releaseEscrowOptions captures how the shared release flow differs between a
// client approving and an operator releasing a dispute: the authorization and
// the UPDATE that finalizes the deal.
type releaseEscrowOptions struct {
	authorize func(*Deal) error
	finalize  func(repo DealRepository, dealID string) error
}

// releaseEscrow is the shared, two-phase escrow release behind ApproveDeal and
// an operator's Release resolution — the money legs are identical, only the
// authorization and the final UPDATE differ.
//
//   - Phase 1 (durable intent): lock the row, authorize, and — when there is
//     no payout yet — commit payout_attempted_at BEFORE any money moves. A
//     crash after this leaves a visible marker; the next attempt reads it and
//     refuses to auto-resend instead of double-paying.
//   - Phase 2 (network): settle the hold and pay the freelancer WITHOUT the
//     row lock or a transaction pinned across the external calls.
//   - Phase 3 (finalize): re-lock the row, re-validate the transition, record
//     the payout checking_id if a fresh payout was just sent, then apply the
//     release UPDATE and commit.
//
// Payout policy (conservative; see explained.md §3):
//   - payout_checking_id set + confirmed paid     -> release without re-paying.
//   - payout_checking_id set + confirmed failed   -> clear tracking, re-pay.
//   - payout_checking_id set + ambiguous status   -> ErrPayoutInFlight, no resend.
//   - payout_attempted_at set, no checking_id      -> ErrPayoutInFlight, no
//     resend (crash window: the earlier attempt may have moved money).
//   - PayInvoice refused with a 4xx                -> provably not sent: clear
//     the marker so a later attempt is legal, surface the error.
//   - PayInvoice failed 5xx/timeout                -> ambiguous: keep the
//     marker, surface ErrPayoutInFlight.
func (s *Service) releaseEscrow(ctx context.Context, dealID string, opts releaseEscrowOptions) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	// Phase 1: authorize under the row lock, then commit the durable intent.
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	repo := s.repo.WithTx(tx)

	deal, err := repo.GetDealForUpdate(ctx, dealID)
	if err != nil {
		return nil, err
	}
	originalStatus := deal.Status
	if err := opts.authorize(deal); err != nil {
		return nil, err
	}

	confirmed := false
	payoutCheckingID := deal.PayoutCheckingID
	switch {
	case deal.PayoutCheckingID != "":
		ok, failed, err := s.checkOutgoingPayout(ctx, deal.PayoutCheckingID)
		if err != nil {
			// Cannot tell whether the earlier payout landed. Never resend.
			return nil, fmt.Errorf("%w: verifying payout %s for deal %s: %v",
				ErrPayoutInFlight, deal.PayoutCheckingID, deal.ID, err)
		}
		switch {
		case ok:
			confirmed = true
		case failed:
			// Definitively not sent — reset the stale tracking and re-try below.
			if err := repo.ClearPayoutTracking(ctx, deal.ID); err != nil {
				return nil, fmt.Errorf("resetting payout tracking for deal %s: %w", deal.ID, err)
			}
			payoutCheckingID = ""
		default:
			// Still pending/in flight with no verdict. Do not resend.
			return nil, fmt.Errorf("%w: payout %s for deal %s is not confirmed",
				ErrPayoutInFlight, deal.PayoutCheckingID, deal.ID)
		}
	case deal.PayoutAttemptedAt.Valid:
		// A previous release recorded an attempt but never confirmed it. This is
		// exactly the crash window — money may or may not have moved.
		return nil, fmt.Errorf("%w: deal %s has an unconfirmed payout attempt", ErrPayoutInFlight, deal.ID)
	}

	if !confirmed {
		if err := repo.MarkPayoutAttempted(ctx, deal.ID); err != nil {
			return nil, fmt.Errorf("recording payout attempt for deal %s: %w", deal.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Phase 2: the money legs, deliberately outside any lock/transaction so a
	// stray LNbits stall cannot pin the row or a pool connection.
	if !confirmed {
		if err := s.settleHoldGuarded(ctx, deal); err != nil {
			// Settlement provably did not complete, so no money moved and the
			// attempt marker is safe to clear: a later request may retry.
			if clearErr := s.clearPayoutAttempt(ctx, deal.ID); clearErr != nil {
				log.Printf("clearing payout attempt for deal %s: %v", deal.ID, clearErr)
			}
			return nil, err
		}

		id, err := s.lnbits.PayInvoice(ctx, deal.PayeeInvoice)
		if err != nil {
			if errors.Is(err, lnbits.ErrPayoutRefused) {
				// Provably not sent (rejected by LNbits before any payment):
				// reset the marker so a later approve is legal.
				if clearErr := s.clearPayoutAttempt(ctx, deal.ID); clearErr != nil {
					log.Printf("clearing payout attempt for deal %s: %v", deal.ID, clearErr)
				}
				return nil, fmt.Errorf("paying freelancer for deal %s: %w", deal.ID, err)
			}
			// Ambiguous (5xx / timeout): may have been sent. Keep the marker.
			return nil, fmt.Errorf("%w: paying freelancer for deal %s: %v", ErrPayoutInFlight, deal.ID, err)
		}
		payoutCheckingID = id

		confirmedPayout, failedPayout, checkErr := s.checkOutgoingPayout(ctx, id)
		switch {
		case checkErr != nil:
			if recordErr := s.repo.UpdatePayoutCheckingID(ctx, deal.ID, id); recordErr != nil {
				log.Printf("recording payout %s for deal %s: %v", id, deal.ID, recordErr)
			}
			return nil, fmt.Errorf("%w: verifying payout %s for deal %s: %v",
				ErrPayoutInFlight, id, deal.ID, checkErr)
		case failedPayout:
			if clearErr := s.clearPayoutAttempt(ctx, deal.ID); clearErr != nil {
				log.Printf("clearing payout attempt for deal %s: %v", deal.ID, clearErr)
			}
			return nil, fmt.Errorf("%w: payout %s for deal %s was refused by LNbits",
				ErrPayoutFailed, id, deal.ID)
		case !confirmedPayout:
			if recordErr := s.repo.UpdatePayoutCheckingID(ctx, deal.ID, id); recordErr != nil {
				log.Printf("recording payout %s for deal %s: %v", id, deal.ID, recordErr)
			}
			return nil, fmt.Errorf("%w: payout %s for deal %s is not confirmed",
				ErrPayoutInFlight, id, deal.ID)
		}
	}

	// Phase 3: re-lock and finalize. Another actor may have moved the deal
	// while the network legs ran, so the transition is re-validated.
	tx2, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx2.Rollback()
	repo2 := s.repo.WithTx(tx2)

	deal2, err := repo2.GetDealForUpdate(ctx, dealID)
	if err != nil {
		return nil, err
	}
	// If the deal was NOT disputed in phase 1 but IS disputed now, a client
	// disputed during the network legs. Abort — the client explicitly froze
	// the funds; they must approve again from disputed (or operator resolves).
	// This prevents approve from silently overwriting a dispute.
	if originalStatus != StatusDisputed && deal2.Status == StatusDisputed {
		return nil, fmt.Errorf("%w: deal was disputed during release; cannot auto-release", ErrInvalidTransition)
	}
	if !CanTransition(deal2.Status, StatusReleased) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal2.Status, StatusReleased)
	}
	if payoutCheckingID != "" && deal2.PayoutCheckingID == "" {
		if err := repo2.UpdatePayoutCheckingID(ctx, dealID, payoutCheckingID); err != nil {
			return nil, fmt.Errorf("recording payout for deal %s: %w", deal.ID, err)
		}
	}
	if err := opts.finalize(repo2, dealID); err != nil {
		return nil, err
	}
	if err := tx2.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	deal2.Status = StatusReleased

	s.anchorReleasedDeal(ctx, deal2)
	s.notifyReleased(deal2)

	return deal2, nil
}

// releaseMilestoneViaCanonicalPath enforces escrow: releasing a milestone must go
// through ReleaseMilestone (settle+pay -> MilestoneReleased=paid). A milestone with
// no escrow leg cannot be released/paid or advanced as paid — caller must fund it first.
func (s *Service) releaseMilestoneViaCanonicalPath(ctx context.Context, deal *Deal, m *Milestone) (*Milestone, error) {
	if m.Status == MilestoneReleased {
		return m, nil
	}
	if m.PreimageHash == "" && m.CheckingID == "" {
		return nil, fmt.Errorf("%w: milestone %d has no escrow hold (fund it first)", ErrInvalidInput, m.Order)
	}
	return s.ReleaseMilestone(ctx, deal, m.ID)
}

// ReleaseMilestone is the ONLY path that moves a milestone escrow to released/paid:
// settles the milestone hold and pays the freelancer, then marks MilestoneReleased.
// completed = approved/payout-pending (if used), never "paid"; released = money moved.
// Conservative/idempotent: existing confirmed payout -> release without re-pay;
// failed payout -> clear+retry; ambiguous/in-flight or unknown crash-window -> ErrPayoutInFlight.
func (s *Service) ReleaseMilestone(ctx context.Context, deal *Deal, milestoneID string) (*Milestone, error) {
	// Lightweight precheck (no lock) so common cases fail fast.
	m0, err := s.repo.GetMilestoneByID(ctx, milestoneID)
	if err != nil {
		return nil, err
	}
	if m0.DealID != deal.ID {
		return nil, ErrMilestoneNotFound
	}
	if m0.Status == MilestoneReleased {
		return m0, nil
	}
	if m0.Preimage == "" || m0.CheckingID == "" {
		return nil, fmt.Errorf("%w: milestone has no escrow invoice (fund it first)", ErrInvalidInput)
	}
	_ = s.refreshMilestonePayment(ctx, m0)

	// Phase 1: lock milestone, verify payout state, commit durable intent before any money moves.
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	repo := s.repo.WithTx(tx)

	m, err := repo.GetMilestoneForUpdate(ctx, milestoneID)
	if err != nil {
		return nil, err
	}
	if m.DealID != deal.ID {
		return nil, ErrMilestoneNotFound
	}
	if m.Status == MilestoneReleased {
		return m, nil
	}
	originalStatus := m.Status

	confirmed := false
	payoutCheckingID := m.PayoutCheckingID
	switch {
	case m.PayoutCheckingID != "":
		ok, failed, err := s.checkOutgoingPayout(ctx, m.PayoutCheckingID)
		if err != nil {
			return nil, fmt.Errorf("%w: verifying milestone payout %s for %s: %v", ErrPayoutInFlight, m.PayoutCheckingID, m.ID, err)
		}
		switch {
		case ok:
			confirmed = true
		case failed:
			if err := repo.ClearMilestonePayoutTracking(ctx, m.ID); err != nil {
				return nil, fmt.Errorf("resetting milestone payout tracking %s: %w", m.ID, err)
			}
			payoutCheckingID = ""
			m.PayoutCheckingID = ""
			m.PayoutAttemptedAt = sql.NullTime{}
		default:
			return nil, fmt.Errorf("%w: milestone payout %s for %s is not confirmed", ErrPayoutInFlight, m.PayoutCheckingID, m.ID)
		}
	case m.PayoutAttemptedAt.Valid:
		return nil, fmt.Errorf("%w: milestone %s has an unconfirmed payout attempt", ErrPayoutInFlight, m.ID)
	}

	if !confirmed {
		// Mirror CanMilestoneTransition — released is only legal from the
		// states below. AwaitingPayment -> released is deliberately excluded
		// (must first refresh funded/in_progress, then submit — same as the
		// deal-level awaiting_payment -> released prohibition). Rejected is
		// included: it is under-review material that an operator release can
		// settle via the canonical path.
		if !CanMilestoneTransition(m.Status, MilestoneReleased) {
			return nil, fmt.Errorf("%w: milestone cannot be released from status %s", ErrInvalidTransition, m.Status)
		}
		if m.Preimage == "" || m.CheckingID == "" {
			return nil, fmt.Errorf("%w: milestone has no escrow invoice (fund it first)", ErrInvalidInput)
		}
		if err := repo.MarkMilestonePayoutAttempted(ctx, m.ID); err != nil {
			return nil, fmt.Errorf("recording milestone payout attempt %s: %w", m.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Phase 2: money legs outside lock/tx.
	if !confirmed {
		if err := s.settleMilestoneHoldGuarded(ctx, m); err != nil {
			_ = s.repo.ClearMilestonePayoutTracking(ctx, m.ID)
			return nil, err
		}
		payee := m.PayeeInvoice
		if payee == "" {
			payee = deal.PayeeInvoice
		}
		id, err := s.lnbits.PayInvoice(ctx, payee)
		if err != nil {
			if errors.Is(err, lnbits.ErrPayoutRefused) {
				_ = s.repo.ClearMilestonePayoutTracking(ctx, m.ID)
				return nil, fmt.Errorf("paying freelancer for milestone %s: %w", m.ID, err)
			}
			return nil, fmt.Errorf("%w: paying freelancer for milestone %s: %v", ErrPayoutInFlight, m.ID, err)
		}
		payoutCheckingID = id

		confirmedPayout, failedPayout, checkErr := s.checkOutgoingPayout(ctx, id)
		switch {
		case checkErr != nil:
			_ = s.repo.UpdateMilestonePayout(ctx, m.ID, id)
			return nil, fmt.Errorf("%w: verifying milestone payout %s for %s: %v",
				ErrPayoutInFlight, id, m.ID, checkErr)
		case failedPayout:
			_ = s.repo.ClearMilestonePayoutTracking(ctx, m.ID)
			return nil, fmt.Errorf("%w: milestone payout %s for %s was refused by LNbits",
				ErrPayoutFailed, id, m.ID)
		case !confirmedPayout:
			_ = s.repo.UpdateMilestonePayout(ctx, m.ID, id)
			return nil, fmt.Errorf("%w: milestone payout %s for %s is not confirmed",
				ErrPayoutInFlight, id, m.ID)
		}
	}

	// Phase 3: finalize under lock. Another actor may have moved the
	// milestone while the network legs ran (reject/exit/dispute), so the
	// transition is re-validated before recording released.
	tx2, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx2.Rollback()
	repo2 := s.repo.WithTx(tx2)
	m2, err := repo2.GetMilestoneForUpdate(ctx, milestoneID)
	if err != nil {
		return nil, err
	}
	// If the milestone was NOT disputed in phase 1 but IS disputed now, a
	// client rejected/exited during the network legs. Abort — the client
	// explicitly froze the funds; they must approve again or let an operator
	// resolve (mirrors the deal-level releaseEscrow guard). Work already
	// settled/paid in Phase 2 stays paid on-chain; the milestone simply
	// cannot be recorded as released by this attempt.
	if originalStatus != MilestoneDisputed && m2.Status == MilestoneDisputed {
		return nil, fmt.Errorf("%w: milestone was disputed during release; cannot auto-release", ErrInvalidTransition)
	}
	// Idempotent finalization: already released needs no write.
	now := time.Now().UTC()
	if m2.Status != MilestoneReleased {
		if !CanMilestoneTransition(m2.Status, MilestoneReleased) {
			return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, m2.Status, MilestoneReleased)
		}
		if err := repo2.UpdateMilestoneStatus(ctx, m2.ID, MilestoneReleased, &now); err != nil {
			return nil, err
		}
	}
	if payoutCheckingID != "" && m2.PayoutCheckingID == "" {
		if err := repo2.UpdateMilestonePayout(ctx, m2.ID, payoutCheckingID); err != nil {
			return nil, fmt.Errorf("recording milestone payout %s: %w", m2.ID, err)
		}
	}
	if err := tx2.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	m2.Status = MilestoneReleased
	if payoutCheckingID != "" {
		m2.PayoutCheckingID = payoutCheckingID
	}
	m2.CompletedAt = sql.NullTime{Time: now, Valid: true}
	return m2, nil
}

// PublicAcceptNextMilestone: sequential per-milestone holds.
// If the target milestone has an escrow leg, release it (settle+pay). Then fund next milestone (create its hold invoice) so client can pay M2.
func (s *Service) PublicAcceptNextMilestone(ctx context.Context, shareToken, milestoneID string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}
	ms, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range ms {
		if ms[i].ID == milestoneID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, ErrMilestoneNotFound
	}
	target := ms[idx]
	if target.Status == MilestoneDisputed || target.Status == MilestoneRejected {
		return nil, fmt.Errorf("%w: milestone is under review/dispute; independent resolution required", ErrInvalidTransition)
	}
	if _, err := s.releaseMilestoneViaCanonicalPath(ctx, deal, &target); err != nil {
		return nil, err
	}
	rel, _ := s.repo.GetMilestoneByID(ctx, target.ID)
	if idx+1 < len(ms) {
		next := ms[idx+1]
		if rel != nil && rel.Status == MilestoneReleased && next.CheckingID == "" && next.AmountSats > 0 && next.Status != MilestoneReleased && next.Status != MilestoneExited && next.Status != MilestoneRefunded {
			if err := s.FundMilestone(ctx, deal, &next); err != nil {
				return nil, err
			}
		} else if next.Status == MilestonePending {
			_ = s.repo.UpdateMilestoneStatus(ctx, next.ID, MilestoneInProgress, nil)
		}
		s.notifyMilestoneMoved(deal, target.Order, next.Order)
	} else {
		if rel != nil && rel.Status == MilestoneReleased {
			_ = s.repo.UpdateStatus(ctx, deal.ID, StatusReleased)
			deal.Status = StatusReleased
			s.notifyReleased(deal)
		}
	}
	return s.GetPublicDeal(ctx, shareToken)
}

// ResolveMilestone is the independent-review money resolution for a disputed milestone.
// release: settle+pay via canonical ReleaseMilestone (idempotent). refund: cancel hold + mark refunded.
// No auto-refund on reject/exit; this explicit resolution moves the held funds.
func (s *Service) ResolveMilestone(ctx context.Context, operatorEmail, dealID, milestoneID string, resolution DisputeResolution) (*Milestone, error) {
	operatorEmail = strings.ToLower(strings.TrimSpace(operatorEmail))
	if operatorEmail == "" || dealID == "" || milestoneID == "" {
		return nil, fmt.Errorf("%w: operator email, deal id and milestone id are required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}
	m, err := s.repo.GetMilestoneByID(ctx, milestoneID)
	if err != nil {
		return nil, err
	}
	if m.DealID != deal.ID {
		return nil, ErrMilestoneNotFound
	}
	// Allowed: disputed/rejected for both resolutions; work_submitted/completed
	// for release (parity with approved work that never reached review).
	allowed := false
	switch {
	case m.Status == MilestoneDisputed || m.Status == MilestoneRejected:
		allowed = true
	case resolution == DisputeResolutionRelease && (m.Status == MilestoneWorkSubmitted || m.Status == MilestoneCompleted):
		allowed = true
	}
	if !allowed {
		return nil, fmt.Errorf("%w: milestone cannot be resolved from status %s", ErrInvalidTransition, m.Status)
	}
	// Escrow mandatory: neither release nor refund can proceed without a hold.
	// (No-hold is always a fund-first error — never marked paid.)
	if m.PreimageHash == "" && m.CheckingID == "" {
		return nil, fmt.Errorf("%w: milestone %d has no escrow hold (fund it first)", ErrInvalidInput, m.Order)
	}
	switch resolution {
	case DisputeResolutionRelease:
		rm, err := s.ReleaseMilestone(ctx, deal, milestoneID)
		if err != nil {
			return nil, err
		}
		// advance sequence/finalize deal like approve paths
		ms, _ := s.repo.GetMilestonesByDeal(ctx, deal.ID)
		idx := -1
		for i := range ms {
			if ms[i].ID == milestoneID {
				idx = i
				break
			}
		}
		if idx >= 0 && idx+1 < len(ms) {
			next := ms[idx+1]
			if next.CheckingID == "" && next.AmountSats > 0 && next.Status != MilestoneReleased && next.Status != MilestoneExited && next.Status != MilestoneRefunded {
				_ = s.FundMilestone(ctx, deal, &next)
			} else if next.Status == MilestonePending {
				_ = s.repo.UpdateMilestoneStatus(ctx, next.ID, MilestoneInProgress, nil)
			}
			s.notifyMilestoneMoved(deal, rm.Order, next.Order)
		} else if idx >= 0 {
			_ = s.repo.UpdateStatus(ctx, deal.ID, StatusReleased)
			s.notifyReleased(deal)
		}
		return rm, nil
	case DisputeResolutionRefund:
		// idempotent: if already refunded, return
		if m.Status == MilestoneRefunded {
			return m, nil
		}
		// cancelMilestoneHold is idempotent: an already-gone hold
		// (UNPAID/EXPIRED/CANCELLED) counts as success; a still-held
		// hold that cannot be cancelled surfaces as an error and the
		// milestone stays disputed (never marked refunded).
		if m.PreimageHash != "" {
			if err := s.cancelMilestoneHold(ctx, m); err != nil {
				return nil, err
			}
		}
		now := time.Now().UTC()
		if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneRefunded, &now); err != nil {
			return nil, err
		}
		// do not auto-fund next on refund; sequence halted pending policy
		m.Status = MilestoneRefunded
		m.CompletedAt = sql.NullTime{Time: now, Valid: true}
		s.notifyExited(deal)
		return m, nil
	default:
		return nil, fmt.Errorf("%w: resolution must be %q or %q", ErrInvalidInput, DisputeResolutionRelease, DisputeResolutionRefund)
	}
}
