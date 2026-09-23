package validate

import (
	"sort"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
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
			if _, ok := methodkind.Lookup(method.Kind); !ok {
				continue
			}
			_, err := planner.BuildValidatedCandidateIntent(tool, method, methodkind.CandidateRequirements{})
			if err != nil {
				r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, ""), Message: err.Error()})
			}
		}
	}
	return r
}
