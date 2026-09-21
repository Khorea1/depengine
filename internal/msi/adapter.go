// Package msi installs Windows Installer packages and owns their uninstall identity.
package msi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
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
	res := rn.Run(ctx, "msiexec", "/package", filepath.Join(tmp, "package.msi"), "/quiet", "/norestart")
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

var _ exec.Adapter = (*Adapter)(nil)
var _ exec.PlanResolver = (*Adapter)(nil)
var _ exec.Remover = (*Adapter)(nil)
