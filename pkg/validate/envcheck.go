package validate

import (
	"context"
	"fmt"
	"sort"

	"depengine/pkg/run"
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

// envToolBinaries lists the common tool binaries to check, grouped by kind.
// These cover the native package managers, language-ecosystem tools, and
// CLI utilities used by depengine adapters.
var envToolBinaries = []struct {
	Name string
	Kind string
}{
	// Native package managers (distro / OS).
	{"apt", "native"},
	{"apt-get", "native"},
	{"dnf", "native"},
	{"pacman", "native"},
	{"zypper", "native"},
	{"brew", "native"},
	{"apk", "native"},
	{"xbps-install", "native"},
	{"emerge", "native"},
	{"opkg", "native"},
	{"pkg", "native"},
	{"pkg_add", "native"},

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

// CheckEnv probes for each known tool binary on PATH via `which`.
// All findings are warnings — a missing tool is environment-specific
// and does not necessarily indicate a schema problem.
func CheckEnv(ctx context.Context, rn run.Runner) *EnvCheckResult {
	result := &EnvCheckResult{}

	// Deduplicate binary names across entries (some names appear in
	// multiple places, e.g. "pkg" for both termux and freebsd).
	seen := make(map[string]bool)

	for _, entry := range envToolBinaries {
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

