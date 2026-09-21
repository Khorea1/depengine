package planner

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// versionResolver projects one family of selector fields into a version
// intent. Resolvers only disambiguate the mode a field means for this
// contract; they never consult whether the contract can honor that mode.
// Honoring is decided afterwards by the capability boundary, so an
// unsupported selector is rejected instead of silently dropped.
type versionResolver func(map[string]any, *methodkind.Contract) *plan.VersionIntent

var versionResolvers = []versionResolver{
	digestIntent,
	tagIntent,
	revisionIntent,
	channelIntent,
	branchIntent,
	exactVersionIntent,
}

// versionIntent returns the single version intent configured on a candidate.
// ResolvedIdentity carries exactly one intent, so several selectors cannot be
// represented; dropping all but one would weaken the request.
func versionIntent(cfg map[string]any, contract *methodkind.Contract) (*plan.VersionIntent, error) {
	var found []plan.VersionIntent
	for _, resolve := range versionResolvers {
		if intent := resolve(cfg, contract); intent != nil {
			found = append(found, *intent)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	}
	modes := make([]string, len(found))
	for i, intent := range found {
		modes[i] = string(intent.Mode)
	}
	return nil, fmt.Errorf("conflicting version selectors (%s); configure exactly one", strings.Join(modes, ", "))
}
