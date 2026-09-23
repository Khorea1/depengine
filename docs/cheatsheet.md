# depengine cheatsheet

Copy-paste reference. For explanations, see
[the README](../README.md), [schema-reference.md](schema-reference.md), and
[cli-reference.md](cli-reference.md).

## Commands

```sh
depengine init --add "zsh,bat,nvim"        # scaffold schema.toml
depengine install                          # install everything
depengine install --only nvim              # one tool
depengine install --dry-run --sort-by name # preview, sorted
depengine install --diagnose               # DEBUG + dry-run + verbose
depengine install --json --skip "bat,lsd"  # JSON output, skip tools
depengine install --jobs 4                 # concurrent workers
depengine install --profile=desktop        # only tools tagged "desktop"

depengine validate --check-env --format json
depengine validate --strict                # warnings → exit code 1

depengine check nvim                       # is nvim installed?
depengine status --orphans                 # installed, not in schema
depengine remove bat
depengine remove --all --dry-run

depengine why nvim --fields                # explain + provenance
depengine graph --format=mermaid --profile=desktop
depengine undo --list                       # show snapshots
depengine undo --snapshot <path>
depengine sbom --format=spdx
depengine diff state1.json state2.json
depengine completion bash | zsh | fish
```

> **Warning:** concurrent native package manager installs (`apt-get`,
> `pacman -S`) can cause lock contention. Prefer `--jobs=1` for many native
> tools.

Full flag list: [cli-reference.md](cli-reference.md).

---

## Schema structure

This is a fragment. Complete project schemas also need `schema_version = 1`.
Projects use `[tools]`; personal manifests use `[packages]`.

```toml
[defaults]          # global defaults (manager, aur_helper, method_prefer)
[tools]             # all dependencies live here
  simple = [...]    #   shorthand list
  name = { ... }    #   inline table
  [tools.NAME]      #   full block (for complex tools)
    [tools.NAME.method]  # one sub-table per candidate method
```

Tool-level fields (`requires`, `pre_install`, `post_install`, `tags`) live
outside methods. Method-level fields (`kind`, `when`, `url`, `build`,
`checksum`, `pkg`, `git`) live inside a method.
Tool-level fields can carry conditions: `post_install = { cmd = "...", when =
{ target_family = ["unix"] } }` skips the hook when it can't apply, and
`requires_when = { fontconfig = { target_family = ["unix"] } }` drops the
dependency from the graph when its condition fails.

```toml
[defaults]
manager = "native"
aur_helper = "paru"                        # or "yay"
method_prefer = ["native", "cargo", "github", "http"] # preferred prefix; defaults remain
```

---

## Declaration forms

| Form | Syntax |
|------|--------|
| Simple list | `simple = ["zsh", "bat", "kitty"]` |
| Per-manager name | `fd = { apt = "fd-find" }` |
| Language manager | `fzf = { go = "github.com/junegunn/fzf" }` |
| Cargo Git revision | `matugen = { cargo = { git = "https://github.com/InioX/matugen", rev = "0123456789abcdef" } }` |
| Cargo registry/version | `ripgrep = { cargo = { pkg = "ripgrep", registry = "corp", version = "14.1.1" } }` |
| Cargo root/target/bins | `tool = { cargo = { pkg = "tool", root = "~/.local/cargo-tools", target = "x86_64-unknown-linux-musl", bins = ["tool"] } }` |
| Conda target/version | `numpy = { conda = { pkg = "numpy", environment = "data", version = "2.1.0", channels = ["conda-forge"] } }` |
| GitHub release | `yq = { github = { repo = "mikefarah/yq", asset = "yq_{os_any}_{arch_any}" } }` |
| Owned archive | `nvim = { github = { repo = "neovim/neovim", asset = "nvim-{os_any}-{arch_any}.tar.gz", strip_components = 1, extract_to = "~/.local/opt/nvim", entrypoints = { nvim = "bin/nvim" } } }` |
| User-scoped artifact | `ripgrep = { github = { repo = "BurntSushi/ripgrep", asset = "ripgrep-{version}-{arch_any}.tar.gz", scope = "user", entrypoints = { rg = "ripgrep" } } }` |
| Snap options | `nvim = { snap = { pkg = "nvim", confinement = "classic", track = "latest", risk = "stable" } }` |
| Chocolatey exact/source/arch | `nvim = { choco = { pkg = "neovim", version = "0.10.4", source = "https://community.chocolatey.org/api/v2/", architecture = "x64", prerelease = true } }` |
| Flatpak remote/branch/scope | `spotify = { flatpak = { pkg = "com.spotify.Client", remote = "flathub", branch = "stable", scope = "user" } }` |
| Ecosystem bucket | `ruff = { python = true }` |
| Full block | multiple methods + `when` + hooks — see [schema-reference.md#platform-targeting](schema-reference.md#platform-targeting) |

Details: [schema-reference.md#naming-a-tool](schema-reference.md#naming-a-tool).

---

## `when` quick fields

```toml
[tools.my-tool.aur]
pkg  = "my-tool"
when = { distro_family = ["arch"] }
```

`distro_family`, `distro_id`, `arch`, `os`, `kernel`, `libc`, `init_system`, `target_family`
(string-list, AND across fields, OR within); `is_wsl`, `is_container`,
`is_android` (bool). See [platform targeting](schema-reference.md#platform-targeting).

---

## Method preference & control

```toml
myapp  = { method_prefer = ["cargo"], cargo = true }   # try cargo first, fall back
legacy = { method_only = ["aur", "git"], aur = { pkg = "legacy" }, git = { url = "..." } }  # only these methods are tried — no native fallback
```

Details: [schema-reference.md#per-tool-method-control](schema-reference.md#per-tool-method-control).

`kind` selects the adapter. A custom subtable name is only a candidate label,
and TOML declaration order never sets execution priority. `method_prefer` is a
prefix with fallbacks; `method_only` is exclusive. Scalar/bool method shorthand
implicitly adds native fallback; explicit method subtables do not. Add a
`native` method explicitly when a full-table declaration should also try native.

---

## Installation methods

Full one-liner-per-method table (with `git`/`http` field lists):
[schema-reference.md#method-reference](schema-reference.md#method-reference).

```
Native:      native (auto-detects apt/pacman/dnf/brew/...)
Language:    cargo, go, pip, pipx, uv, npm, pnpm, bun, gem, yarn,
             yarn-berry, composer, apm
Desktop:     flatpak, snap, vscode, vscodium, cask, mas, appman
Windows:     winget, scoop, choco, msi
Specialized: sdkman, steamcmd, pacstall, aur, conda, asdf, container,
             appimage, android
Other:       git, github, http
```

---

## Placeholders

Runtime URL placeholders and GitHub asset-matching placeholders have different
owners. See [placeholders](schema-reference.md#placeholders).

---

## Schema vs manifest, in one line

`schema.toml` (project, shared) always wins on conflict over
`manifest.toml` (personal, `~/.config/depengine/`). Manifest-only tools are
silently dropped unless `allow_new_tools = true`. See
[manifest merge rules](schema-reference.md#manifest-merge-rules).
