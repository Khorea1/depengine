package contracttest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	executor "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/msi"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

type releaseProbeTransport struct{}

func (releaseProbeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tag := "latest"
	if i := strings.LastIndex(req.URL.Path, "/releases/tags/"); i >= 0 {
		tag = strings.TrimPrefix(req.URL.Path[i+len("/releases/tags/"):], "/")
	} else if strings.HasSuffix(req.URL.Path, "/releases/latest") {
		tag = "v-latest"
	}
	body := fmt.Sprintf(`{"tag_name":%q,"assets":[{"name":"tool.tar.gz","browser_download_url":"https://downloads.example/%s/tool.tar.gz"}]}`, tag, tag)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       ioNopCloser{strings.NewReader(body)},
		Request:    req,
	}, nil
}

type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }

type runtimeRecordingRunner struct {
	calls      []run.FakeCall
	seen       map[string]int
	batchProbe bool
}

type runtimeGPGRunner struct {
	mu    sync.Mutex
	calls []run.FakeCall
	fpr   string
}

func (r *runtimeGPGRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: slices.Clone(args)})
	if name == "which" && len(args) > 0 && (args[0] == "curl" || args[0] == "wget") {
		return run.Result{ExitCode: 1}
	}
	result := run.Result{}
	if name == "gpg" && slices.Contains(args, "--show-key") {
		result.Stdout = []byte("fpr:::::::::" + r.fpr + ":\n")
	}
	if name == "gpg" && slices.Contains(args, "--status-fd=1") {
		result.Stdout = []byte("[GNUPG:] VALIDSIG " + r.fpr + " date time 0 1 8 00 00 " + r.fpr + "\n")
	}
	return result
}

func (r *runtimeRecordingRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: args})
	if r.batchProbe && name == "dpkg" && len(args) > 1 && args[0] == "-s" {
		if r.seen == nil {
			r.seen = make(map[string]int)
		}
		r.seen[args[1]]++
		if r.seen[args[1]] == 1 {
			return run.Result{ExitCode: 1}
		}
	}
	return run.Result{}
}

func TestRuntimeReleaseAndBranchResolution(t *testing.T) {
	saved := ghrelease.Default
	resolver := ghrelease.NewResolver()
	resolver.SetHTTPClient(&http.Client{Transport: releaseProbeTransport{}})
	ghrelease.Default = resolver
	t.Cleanup(func() { ghrelease.Default = saved })

	adapters := []interface {
		ResolvePlan(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error)
	}{
		httpdownload.NewHTTPAdapter(), httpdownload.NewGitHubAdapter(),
		httpdownload.NewAppImageAdapter(), httpdownload.NewAndroidAdapter(), msi.NewAdapter(),
	}
	for _, adapter := range adapters {
		kind := adapter.(interface{ Kind() string }).Kind()
		for _, selector := range []string{"release", "branch"} {
			for _, value := range []string{"edge-a", "edge-b"} {
				cfg := map[string]any{"repo": "owner/repo", "asset": "tool.tar.gz", selector: value}
				method := &config.MethodCandidate{Kind: kind, Config: cfg}
				resolved, err := adapter.ResolvePlan(context.Background(), run.BlockedRunner{}, &config.Tool{Name: "tool"}, method, &plan.ResolvedInstallPlan{})
				if err != nil {
					t.Fatalf("%s.%s=%s: ResolvePlan() error: %v", kind, selector, value, err)
				}
				wantURL := "https://downloads.example/" + value + "/tool.tar.gz"
				if len(resolved.Artifacts) != 1 || resolved.Artifacts[0].URL != wantURL || resolved.Identity.Version != value {
					t.Errorf("%s.%s=%s resolved artifact/version = %#v / %q, want %q / %q", kind, selector, value, resolved.Artifacts, resolved.Identity.Version, wantURL, value)
				}
			}
		}
	}
}

func TestRuntimeArtifactAndGitEffectFieldsHaveEvidence(t *testing.T) {
	owned := map[string]bool{"http": true, "github": true, "appimage": true, "android": true, "msi": true, "git": true}
	for _, contract := range methodkind.Contracts {
		if !owned[contract.Kind] {
			continue
		}
		for field, semantics := range contract.Fields {
			key := contract.Kind + "." + field
			if semantics.Effects&methodkind.EffectExecute != 0 {
				if _, ok := CoverageFor(PhaseExecute, key); !ok {
					t.Errorf("%s has EffectExecute but no runtime evidence", key)
				}
			}
			if semantics.Effects&methodkind.EffectVerify != 0 {
				if _, ok := CoverageFor(PhaseVerify, key); !ok {
					t.Errorf("%s has EffectVerify but no runtime evidence", key)
				}
			}
		}
	}
}

func TestRuntimeHTTPIntegrityAndSigningFieldsAreConsumed(t *testing.T) {
	payload := []byte("runtime integrity probe\n")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	var mu sync.Mutex
	requests := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/artifact":
			_, _ = w.Write(payload)
		case "/checksum":
			_, _ = fmt.Fprintln(w, checksum)
		case "/signature":
			_, _ = w.Write([]byte("fake signature"))
		case "/key":
			_, _ = w.Write([]byte("fake public key"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	const fingerprint = "0123456789ABCDEF0123456789ABCDEF01234567"
	runner := &runtimeGPGRunner{fpr: fingerprint}
	destination := t.TempDir()
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url":                  server.URL + "/artifact",
		"checksum":             "sha256:auto",
		"checksum_url":         server.URL + "/checksum",
		"checksum_file_format": "raw",
		"signature_url":        server.URL + "/signature",
		"signing_key":          server.URL + "/key",
		"extract_to":           destination,
		"binary":               "probe",
		"sudo_required":        false,
	}}
	if err := httpdownload.NewHTTPAdapter().Install(context.Background(), runner, &config.Tool{Name: "probe"}, method); err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	// #nosec G304 -- destination is an isolated t.TempDir owned by this test.
	installed, err := os.ReadFile(filepath.Join(destination, "probe"))
	if err != nil || string(installed) != string(payload) {
		t.Fatalf("installed payload = %q, err=%v", installed, err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/artifact", "/checksum", "/signature", "/key"} {
		if !slices.Contains(requests, path) {
			t.Errorf("runtime never requested %s; requests were %v", path, requests)
		}
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	showKeySawExpectedKey := false
	verifyRan := false
	for _, call := range runner.calls {
		if call.Name == "gpg" && slices.Contains(call.Args, "--show-key") {
			showKeySawExpectedKey = true
		}
		if call.Name == "gpg" && slices.Contains(call.Args, "--status-fd=1") {
			verifyRan = true
		}
	}
	if !showKeySawExpectedKey || !verifyRan {
		t.Fatalf("signing key/signature verification calls missing: %#v", runner.calls)
	}
}

func TestNativePackageOverrideAcrossRuntimeBoundaries(t *testing.T) {
	ctx := context.Background()
	tool := &config.Tool{Name: "fd", Methods: []*config.MethodCandidate{{
		Kind: "native", Config: map[string]any{"pkg": "fd", "pkg_overrides": map[string]any{"apt": "fd-find"}},
	}}}
	method := tool.Methods[0]
	adapter := executor.NewNativeAdapter("debian")
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &runtimeRecordingRunner{}, tool, method, &intent)
	if err != nil {
		t.Fatal(err)
	}
	// NativeAdapter preserves planner intent; Executor applies the clan
	// override before calling this boundary. Model that host-projected plan.
	resolved.Identity.Package = "fd-find"

	runner := &runtimeRecordingRunner{}
	if err := adapter.InstallResolved(ctx, runner, tool, method, resolved); err != nil {
		t.Fatalf("InstallResolved() error: %v", err)
	}
	if !callsContainArg(runner.calls, "fd-find") {
		t.Fatalf("serial install/verification omitted package override: %#v", runner.calls)
	}

	runner.calls = nil
	if _, err := adapter.Observe(ctx, runner, tool, method); err != nil {
		t.Fatalf("Observe() error: %v", err)
	}
	if len(runner.calls) != 1 || !callsContainArg(runner.calls, "fd-find") {
		t.Fatalf("Observe() calls = %#v, want package override", runner.calls)
	}
	runner.calls = nil
	if err := adapter.Remove(ctx, runner, tool, method); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if len(runner.calls) != 1 || !callsContainArg(runner.calls, "fd-find") {
		t.Fatalf("Remove() calls = %#v, want package override", runner.calls)
	}

	batchRunner := &runtimeRecordingRunner{batchProbe: true}
	batch := executor.New()
	executor.WithRunner(batchRunner)(batch)
	executor.WithAdapters(adapter)(batch)
	schema := &config.Schema{
		Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"native"}},
		Tools: map[string]*config.Tool{
			"fd":  tool,
			"bat": {Name: "bat", Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "bat"}}}},
		},
	}
	if _, err := batch.Execute(ctx, schema, "debian"); err != nil {
		t.Fatalf("batch Execute() error: %v", err)
	}
	if !batchCallsContain(batchRunner.calls, "apt-get", "install", "fd-find") {
		t.Fatalf("batch calls = %#v, want apt batch containing fd-find", batchRunner.calls)
	}
}

func callsContainArg(calls []run.FakeCall, want string) bool {
	for _, call := range calls {
		for _, arg := range call.Args {
			if arg == want {
				return true
			}
		}
	}
	return false
}

func batchCallsContain(calls []run.FakeCall, command, operation, packageName string) bool {
	for _, call := range calls {
		if call.Name != "sudo" || len(call.Args) < 3 || call.Args[0] != command || call.Args[1] != operation {
			continue
		}
		if callsContainArg([]run.FakeCall{call}, packageName) {
			return true
		}
	}
	return false
}
