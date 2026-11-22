package deals

import (
	"context"
	"fmt"
	"log"
)

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

func (s *Service) isMilestoneHoldSettled(ctx context.Context, m *Milestone) (bool, error) {
	if m == nil || m.CheckingID == "" {
		return false, fmt.Errorf("%w: milestone has no escrow invoice", ErrInvalidInput)
	}
	payment, err := s.lnbits.CheckPayment(ctx, m.CheckingID)
	if err != nil {
		return false, err
	}
	return isHoldSettled(payment.Details.Status), nil
}
