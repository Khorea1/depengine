package planner

import (
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

func applyIdentity(p *plan.ResolvedInstallPlan, tool *config.Tool, method *config.MethodCandidate, contract *methodkind.Contract) error {
	applyPackageIdentity(&p.Identity, tool, method, contract)
	if err := applyVersionIdentity(&p.Identity, method.Config, contract); err != nil {
		return err
	}
	p.Identity.Source = firstValue(method.Config, "source", "git", "repo")
	if p.Identity.Source == "" && method.Kind == "git" {
		p.Identity.Source = stringValue(method.Config, "url")
	}
	p.Identity.Registry = stringValue(method.Config, "registry")
	p.Identity.Architecture = firstValue(method.Config, "architecture", "target")
	p.Identity.Platform = stringValue(method.Config, "platform")
	if err := applyScope(&p.Identity, method, contract); err != nil {
		return err
	}
	applyEnvironment(&p.Identity, method)
	return nil
}

func applyPackageIdentity(identity *plan.ResolvedIdentity, tool *config.Tool, method *config.MethodCandidate, contract *methodkind.Contract) {
	if _, ok := contract.Fields["pkg"]; !ok {
		return
	}
	identity.Package = stringValue(method.Config, "pkg")
	if identity.Package == "" {
		identity.Package = tool.Name
	}
}

func applyVersionIdentity(identity *plan.ResolvedIdentity, cfg map[string]any, contract *methodkind.Contract) error {
	intent, err := versionIntent(cfg, contract)
	if err != nil || intent == nil {
		return err
	}
	identity.RequestedVersion = intent
	if intent.Mode == plan.VersionExact {
		identity.Version = intent.Value
	}
	if intent.Mode == plan.VersionDigest {
		identity.Digest = intent.Value
	}
	return nil
}
