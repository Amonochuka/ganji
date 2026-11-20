package deals

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DisputeDeal raises a dispute: the client must state, in writing, why they
// refuse the work, and the deal freezes in the 'disputed' arbitration state.
// Only the client (matched by client_email) can dispute.
//
// Disputing here does NOT move money. The hold stays held on the network —
// neither refunding the client nor paying the freelancer — until an arbiter
// resolves the dispute to released or refunded. This is the deliberate
// safeguard against pay → take the work → cancel: the funds are frozen and
// only an operator can release them. A client who changes their mind can
// still approve the deal instead (disputed -> released settles and pays).
func (s *Service) DisputeDeal(ctx context.Context, email, dealID, reason string) (*Deal, error) {
	if dealID == "" {
		return nil, fmt.Errorf("%w: deal id is required", ErrInvalidInput)
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: a dispute reason is required", ErrInvalidInput)
	}
	if length := len([]rune(reason)); length > maxDisputeReasonRunes {
		return nil, fmt.Errorf("%w: dispute reason must be at most %d characters", ErrInvalidInput, maxDisputeReasonRunes)
	}

	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return nil, err
	}

	if !strings.EqualFold(deal.ClientEmail, email) {
		return nil, ErrForbidden
	}

	if !CanTransition(deal.Status, StatusDisputed) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusDisputed)
	}

	// Guard the write against the still-valid status we just read. Without the
	// guard, a dispute racing an approve that already released the deal could
	// flip a released (money-moved) deal back to disputed.
	transitioned, err := s.repo.UpdateDisputeIfCurrent(ctx, dealID, deal.Status, reason)
	if err != nil {
		return nil, fmt.Errorf("raising dispute for deal %s: %w", dealID, err)
	}
	if !transitioned {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, deal.Status, StatusDisputed)
	}

	deal.Status = StatusDisputed
	deal.DisputeReason = reason
	deal.DisputedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	s.notifyDisputed(deal)
	return deal, nil
}
