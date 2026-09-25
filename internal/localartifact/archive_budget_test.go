package localartifact

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestArchiveExpansionBudgetCountsAggregateWritesAtBoundary(t *testing.T) {
	budget := &archiveExpansionBudget{limit: 5}
	var first, second bytes.Buffer
	if _, err := budget.writer(&first).Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.writer(&second).Write([]byte("de")); err != nil {
		t.Fatalf("write at budget boundary: %v", err)
	}
	if budget.used != 5 || first.String()+second.String() != "abcde" {
		t.Fatalf("used = %d, output = %q", budget.used, first.String()+second.String())
	}
	if n, err := budget.writer(&second).Write([]byte("f")); n != 0 || err == nil {
		t.Fatalf("write beyond budget = (%d, %v), want rejection", n, err)
	}
	if second.String() != "de" || budget.used != 5 {
		t.Fatalf("over-budget write changed output or accounting: %q, %d", second.String(), budget.used)
	}
}

func TestArchiveExpansionBudgetRejectsCrossingWriteAtBoundary(t *testing.T) {
	budget := &archiveExpansionBudget{limit: 5}
	var out bytes.Buffer
	n, err := io.Copy(budget.writer(&out), strings.NewReader("abcdef"))
	if n != 5 || err == nil {
		t.Fatalf("over-limit copy = (%d, %v), want 5 bytes then rejection", n, err)
	}
	if out.String() != "abcde" || budget.used != 5 {
		t.Fatalf("over-budget copy changed output or accounting: %q, %d", out.String(), budget.used)
	}
}

func TestArchiveExpansionBudgetCapsEntryCount(t *testing.T) {
	budget := &archiveExpansionBudget{limit: 1, entryLimit: 2}
	if err := budget.accountEntry("a"); err != nil {
		t.Fatal(err)
	}
	if err := budget.accountEntry("b"); err != nil {
		t.Fatal(err)
	}
	if err := budget.accountEntry("c"); err == nil {
		t.Fatal("third entry unexpectedly accepted")
	}
	if budget.entries != 2 {
		t.Fatalf("entries = %d, want 2", budget.entries)
	}
}

func TestArchiveExpansionBudgetCapsPathComplexity(t *testing.T) {
	budget := newArchiveExpansionBudget()
	if err := budget.accountEntry(strings.Repeat("a", archivePathByteLimit+1)); err == nil {
		t.Fatal("oversized entry name unexpectedly accepted")
	}

	deep := strings.Repeat("a/", archivePathDepthLimit) + "a"
	if err := budget.accountEntry(deep); err == nil {
		t.Fatal("over-deep entry path unexpectedly accepted")
	}
}

