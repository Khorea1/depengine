package planner

import (
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

var sourceFields = []struct {
	key  string
	role plan.SourceRole
}{
	{"source", plan.SourceSelection}, {"git", plan.SourceSelection},
	{"registry", plan.SourceRegistry}, {"remote", plan.SourceRemote},
	{"bucket", plan.SourceSelection}, {"index", plan.SourceIndex}, {"index_url", plan.SourceIndex},
}

func applySources(p *plan.ResolvedInstallPlan, method *config.MethodCandidate) {
	for _, source := range method.Sources {
		ref := plan.SourceReference{
			Role: plan.SourceHostConfiguration,
			Kind: source.Kind,
			Name: source.Name,
			URL:  source.URL,
		}
		if source.SecretRef != nil {
			ref.SecretRef = &plan.SecretReference{Provider: source.SecretRef.Provider, Name: source.SecretRef.Name}
		}
		p.Sources = append(p.Sources, ref)
	}
	for _, field := range sourceFields {
		if value := stringValue(method.Config, field.key); value != "" {
			p.Sources = append(p.Sources, sourceReference(field.role, value))
		}
	}
	for _, channel := range stringList(method.Config["channels"]) {
		p.Sources = append(p.Sources, plan.SourceReference{Role: plan.SourceChannel, Name: channel})
	}
}

func sourceReference(role plan.SourceRole, value string) plan.SourceReference {
	if strings.Contains(value, "://") {
		return plan.SourceReference{Role: role, URL: value}
	}
	return plan.SourceReference{Role: role, Name: value}
}
