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
internal/graph       order tools, reject cycles, render graph views
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
| `internal/graph` | Scheduling order, cycle detection, view projections, weak-component analysis, and graph renderers (text, mermaid, dot, terminal diagram) |
| `internal/exec` | Plan execution, adapter registry, candidate fallback, install reports |
| `internal/run` | Subprocess interface used by production code and tests |
| `internal/term` | Terminal geometry detection for width-aware output |
| `internal/platform` | Native host detection, host facts, distro-family classification, and host-version comparison |
| `internal/engine` | Host-fact gathering boundary plus deprecated `DEPENGINE_DETECT_SCRIPT` compatibility |
| `internal/native` | Native package-manager definitions |
| `internal/ecosystem` | Cargo, Go, Python, Node, Flatpak, Snap, and other ecosystem adapters |
| `internal/git` | Git clone and build installs |
| `internal/httpdownload` | Download, extraction, placement, checksum/signature checks |
| `internal/source` | Package-source setup such as PPA, COPR, Brew taps, and Scoop buckets |
| `internal/scoopruntime` | Scoop-specific runtime operations shared by Windows package execution and bucket source management |
| `internal/msi` | Windows MSI install/remove lifecycle |
| `internal/lock` | `depengine.lock` persistence and validation, with bounded legacy v1 resolution |
| `internal/state` | Installed-tool state and cross-platform file locking |
| `internal/validate` | Structural, semantic, and environment validation |
| `internal/sbom` | CycloneDX and SPDX export |

## Adapter boundary

Install methods implement `internal/exec.AdapterV2` and are registered
explicitly during startup. Each adapter resolves an intent to a concrete
`ResolvedInstallPlan`; observation and installation then use that resolved
identity. The executor looks adapters up by method kind and does not call
package managers directly.

Tests and callers can supply per-executor adapters with `WithAdapters()`.
Production uses the process registry populated by `app.InitAdapters()`.

An `Executor` holds configuration shared across its calls, including its adapter
snapshot, per-instance overrides, and an owned copy of the configured method order.
Each `Execute` creates a fresh `runContext` with its effective clan, native manager,
and method order: a schema override applies only to that run; otherwise the
configured order applies. The context also owns the schema, report, failure and
recovery state, dependency maps, and source manager used for candidate preparation.
Host selection and source observations are not retained on the executor.

`ExplainTool` keeps its attempts-only caller contract and creates a fresh,
read-only `source.Manager` for each call. The universal lock resolver uses
`ExplainToolWithSourceRevisions(ctx, tool, clan)` to obtain method attempts and
source revisions together, without retaining source observations on the executor.

Read-only selection and candidate resolution receive the clan explicitly.
`SelectedMethods(tool, clan)` uses the configured order without modifying the
executor; resolution and verification apply native package overrides for that
call's clan. Status, upgrade, removal, and undo do not depend on a preceding run.

Subprocesses go through `internal/run.Runner`. Adapters should not call
`exec.Command` directly.

`platform.ResolveFamily()` classifies the host without inspecting installed
manager binaries. The `native` method selects one default provider for that
family. Competing managers, such as winget, Scoop, and Chocolatey, are explicit
method candidates; their order and fallback remain visible in the plan.
Binary variants may share a provider only when package identity and behavior
match, as with dnf and dnf5. The `scoop` method and `scoop-bucket` source
kind share `internal/scoopruntime`: it owns official Scoop command construction,
output decoding into semantic package/bucket records, resolved package
operations, and bucket repository location. Runtime capability checks gate each
requested version, bucket, scope, architecture, removal, or revision/location
requirement before mutation. The composition helpers allow package and source
consumers to receive the same selected runtime instance; `exec.WithScoopRuntime`
selects it for the built-in adapter and executor-owned source managers. Existing
constructors retain the official runtime as the default. Application
composition currently uses only Official, including app-owned source cleanup;
`WithScoopRuntime` is not an application-level runtime selector.

## Configuration boundary

`internal/config` parses files and validates schema structure, but it does not
detect the current OS or import the executor. Host facts come from
`internal/platform` and are passed where needed. The executor fills missing transient
target architecture and OS metadata from those facts during candidate resolution,
so state-restored candidates can resolve host-specific artifacts without persisting
machine-specific values.

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

## Lock, status, and upgrade flows

For lock v2, `LockDocument` is the operational resolved-identity authority.
When producing a v2 document, `depengine update` obtains candidate plans
through the executor's read-only resolution path and `AdapterV2`, then persists
them only with complete supported coverage. Profiled updates over v1 remain on
the legacy path rather than writing partial v2 coverage. `MethodsHash` and `SourceHash` remain
requested method/source-intent metadata used for frozen validation and update
drift checks; they are not a second concrete-identity resolver. Legacy v1
method pins remain readable and are applied only through the bounded v1
compatibility path.

For a retained v2 projection entry, `update --profile` refreshes the entry
when its `MethodsHash` or source metadata is missing or differs from the
current schema. Source metadata keys are parsed from the right so tool names
containing slashes remain intact. V2 lock construction uses
`lock.NewUniversal`; compatibility `ToolPin` payloads do not override the
projection. `MethodsHash` records candidate kind/label intent, not an HTTP URL
or artifact identity.

`status` loads the v2 document into the executor and reconciles observation
against that locked resolved target directly. Its v1 path retains legacy pin
application.

`upgrade` captures the tracked `ToolState` during discovery and passes it to the
required `ExecuteResolvedUpgradeCandidate` API. That API deep-clones its config
before run initialization, recovery, or prerequisite work can mutate shared input.
Under the state lock, the executor deep-compares current tracked state with that
discovery snapshot before creating replacement WAL or removing anything. It
resolves the old schema candidate with `config.FindMethodCandidate` using its
persisted method kind and label rather than reconstructing it from the desired
candidate. The executor owns replacement: a checksummed state WAL records
intent, exact old/new candidate identities, and resource-use claims before
removal. It persists
each removal/install boundary before host mutation and installs and verifies the
resolved target. Verified tool state, release of old dependent claims, and new
replacement resource ownership are committed atomically while the replacement
WAL remains active; recovery uses
the persisted old state and target rather than resolving a new candidate. The
after-upgrade hook has its own persisted boundary. The postinstall-completion
flag and WAL removal are saved together, including when the hook reports
failure. If the process stops after that
boundary but before the result is saved, recovery blocks rather than replaying a
hook with unknown side effects.

The `sbom` command reports a concrete version already recorded in state first.
If absent, it uses a matching-method immutable v2 projection entry; only a v1
lock may supply a legacy method pin. Malformed or unavailable lock data is
best-effort and does not prevent export; unknown versions remain `0.0.0`.

## Desired-state observation

Consumers share one read-only path for a resolved candidate:

```text
ResolvePlan → Observe resolved target → plan.Reconcile → VerificationResult
```

Install idempotency, `status`, `check`, `why`, upgrade preflight, and remove
consume that result. Presence alone does not establish satisfaction: known
identity fields must match the resolved plan. `absent` and `drifted` remain
distinct; `unknown` means the adapter cannot prove the desired identity, while
`broken` means observation data or verification is invalid. Upgrade and remove
fail closed for unknown/broken targets. Remove accepts drift, and a proven
absent target releases state and ownership without invoking the remover.

Desired-state verification uses the normative [state model](design/state-model.md) for
reconciliation states, authority boundaries, lock coverage, preparation journals, and
ownership invariants.

## Bootstrap and adapter snapshots

`app.InitAdapters()` registers production adapters in the default registry during startup. Each
`Executor` snapshots that registry when constructed; `WithAdapters` supplies per-instance
overrides, which take precedence over the snapshot. The default registry is populated during
bootstrap; runtime-specific adapter choices belong to the executor instance. Adapter authors
should follow the [adapter authoring contract](adapter-authoring.md).

## Process-wide defaults

A few defaults are shared for one process:

| Default | Reason |
|---|---|
| default adapter registry | Composition root registers production adapters; executors snapshot it at construction |
| elevation config | One CLI invocation uses one elevation policy |
| GitHub release resolver | Reuses release lookups during one run |
| logger | One process writes one log stream |

Tests can create isolated instances where isolation matters, including a per-executor adapter override.

## Checks

```sh
go test ./...
go vet ./...
go build -o depengine .
```

The integration suite under `tests/integration` is slower and needs containers
plus network access.
