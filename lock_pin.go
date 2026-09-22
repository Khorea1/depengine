package main

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/state"
)

// lockPinFor looks up a tool's canonical "<tool>/<kind>/<idx>" pin only when
// that lookup is unambiguous. Callers with durable candidate identity should
// use lockPinForToolState or lockPinForCandidate instead.
func lockPinFor(l *lock.Lock, tool, kind string) (lock.ToolPin, bool) {
	if l == nil {
		return lock.ToolPin{}, false
	}
	prefix := tool + "/"
	if kind != "" {
		prefix += kind + "/"
	}
	var (
		matched lock.ToolPin
		found   bool
	)
	for key, pin := range l.Tools {
		if !strings.HasPrefix(key, prefix) || pin.Latest == "" {
			continue
		}
		if found {
			return lock.ToolPin{}, false
		}
		matched = pin
		found = true
	}
	return matched, found
}

// lockPinForCandidate resolves the exact lock key for a concrete schema
// candidate. The index is ordinal within its method kind, matching internal/lock's
// canonical writer. Pointer identity is deliberate: callers resolve the
// candidate from the same normalized Tool whose Methods slice lock.Apply uses.
func lockPinForCandidate(l *lock.Lock, toolName string, tool *config.Tool, candidate *config.MethodCandidate) (lock.ToolPin, bool) {
	if l == nil || tool == nil || candidate == nil {
		return lock.ToolPin{}, false
	}
	kindIndex := 0
	for _, method := range tool.Methods {
		if method == nil || method.Kind != candidate.Kind {
			continue
		}
		if method == candidate {
			key := fmt.Sprintf("%s/%s/%d", toolName, candidate.Kind, kindIndex)
			pin, ok := l.Tools[key]
			return pin, ok && pin.Latest != ""
		}
		kindIndex++
	}
	return lock.ToolPin{}, false
}

// findStateMethodCandidate resolves durable ToolState to one exact candidate in
// the current tool definition. A persisted display label is authoritative. Old
// state that only remembers a kind is accepted only when that kind is unique.
func findStateMethodCandidate(tool *config.Tool, ts state.ToolState) (*config.MethodCandidate, error) {
	if tool == nil {
		return nil, fmt.Errorf("tool definition is required")
	}
	kind := ts.MethodKind
	if kind == "" {
		kind = ts.Method
	}
	if kind == "" {
		return nil, fmt.Errorf("tracked method kind is empty")
	}
	label := ""
	if ts.Method != "" && ts.Method != kind {
		label = ts.Method
	}

	matches := make([]*config.MethodCandidate, 0, 1)
	for _, method := range tool.Methods {
		if method == nil || method.Kind != kind {
			continue
		}
		if label != "" && method.Label != label {
			continue
		}
		matches = append(matches, method)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		if label != "" {
			return nil, fmt.Errorf("tracked candidate %q (kind %q) is absent from the current schema", label, kind)
		}
		return nil, fmt.Errorf("tracked method kind %q is absent from the current schema", kind)
	}
	return nil, fmt.Errorf("tracked method kind %q is ambiguous across %d current schema candidates", kind, len(matches))
}

// lockPinForToolState resolves a pin through the exact candidate represented by
// durable state. It deliberately returns false for ambiguous legacy state
// instead of selecting an arbitrary same-kind pin.
func lockPinForToolState(l *lock.Lock, toolName string, tool *config.Tool, ts state.ToolState) (lock.ToolPin, bool) {
	candidate, err := findStateMethodCandidate(tool, ts)
	if err != nil {
		return lock.ToolPin{}, false
	}
	return lockPinForCandidate(l, toolName, tool, candidate)
}
