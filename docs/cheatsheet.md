# depengine cheatsheet

One-page copy-paste reference. For explanations, see
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

```toml
[defaults]          # global defaults (manager, aur_helper, method_order)
[tools]             # all dependencies live here
  simple = [...]    #   shorthand list
  name = { ... }    #   inline table
  [tools.NAME]      #   full block (for complex tools)
    [tools.NAME.method]  # one sub-table per candidate method
```

**Golden rule:** tool-level fields (`requires`, `pre_install`, `postinstall`,
`tags`) live outside methods; method-level fields (`kind`, `when`, `url`,
`build`, `checksum`, `pkg`, `git`) live inside.

```toml
[defaults]
manager = "native"
aur_helper = "paru"                        # or "yay"
method_order = ["native", "cargo", "go", "pipx", "uv", "pip", "npm", "pnpm",
  "bun", "gem", "yarn", "yarn-berry", "composer", "apm", "vscode",
  "vscodium", "flatpak", "snap", "cask", "mas", "sdkman", "steamcmd",
  "pacstall", "aur", "conda", "asdf", "git", "http"]
```

---

## Declaration forms at a glance

| Form | Syntax |
|------|--------|
| Simple list | `simple = ["zsh", "bat", "kitty"]` |
| Per-manager name | `fd = { apt = "fd-find" }` |
| Language manager | `fzf = { go = "github.com/junegunn/fzf" }` |
| git sub-key | `matugen = { cargo = { git = "https://github.com/InioX/matugen" } }` |
| Ecosystem bucket | `ruff = { python = true }` |
| Full block | multiple methods + `when` + hooks — see [schema-reference.md#platform-targeting](schema-reference.md#platform-targeting) |

Full explanations and more examples: [schema-reference.md#naming-a-tool](schema-reference.md#naming-a-tool).

---

## `when` quick fields

```toml
[tools.my-tool.aur]
pkg  = "my-tool"
when = { distro_family = ["arch"] }
```

`distro_family`, `distro_id`, `arch`, `os`, `kernel`, `libc`, `init_system`
(string-list, AND across fields, OR within); `is_wsl`, `is_container`
(bool). Full table: [schema-reference.md#platform-targeting](schema-reference.md#platform-targeting).

---

## Method order & control

```toml
myapp  = { method_prefer = ["cargo"], cargo = true }   # try cargo first, fall back
legacy = { method_only = ["aur", "git"], aur = { pkg = "legacy" }, git = { url = "..." } }  # only these methods are tried — no native fallback
```

Details: [schema-reference.md#per-tool-method-control](schema-reference.md#per-tool-method-control).

---

## All 30 methods

Full one-liner-per-method table (with `git`/`http` field lists):
[schema-reference.md#method-reference](schema-reference.md#method-reference).

```
Native:      native (auto-detects apt/pacman/dnf/brew/...)
Language:    cargo, go, pip, pipx, uv, npm, pnpm, bun, gem, yarn,
             yarn-berry, composer, apm
Desktop:     flatpak, snap, vscode, vscodium, cask, mas
Windows:     winget, scoop, choco
Specialized: sdkman, steamcmd, pacstall, aur, conda, asdf
Other:       git, http
```

---

## Placeholders

`{arch}` `{os}` `{distro_family}` `{kernel}` `{libc}` `{pkg}` `{latest}` and
8 more — full table: [schema-reference.md#placeholders](schema-reference.md#placeholders).

---

## Schema vs manifest, in one line

`schema.toml` (project, shared) always wins on conflict over
`manifest.toml` (personal, `~/.config/depengine/`). Manifest-only tools are
rejected unless `allow_new_tools = true`. Full merge rules:
[schema-reference.md#manifest-merge-rules](schema-reference.md#manifest-merge-rules).
