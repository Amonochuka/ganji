package deals

import (
	"context"
	"fmt"
	"strings"
)

// UpdatePayeeInvoice lets the freelancer replace the payout destination while
// the deal is still open. If the original payee invoice expired mid-deal the
// approve-time payout leg would fail and leave the escrow settled but the
// deal unreleased; a fresh invoice here unsticks it — re-approving then
// forwards the funds to the new invoice. Frozen once the deal is released or
// refunded.
func (s *Service) UpdatePayeeInvoice(ctx context.Context, userID, dealID, bolt11 string) (*Deal, error) {
	dealID = strings.TrimSpace(dealID)
	bolt11 = strings.TrimSpace(bolt11)

	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	if bolt11 == "" {
		return nil, fmt.Errorf("%w: payee invoice is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	switch deal.Status {
	case StatusReleased, StatusRefunded:
		return nil, fmt.Errorf("%w: payee invoice is frozen once the deal is %s", ErrInvalidInput, deal.Status)
	}

	ms, err := s.repo.GetMilestonesByDeal(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, fmt.Errorf("%w: deal has no milestones", ErrInvalidTransition)
	}
	for i := range ms {
		if ms[i].Status == MilestoneReleased || ms[i].Status == MilestoneRefunded || ms[i].Status == MilestoneExited {
			continue
		}
		if err := s.validatePayeeInvoiceAmount(ctx, bolt11, ms[i].AmountSats); err != nil {
			return nil, err
		}
	}

	if err := s.repo.UpdatePayeeInvoice(ctx, dealID, bolt11); err != nil {
		return nil, fmt.Errorf("updating payee invoice: %w", err)
	}
	if err := s.repo.UpdateMilestonePayeeInvoices(ctx, dealID, bolt11); err != nil {
		return nil, fmt.Errorf("updating milestone payee invoices: %w", err)
	}

	deal.PayeeInvoice = bolt11
	return deal, nil
}
