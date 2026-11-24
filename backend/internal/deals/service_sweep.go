package deals

import (
	"context"
	"time"
)

// SweepExpiredHolds reconciles milestone hold state with LNbits for open deals.
// When a hold invoice expires/cancels/unpaid, committed funds have returned to
// the client on Lightning; mark the milestone refunded. Holds still
// HOLD/ACCEPTED/SETTLED are left untouched. Per-milestone holds may have
// different expiry windows, so sweep checks all open deals. Returns the number
// of milestones reconciled to refunded.
func (s *Service) SweepExpiredHolds(ctx context.Context) (int, error) {
	open, err := s.repo.ListOpenBefore(ctx, time.Time{})
	if err != nil {
		return 0, err
	}

	swept := 0
	for i := range open {
		ms, err := s.repo.GetMilestonesByDeal(ctx, open[i].ID)
		if err != nil {
			continue
		}
		for j := range ms {
			m := ms[j]
			if m.CheckingID == "" {
				continue
			}
			switch m.Status {
			case MilestoneReleased, MilestoneRefunded:
				continue
			}
			payment, err := s.lnbits.CheckPayment(ctx, m.CheckingID)
			if err != nil {
				continue
			}
			switch payment.Details.Status {
			case "UNPAID", "EXPIRED", "CANCELLED":
				// Guard against concurrent approve/refund by only transitioning
				// from the status we observed.
				transitioned, err := s.repo.UpdateMilestoneStatusIfCurrent(ctx, m.ID, m.Status, MilestoneRefunded)
				if err != nil || !transitioned {
					continue
				}
				if _, err := s.repo.UpdateDealStatusFromMilestones(ctx, m.DealID); err == nil {
					if deal, derr := s.repo.GetDealByID(ctx, m.DealID); derr == nil {
						s.notifyRefunded(deal)
					}
				}
				swept++
			default:
				// HOLD / ACCEPTED / SETTLED — funds still committed.
			}
		}
	}

	return swept, nil
}
