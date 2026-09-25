package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
)

func TestResolveInstallManifestPath(t *testing.T) {
	cases := []struct {
		name       string
		noManifest bool
		flag       string
		def        string
		wantPath   string
		wantAuto   bool
	}{
		{"no-manifest ignores default", true, "", "/x/manifest.toml", "", false},
		{"explicit flag wins over default", false, "/custom/manifest.toml", "/x/manifest.toml", "/custom/manifest.toml", false},
		{"empty default means no manifest", false, "", "", "", false},
		{"default auto-detected", false, "", "/x/manifest.toml", "/x/manifest.toml", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPath, gotAuto := resolveInstallManifestPath(tc.noManifest, tc.flag, tc.def)
			if gotPath != tc.wantPath || gotAuto != tc.wantAuto {
				t.Fatalf("got (%q,%v) want (%q,%v)", gotPath, gotAuto, tc.wantPath, tc.wantAuto)
			}
		})
	}
}

func TestValidateInstallSortBy(t *testing.T) {
	lg := log.Default
	if err := validateInstallSortBy("", lg); err != nil {
		t.Fatalf("empty sort must pass, got %v", err)
	}
	for _, valid := range []string{"name", "status", "method"} {
		if err := validateInstallSortBy(valid, lg); err != nil {
			t.Fatalf("sort %q must pass, got %v", valid, err)
		}
	}
	err := validateInstallSortBy("bogus", lg)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("invalid sort must return ExitError, got %T (%v)", err, err)
	}
	if exitErr.Code != 2 {
		t.Fatalf("invalid sort exit code = %d, want 2", exitErr.Code)
	}
}

func TestInstallOutputMode(t *testing.T) {
	cases := []struct {
		name       string
		json, q, v bool
		want       string
	}{
		{"json wins over all", true, true, true, "json"},
		{"quiet selects detail", false, true, false, "detail"},
		{"verbose selects detail", false, false, true, "detail"},
		{"plain selects summary", false, false, false, "summary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := installOutputMode(tc.json, tc.q, tc.v); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestShouldWarnDeprecatedVerbose(t *testing.T) {
	t.Run("explicit verbose", func(t *testing.T) {
		cmd := newInstallCmd()
		if err := cmd.Flags().Set("verbose", "true"); err != nil {
			t.Fatal(err)
		}
		if !shouldWarnDeprecatedVerbose(cmd) {
			t.Fatal("explicit --verbose should emit the deprecation warning")
		}
	})

	t.Run("explicit verbose false", func(t *testing.T) {
		cmd := newInstallCmd()
		if err := cmd.Flags().Set("verbose", "false"); err != nil {
			t.Fatal(err)
		}
		if !shouldWarnDeprecatedVerbose(cmd) {
			t.Fatal("explicit --verbose=false should emit the deprecation warning")
		}
	})

	t.Run("diagnose implied verbose", func(t *testing.T) {
		cmd := newInstallCmd()
		if err := cmd.Flags().Set("diagnose", "true"); err != nil {
			t.Fatal(err)
		}
		if shouldWarnDeprecatedVerbose(cmd) {
			t.Fatal("--diagnose should not emit a deprecation warning for --verbose it implied")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cmd := newInstallCmd()
		if shouldWarnDeprecatedVerbose(cmd) {
			t.Fatal("default install should not emit the --verbose deprecation warning")
		}
	})
}

func TestInstallExitForReport(t *testing.T) {
	if err := installExitForReport(&exec.ExecReport{Success: 3}); err != nil {
		t.Fatalf("clean report must exit nil, got %v", err)
	}
	err := installExitForReport(&exec.ExecReport{Success: 1, Failed: 2})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("failed report must return ExitError, got %T (%v)", err, err)
	}
	if exitErr.Code != 1 {
		t.Fatalf("failed report exit code = %d, want 1", exitErr.Code)
	}
}

func TestShouldShareInstallHint(t *testing.T) {
	if !shouldShareInstallHint(&exec.ExecReport{Success: 1}, false) {
		t.Fatal("successful real install should hint")
	}
	if shouldShareInstallHint(&exec.ExecReport{Success: 1}, true) {
		t.Fatal("dry run must not hint")
	}
	if shouldShareInstallHint(&exec.ExecReport{Success: 1, Failed: 1}, false) {
		t.Fatal("failed install must not hint")
	}
	if shouldShareInstallHint(&exec.ExecReport{}, false) {
		t.Fatal("zero successes must not hint")
	}
}

func TestResolveInstallLockFrozenRejectsBeforeApplyingStaleLock(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	originalURL := "https://example.test/releases/{latest}/tool.tar.gz"
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": originalURL}}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": tool}}
	lk := &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			"tool/http/0": {Latest: "v1.2.3"},
		},
		MethodsHash: map[string]string{"tool": "stale"},
	}
	if err := lock.Save(lock.DefaultPath(schemaPath), lk); err != nil {
		t.Fatal(err)
	}

	_, err := resolveInstallLock(context.Background(), installPlan{schema: schemaPath, frozen: true}, schema, log.Default)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("resolveInstallLock() error = %v, want exit code 2", err)
	}
	if got := method.Config["url"]; got != originalURL {
		t.Fatalf("stale frozen lock mutated schema before rejection: got %v want %s", got, originalURL)
	}
}

func TestResolveInstallLockFrozenRejectsUnreadableLock(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(lock.DefaultPath(schemaPath), []byte("not = [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{}}

	_, err := resolveInstallLock(context.Background(), installPlan{schema: schemaPath, frozen: true}, schema, log.Default)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("resolveInstallLock() error = %v, want exit code 2", err)
	}
}

func TestSaveLockfilePreservesOmittedMethodIdentity(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "depengine.lock")
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"selected": {
			Name: "selected",
			Methods: []*config.MethodCandidate{{
				Kind:   "native",
				Config: map[string]any{"pkg": "selected"},
			}},
		},
	}}
	old := &lock.Lock{
		Version:     1,
		Tools:       map[string]lock.ToolPin{"omitted/http/0": {Latest: "v9.9.9"}},
		MethodsHash: map[string]string{"omitted": "preserve-me"},
	}

	saveLockfile(context.Background(), schema, lockPath, old, log.Default, false)

	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("saveLockfile() did not write lock")
	}
	if got.MethodsHash["omitted"] != "preserve-me" {
		t.Fatalf("omitted method identity = %q, want preserved hash", got.MethodsHash["omitted"])
	}
	if got.Tools["omitted/http/0"].Latest != "v9.9.9" {
		t.Fatalf("omitted pin was not preserved: %#v", got.Tools["omitted/http/0"])
	}
}

func TestSaveLockfilePreservesExistingCompositePinFields(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "depengine.lock")
	newChecksum := "sha256:" + strings.Repeat("b", 64)
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{{
				Kind: "http",
				Config: map[string]any{
					"url":      "https://example.test/releases/v1.2.3/tool.tar.gz",
					"checksum": newChecksum,
				},
			}},
		},
	}}
	old := &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			"tool/http/0": {
				Latest:   "v1.2.3",
				Checksum: "sha256:" + strings.Repeat("a", 64),
			},
		},
		MethodsHash: map[string]string{"tool": "old-hash"},
	}

	saveLockfile(context.Background(), schema, lockPath, old, log.Default, false)

	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	pin := got.Tools["tool/http/0"]
	if pin.Latest != "v1.2.3" {
		t.Fatalf("Latest = %q, want preserved v1.2.3", pin.Latest)
	}
	if pin.Checksum != newChecksum {
		t.Fatalf("Checksum = %q, want newly resolved %q", pin.Checksum, newChecksum)
	}
	if got.MethodsHash["tool"] != "old-hash" {
		t.Fatalf("MethodsHash = %q, want old-hash until explicit update", got.MethodsHash["tool"])
	}
}

func TestResolveInstallLockFrozenAcceptsSelectedSubsetLock(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	full := &config.Schema{Tools: map[string]*config.Tool{
		"selected": {
			Name: "selected",
			Methods: []*config.MethodCandidate{{
				Kind:   "native",
				Config: map[string]any{"pkg": "selected"},
			}},
		},
		"omitted": {
			Name: "omitted",
			Methods: []*config.MethodCandidate{{
				Kind:   "http",
				Config: map[string]any{"url": "https://example.test/releases/{latest}/omitted.tar.gz"},
			}},
		},
	}}
	selected := &config.Schema{Tools: filterTools(full.Tools, "selected", "", "")}
	lk, err := lock.ResolveAll(context.Background(), selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(lock.DefaultPath(schemaPath), lk); err != nil {
		t.Fatal(err)
	}

	got, err := resolveInstallLock(context.Background(), installPlan{schema: schemaPath, frozen: true}, selected, log.Default)
	if err != nil {
		t.Fatalf("resolveInstallLock() rejected selected-scope lock: %v", err)
	}
	if got == nil {
		t.Fatal("resolveInstallLock() returned nil lock")
	}
	if _, ok := got.MethodsHash["omitted"]; ok {
		t.Fatal("test fixture unexpectedly locked omitted tool")
	}
}
