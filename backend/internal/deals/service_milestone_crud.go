package deals

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MilestoneService provides milestone-related operations for a deal.
func (s *Service) CreateMilestone(ctx context.Context, dealID, title string, amountSats int64, order int, deliveryDays ...int) (*Milestone, error) {
	title = strings.TrimSpace(title)
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}
	if title == "" {
		return nil, fmt.Errorf("%w: milestone title is required", ErrInvalidInput)
	}
	if len(title) > maxMilestoneTitleChars {
		return nil, fmt.Errorf("%w: milestone title is too long", ErrInvalidInput)
	}
	if amountSats <= 0 {
		return nil, fmt.Errorf("%w: milestone amount must be greater than zero", ErrInvalidInput)
	}
	if order < 1 {
		return nil, fmt.Errorf("%w: milestone order must be at least 1", ErrInvalidInput)
	}
	days := 7
	if len(deliveryDays) > 0 && deliveryDays[0] != 0 {
		days = deliveryDays[0]
	}
	if days < 1 || days > 3650 {
		return nil, fmt.Errorf("%w: milestone delivery days must be between 1 and 3650", ErrInvalidInput)
	}
	existing, err := s.repo.GetMilestonesByDeal(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if len(existing) >= maxMilestonesPerDeal {
		return nil, fmt.Errorf("%w: too many milestones", ErrInvalidInput)
	}
	milestone := &Milestone{
		DealID:       dealID,
		Title:        title,
		AmountSats:   amountSats,
		Order:        order,
		DeliveryDays: days,
		Status:       MilestonePending,
		CompletedAt:  sql.NullTime{},
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

func (s *Service) UpdateMilestoneStatus(ctx context.Context, dealID, milestoneID string, status MilestoneStatus, completedAt *time.Time) error {
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
	if current.DealID != dealID {
		return ErrMilestoneNotFound
	}
	if !CanMilestoneTransition(current.Status, status) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, status)
	}
	return s.repo.UpdateMilestoneStatus(ctx, milestoneID, status, completedAt)
}
