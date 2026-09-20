package planner

import (
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/plan"
)

func digestIntent(cfg map[string]any, _ *methodkind.Contract) *plan.VersionIntent {
	if value := stringValue(cfg, "digest"); value != "" {
		return &plan.VersionIntent{Mode: plan.VersionDigest, Value: value}
	}
	return nil
}

func tagIntent(cfg map[string]any, contract *methodkind.Contract) *plan.VersionIntent {
	value := stringValue(cfg, "tag")
	if value == "" {
		return nil
	}
	// Mutable image tags are a distinct mode; everywhere else a tag pins a
	// revision, which the capability boundary rejects for methods without it.
	if contract.Supports(methodkind.CapabilityMutableTag) {
		return &plan.VersionIntent{Mode: plan.VersionContainerTag, Value: value}
	}
	return &plan.VersionIntent{Mode: plan.VersionGitTag, Value: value}
}

func exactVersionIntent(cfg map[string]any, _ *methodkind.Contract) *plan.VersionIntent {
	if value := stringValue(cfg, "version"); value != "" {
		return &plan.VersionIntent{Mode: plan.VersionExact, Value: value}
	}
	return nil
}
