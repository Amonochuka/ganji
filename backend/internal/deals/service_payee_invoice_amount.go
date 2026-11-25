package deals

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

func (s *Service) validatePayeeInvoiceAmount(ctx context.Context, bolt11 string, expectedSats int64) error {
	bolt11 = strings.TrimSpace(bolt11)
	if bolt11 == "" {
		return fmt.Errorf("%w: payee invoice is required", ErrInvalidInput)
	}
	if expectedSats <= 0 {
		return fmt.Errorf("%w: expected amount must be positive", ErrInvalidInput)
	}
	decoded, err := s.lnbits.DecodePayment(ctx, bolt11)
	if err != nil {
		return fmt.Errorf("could not decode payee invoice: %w", err)
	}
	if decoded.AmountMsat <= 0 {
		return fmt.Errorf("%w: payee invoice must have a fixed amount", ErrInvalidInput)
	}
	if decoded.AmountMsat%1000 != 0 {
		return fmt.Errorf("%w: payee invoice amount must be a whole sat amount", ErrInvalidInput)
	}
	got := decoded.AmountMsat / 1000
	if got != expectedSats {
		return fmt.Errorf("%w: payee invoice amount %d sats does not match expected %d sats", ErrInvalidInput, got, expectedSats)
	}
	return validatePayeeInvoiceExpiry(decoded)
}

func validatePayeeInvoiceExpiry(decoded *lnbits.DecodedPayment) error {
	if decoded == nil || decoded.Expiry <= 0 {
		return nil
	}
	issuedAt := decoded.Date
	if issuedAt <= 0 {
		issuedAt = decoded.Timestamp
	}
	if issuedAt <= 0 {
		return fmt.Errorf("%w: payee invoice expiry cannot be verified", ErrInvalidInput)
	}
	expiresAt := time.Unix(issuedAt+decoded.Expiry, 0).UTC()
	if !time.Now().Before(expiresAt) {
		return fmt.Errorf("%w: payee invoice has expired", ErrInvalidInput)
	}
	return nil
}

func (s *Service) validatePayeeInvoiceDecodable(ctx context.Context, bolt11 string) error {
	bolt11 = strings.TrimSpace(bolt11)
	if bolt11 == "" {
		return fmt.Errorf("%w: payee invoice is required", ErrInvalidInput)
	}
	decoded, err := s.lnbits.DecodePayment(ctx, bolt11)
	if err != nil {
		return fmt.Errorf("could not decode payee invoice: %w", err)
	}
	if decoded.AmountMsat <= 0 {
		return fmt.Errorf("%w: payee invoice must have a fixed amount", ErrInvalidInput)
	}
	return validatePayeeInvoiceExpiry(decoded)
}
