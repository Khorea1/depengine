package httpdownload

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// retryWithBackoff retries fn up to maxRetries+1 times (1 initial + maxRetries)
// with exponential backoff: baseDelay, baseDelay*2, baseDelay*4, ... capped at maxDelay.
// Does NOT retry if ctx is cancelled, or if the error contains "checksum".
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

		// Don't retry if context is cancelled or expired.
		if ctx.Err() != nil {
			return err
		}

		// Don't retry if it's a checksum error — that's handled separately.
		if strings.Contains(strings.ToLower(err.Error()), "checksum") {
			return err
		}

		// Otherwise, retry.
	}

	return fmt.Errorf("%w (after %d retries)", lastErr, maxRetries)
}
