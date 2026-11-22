package exec

import (
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/plan"
)

// DescribeProspectiveEffects identifies a tentative declared native package
// target for an already selected clan. It does not probe the host: a clan
// supplied by the caller is not proof of the host's package database. The
// returned envelope is always incomplete and must not authorize mutation.
func (a *NativeAdapter) DescribeProspectiveEffects(tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan, transition plan.TransitionKind) (plan.ProspectiveEffectEnvelope, error) {
	clan := ""
	if a != nil {
		clan = a.declaredClan // Immutable: auto-detected a.clan is not safe to read here.
	}
	return describeNativeProspectiveEffects(tool, mc, resolved, transition, "native", clan, "")
}

// DescribeProspectiveEffects identifies a tentative package target for one
// explicit native-manager candidate, including documented manager aliases.
// The candidate's resolved package, not stale method config, is authoritative.
func (a *NativeByManagerAdapter) DescribeProspectiveEffects(tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan, transition plan.TransitionKind) (plan.ProspectiveEffectEnvelope, error) {
	if a == nil {
		return plan.DescribeProspectiveEffects(resolved, transition)
	}
	return describeNativeProspectiveEffects(tool, mc, resolved, transition, a.managerName, findClanByManager(a.managerName), a.managerName)
}

// nativeProspectiveManagerName rejects ambiguous manager labels and keeps
// distinct manager databases (e.g. Homebrew vs MacPorts) separate. Aliases
// like dnf5/yum deliberately resolve to the same declared Fedora manager.
// A matching name here means a matching DECLARED target, not physical overlap.
func nativeProspectiveManagerName(clan, requested string) (string, bool) {
	manager, ok := native.Lookup(clan)
	if !ok || manager.Name == "" {
		return "", false
	}
	if requested == "" {
		return manager.Name, true
	}
	if requested == "port" && clan == "macos" {
		return "port", true
	}
	if mapped, ok := native.ManagerNameToClan(requested); ok {
		if mapped == clan {
			return manager.Name, true
		}
		return "", false
	}
	// A manager label that exists under multiple clans (notably "pkg")
	// is not sufficient evidence of a unique namespace.
	if _, ok := native.LookupManager(requested); ok && requested == manager.Name {
		return manager.Name, true
	}
	return "", false
}

func describeNativeProspectiveEffects(tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan, transition plan.TransitionKind, method, clan, requestedManager string) (plan.ProspectiveEffectEnvelope, error) {
	envelope, err := plan.DescribeProspectiveEffects(resolved, transition)
	if err != nil {
		return envelope, err
	}
	// Upgrade and removal require independently verified prior identities:
	// an install candidate cannot prove what existing package they will touch.
	if transition != plan.TransitionInstall || resolved == nil || tool == nil || mc == nil ||
		tool.Name != resolved.Tool.Name || mc.Kind != method || resolved.Candidate.Method != method ||
		resolved.Identity.Package == "" {
		return envelope, nil
	}
	manager, ok := nativeProspectiveManagerName(clan, requestedManager)
	if !ok {
		return envelope, nil
	}
	// The scope explicitly describes a declared manager namespace, not an
	// actual host database or a canonical physical resource identifier.
	target := plan.ProspectiveTarget{
		Scope: "declared-native-manager", Kind: "package-target",
		Identity: clan + "/" + manager + "/" + resolved.Identity.Package,
	}
	for i := range envelope.Steps {
		if envelope.Steps[i].Phase == "primary_install" {
			envelope.Steps[i].Targets = []plan.ProspectiveTarget{target}
			break
		}
	}
	return envelope, nil
}
