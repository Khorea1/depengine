package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// AsdfAdapter implements the Adapter interface for asdf/mise packages.
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
	desired := asdfVersion(mc)
	for _, cmd := range []string{"asdf", "mise"} {
		if !run.LookPath(ctx, rn, cmd) {
			continue
		}

		if cmd == "asdf" {
			// Check if plugin already exists — `asdf plugin list` lists all plugins.
			plugRes := rn.Run(ctx, cmd, "plugin", "list")
			if plugRes.Err == nil && plugRes.ExitCode == 0 {
				plugins := strings.Split(strings.TrimSpace(string(plugRes.Stdout)), "\n")
				found := false
				for _, p := range plugins {
					if strings.TrimSpace(p) == pkg[0] {
						found = true
						break
					}
				}
				if !found {
					// plugin-add exit code 2 means "plugin already exists" — continue.
					res := rn.Run(ctx, cmd, "plugin-add", pkg[0])
					if res.Err != nil && res.ExitCode != 2 {
						return fmt.Errorf("asdf: plugin-add failed for %s: %w", pkg[0], res.Err)
					}
				}
			} else {
				// `asdf plugin list` failed — try plugin-add directly.
				res := rn.Run(ctx, cmd, "plugin-add", pkg[0])
				if res.Err != nil && res.ExitCode != 2 {
					return fmt.Errorf("asdf: plugin-add failed for %s: %w", pkg[0], res.Err)
				}
			}
		}

		if cmd == "mise" {
			if res := rn.Run(ctx, cmd, "install", pkg[0]+"@"+desired); res.Err != nil {
				return fmt.Errorf("mise: install failed: %w", res.Err)
			}
			if res := rn.Run(ctx, cmd, "use", "-g", pkg[0]+"@"+desired); res.Err != nil {
				return fmt.Errorf("mise: global set failed: %w", res.Err)
			}
		} else {
			if res := rn.Run(ctx, cmd, "install", pkg[0], desired); res.Err != nil {
				return fmt.Errorf("asdf: install failed: %w", res.Err)
			}
			if res := rn.Run(ctx, cmd, "global", pkg[0], desired); res.Err != nil {
				return fmt.Errorf("asdf: global set failed: %w", res.Err)
			}
		}

		return nil
	}
	return fmt.Errorf("asdf: neither asdf nor mise found")
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
	version := asdfVersion(mc)
	resolved := intent.Clone()
	resolved.Identity.Package = pkg[0]
	resolved.Identity.Version = ""
	if version == "latest" {
		resolved.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionLatest}
	} else {
		resolved.Identity.Version = version
		resolved.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: version}
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
	if len(resolved.Operations) > 0 {
		return errors.New("asdf: resolved operations are unsupported")
	}
	pkg, version := resolved.Identity.Package, resolved.Identity.Version
	if pkg == "" {
		return errors.New("asdf: no package name in resolved plan")
	}
	if version == "" {
		version = "latest"
	}
	for _, cmd := range []string{"asdf", "mise"} {
		if !run.LookPath(ctx, rn, cmd) {
			continue
		}
		if cmd == "asdf" {
			plugins := rn.Run(ctx, cmd, "plugin", "list")
			if plugins.Err != nil || plugins.ExitCode != 0 || !hasWord(string(plugins.Stdout), pkg) {
				res := rn.Run(ctx, cmd, "plugin-add", pkg)
				if res.Err != nil && res.ExitCode != 2 {
					return fmt.Errorf("asdf: plugin-add failed for %s: %w", pkg, run.CheckResult(res, "asdf: plugin-add"))
				}
			}
			if err := run.CheckResult(rn.Run(ctx, cmd, "install", pkg, version), "asdf: install"); err != nil {
				return err
			}
			return run.CheckResult(rn.Run(ctx, cmd, "global", pkg, version), "asdf: global")
		}
		if err := run.CheckResult(rn.Run(ctx, cmd, "install", pkg+"@"+version), "mise: install"); err != nil {
			return err
		}
		return run.CheckResult(rn.Run(ctx, cmd, "use", "-g", pkg+"@"+version), "mise: global set")
	}
	return errors.New("asdf: neither asdf nor mise found")
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

var _ exec.Adapter = (*AsdfAdapter)(nil)
var _ exec.AdapterV2 = (*AsdfAdapter)(nil)
var _ exec.Remover = (*AsdfAdapter)(nil)
