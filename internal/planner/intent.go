// Package planner projects normalized schema candidates into adapter-neutral
// plan semantics before any host-dependent resolution or mutation occurs.
package planner

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// BuildCandidateIntent builds the deterministic, host-independent portion of a
// selected candidate's plan. Runtime resolvers may enrich this object with
// concrete versions, revisions, digests, artifact URLs, and placement paths.
func BuildCandidateIntent(tool *config.Tool, method *config.MethodCandidate) (plan.ResolvedInstallPlan, error) {
	if tool == nil || method == nil {
		return plan.ResolvedInstallPlan{}, invalid("candidate", fmt.Errorf("tool and method are required"))
	}
	contract, ok := methodkind.Lookup(method.Kind)
	if !ok {
		return plan.ResolvedInstallPlan{}, &plan.PlannerError{Class: plan.ErrorUnsupportedCapability, Op: "candidate", Err: fmt.Errorf("unknown method kind %q", method.Kind)}
	}
	if method.Err != nil {
		return plan.ResolvedInstallPlan{}, invalid("candidate", method.Err)
	}
	if err := validateConfigKeys(method.Config, contract); err != nil {
		return plan.ResolvedInstallPlan{}, invalid("candidate", err)
	}

	p := plan.New(tool.Name, method.Kind, !method.Inferred)
	if err := applyIdentity(&p, tool, method, contract); err != nil {
		return plan.ResolvedInstallPlan{}, invalid("candidate", err)
	}
	applySources(&p, method)
	if err := applyArtifact(&p, method, contract); err != nil {
		return plan.ResolvedInstallPlan{}, invalid("candidate", err)
	}
	applyPrerequisites(&p, method)
	applyMethodOperations(&p, method, contract)
	if contract.CanRemove {
		p.Removal = plan.RemovalMetadata{Supported: true, Identity: removalIdentity(p)}
	} else {
		p.Removal = plan.RemovalMetadata{Supported: false}
	}
	if err := p.Validate(); err != nil {
		return plan.ResolvedInstallPlan{}, invalid("candidate", err)
	}
	return p, nil
}

func invalid(op string, err error) error {
	return &plan.PlannerError{Class: plan.ErrorInvalidManifest, Op: op, Err: err}
}

func removalIdentity(p plan.ResolvedInstallPlan) string {
	if p.Identity.Package != "" {
		return p.Identity.Package
	}
	return p.Tool.Name
}
