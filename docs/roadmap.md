# Roadmap

Long-lived unfinished work. Last reviewed: 2026-09-26.

Work on the current execution model comes before adding more installer types.

## P0: correctness and security

- [x] Reject ignored adapter fields and cover every meaningful field with a
  behavior test. Static resolution, validation effects, and differential
  execution and verification probes cover every declared field.
- [x] Add typed secret references to planning and runtime resolution for the
  supported transports: Git-backed `brew-tap` and `scoop-bucket` source setup;
  request-scoped Bearer credentials for `http`, `appimage`, `android`, and `msi`
  artifact, explicit checksum, and signature downloads; GitHub release/API and
  asset authentication; origin-scoped Bearer credentials for private HTTPS
  `git` clone/fetch/same-origin recursive submodules and `cargo.git` prefetch;
  and temporary registry auth files for Docker/Podman pulls. References resolve
  at the reached operation and fail closed when missing, empty, invalid, or
  unsupported. Authenticated HTTP rejects remote plaintext HTTP (loopback is
  allowed). Env-backed secret sources are excluded from child environments;
  removal needs `--schema` for the same exclusion because state does not retain
  reference names. Unsupported authenticated operations remain rejected until
  an explicit credential transport is defined.
- [x] Restore fuzzing as a reliable CI gate. The CI manifest is checked
  against Go's runtime test listing, then every runnable target gets a bounded
  fuzz run; missing, renamed, newly added, or build-tagged-out targets fail.
- [x] Triage the security-relevant `gosec` backlog ahead of the general lint
  cleanup. The uncapped scanner now reports zero diagnostics across production
  and tests. Hardening includes archive confinement/limits, owner-only
  state/cache storage, owner-only state locks and temporary GPG key material,
  symlink rejection during snapshot enumeration, and owner-only fixture
  permissions where broader modes are not part of the test contract. Reviewed
  dynamic paths, synthetic credentials, executable fixtures, and controlled
  subprocesses use narrow call-site suppressions; see
  [`gosec-triage.md`](gosec-triage.md).
- [x] Remove the orphaned root lock artifact. No canonical root schema is
  tracked; the stale lock referenced a removed `fastfetch/http/0` method and
  lacked its method identity hash. Root-generated locks are now ignored;
  lock v1 validation remains covered by `internal/lock` fixtures and tests.

## P1: shared install semantics

- [x] Use `ResolvedInstallPlan` consistently in install, upgrade, status,
  remove, validation, explanation, and dry-run. Validation checks the static
  candidate intent; host-aware flows resolve the selected candidate once and
  reconcile observations against that plan. Native batch gates use the same
  verification as serial execution. Remove and undo verify the tracked target
  and project its resolved identity into the removal adapter.
- [~] Finish exact-version support and installed-version checks per adapter.
- [x] Make lock generation/consumption cover every supported mutable method, or
  narrow the documented reproducibility promise. Done: the promise is narrowed —
  `docs/support-boundary.md` tabulates, per selector class, what lock v1 pins and
  what it ignores, and documents every frozen failure mode plus `update` merge
  semantics; the generated `immutable-lock` capability text, the `internal/lock`
  package doc, and the ambiguous install/upgrade/update wording no longer
  overclaim; `depengine update` now preserves pins from a readable existing lock
  when it cannot re-resolve them instead of dropping them (previously it deleted
  materialized `*:auto` checksums and every pin outside `--profile`, breaking
  the next frozen install). Remaining
  selector coverage stays open in the item below and in ADR-001 open work.
- [ ] Cover the mutable selectors lock v1 still ignores: git `branch`/`tag`,
  container `tag`, ecosystem/native resolutions, and channels — either by pinning
  them in `depengine.lock` or by wiring the universal lock projection
  end-to-end. See
  [`design/adr-001-universal-lock-projection.md`](design/adr-001-universal-lock-projection.md);
  current per-selector coverage is tabulated in
  [`support-boundary.md`](support-boundary.md).
- [~] Finish typed package-source selection, trust, ownership, verification,
  and locking.
- [~] Finish recovery for package-source and prerequisite preparation.
- [~] Keep hooks tied to the candidate/transition that actually runs; status
  must not depend on a one-time hook having succeeded earlier.
- [x] Generate schema/docs metadata from adapter capabilities instead of
  scattering method-name conditionals across the codebase. JSON schema
  generation, contracttest coverage registration, environment-target
  projection (`internal/exec/environment_target.go`), validate git/container
  checks, planner identity, and app github-latest handling all derive from
  `methodkind.Contracts`. The final scattered conditional — the
  `cargo.secret_ref` execution-coverage exception, previously inlined in
  three places — now lives in one `kind.field`-keyed exclusion table in
  `internal/contracttest/execution_coverage.go` with a drift guard. Remaining
  kind-based branches implement runtime behavior that is inherently
  method-specific (for example credential transport and source/native
  execution) or native-manager knowledge; schema/docs metadata no longer
  depends on those branches.

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
  responsibilities with separate invariants. Done: both files were reviewed;
  each is one cohesive state machine with cross-block coupling, so both remain
  unsplit.
- [ ] Decide whether tagged `go install github.com/Khorea1/depengine@version`
  is a supported distribution path. If yes, remove the main-module `replace`
  directives and add a release/install smoke check; if no, document the
  unsupported path and point users at the canonical release installation.
- [x] Tighten the runtime-dependency claim. Distinguish the single-file
  `CGO_ENABLED=0` release contract from platform system interfaces and Unix
  host requirements used by OS detection (`sh` and standard utilities).
  Done: README no longer claims that every platform binary has zero shared
  libraries; the release contract binds `CGO_ENABLED=0` to the depengine build,
  and CI inspects the produced Linux ELF for an interpreter or `NEEDED`
  entries. Windows uses the Go-native OS-detection fallback.
- [x] Add a concise alternatives/positioning document comparing depengine's
  project-level install model with tools such as mise, aqua, Nix, and Brewfile,
  focusing on behavioral scope rather than marketing claims. Done:
  [`docs/alternatives.md`](alternatives.md), linked from the README docs table.
- [x] Audit author variants before adding `.mailmap`; only merge identities
  confirmed to belong to the same person. Done: `.mailmap` exists and audit
  on 2026-09-23 verified it complete (7 identities, 6 aliases, no 8th
  identity on any ref).
