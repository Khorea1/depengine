<h1 align="center">depengine</h1>

<p align="center">
  <b>Distro-agnostic dependency installer</b><br>
  Declare <i>what</i> to install — the engine figures out <i>how</i>.
</p>

<p align="center">
  <a href="https://github.com/Khorea1/depengine/actions"><img src="https://github.com/Khorea1/depengine/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-GPL--3.0--or--later-blue" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.26-blue" alt="Go 1.26">
</p>

---

Write a `schema.toml` listing your tools. depengine tries every available
installation method — native package manager, cargo, go, pip, git, http,
flatpak, and more — until one succeeds. Single static Go binary, no runtime
dependencies.

```sh
depengine init --add "zsh,bat,nvim,ruff"   # → creates schema.toml
depengine validate                          # → ✓ schema is valid
depengine install                           # → 4 installed, 0 failed
depengine status                            # → list what's installed
```

> **Platform support:** Linux and macOS are the primary targets and are
> battle-tested. Windows builds are provided (winget/scoop/choco), including
> full file locking and state management, but are newer and less
> battle-tested than Linux/macOS.

## Documentation map

This README covers the essentials. For everything else:

| Document | What's in it |
|----------|---------------|
| [`docs/schema-reference.md`](docs/schema-reference.md) | Every `schema.toml` syntax case, `when` conditions, buckets, method control — the full spec |
| [`docs/cli-reference.md`](docs/cli-reference.md) | Every command and flag, with defaults |
| [`docs/cheatsheet.md`](docs/cheatsheet.md) | One-page copy-paste reference: commands, flags, placeholders |
| [`docs/architecture.md`](docs/architecture.md) | Internal package layout and install flow — for contributors |
| [`docs/depengine.1`](docs/depengine.1) | Man page (`depengine help --man`, or `man depengine` if installed) |
| [`schema/depengine.schema.json`](schema/depengine.schema.json) | JSON Schema for editor autocomplete (taplo, VSCode) |

---

## Your first `schema.toml`

A schema describes **tools** (what you want) and, per tool, **methods** (how
to get it). The engine tries methods in order until one succeeds.

```toml
# schema.toml

# Package name is the same everywhere — the common case.
simple = ["zsh", "bat", "kitty", "mpv"]

# Package name differs per distro's native manager.
fd = { apt = "fd-find" }   # "fd" everywhere else

# A tool only available through a language ecosystem.
ruff = { python = true }   # expands to pip + pipx + uv, pkg name = "ruff"
```

```sh
depengine validate     # check the schema before touching your system
depengine install      # try every tool's methods in order
depengine status       # see what actually got installed, and how
```

That covers the majority of real-world schemas. When you need something more
specific — a git build, an HTTP download with checksum, a platform-specific
condition, a tool-to-tool dependency — see the
**[full schema reference](docs/schema-reference.md)**, which walks through
all eleven syntax cases with real examples.

---

## The sharing workflow

depengine works like a `requirements.txt` or `package.json`, but for system
tools. Write `schema.toml`, commit it, and everyone on the team gets the same
tools.

```mermaid
flowchart LR
    A[Write schema.toml] --> B[depengine install]
    B --> C[depengine status ✓]
    D[git clone] --> E[depengine install]
```

```sh
# --- Project author ---
depengine init --add "zsh,bat,nvim,ruff"
./depengine validate
./depengine install
git add schema.toml depengine.lock && git commit

# --- Everyone else ---
git clone <project> && cd <project>
depengine install                    # same tools, same versions

# --- Optional: pin exact versions ---
./depengine update                   # resolves {latest}, writes depengine.lock
./depengine update --dry-run         # preview what would be pinned
./depengine update --frozen-lockfile # abort if depengine.lock doesn't exist
./depengine install --frozen-lockfile

# --- Everyday commands ---
./depengine status                   # what's installed (positional args are ignored)
./depengine status --json            # same, machine-readable
./depengine remove nvim              # uninstall
./depengine why nvim                 # explain which method would run, and why
./depengine sbom --format cyclonedx  # export an SBOM
```

> **Note on filenames:** `depengine init` creates `schema.toml` by default.
> `depengine.toml` and `depends.toml` are also auto-detected if present
> (useful if you're migrating from another tool or just prefer the name),
> but `schema.toml` is the recommended default — that's what every example
> in this repo uses.

---

## `schema.toml` vs `manifest.toml` — two layers, one merged config

depengine merges two layers per field: the **project schema** and your
**personal manifest**. Neither replaces the other — they complement.

| File | Lives in | Purpose | Shared? |
|------|----------|---------|---------|
| `schema.toml` | Project root | **What** to install — the project's dependency list | Yes, commit it |
| `manifest.toml` | `~/.config/depengine/manifest.toml` | **Your personal install catalog** — how you install things, plus personal defaults | No, stays on your machine |

The manifest does two jobs:

1. **Reusable knowledge base** — your accumulated recipes for how to install
   each tool (cargo vs git vs http, package name per distro, custom build
   steps). Build it once, carry it across every project — no need to repeat
   complex configs in every schema.toml.
2. **Machine-specific overrides** — when a package's name differs on your
   distro, or you prefer a different installation method than what the
   project's schema declares.

```sh
cp manifest.example.toml ~/.config/depengine/manifest.toml
# then edit: add your tools, set per-distro package names, define custom methods
```

When you run `install` or `validate`, your manifest merges with the
project's schema. Three rules to remember:

1. **The schema always wins on conflict.** Your manifest fills in gaps; it
   never overrides what the project declares.
2. **Tools that only exist in your manifest are rejected by default** — this
   stops you from accidentally injecting a personal tool into a shared
   project. Opt in with `[manifest] allow_new_tools = true`.
3. **A few fields can run arbitrary code** (`pre_install`, `post_install`,
   `requires`, `method_order`, `method_prefer`, `method_only`). Your manifest
   can set defaults for these, but the schema still overrides them.

Full breakdown: [manifest merge rules](docs/schema-reference.md#manifest-merge-rules).

---

## Commands at a glance

| Command | Does |
|---------|------|
| `init` | Create a new `schema.toml` |
| `install` | Install all tools from the schema |
| `validate` | Check the schema without installing anything |
| `check <tool>` | Check whether one tool is installed |
| `status` | Show what's installed (all tools; positional args ignored) |
| `remove <tool>` | Uninstall a tool |
| `update` | Resolve `{latest}` placeholders, write `depengine.lock` |
| `upgrade` | Upgrade installed tools to the versions pinned in `depengine.lock` |
| `graph` | Show the dependency graph |
| `why <tool>` | Explain how a tool would be installed |
| `forget <tool>` | Drop a tool from state without touching the system |
| `undo` | Revert the last installation |
| `diff` | Compare two state files |
| `sbom` | Export an SBOM (CycloneDX or SPDX) |
| `completion <shell>` | Generate shell completion scripts |

Every flag and default lives in **[`docs/cli-reference.md`](docs/cli-reference.md)**.

---

## Supported installation methods

| Category | Methods |
|----------|---------|
| **Native** | `native` (auto-detects apt/pacman/dnf/brew/...) + per-manager aliases |
| **Language** | `cargo`, `go`, `pip`, `pipx`, `uv`, `npm`, `pnpm`, `bun`, `gem`, `yarn`, `yarn-berry`, `composer`, `apm` |
| **Desktop** | `flatpak`, `snap`, `vscode`, `vscodium`, `cask` (macOS), `mas` (Mac App Store) |
| **Windows** | `winget`, `scoop`, `choco` |
| **Specialized** | `sdkman`, `steamcmd`, `pacstall`, `aur` (configurable helper), `conda`, `asdf` |
| **Other** | `git` (clone + build), `http` (download + extract + checksum) |

Auto-detected native managers, by distro family:

```
debian → apt      fedora  → dnf       suse    → zypper    arch  → pacman
alpine → apk      void    → xbps      gentoo  → emerge    macos → brew
termux → pkg      freebsd → pkg       openbsd → pkg_add   netbsd → pkg
mint   → apt      opkg    → opkg
```

---

## HTTP download security

HTTP installs can verify the download with a `checksum` field inside the
method block:

```toml
[tools.yq]
  [tools.yq.http]
  url = "https://github.com/mikefarah/yq/releases/download/{latest}/yq_linux_{arch}"
  checksum = "sha256:<hex>"          # pinned — verified exactly
```

- `checksum = "sha256:<hex>"` — the download is hashed and compared against
the pinned value; a mismatch rejects the install. Algorithms: `sha256`,
`sha512`, `sha1`, `md5`.
- `checksum = "sha256:auto"` — the expected hash is fetched from a companion
checksum file on first use. This is **TOFU (Trust On First Use)**: the hash
comes from the same server as the binary, so it is **not** verified against
a trusted source (the engine logs a warning and suggests pinning the hash in
`depengine.lock`). Two fields tune `:auto` resolution:
  - `checksum_url` — explicit URL of the checksum file, when you host it
    separately from the download.
  - `checksum_file_format` — layout of that file: `sha256sum` (hash +
    filename), `bsd` (BSD extended), or `raw` (the whole file is the hash).
    Defaults to auto-detection.

```toml
[tools.yq]
  [tools.yq.http]
  url = "https://github.com/mikefarah/yq/releases/download/{latest}/yq_linux_{arch}"
  checksum = "sha256:auto"
  checksum_url = "https://example.com/sha256sums.txt"
  checksum_file_format = "sha256sum"   # sha256sum | bsd | raw
```

---

## Placeholders

Placeholders like `{arch}`, `{os}`, and `{latest}` get expanded in schema
fields before installation:

```toml
fastfetch = { http = { url = "https://github.com/fastfetch-cli/fastfetch/releases/download/{latest}/fastfetch-linux-amd64.deb" } }
```

Full list of all 15 placeholders: [schema-reference.md#placeholders](docs/schema-reference.md#placeholders).

## Editor support

A [JSON Schema](schema/depengine.schema.json) describes `schema.toml`.
Editors with TOML extensions (e.g. [taplo](https://taplo.tamasfe.dev/) for
VSCode) use it for autocomplete, inline validation, and hover docs. Add to
`.vscode/settings.json`:

```json
{
  "taplo.schema.enabled": true,
  "taplo.schema.url": "https://raw.githubusercontent.com/Khorea1/depengine/main/schema/depengine.schema.json"
}
```

## Environment variables

| Variable | Effect |
|----------|--------|
| `DEPENGINE_DETECT_SCRIPT` | Path to `detect_os.sh` (default: next to the binary) |
| `DEPENGINE_MANIFEST` | Path to the personal manifest, overriding XDG discovery |
| `XDG_STATE_HOME` | Base directory for the state file (default: `~/.local/state/depengine/state.json`) |
| `DEPENGINE_TRACE_ID` | Trace ID propagated to subprocesses |
| `DEPENGINE_LOG_JSON` | `=1` enables JSON log output |

---

## Development

See [`docs/architecture.md`](docs/architecture.md) for package layout and
the install flow.

```sh
go test ./...                    # unit tests
go vet ./...                     # static analysis
go build -o depengine .          # build

cd tests/integration && docker compose up --build   # Debian, Arch, Fedora, Alpine
```

## License

[GNU General Public License v3 or later](LICENSE)
