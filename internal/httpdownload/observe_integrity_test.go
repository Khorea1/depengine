package httpdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestHTTPObservePinnedRawPayloadRejectsWrongInstalledBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rust")
	actual := []byte("installed by schema A")
	if err := os.WriteFile(path, actual, 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	pin := func(b []byte) string {
		hash := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(hash[:])
	}
	tool := &config.Tool{Name: "rust"}
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url": "https://example.invalid/rust", "binary": "rust", "extract_to": root,
		"checksum": pin([]byte("different bytes declared by schema B")),
	}}
	adapter := NewHTTPAdapter()
	runner := &run.FakeRunner{}

	observed, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Presence != plan.PresenceBroken {
		t.Fatalf("wrong bytes observation = %+v, want broken", observed)
	}
	if got := plan.Reconcile(plan.ResolvedIdentity{Package: "rust"}, observed).State; got != plan.StateBroken {
		t.Fatalf("reconciliation = %s, want broken", got)
	}

	method.Config["checksum"] = pin(actual)
	observed, err = adapter.Observe(context.Background(), runner, tool, method)
	if err != nil || observed.Presence != plan.PresencePresent {
		t.Fatalf("matching bytes observation = %+v, err = %v, want present", observed, err)
	}
}

// PATH presence cannot establish that an explicitly pinned raw artifact's
// bytes correspond to the requested download. Legacy unpinned presence-only
// checks remain supported.
func TestHTTPObservePinnedPATHRawPayloadIsUnknown(t *testing.T) {
	tool := &config.Tool{Name: "rust"}
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url": "https://example.invalid/rust", "binary": "rust", "checksum": "sha256:" + hex.EncodeToString(make([]byte, 32)),
	}}
	adapter := NewHTTPAdapter()
	runner := &run.FakeRunner{LookPaths: map[string]bool{"rust": true}}
	observation, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil || observation.Presence != plan.PresenceUnknown {
		t.Fatalf("pinned PATH presence = %+v, %v; want unknown", observation, err)
	}
	delete(method.Config, "checksum")
	observation, err = adapter.Observe(context.Background(), runner, tool, method)
	if err != nil || observation.Presence != plan.PresencePresent {
		t.Fatalf("unpinned PATH presence = %+v, %v; want present", observation, err)
	}
}

// An archive checksum authenticates the downloaded archive, not an extracted
// binary. Observation must not hash the extracted binary against that digest.
func TestHTTPObserveArchiveChecksumNotComparedToEntrypoint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rust"), []byte("extracted bytes"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	tool := &config.Tool{Name: "rust"}
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url": "https://example.invalid/rust.tar.gz", "binary": "rust", "extract_to": root,
		"checksum": "sha256:" + hex.EncodeToString(make([]byte, 32)),
	}}
	observation, err := NewHTTPAdapter().Observe(context.Background(), &run.FakeRunner{}, tool, method)
	if err != nil || observation.Presence != plan.PresencePresent {
		t.Fatalf("archive file presence = %+v, %v; want legacy presence", observation, err)
	}
}
