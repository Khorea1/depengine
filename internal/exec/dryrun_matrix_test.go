package exec

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

// TestDryRunMatrixLeavesZeroHostMutations is the P0.1 acceptance probe: one
// dry-run over a manifest exercising hooks, sources, prerequisites, native
// index sync, HTTP/GitHub artifacts, Git builds, MSI, ecosystem managers, and
// containers must cause zero externally visible host mutations.
//
// Every adapter is mocked with an Install tripwire, hooks point at sentinel
// files, and the runner is fake, so any execution-side leak — hook execution,
// prerequisite installation, source setup, index sync, downloads, builds —
// shows up as a tripwire firing, a sentinel on disk, or a mutating runner
// call.
func TestDryRunMatrixLeavesZeroHostMutations(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	dir := t.TempDir()
	preSentinel := dir + "/dryrun-pre"
	postSentinel := dir + "/dryrun-post"

	var mu sync.Mutex
	var installed []string
	tripwire := func(name string) error {
		mu.Lock()
		installed = append(installed, name)
		mu.Unlock()
		return nil
	}
	mock := func(kind string) *testMockAdapter {
		return &testMockAdapter{
			kindValue:     kind,
			availableFunc: func() bool { return true },
			checkFunc:     func(string) bool { return false },
			installFunc:   tripwire,
		}
	}

	fr := &run.FakeRunner{ExitCode: 0}
	ex := New()
	WithRunner(fr)(ex)
	WithAdapters(
		mock("native"),
		mock("http"),
		mock("github"),
		mock("git"),
		mock("go"),
		mock("cargo"),
		mock("container"),
		mock("msi"),
	)(ex)
	WithAllowArbitraryCode()(ex)
	WithDryRun()(ex)

	nativeWithHooks := &config.Tool{
		Name:        "tool-native",
		PreInstall:  []config.Hook{{Run: []string{"touch", preSentinel}}},
		PostInstall: []config.Hook{{Run: []string{"touch", postSentinel}}},
		Methods:     []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "tool-native"}}},
	}
	sourced := &config.Tool{
		Name:     "tool-sourced",
		Requires: []string{"helper"},
		Methods: []*config.MethodCandidate{{
			Kind:    "native",
			Config:  map[string]any{"pkg": "tool-sourced"},
			Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}},
		}},
	}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"native", "http", "github", "git", "go", "cargo", "container", "msi"}},
		Tools: map[string]*config.Tool{
			"tool-native":    nativeWithHooks,
			"tool-sourced":   sourced,
			"tool-http":      {Name: "tool-http", Methods: []*config.MethodCandidate{{Kind: "http", Config: map[string]any{"url": "https://example.test/tool.tar.gz"}}}},
			"tool-gh":        {Name: "tool-gh", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "org/demo", "asset": "demo.tar.gz"}}}},
			"tool-git":       {Name: "tool-git", Methods: []*config.MethodCandidate{{Kind: "git", Config: map[string]any{"url": "https://example.test/demo.git", "build": map[string]any{"run": []any{"make"}}}}}},
			"tool-go":        {Name: "tool-go", Methods: []*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": "example.test/demo"}}}},
			"tool-cargo":     {Name: "tool-cargo", Methods: []*config.MethodCandidate{{Kind: "cargo", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}}},
			"tool-container": {Name: "tool-container", Methods: []*config.MethodCandidate{{Kind: "container", Config: map[string]any{"manager": "podman", "source": "org/demo", "tag": "edge"}}}},
			"tool-msi":       {Name: "tool-msi", Methods: []*config.MethodCandidate{{Kind: "msi", Config: map[string]any{"url": "https://example.test/tool.msi", "product_name": "Demo"}}}},
			"helper":         {Name: "helper", Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "helper"}, Requires: []string{}}}},
		},
	}
	// Lazy method-level prerequisite: must be planned, never installed.
	schema.Tools["tool-http"].Methods[0].Requires = []string{"helper"}

	report, err := ex.Execute(context.Background(), schema, "debian")
	if err != nil {
		t.Fatalf("dry-run Execute: %v", err)
	}
	if report.Failed != 0 || report.Success != 0 {
		t.Fatalf("dry-run report = %+v, want zero failed/success", report)
	}
	if report.WouldInstall == 0 || report.WouldInstall != len(report.Tools) {
		t.Fatalf("dry-run WouldInstall = %d, want every planned tool: %+v", report.WouldInstall, report)
	}
	for _, tool := range report.Tools {
		if tool.Status != StatusWouldInstall {
			t.Fatalf("tool %s status = %v, want would-install: %+v", tool.Tool, tool.Status, tool)
		}
		if tool.InstallCommitted {
			t.Fatalf("tool %s committed an install during dry-run", tool.Tool)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(installed) != 0 {
		t.Fatalf("dry-run reached adapter Install for %v", installed)
	}

	for _, sentinel := range []string{preSentinel, postSentinel} {
		if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
			t.Fatalf("dry-run hook created sentinel %s (stat err=%v)", sentinel, err)
		}
	}
	entries, err := os.ReadDir(stateHome)
	if err != nil {
		t.Fatalf("reading isolated state home: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry-run wrote state files: %v", entries)
	}

	for _, call := range fr.Calls {
		// Read-only source listing is an allowed probe; anything that adds,
		// installs, syncs, downloads, builds, or removes is a leak.
		if call.Name == "brew" && len(call.Args) == 1 && call.Args[0] == "tap" {
			continue
		}
		joined := strings.ToLower(call.Name + " " + strings.Join(call.Args, " "))
		for _, mutation := range []string{
			"update", "install", "uninstall", "remove", "upgrade",
			"clone", "curl", "wget", "msiexec", "touch", "make",
			"vendor/tools", "dryrun-pre", "dryrun-post",
		} {
			if strings.Contains(joined, mutation) {
				t.Fatalf("dry-run invoked mutating command %q", call.Name+" "+strings.Join(call.Args, " "))
			}
		}
	}
}
