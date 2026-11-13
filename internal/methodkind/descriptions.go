package methodkind

// kindDescriptions holds the one-sentence definition of every method kind.
// The generated JSON Schema and `depengine explain` both read it, so the
// vocabulary has exactly one source.
var kindDescriptions = map[string]string{
	"native":     "Install through the host's own package manager (apt, dnf, pacman, brew, port, pkg, ...).",
	"winget":     "Windows Package Manager (winget).",
	"scoop":      "Scoop packages on Windows.",
	"choco":      "Chocolatey packages on Windows.",
	"cargo":      "Rust crates from crates.io or a Git source, via cargo install.",
	"go":         "Go packages via go install.",
	"pipx":       "Isolated Python CLI tools via pipx.",
	"uv":         "Python CLI tools via uv tool.",
	"pip":        "Python packages via pip.",
	"npm":        "Global npm packages.",
	"pnpm":       "Global pnpm packages.",
	"bun":        "Global bun packages.",
	"gem":        "Ruby gems via gem install.",
	"yarn":       "Global Yarn Classic packages.",
	"yarn-berry": "Global Yarn Berry (v2+) packages.",
	"composer":   "Global PHP Composer packages.",
	"apm":        "Atom packages via the legacy apm CLI.",
	"vscode":     "VS Code extensions.",
	"vscodium":   "VSCodium extensions.",
	"flatpak":    "Flatpak applications, optionally from a named remote and branch.",
	"snap":       "Snap packages, with risk shorthand or structured track/risk/branch.",
	"cask":       "Homebrew Cask applications on macOS.",
	"mas":        "Mac App Store applications.",
	"sdkman":     "SDKMAN! JVM SDKs.",
	"steamcmd":   "SteamCMD app IDs; ordinary installs fail closed before mutation.",
	"appman":     "AppImage packages through AM/AppMan.",
	"pacstall":   "Pacstall packages on Debian-based systems.",
	"aur":        "Arch User Repository packages through the configured AUR helper.",
	"conda":      "Conda packages, optionally into a named environment or prefix.",
	"nix":        "Nix user-profile packages via nix profile.",
	"asdf":       "asdf plugins and versions, optionally through a mise backend.",
	"container":  "Container images pulled with docker or podman.",
	"appimage":   "Portable .AppImage binaries installed under a stable name.",
	"android":    "Download an .apk and hand it to Termux's package installer.",
	"git":        "Clone a Git repository and optionally build it.",
	"local":      "Install a project-vendored file or archive without network access.",
	"github":     "A GitHub release asset or branch tarball, resolved from the real asset list.",
	"http":       "An HTTP artifact that is downloaded, checked, and extracted.",
	"deb":        "A .deb package installed and tracked by its exact dpkg identity.",
	"msi":        "A Windows Installer product tracked by exact registry identity.",
	"exe":        "A Windows executable installer tracked by Add/Remove Programs identity.",
	"msix":       "A signed per-user MSIX package tracked by package name and publisher.",
	"appx":       "A signed per-user AppX package tracked by package name and publisher.",
	"macpkg":     "A signed macOS Installer package tracked by receipt package ID; removal is manual.",
	"dmg":        "One declared .app bundle copied from a read-only disk image.",
}

// Describe returns the short definition of kind. Native manager aliases and
// method aliases describe the contract they resolve to.
func Describe(kind string) string {
	contract, ok := Lookup(kind)
	if !ok {
		return ""
	}
	return kindDescriptions[contract.Kind]
}
