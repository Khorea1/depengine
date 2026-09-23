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

const prerequisiteToolResourcePrefix = "tool:"

// PrerequisiteResource identifies a schema tool used as a candidate-specific
// prerequisite. The tool: namespace keeps prerequisite keys distinct from any
// future package/path prerequisite identities while remaining credential-free
// and stable across hosts.
func PrerequisiteResource(toolName string) (ResourceIdentity, error) {
	if strings.TrimSpace(toolName) != toolName || toolName == "" || strings.ContainsRune(toolName, '\x00') {
		return ResourceIdentity{}, errors.New("prerequisite tool name is required and must not contain surrounding whitespace or NUL")
	}
	resource := ResourceIdentity{Kind: ResourcePrerequisite, Key: prerequisiteToolResourcePrefix + toolName}
	if err := resource.Validate(); err != nil {
		return ResourceIdentity{}, fmt.Errorf("prerequisite tool resource: %w", err)
	}
	return resource, nil
}

// PrerequisiteToolName decodes a tool-backed prerequisite identity. It rejects
// other resource kinds/namespaces rather than guessing how they should be
// cleaned up.
func PrerequisiteToolName(resource ResourceIdentity) (string, error) {
	if err := resource.Validate(); err != nil {
		return "", err
	}
	if resource.Kind != ResourcePrerequisite || !strings.HasPrefix(resource.Key, prerequisiteToolResourcePrefix) {
		return "", fmt.Errorf("resource %q/%q is not a tool prerequisite", resource.Kind, resource.Key)
	}
	name := strings.TrimPrefix(resource.Key, prerequisiteToolResourcePrefix)
	if strings.TrimSpace(name) != name || name == "" || strings.ContainsRune(name, '\x00') {
		return "", errors.New("prerequisite tool resource has invalid tool name")
	}
	return name, nil
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
// preparation mutations. OperationIDs align 1:1 with Operations, which are
// returned in reverse application order so executors can journal each step.
type RollbackDecision struct {
	OperationIDs []string           `json:"operation_ids,omitempty"`
	Operations   []Operation        `json:"operations,omitempty"`
	Retained     []ResourceIdentity `json:"retained,omitempty"`
}

// rollbackStep binds a compensating operation to the preparation mutation it
// reverses. The ID is journaled so crash recovery can resume only operations
// that were not already durably confirmed.
type rollbackStep struct {
	ID        string
	Operation Operation
}

func (p PreparationPlan) rollbackStepsFor(appliedIDs []string, owned []OwnedResourceState) ([]rollbackStep, []ResourceIdentity, error) {
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	ownedByResource, err := ownedResourceSnapshot(owned)
	if err != nil {
		return nil, nil, err
	}
	if len(appliedIDs) > len(p.Prepare) {
		return nil, nil, fmt.Errorf("rollback records %d applied mutations, plan has %d", len(appliedIDs), len(p.Prepare))
	}
	for i, id := range appliedIDs {
		if id != p.Prepare[i].ID {
			return nil, nil, fmt.Errorf("rollback mutation %d is %q, want applied prefix %q", i, id, p.Prepare[i].ID)
		}
	}
	var steps []rollbackStep
	var retained []ResourceIdentity
	for i := len(appliedIDs) - 1; i >= 0; i-- {
		m := p.Prepare[i]
		if m.Policy != RollbackSafe {
			retained = append(retained, m.Resource)
			continue
		}
		if state, ok := ownedByResource[m.Resource]; ok {
			if state.Ownership != m.Ownership {
				return nil, nil, fmt.Errorf("rollback resource %q/%q ownership changed from %q to %q", m.Resource.Kind, m.Resource.Key, m.Ownership, state.Ownership)
			}
			if state.RefCount() > 0 {
				retained = append(retained, m.Resource)
				continue
			}
		}
		steps = append(steps, rollbackStep{ID: m.ID, Operation: cloneOperation(*m.Rollback)})
	}
	return steps, retained, nil
}

// RollbackFor computes rollback for the applied mutation IDs using a complete
// snapshot of already-committed resource ownership. Unknown IDs and malformed
// ownership state are rejected so an executor cannot silently lose ownership
// accounting. A safe rollback is emitted only when the matching resource has
// no committed dependents; otherwise the resource is retained and reported.
//
// The ownership snapshot describes state that existed before the candidate
// being rolled back committed. Absence from the snapshot therefore means the
// prepare mutation created an otherwise-unclaimed resource and it may be
// reversed according to its rollback policy.
func (p PreparationPlan) RollbackFor(appliedIDs []string, owned []OwnedResourceState) (RollbackDecision, error) {
	steps, retained, err := p.rollbackStepsFor(appliedIDs, owned)
	if err != nil {
		return RollbackDecision{}, err
	}
	out := RollbackDecision{Retained: append([]ResourceIdentity(nil), retained...)}
	for _, step := range steps {
		out.OperationIDs = append(out.OperationIDs, step.ID)
		out.Operations = append(out.Operations, cloneOperation(step.Operation))
	}
	return out, nil
}

func ownedResourceSnapshot(states []OwnedResourceState) (map[ResourceIdentity]OwnedResourceState, error) {
	out := make(map[ResourceIdentity]OwnedResourceState, len(states))
	for i, state := range states {
		if err := state.Validate(); err != nil {
			return nil, fmt.Errorf("owned resource %d: %w", i, err)
		}
		if _, exists := out[state.Resource]; exists {
			return nil, fmt.Errorf("duplicate owned resource state %q/%q", state.Resource.Kind, state.Resource.Key)
		}
		copyState := state
		copyState.Dependents = append([]string(nil), state.Dependents...)
		out[state.Resource] = copyState
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

// ValidateOwnedResourceSnapshot validates the complete persisted ownership
// snapshot. Individual resource states must already be canonical, and the
// snapshot itself must be sorted by semantic resource identity with no
// duplicates. Requiring canonical order keeps state checksums/snapshots
// deterministic and prevents two records from claiming the same host resource.
func ValidateOwnedResourceSnapshot(states []OwnedResourceState) error {
	var previous ResourceIdentity
	for i, state := range states {
		if err := state.Validate(); err != nil {
			return fmt.Errorf("owned resource %d: %w", i, err)
		}
		if i > 0 {
			if state.Resource.Kind < previous.Kind ||
				(state.Resource.Kind == previous.Kind && state.Resource.Key <= previous.Key) {
				return errors.New("owned resources must be sorted and unique")
			}
		}
		previous = state.Resource
	}
	return nil
}

// ResourceUse is a runtime observation that a committed tool uses one shared
// host resource. Created is true only when depengine created/re-created the
// resource during this transaction; otherwise a newly observed resource is
// classified as external. It is intentionally not persisted as transaction
// state: ownership/refcounts are the durable projection.
type ResourceUse struct {
	Resource ResourceIdentity `json:"resource"`
	Created  bool             `json:"created"`
}

// ClaimResourceUses projects runtime resource observations into the canonical
// ownership/refcount snapshot for one committed dependent. Existing ownership
// is preserved when the resource merely pre-existed. If a previously external
// resource disappeared and depengine had to recreate it, ownership transitions
// to depengine while preserving all existing dependent claims.
func ClaimResourceUses(states []OwnedResourceState, dependent string, uses []ResourceUse) ([]OwnedResourceState, error) {
	if err := validateDependent(dependent); err != nil {
		return nil, err
	}
	snapshot, err := ownedResourceSnapshot(states)
	if err != nil {
		return nil, err
	}
	seen := make(map[ResourceIdentity]struct{}, len(uses))
	for i, use := range uses {
		if err := use.Resource.Validate(); err != nil {
			return nil, fmt.Errorf("resource use %d: %w", i, err)
		}
		if _, duplicate := seen[use.Resource]; duplicate {
			return nil, fmt.Errorf("duplicate resource use %q/%q", use.Resource.Kind, use.Resource.Key)
		}
		seen[use.Resource] = struct{}{}

		state, exists := snapshot[use.Resource]
		if !exists {
			ownership := OwnershipExternal
			if use.Created {
				ownership = OwnershipDepengine
			}
			state = OwnedResourceState{Resource: use.Resource, Ownership: ownership}
		} else if use.Created && state.Ownership == OwnershipExternal {
			state.Ownership = OwnershipDepengine
		}
		state, err = ClaimResource(state, dependent)
		if err != nil {
			return nil, fmt.Errorf("claim resource use %q/%q: %w", use.Resource.Kind, use.Resource.Key, err)
		}
		snapshot[use.Resource] = state
	}
	return sortedOwnedResourceStates(snapshot), nil
}

// ClaimResource adds one committed dependent without changing ownership.
func ClaimResource(state OwnedResourceState, dependent string) (OwnedResourceState, error) {
	if err := state.Validate(); err != nil {
		return OwnedResourceState{}, err
	}
	if err := validateDependent(dependent); err != nil {
		return OwnedResourceState{}, err
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
	if err := validateDependent(dependent); err != nil {
		return OwnedResourceState{}, false, err
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

func validateDependent(dependent string) error {
	if strings.TrimSpace(dependent) != dependent || dependent == "" {
		return errors.New("dependent is required and must not contain surrounding whitespace")
	}
	if strings.ContainsRune(dependent, '\x00') {
		return errors.New("dependent contains NUL")
	}
	return nil
}

func sortedOwnedResourceStates(states map[ResourceIdentity]OwnedResourceState) []OwnedResourceState {
	if len(states) == 0 {
		return nil
	}
	out := make([]OwnedResourceState, 0, len(states))
	for _, state := range states {
		copyState := state
		copyState.Dependents = append([]string(nil), state.Dependents...)
		out = append(out, copyState)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Resource.Kind != out[j].Resource.Kind {
			return out[i].Resource.Kind < out[j].Resource.Kind
		}
		return out[i].Resource.Key < out[j].Resource.Key
	})
	return out
}

// ResourceReleaseDecision is the pure ownership-state transition used by
// remove. Updated retains zero-ref states until the corresponding host cleanup
// has succeeded; Removable identifies only depengine-owned resources whose last
// committed dependent was released.
type ResourceReleaseDecision struct {
	Updated   []OwnedResourceState `json:"updated,omitempty"`
	Removable []ResourceIdentity   `json:"removable,omitempty"`
}

// ReleaseDependentResources removes one tool's claims from a complete
// ownership snapshot. Resources not claimed by dependent are preserved.
// Depengine-owned resources that reach refcount zero are reported as removable;
// external resources are retained even at zero references.
func ReleaseDependentResources(states []OwnedResourceState, dependent string) (ResourceReleaseDecision, error) {
	if err := validateDependent(dependent); err != nil {
		return ResourceReleaseDecision{}, err
	}
	snapshot, err := ownedResourceSnapshot(states)
	if err != nil {
		return ResourceReleaseDecision{}, err
	}
	var removable []ResourceIdentity
	for resource, state := range snapshot {
		i := sort.SearchStrings(state.Dependents, dependent)
		if i >= len(state.Dependents) || state.Dependents[i] != dependent {
			continue
		}
		next, canRemove, err := ReleaseResource(state, dependent)
		if err != nil {
			return ResourceReleaseDecision{}, err
		}
		snapshot[resource] = next
		if canRemove {
			removable = append(removable, resource)
		}
	}
	sort.Slice(removable, func(i, j int) bool {
		if removable[i].Kind != removable[j].Kind {
			return removable[i].Kind < removable[j].Kind
		}
		return removable[i].Key < removable[j].Key
	})
	return ResourceReleaseDecision{
		Updated:   sortedOwnedResourceStates(snapshot),
		Removable: removable,
	}, nil
}

// FinalizeReleasedResource removes one ownership record only after the caller
// has successfully removed the corresponding host resource. The resource must
// be part of the exact removable set produced by ReleaseDependentResources and
// must still be depengine-owned with zero references. This gives type-specific
// cleanup code a small fail-closed state transition without requiring it to
// synthesize a preparation plan.
func FinalizeReleasedResource(release ResourceReleaseDecision, resource ResourceIdentity) ([]OwnedResourceState, error) {
	if err := resource.Validate(); err != nil {
		return nil, fmt.Errorf("released resource: %w", err)
	}
	snapshot, err := ownedResourceSnapshot(release.Updated)
	if err != nil {
		return nil, err
	}
	removable := false
	for _, candidate := range release.Removable {
		if err := candidate.Validate(); err != nil {
			return nil, fmt.Errorf("removable resource: %w", err)
		}
		if candidate == resource {
			removable = true
		}
	}
	if !removable {
		return nil, fmt.Errorf("resource %q/%q is not approved for cleanup", resource.Kind, resource.Key)
	}
	state, exists := snapshot[resource]
	if !exists {
		return nil, fmt.Errorf("resource %q/%q is absent from released ownership state", resource.Kind, resource.Key)
	}
	if state.Ownership != OwnershipDepengine || state.RefCount() != 0 {
		return nil, fmt.Errorf("resource %q/%q is not a zero-ref depengine-owned resource", resource.Kind, resource.Key)
	}
	delete(snapshot, resource)
	return sortedOwnedResourceStates(snapshot), nil
}

// ResourceCleanupDecision translates zero-reference depengine-owned resources
// into concrete cleanup operations using the preparation semantics that created
// them. Resources without an automatic rollback operation remain explicit in
// Retained and must not be silently dropped from ownership state.
type ResourceCleanupDecision struct {
	Operations []Operation        `json:"operations,omitempty"`
	Cleaned    []ResourceIdentity `json:"cleaned,omitempty"`
	Retained   []ResourceIdentity `json:"retained,omitempty"`
}

// CleanupReleasedResources plans cleanup after ReleaseDependentResources. It
// never mutates the supplied ownership snapshot. Cleanup operations are emitted
// in reverse preparation order, matching rollback ordering. Every removable
// resource must still be present as a depengine-owned zero-ref state and must
// be represented by the current preparation plan; otherwise cleanup fails
// closed instead of losing ownership accounting.
func (p PreparationPlan) CleanupReleasedResources(release ResourceReleaseDecision) (ResourceCleanupDecision, error) {
	if err := p.Validate(); err != nil {
		return ResourceCleanupDecision{}, err
	}
	snapshot, err := ownedResourceSnapshot(release.Updated)
	if err != nil {
		return ResourceCleanupDecision{}, err
	}
	wanted := make(map[ResourceIdentity]struct{}, len(release.Removable))
	for i, resource := range release.Removable {
		if err := resource.Validate(); err != nil {
			return ResourceCleanupDecision{}, fmt.Errorf("removable resource %d: %w", i, err)
		}
		if _, duplicate := wanted[resource]; duplicate {
			return ResourceCleanupDecision{}, fmt.Errorf("duplicate removable resource %q/%q", resource.Kind, resource.Key)
		}
		state, ok := snapshot[resource]
		if !ok {
			return ResourceCleanupDecision{}, fmt.Errorf("removable resource %q/%q is absent from updated ownership state", resource.Kind, resource.Key)
		}
		if state.Ownership != OwnershipDepengine || state.RefCount() != 0 {
			return ResourceCleanupDecision{}, fmt.Errorf("removable resource %q/%q must be depengine-owned with zero dependents", resource.Kind, resource.Key)
		}
		wanted[resource] = struct{}{}
	}

	var out ResourceCleanupDecision
	for i := len(p.Prepare) - 1; i >= 0; i-- {
		mutation := p.Prepare[i]
		if _, ok := wanted[mutation.Resource]; !ok {
			continue
		}
		delete(wanted, mutation.Resource)
		if mutation.Ownership != OwnershipDepengine {
			return ResourceCleanupDecision{}, fmt.Errorf("cleanup resource %q/%q is not depengine-owned in preparation plan", mutation.Resource.Kind, mutation.Resource.Key)
		}
		if mutation.Policy == RollbackSafe {
			out.Operations = append(out.Operations, cloneOperation(*mutation.Rollback))
			out.Cleaned = append(out.Cleaned, mutation.Resource)
		} else {
			out.Retained = append(out.Retained, mutation.Resource)
		}
	}
	if len(wanted) != 0 {
		for resource := range wanted {
			return ResourceCleanupDecision{}, fmt.Errorf("removable resource %q/%q is not represented by preparation plan", resource.Kind, resource.Key)
		}
	}
	return out, nil
}

// FinalizeReleasedResourceCleanup removes resources only after the cleanup
// operations planned by CleanupReleasedResources completed successfully. The
// decision is re-derived from the plan before state is changed, so a caller
// cannot relabel retain_report state as cleaned and silently discard it.
// Retained zero-ref resources remain visible for reporting or later adoption.
func (p PreparationPlan) FinalizeReleasedResourceCleanup(release ResourceReleaseDecision, cleanup ResourceCleanupDecision) ([]OwnedResourceState, error) {
	expected, err := p.CleanupReleasedResources(release)
	if err != nil {
		return nil, err
	}
	if !sameOperationSequence(cleanup.Operations, expected.Operations) ||
		!sameResourceSequence(cleanup.Cleaned, expected.Cleaned) ||
		!sameResourceSequence(cleanup.Retained, expected.Retained) {
		return nil, errors.New("cleanup decision does not match preparation plan")
	}
	snapshot, err := ownedResourceSnapshot(release.Updated)
	if err != nil {
		return nil, err
	}
	for _, resource := range expected.Cleaned {
		delete(snapshot, resource)
	}
	return sortedOwnedResourceStates(snapshot), nil
}

func sameOperationSequence(a, b []Operation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind ||
			a[i].Description != b[i].Description ||
			a[i].Effect != b[i].Effect ||
			a[i].ArbitraryCode != b[i].ArbitraryCode ||
			!sameStringSequence(a[i].Command, b[i].Command) {
			return false
		}
	}
	return true
}

func sameStringSequence(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sameResourceSequence(a, b []ResourceIdentity) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// PreparationRecoveryAction is the fail-closed action selected for a durable
// preparation journal after process restart. Recovery decisions never execute
// host mutations; they only tell the executor which already-declared transition
// is safe to resume or whether commit outcome must first be reconciled.
type PreparationRecoveryAction string

const (
	RecoveryRollbackPreparation PreparationRecoveryAction = "rollback_preparation"
	RecoveryReconcileCommit     PreparationRecoveryAction = "reconcile_commit"
	RecoveryFinalizeCommit      PreparationRecoveryAction = "finalize_commit"
	RecoveryResumeRollback      PreparationRecoveryAction = "resume_rollback"
	RecoveryBlocked             PreparationRecoveryAction = "blocked"
)

// PreparationRecoveryDecision is the pure crash-recovery result for one active
// preparation journal. Rollback is populated only for rollback/resume actions
// and contains only compensation still not durably confirmed. Executors must
// still journal each returned operation through PlanRollbackApply before running it.
// Verification is populated only when a committing journal was reconciled
// against an authoritative desired identity and host observation.
type PreparationRecoveryDecision struct {
	Action       PreparationRecoveryAction `json:"action"`
	Rollback     RollbackDecision          `json:"rollback,omitempty"`
	Verification *VerificationResult       `json:"verification,omitempty"`
	Detail       string                    `json:"detail,omitempty"`
}

// RecoveryFor derives the only safe next step for a persisted active journal.
// Preparation that never entered commit can be rolled back from the exact
// applied prefix. An in-progress rollback resumes the same deterministic
// rollback decision. A committing journal is ambiguous until the caller probes
// the host against the same resolved identity that was being committed.
//
// Callers may pass desired+observation only for a committing journal. Passing
// neither returns RecoveryReconcileCommit. When both are supplied, only a
// fully satisfied reconciliation permits RecoveryFinalizeCommit; absent,
// drifted, unknown, or broken state remains blocked because commit operations
// may have partially mutated the host even when the final target is not healthy.
func (j PreparationJournal) RecoveryFor(p PreparationPlan, owned []OwnedResourceState, desired *ResolvedIdentity, observation *Observation) (PreparationRecoveryDecision, error) {
	if err := j.Validate(p); err != nil {
		return PreparationRecoveryDecision{}, err
	}
	if _, err := ownedResourceSnapshot(owned); err != nil {
		return PreparationRecoveryDecision{}, err
	}
	if (desired == nil) != (observation == nil) {
		return PreparationRecoveryDecision{}, errors.New("recovery reconciliation requires both desired identity and observation")
	}

	status := j.Status
	if status == "" {
		status = PreparationPending
	}
	switch status {
	case PreparationPending, PreparationPreparing, PreparationReady:
		if desired != nil {
			return PreparationRecoveryDecision{}, errors.New("commit reconciliation is only valid for a committing preparation journal")
		}
		rollback, err := p.RollbackFor(j.Applied, owned)
		if err != nil {
			return PreparationRecoveryDecision{}, err
		}
		return PreparationRecoveryDecision{Action: RecoveryRollbackPreparation, Rollback: rollback}, nil

	case PreparationApplying:
		if desired != nil {
			return PreparationRecoveryDecision{}, errors.New("commit reconciliation is only valid for a committing preparation journal")
		}
		return PreparationRecoveryDecision{
			Action: RecoveryBlocked,
			Detail: fmt.Sprintf("preparation mutation %q has an ambiguous host-side outcome; establish explicit applied/not_applied evidence before recovery", j.Applying),
		}, nil

	case PreparationCommitting:
		if desired == nil {
			return PreparationRecoveryDecision{
				Action: RecoveryReconcileCommit,
				Detail: "commit outcome is ambiguous; reconcile the desired identity against current host state before recovery",
			}, nil
		}
		verification := Reconcile(*desired, *observation)
		if err := verification.Validate(); err != nil {
			return PreparationRecoveryDecision{}, fmt.Errorf("commit recovery verification: %w", err)
		}
		out := PreparationRecoveryDecision{Verification: &verification}
		if verification.State == StateSatisfied {
			out.Action = RecoveryFinalizeCommit
			out.Detail = "desired state is satisfied; finalize commit ownership without replaying commit operations"
			return out, nil
		}
		out.Action = RecoveryBlocked
		out.Detail = fmt.Sprintf("commit outcome remains unsafe to infer from verification state %q; do not roll back preparation automatically; if independent evidence proves no commit operation took effect, resolve the commit as not applied first", verification.State)
		return out, nil

	case PreparationRollingBack:
		if desired != nil {
			return PreparationRecoveryDecision{}, errors.New("commit reconciliation is only valid for a committing preparation journal")
		}
		if j.RollbackApplying != "" {
			return PreparationRecoveryDecision{
				Action: RecoveryBlocked,
				Detail: fmt.Sprintf("rollback mutation %q has an ambiguous host-side outcome; establish explicit applied/not_applied evidence before recovery", j.RollbackApplying),
			}, nil
		}
		rollback, err := j.remainingRollback(p, owned)
		if err != nil {
			return PreparationRecoveryDecision{}, err
		}
		return PreparationRecoveryDecision{Action: RecoveryResumeRollback, Rollback: rollback}, nil

	case PreparationCommitted, PreparationRolledBack:
		return PreparationRecoveryDecision{}, fmt.Errorf("terminal preparation journal state %q must not require crash recovery", status)
	default:
		return PreparationRecoveryDecision{}, fmt.Errorf("invalid preparation journal status %q", status)
	}
}

// PreparationStatus is the durable lifecycle state of candidate preparation.
type PreparationStatus string

const (
	PreparationPending     PreparationStatus = "pending"
	PreparationApplying    PreparationStatus = "applying"
	PreparationPreparing   PreparationStatus = "preparing"
	PreparationReady       PreparationStatus = "ready"
	PreparationCommitting  PreparationStatus = "committing"
	PreparationCommitted   PreparationStatus = "committed"
	PreparationRollingBack PreparationStatus = "rolling_back"
	PreparationRolledBack  PreparationStatus = "rolled_back"
)

// PreparationMutationOutcome is externally established evidence for the one
// prepare mutation left ambiguous by a crash after its write-ahead boundary.
// The journal never guesses this outcome: callers must derive it from an
// authoritative, read-only probe or explicit operator recovery decision.
type PreparationMutationOutcome string

const (
	PreparationMutationApplied    PreparationMutationOutcome = "applied"
	PreparationMutationNotApplied PreparationMutationOutcome = "not_applied"
)

// PreparationJournal records both the successfully applied preparation prefix
// and, when present, the single mutation whose host-side outcome is in flight.
// It is deliberately data-only so executors can persist write-ahead boundaries
// before performing subsequent mutations.
type PreparationJournal struct {
	Status           PreparationStatus `json:"status"`
	Applied          []string          `json:"applied,omitempty"`
	Applying         string            `json:"applying,omitempty"`
	RollbackApplied  []string          `json:"rollback_applied,omitempty"`
	RollbackApplying string            `json:"rollback_applying,omitempty"`
}

// NewPreparationJournal creates the initial lifecycle state.
func NewPreparationJournal() PreparationJournal {
	return PreparationJournal{Status: PreparationPending}
}

// ValidateShape checks invariants that can be verified without a
// PreparationPlan. Persistent state should validate a journal against its
// durably stored exact plan with Validate; this helper remains useful for
// standalone decoding/diagnostics before a plan is available.
func (j PreparationJournal) ValidateShape() error {
	status := j.Status
	if status == "" {
		status = PreparationPending
	}

	seen := make(map[string]struct{}, len(j.Applied))
	for i, id := range j.Applied {
		if strings.TrimSpace(id) != id || id == "" || strings.ContainsRune(id, '\x00') {
			return fmt.Errorf("preparation journal mutation %d has invalid id", i)
		}
		if _, exists := seen[id]; exists {
			return errors.New("preparation journal contains duplicate mutation id")
		}
		seen[id] = struct{}{}
	}
	if j.Applying != "" {
		if strings.TrimSpace(j.Applying) != j.Applying || strings.ContainsRune(j.Applying, '\x00') {
			return errors.New("preparation journal applying mutation has invalid id")
		}
		if _, exists := seen[j.Applying]; exists {
			return errors.New("preparation journal applying mutation is already recorded as applied")
		}
	}
	rollbackSeen := make(map[string]struct{}, len(j.RollbackApplied))
	for i, id := range j.RollbackApplied {
		if strings.TrimSpace(id) != id || id == "" || strings.ContainsRune(id, '\x00') {
			return fmt.Errorf("preparation journal rollback mutation %d has invalid id", i)
		}
		if _, exists := rollbackSeen[id]; exists {
			return errors.New("preparation journal contains duplicate rollback mutation id")
		}
		rollbackSeen[id] = struct{}{}
	}
	if j.RollbackApplying != "" {
		if strings.TrimSpace(j.RollbackApplying) != j.RollbackApplying || strings.ContainsRune(j.RollbackApplying, '\x00') {
			return errors.New("preparation journal rollback applying mutation has invalid id")
		}
		if _, exists := rollbackSeen[j.RollbackApplying]; exists {
			return errors.New("preparation journal rollback applying mutation is already recorded as applied")
		}
	}

	if status != PreparationRollingBack && status != PreparationRolledBack && (len(j.RollbackApplied) != 0 || j.RollbackApplying != "") {
		return fmt.Errorf("%s preparation journal cannot contain rollback progress", status)
	}

	switch status {
	case PreparationPending:
		if len(j.Applied) != 0 || j.Applying != "" {
			return errors.New("pending preparation journal cannot contain mutation progress")
		}
	case PreparationApplying:
		if j.Applying == "" {
			return errors.New("applying preparation journal requires an in-flight mutation")
		}
	case PreparationPreparing:
		if len(j.Applied) == 0 {
			return errors.New("preparing journal requires at least one applied mutation")
		}
		if j.Applying != "" {
			return errors.New("preparing journal cannot contain an in-flight mutation")
		}
	case PreparationReady, PreparationCommitting, PreparationCommitted:
		if j.Applying != "" {
			return fmt.Errorf("%s preparation journal cannot contain an in-flight mutation", status)
		}
		// Exact prefix length is plan-dependent and is checked by Validate.
	case PreparationRollingBack, PreparationRolledBack:
		if j.Applying != "" {
			return fmt.Errorf("%s preparation journal cannot contain an in-flight prepare mutation", status)
		}
		if status == PreparationRolledBack && j.RollbackApplying != "" {
			return errors.New("rolled_back preparation journal cannot contain an in-flight rollback mutation")
		}
	default:
		return errors.New("invalid preparation journal status")
	}
	return nil
}

// Validate checks that a persisted preparation journal is a valid prefix of
// the current plan and that its lifecycle status agrees with that prefix. This
// prevents crash recovery from silently accepting reordered, duplicated, or
// stale mutation identities.
func (j PreparationJournal) Validate(p PreparationPlan) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := j.ValidateShape(); err != nil {
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
	rollbackIndex := len(j.Applied)
	for _, id := range append(append([]string(nil), j.RollbackApplied...), j.RollbackApplying) {
		if id == "" {
			continue
		}
		found := -1
		for i := rollbackIndex - 1; i >= 0; i-- {
			if p.Prepare[i].ID == id {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("preparation journal rollback mutation %q is not in the applied prefix or is out of reverse order", id)
		}
		if p.Prepare[found].Policy != RollbackSafe {
			return fmt.Errorf("preparation journal rollback mutation %q is not automatically reversible", id)
		}
		rollbackIndex = found
	}

	switch status {
	case PreparationPending:
		if len(j.Applied) != 0 {
			return errors.New("pending preparation journal cannot contain applied mutations")
		}
	case PreparationApplying:
		if len(j.Applied) >= len(p.Prepare) {
			return errors.New("applying preparation journal cannot follow a complete mutation prefix")
		}
		want := p.Prepare[len(j.Applied)].ID
		if j.Applying != want {
			return fmt.Errorf("preparation journal applying mutation is %q, want %q", j.Applying, want)
		}
	case PreparationPreparing:
		if len(j.Applied) == 0 || len(j.Applied) >= len(p.Prepare) {
			return errors.New("preparing journal requires a non-empty partial mutation prefix")
		}
	case PreparationReady:
		if len(j.Applied) != len(p.Prepare) {
			return errors.New("ready preparation journal must contain every prepare mutation")
		}
	case PreparationCommitting:
		if len(j.Applied) != len(p.Prepare) {
			return errors.New("committing preparation journal must contain every prepare mutation")
		}
	case PreparationCommitted:
		if len(j.Applied) != len(p.Prepare) {
			return errors.New("committed preparation journal must contain every prepare mutation")
		}
	case PreparationRollingBack, PreparationRolledBack:
		// Rollback may follow failure after any valid prefix, including none.
	default:
		return fmt.Errorf("invalid preparation journal status %q", j.Status)
	}
	return nil
}

// PlanApply marks one prepare mutation as in flight. The caller must persist
// this journal before executing the host mutation. If the process crashes after
// this boundary, recovery can distinguish an ambiguous mutation outcome from a
// mutation that definitely never started and will not replay it automatically.
func (j PreparationJournal) PlanApply(p PreparationPlan, id string) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status == "" {
		j.Status = PreparationPending
	}
	switch j.Status {
	case PreparationPending, PreparationPreparing:
	default:
		return PreparationJournal{}, fmt.Errorf("cannot plan preparation mutation in state %q", j.Status)
	}
	if len(j.Applied) >= len(p.Prepare) {
		return PreparationJournal{}, errors.New("all preparation mutations are already recorded")
	}
	want := p.Prepare[len(j.Applied)].ID
	if id != want {
		return PreparationJournal{}, fmt.Errorf("preparation mutation %q planned out of order (want %q)", id, want)
	}
	out := j
	out.Status = PreparationApplying
	out.Applied = append([]string(nil), j.Applied...)
	out.Applying = id
	return out, nil
}

// RecordApplied advances the journal after the in-flight prepare mutation
// completed successfully. PlanApply must have been persisted before the host
// mutation began, so this confirmation only closes an existing WAL boundary.
func (j PreparationJournal) RecordApplied(p PreparationPlan, id string) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status != PreparationApplying {
		return PreparationJournal{}, fmt.Errorf("preparation mutation is not in flight (state %q)", j.Status)
	}
	if id != j.Applying {
		return PreparationJournal{}, fmt.Errorf("preparation mutation %q completed out of order (in flight %q)", id, j.Applying)
	}
	out := j
	out.Applied = append(append([]string(nil), j.Applied...), id)
	out.Applying = ""
	if len(out.Applied) == len(p.Prepare) {
		out.Status = PreparationReady
	} else {
		out.Status = PreparationPreparing
	}
	return out, nil
}

// ResolveApplying closes an ambiguous prepare-mutation WAL record after the
// caller has established whether the host mutation actually completed. An
// applied outcome advances the confirmed prefix exactly as RecordApplied does.
// A not-applied outcome clears only the in-flight marker, returning to the
// pre-mutation lifecycle state so the same mutation may be planned again.
//
// This method is intentionally evidence-driven: RecoveryFor remains blocked
// while the outcome is unknown and never calls ResolveApplying implicitly.
func (j PreparationJournal) ResolveApplying(p PreparationPlan, id string, outcome PreparationMutationOutcome) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status != PreparationApplying {
		return PreparationJournal{}, fmt.Errorf("preparation mutation is not in flight (state %q)", j.Status)
	}
	if id != j.Applying {
		return PreparationJournal{}, fmt.Errorf("preparation mutation %q does not match in-flight mutation %q", id, j.Applying)
	}

	switch outcome {
	case PreparationMutationApplied:
		return j.RecordApplied(p, id)
	case PreparationMutationNotApplied:
		out := j
		out.Applied = append([]string(nil), j.Applied...)
		out.Applying = ""
		if len(out.Applied) == 0 {
			out.Status = PreparationPending
		} else {
			out.Status = PreparationPreparing
		}
		return out, nil
	default:
		return PreparationJournal{}, fmt.Errorf("invalid preparation mutation outcome %q", outcome)
	}
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

// PlanCommit marks that candidate commit operations are about to begin. The
// caller must persist this journal before executing any commit mutation. A
// crash in this state has an ambiguous host-side commit outcome and therefore
// must not be treated as a preparation-only failure that is safe to roll back.
func (j PreparationJournal) PlanCommit(p PreparationPlan) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if !j.ReadyForCommit(p) {
		return PreparationJournal{}, fmt.Errorf("preparation is not ready to commit (state %q)", j.Status)
	}
	out := j
	out.Status = PreparationCommitting
	out.Applied = append([]string(nil), j.Applied...)
	return out, nil
}

// ResolveCommitNotApplied closes an ambiguous committing WAL boundary only
// after the caller has authoritative evidence that none of the commit
// operations took effect. It returns to the ready state so the caller may
// either retry commit or roll back the already-confirmed preparation prefix.
//
// RecoveryFor never invokes this transition implicitly: an absent or drifted
// desired-state probe is not sufficient evidence that commit had no side
// effects. Adapters/executors must establish the stronger, method-specific
// "not applied" fact before calling this method.
func (j PreparationJournal) ResolveCommitNotApplied(p PreparationPlan) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status != PreparationCommitting {
		return PreparationJournal{}, fmt.Errorf("preparation commit is not in progress (state %q)", j.Status)
	}
	out := j
	out.Status = PreparationReady
	out.Applied = append([]string(nil), j.Applied...)
	return out, nil
}

// MarkCommitted records that the candidate commit operations completed. The
// committing state must have been persisted before host mutation began. After
// this point preparation rollback is no longer automatic; ownership/refcount
// state governs future removal.
func (j PreparationJournal) MarkCommitted(p PreparationPlan) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status != PreparationCommitting {
		return PreparationJournal{}, fmt.Errorf("preparation commit is not in progress (state %q)", j.Status)
	}
	out := j
	out.Status = PreparationCommitted
	out.Applied = append([]string(nil), j.Applied...)
	return out, nil
}

// FinalizeCommit records a completed candidate commit and atomically projects
// every prepared resource into ownership/refcount state for dependent. Existing
// resource ownership must match the preparation declaration. The returned
// snapshot is canonical, deeply isolated from the input, and safe to persist as
// one state transition with the committed journal.
func (j PreparationJournal) FinalizeCommit(p PreparationPlan, owned []OwnedResourceState, dependent string) (PreparationJournal, []OwnedResourceState, error) {
	if err := validateDependent(dependent); err != nil {
		return PreparationJournal{}, nil, err
	}
	snapshot, err := ownedResourceSnapshot(owned)
	if err != nil {
		return PreparationJournal{}, nil, err
	}
	committed, err := j.MarkCommitted(p)
	if err != nil {
		return PreparationJournal{}, nil, err
	}
	for _, mutation := range p.Prepare {
		state, exists := snapshot[mutation.Resource]
		if exists {
			if state.Ownership != mutation.Ownership {
				return PreparationJournal{}, nil, fmt.Errorf("commit resource %q/%q ownership is %q, plan requires %q", mutation.Resource.Kind, mutation.Resource.Key, state.Ownership, mutation.Ownership)
			}
		} else {
			state = OwnedResourceState{Resource: mutation.Resource, Ownership: mutation.Ownership}
		}
		state, err = ClaimResource(state, dependent)
		if err != nil {
			return PreparationJournal{}, nil, fmt.Errorf("claim committed resource %q/%q: %w", mutation.Resource.Kind, mutation.Resource.Key, err)
		}
		snapshot[mutation.Resource] = state
	}
	return committed, sortedOwnedResourceStates(snapshot), nil
}

// PlanRollback computes rollback for the journaled mutations against a
// complete snapshot of already-committed shared-resource ownership and marks
// rollback as in progress. The returned decision is a preview/reporting view;
// each compensating operation must cross PlanRollbackApply/RecordRollbackApplied
// before FinalizeRollback is allowed. A committed candidate must instead use
// normal ownership-aware removal semantics.
func (j PreparationJournal) PlanRollback(p PreparationPlan, owned []OwnedResourceState) (PreparationJournal, RollbackDecision, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, RollbackDecision{}, err
	}
	if j.Status == PreparationApplying {
		return PreparationJournal{}, RollbackDecision{}, errors.New("preparation mutation is in progress; mutation outcome must be reconciled before rollback")
	}
	if j.Status == PreparationCommitting {
		return PreparationJournal{}, RollbackDecision{}, errors.New("preparation commit is in progress; commit outcome must be reconciled before rollback")
	}
	if j.Status == PreparationCommitted {
		return PreparationJournal{}, RollbackDecision{}, errors.New("committed preparation cannot use candidate rollback")
	}
	if j.Status == PreparationRolledBack {
		return PreparationJournal{}, RollbackDecision{}, errors.New("preparation is already rolled back")
	}
	if j.Status == PreparationRollingBack {
		return PreparationJournal{}, RollbackDecision{}, errors.New("preparation rollback is already in progress")
	}
	decision, err := p.RollbackFor(j.Applied, owned)
	if err != nil {
		return PreparationJournal{}, RollbackDecision{}, err
	}
	out := j
	out.Status = PreparationRollingBack
	out.Applied = append([]string(nil), j.Applied...)
	out.RollbackApplied = nil
	out.RollbackApplying = ""
	return out, decision, nil
}

// PlanRollbackApply marks the next compensating operation as in flight and
// returns that exact operation. The caller must persist the returned journal
// before executing the operation. Retained resources are skipped because they
// intentionally have no automatic compensation.
func (j PreparationJournal) PlanRollbackApply(p PreparationPlan, owned []OwnedResourceState, id string) (PreparationJournal, Operation, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, Operation{}, err
	}
	if j.Status != PreparationRollingBack {
		return PreparationJournal{}, Operation{}, fmt.Errorf("preparation rollback is not in progress (state %q)", j.Status)
	}
	if j.RollbackApplying != "" {
		return PreparationJournal{}, Operation{}, fmt.Errorf("rollback mutation %q is already in flight", j.RollbackApplying)
	}
	steps, _, err := p.rollbackStepsFor(j.Applied, owned)
	if err != nil {
		return PreparationJournal{}, Operation{}, err
	}
	if len(j.RollbackApplied) > len(steps) {
		return PreparationJournal{}, Operation{}, errors.New("rollback journal contains more completed operations than the current rollback decision")
	}
	for i, done := range j.RollbackApplied {
		if done != steps[i].ID {
			return PreparationJournal{}, Operation{}, fmt.Errorf("rollback journal mutation %d is %q, want %q", i, done, steps[i].ID)
		}
	}
	if len(j.RollbackApplied) == len(steps) {
		return PreparationJournal{}, Operation{}, errors.New("all rollback operations are already recorded")
	}
	want := steps[len(j.RollbackApplied)]
	if id != want.ID {
		return PreparationJournal{}, Operation{}, fmt.Errorf("rollback mutation %q planned out of order (want %q)", id, want.ID)
	}
	out := j
	out.Applied = append([]string(nil), j.Applied...)
	out.RollbackApplied = append([]string(nil), j.RollbackApplied...)
	out.RollbackApplying = id
	return out, cloneOperation(want.Operation), nil
}

// RecordRollbackApplied durably confirms that the one in-flight compensation
// completed successfully. PlanRollbackApply must have been persisted before
// the host mutation began.
func (j PreparationJournal) RecordRollbackApplied(p PreparationPlan, owned []OwnedResourceState, id string) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status != PreparationRollingBack || j.RollbackApplying == "" {
		return PreparationJournal{}, errors.New("rollback mutation is not in flight")
	}
	if id != j.RollbackApplying {
		return PreparationJournal{}, fmt.Errorf("rollback mutation %q completed out of order (in flight %q)", id, j.RollbackApplying)
	}
	steps, _, err := p.rollbackStepsFor(j.Applied, owned)
	if err != nil {
		return PreparationJournal{}, err
	}
	if len(j.RollbackApplied) >= len(steps) || steps[len(j.RollbackApplied)].ID != id {
		return PreparationJournal{}, fmt.Errorf("rollback mutation %q no longer matches the current rollback decision", id)
	}
	out := j
	out.Applied = append([]string(nil), j.Applied...)
	out.RollbackApplied = append(append([]string(nil), j.RollbackApplied...), id)
	out.RollbackApplying = ""
	return out, nil
}

// ResolveRollbackApplying closes an ambiguous rollback WAL record from
// explicit evidence. Applied confirms the compensation; not_applied clears
// only the in-flight marker so the same rollback operation may be retried.
func (j PreparationJournal) ResolveRollbackApplying(p PreparationPlan, owned []OwnedResourceState, id string, outcome PreparationMutationOutcome) (PreparationJournal, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, err
	}
	if j.Status != PreparationRollingBack || j.RollbackApplying == "" {
		return PreparationJournal{}, errors.New("rollback mutation is not in flight")
	}
	if id != j.RollbackApplying {
		return PreparationJournal{}, fmt.Errorf("rollback mutation %q does not match in-flight mutation %q", id, j.RollbackApplying)
	}
	switch outcome {
	case PreparationMutationApplied:
		return j.RecordRollbackApplied(p, owned, id)
	case PreparationMutationNotApplied:
		out := j
		out.Applied = append([]string(nil), j.Applied...)
		out.RollbackApplied = append([]string(nil), j.RollbackApplied...)
		out.RollbackApplying = ""
		return out, nil
	default:
		return PreparationJournal{}, fmt.Errorf("invalid rollback mutation outcome %q", outcome)
	}
}

func (j PreparationJournal) remainingRollback(p PreparationPlan, owned []OwnedResourceState) (RollbackDecision, error) {
	steps, retained, err := p.rollbackStepsFor(j.Applied, owned)
	if err != nil {
		return RollbackDecision{}, err
	}
	if len(j.RollbackApplied) > len(steps) {
		return RollbackDecision{}, errors.New("rollback journal contains more completed operations than the current rollback decision")
	}
	for i, done := range j.RollbackApplied {
		if done != steps[i].ID {
			return RollbackDecision{}, fmt.Errorf("rollback journal mutation %d is %q, want %q", i, done, steps[i].ID)
		}
	}
	out := RollbackDecision{Retained: append([]ResourceIdentity(nil), retained...)}
	for _, step := range steps[len(j.RollbackApplied):] {
		out.OperationIDs = append(out.OperationIDs, step.ID)
		out.Operations = append(out.Operations, cloneOperation(step.Operation))
	}
	return out, nil
}

// FinalizeRollback records successful completion of the rollback operations
// planned by PlanRollback and projects every retained mutation into explicit
// ownership state. This prevents a candidate fallback from silently forgetting
// host mutations that could not be reversed. Retained resources that already
// existed in the ownership snapshot preserve their dependents; newly retained
// resources are recorded with zero committed dependents so state/reporting can
// surface them for later cleanup or adoption.
func (j PreparationJournal) FinalizeRollback(p PreparationPlan, owned []OwnedResourceState) (PreparationJournal, []OwnedResourceState, error) {
	if err := j.Validate(p); err != nil {
		return PreparationJournal{}, nil, err
	}
	if j.Status != PreparationRollingBack {
		return PreparationJournal{}, nil, fmt.Errorf("preparation rollback is not in progress (state %q)", j.Status)
	}
	if j.RollbackApplying != "" {
		return PreparationJournal{}, nil, fmt.Errorf("rollback mutation %q has an ambiguous host-side outcome", j.RollbackApplying)
	}
	remaining, err := j.remainingRollback(p, owned)
	if err != nil {
		return PreparationJournal{}, nil, err
	}
	if len(remaining.Operations) != 0 {
		return PreparationJournal{}, nil, fmt.Errorf("preparation rollback has %d unconfirmed operation(s)", len(remaining.Operations))
	}
	decision, err := p.RollbackFor(j.Applied, owned)
	if err != nil {
		return PreparationJournal{}, nil, err
	}
	snapshot, err := ownedResourceSnapshot(owned)
	if err != nil {
		return PreparationJournal{}, nil, err
	}

	mutations := make(map[ResourceIdentity]PreparationMutation, len(j.Applied))
	for i := range j.Applied {
		mutation := p.Prepare[i]
		mutations[mutation.Resource] = mutation
	}
	for _, resource := range decision.Retained {
		mutation, ok := mutations[resource]
		if !ok {
			return PreparationJournal{}, nil, fmt.Errorf("retained resource %q/%q is not an applied preparation mutation", resource.Kind, resource.Key)
		}
		state, exists := snapshot[resource]
		if exists {
			if state.Ownership != mutation.Ownership {
				return PreparationJournal{}, nil, fmt.Errorf("retained resource %q/%q ownership is %q, plan requires %q", resource.Kind, resource.Key, state.Ownership, mutation.Ownership)
			}
			continue
		}
		snapshot[resource] = OwnedResourceState{Resource: resource, Ownership: mutation.Ownership}
	}

	out := j
	out.Status = PreparationRolledBack
	out.Applied = append([]string(nil), j.Applied...)
	out.RollbackApplied = append([]string(nil), j.RollbackApplied...)
	out.RollbackApplying = ""
	return out, sortedOwnedResourceStates(snapshot), nil
}
