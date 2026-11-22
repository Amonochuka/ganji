package deals

import (
	"context"
	"fmt"
)

// CheckPayment refreshes milestone/deal funding status by checking accepted
// holds on the escrow node. LNbits paid=true is not proof that funds are locked.
func (s *Service) CheckPayment(ctx context.Context, userID, dealID string) (*Deal, error) {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	ms, err := s.repo.GetMilestonesByDeal(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, fmt.Errorf("%w: deal has no milestones", ErrInvalidTransition)
	}
	for i := range ms {
		if err := s.refreshMilestonePayment(ctx, &ms[i]); err != nil {
			return nil, fmt.Errorf("checking payment with lnbits: %w", err)
		}
	}
	fresh, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}
	return fresh, nil
}
