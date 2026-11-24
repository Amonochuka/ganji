package deals

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

// FundDeal funds a deal or milestone hold through a linked client wallet.
// LNbits paid=true is not proof that funds are locked, so the deal only
// transitions from awaiting_payment to locked after the escrow node reports an
// accepted hold under the deal's payment hash.
func (s *Service) FundDeal(ctx context.Context, shareToken, milestoneID, walletURL, walletKey string) (*PublicDeal, error) {
	if strings.TrimSpace(shareToken) == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	endpoint, err := validateWalletEndpoint(walletURL)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(walletKey)
	if key == "" {
		return nil, fmt.Errorf("%w: wallet key is required", ErrInvalidInput)
	}
	if s.acceptedHolds == nil {
		return nil, ErrVerificationUnavailable
	}

	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}

	return s.runFundExclusive(ctx, deal.ID, func() (*PublicDeal, error) {
		ms, err := s.repo.GetMilestonesByDeal(ctx, deal.ID)
		if err != nil {
			return nil, err
		}
		if len(ms) == 0 {
			return nil, fmt.Errorf("%w: deal has no milestones", ErrInvalidTransition)
		}

		if milestoneID != "" {
			m, err := s.repo.GetMilestoneByID(ctx, milestoneID)
			if err != nil {
				return nil, err
			}
			return s.fundMilestoneHold(ctx, deal, shareToken, m, endpoint, key)
		}

		for i := range ms {
			if ms[i].Status == MilestoneAwaitingPayment || ms[i].Status == MilestonePending {
				return s.fundMilestoneHold(ctx, deal, shareToken, &ms[i], endpoint, key)
			}
		}
		return nil, fmt.Errorf("%w: no milestone is awaiting payment", ErrInvalidTransition)
	})
}

func (s *Service) fundMilestoneHold(ctx context.Context, deal *Deal, shareToken string, m *Milestone, walletURL, walletKey string) (*PublicDeal, error) {
	if m.DealID != deal.ID {
		return nil, ErrMilestoneNotFound
	}
	if m.Status != MilestoneAwaitingPayment && m.Status != MilestonePending {
		return nil, fmt.Errorf("%w: milestone %d is %s", ErrInvalidTransition, m.Order, m.Status)
	}

	var err error
	if m.CheckingID == "" || m.Invoice == "" || m.PreimageHash == "" {
		if err = s.FundMilestone(ctx, deal, m); err != nil {
			return nil, err
		}
		m, err = s.repo.GetMilestoneByID(ctx, m.ID)
		if err != nil {
			return nil, err
		}
	}
	if err := s.refreshMilestonePayment(ctx, m); err != nil {
		return nil, fmt.Errorf("checking escrow hold: %w", err)
	}
	m, err = s.repo.GetMilestoneByID(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	if m.Status != MilestoneAwaitingPayment && m.Status != MilestonePending {
		return nil, fmt.Errorf("%w: milestone %d is already %s", ErrInvalidTransition, m.Order, m.Status)
	}

	wallet := lnbits.NewClient(lnbits.Config{URL: walletURL, AdminKey: walletKey})
	if _, err := wallet.PayInvoice(ctx, m.Invoice); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWalletPaymentFailed, err)
	}
	accepted, err := s.waitForAcceptedHold(ctx, m.PreimageHash, m.AmountSats)
	if err != nil {
		return nil, fmt.Errorf("verifying accepted hold: %w", err)
	}
	if !accepted {
		return nil, fmt.Errorf("%w: escrow node did not accept the milestone hold", ErrInvalidTransition)
	}
	if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneFunded, nil); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneInProgress, nil); err != nil {
		return nil, err
	}
	_, _ = s.repo.UpdateStatusIfCurrent(ctx, deal.ID, StatusAwaitingPayment, StatusLocked)
	return s.GetPublicDeal(ctx, shareToken)
}

func (s *Service) waitForAcceptedHold(ctx context.Context, paymentHash string, amountSats int64) (bool, error) {
	const attempts = 15
	const baseDelay = 2 * time.Second
	const jitter = 500 * time.Millisecond

	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		accepted, err := s.acceptedHolds.IsAcceptedHold(ctx, paymentHash, amountSats)
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil
			if accepted {
				return true, nil
			}
		}
		if i == attempts-1 {
			break
		}
		delay := baseDelay + time.Duration(rand.Int63n(int64(jitter)))
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return false, lastErr
			}
			return false, ctx.Err()
		case <-time.After(delay):
		}
	}
	if lastErr != nil {
		return false, lastErr
	}
	return false, nil
}
