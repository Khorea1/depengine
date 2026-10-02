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

// envToolBinary describes one executable checked by validate --check-env.
type envToolBinary struct {
	Name string
	Kind string
}

// supplementalEnvToolBinaries contains only non-native tools. Native manager
// executables come from internal/native so support cannot drift from validation.
var supplementalEnvToolBinaries = []envToolBinary{
	// Language-ecosystem package managers.
	{Name: "cargo", Kind: "lang"},
	{Name: "go", Kind: "lang"},
	{Name: "npm", Kind: "lang"},
	{Name: "pip", Kind: "lang"},
	{Name: "pipx", Kind: "lang"},
	{Name: "uv", Kind: "lang"},
	{Name: "gem", Kind: "lang"},
	{Name: "yarn", Kind: "lang"},
	{Name: "yarn-berry", Kind: "lang"},

	// System CLI tools required by adapters.
	{Name: "git", Kind: "system"},
	{Name: "curl", Kind: "system"},
	{Name: "wget", Kind: "system"},
}

func envToolBinaries() []envToolBinary {
	nativeExecutables := native.ManagerExecutables()
	out := make([]envToolBinary, 0, len(nativeExecutables)+len(supplementalEnvToolBinaries))
	for _, name := range nativeExecutables {
		out = append(out, envToolBinary{Name: name, Kind: "native"})
	}
	out = append(out, supplementalEnvToolBinaries...)
	return out
}

// CheckEnv probes for each known tool binary on PATH via `which`.
// All findings are warnings — a missing tool is environment-specific
// and does not necessarily indicate a schema problem.
func CheckEnv(ctx context.Context, rn run.Runner) *EnvCheckResult {
	result := &EnvCheckResult{}

	// Deduplicate binary names across entries (some names appear in
	// multiple places, e.g. "pkg" for both termux and freebsd).
	seen := make(map[string]bool)

	for _, entry := range envToolBinaries() {
		if seen[entry.Name] {
			continue
		}
		seen[entry.Name] = true

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
