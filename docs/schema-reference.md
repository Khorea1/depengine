# schema.toml — full syntax reference

This is the complete reference for every way to declare a tool in
`schema.toml`. If you're just getting started, read
[the README](../README.md#your-first-schematoml) first — it covers the
~80% case in three lines. Come back here when you need something more
specific.

A schema describes **tools** (dependencies) and **methods** (how to install
each one). The engine tries methods in `method_order` until one succeeds.

**On this page:**

- [Naming a tool](#naming-a-tool) — simple names, per-manager names, ecosystem buckets
- [Custom sources](#custom-sources) — git forks, manual builds, HTTP artifacts
- [Method reference](#method-reference) — one-line syntax for all 30 methods
- [Hooks & dependencies](#hooks--dependencies) — pre-install hooks, tool-to-tool `requires`
- [Platform targeting](#platform-targeting) — `when` conditions, multi-method fallback
- [Method control](#per-tool-method-control) — `method_prefer`, `method_only`
- [Placeholders](#placeholders) — `{arch}`, `{os}`, `{latest}`, and more
- [Manifest merge rules](#manifest-merge-rules) — how schema + personal manifest combine

---

## Naming a tool

### Simple tool (name = package everywhere)

```toml
simple = ["zsh", "bat", "kitty", "mpv"]
```

### Package name varies per native manager

```toml
fd   = { apt = "fd-find" }                 # "fd" on all others
nvim = { pacman = "neovim", apt = "neovim" }
```

### Language ecosystem managers

```toml
organize = { pip = "organize-tool", pipx = "organize-tool" }
fzf      = { go  = "github.com/junegunn/fzf" }
lf       = { go  = "github.com/gokcehan/lf" }
```

### `true` shorthand and ecosystem buckets

When the package name equals the tool name (~80% of Python/Node cases), use
`true` instead of repeating it. Buckets expand to every method in an
ecosystem at once.

| Bucket | Expansion |
|--------|-----------|
| `python = true` | `{ pip = true, pipx = true, uv = true }` |
| `node = true` | `{ npm = true, pnpm = true, bun = true }` |

```toml
ruff     = { python = true }   # ≡ { pip = "ruff", pipx = "ruff", uv = "ruff" }
prettier = { node = true }     # ≡ { npm = "prettier", pnpm = "prettier", bun = "prettier" }
```

> Explicit methods are **not** overridden by the bucket:
> `organize = { pip = "organize-tool", python = true }` keeps `pip` as
> `"organize-tool"` and only expands `pipx`/`uv`.
>
> Buckets also accept a package name string or a config map:
>
> ```toml
> organize = { python = "organize-tool" }
> # ≡ { pip = "organize-tool", pipx = "organize-tool", uv = "organize-tool" }
> ```
>
> `python = false` does not expand — the engine treats `python` as an
> unknown method and errors on `validate`. `all = true` does not exist —
> too imprecise, risks installing the wrong package from the wrong
> ecosystem.

---

## Custom sources

### cargo/go with a custom git source

For when you need a fork or a source other than the official registry:

```toml
matugen = { cargo = { git = "https://github.com/InioX/matugen" } }
```

### Git: clone + manual build

```toml
ctpv = { git = { url = "https://github.com/NikitaIvanovV/ctpv", build = "make && sudo make install" } }
```

| Field | Required | Description |
|-------|----------|--------------|
| `url` | yes | Git repository URL |
| `build` | no | Shell command run in the cloned directory |
| `extract_to` | no | Directory to copy build artifacts into |
| `branch` | no | Branch or tag to clone (default: repo's default branch) |
| `depth` | no | Clone depth — `"1"` for shallow (default), `"0"` for full history |
| `binary` | no | Binary name for check/remove; required for removal from shared directories |

### HTTP: download an artifact (deb, zip, binary)

```toml
fastfetch = { http = {
  url = "https://github.com/fastfetch-cli/fastfetch/releases/download/{latest}/fastfetch-linux-amd64.deb",
  checksum = "sha256:auto"
} }
```

> `checksum` accepts a literal hash (`sha256:...`) or `:auto` (automatic
> resolution — this is Trust On First Use, not offline-verified; prefer a
> literal hash when you can pin one). Use `checksum_url` for a separate
> source, `sudo_required = false` if root isn't needed, and
> `signing_key`/`signature_url` for GPG verification.
>
> **If you omit `checksum` entirely, the file is installed with no
> integrity check at all.** Treat that the same as any other
> arbitrary-code-execution risk in your schema.

| Field | Required | Description |
|-------|----------|--------------|
| `url` | yes | Download URL; supports `{latest}` |
| `checksum` | no | `"sha256:<hex>"`, `"md5:<hex>"`, `"sha1:<hex>"`, `"sha512:<hex>"`, or `"<algo>:auto"` |
| `checksum_url` | no | Explicit URL for the checksum file (overrides auto patterns) |
| `checksum_file_format` | no | `"sha256sum"` (default), `"bsd"`, or `"raw"` |
| `signature_url` | no | GPG detached signature URL, for verifying the checksum file |
| `signing_key` | no | GPG key URL or fingerprint |
| `extract_to` | no | Extraction destination (default: `/usr/local/bin`) |

| `sudo_required` | no | Boolean, default is **path-derived**: `false` when `extract_to` is inside the user's home (e.g. `~/.local/share/fonts`), `true` for system paths (e.g. the `/usr/local/bin` default). Set explicitly to override. |

Archive type is auto-detected from the URL extension: `.tar.gz`, `.tgz`,
`.zip`, `.deb`, `.bin`, or bare binary.

---

## Method reference

One-line syntax for every supported method. All of them accept `when` (see
[Platform targeting](#platform-targeting)); `git` and `http` take extra
fields, documented above under [Custom sources](#custom-sources).

| Method | What it installs | Example |
|--------|-------------------|---------|
| `native` | Auto-detected distro manager (apt/pacman/dnf/brew/...) | `fd = { apt = "fd-find" }` |
| `cargo` | crates.io (or a git repo via `git` sub-key) | `ripgrep = { cargo = "ripgrep" }` |
| `go` | pkg.go.dev (or a git repo via `git` sub-key) | `fzf = { go = "github.com/junegunn/fzf" }` |
| `pip` | Python packages | `organize = { pip = "organize-tool" }` |
| `pipx` | Python CLI tools, isolated environments | `organize = { pipx = "organize-tool" }` |
| `uv` | Python packages via `uv tool` | `organize = { uv = "organize-tool" }` |
| `npm` | Global npm packages | `prettier = { npm = "prettier" }` |
| `pnpm` | Global pnpm packages | `prettier = { pnpm = "prettier" }` |
| `bun` | Global bun packages | `tsx = { bun = "tsx" }` |
| `gem` | Ruby gems | `sass = { gem = "sass" }` |
| `yarn` | Global yarn packages | `typescript = { yarn = "typescript" }` |
| `yarn-berry` | Yarn Berry (v2+) global packages | `typescript = { yarn-berry = "typescript" }` |
| `composer` | Global PHP Composer packages | `php-cs-fixer = { composer = "friendsofphp/php-cs-fixer" }` |
| `apm` | Atom package manager (legacy) | `atom-beautify = { apm = "atom-beautify" }` |
| `vscode` | VS Code extensions | `golang = { vscode = "golang.go" }` |
| `vscodium` | VSCodium extensions | `golang = { vscodium = "golang.go" }` |
| `flatpak` | Flathub apps | `spotify = { flatpak = "com.spotify.Client" }` |
| `snap` | Snap packages | `hello = { snap = "hello" }` |
| `cask` | macOS Homebrew casks | `docker = { cask = "docker" }` |
| `mas` | Mac App Store, by app ID | `xcode = { mas = "497799835" }` |
| `sdkman` | SDKMAN! JVM SDKs | `java17 = { sdkman = "java" }` |
| `steamcmd` | SteamCMD game server tools | `cs2 = { steamcmd = "730" }` |
| `pacstall` | Pacstall packages (Debian-based AUR-like) | `neofetch = { pacstall = "neofetch" }` |
| `aur` | Arch User Repository (via configured `aur_helper`) | `google-chrome = { aur = "google-chrome" }` |
| `winget` | Windows Package Manager | `git = { winget = "Git.Git" }` |
| `scoop` | Windows, via Scoop | `git = { scoop = "git" }` |
| `choco` | Windows, via Chocolatey | `firefox = { choco = "firefox" }` |
| `conda` | Conda packages | `numpy = { conda = "numpy" }` |
| `asdf` | asdf version manager plugins | `nodejs = { asdf = "nodejs" }` |
| `git` | Clone + build (see field table above) | `ctpv = { git = { url = "...", build = "make install" } }` |
| `http` | Download + extract + checksum (see field table above) | `fastfetch = { http = { url = "...", checksum = "sha256:auto" } }` |

---

## Hooks & dependencies

### Pre-install hook (before any method)

```toml
[tools.myenv]
pre_install = "curl -fsSL https://setup.example.com | sh"

  [tools.myenv.native]
  pkg = "my-env"
```

> `pre_install` runs before the first method — if it fails, the tool is
> aborted. Requires `--allow-arbitrary-code` (a security warning is shown by
> default, and the tool is skipped unless the flag is passed).

### Tool-to-tool dependency

```toml
zathura-pdf-mupdf = { requires = ["zathura"], pacman = "zathura-pdf-mupdf" }
```
A dependency can be gated per-platform with `requires_when` — useful when a
dep is only needed for some `when`-gated methods:

```toml
requires      = ["unzip", "fontconfig"]
requires_when = { fontconfig = { target_family = ["unix"] } }
```

`fontconfig` participates in the install graph only on unix; on Windows the
edge disappears (no dangling-ref error, no blocking).

`postinstall` (and `post_install`) additionally accept a table form with
its own `when`, so platform-specific hooks don't run where they can't work:

```toml
postinstall = { cmd = "fc-cache -fv", when = { target_family = ["unix"] } }
```

The plain-string form stays valid and is unconditional.

---

## Platform targeting

### Complex case: multiple methods + distro condition

```toml
[tools.DepartureMono]
postinstall = { cmd = "fc-cache -fv", when = { target_family = ["unix"] } }

  [tools.DepartureMono.aur]
  pkg  = "otf-departure-mono-nerd"
  when = { distro_family = ["arch"] }

  [tools.DepartureMono.scoop]
  pkg  = "nerd-fonts/DepartureMono-NF"
  when = { target_family = ["windows"] }

  [tools.DepartureMono.http]
  url        = "https://github.com/ryanoasis/nerd-fonts/releases/download/{latest}/DepartureMono.zip"
  extract_to = "~/.local/share/fonts/DepartureMono"
  when       = { target_family = ["unix"] }
```

> **Golden rule:** tool-level fields (`requires`, `postinstall`,
> `pre_install`) go _outside_ the method block. Method-specific fields
> (`kind`, `when`, `url`, `build`, `checksum`, `extract_to`, `pkg`, `git`) go
> _inside_.

### Platform conditions (`when`), multi-dimension gating

A method's `when` clause can specify **multiple platform dimensions**. The
engine evaluates all non-empty fields against the detected system facts:

- **AND between fields** — if you specify `arch` + `libc` + `os`, all three
  must match.
- **OR within a field** — `arch = ["x86_64", "aarch64"]` is satisfied by
  either.
- **Empty fields are ignored** — a condition with only `arch` set doesn't
  care about libc.
- **Nil / absent `when` always matches.**
- **`distro_family`** is the resolved *clan* (e.g. Ubuntu → `debian`), not
  the raw distro ID — use `distro_id` for exact-distro matching.

| Field | Type | Comparison | Example values |
|-------|------|------------|-----------------|
| `distro_family` | `string[]` | Exact (case-insensitive) | `arch`, `debian`, `fedora`, `alpine`, `gentoo`, `macos`, `freebsd`... |
| `distro_id` | `string[]` | Exact (case-insensitive) | `ubuntu`, `arch`, `fedora`, `debian`, `alpine`... |
| `arch` | `string[]` | Exact (case-insensitive) | `x86_64`, `aarch64`, `armv7l`... |
| `os` | `string[]` | Exact (case-insensitive) | `linux`, `darwin`, `windows`, `freebsd`, `openbsd`, `netbsd` |
| `target_family` | `string[]` | Exact (case-insensitive) | `unix` (linux, darwin, BSDs, termux), `windows` |
| `kernel` | `string[]` | Exact (case-insensitive) | `6.7.0-arch`, `5.15.0-generic`... |
| `libc` | `string[]` | **Prefix** match | `glibc` matches `glibc 2.35`; `musl` for Alpine |
| `init_system` | `string[]` | Exact (case-insensitive) | `systemd`, `openrc`, `runit`, `sysvinit` |
| `is_wsl` | `bool` | Three-state | Detected via `/proc/version` or `WSL_DISTRO_NAME` |
| `is_container` | `bool` | Three-state | Detected via `.dockerenv`, cgroup, etc. |

```toml
# AUR only on Arch, HTTP fallback everywhere else
[tools.DepartureMono]
  [tools.DepartureMono.aur]
  pkg  = "otf-departure-mono-nerd"
  when = { distro_family = ["arch"] }

  [tools.DepartureMono.http]
  url = "https://github.com/ryanoasis/nerd-fonts/releases/download/{latest}/DepartureMono.zip"
```

```toml
# Different binary per architecture + libc combination
[tools.restic]
  [tools.restic.http]
  url  = "https://github.com/restic/restic/releases/download/{latest}/restic_{latest}_linux_{arch}.bz2"
  when = { arch = ["x86_64", "aarch64"], os = ["linux"], libc = ["glibc"] }

  [tools.restic.http-musl]
  kind = "http"
  url  = "https://github.com/restic/restic/releases/download/{latest}/restic_{latest}_linux_{arch}_musl.bz2"
  when = { arch = ["x86_64", "aarch64"], os = ["linux"], libc = ["musl"] }
```

```toml
# WSL-specific install
[tools.podman]
  [tools.podman.native]
  when = { is_wsl = false }

  [tools.podman.http]
  url  = "https://github.com/containers/podman/releases/download/{latest}/podman-wsl-{arch}.zip"
  when = { is_wsl = true }
```

```toml
# Kernel-specific DKMS package
[tools.v4l2loopback]
  [tools.v4l2loopback.aur]
  pkg  = "v4l2loopback-dkms"
  when = { distro_family = ["arch"], kernel = ["6.7", "6.8", "6.9"] }
```

> **Tip:** run `depengine why <tool>` to see which method applies on your
> current machine and why the others were skipped.

---

## Per-tool method control

Override the method order for a single tool with `method_prefer` (prefix) or
`method_only` (exclusive list):

```toml
# Try cargo first, fall back to the default order:
myapp = { method_prefer = ["cargo"], cargo = true }

# Only use these methods, in this order — no fallback:
legacy = { method_only = ["aur", "git"], aur = { pkg = "legacy" }, git = { url = "..." } }
```

- **`method_prefer`** prepends the listed methods before the global
  `method_order`; methods not listed are still tried as fallbacks.
- **`method_only`** restricts the tool to exactly these methods, in this
  order — the global `method_order` is ignored for this tool.
- Both live at tool level (same level as `requires`, `tags`, `postinstall`),
  not inside a method block.

---

## Placeholders

Placeholders are expanded in every string field before installation.
Unknown placeholders are left untouched, so `depengine validate` can flag
them.

| Placeholder | Source | Example |
|-------------|--------|---------|
| `{arch}` | `detect_os.sh` | `x86_64`, `aarch64` |
| `{os}` | `detect_os.sh` | `linux`, `darwin` |
| `{distro_family}` | Resolved clan | `debian`, `arch`, `fedora` |
| `{id}` | `detect_os.sh` | `ubuntu`, `arch` |
| `{distro_name}` | `detect_os.sh` | `Ubuntu 24.04 LTS` |
| `{distro_id_like}` | `detect_os.sh` | `debian` |
| `{target_family}` | `detect_os.sh` | `linux` |
| `{kernel}` | `detect_os.sh` | `5.15.0` |
| `{libc}` | `detect_os.sh` | `glibc`, `musl` |
| `{init_system}` | `detect_os.sh` | `systemd`, `openrc` |
| `{detection}` | `detect_os.sh` | `os-release` |
| `{confidence}` | `detect_os.sh` | `high`, `medium` |
| `{is_wsl}` / `{is_container}` / `{is_android}` | `detect_os.sh` | `true`, `false` |
| `{pkg}` | Adapter-owned — substituted at install time | package name |
| `{latest}` | Adapter-owned — resolved via GitHub API (`git`/`http`) | `v1.2.3` |

```toml
# {arch}/{os} expand from detect_os.sh before installation:
fastfetch = { http = { url = "https://example.com/{os}/{arch}/fastfetch.deb" } }

# {latest} is resolved by the http/git adapter via GitHub's API:
fastfetch = { http = { url = "https://github.com/fastfetch-cli/fastfetch/releases/download/{latest}/fastfetch-linux-amd64.deb" } }
```

---

## Manifest merge rules

*(See [the README](../README.md#schematoml-vs-manifesttoml--two-layers-one-merged-config)
for a high-level overview of the two layers.)*

`schema.toml` and `~/.config/depengine/manifest.toml` merge per field,
according to a declared strategy per field:

1. **Whole-value overwrite** (most fields): if both layers set the field,
   the schema's value wins.
2. **Map merge** (e.g. `pkg_overrides`): keys from both layers are kept;
   where a key exists in both, the schema wins.
3. **Union** (e.g. `tags`): values from both layers are combined, without
   duplicates.
4. **Schema-overrides** (`pre_install`, `post_install`, `requires`,
   `method_order`, `method_prefer`, `method_only`): the manifest may set
   these as defaults, but the schema layer wins on conflict — the
   manifest's value is replaced, not merged.
5. **Tools only in the manifest** are rejected by default — your personal
   manifest doesn't silently add tools to a project you're working on.
   Set `[manifest] allow_new_tools = true` in your manifest to allow it
   explicitly. Without it, `depengine validate`/`install` errors when a
   manifest-only tool is found.

Run `depengine why <tool> --fields` to see exactly which layer contributed
each field for a given tool.
