package deals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

// ResolveDispute is the arbiter's verdict on a frozen dispute. It is the only
// path out of 'disputed' that moves money, and it is operator-only (enforced
// by middleware.OperatorRequired at the route):
//
//   - release: the work is accepted — settle the hold and forward the sats to
//     the freelancer (the same two-phase, conservative release as ApproveDeal,
//     see releaseEscrow).
//   - refund: the work is rejected — cancel the hold so the sats return to the
//     client on the Lightning network.
//
// The deal must actually be disputed; a deal already released/refunded (or
// never disputed) is refused. The resolution and the deciding operator are
// recorded via UpdateDisputeResolution only AFTER the network leg succeeds,
// so the DB never claims a money move the network did not make. There is no
// direct 'paying' state: the release path's payout_attempted_at marker
// (committed before any money moves) is what survives a crash.
func (s *Service) ResolveDispute(ctx context.Context, operatorEmail, dealID string, resolution DisputeResolution) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	operatorEmail = strings.ToLower(strings.TrimSpace(operatorEmail))
	if operatorEmail == "" {
		return nil, fmt.Errorf("%w: operator email is required", ErrInvalidInput)
	}

	switch resolution {
	case DisputeResolutionRelease:
		deal, err := s.releaseEscrow(ctx, dealID, releaseEscrowOptions{
			authorize: func(deal *Deal) error {
				if deal.Status != StatusDisputed {
					return fmt.Errorf("%w: only a disputed deal can be resolved, got %s", ErrInvalidTransition, deal.Status)
				}
				return nil
			},
			finalize: func(repo DealRepository, id string) error {
				return repo.UpdateDisputeResolution(ctx, id, StatusReleased, operatorEmail)
			},
		})
		if err != nil {
			return nil, err
		}
		deal.ResolvedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
		deal.ResolvedBy = operatorEmail
		return deal, nil
	case DisputeResolutionRefund:
		return s.refundDispute(ctx, operatorEmail, dealID)
	default:
		return nil, fmt.Errorf("%w: resolution must be %q or %q", ErrInvalidInput, DisputeResolutionRelease, DisputeResolutionRefund)
	}
}

// refundDispute is the arbiter's refund verdict: cancel the hold on the
// network so the sats return to the client. It runs inside a single row-locked
// transaction and only records the refund after the cancel succeeds (or
// proves the hold is already gone).
func (s *Service) refundDispute(ctx context.Context, operatorEmail, dealID string) (*Deal, error) {
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
	if deal.Status != StatusDisputed {
		return nil, fmt.Errorf("%w: only a disputed deal can be resolved, got %s", ErrInvalidTransition, deal.Status)
	}

	if err := s.cancelEscrowHold(ctx, deal); err != nil {
		return nil, err
	}
	if err := repo.UpdateDisputeResolution(ctx, dealID, StatusRefunded, operatorEmail); err != nil {
		return nil, fmt.Errorf("recording dispute resolution for deal %s: %w", dealID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	deal.Status = StatusRefunded
	deal.ResolvedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	deal.ResolvedBy = operatorEmail
	s.notifyRefunded(deal)
	return deal, nil
}

// ReconcilePayout rescues a deal whose payout is hung and operator-only.
// releaseEscrow deliberately refuses to auto-resend an ambiguous payout
// (ErrPayoutInFlight): it cannot tell whether an earlier attempt moved money,
// and the conservative policy chooses a stuck-but-visible deal over a possible
// duplicate payout. The operator is the human backstop: they inspect the
// deal's outgoing payments in the LNbits UI and tell the system the truth.
//
//   - ReconcileConfirmPayout: the freelancer WAS paid. The operator passes the
//     real payout_checking_id from LNbits history; the service verifies it is
//     genuinely PAID before releasing the deal (no money moves here — the
//     release only records what already happened).
//   - ReconcileResetPayout: the freelancer was NOT paid (earlier attempt
//     failed before sending). Everything is cleared so a normal approve /
//     resolve-release can run the payout again.
func (s *Service) ReconcilePayout(ctx context.Context, operatorEmail, dealID string, action ReconcileAction, payoutCheckingID string) (*Deal, error) {
	operatorEmail = strings.ToLower(strings.TrimSpace(operatorEmail))
	if operatorEmail == "" {
		return nil, fmt.Errorf("%w: operator email is required", ErrInvalidInput)
	}
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	switch action {
	case ReconcileConfirmPayout:
		payoutCheckingID = strings.TrimSpace(payoutCheckingID)
		if payoutCheckingID == "" {
			return nil, fmt.Errorf("%w: confirm_payout requires the payout_checking_id from LNbits history", ErrInvalidInput)
		}

		// Only release once the claimed payout is verifiably done.
		ok, _, err := s.checkOutgoingPayout(ctx, payoutCheckingID)
		if err != nil {
			return nil, fmt.Errorf("%w: verifying payout %s: %v", ErrPayoutInFlight, payoutCheckingID, err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: payout %s is not confirmed as paid", ErrInvalidInput, payoutCheckingID)
		}

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
		if !CanTransition(deal.Status, StatusReleased) {
			return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusReleased)
		}
		if deal.PayoutCheckingID != payoutCheckingID {
			if err := repo.UpdatePayoutCheckingID(ctx, deal.ID, payoutCheckingID); err != nil {
				return nil, fmt.Errorf("recording reconciled payout for deal %s: %w", deal.ID, err)
			}
		}
		if err := repo.UpdateStatus(ctx, deal.ID, StatusReleased); err != nil {
			return nil, fmt.Errorf("releasing reconciled deal %s: %w", deal.ID, err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit transaction: %w", err)
		}

		deal.Status = StatusReleased
		s.anchorReleasedDeal(ctx, deal)
		s.notifyReleased(deal)
		return deal, nil

	case ReconcileResetPayout:
		deal, err := s.repo.GetDealByID(ctx, dealID)
		if err != nil {
			return nil, err
		}
		if deal.Status == StatusReleased || deal.Status == StatusRefunded {
			return nil, fmt.Errorf("%w: deal %s is already %s", ErrInvalidTransition, deal.ID, deal.Status)
		}
		if deal.PayoutCheckingID == "" && !deal.PayoutAttemptedAt.Valid {
			return nil, fmt.Errorf("%w: deal %s has no payout in flight to reset", ErrInvalidInput, deal.ID)
		}
		if err := s.repo.ClearPayoutTracking(ctx, dealID); err != nil {
			return nil, fmt.Errorf("resetting payout tracking for deal %s: %w", dealID, err)
		}
		deal.PayoutCheckingID = ""
		deal.PayoutAttemptedAt = sql.NullTime{}
		return deal, nil

	default:
		return nil, fmt.Errorf("%w: reconcile action must be %q or %q", ErrInvalidInput, ReconcileConfirmPayout, ReconcileResetPayout)
	}
}

// cancelEscrowHold returns held funds to the client by cancelling the hold
// invoice on the network (the network-level refund). Idempotent: if LNbits
// refuses because the hold is already gone, we confirm the payment is no
// longer committed (UNPAID/EXPIRED/CANCELLED) and treat that as success — the
// sats have already returned to the payer. Anything still held/settled that we
// could not cancel is surfaced as an error so the deal stays disputed.
func (s *Service) cancelEscrowHold(ctx context.Context, deal *Deal) error {
	if deal.PreimageHash == "" {
		return fmt.Errorf("%w: cannot refund a deal without a preimage hash", ErrInvalidInput)
	}

	if _, err := s.lnbits.CancelHold(ctx, deal.PreimageHash); err != nil {
		payment, checkErr := s.lnbits.CheckPayment(ctx, deal.CheckingID)
		if checkErr != nil || !isHoldReleased(payment.Details.Status) {
			return fmt.Errorf("cancelling escrow for deal %s: %w", deal.ID, err)
		}
	}

	return nil
}

// CheckPayment queries the escrow node for an accepted hold on a deal's
// invoice. LNbits paid=true is not proof that funds are locked, so the deal
// only transitions from awaiting_payment to locked after the escrow node
// reports an accepted hold under the deal's payment hash.
func (s *Service) FundDeal(ctx context.Context, shareToken, milestoneID, walletURL, walletKey string) (*PublicDeal, error) {
	if strings.TrimSpace(shareToken) == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	endpoint, err := validateWalletEndpoint(walletURL)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(walletKey)
	if key == "" {
		return nil, fmt.Errorf("%w: wallet key is required", ErrInvalidInput)
	}
	if s.acceptedHolds == nil {
		return nil, ErrVerificationUnavailable
	}

	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}
	unlock := s.lockFund(deal.ID)
	defer unlock()

	if milestoneID != "" {
		m, err := s.repo.GetMilestoneByID(ctx, milestoneID)
		if err != nil {
			return nil, err
		}
		return s.fundMilestoneHold(ctx, deal, shareToken, m, endpoint, key)
	}

	ms, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].Status == MilestoneAwaitingPayment || ms[i].Status == MilestonePending {
			return s.fundMilestoneHold(ctx, deal, shareToken, &ms[i], endpoint, key)
		}
	}
	if len(ms) > 0 {
		return nil, fmt.Errorf("%w: no milestone is awaiting payment", ErrInvalidTransition)
	}
	return s.fundDealHold(ctx, deal, shareToken, endpoint, key)
}

func (s *Service) fundMilestoneHold(ctx context.Context, deal *Deal, shareToken string, m *Milestone, walletURL, walletKey string) (*PublicDeal, error) {
	if m.DealID != deal.ID {
		return nil, ErrMilestoneNotFound
	}
	if m.Status != MilestoneAwaitingPayment && m.Status != MilestonePending {
		return nil, fmt.Errorf("%w: milestone %d is %s", ErrInvalidTransition, m.Order, m.Status)
	}

	var err error
	if m.CheckingID == "" || m.Invoice == "" || m.PreimageHash == "" {
		if err = s.FundMilestone(ctx, deal, m); err != nil {
			return nil, err
		}
		m, err = s.repo.GetMilestoneByID(ctx, m.ID)
		if err != nil {
			return nil, err
		}
	}
	if err := s.refreshMilestonePayment(ctx, m); err != nil {
		return nil, fmt.Errorf("checking escrow hold: %w", err)
	}
	m, err = s.repo.GetMilestoneByID(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	if m.Status != MilestoneAwaitingPayment && m.Status != MilestonePending {
		return nil, fmt.Errorf("%w: milestone %d is already %s", ErrInvalidTransition, m.Order, m.Status)
	}

	wallet := lnbits.NewClient(lnbits.Config{URL: walletURL, AdminKey: walletKey})
	if _, err := wallet.PayInvoice(ctx, m.Invoice); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWalletPaymentFailed, err)
	}
	accepted, err := s.acceptedHolds.IsAcceptedHold(ctx, m.PreimageHash, m.AmountSats)
	if err != nil {
		return nil, fmt.Errorf("verifying accepted hold: %w", err)
	}
	if !accepted {
		return nil, fmt.Errorf("%w: escrow node did not accept the milestone hold", ErrInvalidTransition)
	}
	if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneFunded, nil); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneInProgress, nil); err != nil {
		return nil, err
	}
	_, _ = s.repo.UpdateStatusIfCurrent(ctx, deal.ID, StatusAwaitingPayment, StatusLocked)
	return s.GetPublicDeal(ctx, shareToken)
}

func (s *Service) fundDealHold(ctx context.Context, deal *Deal, shareToken, walletURL, walletKey string) (*PublicDeal, error) {
	if deal.Status == StatusLocked {
		return s.GetPublicDeal(ctx, shareToken)
	}
	if deal.Status != StatusAwaitingPayment {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusLocked)
	}
	if deal.CheckingID == "" {
		return nil, ErrNoCheckingID
	}
	if deal.Invoice == "" || deal.PreimageHash == "" {
		return nil, fmt.Errorf("%w: deal has no escrow invoice", ErrInvalidInput)
	}
	if err := s.refreshPaymentStatus(ctx, deal); err != nil {
		return nil, fmt.Errorf("checking escrow hold: %w", err)
	}
	deal, err := s.repo.GetDealByID(ctx, deal.ID)
	if err != nil {
		return nil, err
	}
	if deal.Status == StatusLocked {
		return s.GetPublicDeal(ctx, shareToken)
	}
	if deal.Status != StatusAwaitingPayment {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusLocked)
	}

	wallet := lnbits.NewClient(lnbits.Config{URL: walletURL, AdminKey: walletKey})
	if _, err := wallet.PayInvoice(ctx, deal.Invoice); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWalletPaymentFailed, err)
	}
	accepted, err := s.acceptedHolds.IsAcceptedHold(ctx, deal.PreimageHash, deal.AmountSats)
	if err != nil {
		return nil, fmt.Errorf("verifying accepted hold: %w", err)
	}
	if !accepted {
		return nil, fmt.Errorf("%w: escrow node did not accept the hold", ErrInvalidTransition)
	}
	transitioned, err := s.repo.UpdateStatusIfCurrent(ctx, deal.ID, StatusAwaitingPayment, StatusLocked)
	if err != nil {
		return nil, err
	}
	if !transitioned {
		current, getErr := s.repo.GetDealByID(ctx, deal.ID)
		if getErr != nil {
			return nil, getErr
		}
		if current.Status != StatusLocked {
			return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, StatusLocked)
		}
		return s.GetPublicDeal(ctx, shareToken)
	}
	deal.Status = StatusLocked
	s.notifyPaymentLocked(deal)
	return s.GetPublicDeal(ctx, shareToken)
}

// SweepExpiredHolds reconciles DB state with LNbits for old open deals.
// When a hold invoice expires (or was never funded and is now UNPAID /
// EXPIRED / CANCELLED), any committed funds have already returned to the
// client on the Lightning network — so the deal should be recorded as
// refunded instead of sitting in awaiting_payment/locked/work_submitted/
// reviewing/disputed forever. Holds that LNbits still reports as held (or
// settled) are left untouched. Returns the number of deals reconciled to
// refunded.
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
			// Guard against racing an approve/refund: only sweep if the deal is
			// still in the state this sweep read (awaiting_payment / locked /
			// work_submitted / reviewing / disputed).
			transitioned, err := s.repo.UpdateStatusIfCurrent(ctx, deal.ID, deal.Status, StatusRefunded)
			if err != nil || !transitioned {
				// Another caller already moved the deal (e.g. just approved it).
				continue
			}
			deal.Status = StatusRefunded
			s.notifyRefunded(deal)
			swept++
		default:
			// HOLD / ACCEPTED / SETTLED — funds still committed.
		}
	}

	return swept, nil
}

// PublicApproveDeal allows a client to approve a deal from the public share link.
// Sequential milestone mode: releases the active milestone escrow (if present) and advances/funds next; final milestone completes deal.
// Legacy mode (no milestones / deal-level escrow): releases the single deal hold.
func (s *Service) PublicApproveDeal(ctx context.Context, shareToken string) (*Deal, error) {
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

	ms, _ := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if len(ms) > 0 {
		for i := range ms {
			_ = s.refreshMilestonePayment(ctx, &ms[i])
		}
		ms, _ = s.repo.GetMilestonesByDeal(ctx, deal.ID)
		// find active milestone to approve/release (disputed requires independent review resolution)
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
				if ms[i].Status == MilestoneAwaitingPayment || ms[i].Status == MilestonePending {
					return nil, fmt.Errorf("%w: milestone %d is not funded/submitted for approval", ErrInvalidTransition, ms[i].Order)
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
		// return fresh deal view
		fresh, _ := s.repo.GetDealByID(ctx, deal.ID)
		if fresh != nil {
			return fresh, nil
		}
		return deal, nil
	}

	// Legacy single-hold flow
	if deal.CheckingID == "" && deal.Status == StatusAwaitingPayment {
		return nil, fmt.Errorf("%w: deal has no escrow to approve (fund it first)", ErrInvalidTransition)
	}
	if deal.Status != StatusLocked && deal.Status != StatusWorkSubmitted {
		return nil, fmt.Errorf("%w: deal cannot be approved from status %s", ErrInvalidTransition, deal.Status)
	}

	return s.releaseEscrow(ctx, deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error {
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

func (s *Service) PublicResetPayout(ctx context.Context, shareToken, milestoneID string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	repo := s.repo.WithTx(tx)

	if milestoneID != "" {
		m, err := repo.GetMilestoneForUpdate(ctx, milestoneID)
		if err != nil {
			return nil, err
		}
		if m.DealID != deal.ID {
			return nil, ErrMilestoneNotFound
		}
		if m.PayoutCheckingID != "" {
			return nil, fmt.Errorf("%w: milestone payout %s is recorded; operator reconciliation required", ErrPayoutInFlight, m.PayoutCheckingID)
		}
		if !m.PayoutAttemptedAt.Valid {
			return nil, fmt.Errorf("%w: milestone %d has no unconfirmed payout attempt", ErrInvalidInput, m.Order)
		}
		if !canResetMilestonePayoutAttempt(m) {
			return nil, fmt.Errorf("%w: milestone %d cannot reset payout from status %s", ErrInvalidTransition, m.Order, m.Status)
		}
		if err := repo.ClearMilestonePayoutTracking(ctx, m.ID); err != nil {
			return nil, fmt.Errorf("resetting milestone payout tracking %s: %w", m.ID, err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit transaction: %w", err)
		}
		return s.GetPublicDeal(ctx, shareToken)
	}

	locked, err := repo.GetDealForUpdate(ctx, deal.ID)
	if err != nil {
		return nil, err
	}
	if locked.PayoutCheckingID != "" {
		return nil, fmt.Errorf("%w: payout %s is recorded; operator reconciliation required", ErrPayoutInFlight, locked.PayoutCheckingID)
	}
	if !locked.PayoutAttemptedAt.Valid {
		return nil, fmt.Errorf("%w: deal has no unconfirmed payout attempt", ErrInvalidInput)
	}
	if !canResetDealPayoutAttempt(locked) {
		return nil, fmt.Errorf("%w: deal cannot reset payout from status %s", ErrInvalidTransition, locked.Status)
	}
	if err := repo.ClearPayoutTracking(ctx, locked.ID); err != nil {
		return nil, fmt.Errorf("resetting payout tracking for deal %s: %w", locked.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	return s.GetPublicDeal(ctx, shareToken)
}

func (s *Service) ExitDeal(ctx context.Context, dealID string) error {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return err
	}
	return s.exitDeal(ctx, deal)
}

func (s *Service) PublicExitDeal(ctx context.Context, shareToken string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}
	if err := s.exitDeal(ctx, deal); err != nil {
		return nil, err
	}
	return s.GetPublicDeal(ctx, shareToken)
}

func (s *Service) exitDeal(ctx context.Context, deal *Deal) error {
	milestones, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for i := range milestones {
		m := milestones[i]
		if m.Status == MilestoneReleased || m.Status == MilestoneRefunded || m.Status == MilestoneExited {
			continue
		}
		switch m.Status {
		case MilestoneAwaitingPayment, MilestoneFunded, MilestoneInProgress, MilestonePending:
			// Not submitted/approved: not paid to freelancer; cancel/refund hold (symmetric).
			if m.PreimageHash != "" && m.Status != MilestonePending {
				if err := s.cancelMilestoneHold(ctx, &m); err != nil {
					log.Printf("warn: cancel milestone hold on exit deal=%s milestone=%s: %v", deal.ID, m.ID, err)
					// Best-effort exit: a failed cancel does not block the
					// exit, but the milestone is still marked refunded so the
					// deal can terminate; hold-expiry sweep is the backstop for
					// an old hold that could not be cancelled. Mirrors the
					// milestone-mode exit contract in the redesign notes.
				}
				if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneRefunded, &now); err != nil {
					return err
				}
			} else {
				if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneExited, &now); err != nil {
					return err
				}
			}
		case MilestoneWorkSubmitted, MilestoneCompleted, MilestoneRejected:
			// Material consequences: do NOT move money. Hold stays; force independent review/dispute resolution.
			if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneDisputed, &now); err != nil {
				return err
			}
			if deal.DisputeReason == "" {
				deal.DisputeReason = "exit requested while milestone requires review (post-submission)"
				_, _ = s.repo.UpdateDisputeIfCurrent(ctx, deal.ID, deal.Status, deal.DisputeReason)
			}
		case MilestoneDisputed:
			// already awaiting resolution; keep hold, no money move
		default:
			_ = s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneExited, &now)
		}
	}

	if len(milestones) == 0 && deal.PreimageHash != "" && deal.Status != StatusReleased && deal.Status != StatusRefunded {
		if deal.Status == StatusWorkSubmitted || deal.Status == StatusReviewing || deal.Status == StatusDisputed {
			if deal.DisputeReason == "" {
				_, _ = s.repo.UpdateDisputeIfCurrent(ctx, deal.ID, deal.Status, "exit requested after submission (requires review)")
			}
		} else {
			_ = s.cancelEscrowHold(ctx, deal)
			_ = s.repo.UpdateStatus(ctx, deal.ID, StatusRefunded)
		}
	}

	s.notifyExited(deal)
	return nil
}

func (s *Service) prepareMilestoneHold(ctx context.Context, deal *Deal, m *Milestone) error {
	if m.AmountSats <= 0 {
		m.AmountSats = deal.AmountSats // fallback (legacy/single)
	}
	payee := m.PayeeInvoice
	if payee == "" {
		payee = deal.PayeeInvoice
	}
	preimage := make([]byte, 32)
	if _, err := rand.Read(preimage); err != nil {
		return fmt.Errorf("generating milestone preimage: %w", err)
	}
	hash := sha256.Sum256(preimage)
	m.Preimage = hex.EncodeToString(preimage[:])
	m.PreimageHash = hex.EncodeToString(hash[:])
	m.PayeeInvoice = payee
	hold, err := s.lnbits.CreateHoldInvoice(ctx, lnbits.CreateHoldInvoiceRequest{
		Out: false, Amount: m.AmountSats, Memo: fmt.Sprintf("%s — M%d: %s", deal.Title, m.Order, m.Title), PaymentHash: m.PreimageHash,
	})
	if err != nil {
		return fmt.Errorf("creating milestone hold invoice: %w", err)
	}
	m.Invoice = hold.PaymentRequest
	m.CheckingID = hold.CheckingID
	m.Status = MilestoneAwaitingPayment
	return nil
}

// FundMilestone creates a hold invoice (escrow leg) for a milestone and stores it.
// Sequential per-milestone holds: client pays this invoice to fund the milestone.
func (s *Service) FundMilestone(ctx context.Context, deal *Deal, m *Milestone) error {
	if err := s.prepareMilestoneHold(ctx, deal, m); err != nil {
		return err
	}
	if err := s.repo.UpdateMilestoneEscrow(ctx, m); err != nil {
		// best-effort cancel orphan
		_, _ = s.lnbits.CancelHold(ctx, m.PreimageHash)
		return err
	}
	return nil
}

func (s *Service) refreshMilestonePayment(ctx context.Context, m *Milestone) error {
	if m.CheckingID == "" || m.Status != MilestoneAwaitingPayment {
		return nil
	}
	payment, err := s.lnbits.CheckPayment(ctx, m.CheckingID)
	if err != nil {
		return err
	}

	funded := payment.Paid
	if !funded && s.acceptedHolds != nil && m.PreimageHash != "" {
		funded, err = s.acceptedHolds.IsAcceptedHold(ctx, m.PreimageHash, m.AmountSats)
		if err != nil {
			return err
		}
	}

	if funded {
		m.Status = MilestoneFunded
		if err := s.repo.UpdateMilestoneEscrow(ctx, m); err == nil {
			// best-effort: move to in_progress if it was just funded
			_ = s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneInProgress, nil)
			m.Status = MilestoneInProgress
		}
	}
	return nil
}

// ReleaseMilestone settles the milestone's hold and pays the freelancer, then marks released.

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

func (s *Service) PublicRejectMilestone(ctx context.Context, shareToken, milestoneID, reason string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalidInput)
	}
	if length := len([]rune(reason)); length > maxDisputeReasonRunes {
		return nil, fmt.Errorf("%w: reason must be at most %d characters", ErrInvalidInput, maxDisputeReasonRunes)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
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
	// Rejected after submission: DO NOT auto-refund. Hold stays; force independent review/dispute.
	// Only allow rejecting submitted/completed (material) work.
	if m.Status != MilestoneWorkSubmitted && m.Status != MilestoneCompleted {
		return nil, fmt.Errorf("%w: milestone cannot be rejected from status %s (submit work or await review)", ErrInvalidTransition, m.Status)
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateMilestoneStatus(ctx, milestoneID, MilestoneDisputed, &now); err != nil {
		return nil, err
	}
	_, _ = s.repo.UpdateDisputeIfCurrent(ctx, deal.ID, deal.Status, reason)
	s.notifyDisputed(deal)
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
