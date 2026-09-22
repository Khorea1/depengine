package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestCapture wraps a logger that writes to an internal buffer, recording
// all log output for assertion in tests. Use NewTestLogger to create one.
type TestCapture struct {
	*slog.Logger
	buf *bytes.Buffer
}

// NewTestLogger creates a TestCapture that records all log output at
// DEBUG level. The returned *TestCapture implements both the logger
// interface and the assertion helpers.
//
// Usage:
//
//	cap := log.NewTestLogger(t)
//	doSomethingThatLogs(cap.Logger)
//	cap.AssertContains("installed via native")
func NewTestLogger(t *testing.T) *TestCapture {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := New(buf, slog.LevelDebug)
	return &TestCapture{Logger: logger, buf: buf}
}

// Lines returns all log lines as a string slice, split by newline.
// Empty lines are omitted.
func (tc *TestCapture) Lines() []string {
	all := tc.buf.String()
	raw := strings.Split(all, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// String returns the full captured log output as a single string.
func (tc *TestCapture) String() string {
	return tc.buf.String()
}

// AssertContains fails the test if substr is not found in any log line.
func (tc *TestCapture) AssertContains(t *testing.T, substr string) {
	t.Helper()
	all := tc.buf.String()
	if !strings.Contains(all, substr) {
		t.Fatalf("expected log output to contain %q.\nFull output:\n%s", substr, all)
	}
}

// AssertNotContains fails the test if substr is found in any log line.
func (tc *TestCapture) AssertNotContains(t *testing.T, substr string) {
	t.Helper()
	all := tc.buf.String()
	if strings.Contains(all, substr) {
		t.Fatalf("expected log output NOT to contain %q.\nFull output:\n%s", substr, all)
	}
}

// AssertLineCount fails if the number of non-empty log lines != want.
func (tc *TestCapture) AssertLineCount(t *testing.T, want int) {
	t.Helper()
	got := len(tc.Lines())
	if got != want {
		t.Fatalf("expected %d log lines, got %d.\nFull output:\n%s", want, got, tc.buf.String())
	}
}

// Reset clears the captured output. Useful when reusing a TestCapture
// across multiple sub-tests.
func (tc *TestCapture) Reset() {
	tc.buf.Reset()
}
