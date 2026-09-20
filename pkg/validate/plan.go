package validate

import (
	"fmt"
	"sort"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/planner"
)

// validatePlanIntents checks the same adapter-neutral static plan consumed by
// execution before any host-dependent availability probe is attempted.
func validatePlanIntents(s *config.Schema) *Result {
	r := &Result{}
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, toolName := range names {
		tool := s.Tools[toolName]
		for i, method := range tool.Methods {
			contract, ok := methodkind.Lookup(method.Kind)
			if !ok {
				continue
			}
			intent, err := planner.BuildCandidateIntent(tool, method)
			if err != nil {
				r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, ""), Message: err.Error()})
				continue
			}
			missing, err := contract.MissingPlanCapabilities(intent)
			if err != nil {
				r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, ""), Message: err.Error()})
				continue
			}
			if missing != 0 {
				r.Add(ValidationError{
					Code:    ErrInvalidValue,
					Field:   fieldPath(toolName, i, ""),
					Message: fmt.Sprintf("method %q cannot honor requested capabilities: %v", method.Kind, methodkind.CapabilityNames(missing)),
				})
			}
		}
	}
	return r
}
