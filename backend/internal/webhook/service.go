package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/deals"
)

const (
	maxSignatureAge = 5 * time.Minute
)

// DealReader abstracts the deal repository methods the webhook needs.
type DealReader interface {
	GetDealByCheckingID(ctx context.Context, checkingID string) (*deals.Deal, error)
	UpdateStatus(ctx context.Context, dealID string, status deals.Status) error
}

type Service struct {
	repo    DealReader
	secret  string
}

func NewService(repo DealReader, secret string) *Service {
	return &Service{
		repo:   repo,
		secret: secret,
	}
}

// HandlePayment processes an LNbits payment notification. It verifies the
// HMAC signature, looks up the deal by checking_id, and transitions it from
// awaiting_payment to locked if the payment was successful.
func (s *Service) HandlePayment(ctx context.Context, rawBody []byte, signatureHeader string, notification *PaymentNotification) error {
	if s.secret != "" {
		if err := s.verifySignature(rawBody, signatureHeader); err != nil {
			return fmt.Errorf("webhook signature verification: %w", err)
		}
	}

	if notification.CheckingID == "" {
		return fmt.Errorf("missing checking_id in webhook payload")
	}

	if notification.Status != "success" {
		return fmt.Errorf("payment not successful: status=%s", notification.Status)
	}

	deal, err := s.repo.GetDealByCheckingID(ctx, notification.CheckingID)
	if err != nil {
		if errors.Is(err, deals.ErrDealNotFound) {
			return fmt.Errorf("no deal found for checking_id %s", notification.CheckingID)
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

// verifySignature validates the LNbits-Signature header using HMAC-SHA256.
// The header format is: t=<unix_timestamp>,v1=<hmac_hex>
// The signed payload is: "{timestamp}.{raw_body}"
func (s *Service) verifySignature(rawBody []byte, header string) error {
	parts := strings.Split(header, ",")
	if len(parts) != 2 {
		return errors.New("invalid signature header format")
	}

	var timestampStr, sig string
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return errors.New("invalid signature header part")
		}
		switch kv[0] {
		case "t":
			timestampStr = kv[1]
		case "v1":
			sig = kv[1]
		}
	}

	if timestampStr == "" || sig == "" {
		return errors.New("missing timestamp or signature in header")
	}

	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp in signature header: %w", err)
	}

	if time.Since(time.Unix(ts, 0)) > maxSignatureAge {
		return errors.New("webhook signature expired")
	}

	payload := fmt.Sprintf("%s.%s", timestampStr, string(rawBody))
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return errors.New("webhook signature mismatch")
	}

	return nil
}
