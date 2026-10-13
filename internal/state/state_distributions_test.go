package state

// State identity for proposal E (8.5b): persisted desired-state hashes must
// follow the specialized effective candidate — equal to a hand-written
// equivalent, distinct from the unspecialized base. Removal keeps consuming
// the recorded state, so no new pinning logic is needed here.

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
)

func TestDesiredStateHashFollowsSpecializedCandidate(t *testing.T) {
	base := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "o/r", "asset": "base.tar.gz", "checksum": "sha256:auto", "scope": "user"},
		Distributions: []config.Distribution{
			{When: &config.Condition{OS: []string{"linux"}}, Asset: config.AssetSelector{Template: "linux-only.tar.gz"}, HasAsset: true},
		},
	}
	facts := &platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"}
	specialized, err := config.SpecializeDistribution(base, facts)
	if err != nil {
		t.Fatal(err)
	}
	handwritten := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "o/r", "asset": "linux-only.tar.gz", "checksum": "sha256:auto", "scope": "user"},
	}
	withMethod := func(mc *config.MethodCandidate) *config.Tool {
		return &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{mc}}
	}
	if got, want := DesiredStateHash(withMethod(specialized)), DesiredStateHash(withMethod(handwritten)); got != want {
		t.Fatal("specialized candidate must hash exactly like its hand-written equivalent")
	}
	if got, base := DesiredStateHash(withMethod(specialized)), DesiredStateHash(withMethod(base)); got == base {
		t.Fatal("specialized candidate must hash distinctly from the unspecialized base")
	}
}
