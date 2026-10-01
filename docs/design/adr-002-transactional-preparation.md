# ADR-002: Transactional candidate preparation with ownership

- Status: decided, partially implemented (2026-09)
- Origin: implementation working notes, condensed into this ADR

## Context

Candidate evaluation mutated the machine — adding sources or installing
prerequisites before the winning candidate was known — and fallback did not
necessarily undo those mutations.

## Decision

Separate read-only probing from mutating preparation, with a durable
write-ahead journal and explicit ownership:

- `plan.PreparationPlan` enforces read-only probes vs mutating
  prepare/commit phases. Every transition validates the persisted journal as
  an exact ordered prefix of the current plan.
- Each prepare mutation is persisted as `applying` before the host mutation and
  confirmed afterward. Commit and rollback are likewise persisted as
  `committing` and `rolling_back` before host-side work. Each rollback
  compensation crosses `rollback_applying` then `rollback_applied`. Terminal
  `committed`/`rolled_back` is reached only after host mutations complete.
- Ambiguous outcomes stay blocked: `applying`/`rollback_applying` records
  close only on explicit `applied`/`not_applied` evidence; unknown commit
  outcomes stay fail-closed unless a method-specific probe establishes the
  stronger "commit not applied" fact. A crash between host execution and
  journal confirmation is represented explicitly, never as "never started".
- The exact resolved preparation plan persists beside every journal (state
  v3), so restart recovery cannot reconstruct a different transaction;
  journal/plan keys pair 1:1.
- Capability/condition gates and source-presence probes are read-only: no
  source is added merely to answer "could this candidate work?". After
  preparing an absent declared source, the executor revalidates candidate
  availability. If the target remains unavailable, the executor compensates
  the source before fallback.
- Ownership is explicit: `ResourceIdentity`/`OwnershipKind`/
  `OwnedResourceState` with sorted unique dependents and refcounts.
  `FinalizeCommit` projects prepared sources into a canonical snapshot;
  `FinalizeRollback` persists every retained source/prerequisite (including
  zero-ref orphans) instead of silently forgetting them. Plan-aware cleanup
  removes only `RollbackSafe` resources (`RollbackRetain` stays explicit).
- State v5 adds a checksummed replacement WAL before destructive upgrades. It
  persists the tracked old state, exact desired target, and resource-use claims
  before removal, then journals removal and installation boundaries before
  those host mutations. After exact-target verification, installed tool state,
  release of the old dependent claims, and the target's replacement ownership
  claims are committed atomically with the `Installed` phase while the
  replacement WAL remains active. Recovery observes old and desired identity independently
  and resumes only an evidenced step; ambiguous observations block. The after-upgrade hook is preceded by a
  persisted `PostHookRunning` boundary. The postinstall-completion flag and WAL
  removal are saved together, including when the hook reports failure; if a
  restart finds `PostHookRunning`, it blocks rather than replaying a hook with
  unknown side effects. A reported post-hook failure does not undo
  the committed installation.
- State v4 persists sticky `root_requested` intent: last-reference
  depengine-owned lazy prerequisites are recursively removed, shared helpers
  wait for the final owner, root/external prerequisites are retained.
- Failed candidates use explicit-retain semantics: installed lazy
  prerequisites remain visible in report/state, never silent orphans.
- Lazy `method.requires` executions use the same candidate WAL, even when no
  package source is missing. Their install crosses the durable committing
  boundary and is reconciled by resolved identity after restart, so an
  interrupted prerequisite install is not blindly replayed. Finalizing a lazy
  prerequisite commit atomically records its `ToolState` and a zero-ref
  depengine-owned prerequisite identity in the same durable state save; crash
  recovery performs the same projection. The dependent candidate persists its
  exact resource-use claims into its own WAL with the committing boundary, then
  atomically saves its `ToolState`, claims those dependents, and removes its WAL
  on success or recovered commit. An owner transaction is created even when no
  package source is missing. A helper remains safely zero-ref if its owner never
  commits.
- Before any new host mutation, the executor replays recoverable source
  preparation/rollback work from the persisted plan, reconciles a committing
  candidate through a read-only identity observation, and records a confirmed
  recovered commit without replaying its hooks or installation. Ambiguous
  outcomes surface as fail-closed recovery errors.
- Hooks stay candidate-local transition events; post-hook failure reports
  against an already-committed transition and never triggers implicit
  compensating uninstall.
- Dry-run projects prepare/commit mutations into `plan_intent` without
  performing them (no state/WAL, no mutating runner calls).

## Open work

- Richer identity observation for automatic commit finalization.
