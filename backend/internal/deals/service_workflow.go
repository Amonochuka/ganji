package deals

import (
	"context"
	"fmt"
)

func (s *Service) UpdateStatus(ctx context.Context, userID, dealID string, newStatus Status) error {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return err
	}

	if deal.FreelancerID != userID {
		return ErrForbidden
	}

	// locked, released, disputed and refunded are money moves that must be
	// driven by the backend — payment detection (webhook/poll), ApproveDeal,
	// or DisputeDeal — never by the freelancer's generic status endpoint.
	// Under the hold-invoice escrow, a freelancer self-marking a deal
	// "locked" (or worse "refunded") would be able to fake that escrow
	// funds are committed.
	switch newStatus {
	case StatusLocked, StatusReleased, StatusDisputed, StatusRefunded:
		return fmt.Errorf("%w: %s is a backend-only money transition (payment detection, approve, dispute)", ErrInvalidTransition, newStatus)
	}

	if !CanTransition(deal.Status, newStatus) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, newStatus)
	}
	return s.repo.UpdateStatus(ctx, dealID, newStatus)
}

// SubmitWork moves a locked deal to work_submitted. Only the freelancer
// (the deal owner) can submit, and the deal must already have at least one
// artifact — you cannot submit nothing.
func (s *Service) SubmitWork(ctx context.Context, userID, dealID string) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if deal.FreelancerID != userID {
		return nil, ErrForbidden
	}

	ms, err := s.repo.GetMilestonesByDeal(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if len(ms) > 0 {
		// Sequential milestone mode: mark active funded/in-progress milestone as submitted.
		for i := range ms {
			if err := s.refreshMilestonePayment(ctx, &ms[i]); err != nil {
				return nil, fmt.Errorf("refreshing milestone %d payment: %w", ms[i].Order, err)
			}
		}
		ms, err = s.repo.GetMilestonesByDeal(ctx, dealID)
		if err != nil {
			return nil, fmt.Errorf("reloading milestones: %w", err)
		}
		idx := -1
		for i := range ms {
			if ms[i].Status == MilestoneInProgress || ms[i].Status == MilestoneFunded || ms[i].Status == MilestoneWorkSubmitted {
				idx = i
				break
			}
		}
		if idx < 0 {
			for i := range ms {
				if ms[i].Status == MilestoneAwaitingPayment {
					return nil, fmt.Errorf("%w: milestone %d is awaiting payment before work can be submitted", ErrInvalidTransition, ms[i].Order)
				}
			}
			return nil, fmt.Errorf("%w: no active milestone to submit", ErrInvalidTransition)
		}
		// Milestone-mode submit requires evidence, same as the legacy
		// single-hold flow. Per-milestone artifact scoping (milestone_id on
		// artifacts) is a follow-up; the deal-level gate closes the current
		// bypass so work cannot be submitted with zero artifacts. Artifacts
		// are anchored to the freelancer's CV at deal release.
		artifacts, err := s.repo.ListArtifactsByDeal(ctx, dealID, nil)
		if err != nil {
			return nil, fmt.Errorf("checking submitted artifacts: %w", err)
		}
		if len(artifacts) == 0 {
			return nil, fmt.Errorf("%w: submit requires at least one artifact", ErrInvalidInput)
		}
		if ms[idx].Status != MilestoneWorkSubmitted && ms[idx].Status != MilestoneReleased && ms[idx].Status != MilestoneCompleted {
			if err := s.repo.UpdateMilestoneStatus(ctx, ms[idx].ID, MilestoneWorkSubmitted, nil); err != nil {
				return nil, fmt.Errorf("update milestone status: %w", err)
			}
			ms[idx].Status = MilestoneWorkSubmitted
		}
		deal.Status = StatusWorkSubmitted
		if err := s.repo.UpdateStatus(ctx, dealID, StatusWorkSubmitted); err != nil {
			return nil, fmt.Errorf("submitting work for deal %s: %w", dealID, err)
		}
		return deal, nil
	}

	artifacts, err := s.repo.ListArtifactsByDeal(ctx, dealID, nil)
	if err != nil {
		return nil, fmt.Errorf("checking submitted artifacts: %w", err)
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("%w: submit requires at least one artifact", ErrInvalidInput)
	}
	if !CanTransition(deal.Status, StatusWorkSubmitted) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusWorkSubmitted)
	}
	if err := s.repo.UpdateStatus(ctx, dealID, StatusWorkSubmitted); err != nil {
		return nil, fmt.Errorf("submitting work for deal %s: %w", dealID, err)
	}
	deal.Status = StatusWorkSubmitted
	return deal, nil
}
