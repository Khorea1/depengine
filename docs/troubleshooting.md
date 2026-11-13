# Troubleshooting

Start with the diagnostic command, which never mutates the host:

```sh
depengine doctor
depengine doctor --json
```

`doctor` reports the detected host, the schema/manifest/catalog layers it
found and where they came from, available native managers, elevation
availability, and the lockfile state. Add `--log-level debug` to any command
for the resolution trace, and set `DEPENGINE_TRACE_ID` to correlate
subprocesses in the logs.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Tool failure / strict mode warnings |
| `2` | Schema error (invalid TOML, validation) |
| `3` | Runtime error (subprocess, filesystem, or other infrastructure failure) |

`0` means the requested work completed. `depengine install` still exits `0`
when the plan is empty; use `--only`, `--skip`, or `--profile` typos as a
signal instead, because a selector that matches nothing now exits `2`.

## Find your message

Search this table for the message you saw. Each entry names the exit code,
the usual cause, the fix, and the command that shows more detail.

| Message | Exit | Cause | Fix | Diagnose |
|---|---|---|---|---|
| `schema %s not found` | 2 | No project schema at the selected path | Run `depengine init`, or pass `--schema <path>` | `depengine doctor` |
| `Run 'depengine init' to create one` | 2 | Same missing-schema condition, shown as guidance | Create a schema or point `--schema` at one | `depengine doctor` |
| `multiple schema files found` | 0 | More than one auto-detected schema candidate | Pass `--schema` explicitly or remove the extra files | `depengine doctor` |
| `--only matched no tool` | 2 | `--only` names a tool the effective schema does not declare | Fix the name or add the tool to `schema.toml` | `depengine validate` |
| `--skip matched no tool` | 2 | `--skip` names a tool the effective schema does not declare | Fix the name or drop it from `--skip` | `depengine validate` |
| `--profile matched no tool` | 2 | `--profile` names a tag no tool declares | Use a declared tag or add it to the tool | `depengine validate` |
| `nothing to do` | 0 | The resolved plan is empty | Check selectors and the schema tool list | `depengine install --dry-run` |
| `--frozen-lockfile requires a readable lockfile` | 2 | The lockfile is missing or unreadable | Run `depengine update` and commit `depengine.lock` | `depengine doctor` |
| `--frozen-lockfile requires lockfile` | 2 | `--frozen-lockfile` was used without a lockfile | Run `depengine update` first | `depengine update --dry-run` |
| `--frozen-lockfile requires an up-to-date supported lock` | 2 | Schema intent or resolved identity drifted from the lock | Re-run `depengine update` and review the change | `depengine diff` |
| `lockfile cannot pin catalog recipes` | 2 | Catalog recipes need a v2 lock | Run a whole-schema `depengine update` | `depengine doctor` |
| `lock: unsupported version` | 2 | The lockfile format is newer than this binary | Upgrade depengine or regenerate the lock | `depengine doctor` |
| `lock: package or registry changed for` | 2 | A package or registry identity changed since the lock was written | Run `depengine update` to accept the change | `depengine why <tool>` |
| `lock: package, registry, or version request changed for` | 2 | A package version or registry request changed since the lock was written | Run `depengine update` to accept the change | `depengine why <tool>` |
| `this method requires a config table` | 2 | A method was written as a scalar or list where a table is required | Use `tool = { native = true }` or a `[tools.tool.method]` block | `depengine validate` |
| `expected table, got` | 2 | A section or tool was written with the wrong TOML shape | Use the table form shown by `depengine explain` | `depengine validate` |
| `unknown field` | 2 | A key is not part of the schema | Fix the spelling or remove the key | `depengine validate` |
| `unknown method kind` | 2 | A method name is not a registered adapter | Use a supported method name | `depengine validate` |
| `cargo: git and version are mutually exclusive` | 2 | A Cargo method sets both `git` and `version` | Keep one selector | `depengine validate` |
| `artifact: url and repo+asset are mutually exclusive` | 2 | An artifact method sets both `url` and `repo`/`asset` | Keep one source form | `depengine validate` |
| `--catalog and --no-catalog are mutually exclusive` | 2 | Both catalog flags were passed | Pass only one | `depengine doctor` |
| `catalog root %q must be absolute` | 2 | A catalog root is relative | Pass an absolute path | `depengine doctor` |
| `bundle export requires a readable v2 lockfile` | 2 | The lock is missing, unreadable, or still v1 | Run `depengine update` first | `depengine doctor` |
| `--output is required` | 2 | `bundle export` has no destination | Pass `--output <dir>` | `depengine bundle export --help` |
| `looks like an installer script` | 2 | A download method points at `.sh`/`.ps1`/`.bat` and similar payloads | Declare a native package or the underlying managed artifact | `depengine validate` |
| `requires --allow-arbitrary-code` | 2 | Hooks, build commands, or other arbitrary code are declared without authorization | Re-run with `--allow-arbitrary-code` after reviewing the code | `depengine install --dry-run` |
| `No tools in state (nothing installed yet)` | 0 | `status` ran before anything was installed | Run `depengine install` or pass `--schema` | `depengine doctor` |
| `No tools in state. Run 'depengine install' first.` | 0 | `status` has no tracked tools to compare | Run `depengine install` | `depengine doctor` |
| `No orphan tools.` | 0 | `status --orphans` found nothing outside the schema | No action needed | `depengine status` |
| `default native manager` | 1 | `doctor` cannot find the clan's default manager on `PATH` | Install the manager or choose another method | `depengine doctor` |
| `native installs need elevation` | 1 | `doctor` found no working `sudo`/`doas`/`pkexec`/`run0` | Install an elevation helper or run as root | `depengine doctor` || `--json is deprecated; use --format=json instead` | 0 | A command still accepts the legacy `--json` shorthand | Switch scripts to `--format json` | `depengine status --help` |
| `--verbose is deprecated` | 0 | Detailed output is the default now | Use `--quiet` to suppress per-tool lines | `depengine install --help` |

## Diagnose by exit code

- **Exit 1**: the command ran but the work failed. Read the per-tool lines and
  the summary; `--quiet` hides progress but not failures. In `validate`,
  `--strict` turns warnings into exit `1`.
- **Exit 2**: the input is invalid or the request cannot be planned. Run
  `depengine validate` for schema problems and `depengine doctor` for layer or
  host-discovery problems.
- **Exit 3**: an external dependency failed, such as a subprocess, filesystem,
  state lock, or network operation. Re-run with `--log-level debug` and check
  `DEPENGINE_TRACE_ID` in the logs.

## Still stuck

Collect the diagnostic bundle before opening an issue:

```sh
depengine doctor --json > doctor.json
depengine validate --format json > validate.json
depengine install --dry-run --json > plan.json
```

These files contain host facts, layer paths, and the planned operations, which
is enough to reproduce most problems without sharing state or credentials.
