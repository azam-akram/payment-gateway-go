package validator

import (
	"testing"
	"time"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func validRequest() models.PaymentRequest {
	future := time.Now().AddDate(1, 0, 0)
	return models.PaymentRequest{
		CardNumber:  "2222405343248877",
		ExpiryMonth: int(future.Month()),
		ExpiryYear:  future.Year(),
		Currency:    "GBP",
		Amount:      100,
		Cvv:         "123",
	}
}

func TestValidate_ValidRequest(t *testing.T) {
	errs := Validate(validRequest())
	assert.Empty(t, errs)
}

func TestValidate_FieldRules(t *testing.T) {
	past := time.Now().AddDate(-1, 0, 0)

	tests := []struct {
		name    string
		mutate  func(r *models.PaymentRequest)
		wantErr string
	}{
		{
			name:    "card number missing",
			mutate:  func(r *models.PaymentRequest) { r.CardNumber = "" },
			wantErr: "card_number is required",
		},
		{
			name:    "card number too short",
			mutate:  func(r *models.PaymentRequest) { r.CardNumber = "1234567890123" }, // 13 chars
			wantErr: "card_number must be between 14 and 19 characters long",
		},
		{
			name:    "card number too long",
			mutate:  func(r *models.PaymentRequest) { r.CardNumber = "123456789012345678901" }, // 21 chars
			wantErr: "card_number must be between 14 and 19 characters long",
		},
		{
			name:    "card number non-numeric",
			mutate:  func(r *models.PaymentRequest) { r.CardNumber = "2222405343ABCD77" },
			wantErr: "card_number must contain only numeric characters",
		},
		{
			name:    "expiry month zero",
			mutate:  func(r *models.PaymentRequest) { r.ExpiryMonth = 0 },
			wantErr: "expiry_month must be between 1 and 12",
		},
		{
			name:    "expiry month too high",
			mutate:  func(r *models.PaymentRequest) { r.ExpiryMonth = 13 },
			wantErr: "expiry_month must be between 1 and 12",
		},
		{
			name:    "expiry year missing",
			mutate:  func(r *models.PaymentRequest) { r.ExpiryYear = 0 },
			wantErr: "expiry_year is required",
		},
		{
			name: "expiry month/year combination in the past",
			mutate: func(r *models.PaymentRequest) {
				r.ExpiryMonth = int(past.Month())
				r.ExpiryYear = past.Year()
			},
			wantErr: "expiry_month and expiry_year combination must be in the future",
		},
		{
			name:    "currency missing",
			mutate:  func(r *models.PaymentRequest) { r.Currency = "" },
			wantErr: "currency is required",
		},
		{
			name:    "currency wrong length",
			mutate:  func(r *models.PaymentRequest) { r.Currency = "GB" },
			wantErr: "currency must be 3 characters",
		},
		{
			name:    "currency not in allow-list",
			mutate:  func(r *models.PaymentRequest) { r.Currency = "JPY" },
			wantErr: `currency must be one of: GBP, USD, EUR (got "JPY")`,
		},
		{
			name:    "amount missing",
			mutate:  func(r *models.PaymentRequest) { r.Amount = 0 },
			wantErr: "amount is required and must be a positive integer",
		},
		{
			name:    "amount negative",
			mutate:  func(r *models.PaymentRequest) { r.Amount = -100 },
			wantErr: "amount is required and must be a positive integer",
		},
		{
			name:    "cvv missing",
			mutate:  func(r *models.PaymentRequest) { r.Cvv = "" },
			wantErr: "cvv is required",
		},
		{
			name:    "cvv too short",
			mutate:  func(r *models.PaymentRequest) { r.Cvv = "12" },
			wantErr: "cvv must be 3-4 characters long",
		},
		{
			name:    "cvv too long",
			mutate:  func(r *models.PaymentRequest) { r.Cvv = "12345" },
			wantErr: "cvv must be 3-4 characters long",
		},
		{
			name:    "cvv non-numeric",
			mutate:  func(r *models.PaymentRequest) { r.Cvv = "12A" },
			wantErr: "cvv must contain only numeric characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.mutate(&req)

			errs := Validate(req)

			assert.Contains(t, errs, tt.wantErr)
		})
	}
}

func TestValidate_MultipleFailuresAccumulate(t *testing.T) {
	req := validRequest()
	req.CardNumber = ""
	req.Cvv = ""

	errs := Validate(req)

	assert.Contains(t, errs, "card_number is required")
	assert.Contains(t, errs, "cvv is required")
	assert.Len(t, errs, 2)
}
