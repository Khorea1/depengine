package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// CargoAdapter extends BaseAdapter with git-repo support: when
// mc.Config["git"] is set, it uses `cargo install --git {url}`
// instead of `cargo install {pkg}`.
type CargoAdapter struct {
	*BaseAdapter
}

// NewCargoAdapter creates a cargo adapter with git-repo support.
func NewCargoAdapter() *CargoAdapter {
	return &CargoAdapter{
		BaseAdapter: NewBaseAdapter(Configs["cargo"]),
	}
}

// Install resolves typed Cargo source/version/revision fields into structured
// argv. Git refs are only meaningful with git and are mutually exclusive.
func (a *CargoAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if !a.Available(ctx, rn) {
		return fmt.Errorf("cargo: binary %q not available on PATH", "cargo")
	}
	cmd, err := cargoInstallCommand(tool, mc)
	if err != nil {
		return err
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "cargo: install")
}

func cargoInstallCommand(tool *config.Tool, mc *config.MethodCandidate) ([]string, error) {
	gitURL, _ := mc.Config["git"].(string)
	version, _ := mc.Config["version"].(string)
	registry, _ := mc.Config["registry"].(string)
	branch, _ := mc.Config["branch"].(string)
	tag, _ := mc.Config["tag"].(string)
	rev, _ := mc.Config["rev"].(string)

	if gitURL != "" && version != "" {
		return nil, fmt.Errorf("cargo: git and version are mutually exclusive")
	}
	if gitURL != "" && registry != "" {
		return nil, fmt.Errorf("cargo: git and registry are mutually exclusive")
	}
	refs := 0
	for _, ref := range []string{branch, tag, rev} {
		if ref != "" {
			refs++
		}
	}
	if refs > 1 {
		return nil, fmt.Errorf("cargo: branch, tag, and rev are mutually exclusive")
	}
	if refs > 0 && gitURL == "" {
		return nil, fmt.Errorf("cargo: branch, tag, and rev require git")
	}

	cmd := []string{"cargo", "install"}
	if gitURL != "" {
		cmd = append(cmd, "--git", gitURL)
		switch {
		case branch != "":
			cmd = append(cmd, "--branch", branch)
		case tag != "":
			cmd = append(cmd, "--tag", tag)
		case rev != "":
			cmd = append(cmd, "--rev", rev)
		}
	} else {
		if registry != "" {
			cmd = append(cmd, "--registry", registry)
		}
		if version != "" {
			cmd = append(cmd, "--version", version)
		}
	}
	cmd = append(cmd, cargoInstallOptions(mc)...)
	if gitURL != "" {
		if pkg, ok := mc.Config["pkg"].(string); ok && pkg != "" {
			cmd = append(cmd, pkg)
		}
		return cmd, nil
	}
	cmd = append(cmd, resolvedPkg(tool, mc))
	return cmd, nil
}

func cargoStringList(mc *config.MethodCandidate, key string) []string {
	if mc == nil {
		return nil
	}
	var out []string
	switch values := mc.Config[key].(type) {
	case []string:
		for _, value := range values {
			if value != "" {
				out = append(out, value)
			}
		}
	case []any:
		for _, raw := range values {
			if value, ok := raw.(string); ok && value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}

func cargoInstallOptions(mc *config.MethodCandidate) []string {
	var args []string
	if features := cargoStringList(mc, "features"); len(features) > 0 {
		args = append(args, "--features", strings.Join(features, ","))
	}
	if noDefault, _ := mc.Config["no_default_features"].(bool); noDefault {
		args = append(args, "--no-default-features")
	}
	for _, bin := range cargoStringList(mc, "bins") {
		args = append(args, "--bin", bin)
	}
	if target, _ := mc.Config["target"].(string); target != "" {
		args = append(args, "--target", target)
	}
	if root, _ := mc.Config["root"].(string); root != "" {
		args = append(args, "--root", config.ExpandHomeDir(root))
	}
	return args
}

// Check reconciles the installed crate entry against exact version and selected
// binaries when those dimensions are declared. The configured install root is
// used for both query and removal so lifecycle operations target one profile.
func (a *CargoAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	version, bins, err := a.installedEntry(ctx, rn, tool, mc)
	if err != nil || version == "" {
		return false
	}
	if want, _ := mc.Config["version"].(string); want != "" && strings.TrimPrefix(version, "v") != strings.TrimPrefix(want, "v") {
		return false
	}
	if wantedBins := cargoStringList(mc, "bins"); len(wantedBins) > 0 {
		present := make(map[string]bool, len(bins))
		for _, bin := range bins {
			present[bin] = true
		}
		for _, bin := range wantedBins {
			if !present[bin] {
				return false
			}
		}
	}
	return true
}

func (a *CargoAdapter) installedEntry(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, []string, error) {
	pkg := tool.Name
	if p, ok := mc.Config["pkg"].(string); ok && p != "" {
		pkg = p
	}
	args := []string{"install", "--list"}
	if root, _ := mc.Config["root"].(string); root != "" {
		args = append(args, "--root", config.ExpandHomeDir(root))
	}
	res := rn.Run(ctx, "cargo", args...)
	if res.Err != nil || res.ExitCode != 0 {
		return "", nil, nil
	}
	lines := strings.Split(string(res.Stdout), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != pkg || !strings.HasSuffix(fields[1], ":") {
			continue
		}
		version := strings.TrimPrefix(strings.TrimSuffix(fields[1], ":"), "v")
		var bins []string
		for j := i + 1; j < len(lines); j++ {
			next := lines[j]
			if strings.TrimSpace(next) == "" {
				continue
			}
			if len(next) == len(strings.TrimLeft(next, " \t")) {
				break
			}
			bins = append(bins, strings.TrimSpace(next))
		}
		return version, bins, nil
	}
	return "", nil, nil
}

// InstalledVersion reports the installed crate version from the same cargo
// install root used by Check.
func (a *CargoAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	version, _, err := a.installedEntry(ctx, rn, tool, mc)
	return version, err
}

// Remove uninstalls from the same root used by Install and Check.
func (a *CargoAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	args := []string{"uninstall"}
	if root, _ := mc.Config["root"].(string); root != "" {
		args = append(args, "--root", config.ExpandHomeDir(root))
	}
	args = append(args, resolvedPkg(tool, mc))
	return run.CheckResult(rn.Run(ctx, "cargo", args...), "cargo: uninstall")
}

func (a *CargoAdapter) CanRemove() bool { return true }

// Ensure CargoAdapter implements execution capabilities.
var _ exec.Adapter = (*CargoAdapter)(nil)
var _ exec.Remover = (*CargoAdapter)(nil)
var _ exec.Versioner = (*CargoAdapter)(nil)
