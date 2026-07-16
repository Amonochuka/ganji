package lightning

type Invoice struct {
	PaymentHash    string `json:"payment_hash"`
	PaymentRequest string `json:"payment_request"`
	AmountSats     int64  `json:"amount_sats"`
	Description    string `json:"description"`
	Paid           bool   `json:"paid"`
}

type CreateInvoiceRequest struct {
	AmountSats  int64
	Description string
}

type CreateInvoiceResponse struct {
	PaymentHash    string `json:"payment_hash"`
	PaymentRequest string `json:"payment_request"`
}
