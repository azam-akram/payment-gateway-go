package acquirer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const defaultTimeout = 5 * time.Second

// ErrBankUnavailable means no authorization decision could be obtained from
// the acquiring bank: a transport-level error, a timeout, a non-200 status,
// or a response body that couldn't be parsed.
var ErrBankUnavailable = errors.New("acquiring bank unavailable")

type BankRequest struct {
	CardNumber string `json:"card_number"`
	ExpiryDate string `json:"expiry_date"`
	Currency   string `json:"currency"`
	Amount     int    `json:"amount"`
	Cvv        string `json:"cvv"`
}

type BankResponse struct {
	Authorized        bool   `json:"authorized"`
	AuthorizationCode string `json:"authorization_code"`
}

// Acquirer sends a payment authorization request to the acquiring bank.
type Acquirer interface {
	Authorize(ctx context.Context, req BankRequest) (BankResponse, error)
}

type HTTPAcquirer struct {
	baseURL string
	client  *http.Client
}

func NewHTTPAcquirer(baseURL string) *HTTPAcquirer {
	return &HTTPAcquirer{
		baseURL: baseURL,
		client:  &http.Client{Timeout: defaultTimeout},
	}
}

func (a *HTTPAcquirer) Authorize(ctx context.Context, req BankRequest) (BankResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return BankResponse{}, fmt.Errorf("marshal bank request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/payments", bytes.NewReader(body))
	if err != nil {
		return BankResponse{}, fmt.Errorf("build bank request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return BankResponse{}, fmt.Errorf("%w: %v", ErrBankUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return BankResponse{}, fmt.Errorf("%w: bank returned status %d", ErrBankUnavailable, resp.StatusCode)
	}

	var bankResp BankResponse
	if err := json.NewDecoder(resp.Body).Decode(&bankResp); err != nil {
		return BankResponse{}, fmt.Errorf("%w: decoding response: %v", ErrBankUnavailable, err)
	}

	return bankResp, nil
}
