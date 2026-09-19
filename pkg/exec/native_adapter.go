package exec

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/native"
	"github.com/Khorea1/depengine/pkg/run"
)

// NativeAdapter wraps pkg/native to implement the Adapter interface.
// It auto-detects the distro clan on first use by probing each known
// native manager binary. This avoids needing the clan at construction.
//
// Sync is NOT handled here — the executor's SyncManager runs index
// synchronization once per session before any tool is installed.
type NativeAdapter struct {
	clan string    // detected on first Available() call
	once sync.Once // ensures detectClan runs exactly once
}

// NewNativeAdapter creates an adapter. Clan is detected automatically.
func NewNativeAdapter(clan string) *NativeAdapter {
	return &NativeAdapter{clan: clan}
}

func (a *NativeAdapter) Kind() string { return "native" }

// detectClan probes known native managers to find one that exists in PATH.
// If a clan was already supplied to NewNativeAdapter (the common case: the
// caller resolved it from OS facts via engine.ResolveFamily), that value is
// authoritative and is never overwritten by probing — probing is only a
// fallback for adapters constructed with an empty clan.
func (a *NativeAdapter) detectClan(ctx context.Context, rn run.Runner) string {
	a.once.Do(func() {
		if a.clan != "" {
			return
		}
		for _, clan := range native.KnownClans() {
			mgr, ok := native.Lookup(clan)
			if !ok {
				continue
			}
			if run.LookPath(ctx, rn, mgr.Name) {
				a.clan = clan
				break
			}
		}
	})
	return a.clan
}

// Available probes each known native manager binary until one is found.
func (a *NativeAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return a.detectClan(ctx, rn) != ""
}

// Check runs the native manager's check command.
func (a *NativeAdapter) Check(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	clan := a.detectClan(ctx, rn)
	if clan == "" {
		return false
	}
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" {
		return false
	}
	cmd := native.BuildCheckCmd(clan, pkg)
	if cmd == nil {
		return false
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return res.Err == nil && res.ExitCode == 0
}

// CheckAvailable reports whether mc's package exists in the detected
// clan's repo/index at all, independent of install status. This is what
// separates "not installed yet" (→ install it) from "not a real package"
// (→ skip, try the next method_order candidate, or fail cleanly) — see
// AvailabilityChecker's doc comment for why this matters for `simple`
// tools. Clans with no SearchCmd configured (native.BuildSearchCmd
// returns nil) fail open and report available, unchanged from before this
// check existed.
func (a *NativeAdapter) CheckAvailable(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	clan := a.detectClan(ctx, rn)
	if clan == "" {
		return true
	}
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" {
		return true
	}
	cmd := native.BuildSearchCmd(clan, pkg)
	if cmd == nil {
		return true
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return res.Err == nil && res.ExitCode == 0
}

// Install runs the install command. Sync is handled by the executor's
// SyncManager, not here.
func (a *NativeAdapter) Install(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) error {
	clan := a.detectClan(ctx, rn)
	if clan == "" {
		return fmt.Errorf("native: no native manager found")
	}
	return runNativeInstall(ctx, rn, "native", clan, mc)
}

// Remove uninstalls a package via the native package manager.
func (a *NativeAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	clan := a.detectClan(ctx, rn)
	if clan == "" {
		return fmt.Errorf("no native manager detected")
	}
	pkgName := pkgFromConfig(mc, clan)
	if pkgName == "" {
		pkgName = tool.Name
	}
	cmd := native.BuildRemoveCmd(clan, pkgName)
	if cmd == nil {
		return fmt.Errorf("no remove command for clan %q", clan)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "native: remove")
}

func (a *NativeAdapter) CanRemove() bool { return true }

// pkgFromConfig extracts the package name from a MethodCandidate's Config.
// When clan is non-empty and the MC has pkg_overrides, it checks for a
// clan-specific override first (e.g. apt→"fd-find" on debian). Falls back
func pkgFromConfig(mc *config.MethodCandidate, clan string) string {
	if clan != "" {
		if overrides, ok := mc.Config["pkg_overrides"].(map[string]any); ok {
			for _, name := range native.ManagerNamesForClan(clan) {
				if pkg, ok := overrides[name].(string); ok && pkg != "" {
					return pkg
				}
			}
		}
	}
	if pkg, ok := mc.Config["pkg"].(string); ok && pkg != "" {
		return pkg
	}
	return ""
}

// runNativeInstall runs the install command for a native package manager.
// Shared by NativeAdapter and NativeByManagerAdapter.
func runNativeInstall(ctx context.Context, rn run.Runner, prefix, clan string, mc *config.MethodCandidate) error {
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" {
		return fmt.Errorf("%s: no package name", prefix)
	}
	cmd := native.BuildInstallCmd(clan, pkg)
	if cmd == nil {
		return fmt.Errorf("%s: no install command for clan %q", prefix, clan)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, prefix+": install")
}

// RegisterNativeManagerAliases registers aliases for each known native
// manager binary name (apt, pacman, dnf, brew, …). This allows schema
// entries that use the manager name directly (e.g. `apt = "fd-find"`) to
// resolve to the native adapter.
func RegisterNativeManagerAliases() {
	seen := map[string]bool{}
	// Register from Manager.Name values (e.g. "apt", "portage", "dnf").
	for _, mgrName := range native.ManagerNames() {
		if mgrName == "" || mgrName == "native" || seen[mgrName] {
			continue
		}
		seen[mgrName] = true
		Register(&NativeByManagerAdapter{managerName: mgrName})
	}
	// Also register from managerNameToClan binary names that differ from
	// Manager.Name (e.g. "emerge" vs "portage").
	for _, binName := range native.ManagerBinaryNames() {
		if binName == "" || seen[binName] {
			continue
		}
		seen[binName] = true
		Register(&NativeByManagerAdapter{managerName: binName})
	}
}

// NativeByManagerAdapter handles a manager-specific method kind like
// "apt", "pacman", "dnf", etc. It finds the right clan at runtime by
// matching the manager binary name against known managers.
//
// This bridges the gap between schema method kinds (which use real
// manager names) and the canonical "native" adapter.
type NativeByManagerAdapter struct {
	managerName string // "apt", "pacman", "dnf", …
	rn          run.Runner
}

func (a *NativeByManagerAdapter) Kind() string { return a.managerName }

func (a *NativeByManagerAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, a.managerName)
}

func (a *NativeByManagerAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	clan := findClanByManager(a.managerName)
	if clan == "" {
		return false
	}
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" {
		return false
	}
	cmd := native.BuildCheckCmd(clan, pkg)
	if cmd == nil {
		return false
	}
	// Use the actual manager binary name (e.g. "dnf5" instead of "dnf").
	cmd = replaceManagerBinary(cmd, a.managerName, clan)
	if a.managerName == "winget" {
		if source, _ := mc.Config["source"].(string); source != "" {
			cmd = append(cmd, "--source", source)
		}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	if a.managerName != "winget" {
		return true
	}
	version, _, ok := wingetPackageFromOutput(res.Stdout, pkg)
	if !ok {
		return false
	}
	if want, _ := mc.Config["version"].(string); want != "" && version != want {
		return false
	}
	return true
}

// CheckAvailable mirrors NativeAdapter.CheckAvailable but resolves the
// clan from the manager binary name (findClanByManager) instead of
// probing, and swaps in the actual manager binary for aliases (dnf5, etc)
// the same way Check and Install do.
func (a *NativeByManagerAdapter) CheckAvailable(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	clan := findClanByManager(a.managerName)
	if clan == "" {
		return true
	}
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" {
		return true
	}
	cmd := native.BuildSearchCmd(clan, pkg)
	if cmd == nil {
		return true
	}
	cmd = replaceManagerBinary(cmd, a.managerName, clan)
	if a.managerName == "winget" {
		cmd = append(cmd, wingetSelectionArgs(mc, true)...)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return res.Err == nil && res.ExitCode == 0
}

func (a *NativeByManagerAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	clan := findClanByManager(a.managerName)
	if clan == "" {
		return fmt.Errorf("native(%s): no clan found for manager", a.managerName)
	}
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" {
		return fmt.Errorf("native(%s): no package name", a.managerName)
	}
	cmd := native.BuildInstallCmd(clan, pkg)
	if cmd == nil {
		return fmt.Errorf("native(%s): no install command for clan %q", a.managerName, clan)
	}
	// Use the actual manager binary name (e.g. "dnf5" instead of "dnf").
	cmd = replaceManagerBinary(cmd, a.managerName, clan)
	if a.managerName == "winget" {
		cmd = append(cmd, wingetSelectionArgs(mc, true)...)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "native("+a.managerName+"): install")
}

func (a *NativeByManagerAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	clan := findClanByManager(a.managerName)
	if clan == "" {
		return fmt.Errorf("native(%s): no clan found for manager", a.managerName)
	}
	if a.managerName != "winget" {
		nativeAdapter := NewNativeAdapter(clan)
		return nativeAdapter.Remove(ctx, rn, tool, mc)
	}
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" && tool != nil {
		pkg = tool.Name
	}
	cmd := native.BuildRemoveCmd(clan, pkg)
	if cmd == nil {
		return fmt.Errorf("native(winget): no remove command")
	}
	for _, item := range []struct{ field, flag string }{{"version", "--version"}, {"source", "--source"}, {"scope", "--scope"}} {
		if value, _ := mc.Config[item.field].(string); value != "" {
			cmd = append(cmd, item.flag, value)
		}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "native(winget): remove")
}

func (a *NativeByManagerAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	if a.managerName != "winget" {
		return "", nil
	}
	clan := findClanByManager(a.managerName)
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" && tool != nil {
		pkg = tool.Name
	}
	cmd := native.BuildCheckCmd(clan, pkg)
	if source, _ := mc.Config["source"].(string); source != "" {
		cmd = append(cmd, "--source", source)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if err := run.CheckResult(res, "winget: version check"); err != nil {
		return "", err
	}
	version, _, ok := wingetPackageFromOutput(res.Stdout, pkg)
	if !ok {
		return "", fmt.Errorf("winget: package %q not present in list output", pkg)
	}
	return version, nil
}

func wingetSelectionArgs(mc *config.MethodCandidate, includeInstaller bool) []string {
	if mc == nil {
		return nil
	}
	var args []string
	for _, item := range []struct{ field, flag string }{
		{"version", "--version"}, {"source", "--source"}, {"scope", "--scope"}, {"architecture", "--architecture"},
	} {
		if value, _ := mc.Config[item.field].(string); value != "" {
			args = append(args, item.flag, value)
		}
	}
	if includeInstaller {
		if value, _ := mc.Config["installer_type"].(string); value != "" {
			args = append(args, "--installer-type", value)
		}
	}
	return args
}

func wingetPackageFromOutput(stdout []byte, pkg string) (version, source string, ok bool) {
	for _, line := range strings.Split(string(stdout), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for i, field := range fields {
			if field != pkg || i+1 >= len(fields) {
				continue
			}
			version = fields[i+1]
			if len(fields) > i+3 {
				source = fields[len(fields)-1]
			}
			return version, source, version != ""
		}
	}
	return "", "", false
}

func (a *NativeByManagerAdapter) CanRemove() bool { return true }

// findClanByManager searches for the clan that manages packages via the
// given binary name (e.g. "apt" → "debian", "emerge" → "gentoo").
// It checks the explicit managerNameToClan reverse map first (handles
// binary names that differ from Manager.Name), then falls back to
// iterating known clans.
func findClanByManager(name string) string {
	// 1. Check explicit reverse map (e.g. "emerge" → "gentoo").
	if clan, ok := native.ManagerNameToClan(name); ok {
		if _, ok := native.Lookup(clan); ok {
			return clan
		}
	}
	// 2. Fallback: search known clans by Manager.Name.
	for _, clan := range native.KnownClans() {
		if mgr, ok := native.Lookup(clan); ok && mgr.Name == name {
			return clan
		}
	}
	return ""
}

// Compile-time interface checks.
var _ Remover = (*NativeAdapter)(nil)
var _ Remover = (*NativeByManagerAdapter)(nil)
var _ AvailabilityChecker = (*NativeAdapter)(nil)
var _ AvailabilityChecker = (*NativeByManagerAdapter)(nil)
var _ Versioner = (*NativeByManagerAdapter)(nil)

// replaceManagerBinary replaces the binary name in a native manager command
// with the actual binary name (e.g. "dnf5" instead of "dnf"). This handles
// the case where a manager kind (e.g. "dnf5") maps to a clan whose default
// binary is different. It skips any known elevation prefix (sudo, pkexec).
func replaceManagerBinary(cmd []string, managerName, clan string) []string {
	if len(cmd) == 0 {
		return cmd
	}
	nm, ok := native.Lookup(clan)
	if !ok {
		return cmd
	}
	// If the manager name matches the clan's default binary, no change needed.
	if managerName == nm.Name {
		return cmd
	}
	// Skip known elevation prefixes to reach the actual manager binary.
	start := 0
	if len(cmd) > 1 && run.IsElevationPrefix(cmd[0]) {
		start = 1
	}
	if start < len(cmd) {
		cmd[start] = managerName
	}
	return cmd
}
