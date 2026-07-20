package lnbits

import (
	"bytes"
	"net/http"
	"context"
	"encoding/json"
	"fmt"
)

type Config struct {
	URL    string
	APIKey string
}

type Client struct {
	url    string
	apiKey string
	http   *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		url:    cfg.URL,
		apiKey: cfg.APIKey,
		http:   &http.Client{},
	}
}

func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*Invoice, error){
	body, err := json.Marshal(req)
	if err != nil{
		return nil, fmt.Errorf("lnbits: marshal create invoice request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"api/v1/payments", bytes.NewReader(body))
	if err != nil{
		return nil, fmt.Errorf("lnbits: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Api-Key", c.apiKey)


}
