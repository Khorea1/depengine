package config

import (
	"sort"

	"github.com/Khorea1/depengine/pkg/methodkind"
)

func OrderMethods(methods []*MethodCandidate, order []string) []*MethodCandidate {
	return selectMethods(methods, order, false)
}

// SelectMethods applies a tool's method policy. Selectors match an exact
// candidate label or every candidate of a kind. method_only is exhaustive;
// method_prefer changes priority without removing fallbacks.
func SelectMethods(tool *Tool, defaultOrder []string, nativeManagerName string) []*MethodCandidate {
	order := EffectiveMethodOrder(tool, defaultOrder, nativeManagerName)
	return selectMethods(tool.Methods, order, len(tool.MethodOnly) > 0)
}

func selectMethods(methods []*MethodCandidate, selectors []string, exclusive bool) []*MethodCandidate {
	remaining := append([]*MethodCandidate(nil), methods...)
	sort.SliceStable(remaining, func(i, j int) bool {
		return methodName(remaining[i]) < methodName(remaining[j])
	})

	out := make([]*MethodCandidate, 0, len(methods))
	for _, selector := range selectors {
		for i := 0; i < len(remaining); {
			if !methodMatchesSelector(remaining[i], selector) {
				i++
				continue
			}
			out = append(out, remaining[i])
			remaining = append(remaining[:i], remaining[i+1:]...)
		}
	}
	if !exclusive {
		out = append(out, remaining...)
	}
	return out
}

func methodMatchesSelector(method *MethodCandidate, selector string) bool {
	return method.Kind == selector || method.Label != "" && method.Label == selector
}

func methodName(method *MethodCandidate) string {
	if method.Label != "" {
		return method.Label
	}
	return method.Kind
}

// ExpandMethodOrder resolves native manager references in a method_order
// list for the current machine's native manager.
//
// Rules:
//   - If the native manager name (e.g. "apt") appears explicitly in the
//     list, all "native" entries are removed (avoids duplicate attempts).
//   - The native manager name entry is replaced with "native" so it matches
//     the method kind used by schema parsing.
//   - All other entries pass through unchanged.
//   - If nativeManagerName is empty (unknown clan), the order is returned
//     unchanged.
func ExpandMethodOrder(order []string, nativeManagerName string) []string {
	if nativeManagerName == "" {
		return order
	}

	hasExplicitNativeMgr := false
	for _, k := range order {
		if k == nativeManagerName {
			hasExplicitNativeMgr = true
			break
		}
	}

	expanded := make([]string, 0, len(order))
	for _, k := range order {
		switch {
		case k == "native" && hasExplicitNativeMgr:
			// Skip: native manager name has its own explicit entry
		case k == nativeManagerName:
			expanded = append(expanded, "native")
		default:
			expanded = append(expanded, k)
		}
	}
	return expanded
}

// MergeMethodOrder merges a tool-specific method_order with the default
// order. The tool's list forms the prefix; entries from the default not
// already in the tool's list are appended as the remainder.
// toolOrder may be nil (returns defaultOrder unchanged).
func MergeMethodOrder(toolOrder, defaultOrder []string) []string {
	if toolOrder == nil {
		return defaultOrder
	}
	seen := make(map[string]bool, len(toolOrder))
	merged := make([]string, 0, len(defaultOrder))
	for _, k := range toolOrder {
		merged = append(merged, k)
		seen[k] = true
	}
	for _, k := range defaultOrder {
		if !seen[k] {
			merged = append(merged, k)
		}
	}
	return merged
}

// ExpandBuckets replaces bucket names in a method order list with their
// constituent concrete method kinds. Delegates to methodkind.ExpandBuckets.
func ExpandBuckets(order []string) []string {
	return methodkind.ExpandBuckets(order)
}

// EffectiveMethodOrder returns the method order effective for a given tool,
// considering per-tool MethodOnly (exclusive), MethodPrefer (preferred prefix),
// or defaultOrder (fallback).
// When nativeManagerName is a specific distro manager (e.g. "apt", "pacman"),
// native manager references in the order are expanded. Pass empty string or "native"
// to skip expansion (appropriate at schema-parse time).
func EffectiveMethodOrder(tool *Tool, defaultOrder []string, nativeManagerName string) []string {
	needsExpand := nativeManagerName != "" && nativeManagerName != "native"

	// Always expand bucket names in the default order first.
	defaultOrder = ExpandBuckets(defaultOrder)

	// method_only: exclusive list — no remainder from defaults.
	if len(tool.MethodOnly) > 0 {
		toolList := ExpandBuckets(tool.MethodOnly)
		if needsExpand {
			return ExpandMethodOrder(toolList, nativeManagerName)
		}
		return toolList
	}

	// method_prefer: prefix + remainder from defaults.
	if len(tool.MethodPrefer) > 0 {
		toolList := ExpandBuckets(tool.MethodPrefer)
		if needsExpand {
			expDefault := ExpandMethodOrder(defaultOrder, nativeManagerName)
			expTool := ExpandMethodOrder(toolList, nativeManagerName)
			return MergeMethodOrder(expTool, expDefault)
		}
		return MergeMethodOrder(toolList, defaultOrder)
	}

	if needsExpand {
		return ExpandMethodOrder(defaultOrder, nativeManagerName)
	}
	return defaultOrder
}
