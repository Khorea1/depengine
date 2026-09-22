package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func TestExplainToolGitHubBranchIsNotRevisionCapability(t *testing.T) {
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(&testMockAdapter{kindValue: "github", availableFunc: func() bool { return true }})(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "github", Config: map[string]any{"repo": "org/demo", "asset": "demo.tar.gz", "branch": "edge"},
	}}}
	attempts := ex.ExplainTool(context.Background(), tool, "unknown")
	if len(attempts) != 1 || attempts[0].Status == "skip_capability" {
		t.Fatalf("attempts = %+v, github branch must remain an artifact selector", attempts)
	}
}
