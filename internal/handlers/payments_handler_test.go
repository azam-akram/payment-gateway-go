package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/acquirer"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/repository"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPaymentHandler(t *testing.T) {
	payment := models.PaymentResponse{
		Id:                 "test-id",
		Status:             "test-successful-status",
		CardNumberLastFour: "1234",
		ExpiryMonth:        10,
		ExpiryYear:         2035,
		Currency:           "GBP",
		Amount:             100,
	}
	ps := repository.NewPaymentsRepository()
	ps.AddPayment(payment)
	svc := service.New(ps, &acquirer.MockAcquirer{})

	payments := NewPaymentsHandler(svc)

	r := chi.NewRouter()
	r.Get("/api/payments/{id}", payments.GetHandler())

	httpServer := &http.Server{
		Addr:    ":8091",
		Handler: r,
	}

	go func() error {
		return httpServer.ListenAndServe()
	}()

	t.Run("PaymentFound", func(t *testing.T) {
		// Create a new HTTP request for testing
		req, _ := http.NewRequest("GET", "/api/payments/test-id", nil)

		// Create a new HTTP request recorder for recording the response
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		// Check the body is not nil
		assert.NotNil(t, w.Body)

		// Check the HTTP status code in the response
		if status := w.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
	})
	t.Run("PaymentNotFound", func(t *testing.T) {
		// Create a new HTTP request for testing with a non-existing payment ID
		req, _ := http.NewRequest("GET", "/api/payments/NonExistingID", nil)

		// Create a new HTTP request recorder for recording the response
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		// Check the HTTP status code in the response
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// validPaymentBody returns a request body that passes every validation
// rule, as a map so individual tests can mutate one field at a time.
func validPaymentBody() map[string]any {
	future := time.Now().AddDate(1, 0, 0)
	return map[string]any{
		"card_number":  "2222405343248877",
		"expiry_month": int(future.Month()),
		"expiry_year":  future.Year(),
		"currency":     "GBP",
		"amount":       100,
		"cvv":          "123",
	}
}

// newPostHandler builds a router serving only PostHandler, backed by a
// fresh in-memory repository and the given fake acquirer.
func newPostHandler(fake *acquirer.MockAcquirer) (http.Handler, *repository.PaymentsRepository) {
	repo := repository.NewPaymentsRepository()
	svc := service.New(repo, fake)
	h := NewPaymentsHandler(svc)

	r := chi.NewRouter()
	r.Post("/api/payments", h.PostHandler())
	return r, repo
}

// doPostJSON issues a POST with a fresh, random Idempotency-Key so unrelated
// test cases never collide with each other. Tests that care about key reuse
// use doPostJSONWithKey directly.
func doPostJSON(t *testing.T, handler http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doPostJSONWithKey(t, handler, body, uuid.NewString())
}

func doPostJSONWithKey(t *testing.T, handler http.Handler, body any, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/payments", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestPostPaymentHandler_Authorized(t *testing.T) {
	fake := &acquirer.MockAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{Authorized: true, AuthorizationCode: "auth-code"}, nil
		},
	}
	handler, repo := newPostHandler(fake)

	w := doPostJSON(t, handler, validPaymentBody())

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp models.PaymentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusAuthorized, resp.Status)
	assert.Equal(t, "8877", resp.CardNumberLastFour)
	assert.NotEmpty(t, resp.Id)
	assert.Equal(t, 1, repo.Count())
}

func TestPostPaymentHandler_Declined(t *testing.T) {
	fake := &acquirer.MockAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{Authorized: false}, nil
		},
	}
	handler, repo := newPostHandler(fake)

	w := doPostJSON(t, handler, validPaymentBody())

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp models.PaymentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusDeclined, resp.Status)
	assert.Equal(t, 1, repo.Count())
}

func TestPostPaymentHandler_Rejected_InvalidCardNumber(t *testing.T) {
	fake := &acquirer.MockAcquirer{}
	handler, repo := newPostHandler(fake)

	body := validPaymentBody()
	body["card_number"] = "123"

	w := doPostJSON(t, handler, body)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, fake.Calls, "acquirer must never be called for a rejected request")
	assert.Equal(t, 0, repo.Count())

	var resp models.RejectedResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusRejected, resp.Status)
	assert.Contains(t, resp.Errors, "card_number must be between 14 and 19 characters long")
}

func TestPostPaymentHandler_Rejected_InvalidCurrency(t *testing.T) {
	fake := &acquirer.MockAcquirer{}
	handler, _ := newPostHandler(fake)

	body := validPaymentBody()
	body["currency"] = "JPY"

	w := doPostJSON(t, handler, body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp models.RejectedResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusRejected, resp.Status)
	assert.Contains(t, resp.Errors[0], "currency must be one of")
}

func TestPostPaymentHandler_Rejected_MissingField(t *testing.T) {
	fake := &acquirer.MockAcquirer{}
	handler, _ := newPostHandler(fake)

	body := validPaymentBody()
	delete(body, "cvv")

	w := doPostJSON(t, handler, body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp models.RejectedResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Errors, "cvv is required")
}

func TestPostPaymentHandler_Rejected_MalformedJSON(t *testing.T) {
	fake := &acquirer.MockAcquirer{}
	handler, repo := newPostHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/payments", strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uuid.NewString())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, fake.Calls)
	assert.Equal(t, 0, repo.Count())

	var resp models.RejectedResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusRejected, resp.Status)
}

func TestPostPaymentHandler_BankUnavailable(t *testing.T) {
	fake := &acquirer.MockAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{}, acquirer.ErrBankUnavailable
		},
	}
	handler, repo := newPostHandler(fake)

	w := doPostJSON(t, handler, validPaymentBody())

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Empty(t, w.Body.Bytes())
	assert.Equal(t, 0, repo.Count())
}

func TestPostPaymentHandler_MissingIdempotencyKey(t *testing.T) {
	fake := &acquirer.MockAcquirer{}
	handler, repo := newPostHandler(fake)

	raw, err := json.Marshal(validPaymentBody())
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/payments", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, fake.Calls)
	assert.Equal(t, 0, repo.Count())

	var resp models.RejectedResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Errors, "Idempotency-Key header is required")
}

func TestPostPaymentHandler_IdempotentReplay(t *testing.T) {
	fake := &acquirer.MockAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{Authorized: true, AuthorizationCode: "auth-code"}, nil
		},
	}
	handler, repo := newPostHandler(fake)
	key := uuid.NewString()
	body := validPaymentBody()

	first := doPostJSONWithKey(t, handler, body, key)
	second := doPostJSONWithKey(t, handler, body, key)

	assert.Equal(t, http.StatusCreated, first.Code)
	assert.Equal(t, http.StatusCreated, second.Code)
	assert.Equal(t, first.Body.String(), second.Body.String(), "a replayed request must return the exact same response")
	assert.Len(t, fake.Calls, 1, "the bank must only be charged once for a retried request")
	assert.Equal(t, 1, repo.Count())
}

func TestPostPaymentHandler_IdempotencyKeyConflict(t *testing.T) {
	fake := &acquirer.MockAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{Authorized: true, AuthorizationCode: "auth-code"}, nil
		},
	}
	handler, repo := newPostHandler(fake)
	key := uuid.NewString()

	first := doPostJSONWithKey(t, handler, validPaymentBody(), key)
	assert.Equal(t, http.StatusCreated, first.Code)

	differentBody := validPaymentBody()
	differentBody["amount"] = 999
	second := doPostJSONWithKey(t, handler, differentBody, key)

	assert.Equal(t, http.StatusConflict, second.Code)
	assert.Len(t, fake.Calls, 1, "the bank must not be called again for a conflicting reused key")
	assert.Equal(t, 1, repo.Count())

	var resp models.RejectedResponse
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &resp))
	assert.Contains(t, resp.Errors, "Idempotency-Key was already used with a different request body")
}

func TestPostPaymentHandler_IdempotencyKey_RetryAfterBankUnavailable(t *testing.T) {
	calls := 0
	fake := &acquirer.MockAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			calls++
			if calls == 1 {
				return acquirer.BankResponse{}, acquirer.ErrBankUnavailable
			}
			return acquirer.BankResponse{Authorized: true, AuthorizationCode: "auth-code"}, nil
		},
	}
	handler, repo := newPostHandler(fake)
	key := uuid.NewString()
	body := validPaymentBody()

	first := doPostJSONWithKey(t, handler, body, key)
	assert.Equal(t, http.StatusServiceUnavailable, first.Code)
	assert.Equal(t, 0, repo.Count())

	second := doPostJSONWithKey(t, handler, body, key)
	assert.Equal(t, http.StatusCreated, second.Code, "a retry after a bank outage must try the bank again, not replay the failure")
	assert.Equal(t, 1, repo.Count())
}

func TestPostPaymentHandler_IdempotencyKey_ConcurrentDuplicate(t *testing.T) {
	fake := &acquirer.MockAcquirer{
		AuthorizeFunc: func(ctx context.Context, req acquirer.BankRequest) (acquirer.BankResponse, error) {
			return acquirer.BankResponse{Authorized: true, AuthorizationCode: "auth-code"}, nil
		},
	}
	handler, repo := newPostHandler(fake)
	key := uuid.NewString()
	body := validPaymentBody()

	var wg sync.WaitGroup
	results := make([]*httptest.ResponseRecorder, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = doPostJSONWithKey(t, handler, body, key)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, http.StatusCreated, results[0].Code)
	assert.Equal(t, http.StatusCreated, results[1].Code)
	assert.Equal(t, results[0].Body.String(), results[1].Body.String())
	assert.Len(t, fake.Calls, 1, "two concurrent requests with the same key must only reach the bank once")
	assert.Equal(t, 1, repo.Count())
}
