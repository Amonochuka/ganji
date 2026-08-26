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
	URL        string
	APIKey     string
	WebhookURL string
}

type Client struct {
	url        string
	apiKey     string
	webhookURL string
	http       *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		url:        cfg.URL,
		apiKey:     cfg.APIKey,
		webhookURL: cfg.WebhookURL,
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
