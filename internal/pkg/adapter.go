// Package pkg installs signed macOS Installer packages and owns their receipt
// identity. Removal is intentionally unsupported: Installer packages can run
// arbitrary scripts, and depengine cannot prove which filesystem effects are
// reversible, so it never claims removal it cannot guarantee.
package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

// Adapter installs a staged .pkg through the macOS `installer` binary and
// observes its package receipt through `pkgutil`. Package signatures are
// verified before installation unless the schema explicitly allows untrusted
// packages.
type Adapter struct {
	http *httpdownload.HTTPAdapter
}

func NewAdapter() *Adapter {
	return &Adapter{http: httpdownload.NewInstallerHTTPAdapter()}
}

func (a *Adapter) Kind() string { return "macpkg" }

// ResolvePlan delegates read-only artifact resolution to the HTTP transport
// used by Install, so PKG dry-runs expose the concrete package URL/version.
func (a *Adapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	return a.http.ResolvePlan(ctx, rn, tool, mc, intent)
}

func (a *Adapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "installer") && run.LookPath(ctx, rn, "pkgutil")
}

// Check reports whether the package receipt is registered.
func (a *Adapter) Check(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	present, _, _ := queryReceipt(ctx, rn, stringValue(mc, "package_id"))
	return present
}

// Observe reports the registered package receipt. The receipt version is
// reported when pkgutil exposes one; presence alone carries only the package
// identity because the download URL is not part of the receipt.
func (a *Adapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, fmt.Errorf("pkg: tool and method are required")
	}
	packageID := stringValue(mc, "package_id")
	if packageID == "" {
		// No identity to query: report absent so presence-only reconciliation
		// does not fabricate a receipt, mirroring the MSI adapter.
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	present, version, err := queryReceipt(ctx, rn, packageID)
	if err != nil {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: fmt.Sprintf("pkg: query receipt: %v", err)}, err
	}
	if !present {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: packageID},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}
	if version != "" {
		observation.Identity.Version = version
		observation.KnownFields = append(observation.KnownFields, plan.FieldVersion)
	}
	return observation, nil
}

func (a *Adapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkgPath, err := a.stagePackage(ctx, rn, tool, mc)
	if err != nil {
		return fmt.Errorf("pkg: %w", err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(pkgPath)) }()
	return a.installStaged(ctx, rn, mc, pkgPath)
}

// InstallResolved executes the already-resolved concrete package URL without
// any release/{latest} resolution: it downloads the staged package via the
// HTTP transport and only then verifies and installs it.
func (a *Adapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	pkgPath, err := a.stageResolved(ctx, rn, tool, mc, resolved)
	if err != nil {
		return fmt.Errorf("pkg: %w", err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(pkgPath)) }()
	return a.installStaged(ctx, rn, mc, pkgPath)
}

// stagePackage downloads the package through the shared HTTP transport into a
// temporary staging directory and returns the staged file path. The caller
// owns cleanup of the returned path's directory.
func (a *Adapter) stagePackage(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	tmp, err := os.MkdirTemp("", "depengine-pkg-*")
	if err != nil {
		return "", fmt.Errorf("staging: %w", err)
	}
	if err := a.http.Install(ctx, rn, tool, stagedConfig(mc, tmp)); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return filepath.Join(tmp, "package.pkg"), nil
}

func (a *Adapter) stageResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (string, error) {
	if resolved == nil || len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL == "" {
		return "", fmt.Errorf("resolved plan has no concrete artifact URL")
	}
	tmp, err := os.MkdirTemp("", "depengine-pkg-*")
	if err != nil {
		return "", fmt.Errorf("staging: %w", err)
	}
	if err := a.http.InstallResolved(ctx, rn, tool, stagedConfig(mc, tmp), resolved); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return filepath.Join(tmp, "package.pkg"), nil
}

// stagedConfig points the HTTP transport at a raw download named package.pkg
// in the staging directory, mirroring the MSI adapter's transport reuse.
func stagedConfig(mc *config.MethodCandidate, tmp string) *config.MethodCandidate {
	clone := *mc
	clone.Config = make(map[string]any, len(mc.Config)+4)
	for key, value := range mc.Config {
		clone.Config[key] = value
	}
	clone.Config["extract_to"] = tmp
	clone.Config["binary"] = "package.pkg"
	clone.Config["sudo_required"] = false
	return &clone
}

// installStaged verifies the package signature unless the schema explicitly
// opts into untrusted packages, then invokes the macOS installer with fixed
// argv through the elevation seam. Installer packages require administrative
// privileges for the / target; the elevated runner handles that prompt, never
// a shell.
func (a *Adapter) installStaged(ctx context.Context, rn run.Runner, mc *config.MethodCandidate, pkgPath string) error {
	if !allowUntrusted(mc) {
		res := rn.Run(ctx, "pkgutil", "--check-signature", pkgPath)
		if res.Err != nil {
			return fmt.Errorf("signature check: %w", res.Err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("package signature check failed (rejected untrusted package; set allow_untrusted to override): %s",
				strings.TrimSpace(string(res.Stderr)))
		}
	}
	res := run.RunElevated(ctx, rn, "installer", "-pkg", pkgPath, "-target", "/")
	if res.Err != nil {
		return fmt.Errorf("install: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("installer exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func allowUntrusted(mc *config.MethodCandidate) bool {
	if mc == nil {
		return false
	}
	allowed, _ := mc.Config["allow_untrusted"].(bool)
	return allowed
}

func stringValue(mc *config.MethodCandidate, key string) string {
	if mc == nil {
		return ""
	}
	value, _ := mc.Config[key].(string)
	return value
}

// Remove is unsupported by contract: Installer packages are not safely
// reversible by receipt deletion. The executor surfaces a manual-removal
// instruction instead.
func (a *Adapter) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return fmt.Errorf("pkg: removal is not supported; installers can mutate the system outside package receipts, use the vendor uninstaller or restore from a snapshot")
}

func (a *Adapter) CanRemove() bool { return false }

// CheckAvailable assumes availability: installer URLs have no cheap local
// index to probe, so an unreachable artifact surfaces at download time.
func (a *Adapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility rejects non-macOS hosts explicitly because the
// installer and receipt APIs are platform-specific.
func (a *Adapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("macOS installer packages require macOS (installer is unavailable on %s)", runtime.GOOS)
	}
	return nil
}

var _ exec.AdapterV2 = (*Adapter)(nil)
