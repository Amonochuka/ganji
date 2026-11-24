package deals

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) GetDealByID(ctx context.Context, dealID, userID, email string, isOperator bool) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if isOperator {
		return deal, nil
	}
	if deal.FreelancerID != userID && !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	return deal, nil
}

func (s *Service) ListByUser(ctx context.Context, userID, email string) ([]Deal, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id is required", ErrInvalidInput)
	}

	return s.repo.ListForUser(ctx, userID, email)
}

func (s *Service) ListAllDeals(ctx context.Context) ([]Deal, error) {
	return s.repo.ListAllDeals(ctx)
}

// ListDisputes returns the arbitration queue: every deal frozen in the
// disputed state, oldest dispute first. Route access is operator-only; the
// service returns the full deal rows an operator needs to adjudicate.
func (s *Service) ListDisputes(ctx context.Context) ([]Deal, error) {
	return s.repo.ListDisputed(ctx)
}
