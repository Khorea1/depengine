# Architecture

This page explains how depengine is split internally. For user-facing setup,
start with the [README](../README.md).

The main path is:

```text
schema.toml + manifest.toml
        |
        v
internal/config      parse, normalize, merge, validate
        |
        v
internal/graph       order tools and reject dependency cycles
        |
        v
internal/exec        choose candidates and run adapters
        |
        +--> internal/native
        +--> internal/ecosystem
        +--> internal/git
        +--> internal/httpdownload
        +--> other typed adapters
        |
        v
state / report / SBOM
```

`main.go` only wires the application together: signals, embedded assets,
adapter registration, and the final exit code. CLI behavior lives in
`internal/app`.

## Main packages

| Package | Responsibility |
|---|---|
| `internal/app` | Cobra commands and CLI workflows |
| `internal/config` | Parse project schemas and personal manifests, expand placeholders, merge layers, validate method kinds |
| `internal/graph` | Topological sorting and cycle detection |
| `internal/exec` | Plan execution, adapter registry, candidate fallback, install reports |
| `internal/run` | Subprocess interface used by production code and tests |
| `internal/platform` | Host facts and distro-family logic |
| `internal/engine` | Runs and parses `detect_os.sh` |
| `internal/native` | Native package-manager definitions |
| `internal/ecosystem` | Cargo, Go, Python, Node, Flatpak, Snap, and other ecosystem adapters |
| `internal/git` | Git clone and build installs |
| `internal/httpdownload` | Download, extraction, placement, checksum/signature checks |
| `internal/source` | Package-source setup such as PPA, COPR, Brew taps, and Scoop buckets |
| `internal/msi` | Windows MSI install/remove lifecycle |
| `internal/lock` | `depengine.lock` resolution and verification |
| `internal/state` | Installed-tool state and cross-platform file locking |
| `internal/validate` | Structural, semantic, and environment validation |
| `internal/sbom` | CycloneDX and SPDX export |

## Adapter boundary

Install methods implement `internal/exec.Adapter` and are registered explicitly
during startup. The executor looks them up by method kind; it does not call
package managers directly.

Tests can supply an isolated registry with `WithAdapters()`. Production uses
the process registry populated by `app.InitAdapters()`.

Subprocesses go through `internal/run.Runner`. Adapters should not call
`exec.Command` directly.

`platform.ResolveFamily()` classifies the host without inspecting installed
manager binaries. The `native` method selects one default provider for that
family. Competing managers, such as winget, Scoop, and Chocolatey, are explicit
method candidates; their order and fallback remain visible in the plan.
Binary variants may share a provider only when package identity and behavior
match, as with dnf and dnf5.

## Configuration boundary

`internal/config` parses files and validates schema structure, but it does not
detect the current OS or import the executor. Host facts come from
`internal/platform` and are passed where needed.

Project and personal configuration use the same grammar but different roots:
projects declare `[tools]`; manifests declare `[packages]`. Project values win
when the two layers conflict.

## Install flow

For each tool, in dependency order, the executor:

1. builds the candidate list;
2. skips candidates whose `when` condition does not match;
3. checks whether the adapter is available;
4. resolves lazy method dependencies and package sources only when that
   candidate is reached;
5. installs with the first candidate that succeeds;
6. records the resulting state and report data.

`method_prefer` changes candidate priority but keeps fallbacks. `method_only`
restricts the candidate list.

## Process-wide defaults

A few defaults are shared for one process:

| Default | Reason |
|---|---|
| adapter registry | Production has one registered adapter set |
| elevation config | One CLI invocation uses one elevation policy |
| GitHub release resolver | Reuses release lookups during one run |
| logger | One process writes one log stream |

Tests can create isolated instances where isolation matters.

## Checks

```sh
go test ./...
go vet ./...
go build -o depengine .
```

The integration suite under `tests/integration` is slower and needs containers
plus network access.
