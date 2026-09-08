package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/acquirer"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestProcessPayment_Authorized(t *testing.T) {
	repo := repository.NewPaymentsRepository()
	fake := &acquirer.FakeAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{Authorized: true, AuthorizationCode: "auth-code"}, nil
		},
	}
	svc := New(repo, fake)

	payment, rejected, err := svc.ProcessPayment(context.Background(), validRequest())

	require.NoError(t, err)
	require.Nil(t, rejected)
	require.NotNil(t, payment)
	assert.Equal(t, models.StatusAuthorized, payment.Status)
	assert.Equal(t, "8877", payment.CardNumberLastFour)
	assert.NotEmpty(t, payment.Id)

	stored := repo.GetPayment(payment.Id)
	require.NotNil(t, stored)
	assert.Equal(t, *payment, *stored)

	require.Len(t, fake.Calls, 1)
	assert.Equal(t, "2222405343248877", fake.Calls[0].CardNumber)
}

func TestProcessPayment_Declined(t *testing.T) {
	repo := repository.NewPaymentsRepository()
	fake := &acquirer.FakeAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{Authorized: false}, nil
		},
	}
	svc := New(repo, fake)

	payment, rejected, err := svc.ProcessPayment(context.Background(), validRequest())

	require.NoError(t, err)
	require.Nil(t, rejected)
	require.NotNil(t, payment)
	assert.Equal(t, models.StatusDeclined, payment.Status)

	assert.Equal(t, 1, repo.Count())
}

func TestProcessPayment_BankRequest_ExpiryDateFormattedMMYYYY(t *testing.T) {
	repo := repository.NewPaymentsRepository()
	fake := &acquirer.FakeAcquirer{}
	svc := New(repo, fake)

	req := validRequest()
	req.ExpiryMonth = 4
	req.ExpiryYear = req.ExpiryYear + 5 // keep it safely in the future

	_, _, err := svc.ProcessPayment(context.Background(), req)

	require.NoError(t, err)
	require.Len(t, fake.Calls, 1)
	assert.Equal(t, "04/"+strconv.Itoa(req.ExpiryYear), fake.Calls[0].ExpiryDate)
}

func TestProcessPayment_InvalidRequest_RejectedWithoutCallingAcquirer(t *testing.T) {
	repo := repository.NewPaymentsRepository()
	fake := &acquirer.FakeAcquirer{}
	svc := New(repo, fake)

	req := validRequest()
	req.CardNumber = "123" // fails length + would-be numeric rule trivially
	req.Cvv = ""

	payment, rejected, err := svc.ProcessPayment(context.Background(), req)

	require.NoError(t, err)
	require.Nil(t, payment)
	require.NotNil(t, rejected)
	assert.Equal(t, models.StatusRejected, rejected.Status)
	assert.Contains(t, rejected.Errors, "cvv is required")

	assert.Empty(t, fake.Calls, "acquirer must never be called for a rejected request")
	assert.Equal(t, 0, repo.Count(), "nothing must be persisted for a rejected request")
}

func TestProcessPayment_BankUnavailable_NothingPersisted(t *testing.T) {
	repo := repository.NewPaymentsRepository()
	fake := &acquirer.FakeAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{}, acquirer.ErrBankUnavailable
		},
	}
	svc := New(repo, fake)

	payment, rejected, err := svc.ProcessPayment(context.Background(), validRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, acquirer.ErrBankUnavailable))
	assert.Nil(t, payment)
	assert.Nil(t, rejected)

	require.Len(t, fake.Calls, 1, "the bank should have been called exactly once")
	assert.Equal(t, 0, repo.Count(), "nothing must be persisted when the bank outcome is unknown")
}

func TestGetPayment_Found(t *testing.T) {
	repo := repository.NewPaymentsRepository()
	svc := New(repo, &acquirer.FakeAcquirer{})

	stored := models.PaymentResponse{Id: "abc-123", Status: models.StatusAuthorized, CardNumberLastFour: "1234"}
	repo.AddPayment(stored)

	payment, found := svc.GetPayment("abc-123")

	assert.True(t, found)
	require.NotNil(t, payment)
	assert.Equal(t, stored, *payment)
}

func TestGetPayment_NotFound(t *testing.T) {
	repo := repository.NewPaymentsRepository()
	svc := New(repo, &acquirer.FakeAcquirer{})

	payment, found := svc.GetPayment("does-not-exist")

	assert.False(t, found)
	assert.Nil(t, payment)
}
