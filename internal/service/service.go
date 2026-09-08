package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/acquirer"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/repository"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/validator"
)

// PaymentService implements the two payment gateway use cases.
type PaymentService struct {
	repo     *repository.PaymentsRepository
	acquirer acquirer.Acquirer
}

func New(repo *repository.PaymentsRepository, acq acquirer.Acquirer) *PaymentService {
	return &PaymentService{repo: repo, acquirer: acq}
}

func (s *PaymentService) ProcessPayment(ctx context.Context, req models.PaymentRequest) (*models.PaymentResponse, *models.RejectedResponse, error) {
	errs := validator.Validate(req)
	if len(errs) > 0 {
		return nil, &models.RejectedResponse{
			Status: models.StatusRejected,
			Errors: errs,
		}, nil
	}

	bankRequest := acquirer.BankRequest{
		CardNumber: req.CardNumber,
		ExpiryDate: formatExpiryDate(req.ExpiryMonth, req.ExpiryYear),
		Currency:   req.Currency,
		Amount:     req.Amount,
		Cvv:        req.Cvv,
	}

	bankResp, err := s.acquirer.Authorize(ctx, bankRequest)
	if err != nil {
		return nil, nil, err
	}

	status := models.StatusDeclined
	if bankResp.Authorized {
		status = models.StatusAuthorized
	}

	payment := models.PaymentResponse{
		Id:                 uuid.NewString(),
		Status:             status,
		CardNumberLastFour: lastFourDigits(req.CardNumber),
		ExpiryMonth:        req.ExpiryMonth,
		ExpiryYear:         req.ExpiryYear,
		Currency:           req.Currency,
		Amount:             req.Amount,
	}
	s.repo.AddPayment(payment)

	return &payment, nil, nil
}

// GetPayment retrieves a previously processed payment by ID. A Rejected
// request is never persisted, so it can never be returned here.
func (s *PaymentService) GetPayment(id string) (*models.PaymentResponse, bool) {
	payment := s.repo.GetPayment(id)
	if payment == nil {
		return nil, false
	}
	return payment, true
}

func formatExpiryDate(month, year int) string {
	return fmt.Sprintf("%02d/%d", month, year)
}

func lastFourDigits(cardNumber string) string {
	return cardNumber[len(cardNumber)-4:]
}
