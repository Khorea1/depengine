package plan

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// PreparationPhase names the lifecycle stage of candidate preparation.
// Probe is observational; prepare/commit/rollback may mutate host state.
type PreparationPhase string

const (
	PhaseProbe    PreparationPhase = "probe"
	PhasePrepare  PreparationPhase = "prepare"
	PhaseCommit   PreparationPhase = "commit"
	PhaseRollback PreparationPhase = "rollback"
)

// ResourceKind identifies shared host state created while preparing a
// candidate. The set is intentionally small and adapter-neutral.
type ResourceKind string

const (
	ResourceSource       ResourceKind = "source"
	ResourcePrerequisite ResourceKind = "prerequisite"
	ResourceOther        ResourceKind = "other"
)

// OwnershipKind distinguishes resources created by depengine from resources
// that pre-existed or are managed externally.
type OwnershipKind string

const (
	OwnershipDepengine OwnershipKind = "depengine"
	OwnershipExternal  OwnershipKind = "external"
)

// RollbackPolicy declares what may happen when preparation fails.
type RollbackPolicy string

const (
	// RollbackSafe means depengine can reverse the mutation when it owns the
	// resource and no other committed dependent still claims it.
	RollbackSafe RollbackPolicy = "safe"
	// RollbackRetain means rollback is unsafe or unavailable. The retained
	// mutation must remain visible in state/reporting rather than being silently
	// forgotten.
	RollbackRetain RollbackPolicy = "retain_report"
)

// ResourceIdentity is the stable adapter-neutral key used for ownership and
// refcount tracking. Key must describe semantic identity, not a secret.
type ResourceIdentity struct {
	Kind ResourceKind `json:"kind"`
	Key  string       `json:"key"`
}

// PreparationMutation describes one reversible or explicitly retained host
// mutation performed before candidate commit.
type PreparationMutation struct {
	ID        string           `json:"id"`
	Resource  ResourceIdentity `json:"resource"`
	Ownership OwnershipKind    `json:"ownership"`
	Apply     Operation        `json:"apply"`
	Rollback  *Operation       `json:"rollback,omitempty"`
	Policy    RollbackPolicy   `json:"rollback_policy"`
}

// PreparationPlan separates read-only candidate probing from mutating
// preparation and commit. Mutations are not probes and must not be required to
// merely answer whether a candidate could work.
type PreparationPlan struct {
	Probe   []Operation           `json:"probe,omitempty"`
	Prepare []PreparationMutation `json:"prepare,omitempty"`
	Commit  []Operation           `json:"commit,omitempty"`
}

// Validate enforces the transactional boundary independently of adapters.
func (p PreparationPlan) Validate() error {
	ids := make(map[string]struct{}, len(p.Prepare))
	resources := make(map[ResourceIdentity]string, len(p.Prepare))
	for i, op := range p.Probe {
		if err := op.Validate(); err != nil {
			return fmt.Errorf("probe operation %d: %w", i, err)
		}
		if op.Effect != EffectReadOnly {
			return fmt.Errorf("probe operation %d (%q) must be read-only", i, op.Kind)
		}
	}
	for i, m := range p.Prepare {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("prepare mutation %d: %w", i, err)
		}
		if _, exists := ids[m.ID]; exists {
			return fmt.Errorf("duplicate preparation mutation id %q", m.ID)
		}
		ids[m.ID] = struct{}{}
		if firstID, exists := resources[m.Resource]; exists {
			return fmt.Errorf("preparation mutations %q and %q target the same resource %q/%q", firstID, m.ID, m.Resource.Kind, m.Resource.Key)
		}
		resources[m.Resource] = m.ID
	}
	for i, op := range p.Commit {
		if err := op.Validate(); err != nil {
			return fmt.Errorf("commit operation %d: %w", i, err)
		}
		if op.Effect != EffectMutation {
			return fmt.Errorf("commit operation %d (%q) must be a mutation", i, op.Kind)
		}
	}
	return nil
}

// Validate checks one preparation mutation.
func (m PreparationMutation) Validate() error {
	if strings.TrimSpace(m.ID) != m.ID || m.ID == "" || strings.ContainsRune(m.ID, '\x00') {
		return errors.New("mutation id is required and must not contain surrounding whitespace")
	}
	if err := m.Resource.Validate(); err != nil {
		return fmt.Errorf("resource: %w", err)
	}
	switch m.Ownership {
	case OwnershipDepengine, OwnershipExternal:
	default:
		return fmt.Errorf("invalid ownership %q", m.Ownership)
	}
	if m.Apply.Effect != EffectMutation {
		return errors.New("apply operation must be classified as mutation")
	}
	if err := m.Apply.Validate(); err != nil {
		return fmt.Errorf("apply operation: %w", err)
	}
	switch m.Policy {
	case RollbackSafe:
		if m.Ownership != OwnershipDepengine {
			return errors.New("safe rollback requires depengine-owned resource")
		}
		if m.Rollback == nil {
			return errors.New("safe rollback requires a rollback operation")
		}
		if m.Rollback.Effect != EffectMutation {
			return errors.New("rollback operation must be classified as mutation")
		}
		if err := m.Rollback.Validate(); err != nil {
			return fmt.Errorf("rollback operation: %w", err)
		}
	case RollbackRetain:
		if m.Rollback != nil {
			return errors.New("retain_report mutation must not declare an automatic rollback operation")
		}
	default:
		return fmt.Errorf("invalid rollback policy %q", m.Policy)
	}
	return nil
}

// Validate checks a resource identity.
func (r ResourceIdentity) Validate() error {
	switch r.Kind {
	case ResourceSource, ResourcePrerequisite, ResourceOther:
	default:
		return fmt.Errorf("invalid resource kind %q", r.Kind)
	}
	if strings.TrimSpace(r.Key) != r.Key || r.Key == "" {
		return errors.New("resource key is required and must not contain surrounding whitespace")
	}
	if err := validateCredentialFreeReference(r.Key); err != nil {
		return fmt.Errorf("resource key: %w", err)
	}
	if strings.Contains(r.Key, "://") {
		if canonical := sanitizeLockReference(r.Key); canonical != r.Key {
			return fmt.Errorf("resource key is not canonical; use %q", canonical)
		}
	}
	return nil
}

// RollbackDecision is the pure result of planning rollback for already-applied
// preparation mutations. Operations are returned in reverse application order.
type RollbackDecision struct {
	Operations []Operation        `json:"operations,omitempty"`
	Retained   []ResourceIdentity `json:"retained,omitempty"`
}

// RollbackFor computes rollback for the applied mutation IDs. Unknown IDs are
// rejected so an executor cannot silently lose ownership accounting.
func (p PreparationPlan) RollbackFor(appliedIDs []string) (RollbackDecision, error) {
	if err := p.Validate(); err != nil {
		return RollbackDecision{}, err
	}
	if len(appliedIDs) > len(p.Prepare) {
		return RollbackDecision{}, fmt.Errorf("rollback records %d applied mutations, plan has %d", len(appliedIDs), len(p.Prepare))
	}
	// Prepare mutations are applied strictly in plan order. Rollback therefore
	// accepts only the exact applied prefix that a valid PreparationJournal can
	// represent. Accepting arbitrary subsets/reordering here would allow an
	// executor to roll back a mutation that could not have been applied alone
	// while silently omitting earlier owned state.
	for i, id := range appliedIDs {
		if id != p.Prepare[i].ID {
			return RollbackDecision{}, fmt.Errorf("rollback mutation %d is %q, want applied prefix %q", i, id, p.Prepare[i].ID)
		}
	}
	var out RollbackDecision
	for i := len(appliedIDs) - 1; i >= 0; i-- {
		m := p.Prepare[i]
		if m.Policy == RollbackSafe {
			out.Operations = append(out.Operations, cloneOperation(*m.Rollback))
		} else {
			out.Retained = append(out.Retained, m.Resource)
		}
	}
	return out, nil
}

// OwnedResourceState is persisted ownership/refcount state for a shared host
// resource. Dependents are tool identities, sorted and unique.
type OwnedResourceState struct {
	Resource   ResourceIdentity `json:"resource"`
	Ownership  OwnershipKind    `json:"ownership"`
	Dependents []string         `json:"dependents,omitempty"`
}

// RefCount returns the number of committed dependents.
func (s OwnedResourceState) RefCount() int { return len(s.Dependents) }

// Validate checks ownership state consistency.
func (s OwnedResourceState) Validate() error {
	if err := s.Resource.Validate(); err != nil {
		return err
	}
	switch s.Ownership {
	case OwnershipDepengine, OwnershipExternal:
	default:
		return fmt.Errorf("invalid ownership %q", s.Ownership)
	}
	last := ""
	for i, dep := range s.Dependents {
		if strings.TrimSpace(dep) != dep || dep == "" {
			return fmt.Errorf("dependent %d is empty or has surrounding whitespace", i)
		}
		if strings.ContainsRune(dep, '\x00') {
			return fmt.Errorf("dependent %d contains NUL", i)
		}
		if i > 0 && dep <= last {
			return errors.New("dependents must be sorted and unique")
		}
		last = dep
	}
	return nil
}

// ClaimResource adds one committed dependent without changing ownership.
func ClaimResource(state OwnedResourceState, dependent string) (OwnedResourceState, error) {
	if err := state.Validate(); err != nil {
		return OwnedResourceState{}, err
	}
	if strings.TrimSpace(dependent) != dependent || dependent == "" {
		return OwnedResourceState{}, errors.New("dependent is required and must not contain surrounding whitespace")
	}
	if strings.ContainsRune(dependent, '\x00') {
		return OwnedResourceState{}, errors.New("dependent contains NUL")
	}
	out := state
	out.Dependents = append([]string(nil), state.Dependents...)
	i := sort.SearchStrings(out.Dependents, dependent)
	if i < len(out.Dependents) && out.Dependents[i] == dependent {
		return out, nil
	}
	out.Dependents = append(out.Dependents, "")
	copy(out.Dependents[i+1:], out.Dependents[i:])
	out.Dependents[i] = dependent
	return out, nil
}

// ReleaseResource removes one dependent. removable is true only when the
// resource is depengine-owned and no committed dependents remain.
func ReleaseResource(state OwnedResourceState, dependent string) (next OwnedResourceState, removable bool, err error) {
	if err := state.Validate(); err != nil {
		return OwnedResourceState{}, false, err
	}
	if strings.TrimSpace(dependent) != dependent || dependent == "" {
		return OwnedResourceState{}, false, errors.New("dependent is required and must not contain surrounding whitespace")
	}
	if strings.ContainsRune(dependent, '\x00') {
		return OwnedResourceState{}, false, errors.New("dependent contains NUL")
	}
	out := state
	out.Dependents = append([]string(nil), state.Dependents...)
	i := sort.SearchStrings(out.Dependents, dependent)
	if i >= len(out.Dependents) || out.Dependents[i] != dependent {
		return OwnedResourceState{}, false, fmt.Errorf("dependent %q does not claim resource", dependent)
	}
	out.Dependents = append(out.Dependents[:i], out.Dependents[i+1:]...)
	return out, out.Ownership == OwnershipDepengine && len(out.Dependents) == 0, nil
}

// PreparationStatus is the durable lifecycle state of candidate preparation.
type PreparationStatus string

const (
	PreparationPending    PreparationStatus = "pending"
	PreparationPreparing  PreparationStatus = "preparing"
	PreparationReady      PreparationStatus = "ready"
	PreparationCommitted  PreparationStatus = "committed"
	PreparationRolledBack PreparationStatus = "rolled_back"
)

// PreparationJournal records which preparation mutations were successfully
// applied. It is deliberately data-only so executors can persist it before
// performing subsequent mutations.
type PreparationJournal struct {
	Status  PreparationStatus `json:"status"`
	Applied []string          `json:"applied,omitempty"`
}

// NewPreparationJournal creates the initial lifecycle state.
func NewPreparationJournal() PreparationJournal {
	return PreparationJournal{Status: PreparationPending}
}

// Validate checks that a persisted preparation journal is a valid prefix of
// the current plan and that its lifecycle status agrees with that prefix. This
// prevents crash recovery from silently accepting reordered, duplicated, or
// stale mutation identities.
func (j PreparationJournal) Validate(p PreparationPlan) error {
	if err := p.Validate(); err != nil {
		return err
	}
	status := j.Status
	if status == "" {
		status = PreparationPending
	}
	if len(j.Applied) > len(p.Prepare) {
		return fmt.Errorf("preparation journal records %d mutations, plan has %d", len(j.Applied), len(p.Prepare))
	}
	for i, id := range j.Applied {
		if id != p.Prepare[i].ID {
			return fmt.Errorf("preparation journal mutation %d is %q, want %q", i, id, p.Prepare[i].ID)
		}
	}

	switch status {
	case PreparationPending:
		if len(j.Applied) != 0 {
			return errors.New("pending preparation journal cannot contain applied mutations")
		}
	case PreparationPreparing:
		if len(j.Applied) == 0 || len(j.Applied) >= len(p.Prepare) {
			return errors.New("preparing journal requires a non-empty partial mutation prefix")
		}
	case PreparationReady:
		if len(j.Applied) != len(p.Prepare) {
			return errors.New("ready preparation journal must contain every prepare mutation")
		}
	case PreparationCommitted:
		if len(j.Applied) != len(p.Prepare) {
			return errors.New("committed preparation journal must contain every prepare mutation")
		}
	case PreparationRolledBack:
		// Rollback may follow failure after any valid prefix, including none.
	default:
		return fmt.Errorf("invalid preparation journal status %q", j.Status)
	}
	return nil
}

// RecordApplied advances the journal after one successful prepare mutation.
// Mutations must be recorded in plan order so rollback is deterministic and a
// crash-recovered journal cannot claim a mutation that was skipped.
func (j PreparationJournal) RecordApplied(p PreparationPlan, id string) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status == "" {
		j.Status = PreparationPending
	}
	switch j.Status {
	case PreparationPending, PreparationPreparing:
	default:
		return PreparationJournal{}, fmt.Errorf("cannot record preparation mutation in state %q", j.Status)
	}
	if len(j.Applied) >= len(p.Prepare) {
		return PreparationJournal{}, errors.New("all preparation mutations are already recorded")
	}
	want := p.Prepare[len(j.Applied)].ID
	if id != want {
		return PreparationJournal{}, fmt.Errorf("preparation mutation %q recorded out of order (want %q)", id, want)
	}
	out := j
	out.Applied = append(append([]string(nil), j.Applied...), id)
	if len(out.Applied) == len(p.Prepare) {
		out.Status = PreparationReady
	} else {
		out.Status = PreparationPreparing
	}
	return out, nil
}

// ReadyForCommit reports whether every declared prepare mutation has been
// recorded successfully. A plan with no preparation mutations is immediately
// ready to commit.
func (j PreparationJournal) ReadyForCommit(p PreparationPlan) bool {
	if err := j.Validate(p); err != nil {
		return false
	}
	if len(p.Prepare) == 0 {
		return j.Status == "" || j.Status == PreparationPending || j.Status == PreparationReady
	}
	return j.Status == PreparationReady && len(j.Applied) == len(p.Prepare)
}

// MarkCommitted records that the candidate commit operations completed. After
// this point preparation rollback is no longer automatic; ownership/refcount
// state governs future removal.
func (j PreparationJournal) MarkCommitted(p PreparationPlan) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if !j.ReadyForCommit(p) {
		return PreparationJournal{}, fmt.Errorf("preparation is not ready to commit (state %q)", j.Status)
	}
	out := j
	out.Status = PreparationCommitted
	out.Applied = append([]string(nil), j.Applied...)
	return out, nil
}

// PlanRollback computes rollback for the journaled mutations and marks the
// journal rolled back. A committed candidate must instead use normal
// ownership-aware removal semantics.
func (j PreparationJournal) PlanRollback(p PreparationPlan) (PreparationJournal, RollbackDecision, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, RollbackDecision{}, err
	}
	if j.Status == PreparationCommitted {
		return PreparationJournal{}, RollbackDecision{}, errors.New("committed preparation cannot use candidate rollback")
	}
	if j.Status == PreparationRolledBack {
		return PreparationJournal{}, RollbackDecision{}, errors.New("preparation is already rolled back")
	}
	decision, err := p.RollbackFor(j.Applied)
	if err != nil {
		return PreparationJournal{}, RollbackDecision{}, err
	}
	out := j
	out.Status = PreparationRolledBack
	out.Applied = append([]string(nil), j.Applied...)
	return out, decision, nil
}
