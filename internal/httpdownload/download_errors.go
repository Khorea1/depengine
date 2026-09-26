package httpdownload

import "fmt"

// HTTPStatusError reports a non-success HTTP response from GoDownloader.
// URL must already be redacted before constructing this error.
type HTTPStatusError struct {
	URL        string
	StatusCode int
	Status     string
}

func (e *HTTPStatusError) Error() string {
	base := fmt.Sprintf("http: %s returned %s", e.URL, e.Status)
	if e.StatusCode == 404 {
		return base + " (hint: check this tool's arch_map/os_map — the upstream release asset may use a different spelling of arch/os than this machine's own)"
	}
	return base
}

// Retryable reports whether retrying the same resolved HTTP request can
// reasonably succeed without changing configuration or credentials.
func (e *HTTPStatusError) Retryable() bool {
	if e == nil {
		return false
	}
	return e.StatusCode == 408 || e.StatusCode == 429 || e.StatusCode >= 500 && e.StatusCode <= 599
}
