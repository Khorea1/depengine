// Package lang provides language-ecosystem adapters (cargo, go, pip, pipx,
// uv, aur). Each adapter follows the same pattern — Available via `which`,
// Check via a package query command, Install via a package install command.
//
// The BaseAdapter struct implements exec.Adapter generically; each concrete
// adapter is just a BaseConfig + registration. See the registry.go file
// for the mapping of method names to adapters.
package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// BaseConfig describes one language adapter. Most adapters share this
// shape; only AUR needs special handling (configurable helper binary).
type BaseConfig struct {
	// KindName matches the method_order entry in schema.toml.
	KindName string

	// Binary is the name of the tool that must exist on PATH
	// (e.g. "cargo", "go", "pip").
	Binary string

	// CheckTmpl is the command template for checking installation.
	// "{pkg}" is replaced with the package name; "{bin}" with the binary
	// name derived from the package/import path (last path element, or the
	// element after /cmd/).
	CheckTmpl []string

	// InstallTmpl is the command template for installing.
	// "{pkg}" is replaced with the package name.
	InstallTmpl []string

	// RemoveTmpl is the command template for uninstalling.
	// "{pkg}" is replaced with the package name. When empty, the
	// adapter does not support automated removal.
	RemoveTmpl []string

	// AvailableExtra, if set, is tried as the Available binary when
	// Binary is not found (e.g. "pip3" when "pip" is missing).
	AvailableExtra string
}

// BaseAdapter implements exec.Adapter for a BaseConfig.
type BaseAdapter struct {
	config BaseConfig
}

// NewBaseAdapter creates an adapter from a static config.
func NewBaseAdapter(config BaseConfig) *BaseAdapter {
	return &BaseAdapter{config: config}
}

func (a *BaseAdapter) Kind() string { return a.config.KindName }

// Available checks whether the required binary exists in PATH.
func (a *BaseAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if run.LookPath(ctx, rn, a.config.Binary) {
		return true
	}
	if a.config.AvailableExtra != "" {
		return run.LookPath(ctx, rn, a.config.AvailableExtra)
	}
	return false
}

// Check runs the check command template. Exit 0 means installed.
// Returns false immediately if the adapter's binary is not available.
func (a *BaseAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	if !a.Available(ctx, rn) {
		return false
	}
	cmd := a.buildCmd(a.config.CheckTmpl, tool, mc)
	if cmd == nil {
		return false
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return res.Err == nil && res.ExitCode == 0
}

// Install runs the install command template.
// Returns an error immediately if the adapter's binary is not available.
func (a *BaseAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if !a.Available(ctx, rn) {
		return fmt.Errorf("%s: binary %q not available on PATH", a.config.KindName, a.config.Binary)
	}
	cmd := a.buildCmd(a.config.InstallTmpl, tool, mc)
	if cmd == nil {
		return fmt.Errorf("%s: no install command", a.config.KindName)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("%s: install failed: %w", a.config.KindName, res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("%s: install exited %d: %s", a.config.KindName, res.ExitCode, stderr)
	}
	return nil
}

func (a *BaseAdapter) CanRemove() bool {
	return len(a.config.RemoveTmpl) > 0
}

// Remove runs the remove command template. Returns nil on success.
func (a *BaseAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	cmd := a.buildCmd(a.config.RemoveTmpl, tool, mc)
	if cmd == nil {
		return fmt.Errorf("%s: no remove command", a.config.KindName)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("%s: remove failed: %w", a.config.KindName, res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("%s: remove exited %d: %s", a.config.KindName, res.ExitCode, stderr)
	}
	return nil
}

// buildCmd substitutes {pkg} and {bin} in the template and returns the command.
// {bin} is derived from the resolved package name via goBinaryName — the
// binary name that a Go `go install <import path>` produces (last path
// element, or the element after a "/cmd/" segment when present).
func (a *BaseAdapter) buildCmd(tmpl []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(tmpl) == 0 {
		return nil
	}
	cmd := exec.SubstitutePkg(tmpl, tool, mc)
	pkg := tool.Name
	if p, ok := mc.Config["pkg"].(string); ok && p != "" {
		pkg = p
	}
	bin := goBinaryName(pkg)
	for i, arg := range cmd {
		cmd[i] = strings.ReplaceAll(arg, "{bin}", bin)
	}
	return cmd
}

// Compile-time interface checks.
var _ exec.Remover = (*BaseAdapter)(nil)

// hasWord reports whether s contains word as a standalone word, using
// simple boundary matching (space, tab, newline, or start/end of string).
// This avoids false positives from substring matches (e.g. "python" matching
// "ms-python.python" or "ipython").
func hasWord(s, word string) bool {
	if word == "" {
		return false
	}
	start := 0
	for start <= len(s) {
		idx := strings.Index(s[start:], word)
		if idx < 0 {
			return false
		}
		abs := start + idx
		before := abs == 0 || s[abs-1] == ' ' || s[abs-1] == '\t' || s[abs-1] == '\n' || s[abs-1] == '-'
		after := abs+len(word) >= len(s) || s[abs+len(word)] == ' ' || s[abs+len(word)] == '\t' || s[abs+len(word)] == '\n' || s[abs+len(word)] == '-'
		if before && after {
			return true
		}
		start = abs + 1
	}
	return false
}
