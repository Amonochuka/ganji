package deals

import (
	"context"
	"fmt"
	"log"
)

// clearPayoutAttempt resets the payout tracking (autocommit, off the money
// path). Only called when the payout is provably not in flight.
func (s *Service) clearPayoutAttempt(ctx context.Context, dealID string) error {
	if err := s.repo.ClearPayoutTracking(ctx, dealID); err != nil {
		return fmt.Errorf("resetting payout tracking for deal %s: %w", dealID, err)
	}
	return nil
}

// anchorReleasedDeal writes the Live CV anchors for accepted work. Best-effort
// and deliberately off the money path: a failed anchor must never roll back a
// completed release, and the public CV self-heals any missing anchors on its
// next read anyway.
func (s *Service) anchorReleasedDeal(ctx context.Context, deal *Deal) {
	if s.cv == nil {
		return
	}
	if err := s.cv.AnchorReleasedDeal(ctx, deal.FreelancerID, deal.ID); err != nil {
		log.Printf("cv: anchoring released deal %s: %v", deal.ID, err)
	}
}
