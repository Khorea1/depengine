package plan

import (
	"fmt"
	"path"
	"strings"
)

// ScopeRoots supplies target-platform roots without consulting the invoking
// process environment. This keeps planning deterministic when the target host
// differs from the planner host (notably for cross-target manifests).
//
// On Unix, XDG values model the target user's environment. Relative XDG paths
// are ignored and replaced by the XDG Base Directory specification defaults,
// because the specification requires these variables to contain absolute paths.
type ScopeRoots struct {
	HomeDir       string
	XDGDataHome   string
	XDGStateHome  string
	XDGCacheHome  string
	XDGConfigHome string
	LocalAppData  string
	ProgramFiles  string
}

// PlacementOverrides are advanced path controls. Non-empty values take
// precedence over scope-derived defaults independently, so users can override
// only the payload root or only the executable/link directory.
type PlacementOverrides struct {
	InstallRoot string
	LinkDir     string
}

// ScopePlacement is the canonical placement produced for artifact-style
// installs. Raw binaries and archives consume the same install/link pair.
// State/cache/config roots are included so all depengine-owned Unix paths derive
// from the same XDG-aware policy instead of being rediscovered by adapters.
type ScopePlacement struct {
	InstallRoot string `json:"install_root"`
	LinkDir     string `json:"link_dir"`
	StateRoot   string `json:"state_root,omitempty"`
	CacheRoot   string `json:"cache_root,omitempty"`
	ConfigRoot  string `json:"config_root,omitempty"`
}

// UnixXDGPaths is the normalized target-user view of the XDG base directories.
// UserBin intentionally remains separate: the XDG Base Directory specification
// defines no XDG_BIN_HOME variable, so ~/.local/bin is used by convention.
type UnixXDGPaths struct {
	DataHome   string
	StateHome  string
	CacheHome  string
	ConfigHome string
	UserBin    string
}

// ResolveUnixXDGPaths normalizes XDG roots without reading environment
// variables. Empty or relative XDG values use the specification defaults under
// home. This mirrors the XDG rule that relative values are invalid and should
// not be used.
func ResolveUnixXDGPaths(roots ScopeRoots) (UnixXDGPaths, error) {
	if roots.HomeDir == "" || !path.IsAbs(roots.HomeDir) {
		return UnixXDGPaths{}, fmt.Errorf("unix user scope requires an absolute HomeDir root")
	}

	return UnixXDGPaths{
		DataHome:   xdgOrDefault(roots.XDGDataHome, path.Join(roots.HomeDir, ".local", "share")),
		StateHome:  xdgOrDefault(roots.XDGStateHome, path.Join(roots.HomeDir, ".local", "state")),
		CacheHome:  xdgOrDefault(roots.XDGCacheHome, path.Join(roots.HomeDir, ".cache")),
		ConfigHome: xdgOrDefault(roots.XDGConfigHome, path.Join(roots.HomeDir, ".config")),
		UserBin:    path.Join(roots.HomeDir, ".local", "bin"),
	}, nil
}

func xdgOrDefault(value, fallback string) string {
	if value != "" && path.IsAbs(value) {
		return path.Clean(value)
	}
	return fallback
}

// ResolveScopePlacement maps portable scope into deterministic target-native
// paths. It performs no filesystem or environment access. Explicit advanced
// paths override the scope defaults after those defaults have been validated.
func ResolveScopePlacement(scope Scope, goos, tool string, roots ScopeRoots, overrides PlacementOverrides) (ScopePlacement, error) {
	if err := scope.Validate(); err != nil {
		return ScopePlacement{}, err
	}
	if err := validateToolPathComponent(tool); err != nil {
		return ScopePlacement{}, err
	}

	var placement ScopePlacement
	switch goos {
	case "windows":
		switch scope {
		case ScopeUser:
			if roots.LocalAppData == "" {
				return ScopePlacement{}, fmt.Errorf("windows user scope requires LocalAppData root")
			}
			placement.InstallRoot = windowsJoin(roots.LocalAppData, "depengine", "tools", tool)
			placement.LinkDir = windowsJoin(roots.LocalAppData, "depengine", "bin")
		case ScopeSystem:
			if roots.ProgramFiles == "" {
				return ScopePlacement{}, fmt.Errorf("windows system scope requires ProgramFiles root")
			}
			placement.InstallRoot = windowsJoin(roots.ProgramFiles, "depengine", "tools", tool)
			placement.LinkDir = windowsJoin(roots.ProgramFiles, "depengine", "bin")
		}
	case "linux", "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		switch scope {
		case ScopeUser:
			xdg, err := ResolveUnixXDGPaths(roots)
			if err != nil {
				return ScopePlacement{}, fmt.Errorf("%s: %w", goos, err)
			}
			placement.InstallRoot = path.Join(xdg.DataHome, "depengine", "tools", tool)
			placement.LinkDir = xdg.UserBin
			placement.StateRoot = path.Join(xdg.StateHome, "depengine")
			placement.CacheRoot = path.Join(xdg.CacheHome, "depengine")
			placement.ConfigRoot = path.Join(xdg.ConfigHome, "depengine")
		case ScopeSystem:
			placement.InstallRoot = path.Join("/opt", "depengine", "tools", tool)
			placement.LinkDir = "/usr/local/bin"
			placement.StateRoot = "/var/lib/depengine"
			placement.CacheRoot = "/var/cache/depengine"
			placement.ConfigRoot = "/etc/depengine"
		}
	default:
		return ScopePlacement{}, fmt.Errorf("unsupported target OS %q for scope placement", goos)
	}

	if overrides.InstallRoot != "" {
		placement.InstallRoot = overrides.InstallRoot
	}
	if overrides.LinkDir != "" {
		placement.LinkDir = overrides.LinkDir
	}
	return placement, nil
}

func validateToolPathComponent(tool string) error {
	if tool == "" || tool == "." || tool == ".." || strings.TrimSpace(tool) != tool {
		return fmt.Errorf("invalid tool path component %q", tool)
	}
	if strings.ContainsAny(tool, `/\\\x00`) {
		return fmt.Errorf("invalid tool path component %q", tool)
	}
	return nil
}

func windowsJoin(root string, elems ...string) string {
	out := strings.TrimRight(strings.ReplaceAll(root, "/", `\`), `\`)
	for _, elem := range elems {
		if elem == "" {
			continue
		}
		out += `\` + strings.Trim(elem, `/\`)
	}
	return out
}
