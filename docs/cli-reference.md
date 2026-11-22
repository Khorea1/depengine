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

## `depengine audit [flags]`

Reconciles the effective schema, the lockfile, tracked state and observed host identity. Every divergence carries a remediation, and one exit code aggregates the result (0 without errors, 1 with errors, 2 invalid schema, 3 runtime failure).

Examples:

```sh
  depengine audit                       # full consistency report
  depengine audit --strict              # warnings also fail the gate
  depengine audit --json                # machine-readable report
  depengine audit --schema ci/schema.toml --no-manifest
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for audit |
| `--json` | local | `false` | shorthand for --format=json |
| `--lock <string>` | local | `` | lockfile to audit (default: derived from schema path) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--schema <string>` | local | `` | path to schema.toml (default: the path recorded in state) |
| `--strict` | local | `false` | treat warnings as errors |

## `depengine bundle [flags]`

Manage offline artifact bundles

Examples:

```sh
  depengine bundle export --output ./offline-bundle
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for bundle |

## `depengine bundle export [flags]`

Export artifacts from a v2 lockfile

Examples:

```sh
  depengine bundle export --lock depengine.lock --output ./offline-bundle
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for export |
| `--json` | local | `false` | shorthand for --format=json |
| `--lock <string>` | local | `` | v2 lockfile to read (default depengine.lock) |
| `-o, --output <string>` | local | `` | new bundle directory to create |

## `depengine check <tool> [flags]`

Reconciles observed host identity with the resolved plan. Only 'satisfied' exits successfully; absent, drifted, unknown, and broken return a failure code.

Examples:

```sh
  depengine check nvim                 # compare desired state with the host
  depengine check nvim --live --json   # probe through the adapter
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for check |
| `--json` | local | `false` | shorthand for --format=json |
| `--live` | local | `false` | check via adapter (may run subprocesses) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
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

Examples:

```sh
  depengine diff state-before.json state-after.json
  depengine diff state-before.json state-after.json --json
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for diff |
| `--json` | local | `false` | shorthand for --format=json |
| `--other <string>` | local | `` | path to other state file (used when no args) |

## `depengine doctor [flags]`

Print host facts, discovered layers (schema, manifest, catalog), available managers and elevation, and lock state. It installs nothing, writes no state, and changes no files.

Examples:

```sh
  depengine doctor
  depengine doctor --json
  depengine doctor --schema ./schema.toml --no-manifest
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for doctor |
| `--json` | local | `false` | shorthand for --format=json |
| `--lock <string>` | local | `` | lockfile to inspect (default: derived from schema path) |
| `--manifest <string>` | local | `` | personal manifest path to inspect (default: auto-detect) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | ignore the personal manifest |
| `--schema <string>` | local | `schema.toml` | schema path to inspect |

## `depengine explain [term...] [flags]`

Answers depengine's vocabulary without leaving the terminal: schema fields, installation methods, and error codes, with the valid form, a minimal example, and the matching document. It reads neither the project schema, the host, nor the network.

Examples:

```sh
  depengine explain                 # list every known term
  depengine explain requires_when   # one field
  depengine explain http            # one method
  depengine explain E_DUPE_TOOL     # one error code
  depengine explain --json http     # machine-readable answer
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for explain |
| `--json` | local | `false` | shorthand for --format=json |

## `depengine forget <tool> [flags]`

Drop the tool's tracking; the system is untouched. For an untracked name, releases dangling shared-resource claims and drops a zero-reference prerequisite ownership record; refuses while tracked tools still require the prerequisite.

Examples:

```sh
  depengine forget nvim   # drop install state; the system is untouched
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for forget |
| `--json` | local | `false` | shorthand for --format=json |

## `depengine graph [flags]`

Show the dependency graph

Examples:

```sh
  depengine graph --format mermaid
  depengine graph --format graph --width 100
  depengine graph --only nvim --direction dependents
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--depth <int>` | local | `-1` | with --only: maximum traversal depth in edges (-1 = unbounded, 0 = only the tool itself) |
| `--direction <string>` | local | `deps` | with --only: traversal direction: deps, dependents, both |
| `--format <string>` | local | `text` | output format: mermaid, dot, text, graph |
| `-h, --help` | local | `false` | help for graph |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--only <string>` | local | `` | only show subgraph for specific tool |
| `--profile <string>` | local | `` | only show tools with matching tag |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--show-inactive` | local | `false` | include guard-inactive edges in effective/resolved views |
| `--skip <string>` | local | `` | skip specific tools (comma-separated) |
| `--view <string>` | local | `declared` | graph projection: declared, effective, resolved |
| `--width <int>` | local | `0` | terminal width for --format graph (0 = detect terminal width) |

## `depengine help [command] [flags]`

Help about any command

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for help |
| `--man` | local | `false` | Show the man page |

## `depengine import <recipe.toml> [flags]`

Validate a recipe before writing it to the catalog: preview declared origins, integrity and candidates; reject arbitrary code and credential references; require a checksum and warn about TOFU. Use --expected-digest to verify a separately trusted recipe identity.

Examples:

```sh
  depengine import ./fd.toml --dry-run   # preview origin, integrity and candidates
  depengine import ./fd.toml --dry-run --json  # inspect the preview in scripts
  depengine import ./fd.toml --force     # replace an existing recipe
  depengine import ./fd.toml --expected-digest sha256:YOUR_TRUSTED_DIGEST  # verify an out-of-band pin
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | destination catalog root (repeatable; the first root wins) |
| `--dry-run` | local | `false` | preview the import without writing the recipe |
| `--expected-digest <string>` | local | `` | require the canonical sha256:<64 hex> recipe digest from a trusted source |
| `--force` | local | `false` | replace an existing recipe with the same tool name |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for import |
| `--json` | local | `false` | shorthand for --format=json |

## `depengine init [flags]`

Initialize a schema.toml for a new project

Examples:

```sh
  depengine init                          # scaffold with placeholder tools
  depengine init --add "zsh,bat,nvim"     # non-interactive tool list
  depengine init --add nvim --dry-run     # preview without creating files
  depengine init --from-state             # adopt installed tools from tracked state
  depengine init --from-manifest --dry-run  # adopt personal manifest entries without writing
  depengine init --schema path/to/schema.toml
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--add <string>` | local | `` | comma-separated tool names to pre-populate (e.g. 'zsh,bat,nvim') |
| `--dry-run` | local | `false` | print the resulting schema without writing it |
| `--from-manifest <string>` | local | `` | adopt tools from the personal manifest (optional manifest path) |
| `--from-state <string>` | local | `` | adopt tools from tracked state (optional state file path) |
| `-h, --help` | local | `false` | help for init |
| `--interactive` | local | `false` | interactive mode: walk through adding tools |
| `--schema <string>` | local | `` | path to write (default: schema.toml) |

## `depengine install [flags]`

Install tools from schema.toml

Aliases: `i`.

Examples:

```sh
  depengine install                          # install every tool declared in schema.toml
  depengine install --only nvim              # one tool plus its dependencies
  depengine install --dry-run --sort-by name # preview the plan without mutating
  depengine install --frozen-lockfile        # reproducible install from depengine.lock
  depengine install --allow-arbitrary-code   # run pre_install/post_install hooks
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--allow-arbitrary-code` | local | `false` | permit hooks, build scripts, and other arbitrary code execution |
| `--bundle <string>` | local | `` | install locked artifacts from a local frozen artifact bundle |
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--diagnose` | local | `false` | diagnostic mode: DEBUG + dry-run + detailed report |
| `--dry-run` | local | `false` | show what would be installed |
| `--format <string>` | local | `text` | output format: text or json |
| `--frozen-lockfile` | local | `false` | fail if depengine.lock is missing, detectably stale, or lacks a supported required pin |
| `-h, --help` | local | `false` | help for install |
| `--jobs <int>` | local | `1` | max concurrent installations (default 1 = sequential) |
| `--json` | local | `false` | shorthand for --format=json |
| `--lock <string>` | local | `` | lockfile to read and update (default: derived from schema path) |
| `--log-level <string>` | local | `` | log level: debug, info, warn, error |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--only <string>` | local | `` | only install specific tool |
| `--profile <string>` | local | `` | only install tools with matching tag (e.g. minimal,desktop,server) |
| `--quiet` | local | `false` | suppress live per-tool status lines; show final per-tool report |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--skip <string>` | local | `` | skip specific tools (comma-separated) |
| `--sort-by <string>` | local | `` | sort output by: name, status, method |
| `--verbose` | local | `false` | deprecated compatibility flag; detailed output is already the default |

## `depengine journal [flags]`

Shows the durable install/remove/upgrade/update/undo history, filterable by tool, command, result, and time window. The journal lives in journal.jsonl next to state.json and contains no secrets.

Examples:

```sh
  depengine journal                          # recent mutations
  depengine journal --tool nvim --since 7d   # one tool, last week
  depengine journal --result failed --json   # failures as machine output
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--command <string>` | local | `` | only entries from this command (install, remove, upgrade, update, undo) |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for journal |
| `--json` | local | `false` | shorthand for --format=json |
| `--limit <int>` | local | `50` | maximum entries to show, most recent first; 0 means all |
| `--result <string>` | local | `` | only entries with this result (success, already, failed, skipped, updated, removed, reverted) |
| `--since <string>` | local | `` | start of the time window (RFC3339, YYYY-MM-DD, or duration such as 7d/24h) |
| `--tool <string>` | local | `` | only entries for this tool |
| `--until <string>` | local | `` | end of the time window (same formats as --since) |

## `depengine remove [tool...] [flags]`

Observes the tracked target before removal. Proven-absent targets release state and ownership; drifted, unknown or broken observations preserve state and fail. If the installation changed outside depengine, use 'depengine forget' to drop tracking and clean up manually.

Aliases: `rm`, `uninstall`.

Examples:

```sh
  depengine remove bat                  # remove one tool
  depengine remove bat lsd --dry-run    # preview the removal
  depengine remove --all --force        # remove everything without prompting
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--all` | local | `false` | remove all tools |
| `--dry-run` | local | `false` | show what would be removed |
| `--force` | local | `false` | skip confirmation when removing all tools |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for remove |
| `--json` | local | `false` | shorthand for --format=json |
| `--only <string>` | local | `` | only remove specific tool (alternative to positional arg) |
| `--quiet` | local | `false` | suppress per-tool status lines |
| `--schema <string>` | local | `` | path to schema.toml (optional, for validation) |

## `depengine sbom [flags]`

Export a software bill of materials of installed state

Examples:

```sh
  depengine sbom                  # CycloneDX
  depengine sbom --format spdx
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `cyclonedx` | output format: cyclonedx or spdx |
| `-h, --help` | local | `false` | help for sbom |
| `--lock <string>` | local | `` | lockfile to read for missing versions (default: derived from recorded schema) |

## `depengine self [flags]`

Manage the depengine executable itself

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for self |

## `depengine self update [flags]`

Rebuild depengine from a Git checkout and atomically replace the running executable. Cloning and building happen in a temporary directory; the previous binary is replaced only after the new build compiles and answers 'version'. Use --check to compare the running revision with the remote without installing, and --dry-run to resolve the source without building.

Examples:

```sh
  depengine self update
  depengine self update --check
  depengine self update --ref master --bin-dir "$HOME/.local/bin"
  depengine self update --recipe recipes/depengine.toml.example --dry-run
  depengine self update --dry-run --format json
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--bin-dir <string>` | local | `` | destination directory (default: the running executable's directory) |
| `--check` | local | `false` | compare revisions without building; exits 1 when an update is available |
| `--dry-run` | local | `false` | resolve the source without building or replacing |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for update |
| `--json` | local | `false` | shorthand for --format=json |
| `--recipe <string>` | local | `` | project-schema fragment declaring the git method (default: embedded recipe) |
| `--ref <string>` | local | `` | branch, tag, or commit to build (default: remote default branch) |
| `--repo <string>` | local | `` | Git source repository (default: the recipe's source) |

## `depengine status [flags]`

Reconciles tracked state with identity observed on the host. Status values include installed, missing, outdated, unknown, and broken.

Aliases: `st`.

Examples:

```sh
  depengine status              # schema vs state vs host
  depengine status --orphans    # installed tools missing from schema
  depengine status --format json
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for status |
| `--json` | local | `false` | shorthand for --format=json |
| `--lock <string>` | local | `` | lockfile to inspect (default: derived from schema path) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--orphans` | local | `false` | show only orphaned tools |
| `--quiet` | local | `false` | suppress layer and manifest header lines |
| `--schema <string>` | local | `` | override schema path |

## `depengine tutorial [flags]`

Creates a temporary project, shows a schema, runs validate/why/update/install --dry-run with an explanation of each output, and removes everything at the end. No step mutates the host.

Examples:

```sh
  depengine tutorial             # guided tour, cleans up afterwards
  depengine tutorial --keep      # keep the temporary project for inspection
  depengine tutorial --json      # machine-readable steps
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for tutorial |
| `--json` | local | `false` | shorthand for --format=json |
| `--keep` | local | `false` | keep the temporary project directory |

## `depengine undo [flags]`

Revert to a previous state snapshot. Removing tools added after the snapshot verifies the recorded target first; drifted, unknown or broken observations preserve state and fail.

Aliases: `rollback`.

Examples:

```sh
  depengine undo --list                # list available snapshots
  depengine undo --snapshot <path>     # restore one snapshot
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for undo |
| `--json` | local | `false` | shorthand for --format=json |
| `--list` | local | `false` | list available snapshots |
| `--snapshot <string>` | local | `` | revert to specific snapshot file path |

## `depengine update [flags]`

Resolve mutable references and write v2 when every tool has an immutable identity; keep v1 for native-only schemas

Aliases: `lock`.

Examples:

```sh
  depengine update                    # resolve mutable references; write depengine.lock
  depengine update --dry-run -v       # show the pins without writing
  depengine update --profile desktop  # re-resolve only tagged tools; keep other pins
  depengine update --prune             # remove metadata for deleted tools
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--dry-run` | local | `false` | show what would be updated without writing lock |
| `--format <string>` | local | `text` | output format: text or json |
| `--frozen-lockfile` | local | `false` | abort if depengine.lock does not exist |
| `-h, --help` | local | `false` | help for update |
| `--json` | local | `false` | shorthand for --format=json |
| `--lock <string>` | local | `` | path to depengine.lock (default: alongside schema.toml) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--profile <string>` | local | `` | only re-resolve tools with matching tag; other pins from a readable lock are preserved |
| `--prune` | local | `false` | remove obsolete lock metadata for tools absent from the full effective schema |
| `--quiet` | local | `false` | suppress progress and the Next hint |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `-v, --verbose` | local | `false` | include the resolved lock projection in the output |

## `depengine upgrade [flags]`

Upgrade installed tools to the targets pinned in depengine.lock

Examples:

```sh
  depengine upgrade             # upgrade drifted tools to the locked targets
  depengine upgrade --only nvim
  depengine upgrade --dry-run   # show the exact targets without upgrading
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--allow-arbitrary-code` | local | `false` | permit hooks, build scripts, and other arbitrary code execution |
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--dry-run` | local | `false` | show what would be upgraded without making changes |
| `--force` | local | `false` | skip confirmation prompt |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for upgrade |
| `--json` | local | `false` | shorthand for --format=json |
| `--lock <string>` | local | `` | lockfile to read (default: derived from schema path) |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--only <string>` | local | `` | only upgrade specific tool |
| `--quiet` | local | `false` | suppress per-tool status lines |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--skip <string>` | local | `` | skip specific tools (comma-separated) |

## `depengine validate [flags]`

Validate schema.toml

Aliases: `lint`.

Examples:

```sh
  depengine validate                     # parse and validate schema.toml
  depengine validate --strict            # warnings become errors
  depengine validate --check-env --format json
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--check-env` | local | `false` | check system environment for required tools |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for validate |
| `--json` | local | `false` | shorthand for --format=json |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |
| `--strict` | local | `false` | treat warnings as errors |

## `depengine version [flags]`

Show version

Visibility: hidden (documented compatibility command).

| Flag | Scope | Default | Description |
|---|---|---|---|
| `-h, --help` | local | `false` | help for version |

## `depengine why [tool] [flags]`

Explain how a tool would be installed

Examples:

```sh
  depengine why nvim --fields   # method, provenance and catalog impact
  depengine why nvim --json
  depengine why --all           # one line per tool in the schema
```

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--all` | local | `false` | explain every tool in the schema, one line each |
| `--catalog <stringArray>` | local | `[]` | path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog) |
| `--fields` | local | `false` | show the full audit: recipe identity, field provenance, selected candidate and catalog-update impact |
| `--format <string>` | local | `text` | output format: text or json |
| `-h, --help` | local | `false` | help for why |
| `--json` | local | `false` | shorthand for --format=json |
| `--manifest <string>` | local | `` | path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml) |
| `--no-catalog` | local | `false` | disable the recipe catalog (default: auto-detect) |
| `--no-manifest` | local | `false` | disable personal manifest (default: auto-detect) |
| `--schema <string>` | local | `schema.toml` | path to schema.toml |

<!-- END GENERATED CLI REFERENCE -->

`status` and `sbom` accept a missing `depengine.lock` as normal, but fail with a runtime error when an existing lockfile cannot be read or is invalid. `status` still degrades to state-only reporting when the schema cannot be loaded, provided its lockfile is readable.

For non-frozen installs using a missing or v1 lockfile, depengine resolves and validates the legacy lock before executing tools. If resolution fails, installation stops; the successful preflight identity is the one saved after execution.

## Mutation journal

`install`, `remove`, `upgrade`, `update`, and `undo` append one redacted
record per tool outcome to `journal.jsonl` next to `state.json`. Records carry
the UTC timestamp, trace id (`DEPENGINE_TRACE_ID`), command, tool, method,
result, exit code, elevation decision, effective schema hash, and the resolved
package/version/revision/source identity. Dry runs are not recorded because
they do not mutate the host.

The file is append-only, owner-only (`0600`), written under its own lock so it
cannot deadlock against a held state lock, and rotated to `journal.jsonl.1` at
8 MiB, replacing the previous rotated generation. Queries hold the shared
journal lock while they read both generations, so a rotation cannot drop one
generation between reads and a concurrent append cannot expose a partial line.
Malformed lines, and records whose `format_version` this build does not
understand, are skipped so one unreadable record cannot hide the rest of the
trail. Secrets are redacted before persistence.

`depengine journal` queries it:

```sh
depengine journal                          # recent mutations
depengine journal --tool nvim --since 7d   # one tool, last week
depengine journal --result failed --json   # failures as machine output
```

| Flag | Default | Description |
|---|---|---|
| `--tool` | `` | only entries for this tool |
| `--command` | `` | only entries from `install`, `remove`, `upgrade`, `update`, or `undo`; other values fail |
| `--result` | `` | only entries with this result (`success`, `already`, `failed`, `skipped`, `updated`, `removed`, `reverted`); other values fail |
| `--since` / `--until` | `` | RFC3339, `YYYY-MM-DD`, or a duration such as `7d`/`24h`, relative to now |
| `--limit` | `50` | most recent matching entries; `0` means all |

## Dry-run plan preview

With `install --dry-run`, each resolved candidate reports the install step's
non-executable preview inside `plan_intent.operations[]`:

| Field | Meaning |
|---|---|
| `preview` | Exact argv the adapter will run, when it knows it before execution; absent otherwise |
| `elevation` | `none`, `required`, or `unknown`; omitted for read-only steps |

`preview` is descriptive only: executable argv still lives in `command` and
stays gated by `--allow-arbitrary-code`, so showing it never turns an install
into arbitrary code. Native package managers, Chocolatey, and HTTP-backed
adapters fill the fields; adapters whose command depends on runtime paths (git,
scoop, container, ecosystems) report `unknown` instead of guessing. The text
output prints the same lines under the per-tool `would install` line.

## `why` audit output

`why --fields` and `why --json` render the same audit report. The text view
prints it after the candidate list; the JSON view is the only stdout payload.
The report answers, for one tool:

- which catalog recipe supplied it (winning root index, root path, recipe path,
  content digest) or `none` when the tool is project-only;
- which layer supplies each field (`catalog`, `manifest`, `schema` or `both`
  when every declaring layer still contributes), the effective value, and which
declared layers were overridden;
- every candidate considered, in executor order, with its status and a stable
  `reason_code`;
- the candidate that would be selected and why;
- what a catalog update would still change, and which fields are shielded by a
  more specific layer.

`why --json` emits one object under the machine-output `data` field with these
stable keys:

| Key | Type | Meaning |
|---|---|---|
| `tool` | string | Requested tool name |
| `recipe` | object or absent | `name`, `layer`, `root_index`, `root`, `path`, `digest` |
| `field_provenance` | array | `field`, `origin`, `effective`, `layers`, `overridden` |
| `catalog_update` | object or absent | `affects_tool`, `fields`, `overridden_fields` |
| `candidates` | array | `kind`, `label`, `candidate`, `candidate_known`, `status`, `reason_code`, `detail`, `intent`, `plan_intent` |
| `selected` | object or absent | `kind`, `label`, `candidate`, `status`, `reason` |

`reason_code` is the machine-readable class of a candidate outcome and is never
derived from `detail` text: `when_not_matched`, `no_distribution`,
`capability_mismatch`, `no_adapter`, `adapter_unavailable`, `package_not_found`,
`host_incompatible`, `plan_resolution_failed`, `specialization_failed`,
`verify_failed`, `state_unresolved`, `source_resolution_failed`,
`source_probe_failed`, `blocked_by_terminal_error`, `method_only` and `virtual`.
Secret references are reported as `<secret reference>` and credential-bearing
values stay redacted.

`why --all` runs the same audit for every tool in the effective schema and
prints one line per tool: the selected candidate, the reason it won, and how
many earlier candidates were rejected. `why --all --json` emits an array of
the per-tool objects described above, ordered by tool name. `--fields` adds the
full audit block under each line.

## Adopting an existing project

`depengine init --from-state` and `--from-manifest` generate `schema.toml` from
installation knowledge that already exists instead of asking you to retype tool
names:

- `--from-state [path]` adopts tracked installations (default: the state file),
  recording the method that installed each tool and its package/artifact
  config.
- `--from-manifest [path]` adopts personal manifest entries (default:
  `~/.config/depengine/manifest.toml`).

Both modes print the result with `--dry-run`, merge into an existing schema
without rewriting declared tools or unrelated comments, and leave `# TODO`
comments where the source does not record enough to declare a method. Alias
labels are normalized to their canonical method kind, and the original provider
is kept as a comment. Review the TODOs before committing: adoption records what
was installed, not necessarily the minimum method set the project should
declare. The two flags cannot be combined with each other, with `--interactive`,
or with `--add`.

## Vocabulary lookup

`depengine explain [term...]` answers questions about the vocabulary offline:
fields (`requires_when`, `distro_version_min`, `arch_map`), installation
methods (`http`, `github`, `aur`, native manager aliases such as `apt`), and
the error codes `validate`/`install --dry-run` already emit (`E_DUPE_TOOL`,
`W_AUTO_CHECKSUM`) together with the resolver error classes
(`unavailable_candidate`, `auth_requirement`).

Each answer carries a one-line definition, the valid form, a minimal example,
and the document anchor that explains it in full. The command reads the
generated JSON Schema and the method contracts only: it does not load
`schema.toml`, probe the host, or touch the network. With no argument it lists
every known term; an unknown term exits `2` and suggests close matches.

The `why` command no longer answers to `explain`; that name now belongs to this
command.

## Guided tutorial

`depengine tutorial` creates a disposable project in a temporary directory,
writes a schema declaring one HTTP tool with a fixed checksum, and walks
`validate`, `why`, `update --dry-run` and `install --dry-run`, printing each
output with a one-line explanation. It never mutates the host, isolates state
inside the temporary directory, and removes it afterwards. `--keep` preserves
the directory for inspection, and `--json` emits the steps under the
machine-output envelope with `<tutorial>` placeholders instead of random paths.

## `audit` consistency gate

`audit` answers "is this project consistent?" without changing anything. It
runs five checks and reports one finding per divergence, each with a suggested
remediation:

| Check | Verifies |
|---|---|
| `schema` | The effective schema parses and validates |
| `layers` | Manifest/catalog merge facts and layer warnings |
| `lock` | `depengine.lock` is readable, decodable, and still matches the schema; a method, package-source, or recipe change reports `E_LOCK_STALE` |
| `state` | Tracked tools are installed, not outdated, and their host identity is verifiable; state entries missing from the schema are reported |
| `ownership` | Retained zero-ref prerequisite records and dangling dependent claims whose tool is no longer tracked are reported with the `depengine forget <tool>` repair command |

Exit codes aggregate the checks: `0` without errors (warnings alone do not
fail), `1` with at least one error, and `--strict` promotes warnings to
failures. An invalid schema exits `2`; a runtime failure exits `3`. `--json`
returns the report under the machine-output envelope with `findings` and
`summary` counts, so a CI gate can assert on codes instead of text.

## Machine output

Every command that renders machine-readable output accepts `--format json` and
the `--json` shorthand, and writes exactly one JSON document to stdout. Human
progress and warnings stay on stderr. The document is a versioned envelope:

```json
{
  "version": 1,
  "command": "status",
  "data": []
}
```

| Field | Meaning |
|---|---|
| `version` | Envelope version, independent of the schema, lock, and state format versions |
| `command` | Canonical command path that produced the payload (`bundle export` for subcommands) |
| `data` | Command-specific payload |

New fields are added inside `data`; envelope fields do not change. Payload
shapes by command:

| Command | `data` |
|---|---|
| `audit` | `schema`, `lock_path`, `lock_version`, `findings`, `summary` |
| `bundle export` | `lock_path`, `output`, `artifacts` |
| `check` | one `tool`, `method`, `status`, `verification` object |
| `diff` | array of changed fields |
| `doctor` | report object |
| `explain` | `query`, `matches` |
| `forget` | `tool` |
| `import` | `outcome` (`previewed`, `written`, `rejected`, `failed`), `would_replace`, `name`, `source`, `destination`, `digest`, `candidates`, `executable`, `warnings`, `errors` |
| `install` | execution report |
| `journal` | `path`, `count`, `entries` |
| `remove` | `dry_run`, `removed`, `failed` |
| `status` | array of tool status entries |
| `tutorial` | `schema`, `directory`, `steps` |
| `undo` | `snapshot`, `removed`, `failed`; with `--list`, `snapshots` |
| `update` | `lock_path`, `lock_version`, `dry_run`, `pinned`, `tools` |
| `upgrade` | counters and per-tool `results` |
| `validate` | `errors`, `warnings` |
| `why` | audit object; an array with `--all` |

For `import`, a valid `--dry-run` returns `previewed` even when the destination
already exists; `would_replace` identifies that collision without modifying
the file. A real import without `--force` returns `rejected` and exit code `2`
when the destination exists. Validation errors are also reported on stderr.
A filesystem write failure returns `failed` with an `errors` entry in the JSON
report and exit code `3`. If writing stdout fails after the recipe is saved,
exit code `3` does not imply the recipe was rolled back; inspect the destination
before retrying.

An unknown `--format` value exits `2`. Schema-loading failures still produce an
envelope, with `E_SCHEMA_*` codes inside `data`. `sbom` and `graph` keep their
command-specific format families (`--format spdx|cyclonedx` and
`--format text|graph`) because their output is not a generic record shape.

## Flag conventions

Flags keep the same name and meaning across commands. A command omits a flag
only when the behavior does not apply, and the exceptions are deliberate:

| Family | Flags | Contract |
|---|---|---|
| Layer scope | `--schema`, `--manifest`, `--no-manifest`, `--catalog`, `--no-catalog` | Project commands resolve the same three layers. `remove` is state-first and uses `--schema` only for legacy v1 pin compatibility. |
| Tool selector | `--only`, `--skip`, `--profile` | Selectors are validated against the effective schema before execution; a value that matches nothing exits `2`. |
| Progress | `--quiet` | Suppresses progress and auxiliary headers, never errors, warnings, or the primary result. |
| Confirmation | `--force` | Skips an interactive confirmation. Only commands that prompt (`remove`, `upgrade`) accept it. |
| Detail | `--verbose` | Adds diagnostic detail. On `install` detailed output is already the default, so the flag is a deprecated no-op with a documented removal window. |
| Machine output | `--format text\|json`, `--json` | Every command that emits records accepts both flags; output is the versioned envelope described above. `--json` is a shorthand for `--format json`. |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Tool failure / strict mode warnings |
| `2` | Schema error (invalid TOML, validation) |
| `3` | Runtime error (subprocess, filesystem, or other infrastructure failure) |

See [troubleshooting](troubleshooting.md) for the cause, fix, and diagnostic
command behind each message.


## Environment variables

| Variable | Purpose |
|---|---|
| `DEPENGINE_LANG` | Opt-in CLI language for command summaries (`en` default, `pt`). Locale variables are ignored; flags, errors and logs stay English. |
| `DEPENGINE_DETECT_SCRIPT` | Deprecated explicit legacy host-detector override; normal detection is native Go |
| `DEPENGINE_MANIFEST` | Override the personal manifest path |
| `DEPENGINE_CATALOG` | Override catalog roots (path list; `--catalog` wins) |
| `DEPENGINE_CACHE_MAX_BYTES` | Download-cache size limit; `0` disables eviction |
| `DEPENGINE_DOWNLOAD_MAX_BYTES` | Maximum bytes accepted per HTTP artifact download (default 4 GiB; positive values override) |
| `XDG_CONFIG_HOME` | Base directory for the personal manifest and catalog |
| `XDG_CACHE_HOME` | Base directory for downloads |
| `XDG_STATE_HOME` | Base directory for state |
| `XDG_DATA_HOME` | Unix base directory for user-scope tool placement |
| `GITHUB_TOKEN` / `GH_TOKEN` | GitHub API and private release authentication |
| `NO_COLOR` / `FORCE_COLOR` | Control ANSI color output |
| `DEPENGINE_TRACE_ID` | Trace ID passed to subprocesses |
| `DEPENGINE_LOG_JSON` | Set to `1` for JSON logs |

When no home or XDG base is available, state and download-cache operations use
a checked per-user private fallback (Unix system-temp root; OS-provided per-user
cache location on Windows and other platforms). If that location is unavailable
or cannot be validated, the operation reports a runtime filesystem error rather
than writing to a shared temporary path or the current working directory.
Secret-bearing environment variables referenced through typed `secret_ref`
configuration are resolved only for the operation that owns them; see
[security](security.md) for credential-scoping rules.
