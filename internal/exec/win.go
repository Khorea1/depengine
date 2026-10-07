package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/scoopruntime"
)

// WindowsAdapters returns the built-in Windows package-manager adapters.
// Callers explicitly add them to their registry at the composition root.
func WindowsAdapters() []AdapterV2 {
	return WindowsAdaptersWithScoop(scoopruntime.NewOfficial())
}

// WindowsAdaptersWithScoop composes Windows adapters with the caller-selected Scoop runtime.
func WindowsAdaptersWithScoop(runtime scoopruntime.Runtime) []AdapterV2 {
	return []AdapterV2{
		&winAdapter{kind: "scoop", runtime: runtime},
		&winAdapter{
			kind:       "choco",
			binary:     "choco",
			installCmd: []string{"choco", "install", "{pkg}", "-y"},
			checkCmd:   []string{"choco", "list", "--exact", "--limit-output", "{pkg}"},
			removeCmd:  []string{"choco", "uninstall", "{pkg}", "-y"},
		},
	}
}

// winAdapter implements AdapterV2 for a Windows package manager (scoop, choco).
// Commands use "{pkg}" as a placeholder for the package name from Tool or config.
type winAdapter struct {
	kind, binary                    string
	installCmd, checkCmd, removeCmd []string
	runtime                         scoopruntime.Runtime
}

func packageName(tool *config.Tool, mc *config.MethodCandidate) string {
	if mc != nil {
		if pkg, _ := mc.Config["pkg"].(string); pkg != "" {
			return pkg
		}
	}
	if tool != nil {
		return tool.Name
	}
	return ""
}

func (w *winAdapter) Kind() string { return w.kind }

func (w *winAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if w.kind == "scoop" {
		return w.runtime.Available(ctx, rn)
	}
	return run.LookPath(ctx, rn, w.binary)
}

func (w *winAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	observation := w.observeInstalled(ctx, rn, tool, mc)
	if observation.Presence != plan.PresencePresent {
		return false
	}
	if wantVersion, _ := mc.Config["version"].(string); wantVersion != "" && observation.Identity.Version != wantVersion {
		return false
	}
	if w.kind == "scoop" {
		if bucket, _ := mc.Config["bucket"].(string); bucket != "" && !strings.EqualFold(observation.Identity.Source, bucket) {
			return false
		}
	}
	return true
}

func (w *winAdapter) observeInstalled(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) plan.Observation {
	if rn == nil {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: "package manager probe is unavailable"}
	}
	pkg := packageName(tool, mc)
	if w.kind == "scoop" {
		scope, _ := mc.Config["scope"].(string)
		installed, err := w.runtime.ObserveInstalled(ctx, rn, pkg, scope)
		if err != nil {
			return plan.Observation{Presence: plan.PresenceUnknown, Detail: "package manager probe failed"}
		}
		if !installed.Present {
			return plan.Observation{Presence: plan.PresenceAbsent}
		}
		if installed.Malformed {
			return plan.Observation{Presence: plan.PresenceBroken, Detail: "package manager returned an invalid installed version"}
		}
		observation := plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: pkg, Version: installed.Version}, KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion}}
		if installed.Bucket != "" {
			observation.Identity.Source = installed.Bucket
			observation.KnownFields = append(observation.KnownFields, plan.FieldSource)
		}
		if scope != "" {
			if scope == "global" {
				observation.Identity.Scope = string(plan.ScopeSystem)
			} else {
				observation.Identity.Scope = string(plan.ScopeUser)
			}
			observation.KnownFields = append(observation.KnownFields, plan.FieldScope)
		}
		return observation
	}
	cmd := SubstitutePkg(w.checkCmd, tool, mc)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil || res.ExitCode != 0 || !hasChocoVersion(res.Stdout, pkg) {
		// Chocolatey 2.x removed --local-only. Prefer the current probe, then
		// fall back for older Chocolatey releases that still require the flag.
		res = rn.Run(ctx, "choco", "list", "--local-only", "--exact", "--limit-output", pkg)
	}
	if res.Err != nil || res.ExitCode != 0 {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: "package manager probe failed"}
	}
	version, ok := chocoVersionFromOutput(res.Stdout, pkg)
	if !ok {
		if outputMentionsPackage(res.Stdout, pkg) {
			return plan.Observation{Presence: plan.PresenceBroken, Detail: "package manager returned an invalid installed version"}
		}
		return plan.Observation{Presence: plan.PresenceAbsent}
	}
	return plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: pkg, Version: version}, KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion}}
}

func outputMentionsPackage(stdout []byte, pkg string) bool {
	for _, line := range strings.Split(string(stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id, _, _ := strings.Cut(fields[0], "|")
		if strings.EqualFold(id, pkg) {
			return true
		}
	}
	return false
}

func chocoVersionFromOutput(stdout []byte, pkg string) (string, bool) {
	for _, line := range strings.Split(string(stdout), "\n") {
		id, version, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || !strings.EqualFold(id, pkg) {
			continue
		}
		return version, version != ""
	}
	return "", false
}

func hasChocoVersion(stdout []byte, pkg string) bool {
	_, ok := chocoVersionFromOutput(stdout, pkg)
	return ok
}

func (w *winAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	if rn == nil {
		return "", fmt.Errorf("%s: no runner", w.kind)
	}
	pkg := packageName(tool, mc)
	switch w.kind {
	case "choco":
		res := rn.Run(ctx, "choco", "list", "--exact", "--limit-output", pkg)
		if err := run.CheckResultCompleteOutput(res, "choco: version check"); err != nil {
			return "", err
		}
		version, ok := chocoVersionFromOutput(res.Stdout, pkg)
		if !ok {
			return "", fmt.Errorf("choco: package %q not present in version output", pkg)
		}
		return version, nil
	case "scoop":
		scope, _ := mc.Config["scope"].(string)
		installed, err := w.runtime.ObserveInstalled(ctx, rn, pkg, scope)
		if err != nil {
			return "", err
		}
		if !installed.Present || installed.Malformed {
			return "", fmt.Errorf("scoop: package %q not present in valid version output", pkg)
		}
		return installed.Version, nil
	default:
		return "", nil
	}
}

func (w *winAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if rn == nil {
		return fmt.Errorf("%s: no runner", w.kind)
	}
	if w.kind == "scoop" {
		version, _ := mc.Config["version"].(string)
		bucket, _ := mc.Config["bucket"].(string)
		scope, _ := mc.Config["scope"].(string)
		architecture, _ := mc.Config["architecture"].(string)
		return w.runtime.InstallResolved(ctx, rn, scoopruntime.InstallTarget{Package: packageName(tool, mc), Version: version, Bucket: bucket, Scope: scope, Architecture: architecture})
	}
	cmd := SubstitutePkg(w.installCmd, tool, mc)
	if w.kind == "choco" {
		extra := make([]string, 0, 8)
		if version, _ := mc.Config["version"].(string); version != "" {
			extra = append(extra, "--version", version)
		}
		if source, _ := mc.Config["source"].(string); source != "" {
			extra = append(extra, "--source", source)
		}
		if architecture, _ := mc.Config["architecture"].(string); architecture == "x86" {
			extra = append(extra, "--forcex86")
		}
		if prerelease, _ := mc.Config["prerelease"].(bool); prerelease {
			extra = append(extra, "--pre")
		}
		if len(extra) > 0 {
			cmd = append(cmd[:len(cmd)-1], append(extra, cmd[len(cmd)-1:]...)...)
		}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, w.kind+": install")
}

func (w *winAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if rn == nil {
		return fmt.Errorf("%s: no runner", w.kind)
	}
	if w.kind == "scoop" {
		bucket, _ := mc.Config["bucket"].(string)
		scope, _ := mc.Config["scope"].(string)
		target := scoopruntime.InstallTarget{Package: packageName(tool, mc), Bucket: bucket, Scope: scope}
		if err := scoopruntime.CheckRemoveCapabilities(w.runtime.Capabilities(), target); err != nil {
			return err
		}
		return w.runtime.RemoveResolved(ctx, rn, target)
	}
	if len(w.removeCmd) == 0 {
		return fmt.Errorf("%s: no remove command configured", w.kind)
	}
	cmd := SubstitutePkg(w.removeCmd, tool, mc)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, w.kind+": remove")
}
func (w *winAdapter) CanRemove() bool {
	if w.kind == "scoop" {
		return w.runtime.Capabilities().Removal
	}
	return len(w.removeCmd) > 0
}

// ResolvePlan returns the static intent unchanged: scoop/choco installs
// perform no dynamic resolution ({latest}, tags, assets), so the intent is
// already the concrete plan.
func (w *winAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, fmt.Errorf("%s: nil plan intent", w.kind)
	}
	resolved := intent.Clone()
	return &resolved, nil
}

// Observe preserves the concrete identity reported by the manager. In
// particular, an installed but different exact version remains present so the
// shared reconciler can classify it as drift instead of unknown/absent.
func (w *winAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	return w.observeInstalled(ctx, rn, tool, mc), nil
}

// InstallResolved executes identity from the resolved plan. Method config is
// consulted only for execution-only switches that are not part of resolved
// identity (currently Chocolatey's prerelease flag).
func (w *winAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil {
		return fmt.Errorf("%s: nil resolved plan", w.kind)
	}
	if rn == nil {
		return fmt.Errorf("%s: no runner", w.kind)
	}
	if err := validateNativeResolvedInstallOperation(w.kind, resolved); err != nil {
		return err
	}
	pkg := resolved.Identity.Package
	if pkg == "" {
		return fmt.Errorf("%s: resolved plan has no package identity", w.kind)
	}

	var cmd []string
	switch w.kind {
	case "scoop":
		scope := resolved.Identity.Scope
		if scope == string(plan.ScopeSystem) {
			scope = "global"
		} else if scope == string(plan.ScopeUser) {
			scope = "user"
		}
		target := scoopruntime.InstallTarget{Package: pkg, Version: resolved.Identity.Version, Bucket: resolvedSelectionSource(resolved), Scope: scope, Architecture: resolved.Identity.Architecture}
		if err := scoopruntime.CheckInstallCapabilities(w.runtime.Capabilities(), target); err != nil {
			return err
		}
		return w.runtime.InstallResolved(ctx, rn, target)
	case "choco":
		cmd = []string{"choco", "install", pkg}
		if resolved.Identity.Version != "" {
			cmd = append(cmd, "--version", resolved.Identity.Version)
		}
		if resolved.Identity.Source != "" {
			cmd = append(cmd, "--source", resolved.Identity.Source)
		}
		if resolved.Identity.Architecture == "x86" {
			cmd = append(cmd, "--forcex86")
		}
		if mc != nil {
			if prerelease, _ := mc.Config["prerelease"].(bool); prerelease {
				cmd = append(cmd, "--pre")
			}
		}
		cmd = append(cmd, "-y")
	default:
		return fmt.Errorf("%s: unsupported Windows package manager", w.kind)
	}

	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, w.kind+": install")
}

func resolvedSelectionSource(resolved *plan.ResolvedInstallPlan) string {
	for _, source := range resolved.Sources {
		if source.Role != plan.SourceSelection {
			continue
		}
		if source.Name != "" {
			return source.Name
		}
		return source.URL
	}
	return ""
}

// CheckAvailable assumes availability: these managers resolve names at
// install time, so an unknown package surfaces there.
func (w *winAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints: these adapters only
// run on Windows hosts by construction.
func (w *winAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
	return nil
}

// Compile-time interface checks.
var _ AdapterV2 = (*winAdapter)(nil)
var _ Versioner = (*winAdapter)(nil)
