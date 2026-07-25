// Package methodkind defines canonical method kind names, bucket definitions,
// and default method ordering. The ecosystem kinds are statically enumerated
// here; native manager binary names and aliases (apt, pacman, dnf, winget,
// opkg, pkg_add, pkgin, pkg, portage, yum, …) are also listed so that
// config.Validate can cross-check them. At runtime, the authoritative set of
// available adapters is exec.RegisteredKinds() — this list is a compile-time
// sanity boundary, not a dynamic registry.
package methodkind

// DefaultMethodOrder is the engine-wide canonical preference order for
// install methods. Used as default and as canonical remainder when the
// user specifies a partial method_order.
var DefaultMethodOrder = []string{
	"native", "scoop", "choco", "cargo", "go", "pipx", "uv", "pip",
	"npm", "pnpm", "bun", "gem", "yarn", "yarn-berry",
	"composer", "apm", "vscode", "vscodium", "flatpak",
	"snap", "cask", "mas", "sdkman", "steamcmd",
	"pacstall", "aur", "conda", "asdf", "git", "http",
}

// DefaultBuckets maps ecosystem names to lists of method kinds.
// Buckets avoid repeating the same set of methods for every tool in the
// same ecosystem.
var DefaultBuckets = map[string][]string{
	"python": {"pip", "pipx", "uv"},
	"node":   {"npm", "pnpm", "bun"},
}

// knownKinds is the full set of valid method kind names: ecosystem kinds
// (cargo, go, pip, …) plus native manager names and aliases (apt, pacman,
// dnf, winget, opkg, pkg_add, pkgin, pkg, portage, yum, …). Keep this
// synchronized with ecosystem.Configs keys in pkg/ecosystem AND with
// native manager names in pkg/native (managers map Manager.Name values
// and managerNameToClan alias keys).
var knownKinds = []string{
	"native",
	"cargo",
	"go",
	"pip",
	"pipx",
	"uv",
	"npm",
	"pnpm",
	"bun",
	"gem",
	"yarn",
	"yarn-berry",
	"composer",
	"apm",
	"vscode",
	"vscodium",
	"flatpak",
	"snap",
	"cask",
	"mas",
	"sdkman",
	"steamcmd",
	"pacstall",
	"aur",
	"conda",
	"asdf",
	"git",
	"http",
	"brew",
	"scoop",
	"choco",
	"dnf",
	"dnf5",
	"emerge",
	"apk",
	"nix",
	"opkg",
	"pacman",
	"pkg",
	"pkg_add",
	"pkgin",
	"portage",
	"xbps",
	"yum",
	"winget",
	"zypper",
	"apt",
	"paru",
	"yay",
}

// knownKindSet is a lookup set built from knownKinds.
var knownKindSet map[string]bool

func init() {
	knownKindSet = make(map[string]bool, len(knownKinds))
	for _, k := range knownKinds {
		knownKindSet[k] = true
	}
}

// KnownKinds returns the complete list of valid method kind names.
func KnownKinds() []string {
	out := make([]string, len(knownKinds))
	copy(out, knownKinds)
	return out
}

// IsKnownKind reports whether k is a valid method kind name.
func IsKnownKind(k string) bool {
	return knownKindSet[k]
}

// ExpandBuckets replaces bucket names in a method order list with their
// constituent concrete method kinds. Unknown names pass through unchanged.
func ExpandBuckets(order []string) []string {
	var expanded []string
	for _, k := range order {
		if methods, ok := DefaultBuckets[k]; ok {
			for _, m := range methods {
				if !contains(expanded, m) {
					expanded = append(expanded, m)
				}
			}
		} else {
			expanded = append(expanded, k)
		}
	}
	return expanded
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
