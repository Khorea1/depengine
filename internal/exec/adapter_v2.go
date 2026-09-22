package exec

import (
	"context"
	"errors"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// AdapterV2 is the plan-aware adapter seam.
//
// It deliberately lives beside Adapter while adapters migrate one at a time.
// The serial executor consumes Observe and InstallResolved for adapters that
// implement V2; other execution paths retain the legacy Adapter contract.
//
// AdapterV2 does not define an operation interpreter. In particular,
// Operation.Command is not a fallback execution mechanism: adapters must give
// each operation its own typed semantics before a V2 executor can apply it.
type AdapterV2 interface {
	Adapter
	ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error)
	Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error)
	InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error
}

// ErrAdapterV2OperationsUnsupported reports that a legacy adapter was asked
// to install a plan containing explicit operations. Legacy adapters only know
// the old Tool/MethodCandidate semantics; treating operation commands as a
// generic fallback would silently turn plan data into arbitrary execution.
var ErrAdapterV2OperationsUnsupported = errors.New("legacy adapter cannot execute resolved plan operations")

// LegacyAdapterV2 adapts an existing Adapter to AdapterV2 without changing
// the legacy adapter or the executor's current dispatch path.
//
// Resolution is intentionally identity-preserving: legacy adapters have no
// plan resolver, so the shim returns a clone of the intent. Observation keeps
// the old boolean Check semantics, mapping true to present and false to
// absent. Installation delegates to the old Install method and never reads
// Operation.Command.
type LegacyAdapterV2 struct {
	legacy Adapter
}

// NewLegacyAdapterV2 returns a plan-aware view of legacy. A nil legacy adapter
// produces nil, matching the registry's convention for an absent adapter.
func NewLegacyAdapterV2(legacy Adapter) *LegacyAdapterV2 {
	if legacy == nil {
		return nil
	}
	return &LegacyAdapterV2{legacy: legacy}
}

var _ AdapterV2 = (*LegacyAdapterV2)(nil)

func (a *LegacyAdapterV2) Kind() string { return a.legacy.Kind() }

func (a *LegacyAdapterV2) Available(ctx context.Context, rn run.Runner) bool {
	return a.legacy.Available(ctx, rn)
}

func (a *LegacyAdapterV2) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	return a.legacy.Check(ctx, rn, tool, mc)
}

func (a *LegacyAdapterV2) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	return a.legacy.Install(ctx, rn, tool, mc)
}

func (a *LegacyAdapterV2) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("legacy adapter received nil plan intent")
	}
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *LegacyAdapterV2) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	presence := plan.PresenceAbsent
	if a.legacy.Check(ctx, rn, tool, mc) {
		presence = plan.PresencePresent
	}
	return plan.Observation{Presence: presence}, nil
}

func (a *LegacyAdapterV2) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil {
		return errors.New("legacy adapter received nil resolved plan")
	}
	if len(resolved.Operations) > 0 {
		return ErrAdapterV2OperationsUnsupported
	}
	return a.legacy.Install(ctx, rn, tool, mc)
}
