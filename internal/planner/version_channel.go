package planner

import (
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

func channelIntent(cfg map[string]any, contract *methodkind.Contract) *plan.VersionIntent {
	selector := &plan.ChannelSelector{
		Name:   stringValue(cfg, "channel"),
		Track:  stringValue(cfg, "track"),
		Risk:   stringValue(cfg, "risk"),
		Branch: stringValue(cfg, "branch"),
	}
	if selector.Name == "" && selector.Track == "" && selector.Risk == "" &&
		(selector.Branch == "" || !contract.Supports(methodkind.CapabilityChannel)) {
		// Undeclared channel/track/risk selectors must still produce intent:
		// capability validation then rejects unsupported methods explicitly.
		// Branch on a non-channel method is handled by branchIntent instead.
		return nil
	}
	return &plan.VersionIntent{Mode: plan.VersionChannel, Channel: selector}
}

// snapTrackingChannel produces the canonical channel reported by snap list.
// Shorthand risk channels select the implicit latest track; an omitted risk
// on a track defaults to stable. Branch is part of the selected channel.
func snapTrackingChannel(selector *plan.ChannelSelector) string {
	if selector == nil {
		return ""
	}
	if selector.Name != "" {
		return "latest/" + selector.Name
	}
	if selector.Track == "" && selector.Risk == "" && selector.Branch == "" {
		return ""
	}
	track, risk := selector.Track, selector.Risk
	if track == "" {
		track = "latest"
	}
	if risk == "" {
		risk = "stable"
	}
	channel := track + "/" + risk
	if selector.Branch != "" {
		channel += "/" + selector.Branch
	}
	return channel
}
