package webhook

import (
	"context"
	"errors"
	"fmt"

	"github.com/Amonochuka/ganji-backend/internal/deals"
	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

// Sentinel errors so the handler can map to distinct HTTP status codes.
var (
	ErrMalformedPayload = errors.New("malformed webhook payload")
	ErrPaymentFailed    = errors.New("payment not successful")
	ErrDealNotFound     = errors.New("no deal for checking_id")
)

// DealReader abstracts the deal repository methods the webhook needs.
type DealReader interface {
	GetDealByCheckingID(ctx context.Context, checkingID string) (*deals.Deal, error)
	UpdateStatus(ctx context.Context, dealID string, status deals.Status) error
}

type Service struct {
	repo   DealReader
	lnbits *lnbits.Client
}

func NewService(repo DealReader, lnbitsClient *lnbits.Client) *Service {
	return &Service{
		repo:   repo,
		lnbits: lnbitsClient,
	}
}

// HandlePayment processes an LNbits payment notification and transitions
// the matching deal from awaiting_payment to locked when payment succeeds.
func (s *Service) HandlePayment(ctx context.Context, notification *PaymentNotification) error {
	if notification.CheckingID == "" {
		return fmt.Errorf("%w: missing checking_id", ErrMalformedPayload)
	}

	payment, err := s.lnbits.CheckPayment(ctx, notification.CheckingID)
	if err != nil {
		return fmt.Errorf("verify payment with lnbits: %w", err)
	}

	if !payment.Paid {
		return fmt.Errorf("%w: payment not confirmed by lnbits", ErrPaymentFailed)
	}

	deal, err := s.repo.GetDealByCheckingID(ctx, notification.CheckingID)
	if err != nil {
		if errors.Is(err, deals.ErrDealNotFound) {
			return fmt.Errorf("%w: %s", ErrDealNotFound, notification.CheckingID)
		}

		return fmt.Errorf("lookup deal by checking_id: %w", err)
	}

	if deal.Status != deals.StatusAwaitingPayment {
		return nil
	}

	if err := s.repo.UpdateStatus(ctx, deal.ID, deals.StatusLocked); err != nil {
		return fmt.Errorf("transition deal %s to locked: %w", deal.ID, err)
	}

	return nil
}
