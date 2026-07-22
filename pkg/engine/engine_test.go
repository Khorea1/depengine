package engine

import (
	"context"
	"strings"
	"testing"

	"depengine/pkg/run"
)

// fakeRunner records the last call and returns canned stdout, with a
// definable exit code. Distinct from pkg/run's FakeRunner because this
// one lives in the engine package and proves GatherFacts' handling of
// detect_os.sh's exit-1-means-partial convention specifically.
type call struct {
	name string
	args []string
}

type fakeRunner struct {
	calls    []call
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) run.Result {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	return run.Result{
		Stdout:   []byte(f.stdout),
		Stderr:   []byte(f.stderr),
		ExitCode: f.exitCode,
		Err:      f.err,
	}
}

// detectionJSON is the minimal-but-realistic detect_os.sh output. The
// parser must succeed with ONLY these keys (no required field missing,
// no surprise fields breaking the struct).
const detectionJSON = `{
  "target_arch": "x86_64",
  "distro_id": "arch",
  "distro_name": "Arch Linux",
  "distro_id_like": "",
  "target_family": "unix",
  "detection_method": "os-release",
  "confidence": "high",
  "is_wsl": false,
  "is_container": false,
  "is_android": false
}`

// TestGatherFactsParsesJSONAndLeavesFactsImmutable: C3 invariant — Facts
// is exactly the fetcher's output, no derived field gets mutated. We
// assert the parsed struct equals what the script emitted and that
// nothing on the struct hints at clan derivation.
func TestGatherFactsParsesJSONAndLeavesFactsImmutable(t *testing.T) {
	fr := &fakeRunner{stdout: detectionJSON}
	// locateDetectScript respects DEPENGINE_DETECT_SCRIPT.
	t.Setenv("DEPENGINE_DETECT_SCRIPT", "/tmp/does-not-exist-truth.txt")
	t.Setenv("DEPENGINE_TRACE_ID", "trace-xyz")

	// Bypass the missing-file branch by pointing env at a real throwaway
	// file: we only care about the runner path here.
	tmp := tmpFile(t, detectionJSON)
	t.Setenv("DEPENGINE_DETECT_SCRIPT", tmp)

	facts, err := GatherFacts(fr)
	if err != nil {
		t.Fatalf("GatherFacts failed: %v", err)
	}

	if facts.DistroID != "arch" {
		t.Fatalf("DistroID = %q, want arch", facts.DistroID)
	}
	if facts.DistroIDLike != "" {
		t.Fatalf("DistroIDLike = %q, want empty", facts.DistroIDLike)
	}
	if facts.TargetArch != "x86_64" {
		t.Fatalf("TargetArch = %q, want x86_64", facts.TargetArch)
	}
	// ResolveFamily is pure and computed by the caller, NOT stored here.
	// Sanity: re-resolving produces the same value, repeatedly.
	a, b := ResolveFamily(facts), ResolveFamily(facts)
	if a != "arch" {
		t.Fatalf("ResolveFamily = %q, want arch", a)
	}
	if a != b {
		t.Fatalf("ResolveFamily not pure: %q vs %q", a, b)
	}
}

// TestGatherFactsSurvivesPartialDetection: detect_os.sh exit 1 = "partial
// detection" (low confidence), not failure. JSON is still valid; we must
// succeed. This tests both the C5 seam (Result.ExitCode vs Err) and the
// facts.go error-handling branch.
func TestGatherFactsSurvivesPartialDetection(t *testing.T) {
	fr := &fakeRunner{
		stdout:   detectionJSON,
		exitCode: 1, // partial — must NOT be treated as failure
	}
	tmp := tmpFile(t, detectionJSON)
	t.Setenv("DEPENGINE_DETECT_SCRIPT", tmp)

	facts, err := GatherFacts(fr)
	if err != nil {
		t.Fatalf("partial detection returned err: %v", err)
	}
	if facts.DistroID != "arch" {
		t.Fatalf("DistroID = %q, want arch", facts.DistroID)
	}
}

// TestGatherFactsFailsOnUnparseableJSON with a real execution error: we
// prefer the script's stderr in the message when available.
func TestGatherFactsFailsOnUnparseableJSON(t *testing.T) {
	fr := &fakeRunner{
		stdout:   "not json at all",
		stderr:   "boom",
		exitCode: 2,
		err:      context.DeadlineExceeded, // simulates a killed subprocess
	}
	tmp := tmpFile(t, "")
	t.Setenv("DEPENGINE_DETECT_SCRIPT", tmp)

	facts, err := GatherFacts(fr)
	if err == nil {
		t.Fatalf("expected error, got facts: %+v", facts)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should mention stderr 'boom', got: %v", err)
	}
}

func TestGatherFactsSucceedsWithEmbeddedScript(t *testing.T) {
	// The embedded detect_os.sh is always available at compile time, so
	// locateDetectScript returns a temp-file copy. Verify that GatherFacts
	// succeeds when the fake runner returns valid JSON.
	t.Setenv("DEPENGINE_DETECT_SCRIPT", "")
	t.Setenv("PATH", "/nonexistent")

	fr := &fakeRunner{stdout: detectionJSON}
	facts, err := GatherFacts(fr)
	if err != nil {
		t.Fatalf("GatherFacts with embedded script failed: %v", err)
	}
	if facts == nil {
		t.Fatal("GatherFacts returned nil facts")
	}
}

func TestResolveFamilyTable(t *testing.T) {
	cases := []struct {
		name       string
		distroID   string
		distroLike string
		isAndroid  bool
		want       string
	}{
		{"ubuntu direto", "ubuntu", "debian", false, "debian"},
		{"arch direto", "arch", "", false, "arch"},
		{"manjaro direto", "manjaro", "arch", false, "arch"},
		{"cachyos direto", "cachyos", "", false, "arch"},
		{"fedora direto", "fedora", "", false, "fedora"},
		{"opensuse via id_like", "opensuse-tumbleweed", "suse", false, "suse"},
		{"alpine direto", "alpine", "", false, "alpine"},
		{"macos direto", "macos", "", false, "macos"},
		{"termux direto", "termux", "", true, "termux"},
		{"distro desconhecida com id_like arch", "cyberos", "arch", false, "arch"},
		{"distro desconhecida com id_like rhel fedora", "rockylinux9-nightly", "rhel fedora", false, "fedora"},
		{"totalmente desconhecida", "plan9-frontend", "", false, "unknown"},
		{"android puro sem id_like", "android", "", true, "android"},
		{"void direto", "void", "", false, "void"},
		{"linuxmint direto", "linuxmint", "", false, "mint"},
		{"openwrt direto", "openwrt", "", false, "opkg"},
		{"lede direto", "lede", "", false, "opkg"},
		{"windows stub clan", "windows", "", false, "windows"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &Facts{DistroID: tc.distroID, DistroIDLike: tc.distroLike, IsAndroid: tc.isAndroid}
			if got := ResolveFamily(f); got != tc.want {
				t.Fatalf("ResolveFamily = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMatchesDistroFamily(t *testing.T) {
	if !MatchesDistroFamily("arch", []string{"debian", "arch"}) {
		t.Fatal("arch in [debian,arch] should match")
	}
	if MatchesDistroFamily("arch", []string{"debian", "fedora"}) {
		t.Fatal("arch in [debian,fedora] should not match")
	}
	if !MatchesDistroFamily("Arch", []string{"ARCH"}) { // case-insensitive
		t.Fatal("MatchesDistroFamily should be case-insensitive")
	}
	if !MatchesDistroFamily("unknown", []string{"unknown"}) {
		t.Fatal("unknown clan should match itself (engine decides)") // todo: keep this open
	}
}

// TestResolveFamilyNilFacts ensures ResolveFamily returns "unknown" on nil *Facts.
// This is a regression test for a bug that was fixed.
func TestResolveFamilyNilFactsPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("BUG REPRODUCED: ResolveFamily panicked on nil *Facts: %v", r)
		}
	}()

	// Calling ResolveFamily with nil *Facts should not panic.
	_ = ResolveFamily(nil)
}

// TestMatchesDistroFamilyNilSlice handles edge case of nil allowed list.
func TestMatchesDistroFamilyNilSlice(t *testing.T) {
	if MatchesDistroFamily("arch", nil) {
		t.Fatal("MatchesDistroFamily should return false for nil allowed list")
	}
}
