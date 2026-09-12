package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/Amonochuka/ganji-backend/internal/deals"
	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

type fakePaymentChecker struct {
	payment *lnbits.CheckPaymentResponse
	err     error

	lastCheckingID string
}

func (f *fakePaymentChecker) CheckPayment(
	ctx context.Context,
	checkingID string,
) (*lnbits.CheckPaymentResponse, error) {
	f.lastCheckingID = checkingID

	if f.err != nil {
		return nil, f.err
	}

	return f.payment, nil
}

type fakeDealReader struct {
	deal *deals.Deal
	err  error

	updatedDealID string
	updatedStatus deals.Status
}

func (f *fakeDealReader) GetDealByCheckingID(
	ctx context.Context,
	checkingID string,
) (*deals.Deal, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.deal, nil
}

func (f *fakeDealReader) UpdateStatus(
	ctx context.Context,
	dealID string,
	status deals.Status,
) error {
	f.updatedDealID = dealID
	f.updatedStatus = status

	return nil
}

func TestHandlePaymentLocksPaidDeal(t *testing.T) {
	paymentChecker := &fakePaymentChecker{
		payment: &lnbits.CheckPaymentResponse{
			Paid: true,
		},
	}

	dealReader := &fakeDealReader{
		deal: &deals.Deal{
			ID:     "deal-123",
			Status: deals.StatusAwaitingPayment,
		},
	}

	service := NewService(dealReader, paymentChecker)

	notification := &PaymentNotification{
		CheckingID: "checking-123",
	}

	err := service.HandlePayment(context.Background(), notification)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if paymentChecker.lastCheckingID != "checking-123" {
		t.Fatalf(
			"expected checking ID checking-123, got %s",
			paymentChecker.lastCheckingID,
		)
	}

	if dealReader.updatedDealID != "deal-123" {
		t.Fatalf(
			"expected deal-123 to be updated, got %s",
			dealReader.updatedDealID,
		)
	}

	if dealReader.updatedStatus != deals.StatusLocked {
		t.Fatalf(
			"expected status locked, got %s",
			dealReader.updatedStatus,
		)
	}
}

func TestHandlePaymentDoesNotLockUnpaidDeal(t *testing.T) {
	paymentChecker := &fakePaymentChecker{
		payment: &lnbits.CheckPaymentResponse{
			Paid: false,
		},
	}

	dealReader := &fakeDealReader{
		deal: &deals.Deal{
			ID:     "deal-123",
			Status: deals.StatusAwaitingPayment,
		},
	}

	service := NewService(dealReader, paymentChecker)

	notification := &PaymentNotification{
		CheckingID: "checking-123",
	}

	err := service.HandlePayment(context.Background(), notification)
	if !errors.Is(err, ErrPaymentFailed) {
		t.Fatalf("expected ErrPaymentFailed, got %v", err)
	}

	if dealReader.updatedDealID != "" {
		t.Fatalf("expected deal not to be updated")
	}
}

func TestHandlePaymentRejectsMissingCheckingID(t *testing.T) {
	paymentChecker := &fakePaymentChecker{
		payment: &lnbits.CheckPaymentResponse{
			Paid: true,
		},
	}

	dealReader := &fakeDealReader{}

	service := NewService(dealReader, paymentChecker)

	err := service.HandlePayment(
		context.Background(),
		&PaymentNotification{},
	)

	if !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("expected ErrMalformedPayload, got %v", err)
	}
}

func TestHandlePaymentHandlesMissingDeal(t *testing.T) {
	paymentChecker := &fakePaymentChecker{
		payment: &lnbits.CheckPaymentResponse{
			Paid: true,
		},
	}

	dealReader := &fakeDealReader{
		err: deals.ErrDealNotFound,
	}

	service := NewService(dealReader, paymentChecker)

	notification := &PaymentNotification{
		CheckingID: "checking-123",
	}

	err := service.HandlePayment(
		context.Background(),
		notification,
	)

	if !errors.Is(err, ErrDealNotFound) {
		t.Fatalf("expected ErrDealNotFound, got %v", err)
	}
}

func TestHandlePaymentDoesNotUpdateAlreadyLockedDeal(t *testing.T) {
	paymentChecker := &fakePaymentChecker{
		payment: &lnbits.CheckPaymentResponse{
			Paid: true,
		},
	}

	dealReader := &fakeDealReader{
		deal: &deals.Deal{
			ID:     "deal-123",
			Status: deals.StatusLocked,
		},
	}

	service := NewService(dealReader, paymentChecker)

	notification := &PaymentNotification{
		CheckingID: "checking-123",
	}

	err := service.HandlePayment(
		context.Background(),
		notification,
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if dealReader.updatedDealID != "" {
		t.Fatalf("expected already locked deal not to be updated")
	}
}

func TestHandlePaymentReturnsLNbitsError(t *testing.T) {
	expectedErr := errors.New("lnbits unavailable")

	paymentChecker := &fakePaymentChecker{
		err: expectedErr,
	}

	dealReader := &fakeDealReader{
		deal: &deals.Deal{
			ID:     "deal-123",
			Status: deals.StatusAwaitingPayment,
		},
	}

	service := NewService(dealReader, paymentChecker)

	err := service.HandlePayment(
		context.Background(),
		&PaymentNotification{
			CheckingID: "checking-123",
		},
	)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped LNbits error, got %v", err)
	}

	if dealReader.updatedDealID != "" {
		t.Fatalf("expected no deal update, got %q", dealReader.updatedDealID)
	}
}
