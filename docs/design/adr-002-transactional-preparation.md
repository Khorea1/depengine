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
- Each prepare mutation crosses a persisted `applying` boundary before host
  mutation and is confirmed afterward; commit planning persists `committing`
  before host-side commit, rollback planning persists `rolling_back` before
  compensation, each rollback compensation crossing symmetric
  `rollback_applying`/`rollback_applied` boundaries. Terminal
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
  source is added merely to answer "could this candidate work?". An absent
  declared source enters durable preparation, availability is revalidated
  after, and an unavailable target compensates the source before fallback.
- Ownership is explicit: `ResourceIdentity`/`OwnershipKind`/
  `OwnedResourceState` with sorted unique dependents and refcounts.
  `FinalizeCommit` projects prepared sources into a canonical snapshot;
  `FinalizeRollback` persists every retained source/prerequisite (including
  zero-ref orphans) instead of silently forgetting them. Plan-aware cleanup
  removes only `RollbackSafe` resources (`RollbackRetain` stays explicit).
- State v4 persists sticky `root_requested` intent: last-reference
  depengine-owned lazy prerequisites are recursively removed, shared helpers
  wait for the final owner, root/external prerequisites are retained.
- Failed candidates use explicit-retain semantics: installed lazy
  prerequisites remain visible in report/state, never silent orphans.
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

- WAL-backed prerequisite preparation (sources are wired; lazy
  `method.requires` still uses explicit-retain).
- Richer identity observation for automatic commit finalization.
