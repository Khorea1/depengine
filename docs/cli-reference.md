# CLI reference

Every command, its flags, and defaults. For a task-oriented walkthrough,
see [the README](../README.md); for `schema.toml` syntax, see
[schema-reference.md](schema-reference.md).

<!-- BEGIN GENERATED CLI REFERENCE -->
## `depengine [flags]`

Distro-agnostic dependency installer

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for depengine |
| `-v, --version` | local | `false` | Show version |

## `depengine check <tool> [flags]`

Reconciles observed host identity with the resolved plan. Only 'satisfied' exits successfully; absent, drifted, unknown, and broken return a failure code.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `` | output format (json) |
| `-h, --help` | local | `false` | help for check |
| `--json` | local | `false` | JSON output |
| `--live` | local | `false` | check via adapter (may run subprocesses) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |

## `depengine completion [flags]`

Generate the autocompletion script for depengine for the specified shell. See each sub-command's help for details on how to use the generated script.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for completion |

## `depengine completion bash`

Generate the autocompletion script for the bash shell.  This script depends on the 'bash-completion' package. If it is not installed already, you can install it via your OS's package manager.  To load completions in your current shell session:  	source <(depengine completion bash)  To load completions for every new session, execute once:  #### Linux:  	depengine completion bash > /etc/bash_completion.d/depengine  #### macOS:  	depengine completion bash > $(brew --prefix)/etc/bash_completion.d/depengine  You will need to start a new shell for this setup to take effect.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for bash |
| `--no-descriptions` | local | `false` | disable completion descriptions |

## `depengine completion fish [flags]`

Generate the autocompletion script for the fish shell.  To load completions in your current shell session:  	depengine completion fish \| source  To load completions for every new session, execute once:  	depengine completion fish > ~/.config/fish/completions/depengine.fish  You will need to start a new shell for this setup to take effect.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for fish |
| `--no-descriptions` | local | `false` | disable completion descriptions |

## `depengine completion powershell [flags]`

Generate the autocompletion script for powershell.  To load completions in your current shell session:  	depengine completion powershell \| Out-String \| Invoke-Expression  To load completions for every new session, add the output of the above command to your powershell profile.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for powershell |
| `--no-descriptions` | local | `false` | disable completion descriptions |

## `depengine completion zsh [flags]`

Generate the autocompletion script for the zsh shell.  If shell completion is not already enabled in your environment you will need to enable it.  You can execute the following once:  	echo "autoload -U compinit; compinit" >> ~/.zshrc  To load completions in your current shell session:  	source <(depengine completion zsh)  To load completions for every new session, execute once:  #### Linux:  	depengine completion zsh > "${fpath[1]}/_depengine"  #### macOS:  	depengine completion zsh > $(brew --prefix)/share/zsh/site-functions/_depengine  You will need to start a new shell for this setup to take effect.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for zsh |
| `--no-descriptions` | local | `false` | disable completion descriptions |

## `depengine diff [file1] [file2] [flags]`

Compare two state files

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for diff |
| `--json` | local | `false` | output as JSON |
| `--other <string>` | local | `` | path to other state file (used when no args) |

## `depengine forget <tool> [flags]`

Forget a tool from state without removing it from the system

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for forget |

## `depengine graph [flags]`

Show the dependency graph

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: mermaid, dot, text |
| `-h, --help` | local | `false` | help for graph |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--only <string>` | local | `` | only show subgraph for specific tool |
| `--profile <string>` | local | `` | only show tools with matching tag |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--skip <string>` | local | `` | skip specific tools (comma-separated) |

## `depengine help [command] [flags]`

Help about any command

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for help |
| `--man` | local | `false` | Show the man page |

## `depengine init [flags]`

Initialize a schema.toml for a new project

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--add <string>` | local | `` | comma-separated tool names to pre-populate (e.g. 'zsh,bat,nvim') |
| `-h, --help` | local | `false` | help for init |
| `--interactive` | local | `false` | interactive mode: walk through adding tools |
| `--schema <string>` | local | `` | path to write (default: schema.toml) |

## `depengine install [flags]`

Install tools from schema.toml

Aliases: `i`.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--allow-arbitrary-code` | local | `false` | permit hooks, build scripts, and other arbitrary code execution |
| `--diagnose` | local | `false` | diagnostic mode: DEBUG + dry-run + verbose |
| `--dry-run` | local | `false` | show what would be installed |
| `--frozen-lockfile` | local | `false` | fail if depengine.lock does not exist or needs update |
| `-h, --help` | local | `false` | help for install |
| `--jobs <int>` | local | `1` | max concurrent installations (default 1 = sequential) |
| `--json` | local | `false` | JSON output |
| `--log-level <string>` | local | `` | log level: debug, info, warn, error |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--only <string>` | local | `` | only install specific tool |
| `--profile <string>` | local | `` | only install tools with matching tag (e.g. minimal,desktop,server) |
| `--quiet` | local | `false` | suppress per-tool status lines; show only final summary |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--skip <string>` | local | `` | skip specific tools (comma-separated) |
| `--sort-by <string>` | local | `` | sort output by: name, status, method |
| `--verbose` | local | `false` | detailed output |

## `depengine remove [tool...] [flags]`

Observes the tracked target before removal. Proven-absent targets release state and ownership; unknown or broken observations preserve state and fail.

Aliases: `rm`, `uninstall`.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--all` | local | `false` | remove all tools |
| `--dry-run` | local | `false` | show what would be removed |
| `--force` | local | `false` | skip confirmation when removing all tools |
| `-h, --help` | local | `false` | help for remove |
| `--only <string>` | local | `` | only remove specific tool (alternative to positional arg) |
| `--schema <string>` | local | `` | path to schema.toml (optional, for validation) |

## `depengine sbom [flags]`

Export a software bill of materials of installed state

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `cyclonedx` | output format: cyclonedx or spdx |
| `-h, --help` | local | `false` | help for sbom |

## `depengine status [flags]`

Reconciles tracked state with identity observed on the host. Status values include installed, missing, outdated, unknown, and broken.

Aliases: `st`.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for status |
| `--json` | local | `false` | JSON output (shorthand for --format=json) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--orphans` | local | `false` | show only orphaned tools |
| `--schema <string>` | local | `` | override schema path |

## `depengine undo [flags]`

Revert to a previous state snapshot

Aliases: `rollback`.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for undo |
| `--list` | local | `false` | list available snapshots |
| `--snapshot <string>` | local | `` | revert to specific snapshot file path |

## `depengine update [flags]`

Resolve and pin versions into depengine.lock

Aliases: `lock`.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--dry-run` | local | `false` | show what would be updated without writing lock |
| `--frozen-lockfile` | local | `false` | abort if depengine.lock does not exist |
| `-h, --help` | local | `false` | help for update |
| `--lock <string>` | local | `` | path to depengine.lock (default: alongside schema.toml) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--profile <string>` | local | `` | only resolve & pin tools with matching tag |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--v` | local | `false` | detailed output |

## `depengine upgrade [flags]`

Upgrade installed tools to pinned versions

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--allow-arbitrary-code` | local | `false` | permit hooks, build scripts, and other arbitrary code execution |
| `--dry-run` | local | `false` | show what would be upgraded without making changes |
| `--force` | local | `false` | skip confirmation prompt |
| `-h, --help` | local | `false` | help for upgrade |
| `--json` | local | `false` | JSON output |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--only <string>` | local | `` | only upgrade specific tool |
| `--quiet` | local | `false` | suppress per-tool status lines |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |

## `depengine validate [flags]`

Validate schema.toml

Aliases: `lint`.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--check-env` | local | `false` | check system environment for required tools |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for validate |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--strict` | local | `false` | treat warnings as errors |

## `depengine version [flags]`

Show version

Visibility: hidden (documented compatibility command).

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for version |

## `depengine why <tool> [flags]`

Explain how a tool would be installed

Aliases: `explain`.

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--fields` | local | `false` | show field-level provenance |
| `-h, --help` | local | `false` | help for why |
| `--json` | local | `false` | JSON output |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |

<!-- END GENERATED CLI REFERENCE -->

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Tool failure / strict mode warnings |
| `2` | Schema error (invalid TOML, validation) |
| `3` | Runtime error (`detect_os.sh` not found, etc.) |
