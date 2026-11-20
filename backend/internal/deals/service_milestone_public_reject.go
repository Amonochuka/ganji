package deals

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Service) PublicRejectMilestone(ctx context.Context, shareToken, milestoneID, reason string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalidInput)
	}
	if length := len([]rune(reason)); length > maxDisputeReasonRunes {
		return nil, fmt.Errorf("%w: reason must be at most %d characters", ErrInvalidInput, maxDisputeReasonRunes)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}
	m, err := s.repo.GetMilestoneByID(ctx, milestoneID)
	if err != nil {
		return nil, err
	}
	if m.DealID != deal.ID {
		return nil, ErrMilestoneNotFound
	}
	// Rejected after submission: DO NOT auto-refund. Hold stays; force independent review/dispute.
	// Only allow rejecting submitted/completed (material) work.
	if m.Status != MilestoneWorkSubmitted && m.Status != MilestoneCompleted {
		return nil, fmt.Errorf("%w: milestone cannot be rejected from status %s (submit work or await review)", ErrInvalidTransition, m.Status)
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateMilestoneStatus(ctx, milestoneID, MilestoneDisputed, &now); err != nil {
		return nil, err
	}
	_, _ = s.repo.UpdateDisputeIfCurrent(ctx, deal.ID, deal.Status, reason)
	s.notifyDisputed(deal)
	return s.GetPublicDeal(ctx, shareToken)
}
