package planner

import (
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/plan"
)

func revisionIntent(cfg map[string]any, _ *methodkind.Contract) *plan.VersionIntent {
	value := stringValue(cfg, "rev")
	if value == "" {
		return nil
	}
	return &plan.VersionIntent{Mode: plan.VersionGitRevision, Value: value}
}

func branchIntent(cfg map[string]any, contract *methodkind.Contract) *plan.VersionIntent {
	value := stringValue(cfg, "branch")
	if value == "" {
		return nil
	}
	if contract.Supports(methodkind.CapabilityChannel) {
		return nil // channel-aware methods read branch through channelIntent
	}
	// Artifact methods declare branch as a resolution field, not a selector.
	_, declared := contract.Fields["branch"]
	if contract.Supports(methodkind.CapabilityRevision) || !declared {
		return &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: value}
	}
	return nil
}
