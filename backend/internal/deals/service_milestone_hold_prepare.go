package deals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

func (s *Service) prepareMilestoneHold(ctx context.Context, deal *Deal, m *Milestone) error {
	if m.AmountSats <= 0 {
		m.AmountSats = deal.AmountSats // fallback (legacy/single)
	}
	payee := m.PayeeInvoice
	if payee == "" {
		payee = deal.PayeeInvoice
	}
	preimage := make([]byte, 32)
	if _, err := rand.Read(preimage); err != nil {
		return fmt.Errorf("generating milestone preimage: %w", err)
	}
	hash := sha256.Sum256(preimage)
	m.Preimage = hex.EncodeToString(preimage[:])
	m.PreimageHash = hex.EncodeToString(hash[:])
	m.PayeeInvoice = payee
	expiry, err := milestoneHoldExpirySeconds(m, time.Now())
	if err != nil {
		return err
	}
	hold, err := s.lnbits.CreateHoldInvoice(ctx, lnbits.CreateHoldInvoiceRequest{
		Out:         false,
		Amount:      m.AmountSats,
		Memo:        fmt.Sprintf("%s — M%d: %s", deal.Title, m.Order, m.Title),
		PaymentHash: m.PreimageHash,
		Expiry:      expiry,
	})
	if err != nil {
		s.cancelOrphanHold(ctx, m.PreimageHash)
		return fmt.Errorf("creating milestone hold invoice: %w", err)
	}
	if hold.PaymentHash != m.PreimageHash {
		s.cancelOrphanHold(ctx, m.PreimageHash)
		return fmt.Errorf("creating milestone hold invoice: provider returned a payment hash that does not match the escrow preimage")
	}
	m.Invoice = hold.PaymentRequest
	m.CheckingID = hold.CheckingID
	m.Status = MilestoneAwaitingPayment
	return nil
}

// FundMilestone creates a hold invoice (escrow leg) for a milestone and stores it.
// Sequential per-milestone holds: client pays this invoice to fund the milestone.
func (s *Service) FundMilestone(ctx context.Context, deal *Deal, m *Milestone) error {
	if err := s.prepareMilestoneHold(ctx, deal, m); err != nil {
		return err
	}
	if err := s.repo.UpdateMilestoneEscrow(ctx, m); err != nil {
		// best-effort cancel orphan
		_, _ = s.lnbits.CancelHold(ctx, m.PreimageHash)
		return err
	}
	return nil
}

func (s *Service) refreshMilestonePayment(ctx context.Context, m *Milestone) error {
	if m.CheckingID == "" || m.Status != MilestoneAwaitingPayment {
		return nil
	}
	if _, err := s.lnbits.CheckPayment(ctx, m.CheckingID); err != nil {
		return err
	}
	if s.acceptedHolds == nil || m.PreimageHash == "" {
		return ErrVerificationUnavailable
	}

	accepted, err := s.acceptedHolds.IsAcceptedHold(ctx, m.PreimageHash, m.AmountSats)
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}

	if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneFunded, nil); err != nil {
		return err
	}
	if err := s.repo.UpdateMilestoneStatus(ctx, m.ID, MilestoneInProgress, nil); err != nil {
		return err
	}
	m.Status = MilestoneInProgress
	_, _ = s.repo.UpdateDealStatusFromMilestones(ctx, m.DealID)
	return nil
}
