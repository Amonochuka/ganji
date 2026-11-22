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

	ms, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, fmt.Errorf("%w: deal has no milestones", ErrInvalidTransition)
	}

	targetID := milestoneID
	if targetID == "" {
		for i := range ms {
			if ms[i].PayoutCheckingID != "" && ms[i].PayoutAttemptedAt.Valid {
				return nil, fmt.Errorf("%w: milestone payout %s is recorded; operator reconciliation required", ErrPayoutInFlight, ms[i].PayoutCheckingID)
			}
			if ms[i].PayoutCheckingID == "" && ms[i].PayoutAttemptedAt.Valid && canResetMilestonePayoutAttempt(&ms[i]) {
				if targetID != "" {
					return nil, fmt.Errorf("%w: milestone_id is required when multiple payout attempts can be reset", ErrInvalidInput)
				}
				targetID = ms[i].ID
			}
		}
		if targetID == "" {
			for i := range ms {
				if ms[i].PayoutAttemptedAt.Valid && !canResetMilestonePayoutAttempt(&ms[i]) && (ms[i].Status == MilestoneReleased || ms[i].Status == MilestoneRefunded) {
					return nil, fmt.Errorf("%w: milestone %d cannot reset payout from status %s", ErrInvalidTransition, ms[i].Order, ms[i].Status)
				}
			}
			return nil, fmt.Errorf("%w: deal has no unconfirmed milestone payout attempt", ErrInvalidInput)
		}
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	repo := s.repo.WithTx(tx)

	m, err := repo.GetMilestoneForUpdate(ctx, targetID)
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
		if m.Status == MilestoneReleased || m.Status == MilestoneRefunded {
			return nil, fmt.Errorf("%w: milestone %d cannot reset payout from status %s", ErrInvalidTransition, m.Order, m.Status)
		}
		return nil, fmt.Errorf("%w: milestone %d has no unconfirmed payout attempt", ErrInvalidInput, m.Order)
	}
	if err := repo.ClearMilestonePayoutTracking(ctx, m.ID); err != nil {
		return nil, fmt.Errorf("resetting milestone payout tracking %s: %w", m.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	return s.GetPublicDeal(ctx, shareToken)
}
