package models

const (
	StatusAuthorized = "Authorized"
	StatusDeclined   = "Declined"
	StatusRejected   = "Rejected"
)

type PaymentRequest struct {
	CardNumber  string `json:"card_number"`
	ExpiryMonth int    `json:"expiry_month"`
	ExpiryYear  int    `json:"expiry_year"`
	Currency    string `json:"currency"`
	Amount      int    `json:"amount"`
	Cvv         string `json:"cvv"`
}

type PaymentResponse struct {
	Id                 string `json:"id"`
	Status             string `json:"status"`
	CardNumberLastFour string `json:"card_number_last_four"`
	ExpiryMonth        int    `json:"expiry_month"`
	ExpiryYear         int    `json:"expiry_year"`
	Currency           string `json:"currency"`
	Amount             int    `json:"amount"`
}

type RejectedResponse struct {
	Status string   `json:"status"`
	Errors []string `json:"errors"`
}
