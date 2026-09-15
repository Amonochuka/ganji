package lnbits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Config struct {
	URL           string
	APIKey        string
	AdminKey      string
	WebhookURL    string
	HoldExpirySec int64 // default expiry for hold invoices (0 = LNbits default)
}

type Client struct {
	url        string
	apiKey     string
	webhookURL string
	adminKey   string
	holdExpiry int64
	http       *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		url:        cfg.URL,
		apiKey:     cfg.APIKey,
		webhookURL: cfg.WebhookURL,
		adminKey:   cfg.AdminKey,
		holdExpiry: cfg.HoldExpirySec,
		http:       &http.Client{},
	}
}

func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*Invoice, error) {
	if req.Webhook == "" && c.webhookURL != "" {
		req.Webhook = c.webhookURL
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal invoice request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.url+"/api/v1/payments",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", c.apiKey)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusMultipleChoices {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, fmt.Errorf(
				"lnbits returned %d and response body could not be read: %w",
				response.StatusCode,
				err,
			)
		}

		return nil, fmt.Errorf(
			"lnbits returned %d: %s",
			response.StatusCode,
			string(body),
		)
	}
	var invoice CreateInvoiceResponse

	if err := json.NewDecoder(response.Body).Decode(&invoice); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &Invoice{
		PaymentRequest: invoice.PaymentRequest,
		PaymentHash:    invoice.PaymentHash,
		CheckingID:     invoice.CheckingID,
	}, nil
}

func (c *Client) CheckPayment(ctx context.Context, checkingID string) (*CheckPaymentResponse, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.url+"/api/v1/payments/"+checkingID,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create payment status request: %w", err)
	}

	request.Header.Set("X-Api-Key", c.apiKey)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send payment status request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusMultipleChoices {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, fmt.Errorf(
				"lnbits returned %d and response body could not be read: %w",
				response.StatusCode,
				err,
			)
		}

		return nil, fmt.Errorf(
			"lnbits returned %d: %s",
			response.StatusCode,
			string(body),
		)
	}

	var payment CheckPaymentResponse

	if err := json.NewDecoder(response.Body).Decode(&payment); err != nil {
		return nil, fmt.Errorf("decode payment status response: %w", err)
	}

	return &payment, nil
}

// CreateHoldInvoice creates a hold invoice locked to the given payment hash
// (sha256 of the preimage Ganji generated). It uses the invoice key, like a
// regular receive invoice. LNbits derives the payment hash from the preimage,
// so the returned bolt11 can only ever be settled by revealing it.
func (c *Client) CreateHoldInvoice(ctx context.Context, req CreateHoldInvoiceRequest) (*Invoice, error) {
	if req.Webhook == "" && c.webhookURL != "" {
		req.Webhook = c.webhookURL
	}
	if req.Unit == "" {
		req.Unit = "sat"
	}
	if req.Expiry == 0 {
		req.Expiry = c.holdExpiry
	}

	var resp CreateInvoiceResponse
	if err := c.postJSON(ctx, "/api/v1/payments", c.apiKey, req, &resp); err != nil {
		return nil, fmt.Errorf("create hold invoice: %w", err)
	}

	return &Invoice{
		PaymentRequest: resp.PaymentRequest,
		PaymentHash:    resp.PaymentHash,
		CheckingID:     resp.CheckingID,
	}, nil
}

// SettleHold completes a held payment by revealing the preimage. Requires
// the wallet admin key — this is the network-level release of escrow. LNbits
// answers with `ok:false` + an error_message for refused settles (e.g.
// "payment already settled"); we surface that as an error so callers never
// mistake a refused settle for a successful one.
func (c *Client) SettleHold(ctx context.Context, preimage string) (*SimpleInvoiceResponse, error) {
	var out SimpleInvoiceResponse
	if err := c.postJSON(ctx, "/api/v1/payments/settle", c.adminKey, SettleHoldRequest{
		Preimage: preimage,
	}, &out); err != nil {
		return &out, err
	}
	if !out.OK {
		return &out, fmt.Errorf("lnbits refused to settle: %s", out.ErrorMessage)
	}
	return &out, nil
}

// CancelHold tears down a held payment, returning the sats to the payer.
// Requires the wallet admin key — this is the network-level refund. A
// cancelled/unknown invoice is reported via `ok:false`.
func (c *Client) CancelHold(ctx context.Context, paymentHash string) (*SimpleInvoiceResponse, error) {
	var out SimpleInvoiceResponse
	if err := c.postJSON(ctx, "/api/v1/payments/cancel", c.adminKey, CancelHoldRequest{
		PaymentHash: paymentHash,
	}, &out); err != nil {
		return &out, err
	}
	if !out.OK {
		return &out, fmt.Errorf("lnbits refused to cancel: %s", out.ErrorMessage)
	}
	return &out, nil
}

// PayInvoice instructs LNbits to pay an outgoing invoice (out: true) from
// the wallet's balance. Used to forward settled escrow to the freelancer.
func (c *Client) PayInvoice(ctx context.Context, bolt11 string) error {
	body, err := json.Marshal(map[string]any{
		"out":    true,
		"bolt11": bolt11,
	})
	if err != nil {
		return fmt.Errorf("marshal pay invoice request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.url+"/api/v1/payments",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create pay invoice request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", c.adminKey)

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("send pay invoice request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusMultipleChoices {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf(
				"lnbits returned %d and response body could not be read: %w",
				response.StatusCode,
				err,
			)
		}
		return fmt.Errorf("lnbits returned %d: %s", response.StatusCode, string(body))
	}

	return nil
}

// postJSON performs a JSON POST and optionally decodes a 2xx response into
// out. apiKeySelect is the LNbits API key used for the request (invoice key
// for reads/creates, admin key for settle/cancel/send).
func (c *Client) postJSON(
	ctx context.Context,
	path string,
	apiKey string,
	req any,
	out any,
) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.url+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", apiKey)

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusMultipleChoices {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf(
				"lnbits returned %d and response body could not be read: %w",
				response.StatusCode,
				err,
			)
		}
		return fmt.Errorf("lnbits returned %d: %s", response.StatusCode, string(body))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
