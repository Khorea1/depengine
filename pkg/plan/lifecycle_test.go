package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

func hook(id string, transition TransitionKind, timing HookTiming) LifecycleHook {
	return LifecycleHook{
		ID:         id,
		Transition: transition,
		Timing:     timing,
		Operation: Operation{
			Kind:          "hook",
			Effect:        EffectMutation,
			Command:       []string{"sh", "-c", "echo hook"},
			ArbitraryCode: true,
		},
		FailurePolicy: HookFailAbort,
	}
}

func TestHookScheduleIsTransitionLocal(t *testing.T) {
	p := New("tool", "apt", true)
	p.Hooks = []LifecycleHook{
		hook("apt-before-install", TransitionInstall, HookBefore),
		hook("after-install", TransitionInstall, HookAfter),
		hook("before-remove", TransitionRemove, HookBefore),
	}

	got, err := p.HookSchedule(TransitionInstall, HookBefore)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "apt-before-install" {
		t.Fatalf("HookSchedule() = %#v", got)
	}
}

func TestHookRequiresArbitraryMutationCommand(t *testing.T) {
	h := hook("x", TransitionInstall, HookBefore)
	h.Operation.ArbitraryCode = false
	if err := h.Validate(); err == nil {
		t.Fatal("expected non-arbitrary hook to fail")
	}
	h = hook("x", TransitionInstall, HookBefore)
	h.Operation.Effect = EffectReadOnly
	if err := h.Validate(); err == nil {
		t.Fatal("expected read-only hook classification to fail")
	}
}

func TestHookFailureOutcomeNeverImplicitlyRollsBackTransition(t *testing.T) {
	before := hook("before", TransitionInstall, HookBefore)
	got, err := before.FailureOutcome()
	if err != nil {
		t.Fatal(err)
	}
	if !got.AbortLifecycle || got.TransitionCommitted || got.RollbackTransition || !got.ReportFailure {
		t.Fatalf("before outcome = %#v", got)
	}

	after := hook("after", TransitionInstall, HookAfter)
	got, err = after.FailureOutcome()
	if err != nil {
		t.Fatal(err)
	}
	if !got.AbortLifecycle || !got.TransitionCommitted || got.RollbackTransition || !got.ReportFailure {
		t.Fatalf("after outcome = %#v", got)
	}
}

func TestHookContinueReportsWithoutAbort(t *testing.T) {
	h := hook("post", TransitionUpgrade, HookAfter)
	h.FailurePolicy = HookFailContinueReport
	got, err := h.FailureOutcome()
	if err != nil {
		t.Fatal(err)
	}
	if got.AbortLifecycle || !got.TransitionCommitted || !got.ReportFailure || got.RollbackTransition {
		t.Fatalf("outcome = %#v", got)
	}
}

func TestEnsureRequiresExplicitReadOnlyCheck(t *testing.T) {
	e := EnsureAction{
		ID:       "config",
		Resource: "config:tool",
		Check:    Operation{Kind: "check-config", Effect: EffectReadOnly},
		Apply:    Operation{Kind: "write-config", Effect: EffectMutation},
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Check.Effect = EffectMutation
	if err := e.Validate(); err == nil {
		t.Fatal("expected mutating ensure check to fail")
	}
}

func TestEnsureRejectsCredentialResource(t *testing.T) {
	e := EnsureAction{
		ID:       "source",
		Resource: "https://user:secret@example.test/repo",
		Check:    Operation{Kind: "check", Effect: EffectReadOnly},
		Apply:    Operation{Kind: "apply", Effect: EffectMutation},
	}
	if err := e.Validate(); err == nil {
		t.Fatal("expected credential-bearing ensure resource to fail")
	}
}

func TestLifecycleSerializationRedactsSecrets(t *testing.T) {
	p := New("tool", "git", true)
	h := hook("pre", TransitionInstall, HookBefore)
	h.Operation.Command = []string{"tool", "--api-key", "hook-secret"}
	p.Hooks = []LifecycleHook{h}
	p.Ensures = []EnsureAction{{
		ID:       "state",
		Resource: "state:tool",
		Check:    Operation{Kind: "check", Effect: EffectReadOnly, Description: "https://example.test/?token=check-secret"},
		Apply:    Operation{Kind: "apply", Effect: EffectMutation, Command: []string{"tool", "--token", "apply-secret"}},
	}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hook-secret", "check-secret", "apply-secret"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("serialized lifecycle leaked %q: %s", secret, b)
		}
	}
}

func TestDuplicateHookAndEnsureIDsFail(t *testing.T) {
	p := New("tool", "git", true)
	p.Hooks = []LifecycleHook{hook("same", TransitionInstall, HookBefore), hook("same", TransitionInstall, HookAfter)}
	if err := p.Validate(); err == nil {
		t.Fatal("expected duplicate hook id to fail")
	}

	p = New("tool", "git", true)
	e := EnsureAction{ID: "same", Resource: "state:a", Check: Operation{Kind: "check", Effect: EffectReadOnly}, Apply: Operation{Kind: "apply", Effect: EffectMutation}}
	p.Ensures = []EnsureAction{e, e}
	if err := p.Validate(); err == nil {
		t.Fatal("expected duplicate ensure id to fail")
	}
}
