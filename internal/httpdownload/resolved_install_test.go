package httpdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/downloadcache"
	depexec "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// TestHTTPAdapterInstallResolvedUsesConcreteURL proves InstallResolved never
// resolves again: the method candidate carries no url/repo/asset at all (any
// resolution attempt would fail), while the resolved plan carries the concrete
// httptest URL. A successful install can only have come from the plan.
func TestHTTPAdapterInstallResolvedUsesConcreteURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\necho hello\n"))
	}))
	defer server.Close()

	extractTo := t.TempDir()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"extract_to":    extractTo,
		"binary":        "demo",
		"sudo_required": false,
	}}
	resolved := plan.New("demo", "http", true)
	resolved.Artifacts = []plan.Artifact{{URL: server.URL + "/demo"}}

	if err := NewHTTPAdapter().InstallResolved(context.Background(), run.OSExecRunner{}, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	// #nosec G304 -- extractTo is a test-owned temporary directory.
	data, err := os.ReadFile(filepath.Join(extractTo, "demo"))
	if err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("installed binary is empty")
	}
}

func TestHTTPAdapterAuthenticatedInstallBypassesURLCache(t *testing.T) {
	const credential = "private-artifact-token"
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer "+credential {
			t.Errorf("Authorization = %q, want Bearer credential", got)
		}
		_, _ = w.Write([]byte("authenticated payload\n"))
	}))
	defer server.Close()

	url := server.URL + "/demo"
	stale := filepath.Join(t.TempDir(), "stale")
	if err := os.WriteFile(stale, []byte("stale cached payload\n"), 0o600); err != nil {
		t.Fatalf("write stale cache seed: %v", err)
	}
	if _, err := downloadcache.Store(url, stale); err != nil {
		t.Fatalf("prime download cache: %v", err)
	}

	extractTo := t.TempDir()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{
		Kind:      "http",
		SecretRef: &config.SecretReference{Provider: "env", Name: "ARTIFACT_TOKEN"},
		Config: map[string]any{
			"extract_to":    extractTo,
			"binary":        "demo",
			"sudo_required": false,
		},
	}
	resolved := plan.New("demo", "http", true)
	resolved.Artifacts = []plan.Artifact{{URL: url}}
	ctx := depexec.WithHTTPArtifactBearer(context.Background(), credential)

	if err := NewHTTPAdapter().InstallResolved(ctx, run.OSExecRunner{}, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("authenticated requests = %d, want 1 despite primed URL cache", requests)
	}
	// #nosec G304 -- extractTo is a test-owned temporary directory.
	data, err := os.ReadFile(filepath.Join(extractTo, "demo"))
	if err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if string(data) != "authenticated payload\n" {
		t.Fatalf("installed payload = %q, want authenticated response", data)
	}
}

func TestHTTPAdapterInstallResolvedUsesResolvedVerificationMetadata(t *testing.T) {
	payload := []byte("resolved payload\n")
	digest := sha256.Sum256(payload)
	wantChecksum := "sha256:" + hex.EncodeToString(digest[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/demo":
			_, _ = w.Write(payload)
		case "/demo.sha256":
			_, _ = w.Write([]byte(hex.EncodeToString(digest[:])))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	extractTo := t.TempDir()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url":                  "https://stale.invalid/demo",
		"checksum":             "sha256:auto",
		"checksum_url":         "https://stale.invalid/demo.sha256",
		"checksum_file_format": "bsd",
		"signature_url":        "https://stale.invalid/demo.sig",
		"signing_key":          "stale-key",
		"extract_to":           extractTo,
		"binary":               "demo",
		"sudo_required":        false,
	}}
	resolved := plan.New("demo", "http", true)
	resolved.Artifacts = []plan.Artifact{{
		URL:                server.URL + "/demo",
		Checksum:           "sha256:auto",
		ChecksumURL:        server.URL + "/demo.sha256",
		ChecksumFileFormat: "raw",
	}}

	if err := NewHTTPAdapter().InstallResolved(context.Background(), run.OSExecRunner{}, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if got, _ := mc.Config["_checksum_resolved"].(string); got != wantChecksum {
		t.Fatalf("_checksum_resolved = %q, want %q", got, wantChecksum)
	}
	// #nosec G304 -- extractTo is a test-owned temporary directory.
	data, err := os.ReadFile(filepath.Join(extractTo, "demo"))
	if err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("installed payload = %q, want %q", data, payload)
	}
}

// TestHTTPAdapterInstallResolvedRequiresConcreteURL is the fail-closed half:
// without a concrete artifact URL there is nothing executable to run.
func TestHTTPAdapterInstallResolvedRequiresConcreteURL(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"extract_to": t.TempDir(),
		"binary":     "demo",
	}}
	resolved := plan.New("demo", "http", true)
	if err := NewHTTPAdapter().InstallResolved(context.Background(), run.OSExecRunner{}, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved() with no artifact URL succeeded, want error")
	}
}
