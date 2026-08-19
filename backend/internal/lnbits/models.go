package lnbits

type CreateInvoiceRequest struct {
	Out    bool   `json:"out"`
	Amount int64  `json:"amount"`
	Memo   string `json:"memo"`
}

// Raw response from LNBits.
type CreateInvoiceResponse struct {
	CheckingID     string `json:"checking_id"`
	PaymentHash    string `json:"payment_hash"`
	PaymentRequest string `json:"payment_request"`
}

// Internal application model.
type Invoice struct {
	CheckingID     string
	PaymentHash    string
	PaymentRequest string
}

type CheckPaymentResponse struct {
	Paid    bool                `json:"paid"`
	Details CheckPaymentDetails `json:"details"`
}

type CheckPaymentDetails struct {
	CheckingID string `json:"checking_id"`
	Amount     int64  `json:"amount"`
	Fee        int64  `json:"fee"`
	Memo       string `json:"memo"`
	Status     string `json:"status"`
}
