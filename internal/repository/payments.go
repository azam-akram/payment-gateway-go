package repository

import (
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
)

type PaymentsRepository struct {
	payments []models.PostPaymentResponse
}

func NewPaymentsRepository() *PaymentsRepository {
	return &PaymentsRepository{
		payments: []models.PostPaymentResponse{},
	}
}

func (ps *PaymentsRepository) GetPayment(id string) *models.PostPaymentResponse {
	for _, element := range ps.payments {
		if element.Id == id {
			return &element
		}
	}
	return nil
}

func (ps *PaymentsRepository) AddPayment(payment models.PostPaymentResponse) {
	ps.payments = append(ps.payments, payment)
}
