// Package native describes — purely declaratively — how the engine talks
// to a distro's package manager. Adding support for a new distro clan is
// a data edit in this file, never a new function.
//
// Two-tier keying (addresses a friction the README itself flags):
//
//   - A "clan" is what ResolveFamily returns and what schema clauses
//     `when = { distro_family = [...] }` compare against. It is a
//     distro-clan predicate: "debian", "arch", "fedora", "suse"...
//     Clans evolve slowly — they describe which roots distros descend
//     from.
//
//   - A "manager key" is what the manager map is indexed by. Today it
//     1:1-collides with the clan, but they are free to diverge: README
//     already notes that rhel/fedora are forced into one family and that
//     windows will need winget/choco/scoop. When that happens, the
//     manager map keys on a finer granule ("windows-winget") while the
//     clan stays "windows" for `when` matching.
//
// Lookup translates clan -> manager entry. Nothing else in the engine
// should read the manager map directly; that foraging pattern is how
// main.go got a second path to the same fact.
package native

import "sort"

// Manager describes one native package manager. All fields are data: no
// code lives here.
type Manager struct {
	Name string // display/log name: "apt", "pacman", "dnf"...

	SudoRequired bool // prefix sudo when the process is not root

	// NeedsSync marks managers that require synchronizing the package
	// index before the first install of a session (e.g. apt-get update).
	// The engine runs this once per execution, not per package.
	NeedsSync bool
	SyncCmd   []string

	// InstallCmd contains "{pkg}" as a placeholder for the package name.
	InstallCmd []string

	// CheckCmd uses "{pkg}" as well; exit code 0 means already installed.
	CheckCmd []string

	// RemoveCmd uses "{pkg}" as a placeholder for the package name.
	// Empty means the manager has no standard remove command and the
	// engine will fall back to manual-removal instructions.
	RemoveCmd []string
}

// managers maps a manager key (NOT directly a clan — see package doc) to
// its Manager entry. Use Lookup to go from clan to Manager; never index
// this map from outside the package.
var managers = map[string]Manager{
	"debian": {
		Name:         "apt",
		SudoRequired: true,
		NeedsSync:    true,
		SyncCmd:      []string{"apt-get", "update"},
		InstallCmd:   []string{"apt-get", "install", "-y", "{pkg}"},
		CheckCmd:     []string{"dpkg", "-s", "{pkg}"},
		RemoveCmd:    []string{"apt-get", "remove", "-y", "{pkg}"},
	},
	"arch": {
		Name:         "pacman",
		SudoRequired: true,
		InstallCmd:   []string{"pacman", "-S", "--noconfirm", "--needed", "{pkg}"},
		CheckCmd:     []string{"pacman", "-Qi", "{pkg}"},
		RemoveCmd:    []string{"pacman", "-R", "--noconfirm", "{pkg}"},
	},
	"fedora": {
		Name:         "dnf",
		SudoRequired: true,
		InstallCmd:   []string{"dnf", "install", "-y", "{pkg}"},
		CheckCmd:     []string{"rpm", "-q", "{pkg}"},
		RemoveCmd:    []string{"dnf", "remove", "-y", "{pkg}"},
	},
	"suse": {
		Name:         "zypper",
		SudoRequired: true,
		InstallCmd:   []string{"zypper", "--non-interactive", "install", "{pkg}"},
		CheckCmd:     []string{"rpm", "-q", "{pkg}"},
		RemoveCmd:    []string{"zypper", "--non-interactive", "remove", "{pkg}"},
	},
	"alpine": {
		Name:         "apk",
		SudoRequired: true,
		NeedsSync:    true,
		SyncCmd:      []string{"apk", "update"},
		InstallCmd:   []string{"apk", "add", "{pkg}"},
		CheckCmd:     []string{"apk", "info", "-e", "{pkg}"},
		RemoveCmd:    []string{"apk", "del", "{pkg}"},
	},
	// void uses xbps-install -Sy (sync + install in one command).
	// NeedsSync is false because -Sy handles the sync inline.
	"void": {
		Name:         "xbps",
		SudoRequired: true,
		InstallCmd:   []string{"xbps-install", "-Sy", "{pkg}"},
		CheckCmd:     []string{"xbps-query", "{pkg}"},
		RemoveCmd:    []string{"xbps-remove", "-y", "{pkg}"},
	},
	"gentoo": {
		Name:         "emerge",
		SudoRequired: true,
		InstallCmd:   []string{"emerge", "--quiet", "{pkg}"},
		CheckCmd:     []string{"equery", "list", "{pkg}"},
		RemoveCmd:    []string{"emerge", "--unmerge", "{pkg}"},
	},
	"macos": {
		Name:         "brew",
		SudoRequired: false,
		InstallCmd:   []string{"brew", "install", "{pkg}"},
		CheckCmd:     []string{"brew", "list", "{pkg}"},
		RemoveCmd:    []string{"brew", "uninstall", "{pkg}"},
	},
	"termux": {
		Name:         "pkg",
		SudoRequired: false,
		NeedsSync:    true,
		SyncCmd:      []string{"pkg", "update", "-y"},
		InstallCmd:   []string{"pkg", "install", "-y", "{pkg}"},
		CheckCmd:     []string{"dpkg", "-s", "{pkg}"},
		RemoveCmd:    []string{"pkg", "uninstall", "-y", "{pkg}"},
	},
	"freebsd": {
		Name:         "pkg",
		SudoRequired: true,
		InstallCmd:   []string{"pkg", "install", "-y", "{pkg}"},
		CheckCmd:     []string{"pkg", "info", "-e", "{pkg}"},
		RemoveCmd:    []string{"pkg", "delete", "-y", "{pkg}"},
	},
	"openbsd": {
		Name:         "pkg_add",
		SudoRequired: true,
		InstallCmd:   []string{"pkg_add", "{pkg}"},
		CheckCmd:     []string{"pkg_info", "-e", "{pkg}"},
		RemoveCmd:    []string{"pkg_delete", "{pkg}"},
	},
	"netbsd": {
		Name:         "pkgin",
		SudoRequired: true,
		InstallCmd:   []string{"pkgin", "-y", "install", "{pkg}"},
		CheckCmd:     []string{"pkg_info", "-e", "{pkg}"},
		RemoveCmd:    []string{"pkgin", "-y", "remove", "{pkg}"},
	},
	// Windows managers.
	// winget is built into Windows 10 1709+ and Windows 11.
	// choco/scoop are third-party; they use native adapter aliases or exec.Adapter-s.
	"windows-winget": {
		Name:         "winget",
		SudoRequired: false,
		InstallCmd:   []string{"winget", "install", "--id", "{pkg}"},
		CheckCmd:     []string{"winget", "list", "--id", "{pkg}"},
		RemoveCmd:    []string{"winget", "uninstall", "--id", "{pkg}"},
	},
	"mint": {
		Name:         "apt",
		SudoRequired: true,
		NeedsSync:    true,
		SyncCmd:      []string{"apt-get", "update"},
		InstallCmd:   []string{"apt-get", "install", "-y", "{pkg}"},
		CheckCmd:     []string{"dpkg", "-s", "{pkg}"},
		RemoveCmd:    []string{"apt-get", "remove", "-y", "{pkg}"},
	},

	// opkg — embedded Linux package manager (OpenWrt, LEDE, etc).
	"opkg": {
		Name:         "opkg",
		SudoRequired: true,
		InstallCmd:   []string{"opkg", "install", "{pkg}"},
		CheckCmd:     []string{"opkg", "status", "{pkg}"},
		RemoveCmd:    []string{"opkg", "remove", "{pkg}"},
	},
}

// managerNameToClan maps a manager binary name to its clan. This handles
// the case where the binary name differs from Manager.Name (e.g. gentoo's
// Manager.Name is "emerge" but the binary is "emerge"). Only entries that
// differ from Manager.Name need to be here; everything else falls through to
// the KnownClans iteration in findClanByManager.
//
// Entries here must be UNAMBIGUOUS: the binary name must belong to exactly
// one clan. Binary names shared across clans (e.g. "pkg" used by both
// termux and freebsd) are intentionally omitted — findClanByManager's
// fallback loop resolves them by iterating KnownClans, which is correct
// (though non-deterministic in ordering, both clans produce valid commands
// for the shared binary).
var managerNameToClan = map[string]string{
	// Disambiguate managers with the same binary name across clans.
	"apt":     "debian", // debian and mint both use "apt"; debian is the primary
	"emerge":  "gentoo", // Manager.Name changed from "portage" to "emerge"
	"portage": "gentoo", // backward compat for schema entries using "portage"
	"yum":     "fedora", // yum is a symlink to dnf on modern systems
	"dnf5":   "fedora", // dnf5 is the new default in Fedora 41+
}

// ManagerNameToClan returns the clan for a given manager binary name. This
// is the primary lookup for findClanByManager; it checks the explicit map
// first, falling back to KnownClans iteration.
func ManagerNameToClan(name string) (string, bool) {
	clan, ok := managerNameToClan[name]
	return clan, ok
}

// ManagerBinaryNames returns the set of all manager binary names registered
// in the reverse map (managerNameToClan). Used by RegisterNativeManagerAliases
// to ensure binary-name method kinds (e.g. "emerge", "yum") get adapter
// registrations even when they differ from Manager.Name.
func ManagerBinaryNames() []string {
	out := make([]string, 0, len(managerNameToClan))
	for name := range managerNameToClan {
		out = append(out, name)
	}
	return out
}

// clanToManagerKey maps a resolved clan to a manager key. Today identity;
// the indirection exists so a future windows/rhel-AUR split can re-key the
// manager map without touching any caller (ResolveFamily, when-clauses).
var clanToManagerKey = map[string]string{
	"debian":  "debian",
	"arch":    "arch",
	"fedora":  "fedora",
	"suse":    "suse",
	"alpine":  "alpine",
	"void":    "void",
	"gentoo":  "gentoo",
	"macos":   "macos",
	"termux":  "termux",
	"freebsd": "freebsd",
	"openbsd": "openbsd",
	"netbsd":  "netbsd",
	"windows": "windows-winget",
	"mint":    "mint",
	"opkg":    "opkg",
}

// Lookup returns the Manager for a resolved clan and whether the clan has
// a known native manager. Unknown clans return the zero Manager and
// false; the caller then falls through to the next entry in
// method_order (cargo/go/pip/...).
func Lookup(clan string) (Manager, bool) {
	key, ok := clanToManagerKey[clan]
	if !ok {
		return Manager{}, false
	}
	m, ok := managers[key]
	if !ok {
		return Manager{}, false
	}
	return m, true
}

// ManagerNames returns the set of unique manager binary names across all
// known clans (e.g., "apt", "pacman", "dnf", "brew"). Used by the executor
// to register manager-specific adapter aliases so that schema method kinds
// like `apt = "fd-find"` are resolved correctly.
func ManagerNames() []string {
	seen := map[string]bool{}
	for _, m := range managers {
		seen[m.Name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
}

// KnownClans returns every clan that maps to a functioning native manager (a
// clan in clanToManagerKey whose key also exists in the managers map).
// Used by schema validation to warn on unreachable `when.distro_family`
// values (TODO §3.2).
func KnownClans() []string {
	out := make([]string, 0, len(clanToManagerKey))
	for clan := range clanToManagerKey {
		if _, ok := Lookup(clan); ok {
			out = append(out, clan)
		}
	}
	return out
}

// AllClans returns every clan name — including clans that may not have a
// functioning native manager (like "windows" with winget, or purely historical
// entries) — so that when-clause validation can check against the complete set
// of possible distro_family values rather than only the subset that has a
// working adapter today.
func AllClans() []string {
	out := make([]string, 0, len(clanToManagerKey))
	for clan := range clanToManagerKey {
		out = append(out, clan)
	}
	sort.Strings(out)
	return out
}

// IsNativeManagerName reports whether name is a known native package-manager
// identifier — either a Manager.Name (apt, pacman, dnf, …) or an entry in
// the reverse alias map (emerge, portage, yum, …). Used by the schema parser
// to distinguish native-manager overrides from language/method kinds.
func IsNativeManagerName(name string) bool {
	// Check Manager.Name values.
	for _, m := range managers {
		if m.Name == name {
			return true
		}
	}
	// Check managerNameToClan binary-name aliases.
	if _, ok := managerNameToClan[name]; ok {
		return true
	}
	return false
}

// ManagerNamesForClan returns every binary name that can refer to the given
// clan's native manager. This is the Manager.Name (e.g. "apt" for debian,
// "pacman" for arch) plus any aliases from managerNameToClan (e.g. "portage"
// and "emerge" both mapping to gentoo). Used by exec adapters to match
// pkg_overrides keys against the current clan.
func ManagerNamesForClan(clan string) []string {
	var names []string
	if mgr, ok := Lookup(clan); ok {
		names = append(names, mgr.Name)
	}
	for binName, c := range managerNameToClan {
		if c == clan {
			names = append(names, binName)
		}
	}
	return names
}
