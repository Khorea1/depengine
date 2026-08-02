package ecosystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
)

func TestGoBinaryName(t *testing.T) {
	cases := []struct {
		importPath string
		want       string
	}{
		// Plain last-element import paths.
		{"github.com/junegunn/fzf", "fzf"},
		{"k8s.io/kubectl", "kubectl"},
		{"example.com/foo/bar", "bar"},
		{"simple", "simple"},
		// Multi-command repo layout: the binary is the element after /cmd/
		// (which is also the last element of the import path).
		{"golang.org/x/tools/cmd/stringer", "stringer"},
		{"golang.org/x/tools/cmd/goimports", "goimports"},
		{"github.com/foo/bar/cmd/baz", "baz"},
		// Slop tolerance.
		{"", ""},
		{"/leading/trailing/", "trailing"},
	}
	for _, tc := range cases {
		if got := goBinaryName(tc.importPath); got != tc.want {
			t.Errorf("goBinaryName(%q) = %q, want %q", tc.importPath, got, tc.want)
		}
	}
}

func TestGoAdapterCanRemove(t *testing.T) {
	if !NewGoAdapter().CanRemove() {
		t.Fatal("GoAdapter.CanRemove() should be true — go removal is supported via binary deletion")
	}
}

func TestGoAdapterRemoveDeletesBinaryFromGOBIN(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	// Explicit pkg config points at the import path; the binary that
	// `go install` produced is named after the /cmd/ element.
	binPath := filepath.Join(binDir, "stringer")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after Remove (err=%v)", binPath, err)
	}
}

func TestGoAdapterRemoveFallsBackToToolNameAsImportPath(t *testing.T) {
	// `go = true` stores an empty pkg config; the import path then comes
	// from tool.Name (which in the schema is the import path itself).
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	binPath := filepath.Join(binDir, "fzf")
	if err := os.WriteFile(binPath, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "github.com/junegunn/fzf"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": ""}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after Remove (err=%v)", binPath, err)
	}
}

func TestGoAdapterRemoveUsesGOPATHBinWhenGOBINUnset(t *testing.T) {
	gopath := t.TempDir()
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", gopath)
	binPath := filepath.Join(gopath, "bin", "fzf")
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "fzf"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "github.com/junegunn/fzf"}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after Remove (err=%v)", binPath, err)
	}
}

func TestGoAdapterRemoveMissingBinaryIsIdempotent(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)

	a := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	// Binary was never installed (or already removed) — must not error.
	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove of missing binary should be a no-op, got %v", err)
	}
}

func TestGoAdapterRemoveRejectsUnresolvableImportPath(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)

	a := NewGoAdapter()
	tool := &config.Tool{Name: ""}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": ""}}

	if err := a.Remove(context.Background(), nil, tool, mc); err == nil {
		t.Fatal("Remove with empty import path should fail")
	}
}

func TestGoAdapterRemoveDoesNotTouchUnrelatedBinaries(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	unrelated := filepath.Join(binDir, "cargo")
	if err := os.WriteFile(unrelated, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated binary %s was affected by Remove: %v", unrelated, err)
	}
}
