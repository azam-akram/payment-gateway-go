package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/idempotency"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/service"
	"github.com/go-chi/chi/v5"
)

// idempotencyKeyHeader is required on every POST /api/payments so a retried
// or duplicated request never charges the bank twice - see decision.md D12.
const idempotencyKeyHeader = "Idempotency-Key"

type PaymentsHandler struct {
	service     *service.PaymentService
	idempotency *idempotency.Store
}

func NewPaymentsHandler(svc *service.PaymentService) *PaymentsHandler {
	return &PaymentsHandler{
		service:     svc,
		idempotency: idempotency.NewStore(),
	}
}

// GetHandler returns an http.HandlerFunc that handles HTTP GET requests.
// It retrieves a payment record by its ID from the storage.
// The ID is expected to be part of the URL.
func (h *PaymentsHandler) GetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		payment, found := h.service.GetPayment(id)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		writeJSON(w, http.StatusOK, payment)
	}
}

func (h *PaymentsHandler) PostHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get(idempotencyKeyHeader)
		if key == "" {
			writeJSON(w, http.StatusBadRequest, models.RejectedResponse{
				Status: models.StatusRejected,
				Errors: []string{idempotencyKeyHeader + " header is required"},
			})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, models.RejectedResponse{
				Status: models.StatusRejected,
				Errors: []string{"failed to read request body: " + err.Error()},
			})
			return
		}
		bodyHash := idempotency.HashBody(body)

		// Serializes every request sharing this key, so a concurrent
		// duplicate waits for the first attempt instead of racing it to
		// the bank.
		unlock := h.idempotency.Lock(key)
		defer unlock()

		if rec, ok := h.idempotency.Get(key); ok {
			if rec.RequestHash != bodyHash {
				writeJSON(w, http.StatusConflict, models.RejectedResponse{
					Status: models.StatusRejected,
					Errors: []string{idempotencyKeyHeader + " was already used with a different request body"},
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(rec.StatusCode)
			w.Write(rec.Body)
			return
		}

		var req models.PaymentRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, models.RejectedResponse{
				Status: models.StatusRejected,
				Errors: []string{"request body must be valid JSON: " + err.Error()},
			})
			return
		}

		payment, rejected, err := h.service.ProcessPayment(r.Context(), req)
		switch {
		case err != nil:
			// Bank unavailable: the outcome is unknown, so it must not be
			// cached - a retry with the same key should try the bank again.
			w.WriteHeader(http.StatusServiceUnavailable)
		case rejected != nil:
			// Validation failed before the bank was ever called: cheap to
			// redo, so it isn't cached either - a corrected retry with the
			// same key should be validated fresh, not replayed.
			writeJSON(w, http.StatusBadRequest, rejected)
		default:
			respBody, err := json.Marshal(payment)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			h.idempotency.Put(key, idempotency.Record{
				RequestHash: bodyHash,
				StatusCode:  http.StatusCreated,
				Body:        respBody,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write(respBody)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
