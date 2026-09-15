package lnbits

// CreateHoldInvoiceRequest creates a hold (HODL) invoice on LNbits. The
// payment hash is derived from the preimage Ganji generates, so LNbits
// locks the incoming payment to exactly that hash. The funds sit held on
// the network until Ganji settles (reveals the preimage) or cancels.
type CreateHoldInvoiceRequest struct {
	Out         bool   `json:"out"`
	Amount      int64  `json:"amount"`
	Memo        string `json:"memo"`
	PaymentHash string `json:"payment_hash"`
	Expiry      int64  `json:"expiry,omitempty"`
	Webhook     string `json:"webhook,omitempty"`
	Unit        string `json:"unit,omitempty"`
}

// SettleHoldRequest is the body for POST /api/v1/payments/settle. LNbits
// verifies the preimage against the payment hash and completes the held
// payment, releasing the funds to the wallet.
type SettleHoldRequest struct {
	Preimage string `json:"preimage"`
}

// CancelHoldRequest is the body for POST /api/v1/payments/cancel. It tears
// down the held HTLC, returning the sats to the sender.
type CancelHoldRequest struct {
	PaymentHash string `json:"payment_hash"`
}

// SimpleInvoiceResponse is the minimal response LNbits returns for
// settle/cancel hold invoice operations.
type SimpleInvoiceResponse struct {
	OK           bool   `json:"ok"`
	CheckingID   string `json:"checking_id"`
	ErrorMessage string `json:"error_message"`
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
