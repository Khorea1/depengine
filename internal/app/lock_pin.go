package app

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/state"
)

func pinnedVersion(pin lock.ToolPin) string {
	if pin.Latest != "" {
		return pin.Latest
	}
	return pin.PackageVersion
}

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
			if !ok || pinnedVersion(pin) == "" {
				return lock.ToolPin{}, false
			}
			if pin.PackageVersion != "" && !lock.MatchesPackagePin(toolName, candidate, pin) {
				return lock.ToolPin{}, false
			}
			return pin, true
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
	return nil, fmt.Errorf("tracked method kind %q is ambiguous across %d current schema candidates (%s): state records only kind %q; %s", kind, len(matches), describeAmbiguousCandidates(tool, matches), kind, ambiguousCandidateRemediation(matches))
}

func ambiguousCandidateRemediation(matches []*config.MethodCandidate) string {
	for _, match := range matches {
		if match != nil && match.Label != "" {
			return fmt.Sprintf("re-install selecting a labeled candidate (e.g. method = %q) to persist exact candidate identity", match.Label)
		}
	}
	return "add distinct labels to the same-kind candidates, then re-install selecting one label to persist exact candidate identity"
}

// describeAmbiguousCandidates renders each ambiguous match as `#<ordinal> "<display>"`
// so upgrade/remove/lock failures point at the exact schema candidates that
// collide. Ordinals are positions in the merged Tool.Methods list, matching
// `depengine why` candidate inspection.
func describeAmbiguousCandidates(tool *config.Tool, matches []*config.MethodCandidate) string {
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		ordinal := -1
		for candidate, declared := range tool.Methods {
			if declared == match {
				ordinal = candidate
				break
			}
		}
		parts = append(parts, fmt.Sprintf("#%d %q", ordinal, candidateDisplayName(match)))
	}
	return strings.Join(parts, ", ")
}

// candidateDisplayName mirrors exec.MethodAttempt.DisplayName without importing
// the exec package: the custom label when present, otherwise the method kind.
func candidateDisplayName(method *config.MethodCandidate) string {
	if method == nil {
		return ""
	}
	if method.Label != "" {
		return method.Label
	}
	return method.Kind
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
