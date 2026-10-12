package httpdownload_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/run"
)

func TestPortableWindowsExecutableInstallsObservesAndRemovesAsRawPayload(t *testing.T) {
	payload := []byte("MZ portable payload; must not execute")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(payload) }))
	defer server.Close()
	root := t.TempDir()
	tool := &config.Tool{Name: "cli"}
	method := &config.MethodCandidate{
		Kind: "http",
		When: &config.Condition{OS: []string{"windows"}},
		Config: map[string]any{
			"url":                 server.URL + "/cli-windows-amd64.exe",
			"portable_executable": true,
			"binary":              "cli.exe",
			"extract_to":          root,
			"sudo_required":       false,
		},
	}
	adapter := httpdownload.NewHTTPAdapter()
	runner := &run.OSExecRunner{}
	if err := adapter.Install(context.Background(), runner, tool, method); err != nil {
		t.Fatalf("Install() = %v", err)
	}
	path := filepath.Join(root, "cli.exe")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("installed payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("installed payload = %q, want exact downloaded bytes %q", got, payload)
	}
	if !adapter.Check(context.Background(), runner, tool, method) {
		t.Fatal("Check() = false after raw payload install")
	}
	observation, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil || observation.Presence != "present" {
		t.Fatalf("Observe() = %+v, %v; want present", observation, err)
	}
	if err := adapter.Remove(context.Background(), runner, tool, method); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("payload still exists after Remove(): stat error = %v", err)
	}
}

func TestPortableExecutableOptInDoesNotPermitExecutionOrInstallerPayloads(t *testing.T) {
	for _, tc := range []struct {
		name string
		when *config.Condition
		url  string
	}{
		{name: "wrong target OS", when: &config.Condition{OS: []string{"linux"}}, url: "https://example.test/cli.exe"},
		{name: "installer format remains forbidden", when: &config.Condition{OS: []string{"windows"}}, url: "https://example.test/setup.msi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				_, _ = w.Write([]byte("MZ payload"))
			}))
			defer server.Close()
			method := &config.MethodCandidate{
				Kind: "http",
				When: tc.when,
				Config: map[string]any{
					"url":                 strings.Replace(tc.url, "https://example.test", server.URL, 1),
					"portable_executable": true,
					"binary":              "cli.exe",
					"extract_to":          t.TempDir(),
				},
			}
			err := httpdownload.NewHTTPAdapter().Install(context.Background(), &run.OSExecRunner{}, &config.Tool{Name: "cli"}, method)
			if err == nil {
				t.Fatal("Install() unexpectedly accepted invalid portable executable configuration")
			}
			if requests != 0 {
				t.Fatalf("invalid artifact reached HTTP server %d times", requests)
			}
		})
	}
}

func TestInstallerExeWithoutOptInRemainsRejected(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte("MZ installer"))
	}))
	defer server.Close()
	method := &config.MethodCandidate{
		Kind: "http",
		When: &config.Condition{OS: []string{"windows"}},
		Config: map[string]any{
			"url":        server.URL + "/setup.exe",
			"binary":     "setup.exe",
			"extract_to": t.TempDir(),
		},
	}
	err := httpdownload.NewHTTPAdapter().Install(context.Background(), &run.OSExecRunner{}, &config.Tool{Name: "setup"}, method)
	if err == nil || !strings.Contains(err.Error(), "platform installer") {
		t.Fatalf("Install() error = %v, want fail-closed installer rejection", err)
	}
	if requests != 0 {
		t.Fatalf("installer payload reached HTTP server %d times", requests)
	}
}
