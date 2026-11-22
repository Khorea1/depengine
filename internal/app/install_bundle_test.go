package app

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/bundle"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/plan"
)

func bundleTestDocument(url, checksum string) plan.LockDocument {
	return plan.LockDocument{Version: plan.CurrentLockVersion, Entries: []plan.LockProjection{{
		Tool:      plan.ToolIdentity{Name: "demo"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Identity:  plan.LockIdentity{Artifacts: []plan.LockedArtifact{{URL: url, Checksum: checksum}}},
	}}}
}

func evalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return resolved
}

func TestValidateInstallBundleMapsLockedArtifactPayload(t *testing.T) {
	payload := []byte("locked artifact")
	digest := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	url := server.URL + "/demo"
	document := bundleTestDocument(url, checksum)

	root := filepath.Join(evalTempDir(t), "bundle")
	if err := bundle.Export(root, document); err != nil {
		t.Fatal(err)
	}
	payloads, err := validateInstallBundle(root, document)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "payloads", hex.EncodeToString(digest[:]))
	if got := payloads[httpdownload.BundleArtifactKey("demo", "http", url, checksum)]; got != want {
		t.Fatalf("payload path = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("payload not readable: %v", err)
	}
}

func TestValidateInstallBundleRejectsMismatchedLock(t *testing.T) {
	payload := []byte("locked artifact")
	digest := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	exported := bundleTestDocument(server.URL+"/demo", checksum)

	root := filepath.Join(t.TempDir(), "bundle")
	if err := bundle.Export(root, exported); err != nil {
		t.Fatal(err)
	}
	if _, err := validateInstallBundle(root, bundleTestDocument(server.URL+"/other", checksum)); err == nil {
		t.Fatal("validateInstallBundle accepted a bundle that does not match the locked artifacts")
	}
}
