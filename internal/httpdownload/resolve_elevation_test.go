package httpdownload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

// resolvePlanner is the read-only half of exec.AdapterV2 that these tests
// exercise: dry-run resolution must report the elevation decision without
// inventing an executable command.
type resolvePlanner interface {
	ResolvePlan(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error)
}

// useGitHubReleaseFixture points release resolution at a local API that serves
// one asset, so GitHub dry-run resolution needs no network.
func useGitHubReleaseFixture(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/demo/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.0.0",
				"assets":   []map[string]string{{"name": "demo.bin", "browser_download_url": server.URL + "/download/demo.bin"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse fixture URL: %v", err)
	}
	resolver := ghrelease.NewResolver()
	resolver.SetHTTPClient(&http.Client{Transport: githubAPIFixtureTransport{apiTarget: target}})
	previous := ghrelease.Default
	ghrelease.Default = resolver
	t.Cleanup(func() { ghrelease.Default = previous })
}

// TestHTTPFamilyResolvePlanAnnotatesInstallElevation covers the non-native
// adapters that share the HTTP download transport. Each of them must record
// the privilege decision on the install step while leaving Preview absent,
// because the concrete argv (temporary paths, extraction) only exists during
// execution.
func TestHTTPFamilyResolvePlanAnnotatesInstallElevation(t *testing.T) {
	exectest.SetHome(t, t.TempDir())
	useGitHubReleaseFixture(t)
	tool := &config.Tool{Name: "demo"}
	const directURL = "https://example.test/demo"
	githubConfig := func(required bool) map[string]any {
		return map[string]any{"repo": "example/demo", "asset": "demo.bin", "release": "latest", "sudo_required": required}
	}

	cases := []struct {
		name    string
		adapter resolvePlanner
		mc      *config.MethodCandidate
		want    plan.ElevationDecision
	}{
		{
			name:    "http elevated placement",
			adapter: NewHTTPAdapter(),
			mc:      &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": directURL, "sudo_required": true}},
			want:    plan.ElevationRequired,
		},
		{
			name:    "http user placement",
			adapter: NewHTTPAdapter(),
			mc:      &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": directURL, "sudo_required": false}},
			want:    plan.ElevationNone,
		},
		{
			name:    "github elevated placement",
			adapter: NewGitHubAdapter(),
			mc:      &config.MethodCandidate{Kind: "github", Config: githubConfig(true)},
			want:    plan.ElevationRequired,
		},
		{
			name:    "github user placement",
			adapter: NewGitHubAdapter(),
			mc:      &config.MethodCandidate{Kind: "github", Config: githubConfig(false)},
			want:    plan.ElevationNone,
		},
		{
			name:    "appimage privileged install dir",
			adapter: NewAppImageAdapter(),
			mc:      &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"url": directURL, "install_dir": "/usr/local/bin"}},
			want:    plan.ElevationRequired,
		},
		{
			name:    "appimage home install dir",
			adapter: NewAppImageAdapter(),
			mc:      &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"url": directURL, "install_dir": "~/.local/bin"}},
			want:    plan.ElevationNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intent, err := planner.BuildCandidateIntent(tool, tc.mc)
			if err != nil {
				t.Fatalf("BuildCandidateIntent() error = %v", err)
			}
			resolved, err := tc.adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, tc.mc, &intent)
			if err != nil {
				t.Fatalf("ResolvePlan() error = %v", err)
			}
			installs := 0
			for _, operation := range resolved.Operations {
				if operation.Kind != "install" {
					continue
				}
				installs++
				if operation.Elevation != tc.want {
					t.Errorf("install elevation = %q, want %q", operation.Elevation, tc.want)
				}
				if len(operation.Preview) != 0 {
					t.Errorf("install preview = %v, want absent until execution", operation.Preview)
				}
				if operation.Command != nil || operation.ArbitraryCode {
					t.Errorf("elevation annotation leaked executable intent: %+v", operation)
				}
			}
			if installs != 1 {
				t.Fatalf("install operations = %d, want 1 (%+v)", installs, resolved.Operations)
			}
			if err := plan.ValidateResolution(intent, *resolved); err != nil {
				t.Fatalf("ValidateResolution() error = %v", err)
			}
		})
	}
}
