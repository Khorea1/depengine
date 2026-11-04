package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
)

// DeclarativeError reports why a recipe cannot enter the catalog. Catalog
// lookup and the import command share this type so both enforce ADR-008
// decision 4 with the same predicate and the same diagnostic shape.
type DeclarativeError struct {
	Recipe   string
	Problems []string
}

func (e *DeclarativeError) Error() string {
	subject := "recipe"
	if e.Recipe != "" {
		subject = fmt.Sprintf("recipe %q", e.Recipe)
	}
	return fmt.Sprintf("%s is not declarative: %s", subject, strings.Join(e.Problems, "; "))
}

// CandidateDisplayName returns the stable name used in diagnostics and in the
// import preview.
func CandidateDisplayName(method *config.MethodCandidate) string {
	if method == nil {
		return "candidate"
	}
	if method.Label != "" {
		return fmt.Sprintf("%s %q", method.Kind, method.Label)
	}
	return method.Kind
}

// ToolDeclarativeFindings reports executable tool-level surfaces.
func ToolDeclarativeFindings(tool *config.Tool) []string {
	if tool == nil {
		return nil
	}
	var findings []string
	if len(tool.PreInstall) > 0 {
		findings = append(findings, "tool pre_install lifecycle hook")
	}
	if len(tool.PostInstall) > 0 {
		findings = append(findings, "tool post_install lifecycle hook")
	}
	return findings
}

// InspectDeclarative reports every executable surface of one candidate:
// lifecycle hooks, command-typed config fields, and a plan that requires
// arbitrary code. It returns an inspection error when the candidate cannot be
// planned at all. The arbitrary-code answer comes from the same predicate the
// execution gate uses, so the catalog and the executor cannot drift.
func InspectDeclarative(tool *config.Tool, method *config.MethodCandidate) ([]string, error) {
	if method == nil {
		return nil, fmt.Errorf("candidate is nil")
	}
	display := CandidateDisplayName(method)

	var findings []string
	if len(method.PreInstall) > 0 {
		findings = append(findings, display+" pre_install lifecycle hook")
	}
	if len(method.PostInstall) > 0 {
		findings = append(findings, display+" post_install lifecycle hook")
	}

	contract, known := methodkind.Lookup(method.Kind)
	if !known {
		return findings, fmt.Errorf("candidate %s uses unknown method kind %q", display, method.Kind)
	}
	for _, key := range sortedConfigKeys(method.Config) {
		if contract.Fields[key].Type == methodkind.Command {
			findings = append(findings, fmt.Sprintf("%s config %q is a command field", display, key))
		}
	}

	intent, err := BuildCandidateIntent(tool, method)
	if err != nil {
		return findings, fmt.Errorf("candidate %s: %w", display, err)
	}
	if methodkind.PlanRequiresArbitraryCode(intent) {
		findings = append(findings, display+" plan requires arbitrary code")
	}
	return findings, nil
}

// ValidateDeclarativeRecipe applies ADR-008 decision 4 to a complete recipe.
// Catalog lookup calls it before returning a recipe; import calls the same
// inspector to build its preview.
func ValidateDeclarativeRecipe(recipe *config.Schema) error {
	if recipe == nil {
		return &DeclarativeError{Problems: []string{"recipe is nil"}}
	}
	var problems []string
	names := make([]string, 0, len(recipe.Tools))
	for name := range recipe.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tool := recipe.Tools[name]
		if tool == nil {
			problems = append(problems, fmt.Sprintf("tool %q is nil", name))
			continue
		}
		for _, finding := range ToolDeclarativeFindings(tool) {
			problems = append(problems, fmt.Sprintf("tool %q: %s", name, finding))
		}
		for _, method := range tool.Methods {
			findings, err := InspectDeclarative(tool, method)
			for _, finding := range findings {
				problems = append(problems, fmt.Sprintf("tool %q: %s", name, finding))
			}
			if err != nil {
				problems = append(problems, fmt.Sprintf("tool %q: %v", name, err))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	recipeName := ""
	if len(names) == 1 {
		recipeName = names[0]
	}
	return &DeclarativeError{Recipe: recipeName, Problems: dedupeStrings(problems)}
}

func sortedConfigKeys(config map[string]any) []string {
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
