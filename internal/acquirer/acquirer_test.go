package acquirer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRequest() BankRequest {
	return BankRequest{
		CardNumber: "2222405343248877",
		ExpiryDate: "04/2025",
		Currency:   "GBP",
		Amount:     100,
		Cvv:        "123",
	}
}

func TestHTTPAcquirer_Authorize_Success(t *testing.T) {
	var receivedBody BankRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/payments", r.URL.Path)

		require.NoError(t, json.NewDecoder(r.Body).Decode(&receivedBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BankResponse{Authorized: true, AuthorizationCode: "auth-code-123"})
	}))
	defer server.Close()

	a := NewHTTPAcquirer(server.URL)
	resp, err := a.Authorize(context.Background(), testRequest())

	require.NoError(t, err)
	assert.Equal(t, testRequest(), receivedBody)
	assert.True(t, resp.Authorized)
	assert.Equal(t, "auth-code-123", resp.AuthorizationCode)
}

func TestHTTPAcquirer_Authorize_Declined(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BankResponse{Authorized: false})
	}))
	defer server.Close()

	a := NewHTTPAcquirer(server.URL)
	resp, err := a.Authorize(context.Background(), testRequest())

	require.NoError(t, err)
	assert.False(t, resp.Authorized)
}

func TestHTTPAcquirer_Authorize_503MapsToErrBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	a := NewHTTPAcquirer(server.URL)
	_, err := a.Authorize(context.Background(), testRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBankUnavailable))
}

func TestHTTPAcquirer_Authorize_UnexpectedStatusMapsToErrBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	a := NewHTTPAcquirer(server.URL)
	_, err := a.Authorize(context.Background(), testRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBankUnavailable))
}

func TestHTTPAcquirer_Authorize_MalformedBodyMapsToErrBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	a := NewHTTPAcquirer(server.URL)
	_, err := a.Authorize(context.Background(), testRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBankUnavailable))
}

func TestHTTPAcquirer_Authorize_UnreachableServerMapsToErrBankUnavailable(t *testing.T) {
	// Point at a server that is immediately closed, so the connection is
	// refused - simulates the bank being unreachable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	a := NewHTTPAcquirer(server.URL)
	_, err := a.Authorize(context.Background(), testRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBankUnavailable))
}

func TestHTTPAcquirer_Authorize_TimeoutMapsToErrBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BankResponse{Authorized: true})
	}))
	defer server.Close()

	a := NewHTTPAcquirer(server.URL)
	a.client.Timeout = 5 * time.Millisecond

	_, err := a.Authorize(context.Background(), testRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBankUnavailable))
}
