package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/models"
	"github.com/cko-recruitment/payment-gateway-challenge-go/internal/service"
	"github.com/go-chi/chi/v5"
)

type PaymentsHandler struct {
	service *service.PaymentService
}

func NewPaymentsHandler(svc *service.PaymentService) *PaymentsHandler {
	return &PaymentsHandler{
		service: svc,
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

		var req models.PaymentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, models.RejectedResponse{
				Status: models.StatusRejected,
				Errors: []string{"request body must be valid JSON: " + err.Error()},
			})
			return
		}

		payment, rejected, err := h.service.ProcessPayment(r.Context(), req)
		switch {
		case err != nil:
			w.WriteHeader(http.StatusServiceUnavailable)
		case rejected != nil:
			writeJSON(w, http.StatusBadRequest, rejected)
		default:
			writeJSON(w, http.StatusCreated, payment)
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
