package lang

import (
	"depengine/pkg/exec"
)

// Configs holds the definitions for all supported language adapters.
// Each entry maps to a method_order name and describes how to check
// for and install packages via that tool.
var Configs = map[string]BaseConfig{
	"cargo": {
		KindName:    "cargo",
		Binary:      "cargo",
		CheckTmpl:   []string{"sh", "-c", `cargo install --list | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"cargo", "install", "{pkg}"},
		RemoveTmpl:  []string{"cargo", "uninstall", "{pkg}"},
	},
	"go": {
		KindName:    "go",
		Binary:      "go",
		CheckTmpl:   []string{"which", "{pkg}"},
		InstallTmpl: []string{"go", "install", "{pkg}@latest"},
		RemoveTmpl:  []string{"go", "clean", "{pkg}"},
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
		CheckTmpl:   []string{"sh", "-c", `pipx list --short 2>/dev/null | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"pipx", "install", "{pkg}"},
		RemoveTmpl:  []string{"pipx", "uninstall", "{pkg}"},
	},
	"uv": {
		KindName:    "uv",
		Binary:      "uv",
		CheckTmpl:   []string{"sh", "-c", `uv tool list 2>/dev/null | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"uv", "tool", "install", "{pkg}"},
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
		AvailableExtra: "corepack",
	},
	"bun": {
		KindName:    "bun",
		Binary:      "bun",
		CheckTmpl:   []string{"sh", "-c", `bun pm ls -g | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"bun", "add", "-g", "{pkg}"},
	},
	"gem": {
		KindName:    "gem",
		Binary:      "gem",
		CheckTmpl:   []string{"sh", "-c", `gem list "$1" | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"gem", "install", "{pkg}"},
	},
	"yarn": {
		KindName:    "yarn",
		Binary:      "yarn",
		CheckTmpl:   []string{"sh", "-c", `yarn global list --depth=0 | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"yarn", "global", "add", "{pkg}"},
	},
	"composer": {
		KindName:    "composer",
		Binary:      "composer",
		CheckTmpl:   []string{"composer", "global", "show", "--locked", "{pkg}"},
		InstallTmpl: []string{"composer", "global", "require", "{pkg}"},
	},
	"apm": {
		KindName:    "apm",
		Binary:      "apm",
		CheckTmpl:   []string{"sh", "-c", `apm list --installed --bare | grep -qF -- "$1"`, "sh", "{pkg}"},
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
	},
	"vscode": {
		KindName:       "vscode",
		Binary:         "code",
		CheckTmpl:      []string{"sh", "-c", `code --list-extensions | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl:    []string{"code", "--install-extension", "{pkg}"},
		AvailableExtra: "code-insiders",
	},
	"vscodium": {
		KindName:    "vscodium",
		Binary:      "codium",
		CheckTmpl:   []string{"sh", "-c", `codium --list-extensions | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"codium", "--install-extension", "{pkg}"},
	},
	"cask": {
		KindName:    "cask",
		Binary:      "brew",
		CheckTmpl:   []string{"brew", "list", "--cask", "{pkg}"},
		InstallTmpl: []string{"brew", "install", "--cask", "{pkg}"},
	},
	"mas": {
		KindName:    "mas",
		Binary:      "mas",
		CheckTmpl:   []string{"sh", "-c", `mas list | grep -qF -- "$1"`, "sh", "{pkg}"},
		InstallTmpl: []string{"mas", "install", "{pkg}"},
	},
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
