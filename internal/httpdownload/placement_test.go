// Package httpdownload implements the HTTP download method family.
//
// placement_test.go pins the ADR-004 placement precedence contract for
// artifact installs: declared portable scope supplies platform-native
// install/link defaults (no Unix paths in authoring), explicit
// extract_to/install_dir/link_dir override independently, and scope-less
// candidates keep the historical defaults byte-for-byte.
package httpdownload

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
)

func httpScopeCandidate(extra map[string]any) *config.MethodCandidate {
	return &config.MethodCandidate{Kind: "http", Config: extra}
}

// TestScopeUserPlacementNeedsNoUnixPaths is the ADR-004 acceptance: a
// user-scoped raw binary resolves to the XDG data home install root and the
// conventional ~/.local/bin link dir without authoring either path.
func TestScopeUserPlacementNeedsNoUnixPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	home := t.TempDir()
	exectest.SetHome(t, home)

	placement, err := ArtifactPlacement(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{"scope": "user"}), "/usr/local/bin", "")
	if err != nil {
		t.Fatalf("ArtifactPlacement() error = %v", err)
	}
	wantInstall := filepath.Join(home, ".local", "share", "depengine", "tools", "ripgrep")
	wantLink := filepath.Join(home, ".local", "bin")
	if placement.InstallRoot != wantInstall {
		t.Fatalf("InstallRoot = %q, want %q", placement.InstallRoot, wantInstall)
	}
	if placement.LinkDir != wantLink {
		t.Fatalf("LinkDir = %q, want %q", placement.LinkDir, wantLink)
	}
}

// TestScopeSystemPlacementUsesPlatformSystemDirs resolves the system scope
// to the conventional Unix system roots.
func TestScopeSystemPlacementUsesPlatformSystemDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	placement, err := ArtifactPlacement(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{"scope": "system"}), "/usr/local/bin", "")
	if err != nil {
		t.Fatalf("ArtifactPlacement() error = %v", err)
	}
	if placement.InstallRoot != filepath.Join("/opt", "depengine", "tools", "ripgrep") {
		t.Fatalf("InstallRoot = %q, want /opt/depengine/tools/ripgrep", placement.InstallRoot)
	}
	if placement.LinkDir != "/usr/local/bin" {
		t.Fatalf("LinkDir = %q, want /usr/local/bin", placement.LinkDir)
	}
}

// TestExplicitPathsOverrideScope pins advanced path precedence: even with a
// scope declared, explicit extract_to/link_dir win independently.
func TestExplicitPathsOverrideScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	home := t.TempDir()
	exectest.SetHome(t, home)

	placement, err := ArtifactPlacement(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{
		"scope":     "user",
		"extract_to": "/srv/custom/tool",
		"link_dir":   "/srv/custom/bin",
	}), "/usr/local/bin", "")
	if err != nil {
		t.Fatalf("ArtifactPlacement() error = %v", err)
	}
	if placement.InstallRoot != "/srv/custom/tool" {
		t.Fatalf("InstallRoot = %q, want explicit /srv/custom/tool", placement.InstallRoot)
	}
	if placement.LinkDir != "/srv/custom/bin" {
		t.Fatalf("LinkDir = %q, want explicit /srv/custom/bin", placement.LinkDir)
	}
}

// TestRelativeOverrideFailsClosedUnderScope rejects non-absolute advanced
// paths when scope is declared: goal-path ambiguity must surface at planning,
// not silently change the target.
func TestRelativeOverrideFailsClosedUnderScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	home := t.TempDir()
	exectest.SetHome(t, home)

	if _, err := ArtifactPlacement(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{
		"scope":     "user",
		"extract_to": "relative/tool",
	}), "/usr/local/bin", ""); err == nil {
		t.Fatal("relative extract_to under scope must be rejected")
	}
}

// TestArtifactPlacementLegacyDefaultsKeepHistoricalPaths proves scope-less
// candidates resolve to the exact historical defaults, so existing manifests
// are untouched by the ADR-004 wiring.
func TestArtifactPlacementLegacyDefaultsKeepHistoricalPaths(t *testing.T) {
	// http raw default: /usr/local/bin, no link dir.
	placement, err := ArtifactPlacement(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{}), "/usr/local/bin", "")
	if err != nil {
		t.Fatal(err)
	}
	if placement.InstallRoot != "/usr/local/bin" || placement.LinkDir != "" {
		t.Fatalf("legacy placement = %+v, want /usr/local/bin and no link dir", placement)
	}

	// Explicit extract_to still wins.
	explicit := t.TempDir()
	placement, err = ArtifactPlacement(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{"extract_to": explicit}), "/usr/local/bin", "")
	if err != nil {
		t.Fatal(err)
	}
	if placement.InstallRoot != explicit {
		t.Fatalf("InstallRoot = %q, want explicit %q", placement.InstallRoot, explicit)
	}
}

// TestInvalidScopeValueIsRejected fails closed on a non-portable scope
// spelling at placement time.
func TestInvalidScopeValueIsRejected(t *testing.T) {
	if _, err := ArtifactPlacement(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{"scope": "galaxy"}), "/usr/local/bin", ""); err == nil {
		t.Fatal("non-portable scope value must be rejected")
	}
}

// TestRawBinaryAndArchiveResolveToSameScopePlacement is the ADR-004
// consistency acceptance: a user-scoped raw binary and a user-scoped archive
// derive the same install/link pair from the same scope, without any
// ~/.local spelling in the candidate.
func TestRawBinaryAndArchiveResolveToSameScopePlacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	home := t.TempDir()
	exectest.SetHome(t, home)
	tool := &config.Tool{Name: "nvim"}

	rawMC := httpScopeCandidate(map[string]any{"scope": "user"})
	archiveMC := httpScopeCandidate(map[string]any{
		"scope":      "user",
		"entrypoints": map[string]any{"nvim": "bin/nvim"},
	})

	rawPlacement, err := ArtifactPlacement(tool, rawMC, "/usr/local/bin", "")
	if err != nil {
		t.Fatal(err)
	}
	archiveInstall := archiveTarget(tool, archiveMC)
	archiveLink := linkTargetDir(archiveMC, archiveInstall, tool)

	if archiveInstall != rawPlacement.InstallRoot {
		t.Fatalf("archive install root = %q, raw install root = %q; want equal", archiveInstall, rawPlacement.InstallRoot)
	}
	if archiveLink != rawPlacement.LinkDir {
		t.Fatalf("archive link dir = %q, raw link dir = %q; want equal", archiveLink, rawPlacement.LinkDir)
	}
	if filepath.Clean(rawPlacement.InstallRoot) == filepath.Join(home, ".local", "opt", "nvim") {
		t.Fatal("scope install must not fall back to the legacy ~/.local/opt default")
	}
}

// TestPlacementOrDefaultFallsBackWhenRootsMissing documents the fail-safe of
// bool-context probes: without a resolvable home the legacy root is returned
// instead of fabricating a target.
func TestPlacementOrDefaultFallsBackWhenRootsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	placement := PlacementOrDefault(&config.Tool{Name: "ripgrep"}, httpScopeCandidate(map[string]any{"scope": "user"}), "/legacy/bin", "")
	if placement.InstallRoot != "/legacy/bin" {
		t.Fatalf("InstallRoot = %q, want legacy fallback", placement.InstallRoot)
	}
}