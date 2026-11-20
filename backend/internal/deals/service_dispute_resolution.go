package deals

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

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
