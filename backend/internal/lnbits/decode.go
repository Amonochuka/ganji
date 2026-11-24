package lnbits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type DecodedPayment struct {
	AmountMsat  int64  `json:"amount_msat"`
	PaymentHash string `json:"payment_hash"`
	Date        int64  `json:"date,omitempty"`
	Timestamp   int64  `json:"timestamp,omitempty"`
	Expiry      int64  `json:"expiry,omitempty"`
}

func (c *Client) DecodePayment(ctx context.Context, bolt11 string) (*DecodedPayment, error) {
	body, err := json.Marshal(map[string]string{"data": bolt11})
	if err != nil {
		return nil, fmt.Errorf("marshal decode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/api/v1/payments/decode", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create decode request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send decode request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("lnbits decode returned %d: %s", resp.StatusCode, string(b))
	}
	var out DecodedPayment
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode payment response: %w", err)
	}
	return &out, nil
}
