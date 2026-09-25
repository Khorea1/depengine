package validate

import (
	"strings"
	"testing"
)

func TestResultAddRedactsSensitiveMessages(t *testing.T) {
	const secret = "redaction-sentinel"
	r := &Result{}
	r.Add(ValidationError{
		Code:    ErrInvalidValue,
		Field:   "tools.demo.methods[0]",
		Message: "parse \"https://user:" + secret + "@%zz.example/repo.git\": invalid URL escape",
	})
	if len(r.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(r.Errors))
	}
	if strings.Contains(r.Errors[0].Message, secret) {
		t.Fatalf("Result.Add() persisted credential in finding: %q", r.Errors[0].Message)
	}
	if !strings.Contains(r.Errors[0].Message, "***") {
		t.Fatalf("Result.Add() message = %q, want redacted marker", r.Errors[0].Message)
	}
}

func TestResultMergeRedactsSensitiveMessages(t *testing.T) {
	const secret = "redaction-sentinel"
	other := &Result{
		Errors: []ValidationError{{
			Code:    ErrInvalidValue,
			Field:   "tools.demo.methods[0]",
			Message: "request failed for https://user:" + secret + "@example.invalid/repo.git",
		}},
		Warnings: []ValidationError{{
			Code:    WarnEnvMissing,
			Field:   "environment",
			Message: "probe https://example.invalid/?token=" + secret,
		}},
	}
	r := &Result{}
	r.Merge(other)
	for _, finding := range r.All() {
		if strings.Contains(finding.Message, secret) {
			t.Fatalf("Result.Merge() persisted credential in finding: %q", finding.Message)
		}
	}
	if len(r.Errors) != 1 || len(r.Warnings) != 1 {
		t.Fatalf("merged result = %+v, want one error and one warning", r)
	}
}
