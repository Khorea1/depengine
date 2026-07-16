package exec

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"depengine/pkg/native"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// NativeAdapter wraps pkg/native to implement the Adapter interface.
// It auto-detects the distro clan on first use by probing each known
// native manager binary. This avoids needing the clan at construction.
//
// Sync is NOT handled here — the executor's SyncManager runs index
// synchronization once per session before any tool is installed.
type NativeAdapter struct {
	clan   string     // detected on first Available() call
	once   sync.Once  // ensures detectClan runs exactly once
}
// NewNativeAdapter creates an adapter. Clan is detected automatically.
func NewNativeAdapter(clan string) *NativeAdapter {
	return &NativeAdapter{clan: clan}
}

func (a *NativeAdapter) Kind() string { return "native" }

// detectClan probes known native managers to find one that exists in PATH.
func (a *NativeAdapter) detectClan(ctx context.Context, rn run.Runner) string {
	a.once.Do(func() {
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
func (a *NativeAdapter) Check(ctx context.Context, rn run.Runner, _ *schema.Tool, mc *schema.MethodCandidate) bool {
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

// Install runs the install command. Sync is handled by the executor's
// SyncManager, not here.
func (a *NativeAdapter) Install(ctx context.Context, rn run.Runner, _ *schema.Tool, mc *schema.MethodCandidate) error {
	clan := a.detectClan(ctx, rn)
	if clan == "" {
		return fmt.Errorf("native: no native manager found")
	}
	return runNativeInstall(ctx, rn, "native", clan, mc)
}

// Remove uninstalls a package via the native package manager.
func (a *NativeAdapter) Remove(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
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
	if res.Err != nil {
		return fmt.Errorf("remove command failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("remove exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func (a *NativeAdapter) CanRemove() bool { return true }


// pkgFromConfig extracts the package name from a MethodCandidate's Config.
// When clan is non-empty and the MC has pkg_overrides, it checks for a
// clan-specific override first (e.g. apt→"fd-find" on debian). Falls back
// to the generic "pkg" key, then to empty string.
func pkgFromConfig(mc *schema.MethodCandidate, clan string) string {
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
func runNativeInstall(ctx context.Context, rn run.Runner, prefix, clan string, mc *schema.MethodCandidate) error {
	pkg := pkgFromConfig(mc, clan)
	if pkg == "" {
		return fmt.Errorf("%s: no package name", prefix)
	}
	cmd := native.BuildInstallCmd(clan, pkg)
	if cmd == nil {
		return fmt.Errorf("%s: no install command for clan %q", prefix, clan)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("%s: install failed: %w", prefix, res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("%s: install exited %d: %s", prefix, res.ExitCode, stderr)
	}
	return nil
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

func (a *NativeByManagerAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
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
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return res.Err == nil && res.ExitCode == 0
}

func (a *NativeByManagerAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
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
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("native(%s): install failed: %w", a.managerName, res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("native(%s): install exited %d: %s", a.managerName, res.ExitCode, stderr)
	}
	return nil
}

func (a *NativeByManagerAdapter) Remove(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	clan := findClanByManager(a.managerName)
	if clan == "" {
		return fmt.Errorf("native(%s): no clan found for manager", a.managerName)
	}
	nativeAdapter := NewNativeAdapter(clan)
	return nativeAdapter.Remove(ctx, rn, tool, mc)
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

// replaceManagerBinary replaces the binary name in a native manager command
// with the actual binary name (e.g. "dnf5" instead of "dnf"). This handles
// the case where a manager kind (e.g. "dnf5") maps to a clan whose default
// binary is different. It skips the "sudo" prefix if present.
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
	// Replace the binary (first arg after "sudo" if present, otherwise first arg).
	start := 0
	if cmd[0] == "sudo" {
		start = 1
	}
	if start < len(cmd) {
		cmd[start] = managerName
	}
	return cmd
}
