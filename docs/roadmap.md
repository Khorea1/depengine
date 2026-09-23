# Roadmap

Long-lived unfinished work. Last reviewed: 2026-09-22.

Work on the current execution model comes before adding more installer types.

## P0: correctness and security

- [~] Reject ignored adapter fields and cover every meaningful field with a
  behavior test.
- [~] Add typed secret references to planning and runtime resolution.

## P1: shared install semantics

- [~] Use `ResolvedInstallPlan` consistently in install, upgrade, status,
  remove, validation, explanation, and dry-run.
- [~] Finish exact-version support and installed-version checks per adapter.
- [~] Make lock generation/consumption cover every supported mutable method, or
  narrow the documented reproducibility promise.
- [ ] Make environment/profile targeting explicit and use the same target for
  install, check, and remove.
- [~] Finish typed package-source selection, trust, ownership, verification,
  and locking.
- [~] Finish recovery for package-source and prerequisite preparation.
- [~] Keep hooks tied to the candidate/transition that actually runs; status
  must not depend on a one-time hook having succeeded earlier.
- [~] Generate schema/docs metadata from adapter capabilities instead of
  scattering method-name conditionals across the codebase.

## P1: AdapterV2 cleanup

- [ ] Remove the legacy `Adapter` contract from `AdapterV2` once all remaining
  adapters use the resolved-plan model.
- [ ] Finish shared desired-state observation across install, upgrade, status,
  remove, validation, explanation, and dry-run.

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
- [ ] Add planner fuzz/property tests and lifecycle/state invariant tests.
- [ ] Keep public claims aligned with behavior the implementation actually
  enforces.
- [ ] Complete [`specs/format-v1-freeze.md`](specs/format-v1-freeze.md) and run
  the v1 freeze review.

## Engineering cleanup

- [ ] Triage the full `golangci-lint` backlog and remove the baseline only when
  a complete run passes. Audit on 2026-09-22: 142 findings (`errcheck` 50,
  `gosec` 50, `staticcheck` 21, `errorlint` 11, `unused` 10); output is capped
  at 50 per category.
- [ ] Review `internal/plan/preparation.go` and split it only where the code has
  distinct responsibilities with separate invariants.
- [ ] Audit author variants before adding `.mailmap`; only merge identities
  confirmed to belong to the same person.
