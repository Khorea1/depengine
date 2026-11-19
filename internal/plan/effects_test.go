package plan

import (
	"reflect"
	"testing"
)

func TestEffectCompletenessIsConservative(t *testing.T) {
	cases := []struct {
		name string
		in   []EffectCompleteness
		want EffectCompleteness
		err  bool
	}{
		{"missing is unknown", nil, EffectUnknown, false},
		{"only exact", []EffectCompleteness{EffectExact}, EffectExact, false},
		{"bounded limits exact", []EffectCompleteness{EffectExact, EffectBounded}, EffectBounded, false},
		{"unknown limits bounded", []EffectCompleteness{EffectBounded, EffectUnknown}, EffectUnknown, false},
		{"unknown limits exact", []EffectCompleteness{EffectUnknown, EffectExact}, EffectUnknown, false},
		{"invalid is never exact", []EffectCompleteness{"", EffectExact}, "", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CombineEffectCompleteness(tt.in...)
			if (err != nil) != tt.err || got != tt.want {
				t.Fatalf("CombineEffectCompleteness(%q) = %q, %v; want %q, err=%t", tt.in, got, err, tt.want, tt.err)
			}
		})
	}
}

func TestEffectEnvelopeIncludesEveryPotentialMutationPhase(t *testing.T) {
	resolved := New("rust", "http", true)
	resolved.Identity.Source = "https://example.test/download"
	resolved.Prerequisites = []Prerequisite{{Name: "curl"}}
	resolved.Sources = []SourceReference{{Role: SourceRegistry, Name: "main"}}
	resolved.Artifacts = []Artifact{{URL: "https://example.test/download"}}
	resolved.Preparation = &PreparationPlan{
		Probe: []Operation{{Kind: "query", Effect: EffectReadOnly}},
		Prepare: []PreparationMutation{{
			ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "source-key"},
			Ownership: OwnershipDepengine,
			Apply:     Operation{Kind: "prepare", Effect: EffectMutation},
			Rollback:  &Operation{Kind: "rollback", Effect: EffectMutation},
			Policy:    RollbackSafe,
		}},
		Commit: []Operation{{Kind: "commit", Effect: EffectMutation}},
	}
	resolved.SourceMutations = []Operation{{Kind: "add-source", Effect: EffectMutation}}
	resolved.Hooks = []LifecycleHook{
		hook("before", TransitionInstall, HookBefore),
		hook("after", TransitionInstall, HookAfter),
		hook("remove-only", TransitionRemove, HookBefore),
	}
	resolved.Hooks[0].When = &HookCondition{OS: []string{"linux"}}
	resolved.Ensures = []EnsureAction{{
		ID: "ensure", Resource: "host:check", Check: Operation{Kind: "check", Effect: EffectReadOnly},
		Apply: Operation{Kind: "apply", Effect: EffectMutation},
	}}
	resolved.Operations = []Operation{{Kind: "readonly", Effect: EffectReadOnly}, {Kind: "install", Effect: EffectMutation}}
	before := resolved.Clone()

	got, err := DescribeProspectiveEffects(&resolved, TransitionInstall)
	if err != nil {
		t.Fatal(err)
	}
	if got.Completeness != EffectUnknown || got.Tool != "rust" || got.Method != "http" {
		t.Fatalf("unexpected envelope header: %+v", got)
	}
	var phases []string
	for _, step := range got.Steps {
		if step.Completeness != EffectUnknown {
			t.Fatalf("unproved effect classified as %q: %+v", step.Completeness, step)
		}
		phases = append(phases, step.Phase)
	}
	want := []string{"prerequisite", "source", "artifact", "prepare", "rollback", "commit", "source_mutation", "hook_before", "hook_after", "ensure", "operation", "primary_install"}
	if !reflect.DeepEqual(phases, want) {
		t.Fatalf("phases = %q; want %q", phases, want)
	}
	if !got.Steps[7].Conditional || !got.Steps[8].Conditional {
		t.Fatal("when-conditioned and post-transition hooks must remain conditional")
	}
	if !reflect.DeepEqual(resolved, before) {
		t.Fatal("effect planning modified the resolved plan")
	}
}

func TestEffectEnvelopeAlwaysAccountsForOpaqueAdapter(t *testing.T) {
	for _, method := range []string{"http", "native", "github", "cargo", "mise"} {
		p := New("rust", method, true)
		envelope, err := DescribeProspectiveEffects(&p, TransitionInstall)
		if err != nil {
			t.Fatal(err)
		}
		if envelope.Completeness != EffectUnknown || len(envelope.Steps) != 1 || envelope.Steps[0].Phase != "primary_install" {
			t.Fatalf("%s: missing implicit opaque adapter action: %+v", method, envelope)
		}
	}
}

func TestEffectEnvelopeTransitionLocalAndNoLeakage(t *testing.T) {
	p := New("rust", "http", true)
	p.Hooks = []LifecycleHook{
		hook("install", TransitionInstall, HookBefore),
		hook("remove", TransitionRemove, HookAfter),
	}
	got, err := DescribeProspectiveEffects(&p, TransitionRemove)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 2 || got.Steps[0].Phase != "hook_after" || got.Steps[1].Phase != "primary_remove" {
		t.Fatalf("unexpected remove phases: %+v", got.Steps)
	}
}

// Removal does not execute the selected install candidate's prerequisites,
// downloads, preparation or installation operations. The opaque removal and
// matching remove hooks remain unknown until the removal path is modeled.
func TestEffectEnvelopeRemoveDoesNotInheritInstallPhases(t *testing.T) {
	p := New("rust", "http", true)
	p.Prerequisites = []Prerequisite{{Name: "curl"}}
	p.Sources = []SourceReference{{Role: SourceRegistry, Name: "main"}}
	p.Artifacts = []Artifact{{URL: "https://example.test/download"}}
	p.Preparation = &PreparationPlan{
		Prepare: []PreparationMutation{{
			ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "source-key"},
			Ownership: OwnershipDepengine, Apply: Operation{Kind: "prepare", Effect: EffectMutation},
			Rollback: &Operation{Kind: "rollback", Effect: EffectMutation}, Policy: RollbackSafe,
		}},
		Commit: []Operation{{Kind: "commit", Effect: EffectMutation}},
	}
	p.SourceMutations = []Operation{{Kind: "add-source", Effect: EffectMutation}}
	p.Ensures = []EnsureAction{{
		ID: "ensure", Resource: "host:check", Check: Operation{Kind: "check", Effect: EffectReadOnly},
		Apply: Operation{Kind: "apply", Effect: EffectMutation},
	}}
	p.Operations = []Operation{{Kind: "install", Effect: EffectMutation}}
	p.Hooks = []LifecycleHook{
		hook("before-install", TransitionInstall, HookBefore),
		hook("before-remove", TransitionRemove, HookBefore),
	}
	got, err := DescribeProspectiveEffects(&p, TransitionRemove)
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	for _, step := range got.Steps {
		phases = append(phases, step.Phase)
	}
	if want := []string{"hook_before", "primary_remove"}; !reflect.DeepEqual(phases, want) {
		t.Fatalf("remove envelope includes unrelated install phases: got %q, want %q", phases, want)
	}
	if got.Completeness != EffectUnknown {
		t.Fatalf("remove claimed exhaustiveness: %s", got.Completeness)
	}
}

func TestEffectEnvelopeRejectsMalformedOrMissingPlans(t *testing.T) {
	if _, err := DescribeProspectiveEffects(nil, TransitionInstall); err == nil {
		t.Fatal("nil plan accepted")
	}
	p := New("x", "http", true)
	if _, err := DescribeProspectiveEffects(&p, "unsupported"); err == nil {
		t.Fatal("invalid transition accepted")
	}
	p.Operations = []Operation{{Kind: "exec", Effect: EffectMutation, Command: []string{"sh", "-c", "echo bad"}}}
	if _, err := DescribeProspectiveEffects(&p, TransitionInstall); err == nil {
		t.Fatal("invalid command accepted")
	}
}
