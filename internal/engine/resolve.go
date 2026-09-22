package engine

import "github.com/Khorea1/depengine/internal/platform"

func ResolveFamily(facts *Facts) string { return platform.ResolveFamily(facts) }

func MatchesDistroFamily(family string, allowed []string) bool {
	return platform.MatchesDistroFamily(family, allowed)
}
