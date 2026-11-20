package deals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

func (s *Service) CreateDeal(ctx context.Context, deal *Deal) error {
	return s.CreateDealWithMilestones(ctx, deal, nil)
}

func (s *Service) CreateDealWithMilestones(ctx context.Context, deal *Deal, milestones []MilestoneInput) error {
	deal.Title = strings.TrimSpace(deal.Title)
	deal.SourcePlatform = strings.TrimSpace(deal.SourcePlatform)
	deal.PayeeInvoice = strings.TrimSpace(deal.PayeeInvoice)

	if deal.FreelancerID == "" {
		return fmt.Errorf("%w: freelancer id is required", ErrInvalidInput)
	}
	deal.ClientEmail = strings.ToLower(strings.TrimSpace(deal.ClientEmail))
	if !isValidEmail(deal.ClientEmail) {
		return fmt.Errorf("%w: valid client email is required", ErrInvalidInput)
	}
	if deal.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	if deal.SourcePlatform == "" {
		return fmt.Errorf("%w: source platform is required", ErrInvalidInput)
	}
	if deal.PayeeInvoice == "" {
		return fmt.Errorf("%w: payee invoice is required", ErrInvalidInput)
	}
	if deal.Status == "" {
		deal.Status = StatusAwaitingPayment
	}

	milestoneMode := len(milestones) > 0
	if milestoneMode {
		var total int64
		for i, mi := range milestones {
			if strings.TrimSpace(mi.Title) == "" || mi.AmountSats <= 0 {
				return fmt.Errorf("%w: each milestone needs a title and positive amount", ErrInvalidInput)
			}
			milestones[i].Title = strings.TrimSpace(mi.Title)
			total += mi.AmountSats
		}
		deal.AmountSats = total
		// No single upfront deal hold in sequential per-milestone mode.
		deal.PreimageHash = ""
		deal.Preimage = ""
		deal.Invoice = ""
		deal.CheckingID = ""
	} else {
		if deal.AmountSats <= 0 {
			return fmt.Errorf("%w: amount must be greater than zero", ErrInvalidInput)
		}
	}

	shareToken, err := generateShareToken()
	if err != nil {
		return fmt.Errorf("generating share token: %w", err)
	}
	deal.ShareToken = shareToken

	var first *Milestone
	if milestoneMode && len(milestones) > 0 {
		first = &Milestone{
			Title:      milestones[0].Title,
			AmountSats: milestones[0].AmountSats,
			Order:      1,
			Status:     MilestonePending,
		}
		if err := s.prepareMilestoneHold(ctx, deal, first); err != nil {
			return err
		}
		deal.Invoice = first.Invoice
		deal.CheckingID = first.CheckingID
		deal.Status = StatusAwaitingPayment
	} else if !milestoneMode {
		preimage := make([]byte, 32)
		if _, err := rand.Read(preimage); err != nil {
			return fmt.Errorf("generating escrow preimage: %w", err)
		}
		hash := sha256.Sum256(preimage)
		deal.Preimage = hex.EncodeToString(preimage[:])
		deal.PreimageHash = hex.EncodeToString(hash[:])

		hold, err := s.lnbits.CreateHoldInvoice(ctx, lnbits.CreateHoldInvoiceRequest{
			Out: false, Amount: deal.AmountSats, Memo: deal.Title, PaymentHash: deal.PreimageHash,
		})
		if err != nil {
			return fmt.Errorf("creating LNBits hold invoice: %w", err)
		}
		deal.Invoice = hold.PaymentRequest
		deal.CheckingID = hold.CheckingID
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		if first != nil && first.PreimageHash != "" {
			s.cancelOrphanHold(ctx, first.PreimageHash)
		}
		if deal.PreimageHash != "" {
			s.cancelOrphanHold(ctx, deal.PreimageHash)
		}
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	repo := s.repo.WithTx(tx)

	if err := repo.CreateDeal(ctx, deal); err != nil {
		if first != nil && first.PreimageHash != "" {
			s.cancelOrphanHold(ctx, first.PreimageHash)
		}
		if deal.PreimageHash != "" {
			s.cancelOrphanHold(ctx, deal.PreimageHash)
		}
		return err
	}

	for i, mi := range milestones {
		m := &Milestone{DealID: deal.ID, Title: mi.Title, AmountSats: mi.AmountSats, Order: i + 1, Status: MilestonePending}
		if i == 0 && first != nil {
			*m = *first
			m.DealID = deal.ID
		}
		if err := repo.CreateMilestone(ctx, m); err != nil {
			if m.PreimageHash != "" {
				s.cancelOrphanHold(ctx, m.PreimageHash)
			}
			if deal.PreimageHash != "" {
				s.cancelOrphanHold(ctx, deal.PreimageHash)
			}
			return err
		}
		if i == 0 {
			if err := repo.UpdateMilestoneEscrow(ctx, m); err != nil {
				s.cancelOrphanHold(ctx, m.PreimageHash)
				if deal.PreimageHash != "" {
					s.cancelOrphanHold(ctx, deal.PreimageHash)
				}
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		if deal.PreimageHash != "" {
			s.cancelOrphanHold(ctx, deal.PreimageHash)
		}
		if first != nil && first.PreimageHash != "" {
			s.cancelOrphanHold(ctx, first.PreimageHash)
		}
		return fmt.Errorf("commit transaction: %w", err)
	}

	if first != nil {
		deal.Invoice = first.Invoice
		deal.CheckingID = first.CheckingID
		deal.Status = StatusAwaitingPayment
	}

	return nil
}

// cancelOrphanHold tears down a hold invoice that was drawn before its deal
// row could be persisted, so a paying client is never stranded against a hold
// with no DB row behind it. Best-effort: cancellation failure is logged, not
// returned — the caller's original error is the one the client must see (and
// the hold-expiry sweep is the backstop for anything left standing).
func (s *Service) cancelOrphanHold(ctx context.Context, preimageHash string) {
	cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.lnbits.CancelHold(cancelCtx, preimageHash); err != nil {
		log.Printf("warn: cancelling orphaned hold %s: %v", preimageHash, err)
	}
}
