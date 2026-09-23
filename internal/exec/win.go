package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// WindowsAdapters returns the built-in Windows package-manager adapters.
// Callers explicitly add them to their registry at the composition root.
func WindowsAdapters() []AdapterV2 {
	return []AdapterV2{
		&winAdapter{
			kind:       "scoop",
			binary:     "scoop",
			installCmd: []string{"scoop", "install", "{pkg}"},
			checkCmd:   []string{"scoop", "list", "{pkg}"},
			removeCmd:  []string{"scoop", "uninstall", "{pkg}"},
		},
		&winAdapter{
			kind:       "choco",
			binary:     "choco",
			installCmd: []string{"choco", "install", "{pkg}", "-y"},
			checkCmd:   []string{"choco", "list", "--local-only", "--exact", "--limit-output", "{pkg}"},
			removeCmd:  []string{"choco", "uninstall", "{pkg}", "-y"},
		},
	}
}

// winAdapter implements Adapter for a Windows package manager (scoop, choco).
// Commands use "{pkg}" as a placeholder for the package name from Tool or config.
type winAdapter struct {
	kind, binary                    string
	installCmd, checkCmd, removeCmd []string
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
	return run.LookPath(ctx, rn, w.binary)
}

func (w *winAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	if rn == nil {
		return false
	}
	cmd := SubstitutePkg(w.checkCmd, tool, mc)
	if w.kind == "scoop" {
		if scope, _ := mc.Config["scope"].(string); scope == "global" {
			cmd = append(cmd, "--global")
		}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	switch w.kind {
	case "choco":
		version, ok := chocoVersionFromOutput(res.Stdout, packageName(tool, mc))
		if !ok {
			return false
		}
		wantVersion, _ := mc.Config["version"].(string)
		return wantVersion == "" || version == wantVersion
	case "scoop":
		version, source, ok := scoopPackageFromOutput(res.Stdout, packageName(tool, mc))
		if !ok {
			return false
		}
		if wantVersion, _ := mc.Config["version"].(string); wantVersion != "" && version != wantVersion {
			return false
		}
		if bucket, _ := mc.Config["bucket"].(string); bucket != "" && !strings.EqualFold(source, bucket) {
			return false
		}
		return true
	default:
		return true
	}
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

func scoopPackageFromOutput(stdout []byte, pkg string) (version, source string, ok bool) {
	for _, line := range strings.Split(string(stdout), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 || !strings.EqualFold(fields[0], pkg) {
			continue
		}
		return fields[1], fields[2], fields[1] != ""
	}
	return "", "", false
}

func (w *winAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	if rn == nil {
		return "", fmt.Errorf("%s: no runner", w.kind)
	}
	pkg := packageName(tool, mc)
	switch w.kind {
	case "choco":
		res := rn.Run(ctx, "choco", "list", "--local-only", "--exact", "--limit-output", pkg)
		if err := run.CheckResult(res, "choco: version check"); err != nil {
			return "", err
		}
		version, ok := chocoVersionFromOutput(res.Stdout, pkg)
		if !ok {
			return "", fmt.Errorf("choco: package %q not present in version output", pkg)
		}
		return version, nil
	case "scoop":
		args := []string{"list", pkg}
		if scope, _ := mc.Config["scope"].(string); scope == "global" {
			args = append(args, "--global")
		}
		res := rn.Run(ctx, "scoop", args...)
		if err := run.CheckResult(res, "scoop: version check"); err != nil {
			return "", err
		}
		version, _, ok := scoopPackageFromOutput(res.Stdout, pkg)
		if !ok {
			return "", fmt.Errorf("scoop: package %q not present in version output", pkg)
		}
		return version, nil
	default:
		return "", nil
	}
}

func (w *winAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if rn == nil {
		return fmt.Errorf("%s: no runner", w.kind)
	}
	cmd := SubstitutePkg(w.installCmd, tool, mc)
	if w.kind == "scoop" {
		pkg := packageName(tool, mc)
		if bucket, _ := mc.Config["bucket"].(string); bucket != "" {
			pkg = bucket + "/" + pkg
		}
		if version, _ := mc.Config["version"].(string); version != "" {
			pkg += "@" + version
		}
		cmd = []string{"scoop", "install", pkg}
		if scope, _ := mc.Config["scope"].(string); scope == "global" {
			cmd = append(cmd, "--global")
		}
		if architecture, _ := mc.Config["architecture"].(string); architecture != "" {
			cmd = append(cmd, "--arch", architecture)
		}
	}
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
	if len(w.removeCmd) == 0 {
		return fmt.Errorf("%s: no remove command configured", w.kind)
	}
	cmd := SubstitutePkg(w.removeCmd, tool, mc)
	if w.kind == "scoop" {
		if scope, _ := mc.Config["scope"].(string); scope == "global" {
			cmd = append(cmd, "--global")
		}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, w.kind+": remove")
}
func (w *winAdapter) CanRemove() bool { return len(w.removeCmd) > 0 }

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

// Observe maps the Check probe onto presence semantics and records the
// resolved package name, mirroring the native adapters.
func (w *winAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if !w.Check(ctx, rn, tool, mc) {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	observation := plan.Observation{Presence: plan.PresencePresent}
	if pkg := packageName(tool, mc); pkg != "" {
		observation.Identity.Package = pkg
		observation.KnownFields = append(observation.KnownFields, plan.FieldPackage)
	}
	return observation, nil
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
		installTarget := pkg
		if bucket := resolvedSelectionSource(resolved); bucket != "" {
			installTarget = bucket + "/" + installTarget
		}
		if resolved.Identity.Version != "" {
			installTarget += "@" + resolved.Identity.Version
		}
		cmd = []string{"scoop", "install", installTarget}
		if resolved.Identity.Scope == string(plan.ScopeSystem) {
			cmd = append(cmd, "--global")
		}
		if resolved.Identity.Architecture != "" {
			cmd = append(cmd, "--arch", resolved.Identity.Architecture)
		}
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
func (w *winAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

// Compile-time interface checks.
var _ Adapter = (*winAdapter)(nil)
var _ AdapterV2 = (*winAdapter)(nil)
var _ Versioner = (*winAdapter)(nil)
