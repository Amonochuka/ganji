package webhook

// PaymentNotification is the JSON body LNbits POSTs to the webhook URL
// when an invoice is paid. Field names match the LNbits Payment model.
type PaymentNotification struct {
	CheckingID  string `json:"checking_id"`
	PaymentHash string `json:"payment_hash"`
	Amount      int64  `json:"amount"`
	Fee         int64  `json:"fee"`
	Memo        string `json:"memo"`
	Status      string `json:"status"`
}
