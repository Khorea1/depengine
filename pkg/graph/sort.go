// Package graph resolves tool dependency ordering.
//
// The executor uses Sort to determine the installation order: tools with
// no dependencies come first (level 0), then tools whose dependencies are
// all satisfied (level 1), and so on. Cycles are detected and reported
// with the involved tool names.
package graph

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
)

// SortOption configures Sort behavior.
type SortOption func(*sortConfig)

type sortConfig struct {
	logger *slog.Logger
}

// WithLogger sets a logger for debug output during sort.
func WithLogger(l *slog.Logger) SortOption {
	return func(c *sortConfig) {
		c.logger = l
	}
}

// Sort returns the tools in topological order grouped by dependency depth.
// Each level contains tools whose dependencies are all satisfied by tools
// in previous levels. Level 0 has no dependencies.
//
// Uses a single Kahn's algorithm pass for both cycle detection and level
// computation. Returns an error if a cycle is detected (CycleError) or if
// a required tool is missing.
func Sort(tools map[string]*config.Tool, opts ...SortOption) ([][]string, error) {
	if len(tools) == 0 {
		return [][]string{}, nil
	}
	cfg := &sortConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Validate that all required tools exist.
	for name, tool := range tools {
		for _, req := range tool.Requires {
			if _, ok := tools[req]; !ok {
				return nil, fmt.Errorf("graph: tool %q requires %q, which is not in schema", name, req)
			}
		}
	}

	// Build adjacency (children) and compute in-degree.
	inDegree := make(map[string]int, len(tools))
	children := make(map[string][]string, len(tools))
	for name, tool := range tools {
		inDegree[name] = len(tool.Requires)
		for _, dep := range tool.Requires {
			children[dep] = append(children[dep], name)
		}
	}

	// Kahn's algorithm: process tools level by level.
	remaining := make(map[string]bool, len(tools))
	for name := range tools {
		remaining[name] = true
	}

	levels := [][]string{}
	for len(remaining) > 0 {
		level := []string{}
		for name := range remaining {
			if inDegree[name] == 0 {
				level = append(level, name)
			}
		}

		// Sort level for deterministic output.
		sort.Strings(level)
		if len(level) == 0 {
			// Remaining tools all have inDegree > 0 → cycle.
			cycle := extractCycle(remaining, tools)
			return nil, &CycleError{Cycle: cycle}
		}

		for _, name := range level {
			delete(remaining, name)
			for _, child := range children[name] {
				inDegree[child]--
			}
		}
		if cfg.logger != nil {
			cfg.logger.Debug("graph", "level", len(levels), "tools", strings.Join(level, ", "))
		}
		levels = append(levels, level)
	}

	return levels, nil
}

// extractCycle finds a cycle among the remaining (unprocessed) tools by
// following Requires edges until a node repeats.
//
// Every remaining tool requires at least one other remaining tool —
// otherwise Kahn's algorithm would have placed it in a level — so a walk
// along Requires edges inside remaining never dead-ends and the path must
// revisit a node. Start nodes are tried in sorted order so the reported
// cycle is deterministic for a given schema.
func extractCycle(remaining map[string]bool, tools map[string]*config.Tool) []string {
	starts := make([]string, 0, len(remaining))
	for name := range remaining {
		starts = append(starts, name)
	}
	sort.Strings(starts)
	for _, name := range starts {
		path := []string{}
		pathSet := map[string]int{}
		cur := name
		for {
			if idx, ok := pathSet[cur]; ok {
				// Found a cycle — slice from the first occurrence.
				return append([]string(nil), path[idx:]...)
			}
			pathSet[cur] = len(path)
			path = append(path, cur)
			// Follow the first requirement that's also in remaining.
			// At least one exists (Kahn's invariant above).
			for _, req := range tools[cur].Requires {
				if remaining[req] {
					cur = req
					break
				}
			}
		}
	}
	return []string{"unknown cycle"} // unreachable: a non-empty remaining set always contains a cycle
}

// CycleError is returned when a dependency cycle is detected.
type CycleError struct {
	Cycle []string // tools in the cycle, in dependency order
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("dependency cycle detected: %s", strings.Join(e.Cycle, " → "))
}
