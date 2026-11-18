package lnd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

var ErrInvalidPaymentHash = errors.New("invalid payment hash")

type Config struct {
	URL          string
	CertPath     string
	MacaroonPath string
	Timeout      time.Duration
}

type Client struct {
	url         string
	http        *http.Client
	macaroonHex string
}

type Invoice struct {
	RHash       string
	State       string
	Settled     bool
	SettleDate  int64
	AmountSats  int64
	AmtPaidSats int64
}

func (i *Invoice) IsAcceptedFor(paymentHash string, amountSats int64) bool {
	if i == nil || i.State != "ACCEPTED" || i.Settled || i.SettleDate != 0 {
		return false
	}
	if i.AmtPaidSats < amountSats || i.AmountSats != amountSats {
		return false
	}
	return strings.EqualFold(i.RHash, paymentHash)
}

func NewClient(cfg Config) (*Client, error) {
	certPEM, err := os.ReadFile(cfg.CertPath)
	if err != nil {
		return nil, fmt.Errorf("read LND TLS certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		return nil, errors.New("LND TLS certificate contains no certificates")
	}

	macaroon, err := os.ReadFile(cfg.MacaroonPath)
	if err != nil {
		return nil, fmt.Errorf("read LND macaroon: %w", err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Client{
		url:         strings.TrimRight(cfg.URL, "/"),
		macaroonHex: hex.EncodeToString(macaroon),
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			},
		},
	}, nil
}

func (c *Client) IsAcceptedHold(ctx context.Context, paymentHash string, amountSats int64) (bool, error) {
	invoice, err := c.LookupInvoice(ctx, paymentHash)
	if err != nil {
		return false, err
	}
	return invoice.IsAcceptedFor(paymentHash, amountSats), nil
}

func (c *Client) LookupInvoice(ctx context.Context, paymentHash string) (*Invoice, error) {
	if !isValidPaymentHash(paymentHash) {
		return nil, ErrInvalidPaymentHash
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/v1/invoice/"+paymentHash, nil)
	if err != nil {
		return nil, fmt.Errorf("create LND invoice request: %w", err)
	}
	request.Header.Set("Grpc-Metadata-macaroon", c.macaroonHex)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send LND invoice request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read LND invoice response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("LND invoice lookup failed: %d %s", response.StatusCode, string(body))
	}

	var raw struct {
		RHash       string      `json:"r_hash"`
		State       string      `json:"state"`
		Settled     bool        `json:"settled"`
		SettleDate  json.Number `json:"settle_date"`
		AmountSats  json.Number `json:"value"`
		AmtPaidSats json.Number `json:"amt_paid_sat"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode LND invoice response: %w", err)
	}

	settleDate, err := parseNumber(raw.SettleDate)
	if err != nil {
		return nil, fmt.Errorf("decode LND settle date: %w", err)
	}
	amountSats, err := parseNumber(raw.AmountSats)
	if err != nil {
		return nil, fmt.Errorf("decode LND invoice amount: %w", err)
	}
	amtPaidSats, err := parseNumber(raw.AmtPaidSats)
	if err != nil {
		return nil, fmt.Errorf("decode LND paid amount: %w", err)
	}

	rHash, err := normalizeRHash(raw.RHash)
	if err != nil {
		return nil, err
	}

	return &Invoice{
		RHash:       rHash,
		State:       strings.ToUpper(raw.State),
		Settled:     raw.Settled,
		SettleDate:  settleDate,
		AmountSats:  amountSats,
		AmtPaidSats: amtPaidSats,
	}, nil
}

func isValidPaymentHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func normalizeRHash(value string) (string, error) {
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return strings.ToLower(value), nil
	}
	decoded, err := base64Decode(value)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("decode LND invoice hash: invalid r_hash")
	}
	return hex.EncodeToString(decoded), nil
}

func parseNumber(value json.Number) (int64, error) {
	if value.String() == "" {
		return 0, nil
	}
	return strconv.ParseInt(value.String(), 10, 64)
}

func base64Decode(value string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(value)
}
