package engine

import (
	"strings"

	"depengine/pkg/log"
)

// idToFamily covers the common case: the distro_id is recognized directly.
// Keys are lowercase (Facts.DistroID already comes lowercase from the
// fetcher).
var idToFamily = map[string]string{
	// família debian (apt)
	"ubuntu": "debian", "debian": "debian",
	"pop": "debian", "elementary": "debian", "raspbian": "debian",
	"kali": "debian", "zorin": "debian", "neon": "debian",

	// família arch (pacman)
	"arch": "arch", "manjaro": "arch", "endeavouros": "arch",
	"garuda": "arch", "artix": "arch", "cachyos": "arch",

	// família fedora/rhel (dnf) — treated as one clan today; both use dnf.
	// If we ever need a distinct rhel/yum path, it splits here while the
	// macos/termux/... entries stay put (and `when.distro_family`
	// compares against clans, not manager keys).
	"fedora": "fedora", "rhel": "fedora", "centos": "fedora",
	"rocky": "fedora", "almalinux": "fedora",

	// família mint (apt) — Linux Mint uses apt but has mint-specific metadata.
	"linuxmint": "mint",

	// família suse (zypper)
	"opensuse-leap": "suse", "opensuse-tumbleweed": "suse", "sles": "suse",

	"alpine":  "alpine",               // apk
	"void":    "void",                 // xbps
	"gentoo":  "gentoo",               // portage/emerge
	"openwrt": "opkg", "lede": "opkg", // opkg (embedded Linux)

	"macos":  "macos",  // brew
	"termux": "termux", // pkg (Termux's apt wrapper)

	"freebsd":   "freebsd", // pkg
	"openbsd":   "openbsd", // pkg_add
	"netbsd":    "netbsd",  // pkgin
	"dragonfly": "freebsd", // uses FreeBSD's pkg
	"windows":   "windows", // stub clan — no native manager yet; Lookup returns false
}

// likeTokenPriority is the fallback: when the distro_id is an unknown
// derivative (a new or niche distro), we try matching against ID_LIKE,
// which in os-release usually lists "who this distro descends from"
// (e.g. ID_LIKE="ubuntu debian" or ID_LIKE="rhel fedora"). Order matters:
// we check the more specific tokens first.
var likeTokenPriority = []struct {
	Token  string
	Family string
}{
	{"arch", "arch"},
	{"debian", "debian"},
	{"ubuntu", "debian"},
	{"fedora", "fedora"},
	{"rhel", "fedora"},
	{"suse", "suse"},
	{"alpine", "alpine"},
	{"opkg", "opkg"},
}

// ResolveFamily translates raw Facts (distro_id / distro_id_like) into the
// "clan" used both to select the native manager (via native.Lookup) and
// to evaluate `when = { distro_family = [...] }` in schema.toml.
//
// ResolveFamily is pure: same Facts in, same name out, no mutation. Callers
// that need the clan keep it as a local — see GatherFacts's caller in
// main.go. Returns "unknown" if nothing matches; the engine then skips to
// the next entry in method_order (cargo/go/pip/...) rather than blocking.
func ResolveFamily(f *Facts) string {
	if f == nil {
		log.Default.Debug("resolve family: nil facts")
		return "unknown"
	}
	id := strings.ToLower(strings.TrimSpace(f.DistroID))

	if fam, ok := idToFamily[id]; ok {
		log.Default.Debug("resolve family: direct match", "distro_id", id, "clan", fam)
		return fam
	}

	likeTokens := strings.Fields(strings.ToLower(f.DistroIDLike))
	for _, rule := range likeTokenPriority {
		for _, t := range likeTokens {
			if t == rule.Token {
				log.Default.Debug("resolve family: id_like match", "distro_id", id, "id_like", f.DistroIDLike, "clan", rule.Family)
				return rule.Family
			}
		}
	}

	if f.IsAndroid {
		log.Default.Debug("resolve family: android", "distro_id", id, "clan", "android")
		return "android"
	}

	log.Default.Debug("resolve family: unknown", "distro_id", id, "clan", "unknown")
	return "unknown"
}

// MatchesDistroFamily implements the semantics of
// `when = { distro_family = ["arch", "debian"] }`: true if the resolved
// clan of the current system is in the list.
//
// Takes the clan explicitly (no cross-file hidden state): the caller has
// already run ResolveFamily once and keeps reusing the same value across
// every `when` clause it evaluates this run. This is the C2 seam — what
// `when` compares against stays a clan even when the manager side grows
// finer keys.
func MatchesDistroFamily(clan string, allowed []string) bool {
	for _, fam := range allowed {
		if strings.EqualFold(fam, clan) {
			return true
		}
	}
	return false
}
