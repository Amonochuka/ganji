package deals

import (
	"context"
	"fmt"
)

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
