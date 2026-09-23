# AGENTS.md — depengine

> A distro-agnostic dependency installer. Projects declare tools in `schema.toml`; depengine tries the configured installation methods until one succeeds. The application ships as a static Go binary.

## Scope

This file contains shared project instructions. Durable project knowledge belongs in `docs/`; operator-specific guidance and task scratch belong in local configuration or `.dev/`.

## Operating principles

- Solve the user's intent while preserving explicit constraints and project invariants. Explain material deviations from a suggested implementation.
- Verify consequential claims against relevant code, tests, configuration, documentation, and history. Do not turn incidental implementation details into policy.
- Make the smallest coherent change. Expand scope only to remove a blocker, preserve an invariant, fix the root cause, or prevent a concrete regression.
- Prefer changes that improve correctness, security, maintainability, performance, or developer experience without unrelated regressions.
- Make local, reversible decisions within scope. Surface material trade-offs involving behavior, compatibility, security, or architecture.

## Project overview

- Go module: `github.com/Khorea1/depengine`; toolchain requirements are defined in `go.mod`.
- `schema.toml` declares project tools; `~/.config/depengine/manifest.toml` holds personal recipes and defaults. Project values win on conflicting fields; manifest-only tools are ignored unless `[manifest] allow_new_tools = true`.
- `depengine.lock` pins supported mutable artifact references. Lockfile coverage and limits are documented in `docs/support-boundary.md`.
- See `README.md` for user-facing behavior and `docs/` for durable architecture, schema, security, and development details.

## Build, test, and validation

Run checks relevant to the change. Common checks are:

```sh
go build -o depengine .
go test -race ./...
go vet ./...
golangci-lint run
```

CI is authoritative for pinned tool versions and the full validation matrix (`.github/workflows/ci.yml`). The container-based integration suites under `tests/` are slower and require Docker or Podman and network access; run them when the change warrants it, not in a unit-test loop.

Never report a check as passing unless it ran. If infrastructure blocks validation, say what failed, what ran, and the strongest alternative check completed.

## Architecture

The main flow is `Parse -> Graph -> Execute`:

- `main.go` is the composition root; CLI workflows belong in `internal/app`.
- `internal/config` parses and validates project schemas and personal manifests. It must not depend on `internal/exec`.
- `internal/graph` orders tool dependencies. Install execution normally goes through `internal/exec.Executor` and its adapter contract. Native sync and batch operations are intentional exceptions contained within `internal/exec`; do not hardcode package-manager commands in the CLI.
- Adapters implement the contract in `internal/exec` and are registered during application bootstrap. Use `internal/run.Runner` for external processes.
- `internal/native` owns the declarative native-manager registry; `internal/state` owns persisted install state and locking.

See `docs/architecture.md` for the package map and execution flow. Check `internal/` for the current package inventory.

## Engineering conventions

- Treat pre-existing changes as owned by the user or another actor. Do not revert, reformat, stage, or overwrite unrelated changes; inspect overlaps before editing.
- Do not invoke `exec.Command` directly from application or adapter code. Route subprocesses through `internal/run.Runner` and the existing elevation abstractions; `internal/run` implements that boundary.
- Add or change adapters through the executor contract and registration path. Tests can use the helpers in `internal/exectest/` and an isolated adapter registry.
- Use English, atomic Conventional Commits (for example, `fix: preserve archive ownership`). Commit identity is operator-local Git configuration.

## Guardrails

- **Safe:** inspect files and Git state; run builds, tests, and relevant static checks.
- **Confirm first:** install or remove host dependencies, materially change `detect_os.sh`, or publish/release.
- **Never:** force-push or commit secrets.
- `pre_install`, `post_install`, `build`, and `build_cmd` can execute arbitrary commands. Prefer argv-form `run = ["program", "arg"]`; shell-string forms invoke a shell. Execution is blocked unless explicitly enabled with `--allow-arbitrary-code`. Treat schemas enabling it as security-sensitive.
- For owned archive payloads, use `extract_to` and `entrypoints`. `binary` does not name an archive-internal path. Removal must delete only owned payloads and declared launchers, never shared parent directories. Use the artifact-specific adapter for installer packages; see `docs/schema-reference.md` for format constraints.
- Before changing OS detection, verify affected distro-family mappings against `internal/native/registry.go` and update the relevant tests.

## Known gotchas

- (2026-09) [CRITICAL] Schemas require `schema_version = 1`; project schemas use `[tools]`, manifests use `[packages]`. Do not add legacy parsers, migrations, or aliases unless `docs/specs/schema-compatibility.md` changes that policy.
- (2026-09) [INFO] `method_only` restricts candidates exclusively; `method_prefer` changes priority and keeps fallbacks. Inferred native candidates depend on declaration form. See the per-tool method-control section in `docs/schema-reference.md`.
- (2026-09) [INFO] Method-level dependencies and sources resolve only when their candidate is reached. `dependency_only` excludes a tool from ordinary roots while keeping it available as a dependency or via `--only`; see `docs/architecture.md` and `docs/schema-reference.md`.

## Git and Worktrunk workflow

Multiple agents and people may share the disk. Never mutate the shared checkout. Before the first edit, autofix, or code generation, create or enter a task worktree with `wt`:

```sh
wt list
wt switch --create agent/<scope>/<verb>-<target>
```

- Use one branch per task and actor. Do not reuse another actor's worktree.
- Name branches `agent/<scope>/<verb>-<target>` or `human/<user>/<scope>/<verb>-<target>`.
- If `wt` is unavailable, stop and report it; do not fall back to raw `git worktree` commands.
- Never push directly to the default branch. Merge only after build, tests, and lint pass; use a pull request when work is concurrent.

## Local operator instructions

Shared project rules live in this file and version-controlled documentation. If `.dev/AGENTS.md` exists, read it as optional, additive operator guidance. It may describe local preferences, tools, or machine-specific details, but it must not weaken or redefine shared invariants, validation requirements, architecture, or safety rules.

`.dev/` is Git-ignored and optional. Plans, scratch notes, generated reports, and temporary task state may live there; a fresh clone must remain complete without it. Promote stable, non-obvious project knowledge to this file or the appropriate document under `docs/`, and avoid duplicate canonical copies. Do not introduce `AGENTS.override.md` as a replacement for the shared instructions.

## Maintaining this file

Keep information here only when it is project-specific, non-obvious, reusable, stable, and likely to affect engineering decisions. Prefer source-of-truth links for detailed or volatile behavior. Keep personal preferences, generic advice, duplicated documentation, and task-specific notes out. Correct stale statements promptly; mark unresolved conflicts explicitly rather than silently choosing between them.
