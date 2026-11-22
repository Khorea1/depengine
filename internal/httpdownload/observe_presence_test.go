package httpdownload

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestHTTPObserveDestinationEvidence(t *testing.T) {
	tool := &config.Tool{Name: "rust"}
	adapter := NewHTTPAdapter()
	runner := &run.FakeRunner{}
	root := t.TempDir()
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url": "https://example.invalid/rust", "binary": "rust", "extract_to": root,
	}}
	observe := func(want plan.PresenceState) {
		t.Helper()
		got, err := adapter.Observe(context.Background(), runner, tool, method)
		if err != nil || got.Presence != want {
			t.Fatalf("Observe() = %+v, err %v; want %s", got, err, want)
		}
		if want != plan.PresencePresent && adapter.Check(context.Background(), runner, tool, method) {
			t.Fatalf("Check() reported installed when Observe() = %s", want)
		}
	}
	observe(plan.PresenceAbsent)

	// Missing nested directories beneath an existing directory are still
	// genuine absence, not an uninspectable installation.
	method.Config["extract_to"] = filepath.Join(root, "not-created", "bin")
	observe(plan.PresenceAbsent)
	method.Config["extract_to"] = root

	if err := os.WriteFile(filepath.Join(root, "rust"), []byte("valid"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	observe(plan.PresencePresent)

	// An existing directory at the binary destination is occupied, not absent.
	// Misclassifying it could authorize a new installation over another resource.
	if err := os.Remove(filepath.Join(root, "rust")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "rust"), 0o700); err != nil {
		t.Fatal(err)
	}
	observe(plan.PresenceBroken)
	if err := os.Remove(filepath.Join(root, "rust")); err != nil {
		t.Fatal(err)
	}

	// The target path is not inspectable: an existing non-directory parent is
	// not evidence that the target is absent. Treat it as inconclusive.
	parent := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(parent, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	method.Config["extract_to"] = parent
	observe(plan.PresenceUnknown)

	// A broken symlink is physically present at the target path. Treating it
	// as absent could let the installer mutate an unrelated resource.
	if runtime.GOOS != "windows" {
		brokenParent := filepath.Join(root, "broken-parent-link")
		if err := os.Symlink(filepath.Join(root, "missing-parent"), brokenParent); err != nil {
			t.Fatal(err)
		}
		method.Config["extract_to"] = brokenParent
		observe(plan.PresenceUnknown)

		// The configured root can itself be beneath a dangling symlink.
		// A missing suffix must not mask the occupied ancestor.
		method.Config["extract_to"] = filepath.Join(brokenParent, "nested", "bin")
		observe(plan.PresenceUnknown)

		method.Config["extract_to"] = root
		if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "rust")); err != nil {
			t.Fatal(err)
		}
		observe(plan.PresenceBroken)
	}
}
