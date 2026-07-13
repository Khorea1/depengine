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

	"depengine/pkg/schema"
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
func Sort(tools map[string]*schema.Tool, opts ...SortOption) ([][]string, error) {
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
			cycle := extractCycle(remaining, children)
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
// following edges until a duplicate is encountered.
func extractCycle(remaining map[string]bool, children map[string][]string) []string {
	visited := map[string]bool{}
	for name := range remaining {
		if visited[name] {
			continue
		}
		path := []string{}
		pathSet := map[string]int{}
		cur := name
		for remaining[cur] {
			if idx, ok := pathSet[cur]; ok {
				// Found a cycle — slice from the first occurrence.
				return append([]string(nil), path[idx:]...)
			}
			pathSet[cur] = len(path)
			path = append(path, cur)
			visited[cur] = true
			// Follow first child that's also in remaining.
			next := ""
			for _, child := range children[cur] {
				if remaining[child] {
					next = child
					break
				}
			}
			if next == "" {
				break // dead end, no cycle from this path
			}
			cur = next
		}
	}
	return []string{"unknown cycle"}
}

// CycleError is returned when a dependency cycle is detected.
type CycleError struct {
	Cycle []string // tools in the cycle, in dependency order
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("dependency cycle detected: %s", strings.Join(e.Cycle, " → "))
}
