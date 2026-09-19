package native

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
)

func init() {
	// Force "sudo" as elevation method for deterministic test results.
	run.OverrideElevation("sudo")
}

func TestBuildCommandsMatchExpectedPerClan(t *testing.T) {
	cases := []struct {
		name        string
		clan        string
		wantKnown   bool
		wantInstall string
		wantCheck   string
	}{
		{"debian apt-get + sudo", "debian", true, "sudo apt-get install -y git", "dpkg -s git"},
		{"arch pacman + sudo", "arch", true, "sudo pacman -S --noconfirm --needed git", "pacman -Qi git"},
		{"fedora dnf + sudo", "fedora", true, "sudo dnf install -y git", "rpm -q git"},
		{"suse zypper + sudo", "suse", true, "sudo zypper --non-interactive install git", "rpm -q git"},
		{"alpine apk + sudo", "alpine", true, "sudo apk add git", "apk info -e git"},
		{"macos brew no sudo", "macos", true, "brew install git", "brew list git"},
		{"termux pkg no sudo", "termux", true, "pkg install -y git", "dpkg -s git"},
		{"freebsd pkg + sudo", "freebsd", true, "sudo pkg install -y git", "pkg info -e git"},
		{"openbsd pkg_add + sudo", "openbsd", true, "sudo pkg_add git", "pkg_info -e git"},
		{"netbsd pkgin + sudo", "netbsd", true, "sudo pkgin -y install git", "pkg_info -e git"},
		{"gentoo emerge + sudo", "gentoo", true, "sudo emerge --quiet git", "equery list git"},
		{"mint apt + sudo", "mint", true, "sudo apt-get install -y git", "dpkg -s git"},
		{"opkg + sudo", "opkg", true, "sudo opkg install git", "opkg status git"},
		{
			"windows winget no sudo", "windows", true,
			"winget install --id git --exact --silent --accept-package-agreements --accept-source-agreements --disable-interactivity",
			"winget list --id git --exact --accept-source-agreements --disable-interactivity",
		},
		{"unknown clan returns nil", "unknown", false, "", ""},
		{"empty clan returns nil", "", false, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			install := BuildInstallCmd(tc.clan, "git")
			check := BuildCheckCmd(tc.clan, "git")
			_, known := Lookup(tc.clan)

			if known != tc.wantKnown {
				t.Fatalf("Lookup known = %v, want %v", known, tc.wantKnown)
			}

			if known {
				if join(install) != tc.wantInstall {
					t.Fatalf("install = %q, want %q", join(install), tc.wantInstall)
				}
				if join(check) != tc.wantCheck {
					t.Fatalf("check = %q, want %q", join(check), tc.wantCheck)
				}
			} else {
				// When the clan is unknown, the engine must fall through to
				// the next method in method_order — that's a nil command,
				// never a wrong command.
				if install != nil {
					t.Fatalf("unknown clan install should be nil, got %v", install)
				}
				if check != nil {
					t.Fatalf("unknown clan check should be nil, got %v", check)
				}
			}
		})
	}
}

// TestBuildSearchCmd covers the availability-check fix: clans where a
// clean exit-code-only repo query is known (void, debian/mint/termux via
// apt-cache, arch, fedora, macos) must return a command, and clans with
// no configured SearchCmd must return nil so callers fail open instead of
// guessing.
func TestBuildSearchCmd(t *testing.T) {
	cases := []struct {
		name string
		clan string
		want string // space-joined expected command, "" means nil
	}{
		{"void xbps-query -R", "void", "xbps-query -R serpantinumd"},
		{"debian apt-cache show", "debian", "apt-cache show serpantinumd"},
		{"mint apt-cache show", "mint", "apt-cache show serpantinumd"},
		{"termux apt-cache show", "termux", "apt-cache show serpantinumd"},
		{"arch pacman -Si", "arch", "pacman -Si serpantinumd"},
		{"fedora dnf list", "fedora", "dnf list serpantinumd"},
		{"macos brew info", "macos", "brew info serpantinumd"},
		{
			"windows winget show --id --exact", "windows",
			"winget show --id serpantinumd --exact --accept-source-agreements --disable-interactivity",
		},
		{"suse has no SearchCmd configured", "suse", ""},
		{"alpine has no SearchCmd configured", "alpine", ""},
		{"unknown clan returns nil", "unknown", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BuildSearchCmd(c.clan, "serpantinumd")
			if c.want == "" {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %q, got nil", c.want)
			}
			if strings.Join(got, " ") != c.want {
				t.Fatalf("expected %q, got %q", c.want, strings.Join(got, " "))
			}
			// Search must never carry sudo — it's a read-only repo query.
			if got[0] == "sudo" {
				t.Fatalf("SearchCmd must not be sudo-prefixed: %v", got)
			}
		})
	}
}

// TestWinGetCommandsAreExactIDBasedAndNonInteractive fixes the full argv
// for every winget command depengine builds (install/check/search/remove).
// winget's default query matching is a case-insensitive substring against
// name/ID/moniker, so a bare "{pkg}" risks resolving to the wrong package;
// every command below must pin an exact --id match and run fully
// non-interactively (no license/source prompt can block with no TTY to
// answer it).
func TestWinGetCommandsAreExactIDBasedAndNonInteractive(t *testing.T) {
	const pkg = "Some.Package"

	cases := []struct {
		name string
		got  []string
		want string
	}{
		{
			"install", BuildInstallCmd("windows", pkg),
			"winget install --id Some.Package --exact --silent --accept-package-agreements --accept-source-agreements --disable-interactivity",
		},
		{
			"check", BuildCheckCmd("windows", pkg),
			"winget list --id Some.Package --exact --accept-source-agreements --disable-interactivity",
		},
		{
			"search", BuildSearchCmd("windows", pkg),
			"winget show --id Some.Package --exact --accept-source-agreements --disable-interactivity",
		},
		{
			"remove", BuildRemoveCmd("windows", pkg),
			"winget uninstall --id Some.Package --exact --silent --disable-interactivity",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if join(c.got) != c.want {
				t.Fatalf("%s = %q, want %q", c.name, join(c.got), c.want)
			}
			if !contains(c.got, "--exact") {
				t.Fatalf("%s must pin --exact, got %v", c.name, c.got)
			}
			if !contains(c.got, "--disable-interactivity") {
				t.Fatalf("%s must run non-interactively, got %v", c.name, c.got)
			}
			// winget has no root/sudo concept the way *nix managers do;
			// elevation (UAC) is each installer's own decision.
			if c.got[0] == "sudo" {
				t.Fatalf("%s must not be sudo-prefixed, got %v", c.name, c.got)
			}
		})
	}
}

func contains(argv []string, arg string) bool {
	for _, a := range argv {
		if a == arg {
			return true
		}
	}
	return false
}

func TestBuildSyncCmdOnlyForManagersThatNeedIt(t *testing.T) {
	synced := map[string]bool{"debian": true, "alpine": true, "termux": true, "mint": true}
	for clan := range synced {
		t.Run(clan+" has sync", func(t *testing.T) {
			if got := BuildSyncCmd(clan); got == nil {
				t.Fatalf("expected sync cmd for %s, got nil", clan)
			}
		})
	}
	noSync := []string{"arch", "fedora", "suse", "macos", "void", "gentoo",
		"freebsd", "openbsd", "netbsd", "windows", "opkg"}
	for _, clan := range noSync {
		t.Run(clan+" no sync", func(t *testing.T) {
			if got := BuildSyncCmd(clan); got != nil {
				t.Fatalf("expected nil sync for %s, got %v", clan, got)
			}
		})
	}
	t.Run("unknown clan no sync", func(t *testing.T) {
		if got := BuildSyncCmd("nonsense"); got != nil {
			t.Fatalf("unknown clan should have nil sync, got %v", got)
		}
	})
}

// CheckCmd must never be sudo-prefixed: probing whether something is
// installed never needs privilege, and forcing it would leak elevation
// budget silently. Invariant preserved across all managers.
func TestCheckCmdIsNeverSudoPrefixed(t *testing.T) {
	for clan := range allKnownClans(t) {
		got := BuildCheckCmd(clan, "anything")
		if got == nil {
			t.Fatalf("Lookup returned nil check for known clan %s", clan)
		}
		if got[0] == "sudo" {
			t.Fatalf("CheckCmd for %s is sudo-prefixed: %v", clan, got)
		}
	}
}

// Brew and pkg (termux) explicitly must not get sudo even though many
// peers do — brew refuses root, termux has no root concept.
func TestBrewAndTermuxHaveNoSudoOnInstall(t *testing.T) {
	for clan := range map[string]struct{}{"macos": {}, "termux": {}} {
		got := BuildInstallCmd(clan, "x")
		if got[0] == "sudo" {
			t.Fatalf("%s install should not have sudo, got %v", clan, got)
		}
	}
}

func TestKnownClansListsAllManaged(t *testing.T) {
	clans := KnownClans()
	// Every returned clan must have a functioning native manager.
	for _, clan := range clans {
		if _, ok := Lookup(clan); !ok {
			t.Fatalf("KnownClans includes %q but Lookup returns false", clan)
		}
	}
	// All actually-managed clans must be represented.
	for clan := range clanToManagerKey {
		if _, ok := Lookup(clan); !ok {
			continue // pre-declared placeholder like "windows"
		}
		found := false
		for _, c := range clans {
			if c == clan {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("KnownClans missing managed clan %q", clan)
		}
	}
	// Verify we don't return duplicates.
	sort.Strings(clans)
	for i := 1; i < len(clans); i++ {
		if clans[i] == clans[i-1] {
			t.Fatalf("KnownClans duplicate entry: %q", clans[i])
		}
	}
}

// allKnownClans returns the same set KnownClans() exposes; derived from
// clanToManagerKey directly here so that a regression that adds a clan
// without wiring Lookup is caught.
func allKnownClans(t *testing.T) map[string]struct{} {
	t.Helper()
	out := make(map[string]struct{}, len(clanToManagerKey))
	for clan := range clanToManagerKey {
		out[clan] = struct{}{}
	}
	return out
}

func join(args []string) string {
	if args == nil {
		return ""
	}
	return strings.Join(args, " ")
}

func TestBuildBatchInstallCmd_EmptyPkgs(t *testing.T) {
	// Should return nil, not panic, when pkgs is empty.
	result := BuildBatchInstallCmd("arch", []string{})
	if result != nil {
		t.Errorf("BuildBatchInstallCmd(arch, []{}) = %v, want nil", result)
	}
}

func TestBuildBatchInstallCmd_NonAtomicClans(t *testing.T) {
	nonAtomic := []string{"macos", "gentoo"}
	for _, clan := range nonAtomic {
		result := BuildBatchInstallCmd(clan, []string{"pkg1"})
		if result != nil {
			t.Errorf("BuildBatchInstallCmd(%q, [pkg1]) = %v, want nil (non-atomic)", clan, result)
		}
	}
}

func TestIsBatchCapable_NonAtomic(t *testing.T) {
	if IsBatchCapable("macos") {
		t.Error("IsBatchCapable(macos) = true, want false (brew is not atomic)")
	}
	if IsBatchCapable("gentoo") {
		t.Error("IsBatchCapable(gentoo) = true, want false (emerge is not atomic)")
	}
	// Sanity check: batch-capable clans still report correctly.
	if !IsBatchCapable("debian") {
		t.Error("IsBatchCapable(debian) = false, want true (apt is atomic)")
	}
	if !IsBatchCapable("arch") {
		t.Error("IsBatchCapable(arch) = false, want true (pacman is atomic)")
	}
}

func TestManagerNamesForClanDeterministicPrimaryThenAliases(t *testing.T) {
	want := []string{"dnf", "dnf5", "yum"}
	for i := 0; i < 100; i++ {
		got := ManagerNamesForClan("fedora")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ManagerNamesForClan(fedora) = %v, want %v", got, want)
		}
	}
}
