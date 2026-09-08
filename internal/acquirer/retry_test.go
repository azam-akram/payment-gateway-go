package acquirer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingAcquirer records how many times Authorize was called and returns
// canned results in order, repeating the last one once exhausted.
type countingAcquirer struct {
	results []struct {
		resp BankResponse
		err  error
	}
	calls int
}

func (c *countingAcquirer) Authorize(ctx context.Context, req BankRequest) (BankResponse, error) {
	i := c.calls
	if i >= len(c.results) {
		i = len(c.results) - 1
	}
	c.calls++
	return c.results[i].resp, c.results[i].err
}

func testRetryConfig() RetryConfig {
	// Fast enough to keep the test suite quick, still exercises real backoff.
	return RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
}

func TestRetryingAcquirer_SucceedsFirstTry_NoRetry(t *testing.T) {
	fake := &countingAcquirer{results: []struct {
		resp BankResponse
		err  error
	}{
		{resp: BankResponse{Authorized: true, AuthorizationCode: "auth-1"}},
	}}
	r := NewRetryingAcquirer(fake, testRetryConfig())

	resp, err := r.Authorize(context.Background(), BankRequest{})

	require.NoError(t, err)
	assert.True(t, resp.Authorized)
	assert.Equal(t, 1, fake.calls)
}

func TestRetryingAcquirer_DeclinedIsNotRetried(t *testing.T) {
	fake := &countingAcquirer{results: []struct {
		resp BankResponse
		err  error
	}{
		{resp: BankResponse{Authorized: false}},
	}}
	r := NewRetryingAcquirer(fake, testRetryConfig())

	resp, err := r.Authorize(context.Background(), BankRequest{})

	require.NoError(t, err)
	assert.False(t, resp.Authorized)
	assert.Equal(t, 1, fake.calls, "a genuine decline is a decision, not a failure - it must never be retried")
}

func TestRetryingAcquirer_RetriesBankUnavailableThenSucceeds(t *testing.T) {
	fake := &countingAcquirer{results: []struct {
		resp BankResponse
		err  error
	}{
		{err: ErrBankUnavailable},
		{err: ErrBankUnavailable},
		{resp: BankResponse{Authorized: true, AuthorizationCode: "auth-1"}},
	}}
	r := NewRetryingAcquirer(fake, testRetryConfig())

	resp, err := r.Authorize(context.Background(), BankRequest{})

	require.NoError(t, err)
	assert.True(t, resp.Authorized)
	assert.Equal(t, 3, fake.calls)
}

func TestRetryingAcquirer_GivesUpAfterMaxAttempts(t *testing.T) {
	fake := &countingAcquirer{results: []struct {
		resp BankResponse
		err  error
	}{
		{err: ErrBankUnavailable},
	}}
	cfg := testRetryConfig()
	r := NewRetryingAcquirer(fake, cfg)

	_, err := r.Authorize(context.Background(), BankRequest{})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBankUnavailable))
	assert.Equal(t, cfg.MaxAttempts, fake.calls)
}

func TestRetryingAcquirer_NonBankUnavailableErrorIsNotRetried(t *testing.T) {
	localErr := errors.New("marshal bank request: boom")
	fake := &countingAcquirer{results: []struct {
		resp BankResponse
		err  error
	}{
		{err: localErr},
	}}
	r := NewRetryingAcquirer(fake, testRetryConfig())

	_, err := r.Authorize(context.Background(), BankRequest{})

	require.Error(t, err)
	assert.Same(t, localErr, err)
	assert.Equal(t, 1, fake.calls, "a local error unrelated to the bank must not be retried")
}

func TestRetryingAcquirer_StopsWhenContextCancelled(t *testing.T) {
	fake := &countingAcquirer{results: []struct {
		resp BankResponse
		err  error
	}{
		{err: ErrBankUnavailable},
	}}
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: 50 * time.Millisecond, MaxDelay: time.Second}
	r := NewRetryingAcquirer(fake, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := r.Authorize(ctx, BankRequest{})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBankUnavailable), "should surface the last real failure, not ctx.Err(), when cancelled mid-backoff")
	assert.Less(t, fake.calls, cfg.MaxAttempts, "should stop retrying once the context is cancelled")
}
