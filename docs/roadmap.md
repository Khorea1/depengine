# Roadmap

Long-lived unfinished work. Last reviewed: 2026-09-24.

Work on the current execution model comes before adding more installer types.

## P0: correctness and security

- [x] Reject ignored adapter fields and cover every meaningful field with a
  behavior test. Static resolution, validation effects, and differential
  execution and verification probes cover every declared field.
- [~] Add typed secret references to planning and runtime resolution. Runtime
  resolution now supports env-backed credentials for Git-backed `brew-tap` and
  `scoop-bucket` source preparation plus request-scoped env-backed Bearer tokens
  for typed `http`, `appimage`, `android`, and `msi` primary artifact,
  explicit checksum, and signature downloads,
  typed GitHub release/API and asset authentication, and origin-scoped Bearer
  authentication for private HTTPS `git` clone/fetch/same-origin recursive
  submodules plus private HTTPS `cargo.git` prefetch before local Cargo install,
  and temporary registry auth files for typed Docker/Podman image pulls.
  Typed HTTP Bearer transports reject remote plaintext HTTP both during static
  validation and again at the request boundary (loopback HTTP remains allowed).
  Typed env-backed secret sources are excluded from child process environments
  across probes, hooks, preparation, and execution; removal requires `--schema`
  for the same exclusion because state does not retain reference names.
  Other authenticated operations still need an explicit credential transport
  before they can be accepted.
- [x] Restore fuzzing as a reliable CI gate. The CI manifest is checked
  against Go's runtime test listing, then every runnable target gets a bounded
  fuzz run; missing, renamed, newly added, or build-tagged-out targets fail.
- [~] Triage the security-relevant `gosec` backlog ahead of the general lint
  cleanup. Reviewed and documented scanner findings, added narrow checksum
  compatibility and TAR normalization suppressions, and fixed the hard-link
  path validation gap. Local/offline archive expansion now has a 4 GiB
  aggregate limit, and TAR mode conversions validate range before narrowing.
  Remaining call-site review is open; see
  [`gosec-triage.md`](gosec-triage.md).
- [x] Remove the orphaned root lock artifact. No canonical root schema is
  tracked; the stale lock referenced a removed `fastfetch/http/0` method and
  lacked its method identity hash. Root-generated locks are now ignored;
  lock v1 validation remains covered by `internal/lock` fixtures and tests.

## P1: shared install semantics

- [~] Use `ResolvedInstallPlan` consistently in install, upgrade, status,
  remove, validation, explanation, and dry-run.
- [~] Finish exact-version support and installed-version checks per adapter.
- [~] Make lock generation/consumption cover every supported mutable method, or
  narrow the documented reproducibility promise.
- [~] Finish typed package-source selection, trust, ownership, verification,
  and locking.
- [~] Finish recovery for package-source and prerequisite preparation.
- [~] Keep hooks tied to the candidate/transition that actually runs; status
  must not depend on a one-time hook having succeeded earlier.
- [~] Generate schema/docs metadata from adapter capabilities instead of
  scattering method-name conditionals across the codebase.
  Remaining method-name conditionals include cargo/conda target handling in
  `internal/exec/environment_target.go`,
  git/container checks in `internal/validate`, git identity handling in
  `internal/planner/identity.go`, and github handling in `internal/app/helpers.go`.

## P2: installer coverage

- [ ] Add typed macOS PKG/DMG and Windows EXE/MSIX/AppX installers.
- [ ] Finish lifecycle/version/source behavior for Chocolatey, Homebrew, Cargo,
  Go, Python/Node/Ruby/PHP, Conda, version managers, Snap, Flatpak, Git,
  containers, and Nix.
- [ ] Add detached-signature support for offline artifacts.
- [ ] Simplify `schema.example.toml` after scope defaults settle; add better
  candidate inspection and ambiguity errors.

## P3: verification and v1 freeze

- [ ] Expand cross-platform manifest fixtures with invalid and adversarial cases.
- [ ] Cover each method contract across validation, identity, dry-run, version,
  source, scope, environment, lock, lifecycle, idempotency, errors, and secret
  redaction.
- [ ] Add planner fuzz/property tests and lifecycle/state invariant tests,
  including command-bearing fields, secret/redaction boundaries, source/URL
  normalization, and lock identity invariants.
- [ ] Keep public claims aligned with behavior the implementation actually
  enforces.
- [ ] Complete [`specs/format-v1-freeze.md`](specs/format-v1-freeze.md) and run
  the v1 freeze review.

## Engineering cleanup

- [x] Evolve `depengine graph` around a typed graph IR before adding a terminal
  diagram renderer. Preserve declared/effective/resolved projections and keep
  visible dependency relations separate from scheduling constraints. See
  [`research/typed-dependency-graph.md`](research/typed-dependency-graph.md).
- [ ] Triage the full `golangci-lint` backlog and remove the baseline only when
  a complete run passes. Audit on 2026-09-22: 142 findings (`errcheck` 50,
  `gosec` 50, `staticcheck` 21, `errorlint` 11, `unused` 10); output is capped
  at 50 per category. Uncapped re-audit on 2026-09-23: 558 findings
  (`errcheck` 159, `gosec` 368, `staticcheck` 19, `errorlint` 11, `unused` 1);
  the per-linter cap hid the true `errcheck`/`gosec` counts.
- [x] Review `internal/plan/preparation.go` and
  `internal/exec/preparation.go`; split them only where the code has distinct
  responsibilities with separate invariants. Done: both files mapped
  (`.dev/preparation-map-2026-09-23.md`,
  `.dev/preparation-exec-map-2026-09-24.md`); each is one cohesive state
  machine with cross-block coupling — verdict: keep both unsplit.
- [ ] Decide whether tagged `go install github.com/Khorea1/depengine@version`
  is a supported distribution path. If yes, remove the main-module `replace`
  directives and add a release/install smoke check; if no, document the
  unsupported path and point users at the canonical release installation.
- [x] Tighten the runtime-dependency claim. Distinguish a statically linked Go
  binary from Unix host requirements used by OS detection (`sh` and standard
  utilities), and keep release packaging/tests aligned with that contract.
  Done: README separates the `CGO_ENABLED=0` release binary (now asserted by a
  release contract test) from the embedded POSIX `sh` script and base utilities
  Unix detection needs; Windows uses the Go-native fallback.
- [x] Add a concise alternatives/positioning document comparing depengine's
  project-level install model with tools such as mise, aqua, Nix, and Brewfile,
  focusing on behavioral scope rather than marketing claims. Done:
  [`docs/alternatives.md`](alternatives.md), linked from the README docs table.
- [x] Audit author variants before adding `.mailmap`; only merge identities
  confirmed to belong to the same person. Done: `.mailmap` exists and audit
  on 2026-09-23 verified it complete (7 identities, 6 aliases, no 8th
  identity on any ref).
