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

func (ps *PaymentsRepository) GetPayment(id string) *models.PaymentResponse {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	payment, ok := ps.payments[id]
	if !ok {
		return nil
	}
	return &payment
}

func (ps *PaymentsRepository) AddPayment(payment models.PaymentResponse) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.payments[payment.Id] = payment
}

// Count returns the number of stored payments. Intended for tests that
// need to assert nothing (or exactly one thing) was persisted.
func (ps *PaymentsRepository) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return len(ps.payments)
}
