package state

import "sort"

// DiffItem represents a single difference between two State values.
type DiffItem struct {
	Name         string `json:"name"`
	Side         string `json:"side"` // "only_a", "only_b", "different"
	MethodA      string `json:"method_a,omitempty"`
	MethodB      string `json:"method_b,omitempty"`
	HashA        string `json:"hash_a,omitempty"`
	HashB        string `json:"hash_b,omitempty"`
	InstalledAtA string `json:"installed_at_a,omitempty"`
	InstalledAtB string `json:"installed_at_b,omitempty"`
}

// stateTools returns the Tools map of s, or nil when s is nil.
func stateTools(s *State) map[string]ToolState {
	if s == nil {
		return nil
	}
	return s.Tools
}

// collectNames returns every tool name present in any of the given states.
func collectNames(states ...*State) map[string]struct{} {
	names := make(map[string]struct{})
	for _, s := range states {
		tools := stateTools(s)
		for name := range tools {
			names[name] = struct{}{}
		}
	}
	return names
}

// Diff compares two State values and returns a sorted list of differences.
// Tools present in both states with the same DefinitionHash are omitted
// (they match). The result is sorted by tool name.
func Diff(a, b *State) []DiffItem {
	var items []DiffItem

	if a == nil && b == nil {
		return items
	}

	names := collectNames(a, b)

	for name := range names {
		ta, okA := stateTools(a)[name]
		tb, okB := stateTools(b)[name]

		switch {
		case okA && !okB:
			items = append(items, DiffItem{
				Name:         name,
				Side:         "only_a",
				MethodA:      ta.Method,
				HashA:        ta.DefinitionHash,
				InstalledAtA: ta.InstalledAt,
			})
		case !okA && okB:
			items = append(items, DiffItem{
				Name:         name,
				Side:         "only_b",
				MethodB:      tb.Method,
				HashB:        tb.DefinitionHash,
				InstalledAtB: tb.InstalledAt,
			})
		default:
			if ta.DefinitionHash != tb.DefinitionHash {
				items = append(items, DiffItem{
					Name:         name,
					Side:         "different",
					MethodA:      ta.Method,
					MethodB:      tb.Method,
					HashA:        ta.DefinitionHash,
					HashB:        tb.DefinitionHash,
					InstalledAtA: ta.InstalledAt,
					InstalledAtB: tb.InstalledAt,
				})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	return items
}
