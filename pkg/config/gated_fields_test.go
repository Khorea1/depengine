package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/engine"
)

func writeTempSchema(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "schema.toml")
	if !strings.Contains(content, "schema_version") {
		content = "schema_version = 1\n" + content
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParsePostInstallTableForm(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
font = { native = true, post_install = { cmd = "fc-cache -fv", when = { target_family = ["unix"] } } }
`)
	s, err := ParseProjectSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tool := s.Tools["font"]
	if tool == nil {
		t.Fatalf("tool font not parsed; tools: %v", keysOf(s.Tools))
	}
	if len(tool.PostInstall) != 1 || tool.PostInstall[0].Run[2] != "fc-cache -fv" {
		t.Errorf("expected cmd extracted, got %#v", tool.PostInstall)
	}
	if when := tool.PostInstall[0].When; when == nil || len(when.TargetFamily) != 1 || when.TargetFamily[0] != "unix" {
		t.Errorf("expected when target_family=[unix], got %+v", when)
	}
}

func TestParsePostInstallTableFormBlock(t *testing.T) {
	p := writeTempSchema(t, `
[tools.font]
native = true
post_install = { cmd = "fc-cache -fv", when = { target_family = ["unix"] } }
`)
	s, err := ParseProjectSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tool := s.Tools["font"]
	if tool == nil {
		t.Fatalf("tool font not parsed")
	}
	if len(tool.PostInstall) != 1 || tool.PostInstall[0].Run[2] != "fc-cache -fv" {
		t.Errorf("expected cmd extracted, got %#v", tool.PostInstall)
	}
	if when := tool.PostInstall[0].When; when == nil || when.TargetFamily[0] != "unix" {
		t.Errorf("expected when target_family=[unix], got %+v", when)
	}
}

func TestParsePostInstallStringStillWorks(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
app = { native = true, post_install = "echo done" }
`)
	s, err := ParseProjectSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tool := s.Tools["app"]
	if len(tool.PostInstall) != 1 || tool.PostInstall[0].Run[2] != "echo done" {
		t.Errorf("expected string form parsed, got %#v", tool.PostInstall)
	}
	if tool.PostInstall[0].When != nil {
		t.Errorf("string form should not set a condition, got %+v", tool.PostInstall[0].When)
	}
}

func TestParsePortableHookVariants(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
app = { native = true, pre_install = [
  { run = ["sh", "-c", "echo unix"], when = { target_family = ["unix"] } },
  { run = ["pwsh.exe", "-NoProfile", "-Command", "Write-Output windows"], when = { target_family = ["windows"] } }
] }
`)
	s, err := ParseProjectSchema(p, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hooks := s.Tools["app"].PreInstall
	if len(hooks) != 2 || hooks[0].Run[0] != "sh" || hooks[1].Run[0] != "pwsh.exe" {
		t.Fatalf("unexpected hooks: %#v", hooks)
	}
	if hooks[0].When == nil || hooks[1].When == nil {
		t.Fatalf("conditions were not parsed: %#v", hooks)
	}
}

func TestParseHookRejectsInvalidRun(t *testing.T) {
	for _, hook := range []string{
		`{ run = [] }`,
		`{ run = [""] }`,
		`{ cmd = "echo old", run = ["echo", "new"] }`,
		`[{ run = ["echo"] }, "echo mixed"]`,
	} {
		p := writeTempSchema(t, "[tools]\napp = { native = true, pre_install = "+hook+" }\n")
		if _, err := ParseProjectSchema(p, nil); err == nil {
			t.Errorf("expected invalid hook %s to fail", hook)
		}
	}
}

func TestParseRequiresWhen(t *testing.T) {
	p := writeTempSchema(t, `
[tools]
dep-a = { native = true }
dep-b = { native = true }
app = { native = true, requires = ["dep-a", "dep-b"], requires_when = { dep-b = { target_family = ["unix"] } } }
`)
	s, err := ParseProjectSchema(p, nil)
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
	if _, err := ParseProjectSchema(p, nil); err == nil {
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
