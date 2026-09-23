# `schema.toml` reference

This is the full reference for `schema.toml`. For setup and common commands,
start with the [README](../README.md#quick-start).

depengine currently accepts `schema_version = 1` only. The format is still
pre-freeze, so version 1 is not yet a permanent backward-compatibility promise.
See [file format compatibility](compatibility.md).

A schema declares **tools** and one or more **install methods** for each tool.
depengine tries candidates in configured priority order until one succeeds.
TOML declaration order does not choose that priority.

Unless a section says "complete", examples below are fragments. Bare tool
assignments belong under `[tools]`; `[tools.NAME...]` examples assume the file
already contains `schema_version = 1`. See
[`schema.example.toml`](../schema.example.toml) for a larger complete example.

**On this page:**

- [Naming a tool](#naming-a-tool) — simple names, per-manager names, ecosystem buckets
- [Custom sources](#custom-sources) — git forks, manual builds, HTTP artifacts
- [Method reference](#method-reference) — one-line syntax for every method
- [Hooks & dependencies](#hooks--dependencies) — hooks before installation, tool-to-tool `requires`
- [Platform targeting](#platform-targeting) — `when` conditions, multi-method fallback
- [Method control](#per-tool-method-control) — `method_prefer`, `method_only`
- [Placeholders](#placeholders) — `{arch}`, `{os}`, `{latest}`, and more
- [Manifest merge rules](#manifest-merge-rules) — how schema + personal manifest combine

---

## Complete copy-paste documents

These examples are complete files and are validated by the test suite.

### Minimal project schema (complete)

```toml
schema_version = 1

[tools]
simple = ["zsh", "bat"]
```

### GitHub release schema (complete)

```toml
schema_version = 1

[tools]
yq = { method_only = ["github"], github = { repo = "mikefarah/yq", asset = "yq_{os_any}_{arch_any}" } }
```

### Ordered candidates and hooks (complete)

```toml
schema_version = 1

[defaults]
method_order = ["native", "cargo", "github", "http"]

[tools]
myapp = { method_prefer = ["cargo"], pre_install = { run = ["test", "-x", "/usr/bin/cargo"] }, post_install = { run = ["myapp", "--version"] }, cargo = true }
```

### Personal manifest admitting new tools (complete)

```toml
schema_version = 1

[manifest]
allow_new_tools = true

[packages]
personal-tool = { git = { url = "https://github.com/example/personal-tool", depth = 1 } }
```

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

When the package name equals the tool name, use `true` instead of repeating it.
Buckets expand to every method in an ecosystem at once.

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

### Cargo with a custom git source

For forks or sources outside the official registry:

```toml
matugen = { cargo = { git = "https://github.com/InioX/matugen", rev = "0123456789abcdef" } }
```

Cargo Git installs accept exactly one of `branch`, `tag`, or `rev`; each of
those fields requires `git`. Registry installs can select a registry and exact
crate version:

```toml
ripgrep = { cargo = { pkg = "ripgrep", registry = "corp", version = "14.1.1" } }
```

`cargo.git` is mutually exclusive with `cargo.version` and `cargo.registry`.

Cargo also accepts typed build/profile options: `features = ["..."]`,
`no_default_features = true`, `bins = ["..."]`, `target = "<triple>"`, and
`root = "<path>"`. `root` is used consistently by install, check, and remove;
selected bins are verified from `cargo install --list`.

For Go tools, `version` selects the suffix passed to `go install` instead of
the default `@latest`:

```toml
stringer = { go = { pkg = "golang.org/x/tools/cmd/stringer", version = "v0.36.0" } }
```

### Git: clone + manual build

```toml
ctpv = { git = { url = "https://github.com/NikitaIvanovV/ctpv", build = [{ run = ["make"] }, { run = ["sudo", "make", "install"] }] } }
```

| Field | Required | Description |
|-------|----------|--------------|
| `url` | yes | Git repository URL |
| `build` | no | Command `{ run = ["program", "arg", ...] }`, or a list of commands, run in the cloned directory. Legacy strings remain POSIX `sh -c` shorthand. |
| `branch` | no | Branch to clone; mutually exclusive with `tag` and `rev` |
| `tag` | no | Tag to clone; mutually exclusive with `branch` and `rev` |
| `rev` | no | Exact Git revision/commit to fetch and check out detached |
| `depth` | no | Non-negative clone/fetch depth as an integer or numeric string — `1` is the default; `0` requests full history |
| `submodules` | no | Run `git submodule update --init --recursive` after the selected revision is checked out |
| `extract_to` | no | Directory to copy build artifacts into |
| `artifact` | no | File or directory inside the clone to copy to `extract_to`; defaults to the clone root |
| `binary` | no | Binary name for check/remove; required for removal from shared directories |
| `managed_paths` | no | Absolute paths owned by the recipe; all must exist for `Check`, and removal deletes only these exact targets |

### HTTP: download an artifact (deb, zip, binary)

```toml
fastfetch = { http = {
  url = "https://github.com/fastfetch-cli/fastfetch/releases/download/{latest}/fastfetch-linux-amd64.deb",
  checksum = "sha256:auto",
  when = { distro_family = ["debian", "mint"], arch = ["x86_64"] }
} }
```

> `checksum` accepts a literal hash (`sha256:...`) or `:auto` (automatic
> resolution — this is Trust On First Use, not offline-verified; prefer a
> literal hash when available). Use `checksum_url` for a separate
> source, `sudo_required = false` if root isn't needed, and
> `signing_key`/`signature_url` for GPG verification.
>
> **If `checksum` is omitted entirely, the file is installed with no
> integrity check at all.** Treat that the same as any other
> arbitrary-code-execution risk in the schema.
>
> `.deb` is a distribution-specific package format, not a portable Linux
> binary. depengine accepts a generic `.deb` automatically on Debian-family
> targets (including the separate `mint` clan). On native Termux, a `.deb`
> built specifically for Termux is valid, but must be scoped explicitly with
> `when = { distro_family = ["termux"] }` or `distro_id = ["termux"]` so a
> Debian/Ubuntu package is not mistaken for a Termux package merely because
> both environments provide `apt`/`dpkg`. A `proot-distro` Debian/Ubuntu guest
> is detected as that guest distro and follows the normal Debian-family rule.
> Other non-Debian targets reject `.deb` by default; an exact
> `distro_family`/`distro_id` condition acts as an explicit author override for
> a package known to be built for that target. CPU architecture remains a
> separate compatibility requirement; hard-coded `amd64`/`arm64` artifacts
> should also be gated with `when.arch` (or expressed through the appropriate
> architecture mapping/placeholders).

| Field | Required | Description |
|-------|----------|--------------|
| `url` or `repo` + `asset` | yes | Exactly one artifact source. Literal URLs support runtime URL placeholders such as `{latest}`, `{arch}`, and `{os}`. `repo` + `asset` uses GitHub release asset matching. |
| `release` | no | Named GitHub release tag for `repo` + `asset` |
| `branch` | no | GitHub release tag expressing rolling-branch intent; when set, it takes precedence over `release` |
| `checksum` | no | `"sha256:<hex>"`, `"md5:<hex>"`, `"sha1:<hex>"`, `"sha512:<hex>"`, or `"<algo>:auto"` |
| `checksum_url` | no | Explicit URL for the checksum file (overrides auto patterns) |
| `checksum_file_format` | no | `"sha256sum"` (default), `"bsd"`, or `"raw"` |
| `signature_url` | no | GPG detached signature URL, for verifying the checksum file |
| `signing_key` | no | GPG key URL or fingerprint |
| `extract_to` | no | Extraction destination (default: `/usr/local/bin`) |
| `strip_components` | no | Remove this many leading archive path components. Applies equally to tar and zip; negative or empty results are rejected. |
| `entrypoints` | no | Map stable command names to relative files inside `extract_to`, e.g. `{ nvim = "bin/nvim" }`. |
| `link_dir` | no | Launcher directory. Defaults to `~/.local/bin` for user payloads and `/usr/local/bin` for system payloads. |
| `binary` | no | Installed filename for a direct asset, or payload name used by check/remove |
| `scope` | no | Portable installation scope: `"user"` (default for user placement under XDG) or `"system"`. When set, supplies platform-native install and link defaults (`~/.local/share/depengine/tools/<tool>` + `~/.local/bin` for user scope, `/opt/depengine/tools/<tool>` + `/usr/local/bin` for system scope) without needing Unix paths in manifests. Explicit `extract_to`/`link_dir` override independently. |
| `sudo_required` | no | Boolean, default is **path-derived**: `false` when `extract_to` is inside the user's home (e.g. `~/.local/share/fonts`), `true` for system paths (e.g. the `/usr/local/bin` default). Set explicitly to override. |
Archives are extracted into private staging, validated, then committed as one
owned payload. `Check` verifies payload files and launchers directly; `Remove`
deletes only declared launchers and the owned payload. `.msi`, `.exe`, `.pkg`
and `.dmg` are rejected by `http` instead of being mistaken for binaries.

Do not embed credentials in HTTP(S) URLs (for example,
`https://token@example.com/file`). depengine rejects credential-bearing URLs so
secrets cannot leak through command arguments, diagnostics, state, or lock data.
For GitHub authentication, use `GITHUB_TOKEN`, `GH_TOKEN`, or an authenticated
GitHub CLI; authenticated asset downloads use the in-process HTTP backend so the
token is sent as an `Authorization` header rather than as a subprocess argument.


### Local/offline: install a vendored artifact

Use `local` when the payload lives inside the project rather than behind a
network URL:

```toml
[tools.mytool.local]
local_path  = "vendor/mytool"
checksum    = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
install_dir = "~/.local/bin"
```

`local_path` is required and is resolved against the directory containing the
project `schema.toml`, including when the method configuration originated in a
personal manifest layer. The portable plan/lock identity keeps only the
normalized project-relative path and checksum; the machine-specific absolute
project root is runtime metadata and is not persisted. Absolute paths, `..`,
backslashes, Windows drive prefixes, and any symlink component are rejected.

| Field | Required | Description |
|-------|----------|-------------|
| `local_path` | yes | Project-relative vendored file. Raw files and `.zip`, `.tar`, `.tar.gz`, `.tgz` are supported offline. Platform installers and archive formats requiring external extraction backends are rejected. |
| `checksum` | no | Fixed `sha256:<64 hex>` digest. It is checked while resolving and rechecked immediately before materialization. `:auto` is not supported for local artifacts. |
| `install_dir` | no | Parent directory. The adapter owns only the child named after the tool. Defaults to `~/.local/bin` for raw files and `~/.local/opt` for archives. |

A raw file becomes `install_dir/<tool>`. An archive is extracted into
`install_dir/<tool>`, not directly into the shared parent. `remove` deletes only
that owned child. Archive installs reserve `.depengine-local-artifact.sha256`
at the archive root as a transactional provenance marker; an archive containing
that path is rejected. `Check` uses the marker to ensure the installed directory
came from the currently resolved vendored bytes. The adapter uses only Go's
standard-library filesystem and archive support; it does not perform network I/O
or invoke a subprocess. Archive recipes currently do not create PATH launchers
automatically, so choose an `install_dir`/payload layout appropriate for how the
tool will be invoked.

### GitHub release assets

Some projects publish a different asset filename convention per architecture
(`amd64` vs `x86_64` vs `x64`, `arm64` vs `aarch64`, ...). Writing one `http`
method per spelling can require several near-identical blocks for a single
tool, one for each naming convention used by the upstream project.

`github` takes a repo and an asset filename **pattern** instead of a fixed
URL, resolves the latest release via the GitHub API, and matches the pattern
against the release's asset names. This avoids enumerating architecture
naming conventions in separate methods:

```toml
[tools.yq.github]
repo  = "mikefarah/yq"
asset = "yq_{os_any}_{arch_any}"

[tools.node_exporter.github]
repo  = "prometheus/node_exporter"      # "owner/repo", or a full github.com URL
asset = "node_exporter-{version}.linux-{arch_any}.tar.gz"
checksum   = "sha256:auto"
extract_to = "~/.local/bin"
strip_components = 1
entrypoints = { node_exporter = "node_exporter" }
link_dir = "~/.local/bin"
```

This single block replaces separate `http`/`http-arm64`/`http-armv7` blocks
for the same tool.

The pattern must match exactly one asset. Zero matches report every available
asset; multiple matches fail and list the ambiguous matches. depengine never
chooses the first result heuristically.

| Field | Required | Description |
|-------|----------|--------------|
| `repo` | yes | `"owner/repo"` (or a full `https://github.com/owner/repo` URL) |
| `asset` | yes | Filename pattern matched against the release's real asset names (see placeholders below) |
| `release` | no | Named release tag to resolve instead of the latest release (e.g. `"nightly"` for a project's rolling pre-release). Defaults to `"latest"`. Mutually exclusive with `branch`. |
| `branch` | no | Literal branch name, for projects that tag a release identically to a branch (e.g. an `"unstable"` rolling build). **Does not query git branches/commits** — it resolves through the same GitHub release-by-tag API as `release`, while recording branch intent. Mutually exclusive with `release`. |

Every other field (`checksum`, `checksum_url`, `checksum_file_format`,
`signature_url`, `signing_key`, `extract_to`, `binary`, `sudo_required`,
`strip_components`, `entrypoints`, `link_dir`) has
the exact same meaning as on `http` — once the asset is resolved, `github`
downloads/verifies/extracts it exactly like `http` would.
For a direct asset, `binary` is the installed filename. For an archive,
`binary` retains its existing payload-check meaning; use `entrypoints` for
stable commands whose files live below the archive root.

Latest releases—implicit or written as `release = "latest"`—are pinned in
`depengine.lock`; installation then resolves the asset only within that pinned
release. Explicit `release` and `branch` values are left unchanged. The method
name is canonically `github`, appears immediately before `http` in the default
order, and has no global `gh` alias. This order only ranks declared candidates;
it does not inject `github` automatically. A label such as `[tools.foo.gh]` is
valid only with `kind = "github"`.

`asset` supports three placeholders resolved by GitHub release asset matching
(they are not part of the regular placeholder table below, and are never
expanded to a single fixed value ahead of time — see the note at the end of
the [Placeholders](#placeholders) section):

- **`{arch_any}`** — matches any known spelling of the machine's
  architecture (e.g. on `x86_64`, also matches `amd64` or `x64` in the asset
  name).
- **`{os_any}`** — matches any known spelling of the machine's OS (e.g. on
  `darwin`, also matches `macos` or `osx`).
- **`{version}`** — the resolved release tag, matched with or without a
  leading `v` (covers both `v1.2.3` tags and `1.2.3`-named assets).

If no asset in the release matches the pattern, `depengine install` fails
with an error listing the assets found in that release. Adjust `asset` to a
matching pattern; depengine does not silently select a non-matching file.

---

### Container: pull an image via docker/podman

`container` pulls an image into the local image store. It does not create a
PATH shim or modify shell configuration. Use it for images invoked through
`docker run` or `podman run`, not for commands expected to exist directly on
PATH.

```toml
[tools.obsidian.container]
manager = "podman"
source  = "lscr.io/linuxserver/obsidian"
tag     = "latest"
```

| Field | Required | Description |
|-------|----------|--------------|
| `manager` | yes | `"docker"` or `"podman"` — picks which binary to drive. Not auto-detected: both can be installed on the same host at once, so guessing which one a given tool wants isn't safe. |
| `source` | yes | The image reference (registry/repo), without the tag. |
| `tag` | no | Defaults to `"latest"`. |
| `digest` | no | Immutable `sha256:<64 hex>` identity; mutually exclusive with `tag`. |
| `platform` | no | OCI platform selector `os/arch[/variant]`, e.g. `linux/amd64` or `linux/arm64/v8`. Applied to pull and verified from image metadata. |

`source` is the repository only: tags and digests belong to separate resolved identity fields and are rejected when embedded in `source`. Registry ports such as `registry.example:5000/team/tool` remain valid. Invalid tag syntax is rejected before invoking Docker/Podman.

`Check` looks at `<manager> images -q <source>:<tag>` — non-empty output
means the image is already pulled. `Remove` runs `<manager> rmi
<source>:<tag>`.

---

### AppImage: portable `.AppImage` binaries

`appimage` resolves and downloads a `.AppImage` artifact using the same
artifact-source, checksum, signature, retry, and cache semantics as `http`,
then does the AppImage-specific
part `http` doesn't: installing under a **stable** name instead of the
versioned filename the release asset ships with (e.g.
`Obsidian-1.5.3.AppImage` → `obsidian`), and optionally writing a
`.desktop` launcher.

```toml
[tools.obsidian.appimage]
repo    = "obsidianmd/obsidian-releases"
asset   = "Obsidian-{version}.AppImage"
desktop = true
# install_dir defaults to ~/.local/bin (user-scope)
```

| Field | Required | Description |
|-------|----------|--------------|
| `url` or `repo` + `asset` | yes | Exactly one artifact source. `repo` + `asset` supports `{version}`, `{arch_any}` and `{os_any}` and is pinned in `depengine.lock`. |
| `install_dir` | no | Destination directory. Defaults to `~/.local/bin` (user-scope). There is no separate `system = true` boolean — pointing this at a system path (e.g. `/usr/local/bin`) is how a system-wide install is requested, and `sudo_required` is derived from the path the same way `http` derives it from `extract_to`. |
| `binary` | no | Final executable name. Defaults to the tool's name. |
| `scope` | no | Portable installation scope: `"user"` (default) or `"system"`. When set, supplies platform-native install root defaults (`~/.local/share/depengine/tools/<tool>` for user scope) while the adapter creates a PATH link in `~/.local/bin`. |
| `desktop` | no | When `true`, also writes `~/.local/share/applications/<binary>.desktop` (a minimal, valid launcher pointing at the installed binary). Always user-scope, regardless of `install_dir`. |
The artifact source must resolve to a filename ending in `.AppImage`; other formats are rejected during validation and again at the adapter boundary.

Shared artifact fields such as `checksum`, `checksum_url`, `signature_url`,
`signing_key`, and `sudo_required` keep the same meaning because the download
is delegated to the `http` adapter. `extract_to` is intentionally not accepted:
`install_dir` is the AppImage destination contract and accepting both would make
`extract_to` appear configurable while the adapter overrides it.

`Check` looks for `install_dir/<binary>` — the resolved *stable* name, not
the downloaded filename. `Remove` deletes that file and, if `desktop` was
set, its `.desktop` entry.

---

### Android: hand a `.apk` to Termux's package installer

`android` resolves and downloads a `.apk` artifact with the same artifact-source,
checksum, signature, retry, and cache semantics as `http`/`appimage`, then does
the one thing neither of those can:
hand the file to Android's own package installer via `termux-open` (from
the `termux-api` package — needs the companion **Termux:API** app installed
too). Runs entirely inside [Termux](https://termux.dev/); see
[`is_android`](#platform-targeting) for how to gate a candidate to it.

```toml
[tools.obsidian.android]
repo = "obsidianmd/obsidian-releases"
asset = "obsidian-{version}-android.apk"
when = { is_android = true }
```

| Field | Required | Description |
|-------|----------|--------------|
| `url` or `repo` + `asset` | yes | Exactly one artifact source; GitHub matching composes with Android dispatch. |

The artifact source must resolve to a filename ending in `.apk`; other formats are rejected before download/dispatch.

Shared transport/integrity fields (`checksum`, `checksum_url`,
`signature_url`, `signing_key`, ...) keep the same meaning. There is no
`install_dir`/`binary`/`extract_to` field here, unlike `appimage`, and
archive-only placement fields (`strip_components`, `entrypoints`,
`link_dir`) are rejected. The `.apk` always lands under a fixed,
depengine-owned cache directory named `<tool>.apk`; it's never meant to end
up on `PATH`.

**What "installed" means here:** success means "handed to the Android
package installer", not "installed" — a human still has to tap through the
installer's prompt, asynchronously and outside depengine's process.
`Check` can only confirm the `.apk` was downloaded and dispatched before
(same file-existence logic as `appimage`), never that the app is actually
present on the system — Termux has no reliable `pm list packages` without
root/adb. There is no automated `Remove`, for the same reason: deleting the
cached `.apk` would not uninstall the app and would misleadingly suggest it
did.

---

## Method reference

Manager-specific options are typed instead of passed through as arbitrary
arguments. WinGet supports `version`, `source`, `scope`, `architecture`, and an
allow-listed `installer_type`. Scoop supports `version`, `bucket`, `scope`, and
`architecture`. Chocolatey supports `version`, `prerelease`, `source`, and
`architecture`. Snap supports confinement and structured channel selection.

This keeps validation and non-interactive/safety flags under depengine's
control.

`git` custom installs may declare `managed_paths = ["/absolute/path", ...]`.
All paths must be absolute after `~` expansion. Filesystem roots, the whole
home directory, and broad shared directories are rejected. `Check` requires
every path and `Remove` deletes only those exact targets.

`container` keeps the repository in `source` and accepts either `tag` (default `latest`) or an immutable `digest`. `tag` and `digest` are mutually exclusive; digests currently use `sha256:<64 hex characters>`. Optional `platform = "os/arch[/variant]"` is passed to the pull operation and verified against local image metadata.

`flatpak` no longer assumes Flathub. `remote` selects and verifies the origin, `branch` is appended to the application ref (`APP//BRANCH`), and `scope = "user" | "system"` is applied consistently to check/install/remove. If `remote` is omitted, depengine leaves remote resolution to Flatpak rather than silently forcing `flathub`.

`snap` keeps `channel = "edge"`-style shorthand for risk-only selection. For full channel identity, use `track`, optional `risk` (default `stable`), and optional `branch`; depengine composes `track/risk[/branch]` and verifies the installed tracking channel from `snap list`. `channel` cannot be combined with the structured fields.


`msi` accepts exactly one artifact source (`url`, or `repo` + `asset`) plus
required `product_name` and optional `publisher`. Registry matching is exact;
install/remove use quiet `msiexec` operations, and reboot-required exit codes
are treated as successful installs.

One-line syntax for every supported method. All accept `when` (see
[Platform targeting](#platform-targeting)); methods with richer configuration
are described above or alongside their examples.

| Method | What it installs | Example |
|--------|-------------------|---------|
| `native` | Auto-detected distro manager (apt/pacman/dnf/brew/...) | `fd = { apt = "fd-find" }` |
| `cargo` | Cargo crates; exact `version`, named `registry`, or Git source with `branch`/`tag`/`rev` | `ripgrep = { cargo = { pkg = "ripgrep", registry = "corp", version = "14.1.1" } }` |
| `go` | Go package path via `go install`; exact `version` avoids `@latest` | `stringer = { go = { pkg = "golang.org/x/tools/cmd/stringer", version = "v0.36.0" } }` |
| `pip` | Python packages; optional exact `version`, verified with `pip show` | `ruff = { pip = { pkg = "ruff", version = "0.13.1" } }` |
| `pipx` | Python CLI tools, isolated environments | `organize = { pipx = "organize-tool" }` |
| `uv` | Python packages via `uv tool` | `organize = { uv = "organize-tool" }` |
| `npm` | Global npm packages; optional exact `version`, verified from `npm ls --json` | `typescript = { npm = { pkg = "typescript", version = "5.9.2" } }` |
| `pnpm` | Global pnpm packages | `prettier = { pnpm = "prettier" }` |
| `bun` | Global bun packages | `tsx = { bun = "tsx" }` |
| `gem` | Ruby gems | `sass = { gem = "sass" }` |
| `yarn` | Global yarn packages | `typescript = { yarn = "typescript" }` |
| `yarn-berry` | Yarn Berry (v2+) global packages | `typescript = { yarn-berry = "typescript" }` |
| `composer` | Global PHP Composer packages | `php-cs-fixer = { composer = "friendsofphp/php-cs-fixer" }` |
| `apm` | Atom package manager (legacy) | `atom-beautify = { apm = "atom-beautify" }` |
| `vscode` | VS Code extensions | `golang = { vscode = "golang.go" }` |
| `vscodium` | VSCodium extensions | `golang = { vscodium = "golang.go" }` |
| `flatpak` | Flatpak apps; optional `remote`, `branch`, and `scope` (`user` or `system`) | `spotify = { flatpak = { pkg = "com.spotify.Client", remote = "flathub", branch = "stable", scope = "user" } }` |
| `snap` | Snap packages; risk shorthand or structured `track`/`risk`/`branch` | `hello = { snap = { pkg = "hello", track = "2.0", risk = "stable" } }` |
| `cask` | macOS Homebrew casks | `docker = { cask = "docker" }` |
| `mas` | Mac App Store, by app ID | `xcode = { mas = "497799835" }` |
| `appman` | AppImage packages via "AM"/"AppMan" (ivan-hc/AM) | `obsidian = { appman = "obsidian" }` |
| `container` | Container images via `docker`/`podman pull`; `tag` or immutable `digest`, optional OCI `platform` | `redis = { container = { manager = "podman", source = "redis", platform = "linux/amd64" } }` |
| `appimage` | Portable `.AppImage` binaries, installed under a stable name | `obsidian = { appimage = { url = "https://…/Obsidian-{latest}.AppImage" } }` |
| `android` | Download a `.apk` and hand it to Termux's package installer | `obsidian = { android = { url = "https://…/obsidian-{latest}-android.apk" }, when = { is_android = true } }` |
| `msi` | Install/remove a Windows Installer product by exact registry identity | `nvim = { msi = { repo = "neovim/neovim", asset = "nvim-win64.msi", product_name = "Neovim" } }` |
| `sdkman` | SDKMAN! JVM SDKs | `java17 = { sdkman = "java" }` |
| `steamcmd` | SteamCMD game server tools | `cs2 = { steamcmd = "730" }` |
| `pacstall` | Pacstall packages (Debian-based AUR-like) | `neofetch = { pacstall = "neofetch" }` |
| `aur` | Arch User Repository (via configured `aur_helper`) | `google-chrome = { aur = "google-chrome" }` |
| `winget` | Windows Package Manager; typed `version`, `source`, `scope`, `architecture`, and `installer_type` | `git = { winget = { pkg = "Git.Git", version = "2.53.0", source = "winget", scope = "machine" } }` |
| `scoop` | Windows via Scoop; optional exact `version`, `bucket`, `scope` (`user` or `global`), and architecture (`32bit`, `64bit`, `arm64`) | `git = { scoop = { pkg = "git", bucket = "main", version = "2.53.0.2" } }` |
| `choco` | Windows, via Chocolatey; exact `version`, `prerelease`, typed `source`, and `architecture` | `firefox = { choco = { pkg = "firefox", version = "128.0.0", source = "https://community.chocolatey.org/api/v2/" } }` |
| `conda` | Conda packages; deterministic `environment`/`prefix`, exact `version`/`build`, ordered `channels` | `numpy = { conda = { pkg = "numpy", environment = "data", version = "2.1.0", channels = ["conda-forge"] } }` |
| `asdf` | asdf version manager plugins | `nodejs = { asdf = "nodejs" }` |
| `git` | Clone + build (see field table above) | `ctpv = { git = { url = "...", build = "make install" } }` |
| `local` | Install a project-vendored raw file or stdlib-supported archive without network access | `mytool = { local = { local_path = "vendor/mytool", checksum = "sha256:..." } }` |
| `github` | Recommended for GitHub Releases; matches an asset *pattern* against the real asset list (see above) | `yq = { github = { repo = "mikefarah/yq", asset = "yq_{os_any}_{arch_any}" } }` |
| `http` | Download + extract + checksum (see field table above) | `fastfetch = { http = { url = "...", checksum = "sha256:auto" } }` |

### Ecosystem desired-state coverage

Ecosystem methods do not all expose the same controls. `pip`, `npm`, `pipx`,
`uv`, `gem`, `composer`, `bun`, `pnpm`, and Yarn Classic support exact version
intent and check the installed version through the package manager. Source or
registry selection is available where the adapter has a typed field.

These methods are not yet universally lockable to immutable package identities.
Authenticated registries must use the package manager's authentication
mechanism rather than credentials embedded in URLs.

---

## Hooks & dependencies

### Pre-install hook (before any method)

```toml
[tools.myenv]
pre_install = "curl -fsSL https://setup.example.com | sh"

  [tools.myenv.native]
  pkg = "my-env"
```

`pre_install` runs before installation. If it fails, the tool stops. Hooks
require `--allow-arbitrary-code`.

The string form is a POSIX shorthand and executes as `sh -c <command>`. For
portable or shell-independent hooks, declare the executable and arguments
explicitly with `run`:

```toml
pre_install = { run = ["curl", "-fsSLo", "tool.zip", "https://example.com/tool.zip"] }
```

`run` never inserts a shell or interprets pipes, redirects, globbing, or
variable expansion. Invoke the desired interpreter explicitly when those
features are required. A list can provide platform-specific variants or
multiple sequential steps:

```toml
pre_install = [
  { run = ["sh", "-c", "curl -fsSL \"$URL\" | tar -xz"], when = { target_family = ["unix"] } },
  { run = ["pwsh.exe", "-NoProfile", "-NonInteractive", "-Command", "Invoke-WebRequest $env:URL -OutFile tool.zip"], when = { target_family = ["windows"] } },
]
```

The shell-command form `{ cmd = "...", when = {...} }` explicitly means
`sh -c`; it is a current schema form, not a version-compatibility parser. Prefer
`run` because it preserves argv boundaries. PowerShell (`pwsh.exe`),
Windows PowerShell (`powershell.exe`), `cmd.exe`, Nushell, or any other
interpreter can be selected explicitly through `run`.

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

Candidate-only prerequisites are declared inside the method. They run lazily
only if that candidate is reached, and a failure discards that candidate while
allowing fallback:

```toml
[tools.software-properties-common]
dependency_only = true
apt = "software-properties-common"

[tools.nvim.ppa]
kind = "native"
pkg = "neovim"
requires = ["software-properties-common"]
sources = [{ kind = "apt-ppa", name = "ppa:neovim-ppa/stable" }]
```

Supported source kinds are `apt-ppa`, `dnf-copr`, `scoop-bucket`, and
`brew-tap`. `url` is supported only for `scoop-bucket` and `brew-tap`;
`apt-ppa` and `dnf-copr` identify their repositories by `name`. A source may
declare
`secret_ref = { provider = "env", name = "CORP_TOKEN" }` to identify external
credentials without storing their value in the schema or plan. Authentication
is required for that candidate; methods without an auth capability reject it
during planning. Runtime secret resolution is not yet implemented. Sources are
checked before mutation. `dependency_only` tools are not normal roots, but
remain selectable with `--only`.

### Virtual tools: dependency groups with no methods

A tool may contain only `requires`, with no install methods:

```toml
[tools.devenv]
requires = ["zsh", "tmux", "fzf"]
```

Such a tool is a dependency group. Installing it installs its requirements and
then reports the group as virtual; it does not invent or execute an install
candidate of its own.

`post_install` accepts the same string, table, and list forms, including
per-command `when` conditions:

```toml
post_install = { run = ["fc-cache", "-fv"], when = { target_family = ["unix"] } }
```

The plain-string form stays valid and is unconditional.

---

## Platform targeting

### Complex case: multiple methods + distro condition

```toml
[tools.DepartureMono]
post_install = { cmd = "fc-cache -fv", when = { target_family = ["unix"] } }

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

> **Field ownership:** tool-level fields (`requires`, `post_install`,
> `pre_install`) go _outside_ the method block. Method-specific fields
> (`kind`, `when`, `requires`, `sources`, `url`, `build`, `checksum`, `extract_to`, `pkg`, `git`) go
> _inside_.

### Platform conditions (`when`), multi-dimension gating

A method's `when` clause can specify **multiple platform dimensions**. The
engine evaluates all non-empty fields against the detected system facts:

- **AND between fields** — when `arch` + `libc` + `os` are specified, all three
  must match.
- **OR within a field** — `arch = ["x86_64", "aarch64"]` is satisfied by
  either.
- **Empty fields are ignored** — a condition with only `arch` set doesn't
  require libc matching.
- **Nil / absent `when` always matches.**
- **`distro_family`** is the resolved *clan* (e.g. Ubuntu → `debian`), not
  the raw distro ID — use `distro_id` for exact-distro matching.

| Field | Type | Comparison | Example values |
|-------|------|------------|-----------------|
| `distro_family` | `string[]` | Exact (case-insensitive) | `arch`, `debian`, `fedora`, `alpine`, `gentoo`, `macos`, `freebsd`... |
| `distro_id` | `string[]` | Exact (case-insensitive) | `ubuntu`, `arch`, `fedora`, `debian`, `alpine`... |
| `distro_version` | `string[]` | Exact normalized version | `22.04`, `24.04`, `14.6.1` |
| `distro_version_min` | `string` | Inclusive minimum | `22.04` |
| `distro_version_max` | `string` | Inclusive maximum | `24.04` |
| `arch` | `string[]` | Exact (case-insensitive) | `x86_64`, `aarch64`, `armv7l`... |
| `os` | `string[]` | Exact (case-insensitive) | `linux`, `darwin`, `windows`, `freebsd`, `openbsd`, `netbsd` |
| `target_family` | `string[]` | Exact (case-insensitive) | `unix` (linux, darwin, BSDs, termux), `windows` |
| `kernel` | `string[]` | Exact (case-insensitive) | `6.7.0-arch`, `5.15.0-generic`... |
| `libc` | `string[]` | **Prefix** match | `glibc` matches `glibc 2.35`; `musl` for Alpine |
| `init_system` | `string[]` | Exact (case-insensitive) | `systemd`, `openrc`, `runit`, `sysvinit` |
| `is_wsl` | `bool` | Three-state | Detected via `/proc/version` or `WSL_DISTRO_NAME` |
| `is_container` | `bool` | Three-state | Detected via `.dockerenv`, cgroup, etc. |
| `is_android` | `bool` | Three-state | Detected via Termux env vars. Needed because `os` reports `linux` on Termux — `os = ["android"]` never matches there |

Version conditions compare numeric and textual runs rather than applying SemVer rules, because OS release identifiers are not uniformly SemVer. Numeric runs ignore leading zeroes (`24.04` equals `24.4`); minimum and maximum bounds are inclusive. A host with no detected distro version does not satisfy a version condition.

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
# Termux-specific asset — `os = ["android"]` won't match here, use is_android.
# The .apk's filename is a direct function of the release tag (no "v" prefix
# mismatch — see the {version} caveat under the Android method above), so a
# plain `url` with {latest} is enough; no `github` candidate needed.
[tools.obsidian]
  [tools.obsidian.android]
  url  = "https://github.com/obsidianmd/obsidian-releases/releases/download/{latest}/obsidian-{latest}-android.apk"
  when = { is_android = true }

  [tools.obsidian.gh_linux]
  kind = "github"
  repo = "obsidianmd/obsidian-releases"
  asset = "Obsidian-{version}.AppImage"
  when = { os = ["linux"] }
```

```toml
# Rolling/unstable build published as a release tagged like the branch
[tools.somefork]
  [tools.somefork.gh_unstable]
  kind   = "github"
  repo   = "someorg/somefork"
  branch = "unstable"   # literal tag name, no ambiguity with "use releases"
  asset  = "somefork-linux-{arch_any}"
```

```toml
# Kernel-specific DKMS package
[tools.v4l2loopback]
  [tools.v4l2loopback.aur]
  pkg  = "v4l2loopback-dkms"
  when = { distro_family = ["arch"], kernel = ["6.7", "6.8", "6.9"] }
```

> Run `depengine why <tool>` to inspect which method applies on the
> current machine and why the others were skipped. Inferred native fallback
> candidates are identified explicitly, and candidates excluded by `method_only`
> are reported as policy exclusions rather than as unavailable methods.
>
> **Native fallback rule:** shorthand scalar/bool declarations are convenience
> syntax and implicitly include a native fallback. For example,
> `fzf = { go = "github.com/junegunn/fzf" }` yields `native` + `go` candidates.
> An explicit method table means exactly what it declares: `[tools.fzf.go]` does
> **not** inject a hidden native candidate. Declare `native` explicitly when a
> full-table tool should retain native fallback. Native-manager shorthand such
> as `apt = "fd-find"` or `brew = "ripgrep"` is itself explicit native intent.

---

## Per-tool method control

`[defaults].method_prefer` and tool-level `method_prefer` are priority prefixes:
unlisted methods remain available as fallbacks. The older
`[defaults].method_order` name is still accepted as a compatibility alias; do
not set both. Use `method_only` when fallback must be disabled.

Override the preference for a single tool with `method_prefer` (prefix) or
`method_only` (exclusive list):

```toml
# Try cargo first, fall back to the default order:
myapp = { method_prefer = ["cargo"], cargo = true }

# Only use these methods, in this order — no fallback:
legacy = { method_only = ["aur", "git"], aur = { pkg = "legacy" }, git = { url = "..." } }
```

- **`method_prefer`** prepends the listed methods before the global
  `method_prefer`; methods not listed are still tried as fallbacks.
- **`method_only`** restricts the tool to exactly these methods, in this
  order — the global `method_prefer` is ignored for this tool.
- Both live at tool level (same level as `requires`, `tags`, `post_install`),
  not inside a method block.

`kind` selects the adapter. A candidate subtable name selects that same kind
only when it is a known method name; otherwise it is a label and the subtable
must set `kind` explicitly. Order selectors match either a kind or an exact
candidate label. TOML declaration order is never an order control, and
`[defaults].method_prefer` plus per-tool `method_prefer` are prefixes: unlisted default
methods remain available. Only `method_only` removes the remainder.

---

## Placeholders

Placeholders are `{name}` tokens. Platform-fact placeholders are expanded in
string fields before installation; adapter-owned placeholders are interpreted
later by the adapter that owns their field.

- **Unknown name** — a typo like `{archh}` isn't in the table below, so it's
  left untouched by `Expand` and flagged as `W_UNKNOWN_PLACEHOLDER`. This
  also catches typos outside the `{lowercase_snake_case}` charset, such as
  `{Arch}`, `{ARCH}`, or `{arch-name}` — placeholder names are matched
  case-sensitively, so any of those are "unknown" even though `{arch}` is
  valid.
- **Wrong owner or field** — adapter-owned tokens only have meaning in the
  locations shown below. In particular, asset-matching tokens in a literal
  `url` are not generic substitutions and remain unresolved.

| Placeholder | Source | Valid for | Example |
|-------------|--------|-----------|---------|
| `{arch}` | `detect_os.sh` | any method | `x86_64`, `aarch64` |
| `{os}` | `detect_os.sh` | any method | `linux`, `darwin` |
| `{distro_family}` | Resolved clan | any method | `debian`, `arch`, `fedora` |
| `{id}` | `detect_os.sh` | any method | `ubuntu`, `arch` |
| `{distro_name}` | `detect_os.sh` | any method | `Ubuntu 24.04 LTS` |
| `{distro_version}` | `detect_os.sh` | any method | `24.04`, `14.6.1` |
| `{distro_id_like}` | `detect_os.sh` | any method | `debian` |
| `{target_family}` | `detect_os.sh` | any method | `unix`, `windows` |
| `{kernel}` | `detect_os.sh` | any method | `5.15.0` |
| `{libc}` | `detect_os.sh` | any method | `glibc`, `musl` |
| `{init_system}` | `detect_os.sh` | any method | `systemd`, `openrc` |
| `{detection}` | `detect_os.sh` | any method | `os-release` |
| `{confidence}` | `detect_os.sh` | any method | `high`, `medium` |
| `{is_wsl}` / `{is_container}` / `{is_android}` | `detect_os.sh` | any method | `true`, `false` |
| `{pkg}` | Adapter-owned — substituted at install time | **`native` only** (or a manager name used directly as `kind`, e.g. `kind = "apt"`) | package name |
| `{latest}` | Adapter-owned — resolved via GitHub API | Literal `url` in `git`, `http`, `appimage`, `android`, or `msi` | `v1.2.3` |
| `{version}` | Adapter-owned — matched against a resolved GitHub release tag | `asset` in `github`, `http`, `appimage`, `android`, or `msi` when using `repo` + `asset` | matches `v1.2.3` or `1.2.3` |
| `{arch_any}` | Adapter-owned — matched (not substituted) against real asset names | Same `repo` + `asset` methods, `asset` field only | matches `x86_64`/`amd64`/`x64`, etc. |
| `{os_any}` | Adapter-owned — matched (not substituted) against real asset names | Same `repo` + `asset` methods, `asset` field only | matches `darwin`/`macos`/`osx`, etc. |

The `detect_os.sh`-sourced placeholders are expanded for every method kind.
The remaining tokens are field-specific: `{pkg}` belongs to native commands,
`{latest}` belongs to literal artifact URLs, and `{version}`/`{arch_any}`/
`{os_any}` belong only to GitHub release `asset` matching.

```toml
# {arch}/{os} expand from detect_os.sh before installation:
fastfetch = { http = { url = "https://example.com/{os}/{arch}/fastfetch.deb" } }

# {latest} is resolved in a literal artifact URL via GitHub's API:
fastfetch = { http = { url = "https://github.com/fastfetch-cli/fastfetch/releases/download/{latest}/fastfetch-linux-amd64.deb" } }

# Matching placeholders belong to repo+asset, never a literal URL:
yq = { github = { repo = "mikefarah/yq", asset = "yq_{os_any}_{arch_any}" } }

# WRONG: {pkg} on an http method is never substituted — it ships as a
# literal "{pkg}" in the URL. {pkg} only works on a native method:
# broken:  sometool = { http = { url = "https://example.com/{pkg}.deb" } }
# correct: sometool = { apt = "sometool-bin" }  # native, {pkg} substituted internally
```

---

## Manifest merge rules

See [the README](../README.md#schematoml-and-manifesttoml) for an overview
of the two layers.

`schema.toml` and `~/.config/depengine/manifest.toml` merge by field:

1. Most scalar/list fields: the schema wins when both files set a value.
2. Maps such as `pkg_overrides`: keys are combined; the schema wins on a
   duplicate key.
3. Sets such as `tags`: values are combined without duplicates.
4. `pre_install`, `post_install`, `requires`, `method_prefer`, and
   `method_only`: a schema value replaces the manifest value instead of
   merging with it.
5. Manifest-only tools are ignored unless the manifest sets
   `[manifest] allow_new_tools = true`.

Run `depengine why <tool>` to see candidate status together with normalized
non-secret identity fields (for example version/revision, registry/source,
scope, environment/prefix, and architecture). Add `--fields` to see exactly
which layer contributed each field for a given tool; `--json` exposes identity
metadata under `intent`.
