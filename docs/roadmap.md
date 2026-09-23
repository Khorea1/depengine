# depengine — schema/install roadmap

> Long-lived unfinished project work. Last audited: 2026-09-22.
>
> P2 is frozen until the P0/P1 semantic core is complete. New installation
> primitives on the legacy execution model are migration debt.

## P0 — correctness and security

- [~] Complete the no-ignored-fields invariant and add one behavior test for
  every non-trivial adapter field.
- [~] Wire explicit secret references into planning and runtime resolution.

## P1 — semantic core

- [~] Complete `ResolvedInstallPlan` integration across install, upgrade,
  status, remove, validation, explanation, and dry-run; one method-specific
  resolution path per candidate.
- [~] Finish version capabilities and desired-state verification per adapter:
  exact versions must never degrade to `latest`, and status must surface drift.
- [~] Make lock generation and consumption universal, credential-free, and
  migration-safe, or narrow the public reproducibility promise.
- [ ] Make environment/profile targeting explicit and use the same target for
  install, check, and removal.
- [~] Finish typed source/registry trust, ownership, selection, verification,
  and locking.
- [~] Finish transactional candidate preparation, recovery, and blocked-state
  handling for sources and prerequisites.
- [~] Complete candidate-local hook integration and make status independent of
  one-time hook execution.
- [~] Generate docs/schema metadata from capability contracts and eliminate
  cross-cutting method-name conditionals.

## P1 — AdapterV2 follow-up

- [ ] Remove the legacy `Adapter` contract embedded in `AdapterV2` and delete
  `LegacyAdapterV2` once remaining legacy primitives have been absorbed into
  the plan-aware contract.
- [ ] Finish shared resolved-plan and desired-state observation semantics
  across install, upgrade, status, remove, validation, explanation, and
  dry-run.

## P2 — frozen method fidelity

- [ ] macOS PKG/DMG and Windows EXE/MSIX/AppX typed installers.
- [ ] Remaining manager fidelity: Chocolatey, Homebrew, Cargo, Go,
  Python/Node/Ruby/PHP, Conda, version managers, Snap, Flatpak, Git,
  containers, and Nix.
- [ ] Offline artifacts: detached signatures. Decide whether a typed
  installer-script escape hatch is still needed after the common model exists.
- [ ] Simplify `schema.example.toml` once scope defaults land; add normalized
  candidate introspection and ambiguity diagnostics.

## P3 — verification and v1 freeze

- [ ] Expand the adversarial cross-platform manifest fixture matrix.
- [ ] Make method contract/conformance suites cover validation, identity,
  dry-run, version/source/scope/environment, lock, lifecycle, idempotency,
  errors, and secret redaction.
- [ ] Add planner fuzz/property and lifecycle/state invariant tests.
- [ ] Align product claims and public documentation with actual guarantees.
- [ ] Complete the criteria in [`specs/format-v1-freeze.md`](specs/format-v1-freeze.md) and run the v1 freeze review.

## Engineering quality

- [ ] Triage the full-repository golangci-lint backlog, fix valid findings, and
  remove the `new-from-rev` baseline only when the complete lint run passes.
  Audit on 2026-09-22: 142 findings (`errcheck` 50, `gosec` 50,
  `staticcheck` 21, `errorlint` 11, `unused` 10); the linter caps displayed
  findings at 50 per category.
- [ ] Review `internal/plan/preparation.go` for distinct change
  responsibilities; split only evidenced boundaries and keep its invariants
  and tests together. Do not split `internal/exec` or test files by size alone.
- [ ] Audit author variants with Git history and normalize future reporting
  with `.mailmap` only for identities verified as equivalent. Do not rewrite
  published history as part of routine cleanup.
