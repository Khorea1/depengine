# depengine — active schema/install DSL backlog

> Contains only unfinished work. Last audited: 2026-09-22.
>
> P2 is frozen until the P0/P1 semantic core is complete. New installation
> primitives on the legacy execution model are migration debt.

## P0 — correctness and security

- [~] Make validation, `why`, dry-run, and execution consume one resolved
  plan; dry-run must render the exact executable plan.
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
- [ ] Resolve scope to native platform paths/flags and remove Unix paths from
  normal manifest authoring.
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

## P1 — AdapterV2 cutover

- [~] Active integration is on
  `agent/coordinator/adapter-v2-wave1-integration`; its successor
  `agent/w3/adapter-v2-cutover` has uncommitted work. Keep both worktrees and
  branches until the cutover is integrated and verified.
- [ ] Migrate remaining adapters with conformance coverage.
- [ ] Fold legacy check/remove/availability/host-compatibility paths into the
  AdapterV2 contract, then delete the legacy interfaces.
- [ ] Prove all lifecycle commands share resolved-plan and observation
  semantics before merging the cutover.

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
- [ ] Align product claims and public documentation with actual guarantees,
  then run the v1 freeze gate.
