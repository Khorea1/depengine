package httpdownload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestVerifyChecksumMismatchIsSemantic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	err := VerifyChecksum(path, "sha256:"+strings.Repeat("0", 64))
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("VerifyChecksum error = %v, want ErrChecksumMismatch", err)
	}
	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("VerifyChecksum error type = %T, want *ChecksumMismatchError", err)
	}
	if mismatch.Algorithm != "sha256" || mismatch.Expected != strings.Repeat("0", 64) || mismatch.Actual == "" {
		t.Fatalf("checksum mismatch = %#v, want populated sha256 details", mismatch)
	}
}

func TestRetryWithBackoffStopsOnChecksumMismatch(t *testing.T) {
	t.Parallel()

	attempts := 0
	mismatch := &ChecksumMismatchError{
		Algorithm: "sha256",
		Expected:  "expected",
		Actual:    "actual",
	}

	err := retryWithBackoff(context.Background(), 3, 0, 0, func(context.Context) error {
		attempts++
		return fmt.Errorf("verify downloaded artifact: %w", mismatch)
	})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("retry error = %v, want ErrChecksumMismatch", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 for deterministic checksum mismatch", attempts)
	}
}

func TestRetryWithBackoffStopsOnPermanentHTTPStatus(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := retryWithBackoff(context.Background(), 3, 0, 0, func(context.Context) error {
		attempts++
		return fmt.Errorf("download: %w", &HTTPStatusError{
			URL:        "https://example.invalid/artifact",
			StatusCode: 404,
			Status:     "404 Not Found",
		})
	})
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != 404 {
		t.Fatalf("retry error = %v, want typed 404", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 for permanent HTTP status", attempts)
	}
}

func TestRetryWithBackoffRetriesRetryableHTTPStatus(t *testing.T) {
	t.Parallel()

	for _, statusCode := range []int{408, 429, 503} {
		statusCode := statusCode
		t.Run(fmt.Sprint(statusCode), func(t *testing.T) {
			t.Parallel()
			attempts := 0
			err := retryWithBackoff(context.Background(), 1, 0, 0, func(context.Context) error {
				attempts++
				if attempts == 1 {
					return &HTTPStatusError{
						URL:        "https://example.invalid/artifact",
						StatusCode: statusCode,
						Status:     fmt.Sprintf("%d transient", statusCode),
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("retry error = %v, want success", err)
			}
			if attempts != 2 {
				t.Fatalf("attempts = %d, want 2", attempts)
			}
		})
	}
}

func TestHTTPStatusErrorKeeps404Hint(t *testing.T) {
	t.Parallel()

	err := &HTTPStatusError{
		URL:        "https://example.invalid/artifact",
		StatusCode: 404,
		Status:     "404 Not Found",
	}
	if !strings.Contains(err.Error(), "arch_map/os_map") {
		t.Fatalf("404 error = %q, want compatibility hint", err)
	}
}

func TestRetryWithBackoffDoesNotClassifyDiagnosticText(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := retryWithBackoff(context.Background(), 1, 0, 0, func(context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary checksum service unavailable")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry error = %v, want success", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestRetryWithBackoffUsesExponentialBackoff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		attempts := 0

		err := retryWithBackoff(context.Background(), 3, time.Second, 10*time.Second, func(context.Context) error {
			attempts++
			if attempts == 4 {
				return nil
			}
			return errors.New("temporary transport failure")
		})
		if err != nil {
			t.Fatalf("retry error = %v, want success", err)
		}
		if attempts != 4 {
			t.Fatalf("attempts = %d, want 4", attempts)
		}
		if elapsed := time.Since(start); elapsed != 7*time.Second {
			t.Fatalf("virtual elapsed time = %v, want 7s (1s + 2s + 4s)", elapsed)
		}
	})
}

func TestRetryWithBackoffCapsDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		err := retryWithBackoff(context.Background(), 4, time.Second, 2*time.Second, func(context.Context) error {
			return errors.New("temporary transport failure")
		})
		if err == nil {
			t.Fatal("retry error = nil, want final error")
		}
		if elapsed := time.Since(start); elapsed != 7*time.Second {
			t.Fatalf("virtual elapsed time = %v, want 7s (1s + 2s + 2s + 2s)", elapsed)
		}
	})
}

func TestRetryWithBackoffCancellationInterruptsDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		attempted := make(chan struct{})
		result := make(chan error, 1)
		start := time.Now()
		attempts := 0

		go func() {
			result <- retryWithBackoff(ctx, 3, time.Hour, time.Hour, func(context.Context) error {
				attempts++
				attempted <- struct{}{}
				return errors.New("temporary transport failure")
			})
		}()

		<-attempted
		cancel()

		err := <-result
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("retry error = %v, want context.Canceled", err)
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1", attempts)
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("virtual elapsed time = %v, want 0 after cancellation", elapsed)
		}
	})
}

func TestRetryWithBackoffReturnsLastError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		want := errors.New("temporary transport failure")
		err := retryWithBackoff(context.Background(), 1, time.Second, time.Second, func(context.Context) error {
			return want
		})
		if !errors.Is(err, want) {
			t.Fatalf("retry error = %v, want wrapped final error", err)
		}
	})
}
