package deals

import (
	"context"
	"fmt"
)

// settleMilestoneHoldGuarded mirrors settleHoldGuarded for a milestone hold.
func (s *Service) settleMilestoneHoldGuarded(ctx context.Context, m *Milestone) error {
	if m.Preimage == "" {
		return fmt.Errorf("%w: cannot release a milestone without an escrow preimage", ErrInvalidInput)
	}
	if _, err := s.lnbits.SettleHold(ctx, m.Preimage); err != nil {
		payment, checkErr := s.lnbits.CheckPayment(ctx, m.CheckingID)
		if checkErr != nil || !isHoldSettled(payment.Details.Status) {
			return fmt.Errorf("settling milestone escrow %s: %w", m.ID, err)
		}
	}
	return nil
}

// cancelMilestoneHold mirrors cancelEscrowHold for a milestone hold.
func (s *Service) cancelMilestoneHold(ctx context.Context, m *Milestone) error {
	if m.PreimageHash == "" {
		return fmt.Errorf("%w: cannot refund a milestone without a preimage hash", ErrInvalidInput)
	}
	if _, err := s.lnbits.CancelHold(ctx, m.PreimageHash); err != nil {
		payment, checkErr := s.lnbits.CheckPayment(ctx, m.CheckingID)
		if checkErr != nil || !isHoldReleased(payment.Details.Status) {
			return fmt.Errorf("cancelling milestone escrow %s: %w", m.ID, err)
		}
	}
	return nil
}

// settleHoldGuarded settles the escrow hold, treating "already settled" as
// success: LNbits refuses the second settle, but a CheckPayment confirming the
// hold is SETTLED proves the funds arrived. Anything still held surfaces an
// error so the payout leg never runs on unsettled escrow.
func (s *Service) settleHoldGuarded(ctx context.Context, deal *Deal) error {
	if deal.Preimage == "" {
		return fmt.Errorf("%w: cannot release a deal without an escrow preimage", ErrInvalidInput)
	}

	if _, err := s.lnbits.SettleHold(ctx, deal.Preimage); err != nil {
		payment, checkErr := s.lnbits.CheckPayment(ctx, deal.CheckingID)
		if checkErr != nil || !isHoldSettled(payment.Details.Status) {
			return fmt.Errorf("settling escrow for deal %s: %w", deal.ID, err)
		}
	}

	return nil
}
