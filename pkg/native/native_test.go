package native

import (
	"sort"
	"strings"
	"testing"
)

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
		{"windows winget no sudo", "windows", true, "winget install --id git", "winget list --id git"},
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
