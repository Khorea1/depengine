package planner_test

import (
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/contracttest"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
)

// TestResolveEffectFieldsMoveStaticIntent is the P0.4 central invariant: a
// schema field accepted with EffectResolve must observably affect the static
// install plan. Two candidates differing only in that field must resolve to
// different intents (or the second value must fail validation). A field that
// builds two identical successful plans is accepted but ignored.
//
// Runtime-resolved fields stay explicit here so a new exclusion cannot become
// an unreviewed escape hatch. Each exclusion names both its reason and the
// production boundary that must provide the behavioral probe.
type resolveEffectExclusion struct {
	Rationale string
	Consumer  string
}

var resolveEffectExclusions = map[string]resolveEffectExclusion{
	"github.release":       {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"http.release":         {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"msi.release":          {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"appimage.release":     {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"android.release":      {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"github.branch":        {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"http.branch":          {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"msi.branch":           {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"appimage.branch":      {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"android.branch":       {"resolved by the runtime download selector", "internal/httpdownload/resolver.go"},
	"git.url":              {"consumed by the git adapter at clone time", "internal/git/adapter.go"},
	"native.pkg_overrides": {"resolved from host clan by the native adapter", "internal/exec/native_adapter.go"},
}

func TestResolveEffectFieldsMoveStaticIntent(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	usedExclusions := make(map[string]bool, len(resolveEffectExclusions))

	for _, contract := range methodkind.Contracts {
		for name, field := range contract.Fields {
			if field.Effects&methodkind.EffectResolve == 0 {
				continue
			}
			key := contract.Kind + "." + name
			if exclusion, ok := resolveEffectExclusions[key]; ok {
				usedExclusions[key] = true
				if exclusion.Rationale == "" || exclusion.Consumer == "" {
					t.Errorf("%s has an incomplete EffectResolve exclusion", key)
				}
				if _, ok := contracttest.CoverageFor(contracttest.PhaseResolveRuntime, key); !ok {
					t.Errorf("%s is excluded from static resolution without a runtime resolution probe", key)
				}
				continue
			}

			pair, ok := contracttest.FieldPair(contract.Kind, name)
			if !ok {
				t.Errorf("%s declares EffectResolve but has no differential probe; add values or justify an exclusion", key)
				continue
			}
			if _, ok := contracttest.BaseConfig(contract.Kind, name); !ok {
				t.Errorf("%s has no probe base config", key)
				continue
			}

			build := func(value any) (any, error) {
				cfg, _ := contracttest.BaseConfig(contract.Kind, name)
				if (contract.Kind == "http" || contract.Kind == "appimage" || contract.Kind == "android" || contract.Kind == "msi") && (name == "secret_ref" || name == "checksum_secret_ref" || name == "signature_secret_ref") || (contract.Kind == "github" || contract.Kind == "git" || contract.Kind == "cargo") && name == "secret_ref" {
					ref := value.(map[string]any)
					method := &config.MethodCandidate{
						Kind:   contract.Kind,
						Config: cfg,
					}
					secretRef := &config.SecretReference{Provider: ref["provider"].(string), Name: ref["name"].(string)}
					switch name {
					case "secret_ref":
						method.SecretRef = secretRef
					case "checksum_secret_ref":
						method.ChecksumSecretRef = secretRef
					case "signature_secret_ref":
						method.SignatureSecretRef = secretRef
					}
					return planner.BuildCandidateIntent(tool, method)
				}
				cfg[name] = value
				return planner.BuildCandidateIntent(tool, &config.MethodCandidate{Kind: contract.Kind, Config: cfg})
			}
			a, err := build(pair[0])
			if err != nil {
				t.Fatalf("%s: probe value %v rejected, fix the probe: %v", key, pair[0], err)
			}
			b, err := build(pair[1])
			if err != nil {
				continue
			}
			if reflect.DeepEqual(a, b) {
				t.Errorf("%s is accepted but ignored: distinct values resolve to identical plans", key)
			}
		}
	}

	for key := range resolveEffectExclusions {
		if !usedExclusions[key] {
			t.Errorf("EffectResolve exclusion %q no longer matches a declared EffectResolve field", key)
		}
	}
}
