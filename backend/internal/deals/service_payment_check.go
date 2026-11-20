package deals

import (
	"context"
	"fmt"
)

// CheckPayment queries the escrow node for an accepted hold on a deal's
// invoice. LNbits paid=true is not proof that funds are locked, so the deal
// only transitions from awaiting_payment to locked after the escrow node
// reports an accepted hold under the deal's payment hash.
func (s *Service) CheckPayment(ctx context.Context, userID, dealID string) (*Deal, error) {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	if deal.CheckingID == "" {
		return nil, ErrNoCheckingID
	}

	if err := s.refreshPaymentStatus(ctx, deal); err != nil {
		return nil, fmt.Errorf("checking payment with lnbits: %w", err)
	}

	return deal, nil
}

// refreshPaymentStatus confirms the invoice with LNbits, asks the escrow node
// for an accepted hold, and moves an awaiting_payment deal to locked only once
// funds are committed. Verification errors are returned so callers cannot
// mistake an unknown state for funding.
func (s *Service) refreshPaymentStatus(ctx context.Context, deal *Deal) error {
	if deal.Status != StatusAwaitingPayment {
		return nil
	}
	if _, err := s.lnbits.CheckPayment(ctx, deal.CheckingID); err != nil {
		return err
	}
	if s.acceptedHolds == nil || deal.PreimageHash == "" {
		return ErrVerificationUnavailable
	}

	accepted, err := s.acceptedHolds.IsAcceptedHold(ctx, deal.PreimageHash, deal.AmountSats)
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}

	transitioned, err := s.repo.UpdateStatusIfCurrent(ctx, deal.ID, StatusAwaitingPayment, StatusLocked)
	if err != nil {
		return err
	}
	if transitioned {
		deal.Status = StatusLocked
		s.notifyPaymentLocked(deal)
	}

	return nil
}
