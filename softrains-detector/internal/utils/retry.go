package utils

import (
	"context"
	"time"
)

const (
	DefaultRetryBudget   = 30 * time.Second
	DefaultRetryInterval = 2 * time.Second
)

// Retry executes op until success, non-retryable error, context cancellation,
// or the retry budget expires.
func Retry(
	ctx context.Context,
	budget time.Duration,
	interval time.Duration,
	isRetryable func(error) bool,
	op func() error,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if budget <= 0 {
		budget = DefaultRetryBudget
	}
	if interval <= 0 {
		interval = DefaultRetryInterval
	}

	deadline := time.Now().Add(budget)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	for {
		err := op()
		if err == nil {
			return nil
		}
		if isRetryable != nil && !isRetryable(err) {
			return err
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}

		wait := interval
		if wait > remaining {
			wait = remaining
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
