package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
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
	return cargoEntrySatisfies(version, bins, mc)
}

// cargoEntrySatisfies reports whether an installed crate entry satisfies the
// declared version and bins intent. Shared by Check and Observe so both paths
// agree on what "installed" means.
func cargoEntrySatisfies(version string, bins []string, mc *config.MethodCandidate) bool {
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

// ResolvePlan records the crate selected by the candidate without rewriting
// planner intent. Source, version, registry, revision, and environment stay
// exactly as projected; InstallResolved consumes them as authoritative.
func (a *CargoAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("cargo: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("cargo: tool and method are required")
	}
	if resolvedPkg(tool, mc) == "" {
		return nil, errors.New("cargo: no package name")
	}
	resolved := intent.Clone()
	if err := validateEnvironmentTarget("cargo", resolved.Identity.Environment, plan.EnvironmentPrefix); err != nil {
		return nil, err
	}
	if resolved.Identity.Package == "" {
		return nil, errors.New("cargo: no package name in plan intent")
	}
	return &resolved, nil
}

// Observe reports the installed crate entry using VerificationResult-compatible
// semantics. The installed version comes from the host query (not the
// request), so version or bins drift surfaces as absence — the same verdict
// Check would give — rather than as a confirmed present.
func (a *CargoAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New("cargo: tool and method are required")
	}
	pkg := resolvedPkg(tool, mc)
	if pkg == "" {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	absent := plan.Observation{
		Presence:    plan.PresenceAbsent,
		Identity:    plan.ObservedIdentity{Package: pkg},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}
	version, bins, err := a.installedEntry(ctx, rn, tool, mc)
	if err != nil || version == "" || !cargoEntrySatisfies(version, bins, mc) {
		return absent, nil
	}
	return plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: pkg, Version: version},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}, nil
}

// InstallResolved executes only the identity resolved into the plan. Source,
// version, registry, git refs, target architecture, and install root come from
// the resolved plan (overriding the candidate config); execution-only build
// options (features, no_default_features, bins) still come from the candidate.
// Explicit operations have no cargo-specific interpretation and are rejected
// rather than treated as arbitrary commands.
func (a *CargoAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return errors.New("cargo: runner is required")
	}
	if resolved == nil {
		return errors.New("cargo: nil resolved plan")
	}
	if err := validateResolvedInstallOperation("cargo", resolved); err != nil {
		return err
	}
	if err := validateEnvironmentTarget("cargo", resolved.Identity.Environment, plan.EnvironmentPrefix); err != nil {
		return err
	}
	if resolved.Identity.Package == "" {
		return errors.New("cargo: no package name in resolved plan")
	}
	if !a.Available(ctx, rn) {
		return fmt.Errorf("cargo: binary %q not available on PATH", "cargo")
	}
	effectiveTool := tool
	if effectiveTool == nil {
		effectiveTool = &config.Tool{Name: resolved.Tool.Name}
	}
	cmd, err := cargoInstallCommand(effectiveTool, cargoResolvedConfig(mc, resolved))
	if err != nil {
		return err
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "cargo: install")
}

// cargoResolvedConfig overlays the resolved identity onto a copy of the
// candidate config so cargoInstallCommand renders the authoritative source,
// version, registry, git ref, and root. The input mc is never mutated; a nil
// mc yields a config derived purely from the plan. In git mode the positional
// package is kept only when it was explicitly configured, preserving the
// legacy fallback where cargo selects the repository package itself.
func cargoResolvedConfig(mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) *config.MethodCandidate {
	cfg := make(map[string]any)
	kind := "cargo"
	if mc != nil {
		for key, value := range mc.Config {
			cfg[key] = value
		}
		if mc.Kind != "" {
			kind = mc.Kind
		}
	}
	if resolved.Identity.Source != "" {
		cfg["git"] = resolved.Identity.Source
	} else {
		delete(cfg, "git")
	}
	if version := resolvedIdentityVersion(resolved.Identity); version != "" {
		cfg["version"] = version
	} else {
		delete(cfg, "version")
	}
	if resolved.Identity.Registry != "" {
		cfg["registry"] = resolved.Identity.Registry
	} else {
		delete(cfg, "registry")
	}
	delete(cfg, "branch")
	delete(cfg, "tag")
	delete(cfg, "rev")
	if requested := resolved.Identity.RequestedVersion; requested != nil {
		switch requested.Mode {
		case plan.VersionGitBranch:
			cfg["branch"] = requested.Value
		case plan.VersionGitTag:
			cfg["tag"] = requested.Value
		case plan.VersionGitRevision:
			cfg["rev"] = requested.Value
		}
	}
	if environment := resolved.Identity.Environment; environment != nil && environment.Kind == plan.EnvironmentPrefix {
		cfg["root"] = environment.Value
	} else {
		delete(cfg, "root")
	}
	if resolved.Identity.Architecture != "" {
		cfg["target"] = resolved.Identity.Architecture
	} else {
		delete(cfg, "target")
	}
	if gitURL, _ := cfg["git"].(string); gitURL == "" {
		cfg["pkg"] = resolved.Identity.Package
	} else if mc != nil {
		if pkg, _ := mc.Config["pkg"].(string); pkg != "" {
			cfg["pkg"] = resolved.Identity.Package
		} else {
			delete(cfg, "pkg")
		}
	} else {
		delete(cfg, "pkg")
	}
	return &config.MethodCandidate{Kind: kind, Config: cfg}
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
// CheckAvailable assumes availability: crates.io has no cheap local index
// to probe, so an unknown crate surfaces at install time.
func (a *CargoAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability.
func (a *CargoAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.Adapter = (*CargoAdapter)(nil)
var _ exec.AdapterV2 = (*CargoAdapter)(nil)
var _ exec.Versioner = (*CargoAdapter)(nil)
