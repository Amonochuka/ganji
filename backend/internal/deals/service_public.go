package deals

import (
	"context"
	"fmt"
	"time"
)

// GetPublicDeal returns a safe, public view of the deal for the shareable
// link. No auth required — anyone holding the (unguessable, rotatable)
// share_token can see it. We expose only the fields needed to pay and track
// the deal, never secrets (preimage, payee_invoice, client_email,
// checking_id, the internal deal id, or the share token itself).
//
// Before answering, GetPublicDeal re-checks the hold's status with LNbits and
// LND and locks the deal if the payment landed. The client who opens this link has no
// account, so they can never hit the freelancer-only poll endpoint — without
// this refresh the page would stay "awaiting_payment" if the webhook was
// delayed, lost, or never configured. LNbits being down is not a visitor
// error: we just fall back to the last known status.
func (s *Service) GetPublicDeal(ctx context.Context, shareToken string) (*PublicDeal, error) {
	if shareToken == "" {
		return nil, fmt.Errorf("%w: share token is required", ErrInvalidInput)
	}
	deal, err := s.repo.GetDealByShareToken(ctx, shareToken)
	if err != nil {
		return nil, err
	}
	ms, _ := s.repo.GetMilestonesByDeal(ctx, deal.ID)
	for i := range ms {
		_ = s.refreshMilestonePayment(ctx, &ms[i])
	}
	ms, _ = s.repo.GetMilestonesByDeal(ctx, deal.ID)
	views := make([]MilestoneView, 0, len(ms))
	activeInvoice := deal.Invoice
	activeStatus := deal.Status
	payoutResetAvailable := false
	for i := range ms {
		var completed *string
		if ms[i].CompletedAt.Valid {
			s := ms[i].CompletedAt.Time.Format(time.RFC3339)
			completed = &s
		}
		resetAvailable := canResetMilestonePayoutAttempt(&ms[i])
		views = append(views, MilestoneView{
			ID:                   ms[i].ID,
			Title:                ms[i].Title,
			AmountSats:           ms[i].AmountSats,
			Order:                ms[i].Order,
			Status:               string(ms[i].Status),
			Invoice:              ms[i].Invoice,
			CompletedAt:          completed,
			DeliveryDays:         ms[i].DeliveryDays,
			DueAt:                milestoneViewFrom(ms[i]).DueAt,
			CreatedAt:            ms[i].CreatedAt,
			PayoutResetAvailable: resetAvailable,
		})
		payoutResetAvailable = payoutResetAvailable || resetAvailable
		if activeInvoice == "" && (ms[i].Status == MilestoneAwaitingPayment || ms[i].Status == MilestonePending || ms[i].Status == MilestoneFunded || ms[i].Status == MilestoneInProgress) && ms[i].Invoice != "" {
			activeInvoice = ms[i].Invoice
		}
		if ms[i].Status == MilestoneAwaitingPayment {
			activeStatus = StatusAwaitingPayment
		}
	}
	if active := ActiveMilestone(ms); active != nil {
		switch active.Status {
		case MilestoneAwaitingPayment, MilestonePending:
			activeStatus = StatusAwaitingPayment
		case MilestoneDisputed, MilestoneRejected:
			activeStatus = StatusDisputed
		case MilestoneWorkSubmitted, MilestoneCompleted, MilestoneFunded, MilestoneInProgress:
			activeStatus = StatusWorkSubmitted
		}
	}
	return &PublicDeal{
		Title:                deal.Title,
		AmountSats:           deal.AmountSats,
		SourcePlatform:       deal.SourcePlatform,
		Invoice:              activeInvoice,
		Status:               activeStatus,
		DisputeReason:        deal.DisputeReason,
		CreatedAt:            deal.CreatedAt,
		Milestones:           views,
		PayoutResetAvailable: payoutResetAvailable,
	}, nil
}
