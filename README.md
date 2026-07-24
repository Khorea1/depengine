[![🇧🇷](https://img.shields.io/badge/lang-pt--br-green)](README-pt.md)
[![🇺🇸](https://img.shields.io/badge/lang-en--us-blue)](README.md)

<h1 align="center">depengine</h1>

<p align="center">
  <b>Distro-agnostic dependency installer</b><br>
  Declare <i>what</i> to install — the engine figures out <i>how</i>.
</p>

<p align="center">
  <a href="https://github.com/depengine/depengine/actions"><img src="https://github.com/depengine/depengine/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/depengine/depengine"><img src="https://pkg.go.dev/badge/github.com/depengine/depengine" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/depengine/depengine"><img src="https://goreportcard.com/badge/github.com/depengine/depengine" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-GPL--3.0--or--later-blue" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.26-blue" alt="Go 1.26">
  <img src="https://img.shields.io/badge/platform-linux%20%7C%20macOS-lightgrey" alt="Platform">
  <img src="https://img.shields.io/badge/static%20binary-%E2%9C%93-brightgreen" alt="Static binary">
  <img src="https://img.shields.io/badge/windows-limited-yellow" alt="Windows">
</p>

---

Write a `schema.toml` listing your tools. depengine tests every available
method — native package manager, cargo, go, pip, git, http, flatpak, and more —
until one succeeds. No runtime dependencies: single static Go binary.

```sh
# Validate your schema
depengine validate --schema schema.toml

# Install everything
depengine install

# Check status
depengine status
```

---

## Quick start

```mermaid
flowchart LR
    A[Write schema.toml] --> B[depengine validate]
    B --> C[depengine update]
    C --> D[depengine install]
    D --> E[depengine status]
    E --> F[depengine check &#9989;]
```

```sh
# Build
go build -o depengine .

# Validate the reference schema
./depengine validate
# → ✓ schema is valid

# Validate with environment checks (tools on PATH)
./depengine validate --check-env
# → warnings for missing tools (expected in dev)

# Install everything from schema
./depengine install

# Quick check
./depengine check nvim
# → Installed

# Structured output for scripts
./depengine validate --format=json
./depengine install --json

# Status and removal
./depengine status                         # list installed
./depengine status nvim                    # specific tool
./depengine remove nvim                    # uninstall

# Resolve {latest} placeholders
./depengine update                         # writes schema.lock

# Export SBOM
./depengine sbom --format cyclonedx        # CycloneDX 1.5
./depengine sbom --format spdx > bom.json  # SPDX 2.3
```
> **Note:** Windows builds are provided but **not fully supported** — file locking
> and state management are not implemented on Windows. Linux and macOS are the
> primary targets.


---

## schema.toml — the declarative heart

A schema describes **tools** (dependencies) and **methods** (how to install).
The engine tries methods in `method_order` until one works.

### Form 1 — Simple tool (name = package across all distros)

```toml
simple = ["zsh", "bat", "kitty", "mpv"]
```

### Form 2 — Package name varies per native manager

```toml
fd   = { apt = "fd-find" }                 # "fd" on all others
nvim = { pacman = "neovim", apt = "neovim" }
```

### Form 3 — Language ecosystem managers

```toml
organize = { pip = "organize-tool", pipx = "organize-tool" }
fzf      = { go  = "github.com/junegunn/fzf" }
lf       = { go  = "github.com/gokcehan/lf" }
```

> When `pkg == tool_name`, use `true` instead of repeating:
> `ruff = { python = true }` ≡ `ruff = { pipx = "ruff", uv = "ruff" }`.
> Buckets (`python`, `node`) expand to all methods in that ecosystem at once
> (see Form 10 below).

### Form 4 — cargo/go with custom git source (not the official repo)

```toml
matugen = { cargo = { git = "https://github.com/InioX/matugen" } }
```

### Form 5 — Pre-install hook (before any method)

```toml
[tools.myenv]
pre_install = "curl -fsSL https://setup.example.com | sh"

  [tools.myenv.native]
  pkg = "my-env"
```

> `pre_install` runs before the first method — if it fails, the tool is aborted.
> Requires `--allow-arbitrary-code` (security warning shown by default).

### Form 6 — Git: clone + manual build

```toml
ctpv = { git = { url = "https://github.com/NikitaIvanovV/ctpv", build = "make && sudo make install" } }
```

### Form 7 — HTTP: download artifact (deb, zip, binary)

```toml
fastfetch = { http = {
  url = "https://github.com/fastfetch-cli/fastfetch/releases/download/{latest}/fastfetch-linux-amd64.deb",
  checksum = "sha256:auto"
} }
```

> `checksum` accepts a literal hash (`sha256:...`) or `:auto` (automatic resolution).
> Use `checksum_url` for a separate source, `sudo_required = false` if root isn't needed,
> and `signing_key`/`signature_url` for GPG verification.

### Form 8 — Tool-to-tool dependency

```toml
zathura-pdf-mupdf = { requires = ["zathura"], pacman = "zathura-pdf-mupdf" }
```

### Form 9 — Complex case: multiple methods + distro condition

```toml
[tools.DepartureMono]
postinstall = "fc-cache -fv"

  [tools.DepartureMono.aur]
  pkg  = "otf-departure-mono-nerd"
  when = { distro_family = ["arch"] }

  [tools.DepartureMono.http]
  url        = "https://github.com/ryanoasis/nerd-fonts/releases/download/{latest}/DepartureMono.zip"
  extract_to = "~/.local/share/fonts/DepartureMono"
```

> **Golden rule:** Tool-level fields (`requires`, `postinstall`, `pre_install`) go
> _outside_ the method block. Method-specific fields (`when`, `url`, `build`,
> `checksum`, `extract_to`, `pkg`, `git`) go _inside_.

### Form 10 — `true` shorthand and ecosystem buckets

When the package name equals the tool name (~80% of Python/Node cases),
use `true` instead of repeating. Buckets expand to all methods in the ecosystem.

**Built-in buckets:**

| Bucket | Expansion | Typical use |
|--------|-----------|-------------|
| `python = true` | `{ pip = true, pipx = true, uv = true }` | Python tools (ruff, httpie, poetry) |
| `node = true` | `{ npm = true, pnpm = true, bun = true }` | Node tools (prettier, eslint, tsx) |

```toml
ruff = { python = true }        # ≡ { pipx = "ruff", uv = "ruff" }
prettier = { node = true }      # ≡ { npm = "prettier", pnpm = "prettier", bun = "prettier" }
httpie = { python = true }      # pip + pipx + uv (pkg=httpie on all)
```

> Each `true` uses the tool name as pkg via SubstitutePkg.
> Explicit methods are NOT overridden by the bucket:
> `organize = { pip = "organize-tool", python = true }` keeps `pip` as
> `"organize-tool"` and only expands `pipx`/`uv`.
>
> Buckets only accept `true`. `python = false` or `python = "foo"` won't expand
> (the engine treats it as an unknown method → error on `validate`).
>
> `all = true` **does not exist** — too imprecise, risks installing the wrong
> package from a different ecosystem.

---

## Editor support

A [JSON Schema](schema/depengine.schema.json) describes the `schema.toml` structure.
Editors with TOML extensions (e.g. [taplo](https://taplo.tamasfe.dev/) for VSCode) use it for:

- **Autocomplete** of fields (`url:`, `build:`, `checksum:`, `when:`, etc.)
- **Inline validation** of types and required fields
- **Hover docs** with field descriptions

Enable it in `.vscode/settings.json`:

```json
{
  "taplo.schema.enabled": true,
  "taplo.schema.url": "https://raw.githubusercontent.com/depengine/depengine/main/schema/depengine.schema.json"
}
```

Or use the local schema:

```json
{
  "taplo.schema.enabled": true,
  "taplo.schema.url": "./schema/depengine.schema.json"
}
```

---

## CLI reference

### `depengine install [flags]`

Installs all tools from the schema, respecting `method_order`, `when`,
`requires`, and topological ordering.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | `depengine.toml` | Path to schema file (auto-detected: depengine.toml, schema.toml, depends.toml) |
| `--dry-run` | `false` | Show what would be installed |
| `--verbose` | `false` | Detailed output per tool |
| `--json` | `false` | JSON output |
| `--only <tool>` | — | Install a single tool |
| `--skip <tools>` | — | Skip comma-separated tools |
| `--sort-by` | — | Sort output: `name`, `status`, `method` |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `--diagnose` | `false` | Diagnostic mode: DEBUG + dry-run + verbose |
| `--profile <tag>` | — | Filter tools by tag (e.g. `desktop`, `server`) |
| `--jobs <n>` | `1` | Max concurrent installations |
| `--allow-arbitrary-code` | `false` | Suppress build script security warnings |
| `--frozen-lockfile` | `false` | Abort if lockfile doesn't exist |
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |
| `--quiet` | `false` | Suppress non-essential output |

### `depengine validate [flags]`

Validates the schema and optionally the environment.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | `depengine.toml` | Path to schema (auto-detected: depengine.toml, schema.toml, depends.toml) |
| `--check-env` | `false` | Check required tools are on PATH |
| `--format` | `text` | Output format: `text` or `json` |
| `--strict` | `false` | Warnings become errors (exit code 1) |
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |

### `depengine check <tool> [flags]`

Checks whether a specific tool is installed.

| Flag | Default | Description |
|------|---------|-------------|
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |
| `--live` | `false` | Check live system instead of state file |
| `--format` | `text` | Output format: `text` or `json` |

### `depengine status [tool]`

Shows installation state of all tools or a specific one.

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Format: `text` or `json` |
| `--orphans` | `false` | Show only installed tools not in schema |
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |

### `depengine remove <tool> [flags]`

Removes a tool installed by depengine (native, cargo, pip, etc.).

| Flag | Default | Description |
|------|---------|-------------|
| `--all` | `false` | Remove all tracked tools |
| `--dry-run` | `false` | Show what would be removed without executing |
| `--force` | `false` | Skip confirmation when removing all tools |
| `--only <tool>` | — | Remove a specific tool (alternative to positional argument) |

### `depengine update [flags]`

Updates `schema.lock` by resolving `{latest}` via GitHub API.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | `depengine.toml` | Path to schema (auto-detected: depengine.toml, schema.toml, depends.toml) |
| `--lock` | `schema.lock` | Path to lockfile |
| `--profile <tag>` | — | Filter tools by tag |
| `--frozen-lockfile` | `false` | Abort if schema.lock doesn't exist |
| `--dry-run` | `false` | Show what would be updated |
| `-v` | `false` | Verbose output |
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |

### `depengine graph [flags]`

Shows the dependency graph as text, Mermaid, or DOT.

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Format: `text`, `mermaid` or `dot` |
| `--only <tool>` | — | Subgraph for one tool |
| `--skip <tools>` | — | Comma-separated tools to skip |
| `--profile <tag>` | — | Filter by tag |
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |

### `depengine why <tool>`

Explains how a tool would be installed, method by method.

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | JSON output |
| `--format` | `text` | Output format: `text` or `json` |

### `depengine forget <tool>`

Removes a tool from state without touching the system.

### `depengine undo [flags]`

Reverts the last installation.

| Flag | Default | Description |
|------|---------|-------------|
| `--list` | `false` | List available snapshots |
| `--snapshot <path>` | — | Revert to a specific snapshot |

### `depengine diff [flags]`

Compares two state files.

| Flag | Default | Description |
|------|---------|-------------|
| `--other <path>` | — | Second state file |
| `--json` | `false` | Output differences as JSON |

### `depengine completion <shell>`

Generates shell completion scripts. Shells: `bash`, `zsh`, `fish`.

### `depengine sbom [flags]`

Exports an SBOM in CycloneDX 1.5 or SPDX 2.3 format.

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `cyclonedx` | Format: `cyclonedx` or `spdx` |

### `depengine help [command]`

Shows help for depengine or a specific command. Use \`depengine help --man\` to display the man page.

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Tool failure / strict mode warnings |
| `2` | Schema error (invalid TOML, validation) |
| `3` | Runtime error (`detect_os.sh` not found, etc.) |

---

## Supported installation methods (30)

| Category | Methods |
|----------|---------|
| **Native** | `native` (auto-detects apt/pacman/dnf/brew/...) + per-manager aliases |
| **Language** | `cargo`, `go`, `pip`, `pipx`, `uv`, `npm`, `pnpm`, `bun`, `gem`, `yarn`, `yarn-berry`, `composer`, `apm` |
| **Desktop** | `flatpak`, `snap`, `vscode`, `vscodium`, `cask` (macOS), `mas` (Mac App Store) |
| **Windows** | `scoop`, `choco` |
| **Specialized** | `sdkman`, `steamcmd`, `pacstall`, `aur` (configurable helper), `conda`, `asdf` |
| **Other** | `git` (clone + build), `http` (download + extract + checksum) |

Auto-detected native managers (15 distro families, ~27 managers):

```
debian  → apt        fedora → dnf      suse   → zypper    arch     → pacman
alpine  → apk        void   → xbps     gentoo → emerge    macos    → brew
termux  → pkg        freebsd → pkg     openbsd → pkg_add  netbsd   → pkg
mint    → apt        opkg   → opkg
```

---

## Architecture

```mermaid
flowchart TB
    subgraph Input
        SCHEMA[schema.toml]
        LOCK[schema.lock]
    end

    subgraph Engine
        PARSER[pkg/schema.ParseSchema]
        GRAPH[pkg/graph\nTopological sort]
        EXEC[pkg/exec.Executor]
    end

    subgraph Adapters
        NATIVE[pkg/native\n15 distro families]
        LANG[pkg/lang\n25 language adapters]
        GIT[pkg/git\nClone + build]
        HTTP[pkg/httpdownload\nDownload + checksum]
    end

    subgraph Output
        STATE[State file]
        REPORT[Install report]
        SBOM[SBOM\nCycloneDX / SPDX]
    end

    SCHEMA --> PARSER
    LOCK --> PARSER
    PARSER --> GRAPH
    GRAPH --> EXEC
    EXEC --> NATIVE
    EXEC --> LANG
    EXEC --> GIT
    EXEC --> HTTP
    NATIVE --> STATE
    LANG --> STATE
    GIT --> STATE
    HTTP --> STATE
    STATE --> REPORT
    STATE --> SBOM
```

### Package layers

| Package | Responsibility |
|---------|----------------|
| `pkg/run` | `Runner` interface — seam for subprocess. Production: `OSExecRunner`. Tests: `FakeRunner`. |
| `pkg/engine` | Invokes `detect_os.sh`, parses JSON → `Facts`, resolves clan via `ResolveFamily` (15 clans) |
| `pkg/native` | Declarative registry of 15 distros. Manager lookup, install command building |
| `pkg/schema` | TOML parser (3 declaration forms) + placeholder expansion + kind validation |
| `pkg/exec` | Central executor + `Adapter` interface + registry + sync manager + reports |
| `pkg/lang` | 25 language/ecosystem adapters (generic `BaseAdapter` + specialized) |
| `pkg/git` | GitAdapter: shallow clone + build |
| `pkg/httpdownload` | HTTPAdapter: download + extraction + SHA256 + `{latest}` resolution |
| `pkg/graph` | Topological sort (Kahn) with cycle detection |
| `pkg/log` | Structured logger via `log/slog` with trace ID, DEBUG–ERROR levels |
| `pkg/validate` | Structural + semantic + environmental validation |

### Installation flow

```mermaid
flowchart LR
    A[For each tool\nin topological order] --> B[For each method\nin method_order]
    B --> C{when matches?}
    C -->|no| B
    C -->|yes| D{Adapter\nAvailable?}
    D -->|no| B
    D -->|yes| E{Already\ninstalled?}
    E -->|yes| B
    E -->|no| F[Install]
    F --> G[Report]
```

---

## Placeholders

The schema supports placeholders resolved before installation:

| Placeholder | Source | Example |
|-------------|--------|---------|
| `{arch}` | `detect_os.sh` | `x86_64`, `aarch64` |
| `{os}` | `detect_os.sh` | `linux`, `darwin` |
| `{distro_family}` | `ResolveFamily` | `debian`, `arch`, `fedora` |
| `{kernel}` | `detect_os.sh` | `5.15.0` |
| `{libc}` | `detect_os.sh` | `glibc`, `musl` |
| `{init_system}` | `detect_os.sh` | `systemd`, `openrc` |
| `{pkg}` | Substituted by adapter at install time | package name |
| `{latest}` | Resolved via GitHub API (git/http adapters) | `v1.2.3` |

Unknown placeholders are flagged by validation (`depengine validate`).

---

## Environment variables

| Variable | Effect |
|----------|--------|
| `DEPENGINE_DETECT_SCRIPT` | Path to `detect_os.sh` (default: next to the binary) |
| `DEPENGINE_TRACE_ID` | Trace ID propagated to subprocesses |
| `DEPENGINE_LOG_JSON` | `=1` enables JSON log output |

---

## Development

```sh
go test ./...                    # 18 packages, 100+ tests
go vet ./...                     # static analysis
go build -o depengine .          # build
./depengine validate --schema schema.toml --check-env --format=json
```

### Integration tests (Docker)

```sh
cd tests/integration
docker compose up --build
# Tests install/validate on Debian, Arch, Fedora, and Alpine
```

---

## License

[GNU General Public License v3 or later](LICENSE)
