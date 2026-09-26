package httpdownload

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// retryWithBackoff retries fn up to maxRetries+1 times (1 initial + maxRetries)
// with exponential backoff: baseDelay, baseDelay*2, baseDelay*4, ... capped at maxDelay.
// Does NOT retry if ctx is cancelled, the error is a checksum mismatch, or a typed HTTP status is permanent.
func retryWithBackoff(ctx context.Context, maxRetries int, baseDelay, maxDelay time.Duration, fn func(context.Context) error) error {
	var lastErr error
	delay := baseDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("retry %d cancelled: %w (last error: %w)", attempt, ctx.Err(), lastErr)
			case <-time.After(delay):
			}
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// Cancellation wins over the attempt error once the context is done.
		// This keeps cancellation semantics deterministic even when cancellation
		// races with fn returning a transient transport error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("retry cancelled: %w (last error: %w)", ctxErr, err)
		}

		// Checksum mismatches are deterministic for the downloaded bytes. Keep
		// retry policy machine-readable instead of depending on diagnostic text.
		if errors.Is(err, ErrChecksumMismatch) {
			return err
		}

		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) && !statusErr.Retryable() {
			return err
		}

		// Otherwise, retry.
	}

	return fmt.Errorf("%w (after %d retries)", lastErr, maxRetries)
}
