package webhook

import "time"

// PaymentNotification is the JSON body LNbits POSTs to the webhook URL
// when an invoice is paid. Field names match the LNbits Payment model.
type PaymentNotification struct {
	CheckingID string `json:"checking_id"`
	PaymentHash string `json:"payment_hash"`
	Amount     int64  `json:"amount"`
	Fee        int64  `json:"fee"`
	Memo       string `json:"memo"`
	Status     string `json:"status"`
	Time       int64  `json:"time"`
}

// SignatureHeader represents the parsed LNbits-Signature header.
// Format: t=<unix_timestamp>,v1=<hmac_sha256_hex>
type SignatureHeader struct {
	Timestamp time.Time
	Signature string
}
