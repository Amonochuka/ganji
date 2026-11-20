package deals

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MilestoneService provides milestone-related operations for a deal.
func (s *Service) CreateMilestone(ctx context.Context, dealID, title string, amountSats int64, order int) (*Milestone, error) {
	title = strings.TrimSpace(title)
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	if title == "" {
		return nil, fmt.Errorf("%w: milestone title is required", ErrInvalidInput)
	}
	if amountSats <= 0 {
		return nil, fmt.Errorf("%w: milestone amount must be greater than zero", ErrInvalidInput)
	}
	if order < 1 {
		return nil, fmt.Errorf("%w: milestone order must be at least 1", ErrInvalidInput)
	}
	milestone := &Milestone{
		DealID:      dealID,
		Title:       title,
		AmountSats:  amountSats,
		Order:       order,
		Status:      MilestonePending,
		CompletedAt: sql.NullTime{},
	}
	if err := s.repo.CreateMilestone(ctx, milestone); err != nil {
		return nil, err
	}
	return milestone, nil
}

func (s *Service) GetMilestonesByDeal(ctx context.Context, dealID string) ([]Milestone, error) {
	milestones, err := s.repo.GetMilestonesByDeal(ctx, dealID)
	if err != nil {
		return nil, err
	}
	return milestones, nil
}

func (s *Service) GetMilestoneByID(ctx context.Context, milestoneID string) (*Milestone, error) {
	milestone, err := s.repo.GetMilestoneByID(ctx, milestoneID)
	if err != nil {
		return nil, err
	}
	return milestone, nil
}

func (s *Service) UpdateMilestoneStatus(ctx context.Context, milestoneID string, status MilestoneStatus, completedAt *time.Time) error {
	// released/refunded are money moves — never set them through this
	// endpoint. released must go through ReleaseMilestone (settle + pay);
	// refunded must go through CancelHold (ResolveMilestone/exit).
	if status == MilestoneReleased || status == MilestoneRefunded {
		return fmt.Errorf("%w: %s is a canonical money transition (must use the escrow release/refund path)", ErrInvalidTransition, status)
	}
	current, err := s.repo.GetMilestoneByID(ctx, milestoneID)
	if err != nil {
		return err
	}
	if !CanMilestoneTransition(current.Status, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, status)
	}
	return s.repo.UpdateMilestoneStatus(ctx, milestoneID, status, completedAt)
}
