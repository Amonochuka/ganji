package deals

import (
	"context"
	"fmt"
)

// RotateShareLink mints a fresh share token for a deal, immediately killing
// any previously shared link (the old token no longer resolves). Freelancer
// only, and frozen once the deal is released/refunded — after the money has
// moved there is nothing left to share. Returns the updated deal so the
// caller can rebuild the link from deal.ShareToken.
func (s *Service) RotateShareLink(ctx context.Context, userID, dealID string) (*Deal, error) {
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

	switch deal.Status {
	case StatusReleased, StatusRefunded:
		return nil, fmt.Errorf("%w: share link is frozen once the deal is %s", ErrInvalidInput, deal.Status)
	}

	token, err := generateShareToken()
	if err != nil {
		return nil, fmt.Errorf("generating share token: %w", err)
	}

	if err := s.repo.UpdateShareToken(ctx, dealID, token); err != nil {
		return nil, fmt.Errorf("rotating share token: %w", err)
	}

	deal.ShareToken = token
	return deal, nil
}
