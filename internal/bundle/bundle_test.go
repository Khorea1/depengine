package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

// evalTempDir resolves the platform temp root so bundle path validation does
// not reject a symlinked ancestor such as macOS /var -> /private/var.
func evalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return resolved
}

func TestExportAndOpenVerifiesLockedPayload(t *testing.T) {
	payload := []byte("locked artifact")
	digest := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	doc := plan.LockDocument{Version: plan.CurrentLockVersion, Entries: []plan.LockProjection{
		{
			Tool: plan.ToolIdentity{Name: "demo"}, Candidate: plan.CandidateIdentity{Method: "http"},
			Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{{URL: server.URL + "/demo", Checksum: checksum}}},
		},
		{
			Tool: plan.ToolIdentity{Name: "other"}, Candidate: plan.CandidateIdentity{Method: "http"},
			Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{{URL: server.URL + "/demo", Checksum: checksum}}},
		},
	}}
	root := filepath.Join(evalTempDir(t), "bundle")
	if err := Export(root, doc); err != nil {
		t.Fatal(err)
	}
	entries, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Checksum != checksum || entries[1].Checksum != checksum {
		t.Fatalf("entries = %#v", entries)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(entries[0].Payload)), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("Open accepted corrupt payload")
	}
}

func TestArtifactEntriesRejectsUnverifiableOrSignedArtifact(t *testing.T) {
	for _, artifact := range []plan.LockedArtifact{
		{URL: "https://example.test/tool"},
		{URL: "https://example.test/tool", Checksum: "sha256:" + string(make([]byte, 64)), SignatureURL: "https://example.test/tool.sig"},
	} {
		doc := plan.LockDocument{Entries: []plan.LockProjection{{Tool: plan.ToolIdentity{Name: "demo"}, Candidate: plan.CandidateIdentity{Method: "http"}, Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{artifact}}}}}
		if _, err := ArtifactEntries(doc); err == nil {
			t.Fatalf("ArtifactEntries accepted unsupported artifact %+v", artifact)
		}
	}
}

func TestOpenRejectsUnreferencedPayload(t *testing.T) {
	payload := []byte("payload")
	digest := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(digest[:])
	doc := plan.LockDocument{Entries: []plan.LockProjection{{
		Tool:      plan.ToolIdentity{Name: "demo"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Identity:  plan.LockIdentity{Artifacts: []plan.LockedArtifact{{URL: "https://example.test/demo", Checksum: checksum}}},
	}}}
	root := filepath.Join(t.TempDir(), "bundle")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "payloads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "payloads", strings.TrimPrefix(checksum, "sha256:")), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "payloads", "unreferenced"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ArtifactEntries(doc)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest{Version: formatVersion, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("Open accepted unreferenced payload")
	}
}

func TestExportRejectsOversizedPayloadAndRemovesOutput(t *testing.T) {
	t.Setenv("DEPENGINE_DOWNLOAD_MAX_BYTES", "8")
	payload := []byte("larger-than-limit")
	digest := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	doc := plan.LockDocument{Entries: []plan.LockProjection{{
		Tool:      plan.ToolIdentity{Name: "demo"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Identity:  plan.LockIdentity{Artifacts: []plan.LockedArtifact{{URL: server.URL, Checksum: checksum}}},
	}}}
	root := filepath.Join(t.TempDir(), "bundle")
	if err := Export(root, doc); err == nil {
		t.Fatal("Export accepted oversized response")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("export destination still exists after failure: %v", err)
	}
}
