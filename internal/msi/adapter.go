// Package msi installs Windows Installer packages and owns their uninstall identity.
package msi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type productFinder interface {
	Find(name, publisher string) (string, bool, error)
}

type Adapter struct {
	http     *httpdownload.HTTPAdapter
	products productFinder
}

func NewAdapter() *Adapter {
	return &Adapter{http: httpdownload.NewHTTPAdapter(), products: newProductFinder()}
}
func (a *Adapter) Kind() string { return "msi" }

// ResolvePlan delegates read-only artifact resolution to the HTTP transport
// used by Install, so MSI dry-runs expose the concrete package URL/version.
func (a *Adapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	return a.http.ResolvePlan(ctx, rn, tool, mc, intent)
}
func (a *Adapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "msiexec")
}

func (a *Adapter) Check(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	_, ok, _ := a.products.Find(stringValue(mc, "product_name"), stringValue(mc, "publisher"))
	return ok
}

// Observe reports whether the Windows product is registered. The registry
// probe establishes presence only: it cannot verify the download URL or
// version the plan resolved, so a present observation carries no identity
// fields and reconciliation treats the install as-is.
func (a *Adapter) Observe(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, fmt.Errorf("msi: tool and method are required")
	}
	_, ok, err := a.products.Find(stringValue(mc, "product_name"), stringValue(mc, "publisher"))
	if err != nil {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: fmt.Sprintf("msi: query installed products: %v", err)}, err
	}
	if !ok {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	return plan.Observation{Presence: plan.PresencePresent}, nil
}

func (a *Adapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	tmp, err := os.MkdirTemp("", "depengine-msi-*")
	if err != nil {
		return fmt.Errorf("msi: staging: %w", err)
	}
	defer os.RemoveAll(tmp)
	clone := *mc
	clone.Config = make(map[string]any, len(mc.Config)+3)
	for key, value := range mc.Config {
		clone.Config[key] = value
	}
	clone.Config["extract_to"] = tmp
	clone.Config["binary"] = "package.msi"
	clone.Config["_allow_installer"] = true
	clone.Config["sudo_required"] = false
	if err := a.http.Install(ctx, rn, tool, &clone); err != nil {
		return fmt.Errorf("msi: %w", err)
	}
	return a.runMsiexec(ctx, rn, mc, filepath.Join(tmp, "package.msi"))
}

// InstallResolved executes the already-resolved concrete MSI URL without any
// release/{latest} resolution: it downloads the staged package via the HTTP
// transport and only then invokes msiexec.
func (a *Adapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil || len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL == "" {
		return fmt.Errorf("msi: resolved plan has no concrete artifact URL")
	}
	tmp, err := os.MkdirTemp("", "depengine-msi-*")
	if err != nil {
		return fmt.Errorf("msi: staging: %w", err)
	}
	defer os.RemoveAll(tmp)
	clone := *mc
	clone.Config = make(map[string]any, len(mc.Config)+3)
	for key, value := range mc.Config {
		clone.Config[key] = value
	}
	clone.Config["extract_to"] = tmp
	clone.Config["binary"] = "package.msi"
	clone.Config["_allow_installer"] = true
	clone.Config["sudo_required"] = false
	if err := a.http.InstallResolved(ctx, rn, tool, &clone, resolved); err != nil {
		return fmt.Errorf("msi: %w", err)
	}
	return a.runMsiexec(ctx, rn, mc, filepath.Join(tmp, "package.msi"))
}

func (a *Adapter) runMsiexec(ctx context.Context, rn run.Runner, mc *config.MethodCandidate, packagePath string) error {
	res := rn.Run(ctx, "msiexec", "/package", packagePath, "/quiet", "/norestart")
	if res.Err != nil {
		return fmt.Errorf("msi: install: %w", res.Err)
	}
	if res.ExitCode != 0 && res.ExitCode != 1641 && res.ExitCode != 3010 {
		return fmt.Errorf("msi: install exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	if res.ExitCode == 1641 || res.ExitCode == 3010 {
		mc.Config["_reboot_required"] = true
	}
	return nil
}

func (a *Adapter) Remove(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) error {
	code, ok, err := a.products.Find(stringValue(mc, "product_name"), stringValue(mc, "publisher"))
	if err != nil {
		return fmt.Errorf("msi: query installed products: %w", err)
	}
	if !ok {
		return fmt.Errorf("msi: product %q is not installed", stringValue(mc, "product_name"))
	}
	res := rn.Run(ctx, "msiexec", "/uninstall", code, "/quiet", "/norestart")
	if res.Err != nil {
		return fmt.Errorf("msi: uninstall: %w", res.Err)
	}
	if res.ExitCode != 0 && res.ExitCode != 1641 && res.ExitCode != 3010 {
		return fmt.Errorf("msi: uninstall exited %d", res.ExitCode)
	}
	return nil
}

func (a *Adapter) CanRemove() bool { return true }

func stringValue(mc *config.MethodCandidate, key string) string {
	value, _ := mc.Config[key].(string)
	return value
}

// CheckAvailable assumes availability: installer URLs have no cheap local
// index to probe, so an unreachable artifact surfaces at download time.
func (a *Adapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability: the MSI payload is Windows-scoped by the method contract.
func (a *Adapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*Adapter)(nil)
