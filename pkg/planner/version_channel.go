package planner

import (
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/plan"
)

func channelIntent(cfg map[string]any, contract *methodkind.Contract) *plan.VersionIntent {
	selector := &plan.ChannelSelector{
		Name:  stringValue(cfg, "channel"),
		Track: stringValue(cfg, "track"),
		Risk:  stringValue(cfg, "risk"),
	}
	if selector.Name == "" && selector.Track == "" && selector.Risk == "" {
		// branch doubles as the channel name only for channel-aware methods.
		if !contract.Supports(methodkind.CapabilityChannel) {
			return nil
		}
		selector.Name = stringValue(cfg, "branch")
	}
	if selector.Name == "" && selector.Track == "" && selector.Risk == "" {
		return nil
	}
	return &plan.VersionIntent{Mode: plan.VersionChannel, Channel: selector}
}
