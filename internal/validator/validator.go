package validator

import (
	"fmt"
	"time"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
)

// allowedCurrencies is the fixed 3-code allow-list required by the
// challenge. Not a general ISO-4217 table by design.
var allowedCurrencies = map[string]bool{
	"GBP": true,
	"USD": true,
	"EUR": true,
}

func Validate(req models.PaymentRequest) []string {
	var errs []string

	errs = append(errs, validateCardNumber(req.CardNumber)...)
	errs = append(errs, validateExpiry(req.ExpiryMonth, req.ExpiryYear)...)
	errs = append(errs, validateCurrency(req.Currency)...)
	errs = append(errs, validateAmount(req.Amount)...)
	errs = append(errs, validateCvv(req.Cvv)...)

	return errs
}

func validateCardNumber(cardNumber string) []string {
	if cardNumber == "" {
		return []string{"card_number is required"}
	}

	var errs []string
	if len(cardNumber) < 14 || len(cardNumber) > 19 {
		errs = append(errs, "card_number must be between 14 and 19 characters long")
	}
	if !isNumeric(cardNumber) {
		errs = append(errs, "card_number must contain only numeric characters")
	}
	return errs
}

func validateExpiry(month, year int) []string {
	var errs []string

	monthValid := month >= 1 && month <= 12
	if !monthValid {
		errs = append(errs, "expiry_month must be between 1 and 12")
	}

	if year == 0 {
		errs = append(errs, "expiry_year is required")
	}

	if monthValid && year != 0 && !expiryIsInFuture(month, year) {
		errs = append(errs, "expiry_month and expiry_year combination must be in the future")
	}

	return errs
}

// expiryIsInFuture treats a card as valid through the end of its expiry
// month (e.g. 04/2025 expires at the start of 05/2025).
func expiryIsInFuture(month, year int) bool {
	startOfMonthAfterExpiry := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)
	return time.Now().UTC().Before(startOfMonthAfterExpiry)
}

func validateCurrency(currency string) []string {
	if currency == "" {
		return []string{"currency is required"}
	}

	if len(currency) != 3 {
		return []string{"currency must be 3 characters"}
	}

	if !allowedCurrencies[currency] {
		return []string{fmt.Sprintf("currency must be one of: GBP, USD, EUR (got %q)", currency)}
	}

	return nil
}

func validateAmount(amount int) []string {
	if amount <= 0 {
		return []string{"amount is required and must be a positive integer"}
	}
	return nil
}

func validateCvv(cvv string) []string {
	if cvv == "" {
		return []string{"cvv is required"}
	}

	var errs []string
	if len(cvv) < 3 || len(cvv) > 4 {
		errs = append(errs, "cvv must be 3-4 characters long")
	}
	if !isNumeric(cvv) {
		errs = append(errs, "cvv must contain only numeric characters")
	}
	return errs
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
