package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/pkg/engine"
)

func writeTempSchema(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParsePostInstallTableForm(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
font = { native = true, postinstall = { cmd = "fc-cache -fv", when = { target_family = ["unix"] } } }
`)
	s, err := ParseSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tool := s.Tools["font"]
	if tool == nil {
		t.Fatalf("tool font not parsed; tools: %v", keysOf(s.Tools))
	}
	if tool.PostInstall != "fc-cache -fv" {
		t.Errorf("expected cmd extracted, got %q", tool.PostInstall)
	}
	if tool.PostInstallWhen == nil || len(tool.PostInstallWhen.TargetFamily) != 1 || tool.PostInstallWhen.TargetFamily[0] != "unix" {
		t.Errorf("expected when target_family=[unix], got %+v", tool.PostInstallWhen)
	}
}

func TestParsePostInstallTableFormBlock(t *testing.T) {
	p := writeTempSchema(t, `
[tools.font]
native = true
postinstall = { cmd = "fc-cache -fv", when = { target_family = ["unix"] } }
`)
	s, err := ParseSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tool := s.Tools["font"]
	if tool == nil {
		t.Fatalf("tool font not parsed")
	}
	if tool.PostInstall != "fc-cache -fv" {
		t.Errorf("expected cmd extracted, got %q", tool.PostInstall)
	}
	if tool.PostInstallWhen == nil || tool.PostInstallWhen.TargetFamily[0] != "unix" {
		t.Errorf("expected when target_family=[unix], got %+v", tool.PostInstallWhen)
	}
}

func TestParsePostInstallStringStillWorks(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
app = { native = true, postinstall = "echo done" }
`)
	s, err := ParseSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tool := s.Tools["app"]
	if tool.PostInstall != "echo done" {
		t.Errorf("expected string form parsed, got %q", tool.PostInstall)
	}
	if tool.PostInstallWhen != nil {
		t.Errorf("string form should not set PostInstallWhen, got %+v", tool.PostInstallWhen)
	}
}

func TestParseRequiresWhen(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
dep-a = { native = true }
dep-b = { native = true }
app = { native = true, requires = ["dep-a", "dep-b"], requires_when = { dep-b = { target_family = ["unix"] } } }
`)
	s, err := ParseSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tool := s.Tools["app"]
	if len(tool.Requires) != 2 {
		t.Fatalf("expected 2 requires, got %v", tool.Requires)
	}
	c, ok := tool.RequiresWhen["dep-b"]
	if !ok || c == nil || c.TargetFamily[0] != "unix" {
		t.Fatalf("expected requires_when.dep-b unix gate, got %+v", tool.RequiresWhen)
	}
}

func TestParseRequiresWhenRejectsNonTable(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
dep = { native = true }
app = { native = true, requires = ["dep"], requires_when = { dep = "unix" } }
`)
	if _, err := ParseSchema(p, nil); err == nil {
		t.Fatal("expected parse error for non-table requires_when entry")
	}
}

func TestEffectiveRequiresFiltering(t *testing.T) {
	unix := &engine.Facts{OS: "linux", TargetFamily: "unix"}
	windows := &engine.Facts{OS: "windows", TargetFamily: "windows"}

	tool := &Tool{
		Name:     "app",
		Requires: []string{"unzip", "fontconfig"},
		RequiresWhen: map[string]*Condition{
			"fontconfig": {TargetFamily: []string{"unix"}},
		},
	}

	got := tool.EffectiveRequires(unix)
	if len(got) != 2 {
		t.Errorf("unix: expected both deps, got %v", got)
	}
	got = tool.EffectiveRequires(windows)
	if len(got) != 1 || got[0] != "unzip" {
		t.Errorf("windows: expected only unzip, got %v", got)
	}

	// nil facts = no filtering (graph validation sees the union).
	got = tool.EffectiveRequires(nil)
	if len(got) != 2 {
		t.Errorf("nil facts: expected union, got %v", got)
	}
}

func TestFilteredToolsClonesOnlyGated(t *testing.T) {
	windows := &engine.Facts{OS: "windows", TargetFamily: "windows"}
	plain := &Tool{Name: "plain", Requires: []string{"x"}}
	gated := &Tool{
		Name:         "gated",
		Requires:     []string{"x", "y"},
		RequiresWhen: map[string]*Condition{"y": {TargetFamily: []string{"unix"}}},
	}
	tools := map[string]*Tool{"plain": plain, "gated": gated}

	out := FilteredTools(tools, windows)
	if out["plain"] != plain {
		t.Error("ungated tool should be passed through (no clone)")
	}
	if out["gated"] == gated {
		t.Error("gated tool must be cloned")
	}
	if len(out["gated"].Requires) != 1 {
		t.Errorf("expected filtered requires [x], got %v", out["gated"].Requires)
	}
}

func keysOf(m map[string]*Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
