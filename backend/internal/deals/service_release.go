package deals

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

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
			settled, settleErr := s.isDealHoldSettled(ctx, deal)
			if settleErr != nil {
				return nil, fmt.Errorf("%w: verifying settled hold for deal %s: %v", ErrPayoutInFlight, deal.ID, settleErr)
			}
			if settled {
				// Hold settled but prior payout definitively failed: funds remain in
				// escrow wallet. Clear failed tracking so payout leg can be retried.
				if err := repo.ClearPayoutTracking(ctx, deal.ID); err != nil {
					return nil, fmt.Errorf("resetting payout tracking for deal %s: %w", deal.ID, err)
				}
				payoutCheckingID = ""
			} else {
				return nil, fmt.Errorf("%w: payout %s for deal %s was refused by LNbits", ErrPayoutFailed, deal.PayoutCheckingID, deal.ID)
			}
		default:
			// Still pending/in flight with no verdict. Do not resend.
			return nil, fmt.Errorf("%w: payout %s for deal %s is not confirmed",
				ErrPayoutInFlight, deal.PayoutCheckingID, deal.ID)
		}
	case deal.PayoutAttemptedAt.Valid:
		// A previous release recorded an attempt but never confirmed payout.
		// If the escrow hold is already settled, funds are in escrow wallet and
		// we may safely retry only the payout leg (settle is idempotent/guarded).
		// If hold state is unknown/not settled, keep this ambiguous to avoid
		// double-paying or releasing without settlement.
		settled, settleErr := s.isDealHoldSettled(ctx, deal)
		if settleErr != nil {
			return nil, fmt.Errorf("%w: verifying settled hold for deal %s: %v", ErrPayoutInFlight, deal.ID, settleErr)
		}
		if !settled {
			return nil, fmt.Errorf("%w: deal %s has an unconfirmed payout attempt", ErrPayoutInFlight, deal.ID)
		}
		confirmed = false
		payoutCheckingID = ""
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
				// Payout provably not sent, but escrow hold may already be settled
				// (e.g. expired/canceled payee invoice after settle). Keep the
				// attempt marker to prevent unsafe auto-resend/release ambiguity;
				// operator/freelancer must fix payee destination and retry reconcile.
				return nil, fmt.Errorf("%w: paying freelancer for deal %s: %v", ErrPayoutFailed, deal.ID, err)
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
			// Definitively failed payout after an attempt was recorded. Keep the
			// attempt marker when escrow may already be settled; this blocks
			// unsafe implicit retries and forces explicit reconciliation/retry.
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
