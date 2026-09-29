// Package windowsinstaller implements executable, MSIX, and AppX installers.
package windowsinstaller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type packageCatalog interface {
	Find(context.Context, run.Runner, string, string, string) (string, bool, error)
}

type Adapter struct {
	kind    string
	http    *httpdownload.HTTPAdapter
	catalog packageCatalog
}

func NewEXEAdapter() *Adapter  { return newAdapter("exe") }
func NewMSIXAdapter() *Adapter { return newAdapter("msix") }
func NewAPPXAdapter() *Adapter { return newAdapter("appx") }

func newAdapter(kind string) *Adapter {
	return &Adapter{kind: kind, http: httpdownload.NewHTTPAdapter(), catalog: newCatalog()}
}

func (a *Adapter) Kind() string { return a.kind }

func (a *Adapter) Available(ctx context.Context, rn run.Runner) bool {
	return runtime.GOOS == "windows" && run.LookPath(ctx, rn, "powershell.exe")
}

func (a *Adapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

func (a *Adapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	return a.http.ResolvePlan(ctx, rn, tool, mc, intent)
}

func (a *Adapter) Observe(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	name, publisher, err := a.identity(mc)
	if err != nil {
		return plan.Observation{}, err
	}
	_, found, err := a.catalog.Find(ctx, rn, a.kind, name, publisher)
	if err != nil {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: fmt.Sprintf("%s: query installed identity: %v", a.kind, err)}, err
	}
	if !found {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	return plan.Observation{Presence: plan.PresencePresent}, nil
}

func (a *Adapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return fmt.Errorf("%s: no runner", a.kind)
	}
	if mc == nil || resolved == nil || len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL == "" {
		return fmt.Errorf("%s: resolved plan has no concrete artifact URL", a.kind)
	}
	if _, _, err := a.identity(mc); err != nil {
		return err
	}
	if a.kind == "exe" {
		if _, err := stringList(mc, "install_args", true); err != nil {
			return err
		}
	} else if scope := stringValue(mc, "scope"); scope != "" && scope != "user" {
		return fmt.Errorf("%s: only user scope is supported", a.kind)
	}

	tmp, err := os.MkdirTemp("", "depengine-"+a.kind+"-*")
	if err != nil {
		return fmt.Errorf("%s: staging: %w", a.kind, err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	clone := cloneMethod(mc)
	filename := "package." + a.kind
	clone.Config["extract_to"] = tmp
	clone.Config["binary"] = filename
	clone.Config["_allow_installer"] = true
	clone.Config["sudo_required"] = false
	if err := a.http.InstallResolved(ctx, rn, tool, clone, resolved); err != nil {
		return fmt.Errorf("%s: %w", a.kind, err)
	}
	path := filepath.Join(tmp, filename)
	if a.kind == "exe" {
		args, _ := stringList(mc, "install_args", true)
		return run.CheckResult(rn.Run(ctx, path, args...), "exe: install")
	}
	return installPackage(ctx, rn, path, a.kind)
}

func (a *Adapter) Remove(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) error {
	if rn == nil {
		return fmt.Errorf("%s: no runner", a.kind)
	}
	name, publisher, err := a.identity(mc)
	if err != nil {
		return err
	}
	identity, found, err := a.catalog.Find(ctx, rn, a.kind, name, publisher)
	if err != nil {
		return fmt.Errorf("%s: query installed identity: %w", a.kind, err)
	}
	if !found {
		return nil
	}
	if a.kind == "exe" {
		uninstaller := stringValue(mc, "uninstall_exe")
		if uninstaller == "" {
			return errors.New("exe: uninstall_exe is required for safe removal")
		}
		if !filepath.IsAbs(uninstaller) {
			return errors.New("exe: uninstall_exe must be an absolute path")
		}
		args, err := stringList(mc, "uninstall_args", false)
		if err != nil {
			return err
		}
		return run.CheckResult(rn.Run(ctx, uninstaller, args...), "exe: uninstall")
	}
	if scope := stringValue(mc, "scope"); scope != "" && scope != "user" {
		return fmt.Errorf("%s: only user scope is supported", a.kind)
	}
	return removePackage(ctx, rn, identity)
}

func (a *Adapter) CanRemove() bool { return true }

func (a *Adapter) CheckHostCompatibility(_ *config.Tool, _ *config.MethodCandidate, _ *plan.ResolvedInstallPlan, _ *engine.Facts, clan string) error {
	if clan != "windows" {
		return fmt.Errorf("%s: Windows host required", a.kind)
	}
	return nil
}

func (a *Adapter) identity(mc *config.MethodCandidate) (string, string, error) {
	if mc == nil {
		return "", "", fmt.Errorf("%s: method configuration is required", a.kind)
	}
	nameField := "pkg"
	if a.kind == "exe" {
		nameField = "product_name"
	}
	name, publisher := stringValue(mc, nameField), stringValue(mc, "publisher")
	if name == "" || publisher == "" {
		return "", "", fmt.Errorf("%s: exact %s and publisher are required", a.kind, nameField)
	}
	return name, publisher, nil
}

func cloneMethod(mc *config.MethodCandidate) *config.MethodCandidate {
	out := *mc
	out.Config = make(map[string]any, len(mc.Config)+4)
	for key, value := range mc.Config {
		out.Config[key] = value
	}
	return &out
}

func stringValue(mc *config.MethodCandidate, key string) string {
	if mc == nil {
		return ""
	}
	value, _ := mc.Config[key].(string)
	return strings.TrimSpace(value)
}

func stringList(mc *config.MethodCandidate, key string, required bool) ([]string, error) {
	value, ok := mc.Config[key]
	if !ok {
		if required {
			return nil, fmt.Errorf("%s: %s must be an explicit tokenized argument list", mc.Kind, key)
		}
		return nil, nil
	}
	args, ok := value.([]string)
	if !ok {
		return nil, fmt.Errorf("%s: %s must be an explicit tokenized argument list", mc.Kind, key)
	}
	return args, nil
}

var _ exec.AdapterV2 = (*Adapter)(nil)
