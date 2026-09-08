package repository

import (
	"sync"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
)

type PaymentsRepository struct {
	mu       sync.RWMutex
	payments map[string]models.PaymentResponse
}

func NewPaymentsRepository() *PaymentsRepository {
	return &PaymentsRepository{
		payments: make(map[string]models.PaymentResponse),
	}
}

// supports only in-memory storage
func (pr *PaymentsRepository) GetPayment(id string) *models.PaymentResponse {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	payment, ok := pr.payments[id]
	if !ok {
		return nil
	}
	return &payment
}

func (pr *PaymentsRepository) AddPayment(payment models.PaymentResponse) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.payments[payment.Id] = payment
}

// Count returns the number of stored payments. Intended for tests that
// need to assert nothing (or exactly one thing) was persisted.
func (pr *PaymentsRepository) Count() int {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	return len(pr.payments)
}
