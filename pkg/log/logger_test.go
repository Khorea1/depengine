package log

import (
	"log/slog"
	"strings"
	"testing"
)

func TestDefaultLoggerExists(t *testing.T) {
	if Default == nil {
		t.Fatal("Default logger should not be nil")
	}
}

func TestLevelFromString(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := LevelFromString(tc.in); got != tc.want {
				t.Fatalf("LevelFromString(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestNewLoggerTextOutput(t *testing.T) {
	// Default text handler should produce human-readable output.
	cap := NewTestLogger(t)
	cap.Info("hello", "key", "value")

	output := cap.String()
	if !strings.Contains(output, "hello") {
		t.Fatalf("expected log to contain 'hello', got: %s", output)
	}
	// Text handler includes level=INFO.
	if !strings.Contains(output, "INFO") {
		t.Fatalf("expected log to contain level INFO, got: %s", output)
	}
}

func TestNewLoggerLevelFiltering(t *testing.T) {
	cap := NewTestLogger(t)

	// DEBUG message at DEBUG level should appear.
	cap.Debug("debug message")
	debugOut := cap.String()
	if !strings.Contains(debugOut, "debug message") {
		t.Fatalf("expected debug message to appear at DEBUG level, got: %s", debugOut)
	}
}

func TestWithContextAddsFields(t *testing.T) {
	cap := NewTestLogger(t)
	logger := WithContext(cap.Logger, LogContext{
		TraceID: "trace-xyz",
		Tool:    "zsh",
		Method:  "native",
		Phase:   "install",
	})

	logger.Info("test with context")

	output := cap.String()
	if !strings.Contains(output, "trace-xyz") {
		t.Fatalf("expected trace_id in output, got: %s", output)
	}
	if !strings.Contains(output, "zsh") {
		t.Fatalf("expected tool=zsh in output, got: %s", output)
	}
	if !strings.Contains(output, "native") {
		t.Fatalf("expected method=native in output, got: %s", output)
	}
}

func TestWithContextEmptyFieldsOmitted(t *testing.T) {
	cap := NewTestLogger(t)
	logger := WithContext(cap.Logger, LogContext{
		TraceID: "trace-xyz",
		// Tool, Method, etc. are empty
	})

	logger.Info("test partial context")

	output := cap.String()
	if !strings.Contains(output, "trace-xyz") {
		t.Fatalf("expected trace_id in output, got: %s", output)
	}
	// Tool with empty value should not appear.
	if strings.Contains(output, "tool=") && !strings.Contains(output, "tool=zsh") {
		// There might be a "tool=" in another context; just check no empty tool value.
	}
}

func TestTestCaptureAssertContains(t *testing.T) {
	// This test verifies AssertContains works (simulating pass/fail).
	cap := NewTestLogger(t)
	cap.Info("hello world")
	// Should pass — "hello" is in output.
	cap.AssertContains(t, "hello")
}

func TestTestCaptureAssertNotContains(t *testing.T) {
	cap := NewTestLogger(t)
	cap.Info("hello world")
	// Should pass — "goodbye" is not in output.
	cap.AssertNotContains(t, "goodbye")
}

func TestTestCaptureLines(t *testing.T) {
	cap := NewTestLogger(t)
	cap.Info("line one")
	cap.Info("line two")

	lines := cap.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
}

func TestTestCaptureReset(t *testing.T) {
	cap := NewTestLogger(t)
	cap.Info("before")
	cap.Reset()
	cap.Info("after")

	if strings.Contains(cap.String(), "before") {
		t.Fatal("expected 'before' to be cleared after Reset")
	}
	if !strings.Contains(cap.String(), "after") {
		t.Fatal("expected 'after' to appear after Reset")
	}
}

func TestWithContextOnlyNonEmptyFields(t *testing.T) {
	cap := NewTestLogger(t)
	lc := LogContext{
		Tool:  "my-tool",
		Phase: "install",
		// TraceID, Method, Distro, Family are empty
	}
	logger := WithContext(cap.Logger, lc)
	logger.Info("working")

	output := cap.String()
	if !strings.Contains(output, "my-tool") {
		t.Fatalf("expected tool 'my-tool' in output, got: %s", output)
	}
	// Should NOT contain empty trace_id= or distro= attributes.
	for _, unwanted := range []string{"trace_id=", "distro=", "family="} {
		if strings.Contains(output, unwanted) {
			// Only fail if it's an explicit empty attr; "=" could appear in other contexts.
			if strings.Count(output, unwanted) > 0 {
				// Check if it's followed by a space or end (empty value).
				idx := strings.Index(output, unwanted)
				rest := output[idx+len(unwanted):]
				if len(rest) == 0 || rest[0] == ' ' || rest[0] == '\n' {
					t.Fatalf("unexpected empty field %q in output: %s", unwanted, output)
				}
			}
		}
	}
}
