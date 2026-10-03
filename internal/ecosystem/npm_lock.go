package ecosystem

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

var npmRegistryPackage = regexp.MustCompile(`^(?:[a-z0-9][a-z0-9._~-]*|@[a-z0-9][a-z0-9._~-]*/[a-z0-9][a-z0-9._~-]*)$`)

// ValidNPMVersion accepts concrete SemVer package versions, not dist-tags or
// version ranges that could resolve differently on a later install.
func ValidNPMVersion(version string) bool {
	if version == "" || strings.ContainsRune(version, '\x00') {
		return false
	}
	coreAndBuild := strings.SplitN(version, "+", 2)
	if len(coreAndBuild) == 2 && !validNPMVersionIdentifiers(coreAndBuild[1], false) {
		return false
	}
	coreAndPre := strings.SplitN(coreAndBuild[0], "-", 2)
	core := strings.Split(coreAndPre[0], ".")
	if len(core) != 3 {
		return false
	}
	for _, part := range core {
		if !validNPMCoreNumber(part) {
			return false
		}
	}
	return len(coreAndPre) == 1 || validNPMVersionIdentifiers(coreAndPre[1], true)
}

func validNPMCoreNumber(value string) bool {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validNPMVersionIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		numeric := true
		for _, char := range identifier {
			if char < '0' || char > '9' {
				numeric = false
			}
			if (char < '0' || char > '9') &&
				(char < 'A' || char > 'Z') &&
				(char < 'a' || char > 'z') &&
				char != '-' {
				return false
			}
		}
		if rejectNumericLeadingZero && numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

// IsNPMRegistryPackage excludes aliases, URLs, paths, and already-versioned
// package specs whose install semantics cannot be replayed as pkg@version.
func IsNPMRegistryPackage(pkg string) bool { return npmRegistryPackage.MatchString(pkg) }

// ResolveLatestNPMVersion reads the registry's latest dist-tag without
// modifying the host. The returned version is safe to append to a package
// name as the exact npm install target.
func ResolveLatestNPMVersion(ctx context.Context, rn run.Runner, pkg, registry string) (string, error) {
	if !IsNPMRegistryPackage(pkg) {
		return "", fmt.Errorf("npm: %q is not a plain registry package name", pkg)
	}
	args := []string{"view", pkg, "dist-tags.latest", "--json"}
	if registry != "" {
		args = append(args, "--registry", registry)
	}
	result := rn.Run(ctx, "npm", args...)
	if err := run.CheckResultCompleteOutput(result, "npm: resolve latest version"); err != nil {
		return "", err
	}
	var version string
	if err := json.Unmarshal(result.Stdout, &version); err != nil || !ValidNPMVersion(version) || strings.ContainsRune(version, '\x00') {
		return "", fmt.Errorf("npm: latest dist-tag for %q did not resolve to a concrete version", pkg)
	}
	return version, nil
}

// ResolveLatestPNPMVersion reads the npm registry latest dist-tag through pnpm
// without modifying the host. The returned version is safe to append to a
// package name as an exact pnpm install target.
type yarnInfoOutput struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// ResolveLatestYarnVersion reads the npm registry latest dist-tag through Yarn
// Classic without modifying the host.
func ResolveLatestYarnVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	if !IsNPMRegistryPackage(pkg) {
		return "", fmt.Errorf("yarn: %q is not a plain registry package name", pkg)
	}
	result := rn.Run(ctx, "yarn", "info", pkg, "version", "--json")
	if err := run.CheckResultCompleteOutput(result, "yarn: resolve latest version"); err != nil {
		return "", err
	}
	var output yarnInfoOutput
	if err := json.Unmarshal(result.Stdout, &output); err != nil || output.Type != "inspect" || !ValidNPMVersion(output.Data) {
		return "", fmt.Errorf("yarn: latest dist-tag for %q did not resolve to a concrete version", pkg)
	}
	return output.Data, nil
}

func ResolveLatestPNPMVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	if !IsNPMRegistryPackage(pkg) {
		return "", fmt.Errorf("pnpm: %q is not a plain registry package name", pkg)
	}
	result := rn.Run(ctx, "pnpm", "view", pkg, "dist-tags.latest", "--json")
	if err := run.CheckResultCompleteOutput(result, "pnpm: resolve latest version"); err != nil {
		return "", err
	}
	var version string
	if err := json.Unmarshal(result.Stdout, &version); err != nil || !ValidNPMVersion(version) {
		return "", fmt.Errorf("pnpm: latest dist-tag for %q did not resolve to a concrete version", pkg)
	}
	return version, nil
}
