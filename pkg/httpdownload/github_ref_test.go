package httpdownload

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
)

func TestGitHubHTTPDelegateDefaultsBinaryToToolName(t *testing.T) {
	mc := &config.MethodCandidate{Kind: "github", Config: map[string]any{"repo": "owner/repo"}}
	got := githubHTTPDelegate(&config.Tool{Name: "tool"}, mc, "https://example.com/asset")
	if got.Config["binary"] != "tool" {
		t.Fatalf("binary = %v, want tool", got.Config["binary"])
	}
	if _, ok := mc.Config["binary"]; ok {
		t.Fatal("delegate mutated source config")
	}
}

func TestGitHubHTTPDelegatePreservesExplicitBinary(t *testing.T) {
	mc := &config.MethodCandidate{Kind: "github", Config: map[string]any{"binary": "yq"}}
	got := githubHTTPDelegate(&config.Tool{Name: "tool"}, mc, "https://example.com/asset")
	if got.Config["binary"] != "yq" {
		t.Fatalf("binary = %v, want yq", got.Config["binary"])
	}
}

func TestGithubRefDefaultsToLatest(t *testing.T) {
	if got := githubRef(map[string]any{}); got != "" {
		t.Errorf("no release/branch set: githubRef = %q, want \"\" (latest)", got)
	}
}

func TestGithubRefExplicitLatest(t *testing.T) {
	if got := githubRef(map[string]any{"release": "latest"}); got != "" {
		t.Errorf("release=latest: githubRef = %q, want \"\" (latest)", got)
	}
}

func TestGithubRefNamedRelease(t *testing.T) {
	if got := githubRef(map[string]any{"release": "nightly"}); got != "nightly" {
		t.Errorf("release=nightly: githubRef = %q, want \"nightly\"", got)
	}
}

func TestGithubRefBranch(t *testing.T) {
	if got := githubRef(map[string]any{"branch": "unstable"}); got != "unstable" {
		t.Errorf("branch=unstable: githubRef = %q, want \"unstable\"", got)
	}
}

func TestGithubRefBranchTakesPriorityIfBothSet(t *testing.T) {
	// Schema-level validation (pkg/validate/structural.go) rejects setting
	// both, but githubRef must still behave deterministically if it's ever
	// called on an unvalidated MethodCandidate (e.g. constructed directly
	// in a test, or a future caller that skips Validate).
	got := githubRef(map[string]any{"release": "nightly", "branch": "unstable"})
	if got != "unstable" {
		t.Errorf("both set: githubRef = %q, want \"unstable\" (branch wins)", got)
	}
}

func TestGithubRefIgnoresNonStringValues(t *testing.T) {
	if got := githubRef(map[string]any{"release": true, "branch": 42}); got != "" {
		t.Errorf("non-string release/branch: githubRef = %q, want \"\" (ignored)", got)
	}
}
