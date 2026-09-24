package ecosystem

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
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// GoAdapter extends BaseAdapter with an import-aware Check. The check
// targets the binary name derived from the Go import path — `go install`
// never puts the import path itself on PATH, e.g.
//
//	fzf = { go = "github.com/junegunn/fzf" }
//
// installs a binary called "fzf", not "github.com/junegunn/fzf".
type GoAdapter struct {
	*BaseAdapter
}

// NewGoAdapter creates a Go adapter with import-aware Check.
func NewGoAdapter() *GoAdapter {
	return &GoAdapter{
		BaseAdapter: NewBaseAdapter(Configs["go"]),
	}
}

// Install honors an exact module/package version when declared. If pkg already
// carries an @version suffix (legacy shorthand), it is preserved rather than
// appending @latest a second time.
func (a *GoAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if !a.Available(ctx, rn) {
		return fmt.Errorf("go: binary %q not available on PATH", "go")
	}
	pkg := importPathFromTool(tool, mc)
	version, _ := mc.Config["version"].(string)
	target, err := goInstallTarget(pkg, version)
	if err != nil {
		return err
	}
	res := rn.Run(ctx, "go", "install", target)
	return run.CheckResult(res, "go: install")
}

// goInstallTarget resolves the `go install` target from an import path and an
// exact version. A version already suffixed to pkg (legacy shorthand) is
// preserved; otherwise an explicit version wins and unpinned installs default
// to @latest.
func goInstallTarget(pkg, version string) (string, error) {
	if version != "" && strings.Contains(pkg, "@") {
		return "", fmt.Errorf("go: pkg %q already contains a version; do not also set version", pkg)
	}
	target := pkg
	if version != "" {
		target += "@" + version
	} else if !strings.Contains(pkg, "@") {
		target += "@latest"
	}
	return target, nil
}

// Check runs `which {bin}` where {bin} is derived from the import path
// (last path element, or the element after /cmd/). Checking the import path
// itself could never pass. Falls back to tool.Name as the binary name when
// it differs from both the import path and the derived binary name (some
// manifests key the tool by its binary name).
func (a *GoAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	if !a.checkPresent(ctx, rn, tool, mc) {
		return false
	}
	version, _ := mc.Config["version"].(string)
	if version == "" {
		return true
	}
	installed, err := a.InstalledVersion(ctx, rn, tool, mc)
	if err != nil || installed == "" {
		return false
	}
	return sameGoVersion(installed, version)
}

func sameGoVersion(left, right string) bool {
	return strings.TrimPrefix(left, "v") == strings.TrimPrefix(right, "v")
}

func (a *GoAdapter) checkPresent(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	method := *mc
	method.Config = make(map[string]any, len(mc.Config))
	for key, value := range mc.Config {
		if key != "version" {
			method.Config[key] = value
		}
	}
	present := a.BaseAdapter.Check(ctx, rn, tool, &method)
	if !present {
		// Fallback: tool.Name may be the actual binary name (e.g. the manifest
		// key doubles as the installed binary while the import path ends in a
		// different name). Never run `which` on the import path itself.
		importPath := importPathFromTool(tool, &method)
		if tool.Name != importPath && tool.Name != goBinaryName(importPath) {
			fallbackMC := &config.MethodCandidate{
				Kind:   method.Kind,
				Config: map[string]any{"pkg": tool.Name},
			}
			present = a.BaseAdapter.Check(ctx, rn, tool, fallbackMC)
		}
	}
	return present
}

// ResolvePlan records the import path selected by the candidate without
// rewriting planner intent. The requested version stays exactly as projected;
// InstallResolved consumes it as authoritative.
func (a *GoAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("go: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("go: tool and method are required")
	}
	if importPathFromTool(tool, mc) == "" {
		return nil, errors.New("go: no package name")
	}
	resolved := intent.Clone()
	if resolved.Identity.Package == "" {
		return nil, errors.New("go: no package name in plan intent")
	}
	return &resolved, nil
}

// Observe reports the same installed-state result as Check using
// VerificationResult-compatible semantics. The installed version is read from
// the host binary when discoverable (mirroring Check's version reconciliation);
// presence is established independently from version equality so an installed
// but different exact version is reported as identity drift rather than absence.
func (a *GoAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New("go: tool and method are required")
	}
	importPath := importPathFromTool(tool, mc)
	if importPath == "" {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	absent := plan.Observation{
		Presence:    plan.PresenceAbsent,
		Identity:    plan.ObservedIdentity{Package: importPath},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}
	if !a.checkPresent(ctx, rn, tool, mc) {
		return absent, nil
	}
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: importPath},
		KnownFields: []plan.IdentityField{plan.FieldPackage},
	}
	if installed, err := a.InstalledVersion(ctx, rn, tool, mc); err == nil && installed != "" {
		// The planner preserves the requested spelling while Go treats a leading
		// "v" as equivalent for exact module versions. Reconcile is deliberately
		// adapter-neutral and exact, so collapse only that established Go
		// equivalence into the requested spelling before exposing the observation.
		if requested, _ := mc.Config["version"].(string); requested != "" && sameGoVersion(installed, requested) {
			installed = requested
		}
		observation.Identity.Version = installed
		observation.KnownFields = append(observation.KnownFields, plan.FieldVersion)
	}
	return observation, nil
}

// InstallResolved executes only the identity resolved into the plan. The
// import path and exact version come from the resolved plan (overriding the
// candidate config); unpinned plans default to @latest and a legacy @version
// suffix on the resolved package is preserved. Explicit operations have no
// go-specific interpretation and are rejected rather than treated as
// arbitrary commands.
func (a *GoAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return errors.New("go: runner is required")
	}
	if resolved == nil {
		return errors.New("go: nil resolved plan")
	}
	if err := validateResolvedInstallOperation("go", resolved); err != nil {
		return err
	}
	if resolved.Identity.Package == "" {
		return errors.New("go: no package name in resolved plan")
	}
	if !a.Available(ctx, rn) {
		return fmt.Errorf("go: binary %q not available on PATH", "go")
	}
	target, err := goInstallTarget(resolved.Identity.Package, resolvedIdentityVersion(resolved.Identity))
	if err != nil {
		return err
	}
	res := rn.Run(ctx, "go", "install", target)
	return run.CheckResult(res, "go: install")
}

// importPathFromTool returns the Go import path for a tool, mirroring how
// Install resolves {pkg}: the explicit pkg config wins, otherwise tool.Name.
func importPathFromTool(tool *config.Tool, mc *config.MethodCandidate) string {
	importPath := tool.Name
	if p, ok := mc.Config["pkg"].(string); ok && p != "" {
		importPath = p
	}
	return importPath
}

// goBinaryName derives the name of the binary that `go install <importPath>`
// produces. The binary is named after the last element of the import path;
// for the common multi-command layout `…/cmd/<name>` that element is the one
// following /cmd/ (e.g. golang.org/x/tools/cmd/stringer → stringer), so the
// element after /cmd/ is preferred when present.
func goBinaryName(importPath string) string {
	if idx := strings.LastIndex(importPath, "@"); idx >= 0 {
		importPath = importPath[:idx]
	}
	trimmed := strings.Trim(importPath, "/")
	if trimmed == "" {
		return importPath
	}
	// Multi-command repos: prefer the element right after /cmd/.
	if idx := strings.LastIndex(trimmed, "/cmd/"); idx >= 0 {
		rest := trimmed[idx+len("/cmd/"):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = rest[:i]
		}
		if rest != "" {
			return rest
		}
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}

// goBinDir resolves the directory where `go install` places binaries.
// The Go toolchain installs to $GOBIN when set, otherwise to $GOPATH/bin
// (with GOPATH defaulting to $HOME/go). depengine's install runs `go install`
// with the parent environment untouched, so removal must mirror the same
// resolution: env GOBIN, then GOPATH/bin, then $HOME/go/bin.
func goBinDir() (string, error) {
	if g := os.Getenv("GOBIN"); g != "" {
		if !filepath.IsAbs(g) {
			abs, err := filepath.Abs(g)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
		return g, nil
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for go bin dir: %w", err)
		}
		gopath = filepath.Join(home, "go")
	} else if paths := filepath.SplitList(gopath); len(paths) > 0 {
		// The go command installs binaries into the bin directory of the first
		// GOPATH entry when GOPATH is a list.
		gopath = paths[0]
	}
	return filepath.Join(gopath, "bin"), nil
}

func goInstalledBinaryPath(tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	importPath := importPathFromTool(tool, mc)
	binary := goBinaryName(importPath)
	if binary == "" || binary == "." || binary == ".." {
		return "", fmt.Errorf("cannot derive binary name from import path %q", importPath)
	}
	binDir, err := goBinDir()
	if err != nil {
		return "", fmt.Errorf("resolve GOBIN dir: %w", err)
	}
	target := filepath.Join(binDir, binary)
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	return target, nil
}

// CanRemove reports that go removals are supported (via binary deletion).
func (a *GoAdapter) CanRemove() bool { return true }

// Remove uninstalls a go tool by deleting the binary that `go install` placed
// in the GOBIN directory. `go clean` is NOT used — it only clears the build
// cache and leaves the installed binary in place. Removing an already-missing
// binary is treated as success (idempotent removal, matching the git adapter).
func (a *GoAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	target, err := goInstalledBinaryPath(tool, mc)
	if err != nil {
		return fmt.Errorf("go: %w", err)
	}
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("go: remove binary %s: %w", target, err)
	}
	return nil
}

// InstalledVersion reports the module version embedded by the Go toolchain in
// the binary installed to GOBIN/GOPATH/bin. It deliberately asks the trusted
// `go` command to inspect build metadata instead of executing the installed
// program with an ad-hoc --version convention.
func (a *GoAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	if rn == nil {
		return "", errors.New("go: runner is required")
	}
	target, err := goInstalledBinaryPath(tool, mc)
	if err != nil {
		return "", fmt.Errorf("go: %w", err)
	}
	res := rn.Run(ctx, "go", "version", "-m", target)
	if err := run.CheckResult(res, "go: inspect installed build info"); err != nil {
		return "", err
	}
	expectedPath := importPathFromTool(tool, mc)
	if idx := strings.LastIndex(expectedPath, "@"); idx >= 0 {
		expectedPath = expectedPath[:idx]
	}
	return goModuleVersionFromBuildInfo(res.Stdout, expectedPath)
}

func goModuleVersionFromBuildInfo(stdout []byte, expectedPath string) (string, error) {
	var packagePath, moduleVersion string
	for _, line := range strings.Split(string(stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "path":
			packagePath = fields[1]
		case "mod":
			if len(fields) >= 3 {
				moduleVersion = fields[2]
			}
		}
	}
	if packagePath == "" {
		return "", errors.New("go: build info did not report a package path")
	}
	if expectedPath != "" && packagePath != expectedPath {
		return "", fmt.Errorf("go: build info package %q does not match requested package %q", packagePath, expectedPath)
	}
	if moduleVersion == "" || moduleVersion == "(devel)" {
		return "", nil
	}
	return moduleVersion, nil
}

// Ensure GoAdapter implements exec.AdapterV2 and exec.Remover.
// CheckAvailable assumes availability: the Go module proxy has no cheap
// local index to probe, so an unknown module surfaces at install time.
func (a *GoAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability.
func (a *GoAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*GoAdapter)(nil)
var _ exec.Versioner = (*GoAdapter)(nil)
