package deals

import (
	"context"
	"fmt"
	"strings"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

func (s *Service) GenerateInvoice(ctx context.Context, walletURL, walletKey string, amountSats int64, memo string) (*lnbits.Invoice, error) {
	if amountSats <= 0 {
		return nil, fmt.Errorf("%w: amount must be greater than zero", ErrInvalidInput)
	}
	endpoint, err := validateWalletEndpoint(walletURL)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(walletKey)
	if key == "" {
		return nil, fmt.Errorf("%w: wallet key is required", ErrInvalidInput)
	}
	memo = strings.TrimSpace(memo)
	if memo == "" {
		memo = "Ganji payout"
	}
	wallet := lnbits.NewClient(lnbits.Config{URL: endpoint, APIKey: key})
	invoice, err := wallet.CreateInvoice(ctx, lnbits.CreateInvoiceRequest{Amount: amountSats, Memo: memo})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWalletInvoiceFailed, err)
	}
	return invoice, nil
}
