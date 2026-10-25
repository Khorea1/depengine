// Package deb installs downloaded Debian packages through dpkg and observes/removes
// the exact package identity recorded by the selected method.
package deb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

type Adapter struct{ http *httpdownload.HTTPAdapter }

func NewAdapter() *Adapter      { return &Adapter{http: httpdownload.NewDebTransportHTTPAdapter()} }
func (a *Adapter) Kind() string { return "deb" }
func (a *Adapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	return a.http.ResolvePlan(ctx, rn, tool, mc, intent)
}
func (a *Adapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "dpkg") && run.LookPath(ctx, rn, "dpkg-deb") && run.LookPath(ctx, rn, "dpkg-query")
}
func (a *Adapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}
func (a *Adapter) CheckHostCompatibility(_ *config.Tool, mc *config.MethodCandidate, _ *plan.ResolvedInstallPlan, facts *platform.Facts, clan string) error {
	return httpdownload.CheckDebHostCompatibility(mc, facts, clan)
}
func (a *Adapter) CanRemove() bool                                                     { return true }
func (a *Adapter) RequiresElevation(*config.Tool, *config.MethodCandidate) bool        { return true }
func (a *Adapter) RequiresRemovalElevation(*config.Tool, *config.MethodCandidate) bool { return true }

func (a *Adapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	observation, err := a.Observe(ctx, rn, tool, mc)
	return err == nil && observation.Presence == plan.PresencePresent
}

func (a *Adapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	pkg := packageName(tool, mc)
	arch := value(mc, "architecture")
	if pkg == "" || !validPackage(pkg) {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: "deb: package identity is missing or invalid"}, nil
	}
	output, err := queryPackageInventory(ctx, rn)
	if err != nil {
		return plan.Observation{Presence: plan.PresenceUnknown}, err
	}
	return observePackageInventory(output, pkg, arch)
}

func queryPackageInventory(ctx context.Context, rn run.Runner) (string, error) {
	// No package pattern lists actual status-database entries, mirroring --list.
	// A successful complete inventory proves absence; targeted exit 1 does not.
	result := rn.Run(ctx, "dpkg-query", "-W", "-f=${Package}\t${db:Status-Status}\t${Version}\t${Architecture}\n")
	if result.Err != nil {
		return "", fmt.Errorf("deb: query package inventory: %w", result.Err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("deb: query package inventory exited %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return string(result.Stdout), nil
}

func observePackageInventory(output, pkg, arch string) (plan.Observation, error) {
	unknown := plan.Observation{Presence: plan.PresenceUnknown}
	if output == "" {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	if !strings.HasSuffix(output, "\n") {
		return unknown, fmt.Errorf("deb: malformed package inventory output")
	}
	output = output[:len(output)-1]
	exactCount, matchingCount := 0, 0
	var matchingStatus string
	var matchingIdentity plan.ObservedIdentity
	for {
		line, remaining, more := strings.Cut(output, "\n")
		if line == "" || strings.ContainsRune(line, '\r') {
			return unknown, fmt.Errorf("deb: malformed package inventory output")
		}
		name, fields, hasName := strings.Cut(line, "\t")
		status, fields, hasStatus := strings.Cut(fields, "\t")
		version, observedArch, hasVersion := strings.Cut(fields, "\t")
		if !hasName || !hasStatus || !hasVersion || strings.ContainsRune(observedArch, '\t') || !validPackage(name) || status == "" || !validArchitecture(observedArch) {
			return unknown, fmt.Errorf("deb: malformed package inventory record")
		}
		if name == pkg {
			exactCount++
			if arch == "" || observedArch == arch {
				matchingCount++
				matchingStatus = status
				matchingIdentity = plan.ObservedIdentity{Package: name, Version: version, Architecture: observedArch}
			}
		}
		if !more {
			break
		}
		output = remaining
	}
	if arch != "" && matchingCount == 0 && exactCount > 0 {
		return unknown, fmt.Errorf("deb: package %q exists only for a different architecture than %q", pkg, arch)
	}
	if matchingCount == 0 {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	if matchingCount > 1 {
		return unknown, fmt.Errorf("deb: multiple package records match %q; configure architecture", pkg)
	}
	switch matchingStatus {
	case "not-installed", "config-files":
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	case "installed":
		if matchingIdentity.Version == "" {
			return unknown, fmt.Errorf("deb: malformed installed package inventory record")
		}
	default:
		return unknown, fmt.Errorf("deb: package %q has uncertain dpkg status %q", pkg, matchingStatus)
	}
	return plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    matchingIdentity,
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion, plan.FieldArchitecture},
	}, nil
}
func (a *Adapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil || len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL == "" {
		return fmt.Errorf("deb: resolved plan has no concrete artifact URL")
	}
	tmp, err := os.MkdirTemp("", "depengine-deb-*")
	if err != nil {
		return fmt.Errorf("deb: staging: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	staged := cloneMethod(mc)
	staged.Config["extract_to"] = tmp
	staged.Config["binary"] = "package.deb"
	staged.Config["sudo_required"] = false
	if err := a.http.InstallResolved(ctx, rn, tool, staged, resolved); err != nil {
		return fmt.Errorf("deb: download: %w", err)
	}
	path := filepath.Join(tmp, "package.deb")
	metadata, err := readMetadata(ctx, rn, path)
	if err != nil {
		return err
	}
	wantPkg := packageName(tool, mc)
	if wantPkg != "" && wantPkg != metadata.pkg {
		return fmt.Errorf("deb: package identity mismatch: artifact is %q, configured %q", metadata.pkg, wantPkg)
	}
	if wantArch := value(mc, "architecture"); wantArch != "" && wantArch != metadata.arch {
		return fmt.Errorf("deb: architecture mismatch: artifact is %q, configured %q", metadata.arch, wantArch)
	}
	if wantVersion := value(mc, "version"); wantVersion != "" && wantVersion != metadata.version {
		return fmt.Errorf("deb: version mismatch: artifact is %q, configured %q", metadata.version, wantVersion)
	}
	res := run.RunElevated(ctx, rn, "dpkg", "--install", "--", path)
	if res.Err != nil {
		return fmt.Errorf("deb: install: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("deb: install exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	if mc != nil {
		if mc.Config == nil {
			mc.Config = make(map[string]any)
		}
		mc.Config["pkg"] = metadata.pkg
		mc.Config["version"] = metadata.version
		mc.Config["architecture"] = metadata.arch
	}
	return nil
}

func (a *Adapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := packageName(tool, mc)
	arch := value(mc, "architecture")
	if pkg == "" || !validPackage(pkg) || arch == "" || !validArchitecture(arch) {
		return fmt.Errorf("deb: refusing removal without exact package and architecture identity")
	}
	query, err := queryInstalled(ctx, rn, pkg, arch)
	if err != nil {
		return err
	}
	want := value(mc, "version")
	if want == "" {
		return fmt.Errorf("deb: refusing removal without recorded package version")
	}
	if query.version != want {
		return fmt.Errorf("deb: refusing removal: installed version %q differs from recorded version %q", query.version, want)
	}
	res := run.RunElevated(ctx, rn, "dpkg", "--remove", "--", pkg+":"+arch)
	if res.Err != nil {
		return fmt.Errorf("deb: remove: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("deb: remove exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func (a *Adapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	pkg := packageName(tool, mc)
	arch := value(mc, "architecture")
	if pkg == "" || arch == "" {
		return "", fmt.Errorf("deb: package and architecture identity are required")
	}
	installed, err := queryInstalled(ctx, rn, pkg, arch)
	return installed.version, err
}

type metadata struct{ pkg, version, arch string }

func readMetadata(ctx context.Context, rn run.Runner, path string) (metadata, error) {
	res := rn.Run(ctx, "dpkg-deb", "--field", path, "Package", "Version", "Architecture")
	if res.Err != nil {
		return metadata{}, fmt.Errorf("deb: inspect package metadata: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return metadata{}, fmt.Errorf("deb: inspect package metadata exited %d", res.ExitCode)
	}
	output := strings.TrimSuffix(string(res.Stdout), "\n")
	packageLine, rest, hasPackage := strings.Cut(output, "\n")
	versionLine, architectureLine, hasVersion := strings.Cut(rest, "\n")
	if !hasPackage || !hasVersion || strings.ContainsRune(architectureLine, '\n') {
		return metadata{}, fmt.Errorf("deb: artifact has invalid Package, Version, or Architecture metadata")
	}
	pkg, hasPackage := strings.CutPrefix(packageLine, "Package: ")
	version, hasVersion := strings.CutPrefix(versionLine, "Version: ")
	arch, hasArchitecture := strings.CutPrefix(architectureLine, "Architecture: ")
	if !hasPackage || !hasVersion || !hasArchitecture || !validPackage(pkg) || version == "" || strings.ContainsAny(version, " \t\r\v\f\x00") || !validArchitecture(arch) {
		return metadata{}, fmt.Errorf("deb: artifact has invalid Package, Version, or Architecture metadata")
	}
	return metadata{pkg, version, arch}, nil
}

type installed struct{ version string }

func queryInstalled(ctx context.Context, rn run.Runner, pkg, arch string) (installed, error) {
	if !validPackage(pkg) || !validArchitecture(arch) {
		return installed{}, fmt.Errorf("deb: invalid package identity for installed-state query")
	}
	output, err := queryPackageInventory(ctx, rn)
	if err != nil {
		return installed{}, err
	}
	observation, err := observePackageInventory(output, pkg, arch)
	if err != nil {
		return installed{}, err
	}
	if observation.Presence != plan.PresencePresent {
		return installed{}, fmt.Errorf("deb: package %s:%s is not installed", pkg, arch)
	}
	return installed{version: observation.Identity.Version}, nil
}

func cloneMethod(mc *config.MethodCandidate) *config.MethodCandidate {
	clone := *mc
	clone.Config = make(map[string]any, len(mc.Config)+3)
	for key, value := range mc.Config {
		clone.Config[key] = value
	}
	return &clone
}
func value(mc *config.MethodCandidate, key string) string {
	if mc == nil {
		return ""
	}
	v, _ := mc.Config[key].(string)
	return v
}
func packageName(tool *config.Tool, mc *config.MethodCandidate) string {
	if pkg := value(mc, "pkg"); pkg != "" {
		return pkg
	}
	if tool != nil {
		return tool.Name
	}
	return ""
}

var packagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
var architecturePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func validPackage(value string) bool      { return packagePattern.MatchString(value) }
func validArchitecture(value string) bool { return architecturePattern.MatchString(value) }

var _ exec.AdapterV2 = (*Adapter)(nil)
var _ exec.ElevationRequirer = (*Adapter)(nil)
var _ exec.RemovalElevationRequirer = (*Adapter)(nil)
