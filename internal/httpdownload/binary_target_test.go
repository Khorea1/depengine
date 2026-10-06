package httpdownload

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestCopyBinaryRejectsTraversalName(t *testing.T) {
	root := t.TempDir()
	destDir := filepath.Join(root, "bin")
	if err := os.Mkdir(destDir, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "source")
	if err := os.WriteFile(src, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := copyBinary(context.Background(), src, destDir, "../outside", &run.FakeRunner{}, false, "tool"); err == nil {
		t.Fatal("copyBinary accepted traversal binary name")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("copyBinary wrote outside destination: %v", err)
	}
}

func TestHTTPAdapterCheckRejectsUnsafeBinary(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{"extract_to": filepath.Join(root, "bin"), "binary": "../outside"}}
	if NewHTTPAdapter().Check(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc) {
		t.Fatal("Check reported an unsafe binary target present")
	}
}

func TestHTTPAdapterRemoveRejectsLegacyArchiveWithoutOwnershipState(t *testing.T) {
	parent := t.TempDir()
	payload := filepath.Join(parent, "payload")
	if err := os.Mkdir(payload, 0700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(payload, "tool")
	unrelated := filepath.Join(payload, "unrelated.txt")
	if err := os.WriteFile(owned, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.test/tool.zip", "extract_to": payload}}
	err := NewHTTPAdapter().Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc)
	if err == nil || !strings.Contains(err.Error(), "ownership state is missing") {
		t.Fatalf("Remove error = %v, want missing ownership state", err)
	}
	for _, path := range []string{owned, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("legacy removal touched %s: %v", path, err)
		}
	}
}

func TestHTTPAdapterRemoveRecordedArchivePayloadPreservesParent(t *testing.T) {
	parent := t.TempDir()
	payload := filepath.Join(parent, "tool-payload")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "bin", "tool"), []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(parent, "other-tool")
	if err := os.WriteFile(unrelated, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.test/tool.zip", "extract_to": payload, ownedArchivePayloadKey: true,
	}}
	tool := &config.Tool{Name: "tool"}
	owner, err := expectedArchiveOwnership(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveOwnership(payload, owner); err != nil {
		t.Fatal(err)
	}
	if err := NewHTTPAdapter().Remove(context.Background(), &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(payload); !os.IsNotExist(err) {
		t.Fatalf("owned payload remains: %v", err)
	}
	if got, err := os.ReadFile(unrelated); err != nil || string(got) != "unrelated" { // #nosec G304 -- unrelated fixture is in the test-owned parent directory.
		t.Fatalf("unrelated parent payload changed: %q, %v", got, err)
	}
}

func TestHTTPAdapterRemoveRejectsArchiveWithMismatchedFilesystemOwnership(t *testing.T) {
	parent := t.TempDir()
	payload := filepath.Join(parent, "tool-payload")
	if err := os.MkdirAll(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	foreignTool := &config.Tool{Name: "other-tool"}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.test/tool.zip", "extract_to": payload, ownedArchivePayloadKey: true,
	}}
	owner, err := expectedArchiveOwnership(foreignTool, mc)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveOwnership(payload, owner); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(payload, "keep")
	if err := os.WriteFile(sentinel, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = NewHTTPAdapter().Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc)
	if err == nil || !strings.Contains(err.Error(), "filesystem ownership") {
		t.Fatalf("Remove error = %v, want filesystem ownership rejection", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "foreign" { // #nosec G304 -- sentinel is inside this test's temporary directory.
		t.Fatalf("mismatched ownership removal changed payload: %q, %v", got, readErr)
	}
}

func TestHTTPAdapterRemoveRejectsUnsafeBinary(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.test/tool", "extract_to": filepath.Join(root, "bin"), "binary": "../outside",
	}}
	err := NewHTTPAdapter().Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc)
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("Remove error = %v, want unsafe binary rejection", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "unrelated" { // #nosec G304 -- outside fixture is in the test-owned t.TempDir.
		t.Fatalf("unsafe remove changed outside path: %q, %v", got, err)
	}
}

func TestHTTPAdapterInstallRecordsArchiveOwnership(t *testing.T) {
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	file, err := zipWriter.Create("tool")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive.Bytes())
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "owned-payload")
	mc := &config.MethodCandidate{Config: map[string]any{
		"url": server.URL + "/tool.zip", "extract_to": dest, "sudo_required": false,
	}}
	resolved := &plan.ResolvedInstallPlan{Artifacts: []plan.Artifact{{URL: server.URL + "/tool.zip"}}}
	if err := NewHTTPAdapter().InstallResolved(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "tool"}, mc, resolved); err != nil {
		t.Fatalf("InstallResolved: %v", err)
	}
	if !ownsArchivePayload(mc) {
		t.Fatal("successful archive installation did not persist payload ownership")
	}
	owner, err := expectedArchiveOwnership(&config.Tool{Name: "tool"}, mc)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyArchiveOwnership(dest, owner); err != nil {
		t.Fatalf("successful archive installation did not persist filesystem ownership: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "tool")); err != nil || string(got) != "payload" { // #nosec G304 -- dest is a test-owned t.TempDir path.
		t.Fatalf("installed payload = %q, %v", got, err)
	}
}
