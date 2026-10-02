package validate

import (
	"context"
	"fmt"
	"sort"

	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/run"
)

// EnvCheck reports the availability of a system tool on the current PATH.
type EnvCheck struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "native", "lang", "system"
	Found   bool   `json:"found"`
	Message string `json:"message,omitempty"`
}

// EnvCheckResult aggregates environment checks.
type EnvCheckResult struct {
	Checks []EnvCheck `json:"checks"`
}

// AddWarning appends a check; all env checks are warnings.
func (r *EnvCheckResult) AddWarning(name, kind, message string) {
	r.Checks = append(r.Checks, EnvCheck{
		Name:    name,
		Kind:    kind,
		Found:   false,
		Message: message,
	})
}

// supplementalEnvToolBinaries contains non-native tools that are not owned by
// the native package-manager registry. Native executables are derived from
// native.ManagerExecutableNames at check time.
var supplementalEnvToolBinaries = []struct {
	Name string
	Kind string
}{
	// Language-ecosystem package managers.
	{"cargo", "lang"},
	{"go", "lang"},
	{"npm", "lang"},
	{"pip", "lang"},
	{"pipx", "lang"},
	{"uv", "lang"},
	{"gem", "lang"},
	{"yarn", "lang"},
	{"yarn-berry", "lang"},

	// System CLI tools required by adapters.
	{"git", "system"},
	{"curl", "system"},
	{"wget", "system"},
}

func effectiveEnvToolBinaries() []struct {
	Name string
	Kind string
} {
	type entry = struct {
		Name string
		Kind string
	}
	byName := make(map[string]entry, len(supplementalEnvToolBinaries)+len(native.ManagerExecutableNames()))
	for _, supplemental := range supplementalEnvToolBinaries {
		byName[supplemental.Name] = entry(supplemental)
	}
	// Native wins on kind when a binary is also present in a supplemental list.
	for _, name := range native.ManagerExecutableNames() {
		byName[name] = entry{Name: name, Kind: "native"}
	}
	out := make([]entry, 0, len(byName))
	for _, item := range byName {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// CheckEnv probes for each known tool binary on PATH via `which`.
// All findings are warnings — a missing tool is environment-specific
// and does not necessarily indicate a schema problem.
func CheckEnv(ctx context.Context, rn run.Runner) *EnvCheckResult {
	result := &EnvCheckResult{}

	for _, entry := range effectiveEnvToolBinaries() {
		found := run.LookPath(ctx, rn, entry.Name)
		check := EnvCheck{
			Name:  entry.Name,
			Kind:  entry.Kind,
			Found: found,
		}
		if !found {
			check.Message = fmt.Sprintf("%s not found on PATH", entry.Name)
		}
		result.Checks = append(result.Checks, check)
	}

	// Sort by kind then by name for stable output.
	sort.Slice(result.Checks, func(i, j int) bool {
		if result.Checks[i].Kind != result.Checks[j].Kind {
			return result.Checks[i].Kind < result.Checks[j].Kind
		}
		return result.Checks[i].Name < result.Checks[j].Name
	})

	return result
}
