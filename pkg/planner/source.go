package planner

import (
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/plan"
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
