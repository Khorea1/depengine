# preparation.go responsibility map (2026-09-23)

File: `internal/plan/preparation.go` (1440 lines) + `preparation_test.go`
(1571 lines). Read-only review; nothing split. Baseline green:
`go test ./internal/plan/` ok, `go build ./...` exit 0.

Shared invariant across the whole file: **pure, fail-closed state
transitions over validated data — never executes host mutations.**
Every mutating step is planned as data (`Operation`) for the executor;
every ambiguity resolves to `blocked`/`retained`, never to guessing.

## Blocks

| Block | Lines | Responsibility | Invariant |
| --- | --- | --- | --- |
| A. Vocabulary | 10–148 | `PreparationPhase`, `ResourceKind`, `OwnershipKind`, `RollbackPolicy`, `ResourceIdentity` + `PrerequisiteResource`/`PrerequisiteToolName`, `PreparationMutation`, `PreparationPlan` + `Validate` | Probe=read-only, prepare=unique resources, commit=mutations; credential-free canonical keys |
| B. Rollback planning | 214–306 | `RollbackDecision`, `rollbackStepsFor`, `RollbackFor`, `ownedResourceSnapshot` | Reverse application order; refcount>0 or non-safe policy ⇒ retain+report; ownership-change ⇒ error |
| C. Ownership projection | 308–491 | `OwnedResourceState`, `ClaimResourceUses`, `ClaimResource`, `ReleaseResource`, sorted/canonical snapshots | Sorted-unique dependents; ownership preserved on pre-existence, transitions to depengine only on recreate; deep isolation |
| D. Removal lifecycle | 493–707 | `ReleaseDependentResources`, `FinalizeReleasedResource`, `CleanupReleasedResources`, `FinalizeReleasedResourceCleanup` + comparators | Zero-ref depengine-owned only removable; cleanup re-derived from plan before state changes (no relabeling retain as cleaned) |
| E. Crash recovery | 709–824 | `PreparationRecoveryAction/Decision`, `RecoveryFor` | Decisions only; ambiguous commit ⇒ reconcile-or-blocked, never auto-rollback; terminal states reject recovery |
| F. Journal WAL | 826–1440 | `PreparationStatus/Outcome/Journal` + 15 methods (`PlanApply`→`FinalizeRollback`) | Write-ahead boundaries persisted before host mutation; prefix-exact vs plan; evidence-driven ambiguity resolution |

## Split verdict: DO NOT SPLIT

- B/C/D/E/F call into each other (F→B `rollbackStepsFor`;
  D↔B share reverse-order + retain semantics; E→B; all share
  `ownedResourceSnapshot`/canonical-order helpers). Cutting by letter
  creates cross-file coupling with no new invariant on either side.
- External coupling anchors the file here: `Operation`, `cloneOperation`,
  `EffectReadOnly/EffectMutation` live in `plan.go:27,215,601`;
  `Reconcile`/`Observation`/`ResolvedIdentity` in `lifecycle.go`/`plan.go`.
  A vocabulary-only split (A) would separate types from their sole users.
- The only genuine wart found is intra-function, not architectural:
  duplicated NUL check in `ClaimResource` (`preparation.go:427-432`) —
  behavior-preserving dedup, queued in lint-triage green list.
- Revisit only if a block gains a *separate* invariant (e.g. journal
  persistence format versioning diverging from in-memory transitions).
  Until then the roadmap item is satisfied by this map, not by a split.
