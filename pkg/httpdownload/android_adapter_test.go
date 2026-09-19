package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

func androidTool(name string) *config.Tool { return &config.Tool{Name: name} }

// androidTestRunner wraps FakeRunner but intercepts "termux-open" calls
// separately, so tests can make `which curl`/`which wget` fail (forcing
// GoDownloader) while still controlling termux-open's own exit code —
// FakeRunner only supports one ExitCode for every command.
type androidTestRunner struct {
	*run.FakeRunner
	termuxOpenCalls [][]string
	termuxOpenExit  int
	termuxOpenErr   error
}

func (r *androidTestRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	if name == "termux-open" {
		r.termuxOpenCalls = append(r.termuxOpenCalls, args)
		return run.Result{ExitCode: r.termuxOpenExit, Err: r.termuxOpenErr}
	}
	return r.FakeRunner.Run(ctx, name, args...)
}

func TestAndroidAdapterKind(t *testing.T) {
	if got := NewAndroidAdapter().Kind(); got != "android" {
		t.Fatalf("Kind() = %q, want %q", got, "android")
	}
}

// --- Available ---

func TestAndroidAdapterAvailableRequiresPrefixEnv(t *testing.T) {
	t.Setenv("PREFIX", "")
	fr := &run.FakeRunner{ExitCode: 0} // `which termux-open` would succeed...
	if NewAndroidAdapter().Available(context.Background(), fr) {
		t.Fatal("Available should be false outside Termux (no $PREFIX), regardless of termux-open on PATH")
	}
	if len(fr.Calls) != 0 {
		t.Errorf("expected no LookPath call when $PREFIX is unset (short-circuit), got %+v", fr.Calls)
	}
}

func TestAndroidAdapterAvailableRequiresTermuxOpen(t *testing.T) {
	t.Setenv("PREFIX", "/data/data/com.termux/files/usr")
	fr := &run.FakeRunner{ExitCode: 1} // `which termux-open` fails
	if NewAndroidAdapter().Available(context.Background(), fr) {
		t.Fatal("Available should be false when termux-open is not on PATH")
	}
}

func TestAndroidAdapterAvailableTrue(t *testing.T) {
	t.Setenv("PREFIX", "/data/data/com.termux/files/usr")
	fr := &run.FakeRunner{ExitCode: 0}
	if !NewAndroidAdapter().Available(context.Background(), fr) {
		t.Fatal("Available should be true when $PREFIX is set and termux-open is on PATH")
	}
}

// --- Check ---

func TestAndroidAdapterCheckInstalled(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.com/app.apk"}}
	wantPath := filepath.Join(config.ExpandHomeDir(androidAPKDir), "obsidian.apk")
	if err := os.MkdirAll(filepath.Dir(wantPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wantPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fr := &run.FakeRunner{}
	if !NewAndroidAdapter().Check(context.Background(), fr, androidTool("obsidian"), mc) {
		t.Fatal("Check should be true when the stable-named .apk exists")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("filesystem check executed commands: %+v", fr.Calls)
	}
}

func TestAndroidAdapterCheckNotInstalled(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.com/app.apk"}}
	fr := &run.FakeRunner{}
	if NewAndroidAdapter().Check(context.Background(), fr, androidTool("obsidian"), mc) {
		t.Fatal("Check should be false when the .apk hasn't been downloaded yet")
	}
}

func TestAndroidAdapterCheckNamelessToolIsFalse(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{}}
	fr := &run.FakeRunner{ExitCode: 0}
	if NewAndroidAdapter().Check(context.Background(), fr, &config.Tool{}, mc) {
		t.Fatal("Check should be false when the tool has no name to derive a filename from")
	}
}

// --- Install: error paths ---

func TestAndroidAdapterInstallNoURL(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{}}
	err := NewAndroidAdapter().Install(context.Background(), &run.FakeRunner{}, androidTool("obsidian"), mc)
	if err == nil {
		t.Fatal("expected error when url is missing")
	}
}

// --- Install: full happy path ---

// The versioned asset must be downloaded directly under the stable
// "<tool>.apk" target and then handed to termux-open.
func TestAndroidAdapterInstallStableNameAndDispatches(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	const body = "fake-apk-bytes"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer ts.Close()

	mc := &config.MethodCandidate{Config: map[string]any{
		"url": ts.URL + "/obsidian-1.5.3-android.apk",
	}}
	tool := androidTool("obsidian")
	rn := &androidTestRunner{FakeRunner: &run.FakeRunner{ExitCode: 1}} // no curl/wget -> GoDownloader

	a := NewAndroidAdapter()
	if err := a.Install(context.Background(), rn, tool, mc); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	stablePath := filepath.Join(config.ExpandHomeDir(androidAPKDir), "obsidian.apk")
	data, err := os.ReadFile(stablePath)
	if err != nil {
		t.Fatalf("expected stable-named apk at %s: %v", stablePath, err)
	}
	if string(data) != body {
		t.Fatalf("installed content = %q, want %q", data, body)
	}

	versionedPath := filepath.Join(config.ExpandHomeDir(androidAPKDir), "obsidian-1.5.3-android.apk")
	if _, err := os.Stat(versionedPath); !os.IsNotExist(err) {
		t.Fatalf("expected versioned filename to be renamed away, but it still exists at %s", versionedPath)
	}

	if len(rn.termuxOpenCalls) != 1 || len(rn.termuxOpenCalls[0]) != 1 || rn.termuxOpenCalls[0][0] != stablePath {
		t.Fatalf("termux-open calls = %+v, want exactly one call with %s", rn.termuxOpenCalls, stablePath)
	}

	if !a.Check(context.Background(), &run.FakeRunner{ExitCode: 0}, tool, mc) {
		t.Error("Check should report installed after a successful Install")
	}
}

// TestAndroidAdapterInstallTermuxOpenFailure ensures a failed dispatch
// (e.g. the Termux:API companion app isn't installed) surfaces as an
// Install error instead of being silently swallowed — the whole point of
// this adapter is the hand-off, so a failed hand-off is a failed Install.
func TestAndroidAdapterInstallTermuxOpenFailure(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content"))
	}))
	defer ts.Close()

	mc := &config.MethodCandidate{Config: map[string]any{"url": ts.URL + "/app.apk"}}
	rn := &androidTestRunner{
		FakeRunner:     &run.FakeRunner{ExitCode: 1},
		termuxOpenExit: 1,
	}

	if err := NewAndroidAdapter().Install(context.Background(), rn, androidTool("app"), mc); err == nil {
		t.Fatal("expected error when termux-open exits non-zero")
	}
}

// --- Remover: intentionally not implemented ---

func TestAndroidAdapterIsNotARemover(t *testing.T) {
	if _, ok := any(NewAndroidAdapter()).(interface{ CanRemove() bool }); ok {
		t.Fatal("android adapter should not implement Remover — see the doc comment on why removal stays manual")
	}
}

func TestAndroidInstallRejectsNonAPKBeforeDownload(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.com/tool.zip"}}
	err := NewAndroidAdapter().Install(context.Background(), fr, androidTool("tool"), mc)
	if err == nil || !strings.Contains(err.Error(), ".apk") {
		t.Fatalf("Install() error = %v, want .apk requirement", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("invalid artifact triggered subprocesses: %#v", fr.Calls)
	}
}
