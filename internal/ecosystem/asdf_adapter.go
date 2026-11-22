package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
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
	installed, foundBackend, err := asdfOrMiseInstalledVersions(ctx, rn, misePackage(miseBackend(mc), pkg[0]), asdfVersion(mc))
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
	return installAsdfOrMise(ctx, rn, misePackage(miseBackend(mc), pkg[0]), asdfVersion(mc))
}

func installAsdfOrMise(ctx context.Context, rn run.Runner, pkg, version string) error {
	if isMiseBackendPackage(pkg) {
		if !run.LookPath(ctx, rn, "mise") {
			return errors.New("asdf: mise is required for the configured backend")
		}
		return installMise(ctx, rn, pkg, version)
	}
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

func misePackage(backend, pkg string) string {
	if backend == "" {
		return pkg
	}
	return backend + ":" + pkg
}

func miseBackend(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	backend, _ := mc.Config["backend"].(string)
	return backend
}

func isSupportedMiseBackend(backend string) bool {
	switch backend {
	case "aqua", "cargo", "github", "go", "npm", "pipx":
		return true
	default:
		return false
	}
}

func isMiseBackendPackage(pkg string) bool {
	for _, backend := range []string{"aqua", "cargo", "github", "go", "npm", "pipx"} {
		if strings.HasPrefix(pkg, backend+":") {
			return true
		}
	}
	return false
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

// asdf list <plugin> prints one installed version per line, with an optional
// asterisk marking the active version. A diagnostic or truncated line cannot
// establish either presence or absence.
func asdfInstalledVersions(output string) ([]string, error) {
	versions := make([]string, 0)
	seen := make(map[string]bool)
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "*") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		}
		fields := strings.Fields(line)
		if len(fields) != 1 || fields[0] != line {
			return nil, fmt.Errorf("asdf: malformed installed version line %q", raw)
		}
		if seen[line] {
			return nil, fmt.Errorf("asdf: duplicate installed version %q", line)
		}
		seen[line] = true
		versions = append(versions, line)
	}
	return versions, nil
}

func miseInstalledVersions(output []byte) ([]string, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("mise: parse installed versions: %w", err)
	}
	if rows == nil {
		return nil, errors.New("mise: installed versions must be a JSON array")
	}
	versions := make([]string, 0, len(rows))
	seen := make(map[string]bool)
	for i, raw := range rows {
		if string(raw) == "null" {
			return nil, fmt.Errorf("mise: installed version row %d is null", i)
		}
		var row struct {
			Version   string `json:"version"`
			Installed *bool  `json:"installed"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, fmt.Errorf("mise: installed version row %d: %w", i, err)
		}
		version := strings.TrimSpace(row.Version)
		if version == "" || version != row.Version || len(strings.Fields(version)) != 1 || row.Installed == nil {
			return nil, fmt.Errorf("mise: incomplete installed version row %d", i)
		}
		if seen[version] {
			return nil, fmt.Errorf("mise: duplicate version %q", version)
		}
		seen[version] = true
		if *row.Installed {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

type asdfBackendObservation struct {
	Backend  string
	Versions []string
}

// collectAsdfMiseInventories queries every available backend. Removal needs the
// entire result to establish a unique owner; an observation may still report
// positively verified presence when another backend's query fails.
func collectAsdfMiseInventories(ctx context.Context, rn run.Runner, pkg string) ([]asdfBackendObservation, bool, error) {
	backends := []string{"asdf", "mise"}
	if isMiseBackendPackage(pkg) {
		backends = []string{"mise"}
	}
	found := false
	var inventories []asdfBackendObservation
	var queryErrors []error
	for _, backend := range backends {
		if !run.LookPath(ctx, rn, backend) {
			continue
		}
		found = true
		var res run.Result
		if backend == "mise" {
			res = rn.Run(ctx, backend, "ls", pkg, "--installed", "--json")
		} else {
			res = rn.Run(ctx, backend, "list", pkg)
		}
		if err := run.CheckResultCompleteOutput(res, backend+": observe installed versions"); err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("%s: observe %q: %w", backend, pkg, err))
			continue
		}
		var versions []string
		var err error
		if backend == "mise" {
			versions, err = miseInstalledVersions(res.Stdout)
		} else {
			versions, err = asdfInstalledVersions(string(res.Stdout))
		}
		if err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("%s: observe %q: %w", backend, pkg, err))
			continue
		}
		inventories = append(inventories, asdfBackendObservation{Backend: backend, Versions: versions})
	}
	return inventories, found, errors.Join(queryErrors...)
}

func asdfOrMiseInstalledVersions(ctx context.Context, rn run.Runner, pkg, desired string) (asdfBackendObservation, bool, error) {
	inventories, found, err := collectAsdfMiseInventories(ctx, rn, pkg)
	var firstInstalled *asdfBackendObservation
	for i := range inventories {
		inventory := &inventories[i]
		if len(inventory.Versions) == 0 {
			continue
		}
		if firstInstalled == nil {
			firstInstalled = inventory
		}
		if desired != "latest" {
			for _, version := range inventory.Versions {
				if version == desired {
					return *inventory, true, nil
				}
			}
		}
	}
	// A failed backend may contain the requested version. Without a positive
	// match, a partial inventory cannot prove drift or absence.
	if err != nil && (desired != "latest" || firstInstalled == nil) {
		return asdfBackendObservation{}, found, err
	}
	if firstInstalled != nil {
		return *firstInstalled, true, nil
	}
	return asdfBackendObservation{}, found, nil
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
	if backend := miseBackend(mc); backend != "" {
		if !isSupportedMiseBackend(backend) {
			return nil, fmt.Errorf("asdf: unsupported mise backend %q", backend)
		}
		if resolved.Identity.Source != "" && resolved.Identity.Source != backend {
			return nil, fmt.Errorf("asdf: resolved source %q conflicts with mise backend %q", resolved.Identity.Source, backend)
		}
		resolved.Identity.Source = backend
	}
	return &resolved, nil
}

func (a *AsdfAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: "asdf: no package name"}, nil
	}
	installed, foundBackend, err := asdfOrMiseInstalledVersions(ctx, rn, misePackage(miseBackend(mc), pkg[0]), asdfVersion(mc))
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
	if backend := miseBackend(mc); backend != "" {
		observation.Identity.Source = backend
		observation.KnownFields = append(observation.KnownFields, plan.FieldSource)
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
	if resolved.Identity.Source != "" {
		if !isSupportedMiseBackend(resolved.Identity.Source) {
			return fmt.Errorf("asdf: unsupported resolved mise backend %q", resolved.Identity.Source)
		}
		pkg = misePackage(resolved.Identity.Source, pkg)
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
// resolves to the single observed installation. Both backends are queried to
// identify which actually owns the requested version. Ambiguous ownership,
// missing versions, and incomplete inventories fail before any uninstall.
func (a *AsdfAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return errors.New("asdf: no package name")
	}
	packageID := misePackage(miseBackend(mc), pkg[0])
	inventories, found, err := collectAsdfMiseInventories(ctx, rn, packageID)
	if err != nil {
		return fmt.Errorf("asdf: observe installed versions before removal: %w", err)
	}
	if !found {
		return errors.New("asdf: neither asdf nor mise found")
	}

	requested := asdfVersion(mc)
	type versionOwner struct {
		backend string
		version string
	}
	var owners []versionOwner
	var versions []string
	for _, inventory := range inventories {
		for _, version := range inventory.Versions {
			if requested == "latest" || version == requested {
				owners = append(owners, versionOwner{backend: inventory.Backend, version: version})
				versions = append(versions, inventory.Backend+"@"+version)
			}
		}
	}
	if len(owners) == 0 {
		if requested == "latest" {
			return fmt.Errorf("asdf: removal requires an installed version; none found for %q", pkg[0])
		}
		return fmt.Errorf("asdf: version %q is not installed for %q", requested, pkg[0])
	}
	if len(owners) > 1 {
		return fmt.Errorf("asdf: removal of %q is ambiguous (installed: %s); select a uniquely owned version or uninstall manually", pkg[0], strings.Join(versions, ", "))
	}
	return removeAsdfVersion(ctx, rn, owners[0].backend, packageID, owners[0].version)
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
func (a *AsdfAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*AsdfAdapter)(nil)
var _ exec.Versioner = (*AsdfAdapter)(nil)
