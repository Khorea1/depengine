package plan

import "testing"

func TestResolveUnixXDGPathsDefaults(t *testing.T) {
	got, err := ResolveUnixXDGPaths(ScopeRoots{HomeDir: "/home/alice"})
	if err != nil {
		t.Fatal(err)
	}
	want := UnixXDGPaths{
		DataHome:   "/home/alice/.local/share",
		StateHome:  "/home/alice/.local/state",
		CacheHome:  "/home/alice/.cache",
		ConfigHome: "/home/alice/.config",
		UserBin:    "/home/alice/.local/bin",
	}
	if got != want {
		t.Fatalf("ResolveUnixXDGPaths() = %+v, want %+v", got, want)
	}
}

func TestResolveUnixXDGPathsHonorsAbsoluteOverrides(t *testing.T) {
	got, err := ResolveUnixXDGPaths(ScopeRoots{
		HomeDir:       "/home/alice",
		XDGDataHome:   "/srv/alice/data",
		XDGStateHome:  "/srv/alice/state",
		XDGCacheHome:  "/tmp/alice-cache",
		XDGConfigHome: "/srv/alice/config",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DataHome != "/srv/alice/data" || got.StateHome != "/srv/alice/state" || got.CacheHome != "/tmp/alice-cache" || got.ConfigHome != "/srv/alice/config" {
		t.Fatalf("XDG overrides not honored: %+v", got)
	}
	if got.UserBin != "/home/alice/.local/bin" {
		t.Fatalf("UserBin = %q, want conventional ~/.local/bin", got.UserBin)
	}
}

func TestResolveUnixXDGPathsIgnoresRelativeOverrides(t *testing.T) {
	got, err := ResolveUnixXDGPaths(ScopeRoots{
		HomeDir:       "/home/alice",
		XDGDataHome:   "relative/data",
		XDGStateHome:  "relative/state",
		XDGCacheHome:  "relative/cache",
		XDGConfigHome: "relative/config",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DataHome != "/home/alice/.local/share" || got.StateHome != "/home/alice/.local/state" || got.CacheHome != "/home/alice/.cache" || got.ConfigHome != "/home/alice/.config" {
		t.Fatalf("relative XDG values should fall back to defaults: %+v", got)
	}
}

func TestResolveUnixXDGPathsRequiresAbsoluteHome(t *testing.T) {
	for _, home := range []string{"", "home/alice"} {
		if _, err := ResolveUnixXDGPaths(ScopeRoots{HomeDir: home}); err == nil {
			t.Fatalf("HomeDir %q unexpectedly accepted", home)
		}
	}
}

func TestResolveScopePlacementUnixUsesXDG(t *testing.T) {
	got, err := ResolveScopePlacement(ScopeUser, "linux", "ripgrep", ScopeRoots{
		HomeDir:      "/home/alice",
		XDGDataHome:  "/mnt/data/alice",
		XDGStateHome: "/mnt/state/alice",
		XDGCacheHome: "/mnt/cache/alice",
	}, PlacementOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if got.InstallRoot != "/mnt/data/alice/depengine/tools/ripgrep" {
		t.Fatalf("InstallRoot = %q", got.InstallRoot)
	}
	if got.LinkDir != "/home/alice/.local/bin" {
		t.Fatalf("LinkDir = %q", got.LinkDir)
	}
	if got.StateRoot != "/mnt/state/alice/depengine" || got.CacheRoot != "/mnt/cache/alice/depengine" || got.ConfigRoot != "/home/alice/.config/depengine" {
		t.Fatalf("XDG placement = %+v", got)
	}

	system, err := ResolveScopePlacement(ScopeSystem, "linux", "ripgrep", ScopeRoots{}, PlacementOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if system.InstallRoot != "/opt/depengine/tools/ripgrep" || system.LinkDir != "/usr/local/bin" {
		t.Fatalf("system placement = %+v", system)
	}
	if system.StateRoot != "/var/lib/depengine" || system.CacheRoot != "/var/cache/depengine" || system.ConfigRoot != "/etc/depengine" {
		t.Fatalf("system depengine roots = %+v", system)
	}
}

func TestResolveScopePlacementWindowsDoesNotUseUnixPaths(t *testing.T) {
	user, err := ResolveScopePlacement(ScopeUser, "windows", "ripgrep", ScopeRoots{LocalAppData: `C:\Users\Alice\AppData\Local`}, PlacementOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if user.InstallRoot != `C:\Users\Alice\AppData\Local\depengine\tools\ripgrep` {
		t.Fatalf("user InstallRoot = %q", user.InstallRoot)
	}
	if user.LinkDir != `C:\Users\Alice\AppData\Local\depengine\bin` {
		t.Fatalf("user LinkDir = %q", user.LinkDir)
	}

	system, err := ResolveScopePlacement(ScopeSystem, "windows", "ripgrep", ScopeRoots{ProgramFiles: `C:\Program Files`}, PlacementOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if system.InstallRoot != `C:\Program Files\depengine\tools\ripgrep` || system.LinkDir != `C:\Program Files\depengine\bin` {
		t.Fatalf("system placement = %+v", system)
	}
}

func TestResolveScopePlacementAdvancedOverridesWin(t *testing.T) {
	got, err := ResolveScopePlacement(ScopeUser, "linux", "tool", ScopeRoots{HomeDir: "/home/alice"}, PlacementOverrides{
		InstallRoot: "/srv/custom/tool",
		LinkDir:     "/srv/custom/bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.InstallRoot != "/srv/custom/tool" || got.LinkDir != "/srv/custom/bin" {
		t.Fatalf("overrides not preserved: %+v", got)
	}
	if got.StateRoot != "/home/alice/.local/state/depengine" || got.CacheRoot != "/home/alice/.cache/depengine" || got.ConfigRoot != "/home/alice/.config/depengine" {
		t.Fatalf("advanced install/link overrides should not replace depengine XDG roots: %+v", got)
	}
}

func TestResolveScopePlacementRequiresTargetRootsAndSafeTool(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		goos  string
		tool  string
		roots ScopeRoots
	}{
		{name: "linux user root", scope: ScopeUser, goos: "linux", tool: "x"},
		{name: "linux relative user root", scope: ScopeUser, goos: "linux", tool: "x", roots: ScopeRoots{HomeDir: "home/a"}},
		{name: "windows user root", scope: ScopeUser, goos: "windows", tool: "x"},
		{name: "windows system root", scope: ScopeSystem, goos: "windows", tool: "x"},
		{name: "unsafe tool", scope: ScopeUser, goos: "linux", tool: "../x", roots: ScopeRoots{HomeDir: "/home/a"}},
		{name: "unknown os", scope: ScopeUser, goos: "plan9", tool: "x", roots: ScopeRoots{HomeDir: "/home/a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ResolveScopePlacement(tt.scope, tt.goos, tt.tool, tt.roots, PlacementOverrides{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
