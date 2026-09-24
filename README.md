<h1 align="center">depengine</h1>

<p align="center">
  <b>Install the tools a project needs without tying the project to one distro.</b>
</p>

<p align="center">
  <a href="https://github.com/Khorea1/depengine/actions"><img src="https://github.com/Khorea1/depengine/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-GPL--3.0--or--later-blue" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.27.1-blue" alt="Go 1.27.1">
</p>

depengine reads a project-owned `schema.toml`, resolves install candidates for
the current host, and installs the declared tools through native package
managers, language ecosystems, release artifacts, Git builds, and other typed
adapters.

The project schema describes intent; the host decides which compatible method
can satisfy it. depengine keeps candidate selection, dependency ordering,
desired-state checks, lock data, and installed state explicit instead of hiding
them in shell scripts.

```sh
depengine init --add "zsh,bat,nvim,ruff"
depengine validate
depengine install
depengine status
```

The release binary is self-contained. Individual install methods still depend on
the package manager, runtime, or toolchain they invoke.

## Quick start

Create a project schema:

```toml
schema_version = 1

[tools]
simple = ["zsh", "bat", "kitty"]

# Native package names may vary by package manager.
fd = { apt = "fd-find" }

# Ecosystem buckets expand to compatible typed methods.
ruff = { python = true }
```

Then validate and install it:

```sh
depengine validate
depengine install
depengine status
```

Commit `schema.toml` with the project. Contributors can use the same declared
tool set while depengine selects compatible methods for their host.

For conditions, hooks, dependencies, archives, GitHub assets, checksums,
platform targeting, and every supported method, use the
[`schema.toml` reference](docs/schema-reference.md).

## How resolution works

At a high level, depengine:

1. parses and validates the project schema and optional personal manifest;
2. builds the dependency graph and rejects invalid dependency cycles;
3. orders each tool's compatible install candidates;
4. resolves the selected candidate to a concrete install plan;
5. observes the target and installs only when reconciliation requires it;
6. records state and, where supported, lock information.

Candidate priority can be changed with `method_prefer`; `method_only` limits
a tool to an explicit set of candidates. Use `depengine why <tool>` to inspect
the decision on the current host.

The implementation model and package boundaries are documented in
[architecture](docs/architecture.md).

## Schema and personal manifest

depengine can merge project intent with operator-local installation knowledge:

| File | Purpose | Commit it? |
|---|---|---|
| `schema.toml` | Project dependencies and project-owned install rules | Yes |
| `~/.config/depengine/manifest.toml` | Personal recipes and machine defaults | No |

Project values win when both layers set the same field. By default,
manifest-only tools are ignored; set `[manifest] allow_new_tools = true` in
the manifest to opt into adding them to a project run.

Project schemas use `[tools]`; personal manifests use `[packages]`.

See [manifest merge rules](docs/schema-reference.md#manifest-merge-rules) for
field-level behavior.

## Common commands

| Command | Purpose |
|---|---|
| `depengine init` | Create `schema.toml` |
| `depengine validate` | Validate configuration without installing |
| `depengine install` | Install declared tools |
| `depengine status` | Reconcile tracked state with the host |
| `depengine check <tool>` | Reconcile one tool |
| `depengine why <tool>` | Explain candidate selection and resolved intent |
| `depengine graph` | Show tool dependencies |
| `depengine update` | Resolve supported mutable references into `depengine.lock` |
| `depengine upgrade` | Upgrade tracked tools using supported lock data |
| `depengine remove <tool>` | Remove a tracked tool |
| `depengine forget <tool>` | Drop state without uninstalling |
| `depengine undo` | Restore a previous install snapshot |
| `depengine sbom` | Export CycloneDX or SPDX SBOM data |

The complete command and flag reference is in
[`docs/cli-reference.md`](docs/cli-reference.md).

## Install methods

depengine supports native package managers, language package managers, desktop
and specialized package ecosystems, containers, local artifacts, Git builds,
GitHub releases, and direct HTTP artifacts.

The authoritative method list, accepted fields, shorthand forms, and lifecycle
semantics live in
[the schema method reference](docs/schema-reference.md#method-reference).
Keeping that detail in one place prevents the README and implementation from
drifting independently.

## Reproducibility and security

`depengine.lock` improves reproducibility for methods whose mutable identities
can currently be resolved and represented by the lock model. Coverage is not
universal: some package-manager and ecosystem installs still resolve through
their own registries at execution time.

```sh
depengine update
depengine install --frozen-lockfile
```

`depengine install --dry-run` plans the operation without running install
adapters or hooks and without writing state, lock data, package-source changes,
or download-cache entries. Planning may still perform read-only probes and
network resolution.

Hooks, build commands, and other arbitrary-code paths require the explicit
`--allow-arbitrary-code` gate. Checksums, signatures, typed secret references,
credential transport, and package-source trust have method-specific boundaries.

Read [support boundaries](docs/support-boundary.md) for reproducibility and
lifecycle limits, and [security](docs/security.md) before enabling arbitrary
code or using mutable/private sources.

## Platform coverage

Linux has the broadest integration coverage. macOS and Windows run the Go test
suite on native CI runners. Windows supports winget, Scoop, Chocolatey, file
locking, and state handling, but package-manager lifecycle coverage is still
lighter than Linux.

Platform support does not imply identical package availability or lifecycle
semantics across managers. See
[the support boundaries](docs/support-boundary.md) and
[the schema reference](docs/schema-reference.md) for method-specific behavior.

## Format status

Project schemas, lockfiles, and installed state currently use version `1`, but
the v1 compatibility freeze is not complete yet. Until that freeze is accepted,
version `1` should not be read as a permanent backward-compatibility promise.

See [file-format compatibility](docs/compatibility.md) and the
[v1 freeze gate](docs/specs/format-v1-freeze.md).

## Editor support

[`schema/depengine.schema.json`](schema/depengine.schema.json) provides editor
completion and validation for `schema.toml`. For Taplo in VS Code:

```json
{
  "taplo.schema.enabled": true,
  "taplo.schema.url": "https://raw.githubusercontent.com/Khorea1/depengine/master/schema/depengine.schema.json"
}
```

## Documentation

| Document | Use it for |
|---|---|
| [`docs/schema-reference.md`](docs/schema-reference.md) | Schema syntax, install methods, conditions, hooks, and merge rules |
| [`docs/cli-reference.md`](docs/cli-reference.md) | Commands, flags, defaults, and exit codes |
| [`docs/cheatsheet.md`](docs/cheatsheet.md) | Copyable command and schema examples |
| [`docs/support-boundary.md`](docs/support-boundary.md) | Reproducibility and lifecycle guarantees |
| [`docs/security.md`](docs/security.md) | Trust, arbitrary code, checksums, signatures, and credentials |
| [`docs/compatibility.md`](docs/compatibility.md) | File-format version policy |
| [`docs/architecture.md`](docs/architecture.md) | Internal packages and execution flow |
| [`docs/design/`](docs/design/) | Accepted architecture decisions |
| [`docs/research/`](docs/research/) | Research supporting future design work |
| [`docs/development.md`](docs/development.md) | Documentation and development-note conventions |
| [`docs/roadmap.md`](docs/roadmap.md) | Long-lived unfinished work |

## Development

```sh
go test ./...
go vet ./...
go build -o depengine .
```

The slower distro integration suite lives under `tests/integration` and
requires Docker or Podman plus network access. CI is authoritative for the full
validation matrix and pinned tooling.

## License

[GNU General Public License v3 or later](LICENSE)
