package plan

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// TransitionKind identifies a real desired-state transition. Hooks are bound
// to one transition and are never considered evidence that desired state is
// satisfied.
type TransitionKind string

const (
	TransitionInstall TransitionKind = "install"
	TransitionUpgrade TransitionKind = "upgrade"
	TransitionRepair  TransitionKind = "repair"
	TransitionRemove  TransitionKind = "remove"
)

// HookTiming identifies whether a hook runs before or after the primary
// transition operation.
type HookTiming string

const (
	HookBefore HookTiming = "before"
	HookAfter  HookTiming = "after"
)

// HookFailurePolicy controls whether a hook error stops the lifecycle. It does
// not imply compensating rollback of a transition that has already committed.
type HookFailurePolicy string

const (
	HookFailAbort          HookFailurePolicy = "abort"
	HookFailContinueReport HookFailurePolicy = "continue_report"
)

// LifecycleHook is an event handler attached to the already-selected
// candidate. Hook operations are always arbitrary code and conservatively
// classified as mutations because the planner cannot prove a command harmless.
type LifecycleHook struct {
	ID            string            `json:"id"`
	Transition    TransitionKind    `json:"transition"`
	Timing        HookTiming        `json:"timing"`
	Operation     Operation         `json:"operation"`
	FailurePolicy HookFailurePolicy `json:"failure_policy"`
}

// Validate enforces hook lifecycle semantics independent of adapters.
func (h LifecycleHook) Validate() error {
	if strings.TrimSpace(h.ID) != h.ID || h.ID == "" || strings.ContainsRune(h.ID, '\x00') {
		return errors.New("hook id is required and must not contain surrounding whitespace")
	}
	if !h.Transition.Valid() {
		return fmt.Errorf("invalid hook transition %q", h.Transition)
	}
	switch h.Timing {
	case HookBefore, HookAfter:
	default:
		return fmt.Errorf("invalid hook timing %q", h.Timing)
	}
	switch h.FailurePolicy {
	case HookFailAbort, HookFailContinueReport:
	default:
		return fmt.Errorf("invalid hook failure policy %q", h.FailurePolicy)
	}
	if h.Operation.Effect != EffectMutation {
		return errors.New("hook operation must be classified as mutation")
	}
	if err := h.Operation.Validate(); err != nil {
		return fmt.Errorf("hook operation: %w", err)
	}
	if !h.Operation.ArbitraryCode {
		return errors.New("hook operation must be marked arbitrary code")
	}
	if len(h.Operation.Command) == 0 {
		return errors.New("hook operation requires a command")
	}
	return nil
}

// Valid reports whether k is a known transition kind.
func (k TransitionKind) Valid() bool {
	switch k {
	case TransitionInstall, TransitionUpgrade, TransitionRepair, TransitionRemove:
		return true
	default:
		return false
	}
}

// HookSchedule returns hooks for exactly one transition/timing pair. Hooks for
// another candidate or transition cannot leak into the schedule because hooks
// live on the selected ResolvedInstallPlan and matching is exact.
func (p ResolvedInstallPlan) HookSchedule(transition TransitionKind, timing HookTiming) ([]LifecycleHook, error) {
	if !transition.Valid() {
		return nil, fmt.Errorf("invalid transition %q", transition)
	}
	if timing != HookBefore && timing != HookAfter {
		return nil, fmt.Errorf("invalid hook timing %q", timing)
	}
	if err := validateHooks(p.Hooks); err != nil {
		return nil, err
	}
	out := make([]LifecycleHook, 0)
	for _, hook := range p.Hooks {
		if hook.Transition == transition && hook.Timing == timing {
			hook.Operation = cloneOperation(hook.Operation)
			out = append(out, hook)
		}
	}
	return out, nil
}

func validateHooks(hooks []LifecycleHook) error {
	ids := make(map[string]struct{}, len(hooks))
	for i, hook := range hooks {
		if err := hook.Validate(); err != nil {
			return fmt.Errorf("hook %d: %w", i, err)
		}
		if _, exists := ids[hook.ID]; exists {
			return fmt.Errorf("duplicate hook id %q", hook.ID)
		}
		ids[hook.ID] = struct{}{}
	}
	return nil
}

// HookFailureOutcome describes what remains true after a hook failure.
type HookFailureOutcome struct {
	AbortLifecycle      bool `json:"abort_lifecycle"`
	TransitionCommitted bool `json:"transition_committed"`
	RollbackTransition  bool `json:"rollback_transition"`
	ReportFailure       bool `json:"report_failure"`
}

// FailureOutcome makes hook error/rollback semantics explicit. A post-hook
// failure never asks the executor to undo an already committed install/upgrade/
// remove implicitly; adapters may expose a separate explicit recovery action.
func (h LifecycleHook) FailureOutcome() (HookFailureOutcome, error) {
	if err := h.Validate(); err != nil {
		return HookFailureOutcome{}, err
	}
	out := HookFailureOutcome{ReportFailure: true}
	if h.Timing == HookAfter {
		out.TransitionCommitted = true
	}
	if h.FailurePolicy == HookFailAbort {
		out.AbortLifecycle = true
	}
	return out, nil
}

// EnsureAction is durable declarative state implemented by an explicit
// read-only check plus an apply operation. Unlike hooks, successful historical
// execution is never enough to claim the state is satisfied.
type EnsureAction struct {
	ID       string    `json:"id"`
	Resource string    `json:"resource"`
	Check    Operation `json:"check"`
	Apply    Operation `json:"apply"`
}

// Validate enforces the check-before-apply contract for durable ensure state.
func (e EnsureAction) Validate() error {
	if strings.TrimSpace(e.ID) != e.ID || e.ID == "" || strings.ContainsRune(e.ID, '\x00') {
		return errors.New("ensure id is required and must not contain surrounding whitespace")
	}
	if strings.TrimSpace(e.Resource) != e.Resource || e.Resource == "" {
		return errors.New("ensure resource is required and must not contain surrounding whitespace")
	}
	if err := validateCredentialFreeReference(e.Resource); err != nil {
		return fmt.Errorf("ensure resource: %w", err)
	}
	if strings.Contains(e.Resource, "://") {
		if canonical := sanitizeLockReference(e.Resource); canonical != e.Resource {
			return fmt.Errorf("ensure resource is not canonical; use %q", canonical)
		}
	}
	if e.Check.Effect != EffectReadOnly {
		return errors.New("ensure check must be read-only")
	}
	if err := e.Check.Validate(); err != nil {
		return fmt.Errorf("ensure check: %w", err)
	}
	if len(e.Check.Command) == 0 && e.Check.Kind == "" {
		return errors.New("ensure check is required")
	}
	if e.Apply.Effect != EffectMutation {
		return errors.New("ensure apply must be classified as mutation")
	}
	if err := e.Apply.Validate(); err != nil {
		return fmt.Errorf("ensure apply: %w", err)
	}
	if len(e.Apply.Command) == 0 && e.Apply.Kind == "" {
		return errors.New("ensure apply is required")
	}
	return nil
}

func validateEnsures(ensures []EnsureAction) error {
	ids := make(map[string]struct{}, len(ensures))
	resources := make(map[string]struct{}, len(ensures))
	for i, ensure := range ensures {
		if err := ensure.Validate(); err != nil {
			return fmt.Errorf("ensure %d: %w", i, err)
		}
		if _, exists := ids[ensure.ID]; exists {
			return fmt.Errorf("duplicate ensure id %q", ensure.ID)
		}
		ids[ensure.ID] = struct{}{}
		if _, exists := resources[ensure.Resource]; exists {
			return fmt.Errorf("duplicate ensure resource %q", ensure.Resource)
		}
		resources[ensure.Resource] = struct{}{}
	}
	return nil
}

// CanonicalEnsures returns deterministic ensure ordering for diagnostics and
// future state persistence.
func CanonicalEnsures(in []EnsureAction) ([]EnsureAction, error) {
	if err := validateEnsures(in); err != nil {
		return nil, err
	}
	out := append([]EnsureAction(nil), in...)
	for i := range out {
		out[i].Check = cloneOperation(out[i].Check)
		out[i].Apply = cloneOperation(out[i].Apply)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ReconciliationDecision is the adapter-neutral lifecycle decision derived
// from a validated verification result. Required is false only when desired
// state is already satisfied. Unknown or broken verification never produces a
// mutating transition because doing so would turn missing evidence into an
// implicit install/upgrade policy.
type ReconciliationDecision struct {
	Required   bool              `json:"required"`
	Transition TransitionKind    `json:"transition,omitempty"`
	State      VerificationState `json:"state"`
}

// LockedReconciliation is the immutable desired-state handoff for lifecycle
// execution. Plan is hydrated from the validated lock before Verification and
// Decision are derived, so a mutable manifest intent cannot silently select a
// newer version/source while install or upgrade is deciding what to do.
//
// When reconciliation is unknown or broken, ReconcileLockedPlan returns this
// value with Plan and Verification populated alongside an error; Decision stays
// zero so callers can report the probe result without treating it as permission
// to mutate the host.
type LockedReconciliation struct {
	Plan         ResolvedInstallPlan    `json:"plan"`
	Verification VerificationResult     `json:"verification"`
	Decision     ReconciliationDecision `json:"decision"`
}

// ReconcileLockedPlan composes the universal lock boundary with desired-state
// verification and lifecycle selection. It performs no host I/O. The immutable
// identity is materialized first; the observation is then compared against that
// pinned identity, and only a validated reconciliation result may select an
// install/upgrade transition.
func ReconcileLockedPlan(doc LockDocument, intent ResolvedInstallPlan, observation Observation) (LockedReconciliation, error) {
	pinned, err := doc.PinnedPlanFor(intent)
	if err != nil {
		return LockedReconciliation{}, fmt.Errorf("locked reconciliation: %w", err)
	}

	verification := Reconcile(pinned.Identity, observation)
	out := LockedReconciliation{Plan: pinned, Verification: verification}
	decision, err := TransitionForVerification(verification)
	if err != nil {
		return out, fmt.Errorf("locked reconciliation: %w", err)
	}
	out.Decision = decision
	return out, nil
}

// TransitionForVerification converts desired-state reconciliation into a
// lifecycle decision without re-resolving manifest intent. Absent state needs
// installation; concrete identity drift needs upgrade; satisfied state is a
// no-op. Unknown and broken results fail closed until the caller can obtain an
// authoritative observation or explicitly choose a recovery policy.
func TransitionForVerification(result VerificationResult) (ReconciliationDecision, error) {
	if err := result.Validate(); err != nil {
		return ReconciliationDecision{}, fmt.Errorf("verification result: %w", err)
	}

	decision := ReconciliationDecision{State: result.State}
	switch result.State {
	case StateSatisfied:
		return decision, nil
	case StateAbsent:
		decision.Required = true
		decision.Transition = TransitionInstall
		return decision, nil
	case StateDrifted:
		decision.Required = true
		decision.Transition = TransitionUpgrade
		return decision, nil
	case StateUnknown:
		return ReconciliationDecision{}, fmt.Errorf("verification state %q cannot select a mutating transition: desired state is unverifiable", result.State)
	case StateBroken:
		return ReconciliationDecision{}, fmt.Errorf("verification state %q cannot select a mutating transition: verification probe failed", result.State)
	default:
		// result.Validate already rejects this, but retain a fail-closed default
		// so this boundary stays safe if VerificationState grows in the future.
		return ReconciliationDecision{}, fmt.Errorf("verification state %q cannot select a lifecycle transition", result.State)
	}
}
