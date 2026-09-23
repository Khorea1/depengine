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
	if !validUnixAbsolutePath(roots.HomeDir) {
		return UnixXDGPaths{}, fmt.Errorf("unix user scope requires an absolute HomeDir root")
	}

	dataHome, err := xdgOrDefault("XDGDataHome", roots.XDGDataHome, path.Join(roots.HomeDir, ".local", "share"))
	if err != nil {
		return UnixXDGPaths{}, err
	}
	stateHome, err := xdgOrDefault("XDGStateHome", roots.XDGStateHome, path.Join(roots.HomeDir, ".local", "state"))
	if err != nil {
		return UnixXDGPaths{}, err
	}
	cacheHome, err := xdgOrDefault("XDGCacheHome", roots.XDGCacheHome, path.Join(roots.HomeDir, ".cache"))
	if err != nil {
		return UnixXDGPaths{}, err
	}
	configHome, err := xdgOrDefault("XDGConfigHome", roots.XDGConfigHome, path.Join(roots.HomeDir, ".config"))
	if err != nil {
		return UnixXDGPaths{}, err
	}

	return UnixXDGPaths{
		DataHome:   dataHome,
		StateHome:  stateHome,
		CacheHome:  cacheHome,
		ConfigHome: configHome,
		UserBin:    path.Join(roots.HomeDir, ".local", "bin"),
	}, nil
}

func xdgOrDefault(name, value, fallback string) (string, error) {
	if value == "" || !path.IsAbs(value) {
		return fallback, nil
	}
	if !validUnixAbsolutePath(value) {
		return "", fmt.Errorf("%s must be a well-formed absolute Unix path", name)
	}
	return path.Clean(value), nil
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
		if err := validateWindowsPathComponent(tool); err != nil {
			return ScopePlacement{}, fmt.Errorf("tool: %w", err)
		}
		switch scope {
		case ScopeUser:
			if !validWindowsAbsolutePath(roots.LocalAppData) {
				return ScopePlacement{}, fmt.Errorf("windows user scope requires LocalAppData root")
			}
			placement.InstallRoot = windowsJoin(roots.LocalAppData, "depengine", "tools", tool)
			placement.LinkDir = windowsJoin(roots.LocalAppData, "depengine", "bin")
		case ScopeSystem:
			if !validWindowsAbsolutePath(roots.ProgramFiles) {
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
		if err := validateTargetAbsolutePath(goos, overrides.InstallRoot); err != nil {
			return ScopePlacement{}, fmt.Errorf("install root override: %w", err)
		}
		placement.InstallRoot = overrides.InstallRoot
	}
	if overrides.LinkDir != "" {
		if err := validateTargetAbsolutePath(goos, overrides.LinkDir); err != nil {
			return ScopePlacement{}, fmt.Errorf("link dir override: %w", err)
		}
		placement.LinkDir = overrides.LinkDir
	}
	return placement, nil
}

func validateTargetAbsolutePath(goos, value string) error {
	switch goos {
	case "windows":
		if !validWindowsAbsolutePath(value) {
			return fmt.Errorf("path %q must be an absolute Windows path", value)
		}
	case "linux", "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		if !validUnixAbsolutePath(value) {
			return fmt.Errorf("path %q must be an absolute Unix path", value)
		}
	default:
		return fmt.Errorf("unsupported target OS %q", goos)
	}
	return nil
}

func validUnixAbsolutePath(value string) bool {
	return value != "" &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsRune(value, '\x00') &&
		path.IsAbs(value) &&
		path.Clean(value) == value
}

// validWindowsAbsolutePath is intentionally host-independent: planning a
// Windows target on Unix must not reinterpret C:\\... as a relative Unix path.
func validWindowsAbsolutePath(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
		return false
	}
	normalized := strings.ReplaceAll(value, "/", `\`)
	if len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '\\' {
		c := normalized[0]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
		return validWindowsPathTail(strings.TrimPrefix(normalized[3:], `\`))
	}
	if strings.HasPrefix(normalized, `\\`) {
		parts := strings.Split(strings.TrimPrefix(normalized, `\\`), `\`)
		if len(parts) < 2 || !validWindowsUNCHead(parts[0]) || !validWindowsUNCHead(parts[1]) {
			return false
		}
		return validWindowsPathParts(parts[2:])
	}
	return false
}

func validWindowsPathTail(tail string) bool {
	if tail == "" {
		return true
	}
	return validWindowsPathParts(strings.Split(tail, `\`))
}

func validWindowsPathParts(parts []string) bool {
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		if err := validateWindowsPathComponent(part); err != nil {
			return false
		}
	}
	return true
}

func validWindowsUNCHead(value string) bool {
	return value != "" && value != "." && value != ".." &&
		!strings.ContainsRune(value, ':') && !strings.HasSuffix(value, ".") && !strings.HasSuffix(value, " ")
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

func validateWindowsPathComponent(value string) error {
	if strings.ContainsRune(value, ':') || strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		return fmt.Errorf("invalid Windows path component %q", value)
	}
	base := value
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("reserved Windows path component %q", value)
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
