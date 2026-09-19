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
	if strings.TrimSpace(h.ID) != h.ID || h.ID == "" {
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
	if strings.TrimSpace(e.ID) != e.ID || e.ID == "" {
		return errors.New("ensure id is required and must not contain surrounding whitespace")
	}
	if strings.TrimSpace(e.Resource) != e.Resource || e.Resource == "" {
		return errors.New("ensure resource is required and must not contain surrounding whitespace")
	}
	if err := validateCredentialFreeReference(e.Resource); err != nil {
		return fmt.Errorf("ensure resource: %w", err)
	}
	if e.Check.Effect != EffectReadOnly {
		return errors.New("ensure check must be read-only")
	}
	if len(e.Check.Command) == 0 && e.Check.Kind == "" {
		return errors.New("ensure check is required")
	}
	if e.Apply.Effect != EffectMutation {
		return errors.New("ensure apply must be classified as mutation")
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
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
