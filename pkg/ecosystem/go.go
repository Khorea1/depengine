package ecosystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
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

// Check runs `which {bin}` where {bin} is derived from the import path
// (last path element, or the element after /cmd/). Checking the import path
// itself could never pass. Falls back to tool.Name as the binary name when
// it differs from both the import path and the derived binary name (some
// manifests key the tool by its binary name).
func (a *GoAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	// which {bin} — the binary name derived from the import path.
	if a.BaseAdapter.Check(ctx, rn, tool, mc) {
		return true
	}

	// Fallback: tool.Name may be the actual binary name (e.g. the manifest
	// key doubles as the installed binary while the import path ends in a
	// different name). Never run `which` on the import path itself.
	importPath := importPathFromTool(tool, mc)
	if tool.Name != importPath && tool.Name != goBinaryName(importPath) {
		fallbackMC := &config.MethodCandidate{
			Kind:   mc.Kind,
			Config: map[string]any{"pkg": tool.Name},
		}
		return a.BaseAdapter.Check(ctx, rn, tool, fallbackMC)
	}
	return false
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
	}
	return filepath.Join(gopath, "bin"), nil
}

// CanRemove reports that go removals are supported (via binary deletion).
func (a *GoAdapter) CanRemove() bool { return true }

// Remove uninstalls a go tool by deleting the binary that `go install` placed
// in the GOBIN directory. `go clean` is NOT used — it only clears the build
// cache and leaves the installed binary in place. Removing an already-missing
// binary is treated as success (idempotent removal, matching the git adapter).
func (a *GoAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	importPath := importPathFromTool(tool, mc)
	binary := goBinaryName(importPath)
	binDir, err := goBinDir()
	if err != nil {
		return fmt.Errorf("go: resolve GOBIN dir: %w", err)
	}
	if binary == "" || binary == "." || binary == ".." {
		return fmt.Errorf("go: cannot derive binary name from import path %q", importPath)
	}
	target := filepath.Join(binDir, binary)
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("go: remove binary %s: %w", target, err)
	}
	return nil
}

// InstalledVersion reports the installed binary's version by running
// "<binary> --version" (or "<binary> version") and parsing the output.
// Best-effort: returns "" when the binary or a version string cannot be
// determined.
func (a *GoAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	bin := goBinaryName(importPathFromTool(tool, mc))
	// Strip any version suffix (e.g. "pkg@v1.2.3").
	if idx := strings.LastIndex(bin, "@"); idx >= 0 {
		bin = bin[:idx]
	}
	if bin == "" || bin == "." || bin == ".." {
		return "", nil
	}

	res := rn.Run(ctx, bin, "--version")
	if res.Err != nil || res.ExitCode != 0 {
		res = rn.Run(ctx, bin, "version")
		if res.Err != nil || res.ExitCode != 0 {
			return "", nil
		}
	}
	line := strings.TrimSpace(string(res.Stdout))
	// Version banners can include build info on later lines.
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return parseVersion(line), nil
}

// parseVersion extracts the first version-looking token from a command output
// line (e.g. "gh version 2.45.0 (2024-...)" → "2.45.0"). Returns "" when
// no token looks like a version.
func parseVersion(line string) string {
	for _, tok := range strings.Fields(line) {
		if v := versionToken(tok); v != "" {
			return v
		}
	}
	return ""
}

// versionToken returns tok as a version string, or "" when tok does not
// look like a version. Accepts a leading "v"/"V" (kept in the result) and
// strips trailing punctuation (e.g. "0.44.1," → "0.44.1").
func versionToken(tok string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(tok, "v"), "V")
	if trimmed == "" {
		return ""
	}
	if first := trimmed[0]; first < '0' || first > '9' {
		return ""
	}
	// Trim trailing characters that cannot be part of a version.
	v := tok
	for len(v) > 0 {
		last := v[len(v)-1]
		isAlnum := (last >= '0' && last <= '9') || (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z')
		if isAlnum || last == '.' || last == '-' || last == '+' || last == '_' {
			break
		}
		v = v[:len(v)-1]
	}
	return v
}

// Ensure GoAdapter implements exec.Adapter and exec.Remover.
var _ exec.Adapter = (*GoAdapter)(nil)
var _ exec.Remover = (*GoAdapter)(nil)
