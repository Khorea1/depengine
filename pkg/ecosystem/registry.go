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
		CheckTmpl:   []string{"cargo", "install", "--list"},
		CheckOutput: checkCargoPackage,
		InstallTmpl: []string{"cargo", "install", "{pkg}"},
		RemoveTmpl:  []string{"cargo", "uninstall", "{pkg}"},
	},
	"go": {
		KindName: "go",
		Binary:   "go",
		// {bin} is the binary name derived from the import path (last path
		// element, or the element after /cmd/): `go install` never puts the
		// import path itself on PATH, so looking up {pkg} could never pass.
		CheckPath:   "{bin}",
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
		CheckTmpl:   []string{"pipx", "list", "--short"},
		CheckOutput: checkFirstField,
		InstallTmpl: []string{"pipx", "install", "{pkg}"},
		RemoveTmpl:  []string{"pipx", "uninstall", "{pkg}"},
	},
	"uv": {
		KindName:    "uv",
		Binary:      "uv",
		CheckTmpl:   []string{"uv", "tool", "list"},
		CheckOutput: checkFirstField,
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
		CheckTmpl:   []string{"bun", "pm", "ls", "-g"},
		CheckOutput: checkLastFieldVersion,
		InstallTmpl: []string{"bun", "add", "-g", "{pkg}"},
		RemoveTmpl:  []string{"bun", "remove", "-g", "{pkg}"},
	},
	"gem": {
		KindName:    "gem",
		Binary:      "gem",
		CheckTmpl:   []string{"gem", "list", "{pkg}"},
		CheckOutput: checkFirstField,
		InstallTmpl: []string{"gem", "install", "{pkg}"},
		RemoveTmpl:  []string{"gem", "uninstall", "{pkg}"},
	},
	"yarn": {
		KindName:    "yarn",
		Binary:      "yarn",
		CheckTmpl:   []string{"yarn", "global", "list", "--depth=0"},
		CheckOutput: checkSecondFieldVersion,
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
		CheckTmpl:   []string{"apm", "list", "--installed", "--bare"},
		CheckOutput: checkLastFieldVersion,
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
		CheckTmpl:      []string{"code", "--list-extensions"},
		CheckOutput:    checkExactLine,
		InstallTmpl:    []string{"code", "--install-extension", "{pkg}"},
		AvailableExtra: "code-insiders",
	},
	// vscodium: same extension-based policy as vscode above. Kept manual.
	"vscodium": {
		KindName:    "vscodium",
		Binary:      "codium",
		CheckTmpl:   []string{"codium", "--list-extensions"},
		CheckOutput: checkExactLine,
		InstallTmpl: []string{"codium", "--install-extension", "{pkg}"},
	},
	"cask": {
		KindName:    "cask",
		Binary:      "brew",
		CheckTmpl:   []string{"brew", "list", "--cask", "{pkg}"},
		InstallTmpl: []string{"brew", "install", "--cask", "{pkg}"},
		RemoveTmpl:  []string{"brew", "uninstall", "--cask", "{pkg}"},
	},
	// appman ("AM"/"AppMan" AppImage package manager, ivan-hc/AM). Installs
	// land on PATH as a binary named after the program (system-wide under
	// /usr/local/bin, or ~/.local/bin in AppMan/--user mode), so a PATH lookup
	// is the reliable check — `am`/`appman` itself has no
	// documented single-package "is this installed?" query. A native PATH
	// lookup avoids depending on a platform shell. `-y` makes `-i`
	// (install) non-interactive; `-R` (as opposed to `-r`) removes without
	// asking for confirmation.
	"appman": {
		KindName:    "appman",
		Binary:      "appman",
		CheckPath:   "{pkg}",
		InstallTmpl: []string{"appman", "-y", "-i", "{pkg}"},
		RemoveTmpl:  []string{"appman", "-R", "{pkg}"},
	},
	// mas installs macOS App Store apps by numeric app id; `mas uninstall`
	// also requires the numeric id, which {pkg} may not be. Kept manual.
	"mas": {
		KindName:    "mas",
		Binary:      "mas",
		CheckTmpl:   []string{"mas", "list"},
		CheckOutput: checkFirstField,
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
