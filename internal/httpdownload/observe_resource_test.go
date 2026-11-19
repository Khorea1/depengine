package httpdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestHTTPObservePhysicalFileSlotEvidence(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "real", "bin")
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(installDir, "rust")
	bytes := []byte("verified raw payload")
	if err := os.WriteFile(binary, bytes, 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	checksum := sha256.Sum256(bytes)
	tool := &config.Tool{Name: "rust"}
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url": "https://example.invalid/rust", "binary": "rust", "extract_to": installDir,
		"checksum": "sha256:" + hex.EncodeToString(checksum[:]),
	}}
	observe := func() plan.Observation {
		t.Helper()
		ob, err := NewHTTPAdapter().Observe(context.Background(), &run.FakeRunner{}, tool, method)
		if err != nil || ob.Presence != plan.PresencePresent {
			t.Fatalf("Observe() = %+v, %v; want present", ob, err)
		}
		return ob
	}

	got := observe()
	want := plan.ObservedResource{Scope: "host-filesystem", Kind: "file-slot", Identity: binary}
	if len(got.Resources) != 1 || got.Resources[0] != want {
		t.Fatalf("resources = %+v, want [%+v]", got.Resources, want)
	}

	// Two schemas may address the same physical slot through a directory
	// symlink; the resulting resource identity must agree.
	if runtime.GOOS != "windows" {
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(filepath.Join(root, "real"), alias); err != nil {
			t.Fatal(err)
		}
		method.Config["extract_to"] = filepath.Join(alias, "bin")
		got = observe()
		if len(got.Resources) != 1 || got.Resources[0] != want {
			t.Fatalf("alias resources = %+v, want [%+v]", got.Resources, want)
		}
		method.Config["extract_to"] = installDir
	}

	// An archive digest does not establish ownership of the extracted file,
	// even when the extracted file happens to exist at a known location.
	method.Config["url"] = "https://example.invalid/rust.tar.gz"
	got = observe()
	if len(got.Resources) != 0 {
		t.Fatalf("archive emitted raw-file resource evidence: %+v", got.Resources)
	}
	method.Config["url"] = "https://example.invalid/setup.msi"
	got = observe()
	if len(got.Resources) != 0 {
		t.Fatalf("installer emitted raw-file resource evidence: %+v", got.Resources)
	}
	method.Config["url"] = "https://example.invalid/rust"

	// An existing symlink is not the same mutation resource as its target;
	// we cannot safely project either as an exclusive file slot.
	if runtime.GOOS != "windows" {
		if err := os.Remove(binary); err != nil {
			t.Fatal(err)
		}
		realFile := filepath.Join(root, "target")
		if err := os.WriteFile(realFile, bytes, 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
			t.Fatal(err)
		}
		if err := os.Symlink(realFile, binary); err != nil {
			t.Fatal(err)
		}
		got = observe()
		if len(got.Resources) != 0 {
			t.Fatalf("symlink emitted raw-file resource evidence: %+v", got.Resources)
		}
	}
}

func TestHTTPObservePhysicalResourceRequiresExplicitEvidence(t *testing.T) {
	tool := &config.Tool{Name: "rust"}
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url": "https://example.invalid/rust", "binary": "rust",
	}}
	got, err := NewHTTPAdapter().Observe(context.Background(), &run.FakeRunner{LookPaths: map[string]bool{"rust": true}}, tool, method)
	if err != nil || got.Presence != plan.PresencePresent || len(got.Resources) != 0 {
		t.Fatalf("PATH-only evidence = %+v, %v; want present without physical identity", got, err)
	}

	root := t.TempDir()
	method.Config["extract_to"] = root
	got, err = NewHTTPAdapter().Observe(context.Background(), &run.FakeRunner{}, tool, method)
	if err != nil || got.Presence != plan.PresenceAbsent || len(got.Resources) != 0 {
		t.Fatalf("absent evidence = %+v, %v; want no physical identity", got, err)
	}

	if err := os.WriteFile(filepath.Join(root, "rust"), []byte("old"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	method.Config["checksum"] = "sha256:" + hex.EncodeToString(make([]byte, sha256.Size))
	got, err = NewHTTPAdapter().Observe(context.Background(), &run.FakeRunner{}, tool, method)
	if err != nil || got.Presence != plan.PresenceBroken || len(got.Resources) != 0 {
		t.Fatalf("digest mismatch evidence = %+v, %v; want broken without physical identity", got, err)
	}
}
