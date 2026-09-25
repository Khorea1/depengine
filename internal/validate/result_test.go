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
