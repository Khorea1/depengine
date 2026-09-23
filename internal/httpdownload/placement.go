// Package httpdownload implements the HTTP(S) download method family
// (http, github, appimage) plus archive extraction and ownership.
//
// placement.go is the single source of truth for artifact placement
// defaults: which install root and link directory a candidate targets.
//
// ADR-004 (docs/design/adr-004-scope-placement.md):
//
//  1. Portable scope vocabulary (user/system) supplies platform-native
//     placement defaults resolved host-independently in plan.  Darwin and
//     the BSDs follow the same Unix XDG model as Linux.
//  2. Explicit extract_to/install_dir and link_dir are advanced overrides
//     and win independently, but must be absolute in the target OS path
//     model.
//  3. Unix paths disappear from normal authoring: a scope-less manifest
//     keeps today's legacy defaults unchanged; a scoped manifest must not
//     spell ~/.local/bin or /usr/local/bin.
//
// Every consumer (plan resolution, install, check/observe, remove,
// elevation) derives placement through ArtifactPlacement so all verdicts
// agree on the same target paths.
package httpdownload

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

// scopeConfigured reports whether the candidate declares portable scope.
// Config validation has already restricted the value to "user"/"system" for
// methods that declare the scope field; legacy candidates never get here.
func scopeConfigured(mc *config.MethodCandidate) bool {
	return strings.TrimSpace(scopeValue(mc)) != ""
}

// ArtifactPlacement resolves the effective (install root, link dir) pair for
// an artifact candidate under ADR-004 precedence:
//
//	explicit extract_to/install_dir, link_dir  → advanced overrides (win)
//	declared scope                            → platform-native placement
//	otherwise                                 → method legacy defaults
//
// The returned placement is the pair every consumer must agree on. Overrides
// are validated as absolute target paths when scope is declared; scope-less
// candidates keep their historical string behavior untouched.
func ArtifactPlacement(tool *config.Tool, mc *config.MethodCandidate, legacyInstallRoot, legacyLinkDir string) (plan.ScopePlacement, error) {
	overrideRoot := config.ExpandHomeDir(installDirConfig(mc))
	overrideLink := config.ExpandHomeDir(linkDirConfig(mc))
	if !scopeConfigured(mc) {
		return legacyPlacement(overrideRoot, overrideLink, legacyInstallRoot, legacyLinkDir), nil
	}
	scope, err := plan.ParseScope(scopeValue(mc))
	if err != nil {
		return plan.ScopePlacement{}, fmt.Errorf("http: placement: %w", err)
	}
	placement, err := plan.ResolveScopePlacement(scope, runtime.GOOS, toolName(tool), hostScopeRoots(), plan.PlacementOverrides{
		InstallRoot: overrideRoot,
		LinkDir:     overrideLink,
	})
	if err != nil {
		return plan.ScopePlacement{}, fmt.Errorf("http: placement: %w", err)
	}
	return placement, nil
}

// legacyPlacement applies the advanced path overrides on top of the method's
// historical defaults. Used for scope-less candidates and as the fail-safe
// fallback of PlacementOrDefault.
func legacyPlacement(overrideRoot, overrideLink, legacyInstallRoot, legacyLinkDir string) plan.ScopePlacement {
	installRoot := config.ExpandHomeDir(legacyInstallRoot)
	if overrideRoot != "" {
		installRoot = overrideRoot
	}
	linkDir := config.ExpandHomeDir(legacyLinkDir)
	if overrideLink != "" {
		linkDir = overrideLink
	}
	return plan.ScopePlacement{InstallRoot: installRoot, LinkDir: linkDir}
}

// PlacementOrDefault is ArtifactPlacement for call contexts that cannot
// surface a placement error (Check, elevation). On resolution failure it
// returns the legacy defaults, which keeps those probes from fabricating a
// target path; real installs and removal use the error-propagating form and
// fail with the placement diagnostic.
func PlacementOrDefault(tool *config.Tool, mc *config.MethodCandidate, legacyInstallRoot, legacyLinkDir string) plan.ScopePlacement {
	placement, err := ArtifactPlacement(tool, mc, legacyInstallRoot, legacyLinkDir)
	if err != nil {
		return legacyPlacement(config.ExpandHomeDir(installDirConfig(mc)), config.ExpandHomeDir(linkDirConfig(mc)), legacyInstallRoot, legacyLinkDir)
	}
	return placement
}

func scopeValue(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	scope, _ := mc.Config["scope"].(string)
	return scope
}

// installDirConfig returns the explicit destination root (extract_to for
// http/github, install_dir for appimage); "" when unset.
func installDirConfig(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	if value, ok := mc.Config["extract_to"].(string); ok && value != "" {
		return value
	}
	value, _ := mc.Config["install_dir"].(string)
	return value
}

func linkDirConfig(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	value, _ := mc.Config["link_dir"].(string)
	return value
}

func toolName(tool *config.Tool) string {
	if tool == nil {
		return ""
	}
	return tool.Name
}

// hostScopeRoots collects the target-host roots placement needs from the
// invoking process. Planning is host-independent (the function performs no
// env access), but the runtime target is the invoking host: HOME plus the
// XDG variables on Unix, LocalAppData/ProgramFiles on Windows. Missing or
// relative XDG values fall back to their specification defaults inside
// plan.ResolveScopePlacement.
func hostScopeRoots() plan.ScopeRoots {
	return plan.ScopeRoots{
		HomeDir:       homeDir(),
		XDGDataHome:   os.Getenv("XDG_DATA_HOME"),
		XDGStateHome:  os.Getenv("XDG_STATE_HOME"),
		XDGCacheHome:  os.Getenv("XDG_CACHE_HOME"),
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		LocalAppData:  os.Getenv("LOCALAPPDATA"),
		ProgramFiles:  os.Getenv("ProgramFiles"),
	}
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
