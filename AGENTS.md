# AGENTS.md — depengine

## Project

depengine is a distro-agnostic dependency installer written in Go. Projects declare required tools in a schema; depengine resolves installation methods and executes them through adapters.

Primary sources of truth:

- `README.md`: user-facing behavior.
- `docs/architecture.md`: package boundaries and execution flow.
- `docs/schema-reference.md` and `docs/specs/`: schema and compatibility contracts.
- `docs/support-boundary.md`: supported artifact/lockfile boundaries.
- `go.mod`, `.golangci.yml`, `.goreleaser.yaml`, and CI: toolchain, release, and validation configuration.
- `schema.toml`: project dependency intent.
- `~/.config/depengine/manifest.toml`: operator-local installation knowledge.

## Core contracts

- Project schema values win over personal manifest values for conflicting fields.
- Manifest-only tools are ignored unless the manifest explicitly allows new tools.
- `depengine.lock` pins supported mutable artifact references according to `docs/support-boundary.md`.
- Keep the `Parse -> Graph -> Execute` separation described in `docs/architecture.md`.
- `internal/config` parses/validates declarations and must not depend on execution packages.
- Install execution belongs behind `internal/exec.Executor` and registered adapters. Do not hard-code package-manager commands in CLI code.
- External processes go through `internal/run.Runner` and the existing elevation abstractions; do not scatter direct `exec.Command` calls through application/adapter code.
- `internal/native` owns native-manager knowledge; `internal/state` owns persisted install state and locking.

## Canonical commands

```sh
go build -o depengine .
go test -race ./...
go vet ./...
golangci-lint run
```

CI is authoritative for pinned tool versions and the complete validation matrix.

Run `scripts/setup-hooks.sh` in each checkout to enable the local Git gates.
Before a commit, the staged tree must pass `go build ./...` and `go vet ./...`.
Before a push, each distinct commit tip being sent must pass those commands,
`go test -race ./...`, and `golangci-lint run`. Install Go and golangci-lint
before pushing. These hooks supplement CI and do not validate intermediate
commits that were created without the commit hook.

Container-based integration suites under `tests/` are slower and require Docker or Podman plus network access. Run them when a change affects real installation behavior rather than in every unit-test loop.

## Security and execution

Schema fields such as `pre_install`, `post_install`, `build`, and `build_cmd` can execute arbitrary commands. Shell-string forms invoke a shell; prefer argv-form execution where supported. Arbitrary-code execution remains gated by the CLI's explicit authorization mechanism.

For archive payloads, preserve the ownership/removal model documented in the schema reference. Removal must not delete shared parent directories.

Installer formats that have dedicated/native handling must not be forced through the generic HTTP adapter when the schema reference excludes them.

OS-detection changes must consider every supported clan exposed by `internal/native.KnownClans()`; do not update a single distro path in isolation when the shared detection contract changes.

## Local state

Do not treat local manifests, state files, caches, or `.dev/` scratch material as shared repository truth.

## Known compatibility facts

Keep detailed compatibility rules in their authoritative docs. In particular:

- project schemas currently require `schema_version = 1`;
- project schemas use `[tools]`, personal manifests use `[packages]`;
- method-selection semantics and inferred native candidates are defined in `docs/schema-reference.md`;
- mutable release-asset behavior and lockfile coverage are defined in `docs/support-boundary.md`.

Do not reintroduce legacy parsing or aliases unless the relevant compatibility spec changes.

## Maintaining this file

Keep only durable depengine-specific constraints here. Prefer links to architecture/spec/schema documents over copied detail.
