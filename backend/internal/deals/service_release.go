package deals

import (
	"context"
	"fmt"
	"strings"
)

// ApproveDeal settles the escrow on the network and forwards the funds to
// the freelancer. Only the client (matched by the client_email recorded on
// the deal) can approve. Because released is a terminal state, approving
// also stamps verified_at for the Live CV.
//
// The two network legs are deliberate and non-atomic:
//  1. SettleHold reveals the preimage, completing the held payment so the
//     sats land in Ganji's LNbits wallet.
//  2. PayInvoice forwards those sats onward to the freelancer's invoice.
//
// The release is two-phase and conservative about payouts (see releaseEscrow
// and explained.md §3). In a nutshell: a durable payout_attempted_at marker
// is committed BEFORE any money moves, the network legs run outside any DB
// lock, and a retry never auto-resends — a verified payout releases without
// re-paying, a provably-failed one is cleared and retried, and anything
// ambiguous surfaces as ErrPayoutInFlight for manual reconciliation.
func (s *Service) ApproveDeal(ctx context.Context, email, dealID string) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	ms, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		_ = s.refreshMilestonePayment(ctx, &ms[i])
	}
	ms, _ = s.repo.GetMilestonesByDeal(ctx, deal.ID)
	idx := -1
	for i := range ms {
		st := ms[i].Status
		if st == MilestoneWorkSubmitted || st == MilestoneInProgress || st == MilestoneFunded || st == MilestoneCompleted {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i := range ms {
			if ms[i].Status == MilestoneDisputed || ms[i].Status == MilestoneRejected {
				return nil, fmt.Errorf("%w: milestone %d is under review/dispute; independent resolution required", ErrInvalidTransition, ms[i].Order)
			}
		}
		return nil, fmt.Errorf("%w: no milestone available for approval", ErrInvalidTransition)
	}
	target := ms[idx]
	if _, err := s.releaseMilestoneViaCanonicalPath(ctx, deal, &target); err != nil {
		return nil, err
	}
	rel, _ := s.repo.GetMilestoneByID(ctx, target.ID)
	if idx+1 < len(ms) {
		next := ms[idx+1]
		if rel != nil && rel.Status == MilestoneReleased && next.CheckingID == "" && next.AmountSats > 0 && next.Status != MilestoneReleased && next.Status != MilestoneExited && next.Status != MilestoneRefunded {
			_ = s.FundMilestone(ctx, deal, &next)
		} else if next.Status == MilestonePending {
			_ = s.repo.UpdateMilestoneStatus(ctx, next.ID, MilestoneInProgress, nil)
		}
		s.notifyMilestoneMoved(deal, target.Order, next.Order)
		deal.Status = StatusWorkSubmitted
	} else {
		if rel != nil && rel.Status == MilestoneReleased {
			_ = s.repo.UpdateStatus(ctx, deal.ID, StatusReleased)
			deal.Status = StatusReleased
			s.notifyReleased(deal)
		}
	}
	fresh, _ := s.repo.GetDealByID(ctx, deal.ID)
	if fresh != nil {
		return fresh, nil
	}
	return deal, nil
}
