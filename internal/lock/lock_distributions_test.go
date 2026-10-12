package lock

// Old-lock compatibility for proposal E (8.5b): a v1 lock written for the
// explicit-candidates representation (A) must fail frozen validation against
// the distributions representation (E) with an explicit drift error — never
// with guessing, identity corruption, or silent ownership loss. Offline.

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

const distributionsPinChecksum = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func githubCandidate(label, asset string) *config.MethodCandidate {
	return &config.MethodCandidate{
		Kind:  "github",
		Label: label,
		Config: map[string]any{
			"repo":     "Dicklesworthstone/beads_rust",
			"asset":    asset,
			"release":  "v1.0.0",
			"checksum": distributionsPinChecksum,
			"scope":    "user",
		},
	}
}

// lockForSchema builds a minimal valid v1 lock for schemas whose github
// methods use literal checksums (no network resolution needed).
func lockForSchema(t *testing.T, s *config.Schema) *Lock {
	t.Helper()
	methodsHash, sourceHash, err := SnapshotIntentMetadata(s)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]ToolPin{}
	for name, tool := range s.Tools {
		count := map[string]int{}
		for _, method := range tool.Methods {
			idx := count[method.Kind]
			count[method.Kind] = idx + 1
			tools[toolKey(name, method.Kind, idx)] = ToolPin{Checksum: distributionsPinChecksum}
		}
	}
	return &Lock{Version: 1, Tools: tools, MethodsHash: methodsHash, SourceHash: sourceHash}
}

func TestFrozenV1AcceptsOwnRepresentation(t *testing.T) {
	schemaA := &config.Schema{Tools: map[string]*config.Tool{"br": {
		Name: "br",
		Methods: []*config.MethodCandidate{
			githubCandidate("github-mac", "br-mac.tar.gz"),
			githubCandidate("github-linux", "br-linux.tar.gz"),
		},
	}}}
	lockA := lockForSchema(t, schemaA)
	if err := ValidateFrozen(schemaA, lockA); err != nil {
		t.Fatalf("control lock must validate: %v", err)
	}
}

func TestFrozenV1RejectsRepresentationChangeExplicitly(t *testing.T) {
	schemaA := &config.Schema{Tools: map[string]*config.Tool{"br": {
		Name: "br",
		Methods: []*config.MethodCandidate{
			githubCandidate("github-mac", "br-mac.tar.gz"),
			githubCandidate("github-linux", "br-linux.tar.gz"),
		},
	}}}
	lockA := lockForSchema(t, schemaA)
	schemaE := &config.Schema{Tools: map[string]*config.Tool{"br": {
		Name: "br",
		Methods: []*config.MethodCandidate{{
			Kind:  "github",
			Label: "",
			Config: map[string]any{
				"repo":     "Dicklesworthstone/beads_rust",
				"asset":    "br-linux.tar.gz",
				"release":  "v1.0.0",
				"checksum": distributionsPinChecksum,
				"scope":    "user",
			},
			Distributions: []config.Distribution{
				{When: &config.Condition{OS: []string{"darwin"}}},
				{When: &config.Condition{OS: []string{"linux"}}},
			},
		}},
	}}}
	err := ValidateFrozen(schemaE, lockA)
	if err == nil || !strings.Contains(err.Error(), "methods changed for tool") {
		t.Fatalf("err = %v, want explicit methods-changed drift (no guessing)", err)
	}
}
