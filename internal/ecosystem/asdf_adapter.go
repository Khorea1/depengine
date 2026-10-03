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
	installed, foundBackend, err := asdfOrMiseInstalledVersions(ctx, rn, pkg[0])
	if err != nil || !foundBackend || len(installed.Versions) == 0 {
		return false
	}
	desired := asdfVersion(mc)
	if desired == "latest" {
		return true
	}
	for _, version := range installed.Versions {
		if version == desired {
			return true
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
	if plugins.Err != nil || plugins.ExitCode != 0 || plugins.WaitErr != nil || !asdfPluginListed(plugins.Stdout, pkg) {
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

func asdfInstalledVersions(output string) []string {
	versions := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "* ")
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		versions = append(versions, fields[0])
	}
	return versions
}

func miseInstalledVersions(output []byte) ([]string, error) {
	var rows []struct {
		Version   string `json:"version"`
		Installed bool   `json:"installed"`
	}
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("mise: parse installed versions: %w", err)
	}
	versions := make([]string, 0, len(rows))
	for _, row := range rows {
		version := strings.TrimSpace(row.Version)
		if version != "" && row.Installed {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

type asdfBackendObservation struct {
	Backend  string
	Versions []string
}

func asdfOrMiseInstalledVersions(ctx context.Context, rn run.Runner, pkg string) (asdfBackendObservation, bool, error) {
	foundBackend := false
	succeeded := false
	var lastErr error
	for _, backend := range []string{"asdf", "mise"} {
		if !run.LookPath(ctx, rn, backend) {
			continue
		}
		foundBackend = true
		var res run.Result
		if backend == "mise" {
			res = rn.Run(ctx, backend, "ls", pkg, "--installed", "--json")
		} else {
			res = rn.Run(ctx, backend, "list", pkg)
		}
		if res.Err != nil || res.ExitCode != 0 {
			lastErr = fmt.Errorf("%s list %s failed: %w", backend, pkg, run.CheckResult(res, "asdf: observe"))
			continue
		}
		var versions []string
		var err error
		if backend == "mise" {
			versions, err = miseInstalledVersions(res.Stdout)
		} else {
			versions = asdfInstalledVersions(string(res.Stdout))
		}
		if err != nil {
			lastErr = err
			continue
		}
		succeeded = true
		if len(versions) > 0 {
			return asdfBackendObservation{Backend: backend, Versions: versions}, true, nil
		}
	}
	if succeeded || !foundBackend {
		return asdfBackendObservation{}, foundBackend, nil
	}
	return asdfBackendObservation{}, true, lastErr
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
	installed, foundBackend, err := asdfOrMiseInstalledVersions(ctx, rn, pkg[0])
	if err != nil {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: err.Error()}, err
	}
	if !foundBackend || len(installed.Versions) == 0 {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: pkg[0]},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}
	desired := asdfVersion(mc)
	if desired == "latest" {
		return observation, nil
	}
	observed := installed.Versions[0]
	for _, version := range installed.Versions {
		if version == desired {
			observed = desired
			break
		}
	}
	observation.Identity.Version = observed
	observation.KnownFields = append(observation.KnownFields, plan.FieldVersion)
	return observation, nil
}

// InstalledVersion reports an exact installed version for pinned intent. When
// another version is installed instead, it reports that concrete version so
// reconciliation can classify the candidate as drifted rather than absent.
// Unpinned asdf/mise installs deliberately return an empty version because
// presence alone does not identify one desired version.
func (a *AsdfAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	if asdfVersion(mc) == "latest" {
		return "", nil
	}
	observation, err := a.Observe(ctx, rn, tool, mc)
	if err != nil {
		return "", err
	}
	if observation.Presence != plan.PresencePresent {
		return "", nil
	}
	return observation.Identity.Version, nil
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
// and `mise uninstall <plugin>@<version>` require an exact installed version.
// The version is read from mc.Config["version"]; a missing or "latest" value
// falls back to the observed installed version so a default-version install
// can be removed. Multiple installed versions without a configured version are
// ambiguous ownership and fail closed; absence fails closed too.
func (a *AsdfAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return fmt.Errorf("asdf: no package name")
	}
	version := asdfVersion(mc)
	if version == "latest" {
		installed, foundBackend, err := asdfOrMiseInstalledVersions(ctx, rn, pkg[0])
		if err != nil {
			return fmt.Errorf("asdf: resolve installed version for removal: %w", err)
		}
		if !foundBackend {
			return errors.New("asdf: neither asdf nor mise found")
		}
		if len(installed.Versions) == 0 {
			return fmt.Errorf("asdf: removal requires an installed version; none found for %q", pkg[0])
		}
		if len(installed.Versions) > 1 {
			return fmt.Errorf("asdf: removal of %q without a configured version is ambiguous (installed: %s); set config version = \"<exact installed version>\" or uninstall manually",
				pkg[0], strings.Join(installed.Versions, ", "))
		}
		version = installed.Versions[0]
		return removeAsdfVersion(ctx, rn, installed.Backend, pkg[0], version)
	}
	for _, backend := range []string{"asdf", "mise"} {
		if run.LookPath(ctx, rn, backend) {
			return removeAsdfVersion(ctx, rn, backend, pkg[0], version)
		}
	}
	return fmt.Errorf("asdf: neither asdf nor mise found")
}

func removeAsdfVersion(ctx context.Context, rn run.Runner, backend, pkg, version string) error {
	var res run.Result
	if backend == "mise" {
		res = rn.Run(ctx, backend, "uninstall", pkg+"@"+version)
	} else {
		res = rn.Run(ctx, backend, "uninstall", pkg, version)
	}
	if res.Err != nil {
		return fmt.Errorf("%s: remove failed: %w", backend, res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s: remove exited %d: %s", backend, res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
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
var _ exec.Versioner = (*AsdfAdapter)(nil)
