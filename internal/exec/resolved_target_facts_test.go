package exec_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	executor "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

type releaseAPIRedirector struct {
	endpoint *url.URL
}

func (r releaseAPIRedirector) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	target := *req.URL
	target.Scheme = r.endpoint.Scheme
	target.Host = r.endpoint.Host
	clone.URL = &target
	clone.Host = target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func TestResolveCandidatePlanSelectsGitHubAssetFromTransientHostFacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/depengine-test-target-facts/target-facts-fix-1/releases/latest" {
			t.Errorf("release API path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"demo-amd64-linux.tar.gz","browser_download_url":"https://github.com/depengine-test-target-facts/target-facts-fix-1/releases/download/v1.2.3/demo-amd64-linux.tar.gz"},{"name":"demo-arm64-linux.tar.gz","browser_download_url":"https://github.com/depengine-test-target-facts/target-facts-fix-1/releases/download/v1.2.3/demo-arm64-linux.tar.gz"},{"name":"demo-amd64-darwin.tar.gz","browser_download_url":"https://github.com/depengine-test-target-facts/target-facts-fix-1/releases/download/v1.2.3/demo-amd64-darwin.tar.gz"},{"name":"demo-arm64-darwin.tar.gz","browser_download_url":"https://github.com/depengine-test-target-facts/target-facts-fix-1/releases/download/v1.2.3/demo-arm64-darwin.tar.gz"}]}`))
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ghrelease.Default.SetHTTPClient(&http.Client{Transport: releaseAPIRedirector{endpoint: endpoint}, Timeout: 5 * time.Second})
	t.Cleanup(func() {
		ghrelease.Default.SetHTTPClient(&http.Client{Timeout: 30 * time.Second})
	})

	newExecutor := func() *executor.Executor {
		ex := executor.New()
		executor.WithAdapters(httpdownload.NewHTTPAdapter())(ex)
		executor.WithRunner(&run.FakeRunner{})(ex)
		executor.WithFacts(&platform.Facts{TargetArch: "x86_64", OS: "linux"})(ex)
		return ex
	}
	tool := &config.Tool{Name: "demo"}
	assetConfig := map[string]any{
		"repo":  "depengine-test-target-facts/target-facts-fix-1",
		"asset": "demo-{arch_any}-{os_any}.tar.gz",
	}

	stateCandidate := &config.MethodCandidate{Kind: "http", Config: assetConfig}
	resolved, err := newExecutor().ResolveCandidatePlan(context.Background(), tool, stateCandidate, "")
	if err != nil {
		t.Fatalf("ResolveCandidatePlan with missing target fields: %v", err)
	}
	if got, want := resolved.Artifacts[0].URL, "https://github.com/depengine-test-target-facts/target-facts-fix-1/releases/download/v1.2.3/demo-amd64-linux.tar.gz"; got != want {
		t.Fatalf("resolved asset URL = %q, want %q", got, want)
	}
	if stateCandidate.TargetArch != "" || stateCandidate.TargetOS != "" {
		t.Fatalf("resolution mutated caller candidate target fields: %q/%q", stateCandidate.TargetArch, stateCandidate.TargetOS)
	}

	explicitCandidate := &config.MethodCandidate{Kind: "http", TargetArch: "aarch64", TargetOS: "darwin", Config: assetConfig}
	resolved, err = newExecutor().ResolveCandidatePlan(context.Background(), tool, explicitCandidate, "")
	if err != nil {
		t.Fatalf("ResolveCandidatePlan with explicit target fields: %v", err)
	}
	if got, want := resolved.Artifacts[0].URL, "https://github.com/depengine-test-target-facts/target-facts-fix-1/releases/download/v1.2.3/demo-arm64-darwin.tar.gz"; got != want {
		t.Fatalf("resolved explicit-target asset URL = %q, want %q", got, want)
	}
	if explicitCandidate.TargetArch != "aarch64" || explicitCandidate.TargetOS != "darwin" {
		t.Fatalf("resolution changed explicit candidate target fields: %q/%q", explicitCandidate.TargetArch, explicitCandidate.TargetOS)
	}
}
