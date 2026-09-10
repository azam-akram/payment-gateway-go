package acquirer

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"
)

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 3,
	BaseDelay:   100 * time.Millisecond,
	MaxDelay:    1 * time.Second,
}

type RetryingAcquirer struct {
	next   Acquirer
	config RetryConfig
}

func NewRetryingAcquirer(next Acquirer, config RetryConfig) *RetryingAcquirer {
	return &RetryingAcquirer{next: next, config: config}
}

func (r *RetryingAcquirer) Authorize(ctx context.Context, req BankRequest) (BankResponse, error) {
	var lastErr error

	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		if attempt > 1 {
			delay := backoff(r.config, attempt)
			slog.WarnContext(ctx, "retrying bank authorization",
				"attempt", attempt,
				"max_attempts", r.config.MaxAttempts,
				"delay_ms", delay.Milliseconds(),
				"error", lastErr,
			)
			if err := sleep(ctx, delay); err != nil {
				return BankResponse{}, lastErr
			}
		}

		resp, err := r.next.Authorize(ctx, req)
		if err == nil {
			return resp, nil
		}
		if !errors.Is(err, ErrBankUnavailable) {
			return BankResponse{}, err
		}
		lastErr = err
	}

	slog.ErrorContext(ctx, "bank unavailable after all retry attempts",
		"max_attempts", r.config.MaxAttempts,
		"error", lastErr,
	)
	return BankResponse{}, lastErr
}

func backoff(cfg RetryConfig, attempt int) time.Duration {
	delay := cfg.BaseDelay << (attempt - 2)
	if delay <= 0 || delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	return time.Duration(rand.Int63n(int64(delay) + 1))
}

// sleep waits for d, or returns ctx.Err() if the context is cancelled
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
