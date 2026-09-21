package plan

import (
	"encoding/json"
	"errors"
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
		Apply:    Operation{Kind: "apply", Effect: EffectMutation, Command: []string{"tool", "--token", "apply-secret"}, ArbitraryCode: true},
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

func TestLifecycleIDsRejectNUL(t *testing.T) {
	h := hook("hook\x00id", TransitionInstall, HookBefore)
	if err := h.Validate(); err == nil {
		t.Fatal("hook ID containing NUL unexpectedly accepted")
	}

	e := EnsureAction{
		ID:       "ensure\x00id",
		Resource: "state:tool",
		Check:    Operation{Kind: "check", Effect: EffectReadOnly},
		Apply:    Operation{Kind: "apply", Effect: EffectMutation},
	}
	if err := e.Validate(); err == nil {
		t.Fatal("ensure ID containing NUL unexpectedly accepted")
	}
}

func TestLifecycleProjectionsDoNotAliasOperationCommands(t *testing.T) {
	p := New("tool", "git", true)
	p.Hooks = []LifecycleHook{hook("before-install", TransitionInstall, HookBefore)}
	p.Ensures = []EnsureAction{{
		ID:       "config",
		Resource: "state:tool",
		Check:    Operation{Kind: "check", Effect: EffectReadOnly, Command: []string{"check-tool"}, ArbitraryCode: true},
		Apply:    Operation{Kind: "apply", Effect: EffectMutation, Command: []string{"apply-tool"}, ArbitraryCode: true},
	}}

	hooks, err := p.HookSchedule(TransitionInstall, HookBefore)
	if err != nil {
		t.Fatal(err)
	}
	originalHook := p.Hooks[0].Operation.Command[0]
	hooks[0].Operation.Command[0] = "mutated-hook"
	if p.Hooks[0].Operation.Command[0] != originalHook {
		t.Fatal("HookSchedule output aliases plan hook command")
	}

	ensures, err := CanonicalEnsures(p.Ensures)
	if err != nil {
		t.Fatal(err)
	}
	originalCheck := p.Ensures[0].Check.Command[0]
	originalApply := p.Ensures[0].Apply.Command[0]
	ensures[0].Check.Command[0] = "mutated-check"
	ensures[0].Apply.Command[0] = "mutated-apply"
	if p.Ensures[0].Check.Command[0] != originalCheck || p.Ensures[0].Apply.Command[0] != originalApply {
		t.Fatal("CanonicalEnsures output aliases plan ensure commands")
	}
}

func TestEnsureActionRequiresCanonicalURLResource(t *testing.T) {
	ensure := EnsureAction{
		ID:       "repo",
		Resource: "HTTPS://EXAMPLE.TEST/index?z=2&a=1",
		Check:    Operation{Kind: "check", Effect: EffectReadOnly},
		Apply:    Operation{Kind: "apply", Effect: EffectMutation},
	}
	if err := ensure.Validate(); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("Validate() error = %v, want canonical ensure resource rejection", err)
	}
	ensure.Resource = "https://example.test/index?a=1&z=2"
	if err := ensure.Validate(); err != nil {
		t.Fatalf("canonical ensure resource rejected: %v", err)
	}
}

func TestTransitionForVerificationConsumesReconciliationState(t *testing.T) {
	tests := []struct {
		name     string
		result   VerificationResult
		want     TransitionKind
		required bool
		wantErr  bool
	}{
		{
			name:   "satisfied is a no-op",
			result: VerificationResult{State: StateSatisfied},
		},
		{
			name:     "absent installs",
			result:   VerificationResult{State: StateAbsent},
			want:     TransitionInstall,
			required: true,
		},
		{
			name: "drift upgrades",
			result: VerificationResult{
				State:       StateDrifted,
				Observed:    ObservedIdentity{Version: "1"},
				KnownFields: []IdentityField{FieldVersion},
				Drift:       []IdentityDrift{{Field: FieldVersion, Desired: "2", Observed: "1"}},
			},
			want:     TransitionUpgrade,
			required: true,
		},
		{
			name: "unknown fails closed",
			result: VerificationResult{
				State:        StateUnknown,
				Unverifiable: []IdentityField{FieldVersion},
			},
			wantErr: true,
		},
		{
			name:    "broken fails closed",
			result:  VerificationResult{State: StateBroken, Detail: "probe failed"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TransitionForVerification(tt.result)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("TransitionForVerification() = %#v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("TransitionForVerification() error: %v", err)
			}
			if got.Required != tt.required || got.Transition != tt.want || got.State != tt.result.State {
				t.Fatalf("TransitionForVerification() = %#v, want required=%v transition=%q state=%q", got, tt.required, tt.want, tt.result.State)
			}
		})
	}
}

func TestTransitionForVerificationRejectsInvalidResult(t *testing.T) {
	result := VerificationResult{State: StateDrifted}
	if _, err := TransitionForVerification(result); err == nil {
		t.Fatal("TransitionForVerification accepted invalid reconciliation result")
	}
}

func TestReconcileLockedPlanUsesPinnedIdentityForLifecycleDecision(t *testing.T) {
	resolved := New("tool", "native", true)
	resolved.Identity = ResolvedIdentity{
		RequestedVersion: &VersionIntent{Mode: VersionLatest},
		Version:          "2.0.0",
		Source:           "stable",
	}
	doc, err := BuildLockDocument([]ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}

	intent := resolved
	intent.Identity.Version = ""
	got, err := ReconcileLockedPlan(doc, intent, Observation{
		Presence: PresencePresent,
		Identity: ObservedIdentity{
			Version: "1.9.0",
			Source:  "stable",
		},
		KnownFields: []IdentityField{FieldVersion, FieldSource},
	})
	if err != nil {
		t.Fatalf("ReconcileLockedPlan() error: %v", err)
	}
	if got.Plan.Identity.Version != "2.0.0" {
		t.Fatalf("pinned version = %q, want 2.0.0", got.Plan.Identity.Version)
	}
	if got.Verification.State != StateDrifted {
		t.Fatalf("verification state = %q, want %q", got.Verification.State, StateDrifted)
	}
	if len(got.Verification.Drift) != 1 || got.Verification.Drift[0].Desired != "2.0.0" {
		t.Fatalf("verification drift = %#v, want pinned desired version 2.0.0", got.Verification.Drift)
	}
	if !got.Decision.Required || got.Decision.Transition != TransitionUpgrade {
		t.Fatalf("decision = %#v, want required upgrade", got.Decision)
	}
}

func TestReconcileLockedPlanRejectsMutableIntentDriftBeforeLifecycleSelection(t *testing.T) {
	resolved := New("tool", "native", true)
	resolved.Identity = ResolvedIdentity{
		RequestedVersion: &VersionIntent{Mode: VersionLatest},
		Version:          "2.0.0",
		Source:           "stable",
	}
	doc, err := BuildLockDocument([]ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}

	intent := resolved
	intent.Identity.Version = ""
	intent.Identity.Source = "edge"
	got, err := ReconcileLockedPlan(doc, intent, Observation{Presence: PresenceAbsent})
	if !errors.Is(err, ErrLockMismatch) {
		t.Fatalf("ReconcileLockedPlan() error = %v, want ErrLockMismatch", err)
	}
	if got.Decision.Required || got.Decision.Transition != "" || got.Verification.State != "" {
		t.Fatalf("ReconcileLockedPlan() returned lifecycle output after lock mismatch: %#v", got)
	}
}

func TestReconcileLockedPlanFailsClosedButPreservesUnknownVerification(t *testing.T) {
	resolved := New("tool", "native", true)
	resolved.Identity = ResolvedIdentity{
		RequestedVersion: &VersionIntent{Mode: VersionLatest},
		Version:          "2.0.0",
	}
	doc, err := BuildLockDocument([]ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}
	intent := resolved
	intent.Identity.Version = ""

	got, err := ReconcileLockedPlan(doc, intent, Observation{
		Presence: PresenceUnknown,
		Detail:   "manager cannot report installed version",
	})
	if err == nil {
		t.Fatal("ReconcileLockedPlan() accepted unverifiable desired state")
	}
	if got.Plan.Identity.Version != "2.0.0" {
		t.Fatalf("pinned version = %q, want 2.0.0", got.Plan.Identity.Version)
	}
	if got.Verification.State != StateUnknown {
		t.Fatalf("verification state = %q, want %q", got.Verification.State, StateUnknown)
	}
	if got.Decision.Required || got.Decision.Transition != "" {
		t.Fatalf("unknown verification selected mutation: %#v", got.Decision)
	}
}
