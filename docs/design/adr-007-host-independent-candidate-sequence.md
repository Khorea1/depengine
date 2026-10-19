# ADR-007: The candidate sequence of a tool is host-independent

Status: Accepted

## Context

Several identity-bearing mechanisms read a tool's whole candidate list:

- `computeMethodsHash` hashes the ordered `Kind` and `Label` of every method of
  the tool (`internal/lock/lock.go`).
- Lock v1 pin keys are `<tool>/<kind>/<index-within-kind>` over the same list.
- Upgrade finds the previously tracked candidate with
  `config.FindMethodCandidate(tool, kind, label)` over the same list (ADR-005).

ADR-005 builds each candidate's static intent without probing the host, and
ADR-001 states that `MethodsHash` identifies candidate kind/label intent.
Proposal E (`docs/specs/github-distributions.md`) added facts-keyed variants
inside one candidate: `config.SpecializeDistribution` returns a conventional
copy and never alters the list, so the effective plan of a candidate with
distributions is built from a facts-dependent copy while the candidate's
identity and the tool's candidate list are untouched. The invariant that
identity depends on is therefore about the candidate sequence, not about the
content of every intent.

A planned catalog layer (lookup of a recipe by tool name) and imported manifests
invite an optimization: return only the candidates that match the current host.
An executable experiment shows that this breaks identity. With four `github`
candidates differing by `when`, filtering the list per host produced a different
`MethodsHash` on each host, mapped the same key `br/github/0` to a different
candidate on each host, and made `FindMethodCandidate` fail for a tracked
candidate when the host facts changed. The same recipe in E form (one candidate,
four distributions) kept one `MethodsHash` on every host.

## Decision

1. The candidate sequence of a tool in the merged schema (count, order, `Kind`,
   `Label`) is a function of the declared layers only: project schema, personal
   manifest, and any catalog or imported layer. It never depends on host facts.
2. Host facts act only at these points, none of which changes the sequence:
   eligibility (`when`) evaluated per candidate at attempt and explain time;
   specialization within a candidate that produces a copy (the
   `SpecializeDistribution` pattern); and adapter resolution.
3. Identity-bearing code (intent hashes, pin keys, tracked-candidate lookup,
   persisted state) reads only the host-independent sequence.
4. A layer that supplies recipes by name returns the complete candidate set for
   that name. It does not pre-filter by host. A host-sliced view may be derived
   after identity is computed and is never consumed by identity-bearing code.

## Alternatives considered

- Host-filtered catalogs or imports: rejected; identity diverges per host (see
  Context).
- Host-keyed identity (hash over the filtered sequence plus a host fingerprint):
  rejected for now. It multiplies identity states. Reconsider only if per-host
  locks become an explicit goal.
- Static expansion at load (`cases`): already rejected by proposal E.
- Accept enumerated recipes and optimize only the executor: an interim reality,
  not the contract for catalogs.

## Consequences

- The invariant is checkable at the loader boundary: loading the same inputs
  under different host facts must yield a deep-equal candidate sequence.
- The catalog contract stays small: a lookup returns a complete recipe.
- The executor still visits every candidate of a tool. Cost is bounded by recipe
  discipline (factoring with distributions, a per-tool candidate budget enforced
  where recipes enter), not by the engine.
- `MethodsHash` covers only the ordered `Kind` and `Label` of the candidates
  (ADR-001), so a recipe whose content changes while its sequence stays the same
  does not move it. Catalog and import layers therefore need their own pinned
  identity (ADR-008).

## Non-goals

- No change to lock formats, `MethodsHash` semantics, or `when` semantics.
- No decision on extending `distributions` beyond `github`.
- Not decided here: when a `when` result may justify skipping work. That depends
  on whether host facts are conclusive and belongs to the arbitrary-code gate
  work.

## Verification

- A loader invariance test: same inputs loaded through the fact-aware loader
  (`app.loadProject`: the placeholder map comes from host facts) under different
  `platform.Facts` yield a deep-equal candidate sequence, modulo the host-stamped
  fields `TargetArch` and `TargetOS` that the parser sets on every candidate, and
  equal `SnapshotIntentMetadata`. Content that embeds a `{placeholder}` is
  expanded per host by design and is excluded from deep equality; its identity
  projection (count, order, kind, label) is not
  (`TestCandidateSequenceIsHostIndependent`).
- Goldens for recipes expressed in enumerated and in E form.

## Follow-ups

- Interaction with the lock v1 retirement gate: the index-keyed v1 scheme is the
  most fragile consumer of the sequence.
- Known violation of decision 1 (found by the invariance test): host
  placeholders in `[defaults].method_order` are expanded before candidates are
  ordered (`internal/config/parse.go`), so candidate order, pin keys and
  `MethodsHash` follow the host, and `validate`, which loads with an empty
  placeholder map, sees another order. Pinned as a strict expected failure in
  `TestKnownViolationMethodOrderPlaceholderReordersCandidates`. Options: reject
  placeholders in `[defaults].method_order`, stop expanding it at load and resolve
  it at run time, or amend this decision. Undecided.
