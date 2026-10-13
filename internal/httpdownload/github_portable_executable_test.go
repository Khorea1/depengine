package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/validate"
)

func TestGitHubPortableExecutableDistributionInstallsAsRawPayload(t *testing.T) {
	payload := []byte("MZ portable release payload; never execute")
	serverURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/demo/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"portable.exe","browser_download_url":"` + serverURL + `/download/portable.exe"}]}`))
		case "/download/portable.exe":
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	apiTarget, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousResolver := ghrelease.Default
	resolver := ghrelease.NewResolver()
	resolver.SetHTTPClient(&http.Client{Transport: githubAPIFixtureTransport{apiTarget: apiTarget}})
	ghrelease.Default = resolver
	t.Cleanup(func() { ghrelease.Default = previousResolver })

	root := t.TempDir()
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{
		Kind: "github",
		When: &config.Condition{Arch: []string{"x86_64"}},
		Config: map[string]any{
			"repo":                "owner/demo",
			"asset":               "demo-linux.tar.gz",
			"binary":              "demo",
			"extract_to":          root,
			"portable_executable": true,
			"sudo_required":       false,
		},
		Distributions: []config.Distribution{{
			When:      &config.Condition{OS: []string{"windows"}},
			Asset:     config.AssetSelector{Template: "portable.exe"},
			HasAsset:  true,
			Binary:    "portable.exe",
			HasBinary: true,
		}},
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{"demo": {Name: "demo", Methods: []*config.MethodCandidate{method}}}}
	if result := validate.ValidateSchema(schema, nil); result.HasErrors() {
		t.Fatalf("shared Windows .exe distribution failed validation: %v", result.Errors)
	}

	method, err = config.SpecializeDistribution(method, &platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"})
	if err != nil || method == nil {
		t.Fatalf("SpecializeDistribution() = %v, %v", method, err)
	}
	if method.When == nil || len(method.When.Arch) != 1 || method.When.Arch[0] != "x86_64" {
		t.Fatalf("method-level condition was not preserved: %+v", method.When)
	}
	if selected := method.SelectedDistributionWhen; selected == nil || len(selected.OS) != 1 || selected.OS[0] != "windows" {
		t.Fatalf("selected distribution condition = %+v, want Windows", selected)
	}

	runner := &run.FakeRunner{}
	adapter := NewGitHubAdapter()
	if err := adapter.Install(context.Background(), runner, tool, method); err != nil {
		t.Fatalf("GitHub Install() = %v", err)
	}

	installedPath := filepath.Join(root, "portable.exe")
	got, err := os.ReadFile(installedPath) // #nosec G304 -- installedPath is below this test's t.TempDir().
	if err != nil {
		entries, _ := os.ReadDir(root)
		t.Fatalf("installed release payload: %v; root entries: %+v", err, entries)
	}
	if string(got) != string(payload) {
		t.Fatalf("installed bytes = %q, want raw asset bytes %q", got, payload)
	}
	observation, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil || observation.Presence != "present" {
		t.Fatalf("Observe() = %+v, %v; want present", observation, err)
	}
	if err := adapter.Remove(context.Background(), runner, tool, method); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
		t.Fatalf("portable payload still exists after removal: %v", err)
	}
	for _, call := range runner.Calls {
		if call.Name != "gh" || len(call.Args) != 2 || call.Args[0] != "auth" || call.Args[1] != "token" {
			t.Fatalf("runner call = %+v, want only the GitHub token lookup", call)
		}
	}
}

func TestPortableExecutableAuthorizationStaysWindowsAndGitHubScoped(t *testing.T) {
	resolvedURL := "https://example.test/cli.exe"
	for _, tc := range []struct {
		name         string
		kind         string
		when         *config.Condition
		distribution *config.Condition
		wantAllowed  bool
	}{
		{name: "method-level Windows candidate", kind: "http", when: &config.Condition{OS: []string{"windows"}}, wantAllowed: true},
		{name: "selected Windows GitHub distribution", kind: "github", when: &config.Condition{Arch: []string{"x86_64"}}, distribution: &config.Condition{OS: []string{"windows"}}, wantAllowed: true},
		{name: "selected non-Windows GitHub distribution", kind: "github", when: &config.Condition{Arch: []string{"x86_64"}}, distribution: &config.Condition{OS: []string{"linux"}}},
		{name: "generic HTTP cannot use distribution metadata", kind: "http", when: &config.Condition{Arch: []string{"x86_64"}}, distribution: &config.Condition{OS: []string{"windows"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := &config.MethodCandidate{
				Kind:                     tc.kind,
				When:                     tc.when,
				SelectedDistributionWhen: tc.distribution,
				Config:                   map[string]any{"portable_executable": true, "binary": "cli.exe"},
			}
			got := portableExecutableArtifactAllowed(candidate, resolvedURL)
			if got != tc.wantAllowed {
				t.Fatalf("portableExecutableArtifactAllowed() = %t, want %t", got, tc.wantAllowed)
			}
		})
	}
}
