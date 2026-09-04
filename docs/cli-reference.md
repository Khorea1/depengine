# CLI reference

Every command, its flags, and defaults. For a task-oriented walkthrough,
see [the README](../README.md); for `schema.toml` syntax, see
[schema-reference.md](schema-reference.md).

## `depengine init [flags]`

Creates a new `schema.toml` (or a custom path). Fails if the file already exists.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema <path>` | `schema.toml` | Path to write the schema file |
| `--add <tools>` | — | Comma-separated tool names to pre-populate |
| `--interactive` | `false` | Interactive wizard — prompts for tools, methods, and options step by step |

## `depengine install [flags]`

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
| `--allow-arbitrary-code` | `false` | Suppress security warnings for build scripts / arbitrary code — without it, tools with hooks or build scripts that can run arbitrary code are blocked |
| `--frozen-lockfile` | `false` | Abort if `depengine.lock` doesn't exist |
| `--manifest <path>` | auto (XDG_CONFIG_HOME) | Path to personal manifest |
| `--no-manifest` | `false` | Disable personal manifest |
| `--quiet` | `false` | Suppress non-essential output |

## `depengine validate [flags]`

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | auto | Path to schema |
| `--check-env` | `false` | Check required tools are on PATH |
| `--format` | `text` | `text` or `json` |
| `--strict` | `false` | Warnings become errors (exit code 1) |
| `--manifest` / `--no-manifest` | — | Same as `install` |

## `depengine check <tool> [flags]`

| Flag | Default | Description |
|------|---------|-------------|
| `--live` | `false` | Check the live system instead of the state file |
| `--format` | `text` | `text` or `json` |

## `depengine status [flags]`

Shows the installation status of all tools in state against the schema.
Positional arguments are ignored — `status` always lists every tracked tool.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | from state | Override schema path |
| `--format` | `text` | `text` or `json` |
| `--json` | `false` | JSON output (shorthand for `--format=json`) |
| `--orphans` | `false` | Show only installed tools not in the schema |
| `--manifest` / `--no-manifest` | — | Same as `install` |

## `depengine remove <tool> [flags]`

| Flag | Default | Description |
|------|---------|-------------|
| `--all` | `false` | Remove all tracked tools |
| `--dry-run` | `false` | Show what would be removed |
| `--force` | `false` | Skip confirmation when removing all |

## `depengine update [flags]`

Resolves `{latest}` placeholders and writes `depengine.lock`.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | auto | Path to schema |
| `--lock` | alongside schema | Path to `depengine.lock` |
| `--profile` | — | Filter tools by tag |
| `--frozen-lockfile` | `false` | Abort if `depengine.lock` doesn't exist |
| `--dry-run` | `false` | Show what would change without writing |
| `-v` | `false` | Verbose output |
| `--manifest` / `--no-manifest` | — | Same as `install` |

## `depengine upgrade [flags]`

Upgrades installed tools whose recorded version is older than the pinned
version in `depengine.lock`. Requires a lockfile — run `depengine update`
first to resolve and pin versions. For each outdated tool, removes the old
install and reinstalls at the pinned version, then updates state.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | auto | Path to schema |
| `--manifest` / `--no-manifest` | — | Same as `install` |
| `--only <tool>` | — | Only upgrade this tool |
| `--dry-run` | `false` | Show what would be upgraded without making changes |
| `--force` | `false` | Skip confirmation prompt |
| `--json` | `false` | JSON output |
| `--quiet` | `false` | Suppress per-tool status lines |
| `--allow-arbitrary-code` | `false` | Suppress security warnings for build scripts / arbitrary code |

## `depengine graph [flags]`

Shows the dependency graph as text, Mermaid, or DOT.

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | `text`, `mermaid`, or `dot` |
| `--only <tool>` | — | Subgraph for one tool |

## `depengine why <tool> [flags]`

Explains how a tool would be installed, method by method.

| Flag | Default | Description |
|------|---------|-------------|
| `--schema` | auto | Path to schema |
| `--json` | `false` | JSON output |
| `--fields` | `false` | Show field-level provenance — which layer (schema/manifest) contributed each field |
| `--manifest` / `--no-manifest` | — | Same as `install` |

## `depengine forget <tool>`

Removes a tool from state without touching the system.

## `depengine undo [flags]`

Reverts the last installation using a snapshot.

| Flag | Default | Description |
|------|---------|-------------|
| `--list` | `false` | List available snapshots |
| `--snapshot <path>` | latest | Revert to a specific snapshot file |

## `depengine diff [flags]`

Compares two state files.

## `depengine sbom [flags]`

Exports an SBOM in CycloneDX 1.5 or SPDX 2.3.

## `depengine completion <shell>`

Generates shell completion scripts (`bash`, `zsh`, `fish`).

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Tool failure / strict mode warnings |
| `2` | Schema error (invalid TOML, validation) |
| `3` | Runtime error (`detect_os.sh` not found, etc.) |
