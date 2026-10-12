package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// redirectGitHubAPI points ghrelease.Default's HTTP client at serverURL for
// the duration of the test, preserving request paths like the redirect
// tripper in internal/ghrelease tests. Callers must use a repo unique to
// the test so Default's in-memory release cache cannot serve a stale hit.
func redirectGitHubAPI(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	ghrelease.Default.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(req)
	})})
	t.Cleanup(func() {
		ghrelease.Default.SetHTTPClient(&http.Client{Timeout: 30 * time.Second})
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// regexLifecycleServer serves a fake GitHub releases API (latest release
// with tag and the given asset names) plus the asset bytes themselves, so
// Install tests exercise regex resolution without network. Asset download
// URLs keep their real extensions so format detection sees them.
func regexLifecycleServer(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			type apiAsset struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}
			var list []apiAsset
			for name := range assets {
				list = append(list, apiAsset{Name: name, URL: server.URL + "/files/" + name})
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": list}); err != nil {
				t.Errorf("encode release: %v", err)
			}
			return
		}
		if data, ok := assets[strings.TrimPrefix(r.URL.Path, "/files/")]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func testTarGzipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testZipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestGitHubRegexArchiveInstallRemoveWithoutEntrypoints is the core T3
// lifecycle regression: a .tar.gz selected by regex carries no format
// signal in its selector text, yet Install must treat it as an archive
// (no injected binary name) and Remove must take the archive path via
// persisted ownership even though no entrypoints are declared.
func TestGitHubRegexArchiveInstallRemoveWithoutEntrypoints(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX payload ownership assertion")
	}
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	const payloadFile = "payload-data/file.txt"
	server := regexLifecycleServer(t, "v1.0.0", map[string][]byte{
		"regextool-1.0.0-linux-amd64.tar.gz": testTarGzipBytes(t, map[string]string{payloadFile: "regextool payload"}),
	})
	redirectGitHubAPI(t, server.URL)

	dest := filepath.Join(root, "dest")
	tool := &config.Tool{Name: "regextool"}
	mc := &config.MethodCandidate{
		Kind:       "github",
		TargetArch: "amd64",
		TargetOS:   "linux",
		Config: map[string]any{
			"repo":          "t3-owner/t3-regex-tarball",
			"asset":         map[string]any{"regex": `regextool-{version}-linux-{arch_any}\.tar\.gz`},
			"extract_to":    dest,
			"sudo_required": false,
		},
	}

	adapter := NewGitHubAdapter()
	if err := adapter.Install(ctx, &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !mc.OwnsArchivePayload {
		t.Fatal("Install() did not record archive payload ownership")
	}
	// #nosec G304 -- dest is a test-owned temporary directory.
	data, err := os.ReadFile(filepath.Join(dest, payloadFile))
	if err != nil {
		t.Fatalf("installed payload file: %v", err)
	}
	if string(data) != "regextool payload" {
		t.Fatalf("installed payload = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dest, archiveOwnershipMarkerName)); err != nil {
		t.Fatalf("ownership marker: %v", err)
	}

	if err := adapter.Remove(ctx, &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("payload remains: %v", err)
	}
}

// TestGitHubRegexZipInstallRemoveWithEntrypoints covers the same selector
// blindness for .zip: the concrete URL — not the regex text — decides the
// archive path, entrypoint launchers are created, and removal takes both.
func TestGitHubRegexZipInstallRemoveWithEntrypoints(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher assertion")
	}
	ctx := context.Background()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	exectest.SetHome(t, home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	server := regexLifecycleServer(t, "v2.0.0", map[string][]byte{
		"zippy-2.0.0-linux-x86_64.zip": testZipBytes(t, map[string]string{"bin/zippy": "zippy binary"}),
	})
	redirectGitHubAPI(t, server.URL)

	dest, links := filepath.Join(home, "opt", "zippy"), filepath.Join(home, "bin")
	tool := &config.Tool{Name: "zippy"}
	mc := &config.MethodCandidate{
		Kind:       "github",
		TargetArch: "x86_64",
		TargetOS:   "linux",
		Config: map[string]any{
			"repo":          "t3-owner/t3-regex-zip",
			"asset":         map[string]any{"regex": `zippy-{version}-linux-{arch_any}\.zip`},
			"extract_to":    dest,
			"link_dir":      links,
			"entrypoints":   map[string]any{"zippy": "bin/zippy"},
			"sudo_required": false,
		},
	}

	adapter := NewGitHubAdapter()
	if err := adapter.Install(ctx, &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !mc.OwnsArchivePayload {
		t.Fatal("Install() did not record archive payload ownership")
	}
	target, err := os.Readlink(filepath.Join(links, "zippy"))
	if err != nil {
		t.Fatalf("launcher: %v", err)
	}
	if filepath.Clean(target) != filepath.Clean(filepath.Join(dest, "bin", "zippy")) {
		t.Fatalf("launcher target = %q", target)
	}

	if err := adapter.Remove(ctx, &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("payload remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(links, "zippy")); !os.IsNotExist(err) {
		t.Fatalf("launcher remains: %v", err)
	}
}

// TestGitHubRegexRemoveRefusesPayloadWithoutOwnership locks in the
// fail-closed guards for regex-configured payloads: without persisted
// ownership (or with a foreign marker) removal is refused and the
// directory is left untouched.
func TestGitHubRegexRemoveRefusesPayloadWithoutOwnership(t *testing.T) {
	ctx := context.Background()
	regexConfig := func(payload string) map[string]any {
		return map[string]any{
			"repo":        "t3-owner/t3-regex-legacy",
			"asset":       map[string]any{"regex": `regextool-.*\.tar\.gz`},
			"extract_to":  payload,
			"entrypoints": map[string]any{"regextool": "bin/regextool"},
		}
	}

	t.Run("missing marker", func(t *testing.T) {
		root := t.TempDir()
		payload := filepath.Join(root, "payload")
		if err := os.MkdirAll(payload, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(payload, "stale"), []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		tool := &config.Tool{Name: "regextool"}
		mc := &config.MethodCandidate{Kind: "github", Config: regexConfig(payload)}
		if err := NewGitHubAdapter().Remove(ctx, &run.FakeRunner{}, tool, mc); err == nil {
			t.Fatal("Remove() without ownership succeeded, want refusal")
		}
		if _, err := os.Stat(filepath.Join(payload, "stale")); err != nil {
			t.Fatalf("payload changed after refusal: %v", err)
		}
	})

	t.Run("foreign marker", func(t *testing.T) {
		root := t.TempDir()
		payload := filepath.Join(root, "payload")
		if err := os.MkdirAll(payload, 0o700); err != nil {
			t.Fatal(err)
		}
		foreignMC := &config.MethodCandidate{Kind: "github"}
		owner, err := expectedArchiveOwnership(&config.Tool{Name: "other"}, foreignMC)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeArchiveOwnership(payload, owner); err != nil {
			t.Fatal(err)
		}
		tool := &config.Tool{Name: "regextool"}
		mc := &config.MethodCandidate{Kind: "github", Config: regexConfig(payload), OwnsArchivePayload: true}
		if err := NewGitHubAdapter().Remove(ctx, &run.FakeRunner{}, tool, mc); err == nil {
			t.Fatal("Remove() of foreign payload succeeded, want refusal")
		}
		if _, err := os.Stat(filepath.Join(payload, archiveOwnershipMarkerName)); err != nil {
			t.Fatalf("payload changed after refusal: %v", err)
		}
	})
}

// TestHTTPRawBinaryRemoveUnchanged pins the raw-binary removal path: only
// the validated binary target is removed, siblings and the directory stay.
func TestHTTPRawBinaryRemoveUnchanged(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	extractTo := filepath.Join(root, "bin")
	if err := os.MkdirAll(extractTo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractTo, "demo"), []byte("binary"), 0o755); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractTo, "other"), []byte("sibling"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"url":        "https://example.invalid/demo",
		"extract_to": extractTo,
		"binary":     "demo",
	}}
	if err := NewHTTPAdapter().Remove(ctx, &run.FakeRunner{}, tool, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractTo, "demo")); !os.IsNotExist(err) {
		t.Fatalf("binary remains: %v", err)
	}
	// #nosec G304 -- extractTo is a test-owned temporary directory.
	if data, err := os.ReadFile(filepath.Join(extractTo, "other")); err != nil || string(data) != "sibling" {
		t.Fatalf("sibling changed: %q, %v", data, err)
	}
	if info, err := os.Stat(extractTo); err != nil || !info.IsDir() {
		t.Fatalf("install directory removed: %v", err)
	}
}

// TestGitHubInstallResolvedIgnoresRegexSelectorText proves the resolved
// plan is authoritative: even a hostile selector text (one that would look
// like a .deb) never influences classification — the concrete artifact URL
// installs as a raw binary under the tool name.
func TestGitHubInstallResolvedIgnoresRegexSelectorText(t *testing.T) {
	payload := []byte("resolved raw payload\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	extractTo := t.TempDir()
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "github", Config: map[string]any{
		"repo":          "t3-owner/t3-regex-stale",
		"asset":         map[string]any{"regex": `.*\.deb`},
		"extract_to":    extractTo,
		"sudo_required": false,
	}}
	resolved := plan.New("demo", "github", true)
	resolved.Artifacts = []plan.Artifact{{URL: server.URL + "/download"}}

	if err := NewGitHubAdapter().InstallResolved(context.Background(), &run.FakeRunner{}, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	// #nosec G304 -- extractTo is a test-owned temporary directory.
	data, err := os.ReadFile(filepath.Join(extractTo, "demo"))
	if err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("installed payload = %q, want resolved bytes", data)
	}
	if _, err := os.Stat(filepath.Join(extractTo, "download")); !os.IsNotExist(err) {
		t.Fatalf("selector text leaked into install path: %v", err)
	}
	if mc.OwnsArchivePayload {
		t.Fatal("raw install recorded archive payload ownership")
	}
}

// TestRequiresElevationRegexSelectorSeesLauncherDir proves a regex selector
// (whose format is unknowable without resolution) cannot silently drop the
// launcher-directory elevation that an archive install would need.
func TestRequiresElevationRegexSelectorSeesLauncherDir(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	exectest.SetHome(t, home)
	tool := &config.Tool{Name: "regextool"}
	regexAsset := func() map[string]any {
		return map[string]any{"regex": `regextool-.*`}
	}

	mc := &config.MethodCandidate{Kind: "github", Config: map[string]any{
		"repo":        "t3-owner/t3-regex-elevation",
		"asset":       regexAsset(),
		"extract_to":  filepath.Join(home, "tools", "regextool"),
		"entrypoints": map[string]any{"regextool": "bin/regextool"},
		"link_dir":    "/usr/local/bin",
	}}
	if !NewHTTPAdapter().RequiresElevation(tool, mc) {
		t.Fatal("RequiresElevation() = false, want true for a system launcher directory")
	}

	// Explicit sudo_required stays authoritative over the conservative
	// unknown-format answer.
	mc.Config["sudo_required"] = false
	if NewHTTPAdapter().RequiresElevation(tool, mc) {
		t.Fatal("RequiresElevation() = true with sudo_required=false, want explicit override")
	}
}
