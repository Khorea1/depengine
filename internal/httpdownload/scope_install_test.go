// Package httpdownload implements the HTTP download method family.
//
// scope_install_test.go proves the ADR-004 end-to-end behavior: a user-scoped
// raw binary install lands under the XDG data home, gets a PATH link in the
// conventional ~/.local/bin, checks/observes as present, requires no
// elevation, and removes symmetrically — with no ~/.local path authored.
package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

// TestHTTPScopeUserRawBinaryInstallCheckRemoveRoundTrip drives the full
// install → check/observe → remove cycle for a user-scoped raw binary with no
// extract_to/link_dir in the candidate.
func TestHTTPScopeUserRawBinaryInstallCheckRemoveRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	ctx := context.Background()
	adapter := NewHTTPAdapter()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\necho scope-demo\n"))
	}))
	defer server.Close()

	home := t.TempDir()
	exectest.SetHome(t, home)

	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url":           server.URL + "/demo",
		"scope":         "user",
		"sudo_required": false,
	}}

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Scope != "user" {
		t.Fatalf("resolved scope = %q, want user", resolved.Identity.Scope)
	}

	installRoot := filepath.Join(home, ".local", "share", "depengine", "tools", "demo")
	linkDir := filepath.Join(home, ".local", "bin")

	if adapter.RequiresElevation(tool, mc) {
		t.Fatal("user-scope install under HOME must not require elevation")
	}

	if err := adapter.InstallResolved(ctx, run.OSExecRunner{}, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}

	// Payload binary lives in the scope install root.
	if _, err := os.Stat(filepath.Join(installRoot, "demo")); err != nil {
		t.Fatalf("payload binary: %v", err)
	}
	// PATH link points at the payload.
	link := filepath.Join(linkDir, "demo")
	actual, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("PATH link: %v", err)
	}
	if filepath.Clean(actual) != filepath.Clean(filepath.Join(installRoot, "demo")) {
		t.Fatalf("link target = %q, want %q", actual, filepath.Join(installRoot, "demo"))
	}

	// Check and Observe agree the tool is present.
	if !adapter.Check(ctx, run.OSExecRunner{}, tool, mc) {
		t.Fatal("Check should report present after install")
	}
	observation, err := adapter.Observe(ctx, run.OSExecRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe presence = %q, want present", observation.Presence)
	}

	// Remove clears the payload and the link.
	if err := adapter.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("stale PATH link after remove")
	}
	if _, err := os.Lstat(filepath.Join(installRoot, "demo")); err == nil {
		t.Fatal("stale payload after remove")
	}
	if adapter.Check(ctx, run.OSExecRunner{}, tool, mc) {
		t.Fatal("Check should report absent after remove")
	}
}

// TestAppImageScopeUserBinaryTargetUsesScopeInstallRoot proves the appimage
// default install_dir follows the scope placement instead of ~/.local/bin.
func TestAppImageScopeUserBinaryTargetUsesScopeInstallRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	home := t.TempDir()
	exectest.SetHome(t, home)

	tool := &config.Tool{Name: "obsidian"}
	mc := &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"scope": "user"}}
	installDir, name := binaryTarget(tool, mc)
	if name != "obsidian" {
		t.Fatalf("binary name = %q, want obsidian", name)
	}
	want := filepath.Join(home, ".local", "share", "depengine", "tools", "obsidian")
	if filepath.Clean(installDir) != filepath.Clean(want) {
		t.Fatalf("install_dir = %q, want %q", installDir, want)
	}
}

func TestHTTPScopeRawRemoveRefusesReplacedLauncher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink assertion")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	exectest.SetHome(t, home)
	payload := filepath.Join(home, "tools", "demo")
	links := filepath.Join(home, "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "demo"), []byte("owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "replacement")
	if err := os.WriteFile(foreign, []byte("replacement"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(links, "demo")
	if err := os.Symlink(foreign, launcher); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"scope":      "user",
		"extract_to": payload,
		"link_dir":   links,
	}}
	err := NewHTTPAdapter().Remove(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "demo"}, mc)
	if err == nil {
		t.Fatal("expected removal of a replaced launcher to be refused")
	}
	if _, err := os.Stat(filepath.Join(payload, "demo")); err != nil {
		t.Fatalf("payload was removed after launcher refusal: %v", err)
	}
	actual, err := os.Readlink(launcher)
	if err != nil || actual != foreign {
		t.Fatalf("replacement launcher changed: target=%q err=%v", actual, err)
	}
}

func TestHTTPScopeSystemRemoveElevatesPayloadDeletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission and symlink assertion is POSIX-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can remove the protected payload without elevation")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	lockedParent := filepath.Join(root, "system")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockedParent, 0o755); err != nil {
		t.Fatal(err)
	}
	exectest.SetHome(t, home)
	payload := filepath.Join(lockedParent, "demo")
	links := filepath.Join(home, "bin")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "demo"), []byte("owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(payload, "demo"), filepath.Join(links, "demo")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockedParent, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(lockedParent, 0o755)

	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"scope":      "system",
		"extract_to": payload,
		"link_dir":   links,
	}}
	if err := NewHTTPAdapter().Remove(context.Background(), fr, &config.Tool{Name: "demo"}, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(fr.Calls) != 1 || fr.Calls[0].Name != "sudo" || len(fr.Calls[0].Args) < 2 || fr.Calls[0].Args[0] != "rm" || fr.Calls[0].Args[1] != "-rf" {
		t.Fatalf("elevated payload removal calls = %+v, want sudo rm -rf", fr.Calls)
	}
}
