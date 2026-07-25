<h1 align="center">depengine</h1>

<p align="center">
  <b>Distro-agnostic dependency installer</b><br>
  Declare <i>what</i> to install — the engine figures out <i>how</i>.
</p>

<p align="center">
  <a href="https://github.com/Khorea1/depengine/actions"><img src="https://github.com/Khorea1/depengine/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://goreportcard.com/report/github.com/Khorea1/depengine"><img src="https://goreportcard.com/badge/github.com/Khorea1/depengine" alt="Go Report Card"></a>
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
| [`docs/schema-reference.md`](docs/schema-reference.md) | Every `schema.toml` syntax case (1–11), `when` conditions, buckets, method control — the full spec |
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
./depengine update                   # writes depengine.lock
./depengine install --frozen-lockfile

# --- Everyday commands ---
./depengine status                   # what's installed
./depengine status nvim              # a specific tool
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

## `schema.toml` vs `manifest.toml` — two files, two jobs

Most projects only ever need `schema.toml`. The second file exists for one
specific situation: when a package's name differs on your machine in a way
that isn't worth putting in the shared, version-controlled schema.

| File | Lives in | Answers | Shared with your team? |
|------|----------|---------|--------------------------|
| `schema.toml` | Project root | **What** to install | Yes — commit it |
| `manifest.toml` | `~/.config/depengine/manifest.toml` | **How** to install, personally | No — stays on your machine |

```sh
# Only if you need it:
cp manifest.example.toml ~/.config/depengine/manifest.toml
# edit method overrides for your machine, e.g. pkg_overrides.pacman = "..."
```

Fields overlap field-by-field where it makes sense (e.g. per-manager package
name overrides), and the schema always wins when both define the same
top-level field. Fields that could inject arbitrary intent into a shared
project (`pre_install`, `post_install`, `requires`, `tags`) are rejected if
you try to set them in the manifest — see
[the merge rules in the schema reference](docs/schema-reference.md#manifest-merge-rules)
for the full breakdown.

---

## CLI reference

### `depengine init [flags]`

Creates a new `schema.toml` (or a custom path). Fails if the file already exists.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema <path>` | `schema.toml` | Path to write the schema file |
| `--add <tools>` | — | Comma-separated tool names to pre-populate |
| `--interactive` | `false` | Interactive wizard — prompts for tools, methods, and options step by step |

### `depengine install [flags]`

Installs all tools from the schema, respecting `method_order`, `when`,
`requires`, and topological ordering.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | auto (`schema.toml` → `depengine.toml` → `depends.toml`) | Path to schema file |
| `--dry-run` | `false` | Show what would be installed |
| `--json` | `false` | JSON output |
| `--only <tool>` | — | Install a single tool |
| `--skip <tools>` | — | Skip comma-separated tools |
| `--sort-by` | — | Sort output: `name`, `status`, `method` |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `--diagnose` | `false` | Diagnostic mode: DEBUG + dry-run + verbose |
| `--profile <tag>` | — | Filter tools by tag (e.g. `desktop`, `server`) |
| `--jobs <n>` | `1` | Max concurrent installations |
| `--allow-arbitrary-code` | `false` | Allow tools with build scripts / hooks that run arbitrary code |
| `--frozen-lockfile` | `false` | Abort if `depengine.lock` doesn't exist |
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |
| `--quiet` | `false` | Suppress non-essential output |

### `depengine validate [flags]`

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | auto | Path to schema |
| `--check-env` | `false` | Check required tools are on PATH |
| `--format` | `text` | `text` or `json` |
| `--strict` | `false` | Warnings become errors (exit code 1) |
| `--manifest` / `--no-manifest` | — | Same as `install` |

### `depengine check <tool> [flags]`

| Flag | Default | Description |
|------|---------|-------------|
| `--live` | `false` | Check the live system instead of the state file |
| `--format` | `text` | `text` or `json` |

### `depengine status [tool]`

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | `text` or `json` |
| `--orphans` | `false` | Show only installed tools not in the schema |

### `depengine remove <tool> [flags]`

| Flag | Default | Description |
|------|---------|-------------|
| `--all` | `false` | Remove all tracked tools |
| `--dry-run` | `false` | Show what would be removed |
| `--force` | `false` | Skip confirmation when removing all |

### `depengine update [flags]`

Resolves `{latest}` placeholders and writes `depengine.lock`.

| Flag | Default | Description |
|------|---------|-------------|
| `--lock` | `depengine.lock` | Path to lockfile |
| `--profile <tag>` | — | Filter tools by tag |
| `--frozen-lockfile` | `false` | Abort if `depengine.lock` doesn't exist |
| `--dry-run` | `false` | Show what would change |

### `depengine graph [flags]`

Shows the dependency graph as text, Mermaid, or DOT.

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | `text`, `mermaid`, or `dot` |
| `--only <tool>` | — | Subgraph for one tool |

### `depengine why <tool> [flags]`

Explains how a tool would be installed, method by method.

| Flag | Default | Description |
|------|---------|-------------|
| `--fields` | `false` | Show field-level provenance — which layer (schema/manifest) contributed each field |
| `--format` | `text` | `text` or `json` |

### `depengine forget <tool>`

Removes a tool from state without touching the system.

### `depengine undo [flags]`

Reverts the last installation using a snapshot.

### `depengine diff [flags]`

Compares two state files.

### `depengine sbom [flags]`

Exports an SBOM in CycloneDX 1.5 or SPDX 2.3.

### `depengine completion <shell>`

Generates shell completion scripts (`bash`, `zsh`, `fish`).

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Tool failure / strict mode warnings |
| `2` | Schema error (invalid TOML, validation) |
| `3` | Runtime error (`detect_os.sh` not found, etc.) |

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

## Placeholders

| Placeholder | Source | Example |
|-------------|--------|---------|
| `{arch}` | `detect_os.sh` | `x86_64`, `aarch64` |
| `{os}` | `detect_os.sh` | `linux`, `darwin` |
| `{distro_family}` | Resolved clan | `debian`, `arch`, `fedora` |
| `{kernel}` | `detect_os.sh` | `5.15.0` |
| `{libc}` | `detect_os.sh` | `glibc`, `musl` |
| `{init_system}` | `detect_os.sh` | `systemd`, `openrc` |
| `{pkg}` | Substituted by the adapter at install time | package name |
| `{latest}` | Resolved via GitHub API (`git`/`http` adapters) | `v1.2.3` |

Unknown placeholders are flagged by `depengine validate`.

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
