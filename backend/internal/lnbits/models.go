package lnbits

type CreateInvoiceRequest struct {
	Out    bool   `json:"out"`
	Amount int64  `json:"amount"`
	Memo   string `json:"memo"`
}

type Invoice struct {
	CheckingID    string `json:"checking_id"`
	PaymentHash   string `json:"payment_hash"`
	PaymentRequest string `json:"payment_request"`
}