package planner

import (
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

func applyPrerequisites(p *plan.ResolvedInstallPlan, method *config.MethodCandidate) {
	for _, name := range method.Requires {
		p.Prerequisites = append(p.Prerequisites, plan.Prerequisite{Name: name})
	}
}

func applyMethodOperations(p *plan.ResolvedInstallPlan, method *config.MethodCandidate, contract *methodkind.Contract) {
	for name, field := range contract.Fields {
		if field.Type != methodkind.Command {
			continue
		}
		if _, configured := method.Config[name]; configured {
			p.Operations = append(p.Operations, plan.Operation{
				Kind:          name,
				Description:   "configured method command",
				Effect:        plan.EffectMutation,
				ArbitraryCode: true,
			})
		}
	}
	p.Operations = append(p.Operations, plan.Operation{Kind: "install", Effect: plan.EffectMutation})
}
