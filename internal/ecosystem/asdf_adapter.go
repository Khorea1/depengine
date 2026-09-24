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

// AsdfAdapter implements exec.AdapterV2 for asdf/mise packages.
type AsdfAdapter struct{}

// NewAsdfAdapter creates a new AsdfAdapter.
func NewAsdfAdapter() *AsdfAdapter {
	return &AsdfAdapter{}
}

func (a *AsdfAdapter) Kind() string { return "asdf" }

func (a *AsdfAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "asdf") || run.LookPath(ctx, rn, "mise")
}

func (a *AsdfAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return false
	}
	desired := asdfVersion(mc)
	for _, cmd := range []string{"asdf", "mise"} {
		if run.LookPath(ctx, rn, cmd) {
			res := rn.Run(ctx, cmd, "list", pkg[0])
			if res.Err != nil || res.ExitCode != 0 {
				continue
			}
			out := strings.TrimSpace(string(res.Stdout))
			if desired == "latest" {
				return out != ""
			}
			if hasWord(out, desired) {
				return true
			}
		}
	}
	return false
}

func (a *AsdfAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return fmt.Errorf("asdf: no package name")
	}
	return installAsdfOrMise(ctx, rn, pkg[0], asdfVersion(mc))
}

func installAsdfOrMise(ctx context.Context, rn run.Runner, pkg, version string) error {
	for _, backend := range []string{"asdf", "mise"} {
		if !run.LookPath(ctx, rn, backend) {
			continue
		}
		if backend == "asdf" {
			return installAsdf(ctx, rn, pkg, version)
		}
		return installMise(ctx, rn, pkg, version)
	}
	return errors.New("asdf: neither asdf nor mise found")
}

func installAsdf(ctx context.Context, rn run.Runner, pkg, version string) error {
	plugins := rn.Run(ctx, "asdf", "plugin", "list")
	if plugins.Err != nil || plugins.ExitCode != 0 || !asdfPluginListed(plugins.Stdout, pkg) {
		res := rn.Run(ctx, "asdf", "plugin", "add", pkg)
		// Older asdf releases used exit code 2 for "already exists". Keep
		// accepting that result while using the non-hyphenated command form
		// required by asdf 0.16+.
		if res.ExitCode != 2 {
			if err := run.CheckResult(res, "asdf: plugin add"); err != nil {
				return fmt.Errorf("asdf: plugin add %s: %w", pkg, err)
			}
		}
	}
	if err := run.CheckResult(rn.Run(ctx, "asdf", "install", pkg, version), "asdf: install"); err != nil {
		return err
	}
	// asdf 0.16 removed `global`; `set --home` is its supported
	// replacement and writes the same user-level .tool-versions intent.
	return run.CheckResult(rn.Run(ctx, "asdf", "set", "--home", pkg, version), "asdf: set --home")
}

func installMise(ctx context.Context, rn run.Runner, pkg, version string) error {
	spec := pkg + "@" + version
	if err := run.CheckResult(rn.Run(ctx, "mise", "install", spec), "mise: install"); err != nil {
		return err
	}
	return run.CheckResult(rn.Run(ctx, "mise", "use", "-g", spec), "mise: global set")
}

func asdfPluginListed(stdout []byte, pkg string) bool {
	for _, plugin := range strings.Split(strings.TrimSpace(string(stdout)), "\n") {
		if strings.TrimSpace(plugin) == pkg {
			return true
		}
	}
	return false
}

func asdfVersion(mc *config.MethodCandidate) string {
	if mc != nil {
		if version, ok := mc.Config["version"].(string); ok && strings.TrimSpace(version) != "" {
			return strings.TrimSpace(version)
		}
	}
	return "latest"
}

func (a *AsdfAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("asdf: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("asdf: tool and method are required")
	}
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return nil, errors.New("asdf: no package name")
	}
	resolved := intent.Clone()
	if resolved.Identity.Package == "" {
		return nil, errors.New("asdf: no package name in plan intent")
	}
	return &resolved, nil
}

func (a *AsdfAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: "asdf: no package name"}, nil
	}
	desired := asdfVersion(mc)
	foundBackend := false
	var lastErr error
	for _, cmd := range []string{"asdf", "mise"} {
		if !run.LookPath(ctx, rn, cmd) {
			continue
		}
		foundBackend = true
		res := rn.Run(ctx, cmd, "list", pkg[0])
		if res.Err != nil || res.ExitCode != 0 {
			lastErr = fmt.Errorf("%s list %s failed: %w", cmd, pkg[0], run.CheckResult(res, "asdf: observe"))
			continue
		}
		out := strings.TrimSpace(string(res.Stdout))
		if out == "" || (desired != "latest" && !hasWord(out, desired)) {
			continue
		}
		identity := plan.ObservedIdentity{Package: pkg[0]}
		fields := []plan.IdentityField{plan.FieldPackage}
		if desired != "latest" {
			identity.Version, fields = desired, append(fields, plan.FieldVersion)
		}
		return plan.Observation{Presence: plan.PresencePresent, Identity: identity, KnownFields: fields}, nil
	}
	if lastErr != nil {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: lastErr.Error()}, lastErr
	}
	if !foundBackend {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	return plan.Observation{Presence: plan.PresenceAbsent}, nil
}

func (a *AsdfAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return errors.New("asdf: runner is required")
	}
	if resolved == nil {
		return errors.New("asdf: nil resolved plan")
	}
	if err := validateResolvedInstallOperation("asdf", resolved); err != nil {
		return err
	}
	pkg, version := resolved.Identity.Package, resolved.Identity.Version
	if pkg == "" {
		return errors.New("asdf: no package name in resolved plan")
	}
	if version == "" {
		version = "latest"
	}
	return installAsdfOrMise(ctx, rn, pkg, version)
}

// CanRemove reports whether this adapter supports removal. Removal is
// possible when an explicit version is known (see Remove).
func (a *AsdfAdapter) CanRemove() bool { return true }

// Remove uninstalls a specific version of a tool via asdf or mise.
//
// asdf/mise uninstall are version-dependent: `asdf uninstall <plugin> <version>`
// and `mise uninstall
// <plugin>@<version>` require an exact installed version. The version is read
// from mc.Config["version"]; when absent, removal cannot proceed and a manual
// command is suggested.
func (a *AsdfAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return fmt.Errorf("asdf: no package name")
	}
	version, ok := mc.Config["version"].(string)
	if !ok || version == "" {
		return fmt.Errorf("asdf: removal is version-dependent; set config version = \"<exact installed version>\" or run `asdf uninstall %s <version>` manually", pkg[0])
	}
	for _, cmd := range []string{"asdf", "mise"} {
		if !run.LookPath(ctx, rn, cmd) {
			continue
		}
		var res run.Result
		if cmd == "mise" {
			res = rn.Run(ctx, cmd, "uninstall", pkg[0]+"@"+version)
		} else {
			res = rn.Run(ctx, cmd, "uninstall", pkg[0], version)
		}
		if res.Err != nil {
			return fmt.Errorf("%s: remove failed: %w", cmd, res.Err)
		}
		if res.ExitCode != 0 {
			stderr := strings.TrimSpace(string(res.Stderr))
			return fmt.Errorf("%s: remove exited %d: %s", cmd, res.ExitCode, stderr)
		}
		return nil
	}
	return fmt.Errorf("asdf: neither asdf nor mise found")
}

// CheckAvailable assumes availability: asdf has no cheap local index to
// probe, so an unknown plugin surfaces at install time.
func (a *AsdfAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability.
func (a *AsdfAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*AsdfAdapter)(nil)
