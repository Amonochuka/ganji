package deals

import (
	"context"
	"fmt"
	"log"
	"time"
)

func (s *Service) ExitDeal(ctx context.Context, dealID string) error {
	deal, err := s.repo.GetDealByID(ctx, dealID)
	if err != nil {
		return err
	}
	return s.exitDeal(ctx, deal)
}

func (s *Service) PublicExitDeal(ctx context.Context, shareToken string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}
	if err := s.exitDeal(ctx, deal); err != nil {
		return nil, err
	}
	return s.GetPublicDeal(ctx, shareToken)
}

func (s *Service) exitDeal(ctx context.Context, deal *Deal) error {
	milestones, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for i := range milestones {
		m := milestones[i]
		if m.Status == MilestoneReleased || m.Status == MilestoneRefunded || m.Status == MilestoneExited {
			continue
		}
		switch m.Status {
		case MilestoneAwaitingPayment, MilestoneFunded, MilestoneInProgress, MilestonePending:
			// Not submitted/approved: not paid to freelancer; cancel/refund hold (symmetric).
			if m.PreimageHash != "" && m.Status != MilestonePending {
				if err := s.cancelMilestoneHold(ctx, &m); err != nil {
					log.Printf("warn: cancel milestone hold on exit deal=%s milestone=%s: %v", deal.ID, m.ID, err)
					// Best-effort exit: a failed cancel does not block the
					// exit, but the milestone is still marked refunded so the
					// deal can terminate; hold-expiry sweep is the backstop for
					// an old hold that could not be cancelled. Mirrors the
					// milestone-mode exit contract in the redesign notes.
				}
				if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneRefunded, &now); err != nil {
					return err
				}
			} else {
				if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneExited, &now); err != nil {
					return err
				}
			}
		case MilestoneWorkSubmitted, MilestoneCompleted, MilestoneRejected:
			// Material consequences: do NOT move money. Hold stays; force independent review/dispute resolution.
			if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneDisputed, &now); err != nil {
				return err
			}
			if deal.DisputeReason == "" {
				deal.DisputeReason = "exit requested while milestone requires review (post-submission)"
				_, _ = s.repo.UpdateDisputeIfCurrent(ctx, deal.ID, deal.Status, deal.DisputeReason)
			}
		case MilestoneDisputed:
			// already awaiting resolution; keep hold, no money move
		default:
			_ = s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneExited, &now)
		}
	}

	if len(milestones) == 0 {
		return fmt.Errorf("%w: deal has no milestones", ErrInvalidTransition)
	}

	_, _ = s.repo.UpdateDealStatusFromMilestones(ctx, deal.ID)
	s.notifyExited(deal)
	return nil
}
