package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
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
	data, err := os.ReadFile(filepath.Join(extractTo, "demo"))
	if err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("installed binary is empty")
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
