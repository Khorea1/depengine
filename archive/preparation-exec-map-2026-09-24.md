# preparation.go (exec) responsibility map (2026-09-24)

File: `internal/exec/preparation.go` (676 lines) + `preparation_test.go`
(964 lines). Follow-up to the plan-side map
(`.dev/preparation-map-2026-09-23.md`), which covered
`internal/plan/preparation.go` (DO NOT SPLIT); this map covers the executor
side and completes the roadmap item's second file. Read-only review; nothing
split. Baseline green: `go build ./...` exit 0, `go test ./internal/exec/` ok.

Shared invariant across the whole file: **every host mutation (source add or
remove) sits between a persisted WAL boundary and a persisted confirmation;
in-flight markers are resolved by read-only presence probes; ambiguity
resolves to blocked, never to guessing.** The file is an extension of the
`Executor` type (all top-level functions are `*Executor` methods or methods on
the two file-local types) that drives one journaled state machine end-to-end.

## Blocks

| Block | Lines | Responsibility | Invariant |
| --- | --- | --- | --- |
| A. Vocabulary | 22–71 | `candidateSourcePreparation`, `sourcePreparationTransaction`, `recoveredCandidateCommit`, `preparationBlockedError` + `preparationBlocked`/`blockPreparation` | A tx wraps executor + locked state + key + plan; blocked errors carry the unambiguous reason (probe-failed/rollback-failed/WAL-failed) for a blocked outcome |
| B. Key derivation | 72–111 | `candidatePreparationKey` / `candidatePreparationSubject` | Intent-aware canonical key `candidate/v1/<b64tool>/<b64method>/<sha256>`; subject parse validates structure, b64 identity, and digest before use |
| C. Probe + prepare | 113–241 | `probeCandidateSources`, `prepareCandidateSources`, `resolveSourceSecrets` | Validate every ownership identity before any mutation; WAL boundary persisted before add, confirmation after; state-less (library) path degrades to best-effort add discriminated by a presence probe |
| D. Tx lifecycle | 276–401 | `planCommit`, `finalizeCommit`, `rollback`, `leaveCommitUnresolved`, tx `rollback`/`resumeRollback`/`applyRollbackDecision`/`close`, `preparationMutationByID` | Rollback executes the persisted rollback decision, never memory; per-op WAL apply → host mutation → confirm; a closed tx is a no-op |
| E. Recovery | 411–676 | `preparationRecoveryNeedsElevation`, `recoveryCandidate`, `observeRecoveryCandidate`, `containsIdentityField`, `reconcilePreparationCommit`, `recoverPreparationTransactions`, `recoverPreparationTransaction` | Recovery runs before any new host mutation; in-flight add/remove is resolved by a presence probe before planning; commit reconciliation requires observation to establish a matching identity — fail-closed, never replay/rollback by guessing |

## Split verdict: DO NOT SPLIT

- One hinge type holds the file together: `sourcePreparationTransaction` is
  created in C (prepare), advanced through D (commit/finalize/rollback), and
  rebuilt from the persisted journal in E (each recovery decision path
  constructs a fresh tx and calls the same `applyRollbackDecision`).
- Recovery shares the exact rollback machinery with the live failure path:
  `applyRollbackDecision` is called both by `tx.rollback` from C's add-failure
  handling and by E's `RecoveryRollbackPreparation`/`ResumeRollback` cases.
  Same invariant on both sides, not a separate one — the roadmap condition
  ("only where the code has distinct responsibilities with separate
  invariants") fails.
- The blocked-error vocabulary is shared across C and D, and `attempt.go`
  gates rollback on `preparationBlocked(err)`; a split would need to export
  the sentinel across package-internal boundaries without gaining a new
  invariant.
- A vocabulary-only split (A) would separate the two tx types from their sole
  users, replicating the plan-side finding.
- External helpers already live in their owning files:
  `candidatePlanIntent` (`capability.go`), `methodForResolvedTarget`
  (`environment_target.go`), `sourceResourceUses` (`execute.go`). The file's
  remaining externals are the `plan`/`source`/`state`/`secret` APIs that any
  split would still import unchanged.
- Test cohesion mirrors code cohesion: 15 tests exercise the whole file as one
  unit, including cross-block transitions (unresolved commit left for
  recovery, cancellation leaving a recoverable journal, recovered commits not
  replaying hooks/security gates).

Revisit only if a block gains a *separate* invariant: e.g. recovery gains a
second persisted medium, a standalone recovery CLI entry point, or the WAL
journal format versioning diverges from in-memory transitions (as noted for
the plan side).

## Notes

- The in-flight marker branches in `recoverPreparationTransaction` mirror the
  outcome resolution in `prepareCandidateSources`/`applyRollbackDecision`
  with deliberately inverted `present ⇒ outcome` mapping (add: present ⇒
  applied; rollback: present ⇒ not-applied). Extracting a shared helper would
  obscure the inversion; left as-is.
- `preparationRecoveryNeedsElevation` fails open (false) when
  `depstate.Load()` errors — by design: recovery itself surfaces corruption
  before any mutation; the helper only decides whether to establish the
  interactive elevation session first.

## Adjacent findings (recorded, not acted on here)

- `unused`: `runGraph` (`internal/app/graph_why.go:54`) — that file is owned
  by the active graph worktree (feat/graph-terminal-renderer); left for that
  stream.
- `errorlint` ×2 prod sites: `internal/plan/preparation.go:373` and
  `internal/httpdownload/gpg.go:196` — `%w` conversions on real error-chain
  paths; rendered messages may be asserted; pending the lint-policy decision.
- `staticcheck` 9 remain and are all the documented exclusions: SA1019 ×2,
  ST1005 ×1, QF1012 ×6 in `state/hash.go` (the errcheck-trap, reverted
  2026-09-23).