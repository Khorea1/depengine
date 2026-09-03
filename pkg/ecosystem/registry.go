package ecosystem

import (
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/methodkind"
)

// Configs holds the definitions for all supported language adapters.
// Each entry maps to a method_order name and describes how to check
// for and install packages via that tool.
//
// Removal policy: entries with a RemoveTmpl support automated removal
// (BaseAdapter.CanRemove == true). Entries without one intentionally stay
// "manual remove required" — see the per-entry comments for why.
var Configs = map[string]BaseConfig{
	"cargo": {
		KindName:    "cargo",
		Binary:      "cargo",
		CheckTmpl:   []string{"sh", "-c", `cargo install --list | grep -qE "^$1 v"`, "sh", "{pkg}"},
		InstallTmpl: []string{"cargo", "install", "{pkg}"},
		RemoveTmpl:  []string{"cargo", "uninstall", "{pkg}"},
	},
	"go": {
		KindName: "go",
		Binary:   "go",
		// {bin} is the binary name derived from the import path (last path
		// element, or the element after /cmd/): `go install` never puts the
		// import path itself on PATH, so `command -v {pkg}` could never pass.
		// `command -v` is a POSIX builtin — no external `which` binary needed.
		CheckTmpl:   []string{"sh", "-c", `command -v "$1" >/dev/null`, "sh", "{bin}"},
		InstallTmpl: []string{"go", "install", "{pkg}@latest"},
		// No RemoveTmpl: `go clean` does not uninstall (it only clears the
		// build cache), so removal is handled by GoAdapter.Remove, which
		// deletes the installed binary from the GOBIN directory.
	},
	"pip": {
		KindName:       "pip",
		Binary:         "pip",
		CheckTmpl:      []string{"pip", "show", "{pkg}"},
		InstallTmpl:    []string{"pip", "install", "{pkg}"},
		RemoveTmpl:     []string{"pip", "uninstall", "-y", "{pkg}"},
		AvailableExtra: "pip3",
	},
	"pipx": {
		KindName:    "pipx",
		Binary:      "pipx",
		CheckTmpl:   []string{"sh", "-c", `pipx list --short 2>/dev/null | awk -v n="$1" '$1 == n { found = 1 } END { exit !found }'`, "sh", "{pkg}"},
		InstallTmpl: []string{"pipx", "install", "{pkg}"},
		RemoveTmpl:  []string{"pipx", "uninstall", "{pkg}"},
	},
	"uv": {
		KindName:    "uv",
		Binary:      "uv",
		CheckTmpl:   []string{"sh", "-c", `uv tool list 2>/dev/null | awk -v n="$1" '$1 == n { found = 1 } END { exit !found }'`, "sh", "{pkg}"},
		InstallTmpl: []string{"uv", "tool", "install", "{pkg}"},
		RemoveTmpl:  []string{"uv", "tool", "uninstall", "{pkg}"},
	},
	"npm": {
		KindName:    "npm",
		Binary:      "npm",
		CheckTmpl:   []string{"npm", "ls", "-g", "--depth=0", "{pkg}"},
		InstallTmpl: []string{"npm", "install", "-g", "{pkg}"},
		RemoveTmpl:  []string{"npm", "uninstall", "-g", "{pkg}"},
	},
	"pnpm": {
		KindName:       "pnpm",
		Binary:         "pnpm",
		CheckTmpl:      []string{"pnpm", "ls", "-g", "--depth=0", "{pkg}"},
		InstallTmpl:    []string{"pnpm", "add", "-g", "{pkg}"},
		RemoveTmpl:     []string{"pnpm", "remove", "-g", "{pkg}"},
		AvailableExtra: "corepack",
	},
	"bun": {
		KindName:    "bun",
		Binary:      "bun",
		CheckTmpl:   []string{"sh", "-c", `bun pm ls -g | awk -v n="$1" 'index($NF, n "@") == 1 { found = 1 } END { exit !found }'`, "sh", "{pkg}"},
		InstallTmpl: []string{"bun", "add", "-g", "{pkg}"},
		RemoveTmpl:  []string{"bun", "remove", "-g", "{pkg}"},
	},
	"gem": {
		KindName:    "gem",
		Binary:      "gem",
		CheckTmpl:   []string{"sh", "-c", `gem list "$1" | awk -v n="$1" '$1 == n { found = 1 } END { exit !found }'`, "sh", "{pkg}"},
		InstallTmpl: []string{"gem", "install", "{pkg}"},
		RemoveTmpl:  []string{"gem", "uninstall", "{pkg}"},
	},
	"yarn": {
		KindName:    "yarn",
		Binary:      "yarn",
		CheckTmpl:   []string{"sh", "-c", `yarn global list --depth=0 | awk -v n="$1" 'index($2, "\"" n "@") == 1 { found = 1 } END { exit !found }'`, "sh", "{pkg}"},
		InstallTmpl: []string{"yarn", "global", "add", "{pkg}"},
		RemoveTmpl:  []string{"yarn", "global", "remove", "{pkg}"},
	},
	"composer": {
		KindName:    "composer",
		Binary:      "composer",
		CheckTmpl:   []string{"composer", "global", "show", "--locked", "{pkg}"},
		InstallTmpl: []string{"composer", "global", "require", "{pkg}"},
		RemoveTmpl:  []string{"composer", "global", "remove", "{pkg}"},
	},
	// apm is deprecated (Atom was discontinued); `apm uninstall` is
	// unreliable against modern registries. Kept manual.
	"apm": {
		KindName:    "apm",
		Binary:      "apm",
		CheckTmpl:   []string{"sh", "-c", `apm list --installed --bare | awk -v n="$1" 'index($1, n "@") == 1 { found = 1 } END { exit !found }'`, "sh", "{pkg}"},
		InstallTmpl: []string{"apm", "install", "{pkg}"},
	},
	"flatpak": {
		KindName:    "flatpak",
		Binary:      "flatpak",
		CheckTmpl:   []string{"flatpak", "info", "{pkg}"},
		InstallTmpl: []string{"flatpak", "install", "-y", "flathub", "{pkg}"},
		RemoveTmpl:  []string{"flatpak", "uninstall", "-y", "{pkg}"},
	},
	"snap": {
		KindName:    "snap",
		Binary:      "snap",
		CheckTmpl:   []string{"snap", "list", "{pkg}"},
		InstallTmpl: []string{"snap", "install", "{pkg}"},
		RemoveTmpl:  []string{"snap", "remove", "{pkg}"},
	},
	// vscode/vscodium install editor extensions; uninstalling an extension
	// (`code --uninstall-extension`) only matches exact extension IDs and
	// removing the editor itself is out of scope. Kept manual.
	"vscode": {
		KindName:       "vscode",
		Binary:         "code",
		CheckTmpl:      []string{"sh", "-c", `code --list-extensions | grep -qFx -- "$1"`, "sh", "{pkg}"},
		InstallTmpl:    []string{"code", "--install-extension", "{pkg}"},
		AvailableExtra: "code-insiders",
	},
	// vscodium: same extension-based policy as vscode above. Kept manual.
	"vscodium": {
		KindName:    "vscodium",
		Binary:      "codium",
		CheckTmpl:   []string{"sh", "-c", `codium --list-extensions | grep -qFx -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"codium", "--install-extension", "{pkg}"},
	},
	"cask": {
		KindName:    "cask",
		Binary:      "brew",
		CheckTmpl:   []string{"brew", "list", "--cask", "{pkg}"},
		InstallTmpl: []string{"brew", "install", "--cask", "{pkg}"},
		RemoveTmpl:  []string{"brew", "uninstall", "--cask", "{pkg}"},
	},
	// mas installs macOS App Store apps by numeric app id; `mas uninstall`
	// also requires the numeric id, which {pkg} may not be. Kept manual.
	"mas": {
		KindName:    "mas",
		Binary:      "mas",
		CheckTmpl:   []string{"sh", "-c", `mas list | awk -v n="$1" '$1 == n { found = 1 } END { exit !found }'`, "sh", "{pkg}"},
		InstallTmpl: []string{"mas", "install", "{pkg}"},
	},
}

func init() {
	// Verify that all Configs keys are known method kinds.
	// This ensures the ecosystem package stays consistent with methodkind,
	// the single source of truth for kind names.
	for name := range Configs {
		if !methodkind.IsKnownKind(name) {
			panic("ecosystem: Configs key " + name + " is not in methodkind.KnownKinds()")
		}
	}
}

// RegisterAll registers all language adapters with the global exec registry.
// AUR needs special construction (configurable helper binary); cargo needs
// git-repo support. Call this once from main() or an init().
func RegisterAll(aurHelper string) {
	// cargo has special git-repo support.
	exec.Register(NewCargoAdapter())

	// go has special Check (falls back to tool.Name for binary name).
	exec.Register(NewGoAdapter())

	// The rest use the generic BaseAdapter pattern.
	for name, cfg := range Configs {
		if name == "cargo" || name == "go" {
			continue // registered above
		}
		exec.Register(NewBaseAdapter(cfg))
	}

	// AUR uses a configurable helper binary.
	exec.Register(NewAURAdapter(aurHelper))
	// Also register named AUR helper aliases (paru, yay) so schema entries
	// like `paru = "pkg"` work directly.
	RegisterAURAliases()

	// Specialized adapters (not BaseAdapter-compatible).
	exec.Register(NewSDKManAdapter())
	exec.Register(NewSteamCMDAdapter())
	exec.Register(NewYarnBerryAdapter())
	exec.Register(NewPacstallAdapter())
	exec.Register(NewCondaAdapter())
	exec.Register(NewAsdfAdapter())
}

// ReconfigureAUR replaces the AUR adapter in the global registry with one
// configured to use the given helper binary. This allows the schema's
// defaults.aur_helper setting to override the init-time default ("paru").
// If helper is empty, the call is a no-op (keep the current default).
func ReconfigureAUR(helper string) {
	if helper == "" {
		return
	}
	exec.Replace(NewAURAdapter(helper))
}
