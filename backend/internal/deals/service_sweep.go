package deals

import (
	"context"
	"time"
)

// SweepExpiredHolds reconciles DB state with LNbits for old open deals.
// When a hold invoice expires (or was never funded and is now UNPAID /
// EXPIRED / CANCELLED), any committed funds have already returned to the
// client on the Lightning network — so the deal should be recorded as
// refunded instead of sitting in awaiting_payment/locked/work_submitted/
// reviewing/disputed forever. Holds that LNbits still reports as held (or
// settled) are left untouched. Returns the number of deals reconciled to
// refunded.
func (s *Service) SweepExpiredHolds(ctx context.Context, cutoff time.Time) (int, error) {
	open, err := s.repo.ListOpenBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}

	swept := 0
	for i := range open {
		deal := &open[i]
		if deal.CheckingID == "" {
			continue
		}

		payment, err := s.lnbits.CheckPayment(ctx, deal.CheckingID)
		if err != nil {
			// LNbits unreachable — leave the deal for the next sweep.
			continue
		}

		switch payment.Details.Status {
		case "UNPAID", "EXPIRED", "CANCELLED":
			// Guard against racing an approve/refund: only sweep if the deal is
			// still in the state this sweep read (awaiting_payment / locked /
			// work_submitted / reviewing / disputed).
			transitioned, err := s.repo.UpdateStatusIfCurrent(ctx, deal.ID, deal.Status, StatusRefunded)
			if err != nil || !transitioned {
				// Another caller already moved the deal (e.g. just approved it).
				continue
			}
			deal.Status = StatusRefunded
			s.notifyRefunded(deal)
			swept++
		default:
			// HOLD / ACCEPTED / SETTLED — funds still committed.
		}
	}

	return swept, nil
}
