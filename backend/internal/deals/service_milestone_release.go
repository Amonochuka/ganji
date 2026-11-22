package deals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

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
			settled, settleErr := s.isMilestoneHoldSettled(ctx, m)
			if settleErr != nil {
				return nil, fmt.Errorf("%w: verifying settled hold for milestone %s: %v", ErrPayoutInFlight, m.ID, settleErr)
			}
			if settled {
				if err := repo.ClearMilestonePayoutTracking(ctx, m.ID); err != nil {
					return nil, fmt.Errorf("resetting milestone payout tracking %s: %w", m.ID, err)
				}
				payoutCheckingID = ""
				m.PayoutCheckingID = ""
				m.PayoutAttemptedAt = sql.NullTime{}
			} else {
				return nil, fmt.Errorf("%w: milestone payout %s for %s was refused by LNbits", ErrPayoutFailed, m.PayoutCheckingID, m.ID)
			}
		default:
			return nil, fmt.Errorf("%w: milestone payout %s for %s is not confirmed", ErrPayoutInFlight, m.PayoutCheckingID, m.ID)
		}
	case m.PayoutAttemptedAt.Valid:
		settled, settleErr := s.isMilestoneHoldSettled(ctx, m)
		if settleErr != nil {
			return nil, fmt.Errorf("%w: verifying settled hold for milestone %s: %v", ErrPayoutInFlight, m.ID, settleErr)
		}
		if !settled {
			return nil, fmt.Errorf("%w: milestone %s has an unconfirmed payout attempt", ErrPayoutInFlight, m.ID)
		}
		confirmed = false
		payoutCheckingID = ""
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
				// Keep attempt marker: hold may be settled and funds are in escrow.
				return nil, fmt.Errorf("%w: paying freelancer for milestone %s: %v", ErrPayoutFailed, m.ID, err)
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
			// Keep marker: escrow may already be settled; explicit reconciliation needed.
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
	s.anchorReleasedDeal(ctx, deal)
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

// ReconcileMilestonePayout is the operator backstop for a hung milestone payout.
// confirm_payout: verifies LNbits outgoing payout is paid/terminal success,
// records payout_checking_id and releases milestone without moving money.
// reset_payout: clears an unconfirmed attempt marker so normal release can retry.
func (s *Service) ReconcileMilestonePayout(ctx context.Context, operatorEmail, dealID, milestoneID string, action ReconcileAction, payoutCheckingID string) (*Milestone, error) {
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

	switch action {
	case ReconcileConfirmPayout:
		payoutCheckingID = strings.TrimSpace(payoutCheckingID)
		if payoutCheckingID == "" {
			return nil, fmt.Errorf("%w: payout_checking_id is required for confirm_payout", ErrInvalidInput)
		}
		if m.Status == MilestoneReleased {
			if m.PayoutCheckingID == payoutCheckingID || m.PayoutCheckingID == "" {
				_ = s.repo.UpdateMilestonePayout(ctx, m.ID, payoutCheckingID)
				m.PayoutCheckingID = payoutCheckingID
				return m, nil
			}
			return nil, fmt.Errorf("%w: milestone already released with different payout %s", ErrInvalidTransition, m.PayoutCheckingID)
		}
		if !CanMilestoneTransition(m.Status, MilestoneReleased) {
			return nil, fmt.Errorf("%w: milestone cannot be released from status %s", ErrInvalidTransition, m.Status)
		}
		ok, failed, err := s.checkOutgoingPayout(ctx, payoutCheckingID)
		if err != nil {
			return nil, fmt.Errorf("%w: verifying payout %s: %v", ErrPayoutInFlight, payoutCheckingID, err)
		}
		if !ok || failed {
			return nil, fmt.Errorf("%w: payout %s is not confirmed as paid", ErrInvalidInput, payoutCheckingID)
		}
		settled, err := s.isMilestoneHoldSettled(ctx, m)
		if err != nil || !settled {
			return nil, fmt.Errorf("%w: milestone hold is not settled; cannot confirm payout without settled escrow", ErrInvalidInput)
		}
		now := time.Now().UTC()
		if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneReleased, &now); err != nil {
			return nil, err
		}
		if err := s.repo.UpdateMilestonePayout(ctx, m.ID, payoutCheckingID); err != nil {
			return nil, err
		}
		m.Status = MilestoneReleased
		m.PayoutCheckingID = payoutCheckingID
		m.CompletedAt = sql.NullTime{Time: now, Valid: true}
		s.anchorReleasedDeal(ctx, deal)
		// advance sequence/finalize deal like release path
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
		} else if idx >= 0 {
			_ = s.repo.UpdateStatus(ctx, deal.ID, StatusReleased)
			s.notifyReleased(deal)
		}
		return m, nil

	case ReconcileResetPayout:
		if m.PayoutCheckingID != "" {
			return nil, fmt.Errorf("%w: milestone payout %s is recorded; cannot reset", ErrPayoutInFlight, m.PayoutCheckingID)
		}
		if !m.PayoutAttemptedAt.Valid {
			return nil, fmt.Errorf("%w: milestone has no unconfirmed payout attempt", ErrInvalidInput)
		}
		if !canResetMilestonePayoutAttempt(m) {
			return nil, fmt.Errorf("%w: milestone cannot reset payout from status %s", ErrInvalidTransition, m.Status)
		}
		if err := s.repo.ClearMilestonePayoutTracking(ctx, m.ID); err != nil {
			return nil, err
		}
		m.PayoutCheckingID = ""
		m.PayoutAttemptedAt = sql.NullTime{}
		return m, nil

	default:
		return nil, fmt.Errorf("%w: reconcile action must be %q or %q", ErrInvalidInput, ReconcileConfirmPayout, ReconcileResetPayout)
	}
}
