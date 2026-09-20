package planner

import (
	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/plan"
)

func applyArtifact(p *plan.ResolvedInstallPlan, method *config.MethodCandidate, contract *methodkind.Contract) {
	if contract.Artifact == nil {
		return
	}
	url := stringValue(method.Config, "url")
	if url == "" {
		if asset := stringValue(method.Config, "asset"); asset != "" {
			p.Operations = append(p.Operations, plan.Operation{Kind: "resolve-artifact", Description: asset, Effect: plan.EffectReadOnly})
		}
		return
	}
	p.Artifacts = append(p.Artifacts, plan.Artifact{
		URL:          url,
		Checksum:     stringValue(method.Config, "checksum"),
		SignatureURL: stringValue(method.Config, "signature_url"),
	})
}
