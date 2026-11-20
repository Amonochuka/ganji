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

// checkOutgoingPayout reports whether an outgoing payout payment is confirmed,
// failed, or still ambiguous. It reports:
//   - confirmed=true when the payout is verifiably done (paid + a terminal
//     success status);
//   - failed=true when the payout is verifiably NOT done (a terminal failure
//     status);
//   - otherwise ambiguous — the payout may still be in flight.
func (s *Service) checkOutgoingPayout(ctx context.Context, payoutCheckingID string) (confirmed, failed bool, err error) {
	payment, err := s.lnbits.CheckPayment(ctx, payoutCheckingID)
	if err != nil {
		return false, false, err
	}
	if payment.Paid && isPayoutConfirmed(payment.Details.Status) {
		return true, false, nil
	}
	if isPayoutFailed(payment.Details.Status) {
		return false, true, nil
	}
	return false, false, nil
}
