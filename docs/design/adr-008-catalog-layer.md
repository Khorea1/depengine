# ADR-008: A catalog layer supplies recipes by name

Status: Proposed

Every decision below carries its origin. **Maintainer** means stated by the
maintainer in session.
**Evidence** means supported by a measurement or by code reading cited in
Context. **Proposal** means this draft recommends it and it needs a decision.
Nothing here defines syntax, a binary format, or a directory layout.

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

1. **Ownership and precedence.** *(Maintainer, T1, T4, T5.)* The user owns the
   catalog. Derived sources, for example Repology, are not the only way to
   declare recipes. Precedence is user layer over (manifest plus derived
   sources), as the merge already does. Within the lower layer, an ordered list
   of sources applies and the first wins, with no third precedence level.

2. **Lookup contract.** *(Evidence.)* `Lookup(name)` returns the complete
   recipe for a name, or not found. It takes no host facts and filters nothing
   (ADR-007 decision 4). It returns candidates in declared order. Storage is
   hidden behind the contract. The first backend is one TOML file per package,
   with the file system as the index. The spike showed this stays flat from
   1 000 to 100 000 recipes and returns candidates, `MethodsHash` and
   `SourceHash` equal to the eager path for the same tools at three scales.

3. **One resolution point.** *(Evidence, Proposal for the exact semantics.)*
   Lookup is invoked from a single function used by both `loadProject` and the
   `validate` path. Otherwise `validate` checks less than commands execute. The
   names requested are the tools the project schema declares, which is the
   current pull semantic. Catalog, manifest and schema merge in that order,
   before identity is computed.

4. **Declarative-only import.** *(Maintainer, T2; Proposal for the mechanical
   definition.)* A recipe entering from a catalog or import is rejected if any
   of its candidates sets a lifecycle hook, sets a command-typed field, or whose
   plan reports arbitrary code (`methodkind.PlanRequiresArbitraryCode`, the
   same predicate the executor gate uses). Defining "declarative" by the gate's
   own predicate keeps the import validator and the executor from drifting.

5. **Identity pin.** *(Proposal; open decision D1.)* For every tool in the
   install closure that came from a catalog, the v2 lock records a content
   digest of the recipe. Frozen validation fails on mismatch. The digest is
   computed over the canonical recipe before host placeholders are expanded,
   so it is the same on every host (ADR-007 decision 1). It covers the recipe as
   written, not the resolved plan, because recipe fields such as `extract_to`,
   `link_dir` and `entrypoints` do not reach the resolved plan. A catalog revision may
   be recorded as a hint for retrieving the same recipe, never as the
   verification. The digest verifies; the revision retrieves.

6. **Lock version.** *(Evidence, from ADR-001 and `dep-41b`.)* Catalog identity
   is persisted only in lock v2. Using a catalog layer against a v1 lock fails
   closed and requires a whole-schema `update`. The catalog adds no obligation
   to the v1 index-keyed scheme, which is compatibility data and retires under
   the `dep-41b` gate.

7. **Candidate budget at entry.** *(Direction from ADR-007 Consequences;
   Proposal for the value; open decision D3.)* A per-tool candidate limit is
   applied where recipes enter, to avoid rebuilding the flat matrix in
   distributed form. It warns first and does not block. See open decisions for
   the value.

8. **Provenance.** *(Proposal.)* `why --fields` shows the layer a tool's
   recipe came from: catalog, import, manifest or schema.

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
  arbitrary-code gate does not see (open decision D5).

## Non-goals

- No import syntax, binary format or directory layout.
- No new language for recipes (T3, cancelled by the maintainer; reopens only if a
  real, documented manifest cannot be expressed in TOML).
- No decision on `distributions` beyond `github` (D4).
- No change to `MethodsHash` or `SourceHash` semantics.
- No change to the arbitrary-code gate.

## Open decisions

| # | Question | Recommendation | Evidence and gaps |
|---|---|---|---|
| D1 | Pin unit | Per-recipe digest over the whole recipe to verify, catalog revision only to retrieve | A whole-catalog pin is noisy at scale. The `update` behavior was verified (see Context): the lock does not cover recipe content, so `MethodsHash` and the resolved plan cannot stand in for a digest. Not measured: how many distinct recipe fields stay invisible to the plan across kinds; the field `Effects` flags are not a reliable proxy (they list `pkg` and `checksum`, which do reach the plan). |
| D3 | Candidate budget | Warn only; provisional threshold of 8 | Planning costs about 16–20 µs per candidate with the slimmed arbitrary-code gate, on the measured machine, so cost does not bind below dozens of candidates; the limit is governance. The largest hand-written tool in the repository has 8 candidates (`neovim` in `schema.example.toml`; a research matrix has 10). 38 parseable TOML files, mostly synthetic or test fixtures: not a corpus. |
| D4 | `distributions` beyond `github` | Undecided | No corpus. |
| D5 | Trust root for imports | Digest pinned by the importer (decision 5) plus an integrity floor: reject recipes that omit `checksum`, flag `:auto` | `:auto` is Trust On First Use and no checksum means no integrity check. Signature support and the position once v2 allows hooks are not decided. |
| D7 | Catalog layers and `allow_new_tools`; transitive `requires` | A catalog layer never sets `allow_new_tools`; names are only those the project declares | With `allow_new_tools` the manifest keeps every tool, which has no pull-by-name meaning. Not verified: how `requires` of a catalog tool not declared in the schema is treated. |

Resolved by evidence: **D2** (placement: decision 3) and **D6** (decision 6).

## Prerequisite

ADR-007 decision 1 does not hold today for schemas that put host placeholders in
`[defaults].method_order`: candidate order, pin keys and `MethodsHash` follow the
host, and `validate` loads with an empty map and sees yet another order
(`TestKnownViolationMethodOrderPlaceholderReordersCandidates`). A catalog layer
returns candidates that are then ordered by that code, so this must be resolved
first.

## Verification

- Acceptance test for the implementation: for the same tools, lookup and the
  eager path yield deep-equal candidates (modulo `TargetArch` and `TargetOS`),
  equal `MethodsHash` and equal `SourceHash`, at 1 000 and 100 000 entries.
  The spike compared 20 tools out of generated catalogs and is easy to recreate.
- The ADR-007 invariance test runs on catalog-sourced tools.
- A frozen install fails when a catalog recipe's digest differs from the lock.
- A recipe with a hook, a command-typed field or an arbitrary-code plan is
  rejected at import, with a fixture for each.
