package exec

// Permanent characterization of the arbitrary-code gate
// (dep-gate-lean-operations-bit-7j4).
//
// methodRunsArbitraryCode answers one question: does executing this candidate
// cross the arbitrary-code capability boundary? The production gate answers it
// by reading one bit of the validated candidate plan. legacyMethodRunsArbitraryCode
// below is the gate as it was before that slimming: the generic planning
// boundary (candidatePlanIntent: build + CheckRequirements) followed by a full
// PlanCapabilities pass. The two must agree on every candidate, in both
// directions. A divergence where the new gate returns false and the legacy
// gate true is a fail-open regression; the opposite direction is an
// unannounced behavior change. Either fails this test.
//
// The test also pins the premise the slimming relies on, so a future change to
// Validate, SourceCapabilities or the version-mode table cannot silently
// reopen the gap: a plan that BuildCandidateIntent accepted must never make
// PlanCapabilities fail, and the capability bit must equal
// methodkind.PlanRequiresArbitraryCode.

import (
	"fmt"
	"sort"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
)

// legacyMethodRunsArbitraryCode is the gate before dep-gate-lean-operations-bit-7j4,
// kept verbatim as the test oracle. Do not "improve" it: its value is that it
// does not share code with the production gate beyond the shared primitives.
func legacyMethodRunsArbitraryCode(tool *config.Tool, method *config.MethodCandidate) bool {
	if intent, _ := candidatePlanIntent(tool, method); intent != nil {
		if capabilities, err := methodkind.PlanCapabilities(*intent); err == nil {
			return capabilities&methodkind.CapabilityArbitraryCode != 0
		}
	}
	return len(method.PreInstall) > 0 || len(method.PostInstall) > 0 || configuredCommand(method)
}

// gateCase is one candidate of the characterization matrix.
type gateCase struct {
	name   string
	tool   *config.Tool
	method *config.MethodCandidate
}

var gateCommandForms = []struct {
	name  string
	value any
}{
	{"shell-string", "make install"},
	{"argv-table", map[string]any{"run": []any{"make", "install"}}},
	{"command-list", []any{map[string]any{"run": []any{"make"}}}},
}

// gateFieldValue returns a plausible value for one schema field so that the
// generated candidate has a fair chance of passing the planner. Failing to
// plan is also a covered branch; the test only requires every branch to be hit.
func gateFieldValue(name string, field methodkind.Field) any {
	switch field.Type {
	case methodkind.Boolean:
		return true
	case methodkind.Integer:
		return 1
	case methodkind.IntegerOrString:
		return "1"
	case methodkind.StringList:
		return []any{"x"}
	case methodkind.StringMap, methodkind.StringStringMap:
		return map[string]any{"a": "b"}
	case methodkind.Command:
		return "make install"
	}
	if len(field.Enum) > 0 {
		return field.Enum[0]
	}
	switch name {
	case "url":
		return "https://example.test/x.git"
	case "local_path":
		return "/tmp/x"
	}
	return "x"
}

// gateBaseConfigs returns a "required fields only" config for the contract
// (satisfying its first source alternative), and the empty config.
func gateBaseConfigs(contract *methodkind.Contract) []map[string]any {
	base := map[string]any{}
	for name, field := range contract.Fields {
		if field.Required && field.Type != methodkind.Command && field.Type != methodkind.SecretRef {
			base[name] = gateFieldValue(name, field)
		}
	}
	if len(contract.SourceAlternatives) > 0 {
		for _, name := range contract.SourceAlternatives[0] {
			if field, ok := contract.Fields[name]; ok && field.Type != methodkind.Command && field.Type != methodkind.SecretRef {
				base[name] = gateFieldValue(name, field)
			}
		}
	}
	return []map[string]any{{}, base}
}

func gateCloneConfig(in map[string]any, extra map[string]any) map[string]any {
	out := make(map[string]any, len(in)+len(extra))
	for k, v := range in {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

var gateHooks = []struct {
	name string
	pre  []config.Hook
	post []config.Hook
}{
	{name: "no-hooks"},
	{name: "pre", pre: []config.Hook{{Run: []string{"echo", "pre"}}}},
	{name: "post", post: []config.Hook{{Run: []string{"echo", "post"}}}},
}

func buildGateMatrix() []gateCase {
	var cases []gateCase
	add := func(name string, toolHooks int, methodHooks int, method *config.MethodCandidate) {
		tool := &config.Tool{Name: "demo"}
		th := gateHooks[toolHooks]
		tool.PreInstall, tool.PostInstall = th.pre, th.post
		mh := gateHooks[methodHooks]
		m := *method
		m.PreInstall, m.PostInstall = mh.pre, mh.post
		tool.Methods = []*config.MethodCandidate{&m}
		cases = append(cases, gateCase{
			name:   fmt.Sprintf("%s/tool=%s/method=%s", name, th.name, mh.name),
			tool:   tool,
			method: &m,
		})
	}
	hookCombos := func(name string, method *config.MethodCandidate) {
		for toolHooks := range gateHooks {
			for methodHooks := range gateHooks {
				add(name, toolHooks, methodHooks, method)
			}
		}
	}

	for _, contract := range methodkind.Contracts {
		contract := contract
		kind := contract.Kind
		for bi, base := range gateBaseConfigs(&contract) {
			bname := fmt.Sprintf("%s/base%d", kind, bi)
			hookCombos(bname, &config.MethodCandidate{Kind: kind, Config: gateCloneConfig(base, nil)})
			hookCombos(bname+"/unknown-key", &config.MethodCandidate{Kind: kind, Config: gateCloneConfig(base, map[string]any{"no_such_field": "x"})})
			hookCombos(bname+"/builder-error", &config.MethodCandidate{Kind: kind, Config: gateCloneConfig(base, nil), Err: fmt.Errorf("rejected by parser")})
			hookCombos(bname+"/secret", &config.MethodCandidate{Kind: kind, Config: gateCloneConfig(base, nil), SecretRef: &config.SecretReference{Provider: "env", Name: "DEMO_TOKEN"}})
			hookCombos(bname+"/inferred", &config.MethodCandidate{Kind: kind, Config: gateCloneConfig(base, nil), Inferred: true})

			var commandFields []string
			for name, field := range contract.Fields {
				if field.Type == methodkind.Command {
					commandFields = append(commandFields, name)
				}
			}
			sort.Strings(commandFields)
			for _, field := range commandFields {
				for _, form := range gateCommandForms {
					cfg := gateCloneConfig(base, map[string]any{field: form.value})
					hookCombos(fmt.Sprintf("%s/%s=%s", bname, field, form.name), &config.MethodCandidate{Kind: kind, Config: cfg})
					cfg = gateCloneConfig(base, map[string]any{field: form.value, "no_such_field": "x"})
					hookCombos(fmt.Sprintf("%s/%s=%s/unknown-key", bname, field, form.name), &config.MethodCandidate{Kind: kind, Config: cfg})
				}
			}
		}
	}

	// A candidate that plans fine but whose adapter contract cannot honor the
	// request: CheckRequirements fails while the plan stays valid and non-nil.
	// The gate must keep answering from the plan.
	hookCombos("go/tag-unsupported", &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/demo", "tag": "v1"}})
	hookCombos("go/tag-unsupported+command", &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/demo", "tag": "v1", "build": "make"}})

	// Method kinds with no registered contract (programmatic/custom adapters).
	// They carry executor-owned hooks in a minimal plan; tool-level hooks must
	// keep tripping the gate for them.
	for _, kind := range []string{"custom-unregistered", ""} {
		for _, cfg := range []map[string]any{nil, {"build": "make"}, {"anything": "x"}} {
			hookCombos(fmt.Sprintf("unregistered(%q)/cfg=%v", kind, cfg), &config.MethodCandidate{Kind: kind, Config: cfg})
		}
		hookCombos(fmt.Sprintf("unregistered(%q)/builder-error", kind), &config.MethodCandidate{Kind: kind, Err: fmt.Errorf("rejected")})
	}
	return cases
}

func TestMethodRunsArbitraryCodeMatchesLegacyGate(t *testing.T) {
	cases := buildGateMatrix()
	type branch struct{ built, requirementsFailed, buildFailed, unregistered, gated int }
	var seen branch

	for _, tc := range cases {
		got := methodRunsArbitraryCode(tc.tool, tc.method)
		want := legacyMethodRunsArbitraryCode(tc.tool, tc.method)
		if got != want {
			direction := "FAIL-OPEN: new gate blocks less than the legacy gate"
			if got {
				direction = "behavior change: new gate blocks more than the legacy gate"
			}
			t.Errorf("%s: methodRunsArbitraryCode = %v, legacy = %v (%s)", tc.name, got, want, direction)
		}
		if got {
			seen.gated++
		}

		if _, registered := methodkind.Lookup(tc.method.Kind); !registered {
			seen.unregistered++
			continue
		}
		intent, err := planner.BuildCandidateIntent(tc.tool, tc.method)
		if err != nil {
			seen.buildFailed++
			continue
		}
		seen.built++

		// Premise 1: a plan the builder accepted never makes PlanCapabilities
		// fail, so the legacy static fallback after it is unreachable.
		capabilities, err := methodkind.PlanCapabilities(intent)
		if err != nil {
			t.Errorf("%s: PlanCapabilities failed on a plan BuildCandidateIntent accepted: %v", tc.name, err)
			continue
		}
		// Premise 2: the capability bit is exactly the cheap predicate.
		if bit := capabilities&methodkind.CapabilityArbitraryCode != 0; bit != methodkind.PlanRequiresArbitraryCode(intent) {
			t.Errorf("%s: PlanCapabilities bit = %v, PlanRequiresArbitraryCode = %v", tc.name, bit, methodkind.PlanRequiresArbitraryCode(intent))
		}
		// Branch coverage: candidates whose requirements fail still get the
		// plan's answer.
		if _, reqErr := candidatePlanIntentErr(tc.tool, tc.method); reqErr != nil {
			seen.requirementsFailed++
		}
	}

	// The matrix must reach every branch, or it proves nothing about it.
	for name, n := range map[string]int{
		"candidates":                          len(cases),
		"plans built":                         seen.built,
		"plans built but requirements failed": seen.requirementsFailed,
		"builder rejected (static fallback)":  seen.buildFailed,
		"unregistered kinds (generic path)":   seen.unregistered,
		"candidates gated (true)":             seen.gated,
	} {
		if n == 0 {
			t.Errorf("matrix never reached branch %q; the characterization is vacuous for it", name)
		}
	}
	t.Logf("matrix: %d candidates; built=%d requirements-failed=%d builder-rejected=%d unregistered=%d gated=%d",
		len(cases), seen.built, seen.requirementsFailed, seen.buildFailed, seen.unregistered, seen.gated)
}

// Explicit expectations for the cases whose answer matters most for security.
// The equivalence test above proves "same as before"; this proves "and the
// answer is the right one" for the corners the review identified.
func TestMethodRunsArbitraryCodeCorners(t *testing.T) {
	toolHook := []config.Hook{{Run: []string{"echo", "x"}}}
	methodHook := []config.Hook{{Run: []string{"echo", "y"}}}

	for _, tc := range []struct {
		name   string
		tool   *config.Tool
		method *config.MethodCandidate
		want   bool
	}{
		{
			name:   "unregistered kind with a tool-level hook is gated",
			tool:   &config.Tool{Name: "demo", PreInstall: toolHook},
			method: &config.MethodCandidate{Kind: "custom-unregistered"},
			want:   true,
		},
		{
			name:   "unregistered kind with a method-level hook is gated",
			tool:   &config.Tool{Name: "demo"},
			method: &config.MethodCandidate{Kind: "custom-unregistered", PostInstall: methodHook},
			want:   true,
		},
		{
			name:   "unregistered kind with nothing executable is not gated",
			tool:   &config.Tool{Name: "demo"},
			method: &config.MethodCandidate{Kind: "custom-unregistered"},
			want:   false,
		},
		{
			name:   "registered kind with a tool-level hook is gated",
			tool:   &config.Tool{Name: "demo", PostInstall: toolHook},
			method: &config.MethodCandidate{Kind: "aur", Config: map[string]any{"pkg": "foo"}},
			want:   true,
		},
		{
			name:   "registered kind without code is not gated",
			tool:   &config.Tool{Name: "demo"},
			method: &config.MethodCandidate{Kind: "aur", Config: map[string]any{"pkg": "foo"}},
			want:   false,
		},
		{
			name:   "requirements-failing candidate keeps the plan answer (code)",
			tool:   &config.Tool{Name: "demo"},
			method: &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/demo", "tag": "v1"}, PreInstall: methodHook},
			want:   true,
		},
		{
			name:   "requirements-failing candidate keeps the plan answer (no code)",
			tool:   &config.Tool{Name: "demo"},
			method: &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.test/demo", "tag": "v1"}},
			want:   false,
		},
		{
			name:   "unplannable candidate with a command field fails closed",
			tool:   &config.Tool{Name: "demo"},
			method: &config.MethodCandidate{Kind: "git", Config: map[string]any{"url": "https://example.test/demo.git", "build": []any{"make"}, "no_such_field": "x"}},
			want:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := methodRunsArbitraryCode(tc.tool, tc.method); got != tc.want {
				t.Fatalf("methodRunsArbitraryCode = %v, want %v", got, tc.want)
			}
			if got := CandidateRunsArbitraryCode(tc.tool, tc.method); got != tc.want {
				t.Fatalf("CandidateRunsArbitraryCode = %v, want %v", got, tc.want)
			}
		})
	}

	// Pre-conditions of the two requirements-failing cases: they must really
	// be the corner (non-nil intent AND a planner error), or the cases above
	// would silently test something else.
	for _, method := range []*config.MethodCandidate{
		{Kind: "go", Config: map[string]any{"pkg": "example.test/demo", "tag": "v1"}},
	} {
		intent, err := candidatePlanIntentErr(&config.Tool{Name: "demo"}, method)
		if intent == nil || err == nil {
			t.Fatalf("expected a non-nil intent with a planner error for %+v, got intent=%v err=%v", method, intent != nil, err)
		}
	}
}
