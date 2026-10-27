# ADR-008: A catalog layer supplies recipes by name

Status: Accepted (2026-10-25). Implementation follows the phased issues under `dep-ixd`.
Storage layout and path resolution are decided in
[ADR-009](adr-009-catalog-layout.md).

Every decision below carries its origin. **Maintainer** means stated by the
maintainer in session.
**Evidence** means supported by a measurement or by code reading cited in
Context. **Proposal** means the draft recommended it; the 2026-10-25 decision
accepts it or marks it deferred in the status table.
Nothing here defines syntax or a binary format. The directory layout and
path resolution are decided in [ADR-009](adr-009-catalog-layout.md).

## Context

A personal manifest is parsed whole and then filtered by name:
`config.ParseManifest` decodes the entire file, and `config.FilterManifestTools`
drops the tools the project schema does not declare (`internal/config/manifest.go`;
called from `loadProject` in `internal/app/helpers.go`). The semantics are
already pull-by-name; the mechanism is eager.

**Cost.** Measured on one vCPU with a warm page cache, with the project asking
for 20 tools out of a catalog of N recipes in the E form of `br` (four
`distributions`, about 730 bytes each):

| N | whole-manifest parse and filter | 20 lookups, one TOML per package |
|---|---|---|
| 1 000 | 109–139 ms | 4–5 ms |
| 10 000 | about 2.3 s | about 2.4 ms |
| 100 000 | 328 s and 401 s (one sample each), 1.6 GB peak RSS | 2.3–2.8 ms |

The eager cost is quadratic. A CPU profile attributes about 37% of the time at
N = 10 000 to `go-toml/v2` `SeenTracker.find`, which scans every sibling table
under `packages` at each table header to detect duplicate keys (v2.2.4,
`internal/tracker/seen.go`). The cost comes from one wide table, not from file
size. The figures come from a throwaway spike (2026-10-18) whose harness is not
in the repository; each N = 100 000 cell is a single run, and the two runs
differed by 22%.

**Load paths.** All host-dependent commands load through `app.loadProject`,
with a placeholder map derived from host facts. `validate` has its own path
(`runValidate` and `mergeManifest`) with an empty map.
`config.ResolveSchemaFromFiles` has no production caller.

**Identity.** `MethodsHash` hashes only the ordered `Kind` and `Label` of a
tool's candidates (`computeMethodsHash`), and `SourceHash` hashes host-source
identity. ADR-001 states that `MethodsHash` "identifies candidate kind/label
intent, not a source URL or artifact"; the concrete identity (artifact URL,
checksum, requested version, target) lives in the v2 `LockDocument`, and
`VerifyResolvedPlanAgainstLock` rejects drift against it. Consequence for this
ADR: a recipe whose content changes while its candidate sequence stays the same
does not move `MethodsHash`. A catalog therefore needs an identity of its own.

A throwaway test of `update` and the install-time checks (2026-10-18; `http`
recipes with literal checksums, resolved offline) showed what the lock does and
does not see when a recipe's content changes while `Kind` and `Label` do not:

- A whole-schema `update` re-resolves every tool from the current recipe, so a
  changed URL and checksum are adopted into the lock without any notice.
- An entry retained by `update --profile` is not refreshed
  (`universalLockIntentDrifted` compares only `MethodsHash` and `SourceHash`).
  The install-time identity check then rejects the stale entry ("artifact
  identity changed"), while `ValidateFrozen` still passes.
- Changing only `extract_to`, `link_dir` and `entrypoints` leaves the resolved
  plan identical, so the lock and both checks accept it, whether the tool was
  selected or retained. Whether the executor applies those fields at install
  time follows from the contract (`EffectExecute`) and was not exercised.

The lock therefore pins what a recipe resolves to, not the recipe.

**Trust.** `checksum = "sha256:auto"` is documented as Trust On First Use, and a
recipe that omits `checksum` installs with no integrity check
(`docs/schema-reference.md`). A declarative recipe is not a safe recipe.

## Decision

1. **Ownership and precedence.** *(Maintainer, T1, T4, T5; clarified
   2026-10-25.)* The user owns the catalog. Derived sources, for example
   Repology, are not the only way to declare recipes. Precedence, highest to
   lowest, is: project schema, personal manifest, user catalog, derived sources.
   The project schema wins conflicting fields (the repository contract), the
   manifest overrides catalog defaults, and within the catalog an ordered list
   of sources applies with the first match winning. Merge order is therefore
   catalog, manifest, project schema, before identity is computed. No catalog or
   derived layer sets `allow_new_tools`; that remains a manifest-only switch.

2. **Lookup contract.** *(Evidence.)* `Lookup(name)` returns the complete
   recipe for a name, or not found. It takes no host facts and filters nothing
   (ADR-007 decision 4). It returns candidates in declared order. Storage is
   hidden behind the contract. The first backend is one TOML file per package,
   with the file system as the index; its layout and path resolution are
   decided in [ADR-009](adr-009-catalog-layout.md). The spike showed this stays flat from
   1 000 to 100 000 recipes and returns candidates, `MethodsHash` and
   `SourceHash` equal to the eager path for the same tools at three scales.
   A recipe is host-independent: lookup returns every declared candidate in
   declared order and never filters or reorders by host. `when` conditions are
   evaluated at candidate selection, not at lookup, so a lock written on one
   host keeps the same candidate identity on another.

3. **One resolution point.** *(Evidence; accepted 2026-10-25 for the exact
   semantics.)*
   Lookup is invoked from a single function used by both `loadProject` and the
   `validate` path. Otherwise `validate` checks less than commands execute. The
   names requested are the tools the project schema declares, which is the
   current pull semantic. Catalog, manifest and schema merge in that order,
   before identity is computed. The shared function is also the parity boundary
   between `validate` and execution: both parse, resolve and order the same
   recipe set; only the host facts available for `when` evaluation differ. A
   schema parsed with no host facts must still produce the same candidate
   sequence as one parsed with host facts (see Verification).

4. **Declarative-only import.** *(Maintainer, T2; accepted 2026-10-25 for the
   mechanical definition.)* A recipe entering from a catalog or import is
   rejected if any of its candidates sets a lifecycle hook, sets a
   command-typed field, or whose
   plan reports arbitrary code (`methodkind.PlanRequiresArbitraryCode`, the
   same predicate the executor gate uses). Defining "declarative" by the gate's
   own predicate keeps the import validator and the executor from drifting.

5. **Identity pin.** *(Accepted 2026-10-25; D1 resolved.)* For every tool in the
   install closure that came from a catalog, the v2 lock records a content
   digest of the recipe. Frozen validation fails on mismatch. The digest is
   computed over the canonical recipe before host placeholders are expanded,
   so it is the same on every host (ADR-007 decision 1). It covers the recipe as
   written, not the resolved plan, because recipe fields such as `extract_to`,
   `link_dir` and `entrypoints` do not reach the resolved plan. A catalog revision may
   be recorded as a hint for retrieving the same recipe, never as the
   verification. The digest verifies; the revision retrieves.
   **Implementation (2026-10-25):** the canonical form is the TOML document
   decoded into a map, encoded as JSON with sorted keys, and hashed with
   SHA-256 (`sha256:<hex>`). It is persisted as `recipe_digest` in lock v2,
   keyed by tool name; every field is included, while comments, whitespace and
   key order are not. Lock v1 carries no such field and rejects a catalog-sourced
   tool until a whole-schema update writes v2.

6. **Lock version.** *(Evidence, from ADR-001 and `dep-41b`.)* Catalog identity
   is persisted only in lock v2. Using a catalog layer against a v1 lock fails
   closed and requires a whole-schema `update`. The catalog adds no obligation
   to the v1 index-keyed scheme, which is compatibility data and retires under
   the `dep-41b` gate.

7. **Candidate budget at entry.** *(Accepted 2026-10-25; D3 resolved: warn
   only, provisional threshold 8, revisit with the corpus in dep-ixd.13.)* A
   per-tool candidate limit is applied where recipes enter, to avoid rebuilding
   the flat matrix in distributed form. It warns first and does not block.

8. **Provenance.** *(Accepted 2026-10-25; central contract, not optional
   diagnostics. Implemented 2026-10-27.)* `why --fields` shows the layer a
   tool's recipe came from: catalog, import, manifest or schema, together with
   inherited and overridden fields, candidates considered and excluded, the
   chosen adapter, and the recipe identity. The same report is emitted by
   `why --json`; candidate outcomes carry a stable `reason_code` that is never
   derived from human text. Field origins survive multi-layer merges, so a
   value inherited from the catalog keeps that origin after passing through a
   later layer.

## Layer contract

| Layer | Supplies | May set `allow_new_tools` | Precedence |
|---|---|---|---|
| Project schema | Intent: tools, profiles, project constraints, explicit overrides | No (field is manifest-only) | Highest |
| Personal manifest | Operator-local installation knowledge and overrides | Yes | Above catalog |
| User catalog | Reusable recipes consulted by name | Never | Above derived sources |
| Derived sources | Generated hints, for example Repology | Never | Lowest |

The project schema declares the top-level tools. A catalog recipe's `requires`
is part of its dependency intent: the resolver includes those names in the
closure and resolves each one through the catalog or a local definition. A name
in the closure that resolves nowhere is a hard validation error, never a silent
skip. This does not let a catalog widen the project: top-level names are still
only those the project schema declares.

## Alternatives considered

- **Keep eager manifests and shard them.** Not measured; the quadratic term
  suggests it would help. Rejected as the contract because the user would manage
  shards by hand and the pull-by-name semantics would stay implicit.
- **`zip` or an own index.** Not measured against the file-per-package backend
  on warm cache. The earlier synthetic figure (about 220 ms cold start at
  100 000 entries) is not comparable. Reconsider only if a measurement on the
  target platforms shows the directory approach failing.
- **Host-specialized catalogs.** Rejected by ADR-007.
- **Pin the whole catalog by revision.** Rejected as the verification: it makes
  drift mean "the catalog changed", which at scale is almost always true and
  says nothing about the tools in use. Kept as a retrieval hint.
- **Rely on `MethodsHash` for recipe identity.** Rejected: it ignores content.

## Consequences

- Startup cost no longer depends on catalog size for the common case.
- The lock gains one digest per catalog-sourced tool in the closure, not one per
  catalog entry.
- `validate` and the install commands share one lookup path.
- Imports cannot carry hooks or commands in v1. A declarative recipe can still
  direct an install at a repository and asset of its choosing, which the
  arbitrary-code gate does not see; D5's integrity floor (no checksum rejected,
  `:auto` flagged) is the compensating control.
- A catalog layer cannot widen the project: top-level names remain
  project-declared, and transitive `requires` must resolve or validation fails.

## Non-goals

- No import syntax or binary format. Directory layout and path resolution
  are ADR-009, not this ADR.
- No new language for recipes (T3, cancelled by the maintainer; reopens only if a
  real, documented manifest cannot be expressed in TOML).
- No support for `distributions` beyond `github` until D4 is decided with a
  real corpus (`dep-ixd.13`).
- No change to `MethodsHash` or `SourceHash` semantics.
- No change to the arbitrary-code gate.

## Decision status

| # | Question | Decision (2026-10-25) | Evidence and gaps |
|---|---|---|---|
| D1 | Pin unit | **Resolved:** per-recipe digest over the whole recipe to verify; catalog revision only to retrieve | A whole-catalog pin is noisy at scale. The `update` behavior was verified (see Context): the lock does not cover recipe content, so `MethodsHash` and the resolved plan cannot stand in for a digest. How many distinct recipe fields stay invisible to the plan across kinds is still unmeasured; the field `Effects` flags are not a reliable proxy. |
| D3 | Candidate budget | **Resolved:** warn only; provisional threshold of 8, revisit with the corpus in `dep-ixd.13` | Planning costs about 16–20 µs per candidate with the slimmed arbitrary-code gate, so cost does not bind below dozens of candidates; the limit is governance. The largest hand-written tool in the repository has 8 candidates. 38 parseable TOML files, mostly synthetic or test fixtures: not a corpus. |
| D4 | `distributions` beyond `github` | **Deferred:** no support until a real corpus (`dep-ixd.13`) justifies a decision | No corpus. A schema using another value remains out of contract. |
| D5 | Trust root for imports | **Resolved:** digest pinned by the importer (decision 5) plus an integrity floor: reject recipes that omit `checksum`, flag `:auto` | `:auto` is Trust On First Use and no checksum means no integrity check. Signature support and the position once v2 allows hooks remain deferred. |
| D7 | Catalog layers and `allow_new_tools`; transitive `requires` | **Resolved:** a catalog layer never sets `allow_new_tools`; top-level names are only those the project declares; transitive `requires` resolve through the catalog and fail closed when unresolvable | With `allow_new_tools` the manifest keeps every tool, which has no pull-by-name meaning. The closure rule is stated in the layer contract above. |

Resolved by evidence: **D2** (placement: decision 3) and **D6** (decision 6).
Resolved by decision: **D1**, **D3**, **D5**, **D7**.
Deferred: **D4** (blocked on a real recipe corpus).

## Prerequisite

Resolved on 2026-10-25 by rejecting host placeholders in method order selectors
before expansion (`defaults.method_prefer`, the `defaults.method_order`
compatibility alias, and tool-level `method_prefer`/`method_only`). Previously,
ADR-007 decision 1 did not hold for schemas that put host placeholders in
`[defaults].method_order`: candidate order, pin keys and `MethodsHash` followed
the host, and `validate` loaded with an empty map and saw yet another order.
Selectors are now literal; per-host selection belongs in candidate `when`
conditions, so a catalog layer receives candidates that are ordered
host-independently.

## Verification

- Acceptance test for the implementation: for the same tools, lookup and the
  eager path yield deep-equal candidates (modulo `TargetArch` and `TargetOS`),
  equal `MethodsHash` and equal `SourceHash`, at 1 000 and 100 000 entries.
  The spike compared 20 tools out of generated catalogs and is easy to recreate.
- The ADR-007 invariance test runs on catalog-sourced tools.
- A frozen install fails when a catalog recipe's digest differs from the lock.
- A recipe with a hook, a command-typed field or an arbitrary-code plan is
  rejected at import, with a fixture for each.
- A schema parsed with no host facts and with host facts yields the same
  candidate sequence (config-level parity test).
- A host placeholder in an order selector is rejected before expansion
  (`internal/config/order_selector_placeholder_test.go`), closing the ADR-007
  prerequisite.
- Catalog, manifest and project schema merge in that order; the project schema
  wins conflicting fields and `allow_new_tools` (contract test).
