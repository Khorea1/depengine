package planner_test

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/planner"
)

func TestBuildCandidateIntentFeedsCapabilityBoundary(t *testing.T) {
	tool, method := candidate("demo", "native", map[string]any{"pkg": "demo", "version": "1.2.3"})
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	contract, _ := methodkind.Lookup("native")
	missing, err := contract.MissingPlanCapabilities(p)
	if err != nil {
		t.Fatalf("MissingPlanCapabilities() error: %v", err)
	}
	if missing&methodkind.CapabilityExactVersion == 0 {
		t.Fatalf("missing = %v, want exact-version", methodkind.CapabilityNames(missing))
	}
}

func candidate(name, kind string, cfg map[string]any) (*config.Tool, *config.MethodCandidate) {
	method := &config.MethodCandidate{Kind: kind, Config: cfg}
	return &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}, method
}
