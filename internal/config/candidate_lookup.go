package config

import "fmt"

// FindMethodCandidate resolves one exact configured candidate by kind and optional label.
// An empty label is accepted only when the method kind is unique on the tool.
func FindMethodCandidate(tool *Tool, kind, label string) (*MethodCandidate, error) {
	if tool == nil {
		return nil, fmt.Errorf("tool definition is required")
	}
	if kind == "" {
		return nil, fmt.Errorf("method kind is required")
	}
	var match *MethodCandidate
	count := 0
	for _, method := range tool.Methods {
		if method == nil || method.Kind != kind || (label != "" && method.Label != label) {
			continue
		}
		match = method
		count++
	}
	if count == 1 {
		return match, nil
	}
	if count == 0 {
		if label != "" {
			return nil, fmt.Errorf("candidate %q (kind %q) is absent from tool %q", label, kind, tool.Name)
		}
		return nil, fmt.Errorf("method kind %q is absent from tool %q", kind, tool.Name)
	}
	if label != "" {
		return nil, fmt.Errorf("candidate %q (kind %q) is ambiguous in tool %q", label, kind, tool.Name)
	}
	return nil, fmt.Errorf("method kind %q is ambiguous in tool %q (%d candidates)", kind, tool.Name, count)
}
