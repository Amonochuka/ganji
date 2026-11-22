package deals

import (
	"context"
	"fmt"
	"strings"
)

// ResolveDispute resolves a deal-level dispute in milestone-only mode by
// resolving the first disputed/rejected milestone requiring operator action.
func (s *Service) ResolveDispute(ctx context.Context, operatorEmail, dealID string, resolution DisputeResolution) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	operatorEmail = strings.ToLower(strings.TrimSpace(operatorEmail))
	if operatorEmail == "" {
		return nil, fmt.Errorf("%w: operator email is required", ErrInvalidInput)
	}

	ms, err := s.repo.GetMilestonesByDeal(ctx, dealID)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range ms {
		if ms[i].Status == MilestoneDisputed || ms[i].Status == MilestoneRejected {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("%w: no disputed milestone found", ErrInvalidTransition)
	}
	if _, err := s.ResolveMilestone(ctx, operatorEmail, dealID, ms[idx].ID, resolution); err != nil {
		return nil, err
	}
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}
	deal.ResolvedBy = operatorEmail
	return deal, nil
}

// ReconcilePayout is the legacy deal-level endpoint; in milestone-only mode it
// reconciles a specific milestone when milestone_id is provided. deal-level
// without milestone_id is unsupported.
func (s *Service) ReconcilePayout(ctx context.Context, operatorEmail, dealID, milestoneID string, action ReconcileAction, payoutCheckingID string) (*Deal, error) {
	operatorEmail = strings.ToLower(strings.TrimSpace(operatorEmail))
	if operatorEmail == "" {
		return nil, fmt.Errorf("%w: operator email is required", ErrInvalidInput)
	}
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	if strings.TrimSpace(milestoneID) == "" {
		return nil, fmt.Errorf("%w: milestone_id is required for reconcile in milestone-only mode", ErrInvalidInput)
	}
	if _, err := s.ReconcileMilestonePayout(ctx, operatorEmail, dealID, strings.TrimSpace(milestoneID), action, payoutCheckingID); err != nil {
		return nil, err
	}
	return s.repo.GetDealByID(ctx, dealID)
}
