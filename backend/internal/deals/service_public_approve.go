package deals

import (
	"context"
	"fmt"
)

// PublicApproveDeal allows a client to approve a deal from the public share link.
// Sequential milestone mode: releases the active milestone escrow (if present) and advances/funds next; final milestone completes deal.
// Legacy mode (no milestones / deal-level escrow): releases the single deal hold.
func (s *Service) PublicApproveDeal(ctx context.Context, shareToken string) (*Deal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
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
			if ms[i].Status == MilestoneAwaitingPayment || ms[i].Status == MilestonePending {
				return nil, fmt.Errorf("%w: milestone %d is not funded/submitted for approval", ErrInvalidTransition, ms[i].Order)
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
		if rel != nil && rel.Status == MilestoneReleased && next.Status != MilestoneReleased && next.Status != MilestoneExited && next.Status != MilestoneRefunded {
			if next.Status == MilestonePending || next.Status == MilestoneAwaitingPayment || next.CheckingID == "" {
				if err := s.FundMilestone(ctx, deal, &next); err != nil {
					return nil, fmt.Errorf("milestone %d was released but milestone %d could not be prepared for funding: %w", target.Order, next.Order, err)
				}
			}
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
