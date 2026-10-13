package exec

import (
	"errors"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ghrelease"
)

// ErrIntegrityMismatch identifies integrity verification failures where the
// downloaded bytes do not match the declared trust inputs (checksum,
// signature). It is terminal per the github-distributions contract: a bad
// checksum must never silently switch source to another method. Adapters
// signal it by returning *IntegrityError or by wrapping this sentinel with
// %w; the executor classifies with errors.Is/errors.As, never by text.
//
// NOTE for adapter authors (including a future internal/httpdownload pass):
// that package already defines its own mismatch error, which this package
// cannot import without an import cycle. Until the adapters wrap this
// sentinel, only errors explicitly marked with it (or *IntegrityError) take
// the terminal path; unmarked install failures keep historic fallback.
var ErrIntegrityMismatch = errors.New("integrity verification failed")

// IntegrityError is the typed terminal form of an integrity failure observed
// while installing one method candidate. Method names the failing candidate
// for diagnostics; Reason carries the human detail; Err carries the
// underlying cause when one exists.
type IntegrityError struct {
	Method string
	Reason string
	Err    error
}

func (e *IntegrityError) Error() string {
	if e == nil {
		return ErrIntegrityMismatch.Error()
	}
	detail := e.Reason
	if detail == "" && e.Err != nil {
		detail = e.Err.Error()
	}
	if detail == "" {
		detail = "integrity check failed"
	}
	if e.Method == "" {
		return fmt.Sprintf("%s: %s", ErrIntegrityMismatch, detail)
	}
	return fmt.Sprintf("%s: %s: %s", ErrIntegrityMismatch, e.Method, detail)
}

// Is classifies the failure without parsing diagnostic text.
func (e *IntegrityError) Is(target error) bool { return target == ErrIntegrityMismatch }

// Unwrap exposes the underlying cause, if any.
func (e *IntegrityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// terminalError marks a method failure that must abort the tool instead of
// falling back to the next candidate. It wraps (never rewords) the cause so
// errors.As/errors.Is keep finding the original typed error through it.
type terminalError struct {
	err error
}

func (e *terminalError) Error() string { return e.err.Error() }
func (e *terminalError) Unwrap() error { return e.err }

// IsTerminalMethodError reports whether a method failure aborts the tool
// (terminal) or allows fallback to the next candidate (recoverable). This is
// the single classification point shared by the serial executor, lock
// resolution, state verification, and explanation: callers branch on this,
// never on error text.
//
// Terminal (abort the tool, never silent fallback):
//   - ambiguous distributions (*config.AmbiguousDistributionsError) and any
//     other distribution specialization failure (wrapped in terminalError by
//     specializeMethodForHost: an invalid effective configuration is a broken
//     manifest, not an unavailable package);
//   - asset resolution mismatch or invalid selectors from ghrelease
//     (zero/multiple catalog matches, uncompilable selector);
//   - integrity failures (*IntegrityError / ErrIntegrityMismatch).
//
// Recoverable (try the next method as before):
//   - ineligibility (no matching distribution, when mismatch) — these are not
//     errors at all, they skip;
//   - transport, API, and availability failures (fetch errors, missing
//     packages, unavailable adapters);
//   - anything unmarked, preserving the historic v1 fallback for candidates
//     without distributions.
func IsTerminalMethodError(err error) bool {
	if err == nil {
		return false
	}
	var ambiguous *config.AmbiguousDistributionsError
	if errors.As(err, &ambiguous) {
		return true
	}
	var terminal *terminalError
	if errors.As(err, &terminal) {
		return true
	}
	var assetErr *ghrelease.AssetResolutionError
	if errors.As(err, &assetErr) {
		return true
	}
	if errors.Is(err, ghrelease.ErrNoAssetMatch) ||
		errors.Is(err, ghrelease.ErrAmbiguousAssetMatch) ||
		errors.Is(err, ghrelease.ErrInvalidAssetSelector) {
		return true
	}
	var integrityErr *IntegrityError
	if errors.As(err, &integrityErr) {
		return true
	}
	return errors.Is(err, ErrIntegrityMismatch)
}
