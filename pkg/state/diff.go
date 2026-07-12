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

// Diff compares two State values and returns a sorted list of differences.
// Tools present in both states with the same DefinitionHash are omitted
// (they match). The result is sorted by tool name.
func Diff(a, b *State) []DiffItem {
	var items []DiffItem

	// Collect all tool names from both states.
	names := make(map[string]struct{})
	for name := range a.Tools {
		names[name] = struct{}{}
	}
	for name := range b.Tools {
		names[name] = struct{}{}
	}

	for name := range names {
		ta, okA := a.Tools[name]
		tb, okB := b.Tools[name]
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
			// In both — compare hashes.
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
