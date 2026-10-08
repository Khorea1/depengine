// Package dmg mounts a macOS disk image read-only, installs exactly one
// declared .app bundle from it, and owns the exact destination copy. The
// image is transport only: no auto-open, no interaction, unconditional
// detach in every outcome.
package dmg

import (
	"context"
	"errors"
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

// Adapter installs a declared .app bundle from a staged .dmg into the
// Applications directory matching the portable scope and removes exactly
// that destination.
type Adapter struct {
	http *httpdownload.HTTPAdapter
}

func NewAdapter() *Adapter {
	return &Adapter{http: httpdownload.NewInstallerHTTPAdapter()}
}

func (a *Adapter) Kind() string { return "dmg" }

// ResolvePlan delegates read-only artifact resolution to the HTTP transport
// used by Install, so DMG dry-runs expose the concrete image URL/version.
func (a *Adapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	return a.http.ResolvePlan(ctx, rn, tool, mc, intent)
}

func (a *Adapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "hdiutil") && run.LookPath(ctx, rn, "ditto")
}

func (a *Adapter) Check(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	target, err := appTarget(mc)
	if err != nil {
		return false
	}
	info, err := os.Stat(target)
	return err == nil && info.IsDir()
}

// Observe reports whether the declared app bundle exists at the exact
// destination owned by this method. A present bundle carries only the app
// identity: the image URL/version from the plan is not part of the bundle.
func (a *Adapter) Observe(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, fmt.Errorf("dmg: tool and method are required")
	}
	app := appName(mc)
	if app == "" {
		// No identity to probe: report absent rather than fabricating a target.
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	target, err := appTarget(mc)
	if err != nil {
		return plan.Observation{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return plan.Observation{Presence: plan.PresenceAbsent}, nil
		}
		return plan.Observation{Presence: plan.PresenceBroken, Detail: fmt.Sprintf("dmg: stat %s: %v", target, err)}, err
	}
	if !info.IsDir() {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: fmt.Sprintf("dmg: %s is not a directory", target)}, fmt.Errorf("dmg: %s is not a directory", target)
	}
	return plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: app},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}, nil
}

func (a *Adapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	dmgPath, err := a.stageImage(ctx, rn, tool, mc)
	if err != nil {
		return fmt.Errorf("dmg: %w", err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(dmgPath)) }()
	return a.installStaged(ctx, rn, mc, dmgPath)
}

// InstallResolved executes the already-resolved concrete image URL without
// any release/{latest} resolution: it downloads the staged image, mounts it
// read-only, validates the declared app bundle, and copies it to the exact
// target, detaching in every outcome.
func (a *Adapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	dmgPath, err := a.stageResolved(ctx, rn, tool, mc, resolved)
	if err != nil {
		return fmt.Errorf("dmg: %w", err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(dmgPath)) }()
	return a.installStaged(ctx, rn, mc, dmgPath)
}

func (a *Adapter) stageImage(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	tmp, err := os.MkdirTemp("", "depengine-dmg-*")
	if err != nil {
		return "", fmt.Errorf("staging: %w", err)
	}
	if err := a.http.Install(ctx, rn, tool, stagedConfig(mc, tmp)); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return filepath.Join(tmp, "package.dmg"), nil
}

func (a *Adapter) stageResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (string, error) {
	if resolved == nil || len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL == "" {
		return "", fmt.Errorf("resolved plan has no concrete artifact URL")
	}
	tmp, err := os.MkdirTemp("", "depengine-dmg-*")
	if err != nil {
		return "", fmt.Errorf("staging: %w", err)
	}
	if err := a.http.InstallResolved(ctx, rn, tool, stagedConfig(mc, tmp), resolved); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return filepath.Join(tmp, "package.dmg"), nil
}

func stagedConfig(mc *config.MethodCandidate, tmp string) *config.MethodCandidate {
	clone := *mc
	clone.Config = make(map[string]any, len(mc.Config)+4)
	for key, value := range mc.Config {
		clone.Config[key] = value
	}
	clone.Config["extract_to"] = tmp
	clone.Config["binary"] = "package.dmg"
	clone.Config["sudo_required"] = false
	return &clone
}

// installStaged mounts the image read-only at a controlled mount point,
// validates the declared bundle, copies it to the scope-mapped Applications
// directory, and detaches unconditionally.
func (a *Adapter) installStaged(ctx context.Context, rn run.Runner, mc *config.MethodCandidate, dmgPath string) error {
	if _, err := appTarget(mc); err != nil {
		return err
	}
	staging, err := os.MkdirTemp("", "depengine-dmg-mount-*")
	if err != nil {
		return fmt.Errorf("mount staging: %w", err)
	}
	deferredCleanup := func() { _ = os.RemoveAll(staging) }
	defer deferredCleanup()

	mountPoint := filepath.Join(staging, "mnt")
	if err := os.Mkdir(mountPoint, 0o700); err != nil {
		return fmt.Errorf("mount point: %w", err)
	}
	res := rn.Run(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mountPoint, dmgPath)
	if res.Err != nil {
		return fmt.Errorf("attach: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("hdiutil attach exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	// Detach is attempted in every outcome after a successful attach.
	defer func() {
		detach := rn.Run(ctx, "hdiutil", "detach", mountPoint)
		if detach.Err == nil && detach.ExitCode != 0 && detach.ExitCode != 16 {
			// Exit 16 means "no such mount"; anything else is a real failure.
			_ = detach // best effort: a leaked mount is visible to the operator
		}
	}()

	return a.installFromMount(ctx, rn, mc, mountPoint)
}

// installFromMount validates the declared bundle on an attached volume and
// copies it to the scope-mapped Applications directory. It is split out so
// tests can exercise the full validate/copy/verify path against a real
// fixture bundle without invoking hdiutil.
func (a *Adapter) installFromMount(ctx context.Context, rn run.Runner, mc *config.MethodCandidate, mountPoint string) error {
	app := appName(mc)
	target, err := appTarget(mc)
	if err != nil {
		return err
	}
	bundle := filepath.Join(mountPoint, app)
	if err := validateBundle(bundle, app); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}
	copyRes := a.copyBundle(ctx, rn, mc, bundle, target)
	if copyRes.Err != nil {
		return fmt.Errorf("copy bundle: %w", copyRes.Err)
	}
	if copyRes.ExitCode != 0 {
		return fmt.Errorf("ditto exited %d: %s", copyRes.ExitCode, strings.TrimSpace(string(copyRes.Stderr)))
	}
	return nil
}

func (a *Adapter) copyBundle(ctx context.Context, rn run.Runner, mc *config.MethodCandidate, bundle, target string) run.Result {
	if systemScope(mc) {
		return run.RunElevated(ctx, rn, "ditto", bundle, target)
	}
	return rn.Run(ctx, "ditto", bundle, target)
}

// validateBundle rejects anything that is not a real .app bundle: the
// declared name must be a plain single-path .app and the mount must contain
// its Info.plist. This prevents installing arbitrary volumes or escaping the
// mount root.
func validateBundle(bundle, app string) error {
	if !strings.HasSuffix(app, ".app") || strings.Contains(app, "/") || strings.HasPrefix(app, ".") || app == ".app" || app == ".." {
		return fmt.Errorf("dmg: app %q must be a plain .app bundle name", app)
	}
	info, err := os.Stat(bundle)
	if err != nil {
		return fmt.Errorf("dmg: bundle %q not found on volume: %w", app, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("dmg: %q on volume is not a directory", app)
	}
	if _, err := os.Stat(filepath.Join(bundle, "Contents", "Info.plist")); err != nil {
		return fmt.Errorf("dmg: %q is not an app bundle (missing Contents/Info.plist): %w", app, err)
	}
	return nil
}

// Remove deletes exactly the owned destination bundle. The path is rebuilt
// through the same strict validation as install, so removal can never touch
// a directory outside the Applications root chosen by scope.
func (a *Adapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if tool == nil || mc == nil {
		return errors.New("dmg: tool and method are required")
	}
	target, err := appTarget(mc)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(target, filepath.Join("Applications", appName(mc))) {
		return fmt.Errorf("dmg: refuse to remove %s: not a scoped Applications bundle", target)
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("dmg: stat %s: %w", target, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("dmg: %s is not a directory", target)
	}
	if systemScope(mc) {
		res := run.RunElevated(ctx, rn, "rm", "-rf", target)
		if res.Err != nil {
			return fmt.Errorf("dmg: remove: %w", res.Err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("dmg: rm exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
		}
		return nil
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("dmg: remove: %w", err)
	}
	return nil
}

func (a *Adapter) CanRemove() bool { return true }

// CheckAvailable assumes availability: image URLs have no cheap local index
// to probe, so an unreachable artifact surfaces at download time.
func (a *Adapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility rejects non-macOS hosts explicitly: mounting the
// image requires hdiutil, which only exists on macOS.
func (a *Adapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("dmg disk images require macOS (hdiutil is unavailable on %s)", runtime.GOOS)
	}
	return nil
}

var _ exec.AdapterV2 = (*Adapter)(nil)
