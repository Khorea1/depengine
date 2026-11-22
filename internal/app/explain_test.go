package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/explain"
)

func mustExplainCatalog(t *testing.T) *explain.Catalog {
	t.Helper()
	catalog, err := explain.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return catalog
}

func TestExplainRendersFieldText(t *testing.T) {
	stderr := captureStderr(t, func() {
		if err := runExplain(mustExplainCatalog(t), []string{"requires_when"}, false); err != nil {
			t.Fatalf("runExplain: %v", err)
		}
	})
	for _, want := range []string{
		"fields", "requires_when", "form:", "example: requires_when =",
		"docs:    docs/schema-reference.md#tool-to-tool-dependency",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("text output missing %q:\n%s", want, stderr)
		}
	}
}

func TestExplainJSONEnvelope(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runExplain(mustExplainCatalog(t), []string{"http", "E_DUPE_TOOL"}, true); err != nil {
			t.Fatalf("runExplain: %v", err)
		}
	})
	envelope := decodeEnvelope(t, out)
	if envelope.Command != "explain" {
		t.Fatalf("envelope command = %q, want explain", envelope.Command)
	}
	var report explainReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Query != "http E_DUPE_TOOL" {
		t.Errorf("query = %q", report.Query)
	}
	if len(report.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(report.Matches))
	}
	if report.Matches[0].Group != explain.GroupMethod || report.Matches[1].Group != explain.GroupErrorCode {
		t.Errorf("match groups = %q, %q", report.Matches[0].Group, report.Matches[1].Group)
	}
}

func TestExplainListsEveryTermWithoutArguments(t *testing.T) {
	stderr := captureStderr(t, func() {
		if err := runExplain(mustExplainCatalog(t), nil, false); err != nil {
			t.Fatalf("runExplain: %v", err)
		}
	})
	for _, want := range []string{"fields", "methods", "error codes", "resolver error classes"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("listing missing group %q", want)
		}
	}
	if !strings.Contains(stderr, "  native — ") {
		t.Errorf("listing missing the native method:\n%s", stderr[:200])
	}
}

func TestExplainUnknownTermFailsWithSuggestion(t *testing.T) {
	stderr := captureStderr(t, func() {
		err := runExplain(mustExplainCatalog(t), []string{"reques_when"}, false)
		if code := requireExitCode(t, err); code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, `no vocabulary entry for "reques_when"`) {
		t.Errorf("missing unknown-term error: %s", stderr)
	}
	if !strings.Contains(stderr, "requires_when") {
		t.Errorf("missing suggestion: %s", stderr)
	}
}

func TestExplainIsItsOwnCommand(t *testing.T) {
	setEnglishLocale(t)
	root := newRootCmd()
	found, _, err := root.Find([]string{"explain"})
	if err != nil {
		t.Fatalf("find explain: %v", err)
	}
	if found.Name() != "explain" {
		t.Fatalf("explain resolved to %q", found.CommandPath())
	}
	why, _, err := root.Find([]string{"why"})
	if err != nil {
		t.Fatalf("find why: %v", err)
	}
	if len(why.Aliases) != 0 {
		t.Fatalf("why aliases = %v, want none", why.Aliases)
	}
}
