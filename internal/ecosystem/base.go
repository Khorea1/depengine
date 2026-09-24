// Package ecosystem provides language-ecosystem adapters (cargo, go, pip, pipx,
// uv, aur). Each adapter follows the same pattern — native PATH availability,
// a package query for checks, and a package install command.
//
// The BaseAdapter struct implements exec.AdapterV2 generically; each concrete
// adapter is just a BaseConfig + registration. See the registry.go file
// for the mapping of method names to adapters.
package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
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

	// CheckPath, when set, checks an executable name directly on PATH instead
	// of running CheckTmpl. It accepts the same placeholders.
	CheckPath string

	// CheckOutput optionally validates stdout after CheckTmpl succeeds.
	CheckOutput CheckOutputMatcher

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

// CheckOutputMatcher reports whether command output confirms pkg is installed.
type CheckOutputMatcher func(output, pkg string) bool

// BaseAdapter implements exec.AdapterV2 for a BaseConfig.
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

// commandForAvailableBinary rewrites commands that invoke Binary when the
// adapter was admitted through AvailableExtra. AvailableExtra is deliberately
// limited to drop-in executable aliases (for example pip3 for pip and
// code-insiders for code), so argv after the executable remains unchanged.
func (a *BaseAdapter) commandForAvailableBinary(ctx context.Context, rn run.Runner, cmd []string) []string {
	if len(cmd) == 0 || a.config.AvailableExtra == "" || cmd[0] != a.config.Binary {
		return cmd
	}
	if run.LookPath(ctx, rn, a.config.Binary) || !run.LookPath(ctx, rn, a.config.AvailableExtra) {
		return cmd
	}
	out := append([]string(nil), cmd...)
	out[0] = a.config.AvailableExtra
	return out
}

func (a *BaseAdapter) runConfiguredBinary(ctx context.Context, rn run.Runner, args ...string) run.Result {
	cmd := append([]string{a.config.Binary}, args...)
	cmd = a.commandForAvailableBinary(ctx, rn, cmd)
	return rn.Run(ctx, cmd[0], cmd[1:]...)
}

// Check runs the check command template. Exit 0 means installed.
// Returns false immediately if the adapter's binary is not available.
func (a *BaseAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	if !a.Available(ctx, rn) {
		return false
	}
	if a.config.CheckPath != "" {
		name := a.buildCmd([]string{a.config.CheckPath}, tool, mc)[0]
		return run.LookPath(ctx, rn, name)
	}
	if a.config.KindName == "flatpak" {
		return a.checkFlatpak(ctx, rn, tool, mc)
	}
	if a.config.KindName == "snap" {
		return a.checkSnap(ctx, rn, tool, mc)
	}
	if a.config.KindName == "pip" {
		return a.checkPip(ctx, rn, tool, mc)
	}
	if a.config.KindName == "pipx" {
		return a.checkPipx(ctx, rn, tool, mc)
	}
	if a.config.KindName == "uv" {
		return a.checkUV(ctx, rn, tool, mc)
	}
	if a.config.KindName == "gem" {
		return a.checkGem(ctx, rn, tool, mc)
	}
	if a.config.KindName == "composer" {
		return a.checkComposer(ctx, rn, tool, mc)
	}
	if a.config.KindName == "bun" {
		return a.checkBun(ctx, rn, tool, mc)
	}
	if a.config.KindName == "pnpm" {
		return a.checkPNPM(ctx, rn, tool, mc)
	}
	if a.config.KindName == "yarn" {
		return a.checkYarn(ctx, rn, tool, mc)
	}
	if a.config.KindName == "npm" {
		return a.checkNPM(ctx, rn, tool, mc)
	}
	cmd := a.buildCmd(a.config.CheckTmpl, tool, mc)
	if cmd == nil {
		return false
	}
	cmd = a.commandForAvailableBinary(ctx, rn, cmd)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	if a.config.CheckOutput == nil {
		return true
	}
	return a.config.CheckOutput(string(res.Stdout), resolvedPkg(tool, mc))
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
	cmd = a.commandForAvailableBinary(ctx, rn, cmd)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, a.config.KindName+": install")
}

// ResolvePlan records the package selected by the candidate without rewriting
// planner intent. Version, registry, and other identity dimensions stay
// exactly as the planner projected them; InstallResolved consumes them as
// authoritative.
func (a *BaseAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New(a.config.KindName + ": nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New(a.config.KindName + ": tool and method are required")
	}
	if resolvedPkg(tool, mc) == "" {
		return nil, errors.New(a.config.KindName + ": no package name")
	}
	resolved := intent.Clone()
	if resolved.Identity.Package == "" {
		return nil, errors.New(a.config.KindName + ": no package name in plan intent")
	}
	return &resolved, nil
}

// Observe reports the same installed-state result as Check using
// VerificationResult-compatible semantics. A requested version is reported as
// known only because Check verified it: every ExactVersion-capable base kind
// (pip, pipx, uv, npm, pnpm, bun, gem, yarn, composer) compares the installed
// version inside Check, and kinds without version support never carry one.
func (a *BaseAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New(a.config.KindName + ": tool and method are required")
	}
	pkg := resolvedPkg(tool, mc)
	if pkg == "" {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	if !a.Check(ctx, rn, tool, mc) {
		return plan.Observation{
			Presence:    plan.PresenceAbsent,
			Identity:    plan.ObservedIdentity{Package: pkg},
			KnownFields: []plan.IdentityField{plan.FieldPackage},
		}, nil
	}
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: pkg},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}
	if version, _ := mc.Config["version"].(string); version != "" {
		observation.Identity.Version = version
		observation.KnownFields = append(observation.KnownFields, plan.FieldVersion)
	}
	return observation, nil
}

// InstallResolved executes only the identity and source selection resolved
// into the plan. Execution-only flags that carry no plan representation
// (for example confinement) still come from the candidate config.
// Explicit operations have no generic interpretation and are rejected rather
// than treated as arbitrary commands.
func (a *BaseAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return errors.New(a.config.KindName + ": runner is required")
	}
	if resolved == nil {
		return errors.New(a.config.KindName + ": nil resolved plan")
	}
	if err := validateResolvedInstallOperation(a.config.KindName, resolved); err != nil {
		return err
	}
	if resolved.Identity.Package == "" {
		return errors.New(a.config.KindName + ": no package name in resolved plan")
	}
	if !a.Available(ctx, rn) {
		return fmt.Errorf("%s: binary %q not available on PATH", a.config.KindName, a.config.Binary)
	}
	effectiveTool := tool
	if effectiveTool == nil {
		effectiveTool = &config.Tool{Name: resolved.Tool.Name}
	}
	effectiveMethod, err := resolvedMethodCandidate(mc, resolved)
	if err != nil {
		return fmt.Errorf("%s: resolved method: %w", a.config.KindName, err)
	}
	cmd := a.buildCmd(a.config.InstallTmpl, effectiveTool, effectiveMethod)
	if cmd == nil {
		return fmt.Errorf("%s: no install command", a.config.KindName)
	}
	cmd = a.commandForAvailableBinary(ctx, rn, cmd)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, a.config.KindName+": install")
}

// resolvedMethodCandidate overlays the resolved identity onto a copy of the
// candidate config so buildCmd renders the authoritative package, version,
// source selection, scope, and channel/revision selectors. Execution-only
// fields stay as configured. The input mc is never mutated; a nil mc yields a
// config derived purely from the plan.
func resolvedMethodCandidate(mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (*config.MethodCandidate, error) {
	cfg := make(map[string]any)
	kind := resolved.Candidate.Method
	if mc != nil {
		for key, value := range mc.Config {
			cfg[key] = value
		}
		if mc.Kind != "" {
			kind = mc.Kind
		}
	}
	cfg["pkg"] = resolved.Identity.Package
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
	if resolved.Identity.Source != "" {
		cfg["source"] = resolved.Identity.Source
	} else {
		delete(cfg, "source")
	}

	switch kind {
	case "pip", "pipx":
		setResolvedSourceField(cfg, "index_url", resolved.Sources, plan.SourceIndex)
	case "uv":
		setResolvedSourceField(cfg, "index", resolved.Sources, plan.SourceIndex)
	case "flatpak":
		setResolvedSourceField(cfg, "remote", resolved.Sources, plan.SourceRemote)
	}

	if resolved.Identity.Scope != "" {
		contract, ok := methodkind.Lookup(kind)
		if !ok {
			return nil, fmt.Errorf("unknown method kind %q", kind)
		}
		scope, err := contract.AdapterScope(plan.Scope(resolved.Identity.Scope))
		if err != nil {
			return nil, err
		}
		cfg["scope"] = scope
	}

	applyResolvedSelector(cfg, kind, resolved.Identity.RequestedVersion)
	return &config.MethodCandidate{Kind: kind, Config: cfg}, nil
}

func setResolvedSourceField(cfg map[string]any, field string, sources []plan.SourceReference, role plan.SourceRole) {
	delete(cfg, field)
	for _, source := range sources {
		if source.Role != role {
			continue
		}
		if source.URL != "" {
			cfg[field] = source.URL
		} else if source.Name != "" {
			cfg[field] = source.Name
		}
		return
	}
}

func applyResolvedSelector(cfg map[string]any, kind string, requested *plan.VersionIntent) {
	switch kind {
	case "flatpak":
		delete(cfg, "branch")
		if requested != nil && requested.Mode == plan.VersionGitBranch {
			cfg["branch"] = requested.Value
		}
	case "snap":
		delete(cfg, "channel")
		delete(cfg, "track")
		delete(cfg, "risk")
		delete(cfg, "branch")
		if requested == nil || requested.Mode != plan.VersionChannel || requested.Channel == nil {
			return
		}
		if requested.Channel.Name != "" {
			cfg["channel"] = requested.Channel.Name
		}
		if requested.Channel.Track != "" {
			cfg["track"] = requested.Channel.Track
		}
		if requested.Channel.Risk != "" {
			cfg["risk"] = requested.Channel.Risk
		}
	}
}

// resolvedIdentityVersion mirrors plan reconciliation: an exact requested
// version is the desired version even when Identity.Version itself is empty
// (e.g. hand-built plans that only set RequestedVersion).
func resolvedIdentityVersion(identity plan.ResolvedIdentity) string {
	if identity.Version != "" {
		return identity.Version
	}
	if identity.RequestedVersion != nil && identity.RequestedVersion.Mode == plan.VersionExact {
		return identity.RequestedVersion.Value
	}
	return ""
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
	cmd = a.commandForAvailableBinary(ctx, rn, cmd)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, a.config.KindName+": remove")
}

// buildCmd substitutes {pkg} and {bin} in the template and returns the command.
// {bin} is derived from the resolved package name via goBinaryName — the
// binary name that a Go `go install <import path>` produces (last path
// element, or the element after a "/cmd/" segment when present).
func (a *BaseAdapter) buildCmd(tmpl []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(tmpl) == 0 {
		return nil
	}
	if a.config.KindName == "flatpak" {
		return buildFlatpakCmd(tmpl, tool, mc)
	}
	cmd := exec.SubstitutePkg(tmpl, tool, mc)
	pkg := resolvedPkg(tool, mc)
	if a.config.KindName == "pip" && len(tmpl) > 1 && tmpl[1] == "install" {
		if version, _ := mc.Config["version"].(string); version != "" {
			for i := range cmd {
				if cmd[i] == pkg {
					cmd[i] = pkg + "==" + version
				}
			}
		}
		if indexURL, _ := mc.Config["index_url"].(string); indexURL != "" {
			cmd = append(cmd, "--index-url", indexURL)
		}
	}
	if a.config.KindName == "pipx" {
		cmd = buildPipxCmd(cmd, tool, mc)
	}
	if a.config.KindName == "uv" {
		cmd = buildUVCmd(cmd, tool, mc)
	}
	if a.config.KindName == "gem" {
		cmd = buildGemCmd(cmd, tool, mc)
	}
	if a.config.KindName == "composer" {
		cmd = buildComposerCmd(cmd, tool, mc)
	}
	if a.config.KindName == "bun" {
		cmd = buildBunCmd(cmd, tool, mc)
	}
	if a.config.KindName == "pnpm" {
		cmd = buildPNPMCmd(cmd, tool, mc)
	}
	if a.config.KindName == "yarn" {
		cmd = buildYarnCmd(cmd, tool, mc)
	}
	if a.config.KindName == "npm" && len(tmpl) > 1 && tmpl[1] == "install" {
		if version, _ := mc.Config["version"].(string); version != "" {
			for i := range cmd {
				if cmd[i] == pkg {
					cmd[i] = pkg + "@" + version
				}
			}
		}
		if registry, _ := mc.Config["registry"].(string); registry != "" {
			cmd = append(cmd, "--registry", registry)
		}
	}
	bin := goBinaryName(pkg)
	for i, arg := range cmd {
		cmd[i] = strings.ReplaceAll(arg, "{bin}", bin)
	}
	if a.config.KindName == "snap" && len(tmpl) > 1 && tmpl[1] == "install" {
		if confinement, _ := mc.Config["confinement"].(string); confinement == "classic" || confinement == "devmode" {
			cmd = append(cmd, "--"+confinement)
		}
		if channel := snapRequestedChannel(mc); channel != "" && channel != "stable" && channel != "latest/stable" {
			cmd = append(cmd, "--channel="+channel)
		}
	}
	return cmd
}

func snapRequestedChannel(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	if channel, _ := mc.Config["channel"].(string); channel != "" {
		return channel
	}
	track, _ := mc.Config["track"].(string)
	risk, _ := mc.Config["risk"].(string)
	branch, _ := mc.Config["branch"].(string)
	if track == "" && risk == "" && branch == "" {
		return ""
	}
	if track == "" {
		track = "latest"
	}
	if risk == "" {
		risk = "stable"
	}
	channel := track + "/" + risk
	if branch != "" {
		channel += "/" + branch
	}
	return channel
}

func snapTrackingFromList(output, pkg string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != pkg {
			continue
		}
		return fields[3], true
	}
	return "", false
}

func (a *BaseAdapter) checkSnap(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := resolvedPkg(tool, mc)
	res := rn.Run(ctx, "snap", "list", pkg)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	want := snapRequestedChannel(mc)
	if want == "" {
		return true
	}
	got, ok := snapTrackingFromList(string(res.Stdout), pkg)
	if !ok {
		return false
	}
	// Risk-only shorthand maps to Snap's implicit `latest/<risk>` track.
	if !strings.Contains(want, "/") {
		want = "latest/" + want
	}
	return got == want
}

func flatpakRef(tool *config.Tool, mc *config.MethodCandidate) string {
	ref := resolvedPkg(tool, mc)
	if branch, _ := mc.Config["branch"].(string); branch != "" && !strings.Contains(ref, "//") {
		ref += "//" + branch
	}
	return ref
}

func flatpakScopeArg(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	scope, _ := mc.Config["scope"].(string)
	switch scope {
	case "user":
		return "--user"
	case "system":
		return "--system"
	default:
		return ""
	}
}

func buildFlatpakCmd(tmpl []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(tmpl) < 2 {
		return exec.SubstitutePkg(tmpl, tool, mc)
	}
	ref := flatpakRef(tool, mc)
	scope := flatpakScopeArg(mc)
	switch tmpl[1] {
	case "install":
		cmd := []string{"flatpak", "install", "-y"}
		if scope != "" {
			cmd = append(cmd, scope)
		}
		if remote, _ := mc.Config["remote"].(string); remote != "" {
			cmd = append(cmd, remote)
		}
		return append(cmd, ref)
	case "uninstall":
		cmd := []string{"flatpak", "uninstall", "-y"}
		if scope != "" {
			cmd = append(cmd, scope)
		}
		return append(cmd, ref)
	case "info":
		cmd := []string{"flatpak", "info"}
		if scope != "" {
			cmd = append(cmd, scope)
		}
		return append(cmd, ref)
	default:
		return exec.SubstitutePkg(tmpl, tool, mc)
	}
}

func (a *BaseAdapter) checkFlatpak(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	cmd := []string{"flatpak", "info"}
	if scope := flatpakScopeArg(mc); scope != "" {
		cmd = append(cmd, scope)
	}
	remote, _ := mc.Config["remote"].(string)
	if remote != "" {
		cmd = append(cmd, "--show-origin")
	}
	cmd = append(cmd, flatpakRef(tool, mc))
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	if remote == "" {
		return true
	}
	return strings.TrimSpace(string(res.Stdout)) == remote
}

func pipVersionFromShow(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "Version") {
			continue
		}
		version := strings.TrimSpace(value)
		return version, version != ""
	}
	return "", false
}

func (a *BaseAdapter) checkPip(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := resolvedPkg(tool, mc)
	res := a.runConfiguredBinary(ctx, rn, "show", pkg)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	want, _ := mc.Config["version"].(string)
	if want == "" {
		return true
	}
	got, ok := pipVersionFromShow(string(res.Stdout))
	return ok && got == want
}

// InstalledVersion reports versions for BaseAdapter methods whose underlying
// CLI exposes a stable installed-version query. Methods without such a query
// return an empty version rather than inferring one from the requested config.
func (a *BaseAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	pkg := resolvedPkg(tool, mc)
	switch a.config.KindName {
	case "pip":
		res := a.runConfiguredBinary(ctx, rn, "show", pkg)
		if res.Err != nil || res.ExitCode != 0 {
			return "", run.CheckResult(res, "pip: show installed version")
		}
		version, ok := pipVersionFromShow(string(res.Stdout))
		if !ok {
			return "", fmt.Errorf("pip: installed package %q did not report a Version field", pkg)
		}
		return version, nil
	case "pipx":
		return a.pipxInstalledVersion(ctx, rn, pkg, mc)
	case "uv":
		return a.uvInstalledVersion(ctx, rn, pkg)
	case "gem":
		return a.gemInstalledVersion(ctx, rn, pkg)
	case "composer":
		return a.composerInstalledVersion(ctx, rn, pkg)
	case "bun":
		return a.bunInstalledVersion(ctx, rn, pkg)
	case "pnpm":
		return a.pnpmInstalledVersion(ctx, rn, pkg)
	case "yarn":
		return a.yarnInstalledVersion(ctx, rn, pkg)
	case "npm":
		return a.npmInstalledVersion(ctx, rn, pkg)
	default:
		return "", nil
	}
}

type pipxListOutput struct {
	Venvs map[string]struct {
		MainPackage struct {
			Package        string `json:"package"`
			PackageVersion string `json:"package_version"`
		} `json:"main_package"`
	} `json:"venvs"`
}

func buildPipxCmd(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(cmd) < 2 {
		return cmd
	}
	pkg := resolvedPkg(tool, mc)
	version, _ := mc.Config["version"].(string)
	scope, _ := mc.Config["scope"].(string)
	indexURL, _ := mc.Config["index_url"].(string)
	switch cmd[1] {
	case "install":
		out := []string{"pipx", "install"}
		if scope == "global" {
			out = append(out, "--global")
		}
		if indexURL != "" {
			out = append(out, "--index-url", indexURL)
		}
		spec := pkg
		if version != "" {
			spec += "==" + version
		}
		return append(out, spec)
	case "uninstall":
		out := []string{"pipx", "uninstall"}
		if scope == "global" {
			out = append(out, "--global")
		}
		return append(out, pkg)
	}
	return cmd
}

func pipxVersionFromList(output, pkg string) (string, bool) {
	var parsed pipxListOutput
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return "", false
	}
	if env, ok := parsed.Venvs[pkg]; ok && env.MainPackage.PackageVersion != "" {
		if env.MainPackage.Package == "" || strings.EqualFold(env.MainPackage.Package, pkg) {
			return env.MainPackage.PackageVersion, true
		}
	}
	for _, env := range parsed.Venvs {
		if strings.EqualFold(env.MainPackage.Package, pkg) && env.MainPackage.PackageVersion != "" {
			return env.MainPackage.PackageVersion, true
		}
	}
	return "", false
}

func (a *BaseAdapter) pipxInstalledVersion(ctx context.Context, rn run.Runner, pkg string, mc *config.MethodCandidate) (string, error) {
	args := []string{"list", "--output", "json"}
	if scope, _ := mc.Config["scope"].(string); scope == "global" {
		args = append(args, "--global")
	}
	args = append(args, pkg)
	res := a.runConfiguredBinary(ctx, rn, args...)
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "pipx: list installed version")
	}
	version, ok := pipxVersionFromList(string(res.Stdout), pkg)
	if !ok {
		return "", fmt.Errorf("pipx: installed package %q was not present in list output", pkg)
	}
	return version, nil
}

func (a *BaseAdapter) checkPipx(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := resolvedPkg(tool, mc)
	version, err := a.pipxInstalledVersion(ctx, rn, pkg, mc)
	if err != nil {
		return false
	}
	want, _ := mc.Config["version"].(string)
	return want == "" || version == want
}

func buildUVCmd(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(cmd) < 3 || cmd[0] != "uv" || cmd[1] != "tool" {
		return cmd
	}
	pkg := resolvedPkg(tool, mc)
	switch cmd[2] {
	case "install":
		out := []string{"uv", "tool", "install"}
		if index, _ := mc.Config["index"].(string); index != "" {
			out = append(out, "--index", index)
		}
		if version, _ := mc.Config["version"].(string); version != "" {
			pkg += "==" + version
		}
		return append(out, pkg)
	case "uninstall":
		return []string{"uv", "tool", "uninstall", pkg}
	}
	return cmd
}

func uvVersionFromList(output, pkg string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != pkg {
			continue
		}
		version := strings.TrimPrefix(fields[1], "v")
		if version != "" {
			return version, true
		}
	}
	return "", false
}

func (a *BaseAdapter) uvInstalledVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	res := a.runConfiguredBinary(ctx, rn, "tool", "list")
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "uv: list installed version")
	}
	version, ok := uvVersionFromList(string(res.Stdout), pkg)
	if !ok {
		return "", fmt.Errorf("uv: installed package %q was not present in tool list output", pkg)
	}
	return version, nil
}

func (a *BaseAdapter) checkUV(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	version, err := a.uvInstalledVersion(ctx, rn, resolvedPkg(tool, mc))
	if err != nil {
		return false
	}
	want, _ := mc.Config["version"].(string)
	return want == "" || version == want
}

func buildGemCmd(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(cmd) < 2 || cmd[0] != "gem" {
		return cmd
	}
	pkg := resolvedPkg(tool, mc)
	version, _ := mc.Config["version"].(string)
	source, _ := mc.Config["source"].(string)
	scope, _ := mc.Config["scope"].(string)
	switch cmd[1] {
	case "install":
		out := []string{"gem", "install", pkg}
		if version != "" {
			out = append(out, "--version", version)
		}
		if source != "" {
			out = append(out, "--clear-sources", "--source", source)
		}
		if scope == "user" {
			out = append(out, "--user-install")
		}
		return out
	case "uninstall":
		out := []string{"gem", "uninstall", pkg, "--executables", "--ignore-dependencies"}
		if version != "" {
			out = append(out, "--version", version)
		}
		if scope == "user" {
			out = append(out, "--user-install")
		}
		return out
	}
	return cmd
}

func gemVersionsFromList(output, pkg string) []string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, pkg+" (") || !strings.HasSuffix(line, ")") {
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(line, pkg+" ("), ")")
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if v := strings.TrimSpace(part); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

func (a *BaseAdapter) gemInstalledVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	res := a.runConfiguredBinary(ctx, rn, "list", "--local", "--exact", pkg, "--all")
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "gem: list installed version")
	}
	versions := gemVersionsFromList(string(res.Stdout), pkg)
	if len(versions) == 0 {
		return "", fmt.Errorf("gem: installed package %q was not present in list output", pkg)
	}
	return versions[0], nil
}

func (a *BaseAdapter) checkGem(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := resolvedPkg(tool, mc)
	res := a.runConfiguredBinary(ctx, rn, "list", "--local", "--exact", pkg, "--all")
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	versions := gemVersionsFromList(string(res.Stdout), pkg)
	if len(versions) == 0 {
		return false
	}
	want, _ := mc.Config["version"].(string)
	if want == "" {
		return true
	}
	for _, got := range versions {
		if got == want {
			return true
		}
	}
	return false
}

func buildComposerCmd(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(cmd) < 3 || cmd[0] != "composer" || cmd[1] != "global" {
		return cmd
	}
	pkg := resolvedPkg(tool, mc)
	version, _ := mc.Config["version"].(string)
	switch cmd[2] {
	case "require":
		if version != "" {
			pkg += ":" + version
		}
		return []string{"composer", "global", "require", "--no-interaction", pkg}
	case "remove":
		return []string{"composer", "global", "remove", "--no-interaction", pkg}
	case "show":
		return []string{"composer", "global", "show", "--locked", pkg}
	}
	return cmd
}

func composerVersionFromShow(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "versions" {
			continue
		}
		for _, field := range strings.Fields(value) {
			if field == "*" {
				continue
			}
			if field != "" {
				return strings.TrimPrefix(field, "v"), true
			}
		}
	}
	return "", false
}

func (a *BaseAdapter) composerInstalledVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	res := a.runConfiguredBinary(ctx, rn, "global", "show", "--locked", pkg)
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "composer: show installed version")
	}
	version, ok := composerVersionFromShow(string(res.Stdout))
	if !ok {
		return "", fmt.Errorf("composer: installed package %q did not report a versions field", pkg)
	}
	return version, nil
}

func (a *BaseAdapter) checkComposer(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	version, err := a.composerInstalledVersion(ctx, rn, resolvedPkg(tool, mc))
	if err != nil {
		return false
	}
	want, _ := mc.Config["version"].(string)
	want = strings.TrimPrefix(want, "v")
	return want == "" || version == want
}

func buildBunCmd(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(cmd) < 2 || cmd[0] != "bun" {
		return cmd
	}
	pkg := resolvedPkg(tool, mc)
	version, _ := mc.Config["version"].(string)
	registry, _ := mc.Config["registry"].(string)
	switch cmd[1] {
	case "add":
		out := []string{"bun", "add", "-g"}
		if registry != "" {
			out = append(out, "--registry", registry)
		}
		if version != "" {
			pkg += "@" + version
		}
		return append(out, pkg)
	case "remove":
		return []string{"bun", "remove", "-g", pkg}
	}
	return cmd
}

func bunVersionFromList(output, pkg string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "├└─│ ")
		idx := strings.LastIndex(line, "@")
		if idx <= 0 || idx == len(line)-1 {
			continue
		}
		if line[:idx] == pkg {
			return line[idx+1:], true
		}
	}
	return "", false
}

func (a *BaseAdapter) bunInstalledVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	res := a.runConfiguredBinary(ctx, rn, "pm", "ls", "-g")
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "bun: list installed version")
	}
	version, ok := bunVersionFromList(string(res.Stdout), pkg)
	if !ok {
		return "", fmt.Errorf("bun: installed package %q was not present in global list output", pkg)
	}
	return version, nil
}

func (a *BaseAdapter) checkBun(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	version, err := a.bunInstalledVersion(ctx, rn, resolvedPkg(tool, mc))
	if err != nil {
		return false
	}
	want, _ := mc.Config["version"].(string)
	return want == "" || version == want
}

type pnpmListOutput struct {
	Dependencies map[string]struct {
		Version string `json:"version"`
	} `json:"dependencies"`
}

func buildPNPMCmd(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(cmd) < 2 || cmd[0] != "pnpm" {
		return cmd
	}
	pkg := resolvedPkg(tool, mc)
	if version, _ := mc.Config["version"].(string); version != "" {
		pkg += "@" + version
	}
	switch cmd[1] {
	case "add":
		return []string{"pnpm", "add", "-g", pkg}
	case "remove":
		return []string{"pnpm", "remove", "-g", resolvedPkg(tool, mc)}
	}
	return cmd
}

func pnpmVersionFromList(output, pkg string) (string, bool) {
	var parsed []pnpmListOutput
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return "", false
	}
	for _, root := range parsed {
		if dep, ok := root.Dependencies[pkg]; ok && dep.Version != "" {
			return dep.Version, true
		}
	}
	return "", false
}

func (a *BaseAdapter) pnpmInstalledVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	res := a.runConfiguredBinary(ctx, rn, "list", "-g", "--depth=0", "--json")
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "pnpm: list installed version")
	}
	version, ok := pnpmVersionFromList(string(res.Stdout), pkg)
	if !ok {
		return "", fmt.Errorf("pnpm: installed package %q was not present in global list output", pkg)
	}
	return version, nil
}

func (a *BaseAdapter) checkPNPM(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	version, err := a.pnpmInstalledVersion(ctx, rn, resolvedPkg(tool, mc))
	if err != nil {
		return false
	}
	want, _ := mc.Config["version"].(string)
	return want == "" || version == want
}

func buildYarnCmd(cmd []string, tool *config.Tool, mc *config.MethodCandidate) []string {
	if len(cmd) < 3 || cmd[0] != "yarn" || cmd[1] != "global" {
		return cmd
	}
	pkg := resolvedPkg(tool, mc)
	switch cmd[2] {
	case "add":
		if version, _ := mc.Config["version"].(string); version != "" {
			pkg += "@" + version
		}
		return []string{"yarn", "global", "add", pkg}
	case "remove":
		return []string{"yarn", "global", "remove", pkg}
	}
	return cmd
}

func yarnVersionFromList(output, pkg string) (string, bool) {
	needle := pkg + "@"
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, needle)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(needle):]
		end := strings.IndexAny(rest, "\"' ,()")
		if end >= 0 {
			rest = rest[:end]
		}
		if rest != "" {
			return rest, true
		}
	}
	return "", false
}

func (a *BaseAdapter) yarnInstalledVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	res := a.runConfiguredBinary(ctx, rn, "global", "list", "--depth=0")
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "yarn: list installed version")
	}
	version, ok := yarnVersionFromList(string(res.Stdout), pkg)
	if !ok {
		return "", fmt.Errorf("yarn: installed package %q was not present in global list output", pkg)
	}
	return version, nil
}

func (a *BaseAdapter) checkYarn(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	version, err := a.yarnInstalledVersion(ctx, rn, resolvedPkg(tool, mc))
	if err != nil {
		return false
	}
	want, _ := mc.Config["version"].(string)
	return want == "" || version == want
}

type npmListOutput struct {
	Dependencies map[string]struct {
		Version string `json:"version"`
	} `json:"dependencies"`
}

func npmVersionFromList(output, pkg string) (string, bool) {
	var parsed npmListOutput
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return "", false
	}
	entry, ok := parsed.Dependencies[pkg]
	return entry.Version, ok && entry.Version != ""
}

func (a *BaseAdapter) npmInstalledVersion(ctx context.Context, rn run.Runner, pkg string) (string, error) {
	res := a.runConfiguredBinary(ctx, rn, "ls", "-g", "--depth=0", "--json", pkg)
	if res.Err != nil || res.ExitCode != 0 {
		return "", run.CheckResult(res, "npm: list installed version")
	}
	version, ok := npmVersionFromList(string(res.Stdout), pkg)
	if !ok {
		return "", fmt.Errorf("npm: installed package %q was not present in list output", pkg)
	}
	return version, nil
}

func (a *BaseAdapter) checkNPM(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := resolvedPkg(tool, mc)
	version, err := a.npmInstalledVersion(ctx, rn, pkg)
	if err != nil {
		return false
	}
	want, _ := mc.Config["version"].(string)
	return want == "" || version == want
}

func resolvedPkg(tool *config.Tool, mc *config.MethodCandidate) string {
	if pkg, ok := mc.Config["pkg"].(string); ok && pkg != "" {
		return pkg
	}
	return tool.Name
}

func checkExactLine(output, pkg string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == pkg {
			return true
		}
	}
	return false
}

func checkFirstField(output, pkg string) bool {
	return checkField(output, pkg, 0, false)
}

func checkLastFieldVersion(output, pkg string) bool {
	return checkField(output, pkg, -1, true)
}

func checkSecondFieldVersion(output, pkg string) bool {
	return checkField(output, pkg, 1, true)
}

func checkField(output, pkg string, index int, versioned bool) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		fieldIndex := index
		if fieldIndex < 0 {
			fieldIndex = len(fields) + fieldIndex
		}
		if fieldIndex < 0 || fieldIndex >= len(fields) {
			continue
		}
		field := strings.Trim(fields[fieldIndex], `"`)
		if (!versioned && field == pkg) || (versioned && strings.HasPrefix(field, pkg+"@")) {
			return true
		}
	}
	return false
}

func checkCargoPackage(output, pkg string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == pkg && strings.HasPrefix(fields[1], "v") {
			return true
		}
	}
	return false
}

// Compile-time interface checks.
// CheckAvailable assumes availability: these managers have no cheap local
// index to probe, so an unknown package surfaces at install time.
func (a *BaseAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability.
func (a *BaseAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.Versioner = (*BaseAdapter)(nil)
var _ exec.AdapterV2 = (*BaseAdapter)(nil)

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
