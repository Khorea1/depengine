package exec

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/source"
)

const (
	explainAlphaHead = "0123456789abcdef0123456789abcdef01234567"
	explainBetaHead  = "1123456789abcdef0123456789abcdef01234567"
	explainNextHead  = "2123456789abcdef0123456789abcdef01234567"
)

type explainSourceRunner struct {
	*run.FakeRunner
	heads map[string]string
}

func (r *explainSourceRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	result := r.FakeRunner.Run(ctx, name, args...)
	var stdout string
	switch {
	case name == "brew" && len(args) == 1 && args[0] == "tap":
		stdout = "vendor/alpha\nvendor/beta\n"
	case name == "brew" && len(args) == 3 && args[0] == "tap-info" && args[1] == "--json=v1":
		tap := args[2]
		stdout = fmt.Sprintf(`[{"name":%q,"remote":%q}]`, tap, "https://github.com/"+tap+".git")
	case name == "brew" && len(args) == 2 && args[0] == "--repo":
		stdout = "/fake/homebrew/Library/Taps/" + strings.ReplaceAll(args[1], "/", "/homebrew-")
	case name == "git" && len(args) == 5 && args[0] == "-C" && args[2] == "rev-parse" && args[3] == "--verify" && args[4] == "HEAD^{commit}":
		for tap, head := range r.heads {
			if strings.Contains(args[1], strings.ReplaceAll(tap, "/", "/homebrew-")) {
				stdout = head
				break
			}
		}
	}
	result.Stdout = []byte(stdout)
	result.Stderr = nil
	result.ExitCode = 0
	result.Err = nil
	return result
}

func TestExplainToolWithSourceRevisionsUsesFreshCurrentToolSnapshot(t *testing.T) {
	runner := &explainSourceRunner{
		FakeRunner: &run.FakeRunner{},
		heads: map[string]string{
			"vendor/alpha": explainAlphaHead,
			"vendor/beta":  explainBetaHead,
		},
	}
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		presence:        plan.PresenceAbsent,
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)

	alpha := explainSourceTool("alpha-tool", "vendor/alpha")
	beta := explainSourceTool("beta-tool", "vendor/beta")

	attempts, revisions := ex.ExplainToolWithSourceRevisions(context.Background(), alpha, "")
	assertExplainSourceSnapshot(t, attempts, revisions, alpha.Name, "vendor/alpha", explainAlphaHead)

	// ExplainTool must take its own fresh source snapshot without changing or
	// contaminating a later explicit source-revision result on this Executor.
	runner.heads["vendor/alpha"] = explainNextHead
	plainAttempts := ex.ExplainTool(context.Background(), alpha, "")
	assertExplainPlanIdentity(t, plainAttempts, alpha.Name, "alpha-package")

	attempts, revisions = ex.ExplainToolWithSourceRevisions(context.Background(), beta, "")
	assertExplainSourceSnapshot(t, attempts, revisions, beta.Name, "vendor/beta", explainBetaHead)

	// Re-reading the same local source after HEAD changes must report the new
	// commit, not a revision retained by an earlier explanation.
	attempts, revisions = ex.ExplainToolWithSourceRevisions(context.Background(), alpha, "")
	assertExplainSourceSnapshot(t, attempts, revisions, alpha.Name, "vendor/alpha", explainNextHead)
}

func explainSourceTool(toolName, tap string) *config.Tool {
	packageName := strings.TrimSuffix(toolName, "-tool") + "-package"
	return &config.Tool{
		Name:       toolName,
		MethodOnly: []string{"cargo"},
		Methods: []*config.MethodCandidate{{
			Kind:   "cargo",
			Config: map[string]any{"pkg": packageName},
			Sources: []config.Source{{
				Kind: "brew-tap",
				Name: tap,
				URL:  "https://github.com/" + tap + ".git",
			}},
		}},
	}
}

func assertExplainSourceSnapshot(t *testing.T, attempts []MethodAttempt, revisions []source.SourceRevision, toolName, sourceName, revision string) {
	t.Helper()
	packageName := strings.TrimSuffix(toolName, "-tool") + "-package"
	assertExplainPlanIdentity(t, attempts, toolName, packageName)
	if len(revisions) != 1 {
		t.Fatalf("source revisions for %s = %+v, want only %s", toolName, revisions, sourceName)
	}
	got := revisions[0]
	if got.Kind != "brew-tap" || got.Name != sourceName || got.Revision != revision {
		t.Fatalf("source revisions for %s = %+v, want brew-tap %s at %s", toolName, revisions, sourceName, revision)
	}
}

func assertExplainPlanIdentity(t *testing.T, attempts []MethodAttempt, toolName, packageName string) {
	t.Helper()
	if len(attempts) != 1 {
		t.Fatalf("attempts for %s = %+v, want one candidate", toolName, attempts)
	}
	attempt := attempts[0]
	if attempt.Kind != "cargo" || !attempt.CandidateKnown || attempt.Candidate != 0 {
		t.Fatalf("attempt identity for %s = %+v, want declared cargo candidate 0", toolName, attempt)
	}
	if attempt.PlanIntent == nil || attempt.PlanIntent.Tool.Name != toolName || attempt.PlanIntent.Identity.Package != packageName {
		t.Fatalf("plan identity for %s = %+v, want package %s", toolName, attempt.PlanIntent, packageName)
	}
}

var _ run.Runner = (*explainSourceRunner)(nil)
