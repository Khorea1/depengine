// Package methodkind is the single source of truth for method kind names,
// bucket definitions, and canonical method ordering. It centralizes
// knowledge that was previously duplicated across pkg/config and pkg/ecosystem.
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

// knownKinds is the full set of valid method kind names. Keep this
// synchronized with the kinds registered in pkg/ecosystem.
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
	"dnf5",
	"xbps",
	"zypper",
	"emerge",
	"apk",
	"nix",
	"pacman",
	"apt",
	"dnf",
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
