package httpdownload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// appImageDesktopDir is fixed (not configurable) — the TODO this adapter
// implements is explicit that the shortcut always goes to the standard
// XDG user applications directory, never a system one, so it never needs
// sudo_required at all.
const appImageDesktopDir = "~/.local/share/applications"

// AppImageAdapter implements exec.Adapter for the "appimage" method kind:
// resolves a URL exactly like "http" does ({latest}/{version}/{arch}/{os}
// placeholders, checksum verification, retry/cache), then does the
// AppImage-specific part HTTPAdapter doesn't know about — installing under
// a STABLE name (not the versioned filename the release asset ships with)
// and, optionally, a .desktop launcher.
//
// Config fields:
//
//	url         (required) same meaning as on "http"
//	install_dir (optional) destination directory; default ~/.local/bin
//	             (user-scope). Pointing this at a system path (e.g.
//	             /usr/local/bin) is how a system-wide install is requested —
//	             there is no separate "system" boolean; sudo_required is
//	             derived from install_dir the same way "http" derives it
//	             from extract_to (see defaultSudoRequired), and can be
//	             overridden explicitly.
//	binary      (optional) final executable name; default: the tool name.
//	desktop     (optional bool) also write a .desktop launcher to
//	             ~/.local/share/applications/<binary>.desktop.
//
// Every other http field (checksum, checksum_url, signing_key, ...) has the
// exact same meaning as on "http", because the download itself is delegated
// to HTTPAdapter unchanged.
type AppImageAdapter struct {
	http *HTTPAdapter
}

// NewAppImageAdapter creates an "appimage" adapter, delegating download,
// checksum and retry logic to an HTTPAdapter.
func NewAppImageAdapter() *AppImageAdapter {
	return &AppImageAdapter{http: NewHTTPAdapter()}
}

func (a *AppImageAdapter) Kind() string { return "appimage" }

// ResolvePlan reuses HTTP artifact resolution so dry-run reports the concrete
// URL/version consumed by the delegated downloader.
func (a *AppImageAdapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	return resolveDownloadPlan(ctx, rn, mc, intent)
}

func (a *AppImageAdapter) RequiresElevation(tool *config.Tool, mc *config.MethodCandidate) bool {
	installDir, name := binaryTarget(tool, mc)
	return a.http.RequiresElevation(tool, httpDelegate(mc, installDir, name))
}

// Available mirrors HTTPAdapter: Go's net/http is always available.
func (a *AppImageAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return a.http.Available(ctx, rn)
}

// binaryTarget resolves the (install_dir, binary name) pair a config
// candidate targets, applying the appimage-specific defaults HTTPAdapter
// doesn't know about (~/.local/bin instead of /usr/local/bin, tool name
// instead of a URL-derived filename).
func binaryTarget(tool *config.Tool, mc *config.MethodCandidate) (installDir, name string) {
	installDir, _ = mc.Config["install_dir"].(string)
	if installDir == "" {
		installDir = "~/.local/bin"
	}
	installDir = config.ExpandHomeDir(installDir)

	name, _ = mc.Config["binary"].(string)
	if name == "" && tool != nil {
		name = tool.Name
	}
	return installDir, name
}

// httpDelegate builds the HTTPAdapter-facing candidate: same Config, but
// extract_to/binary point at the resolved (installDir, name) pair instead
// of whatever "http" would have defaulted to.
func httpDelegate(mc *config.MethodCandidate, installDir, name string) *config.MethodCandidate {
	cfg := make(map[string]any, len(mc.Config)+2)
	for k, v := range mc.Config {
		cfg[k] = v
	}
	cfg["extract_to"] = installDir
	cfg["binary"] = name
	return &config.MethodCandidate{Kind: "http", Label: mc.Label, When: mc.When, Config: cfg}
}

// Check delegates to HTTPAdapter.Check against the resolved install_dir and
// binary name — the same "does the target file exist" logic "http" already
// implements, no network resolution needed.
func (a *AppImageAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	installDir, name := binaryTarget(tool, mc)
	if name == "" {
		return false
	}
	return a.http.Check(ctx, rn, tool, httpDelegate(mc, installDir, name))
}

// Install downloads the AppImage under its stable binary name via HTTPAdapter
// and optionally writes a .desktop launcher.
func (a *AppImageAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if err := requireMethodArtifact("appimage", mc); err != nil {
		return err
	}

	installDir, name := binaryTarget(tool, mc)
	if name == "" {
		return fmt.Errorf("appimage: tool %q has no name and no binary field to install under", tool.Name)
	}

	if err := a.http.Install(ctx, rn, tool, httpDelegate(mc, installDir, name)); err != nil {
		return fmt.Errorf("appimage: %w", err)
	}

	if desktop, _ := mc.Config["desktop"].(bool); desktop {
		if err := writeDesktopEntry(tool, name, filepath.Join(installDir, name)); err != nil {
			return fmt.Errorf("appimage: desktop entry: %w", err)
		}
	}

	return nil
}

// desktopEntryTemplate is the minimal valid .desktop file per the
// freedesktop.org Desktop Entry Specification — just enough for a launcher
// to appear and run the installed binary.
const desktopEntryTemplate = `[Desktop Entry]
Type=Application
Name=%s
Exec=%s
Terminal=false
Categories=Utility;
`

// writeDesktopEntry writes a .desktop launcher for the installed binary to
// the fixed, user-scope XDG applications directory. Always user-scope by
// design (see appImageDesktopDir) — a system-wide launcher would need its
// own sudo handling for no real benefit, since XDG picks up per-user
// entries for the desktop environment either way.
func writeDesktopEntry(tool *config.Tool, name, execPath string) error {
	dir := config.ExpandHomeDir(appImageDesktopDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	displayName := name
	if tool != nil && tool.Name != "" {
		displayName = tool.Name
	}
	content := fmt.Sprintf(desktopEntryTemplate, displayName, execPath)
	path := filepath.Join(dir, name+".desktop")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Remove deletes the installed binary via HTTPAdapter.Remove, plus the
// .desktop launcher when one was configured. The launcher is removed based
// on current schema state (mc.Config["desktop"]) rather than any
// installed-state record — if the schema changed between install and
// remove, that's the same limitation every other config-driven removal in
// this codebase already has.
func (a *AppImageAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	installDir, name := binaryTarget(tool, mc)
	if err := a.http.Remove(ctx, rn, tool, httpDelegate(mc, installDir, name)); err != nil {
		return fmt.Errorf("appimage: %w", err)
	}

	if desktop, _ := mc.Config["desktop"].(bool); desktop && name != "" {
		path := filepath.Join(config.ExpandHomeDir(appImageDesktopDir), name+".desktop")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("appimage: remove desktop entry %s: %w", path, err)
		}
	}
	return nil
}

func (a *AppImageAdapter) CanRemove() bool { return a.http.CanRemove() }

// Compile-time interface checks.
var _ exec.Adapter = (*AppImageAdapter)(nil)
var _ exec.PlanResolver = (*AppImageAdapter)(nil)
var _ exec.Remover = (*AppImageAdapter)(nil)
