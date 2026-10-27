# ADR-009: Catalog storage layout and path resolution

Status: Accepted (2026-10-25). Implementation follows `dep-ixd.3`.

ADR-008 accepts a catalog layer below the personal manifest and fixes the lookup
contract, precedence, identity and trust rules. It deliberately leaves the
storage layout and path resolution open. This ADR decides them without defining
the full recipe syntax, which belongs to `dep-ixd.3`.

## Context

`dep-ixd.3` cannot implement `Lookup(name)` without deciding:

- whether the catalog belongs to the user, the project, or both;
- where the root lives on Linux, macOS and Windows, including environments
  without `HOME` or XDG variables;
- how a tool name maps to a file name without collisions on case-insensitive
  filesystems;
- what absence, unreadable files, invalid TOML, duplicates and symlinks mean;
- how the layout supports multiple ordered sources without scanning the whole
  catalog.

Current code evidence:

- `config.DefaultManifestPath` resolves `DEPENGINE_MANIFEST`, then
  `$XDG_CONFIG_HOME/depengine/manifest.toml`, then
  `~/.config/depengine/manifest.toml`. It has no macOS or Windows branch
  (`internal/config/manifest.go`).
- Project schema discovery is cwd-relative and independent
  (`internal/app/helpers.go`).
- `loadProject` and `runValidate` still reach the manifest through different
  helpers (`internal/app/helpers.go`, `internal/app/validate_check.go`).
  ADR-008 decision 3 requires one resolver for both.
- Tool names are unconstrained TOML keys; repository examples include uppercase
  names such as `DepartureMono`. A file-per-recipe layout therefore needs an
  explicit name grammar.
- ADR-008 decision 1 assigns the catalog to the user and allows an ordered list
  of sources with the first match winning.
- The catalog spike measured 20 lookups at 2.3–2.8 ms for 1,000 and 100,000
  recipes, against 109–139 ms and 328–401 s for whole-manifest parsing
  (ADR-008 Context).

## Decision

### 1. One recipe per file; the file system is the index

The catalog is a flat directory. The recipe for name `N` is `<root>/N.toml`.
There is no index file, manifest, shard, archive or cache in v1. `Lookup(N)`
forms one path per root and opens it; it never lists the directory.
`catalog list`, `catalog doctor` and a future `catalog search` may scan, but no
`install`, `update`, `upgrade`, `validate`, `check`, `status`, `why` or
`graph` path may.

### 2. Scope: user catalog only in v1

The catalog is user-owned. The default root is
`<platform config dir>/depengine/catalog`:

| Platform | Base directory | Default root |
|---|---|---|
| Linux/Unix | `$XDG_CONFIG_HOME` when absolute, otherwise `$HOME/.config` | `<base>/depengine/catalog` |
| macOS | `$HOME/Library/Application Support` | `<base>/depengine/catalog` |
| Windows | `%AppData%` | `<base>\depengine\catalog` |

An empty or relative `XDG_CONFIG_HOME` is ignored on Unix and falls back to
`$HOME/.config`, mirroring `plan.ResolveUnixXDGPaths`. When the platform base
cannot be determined (no `HOME`, no `XDG_CONFIG_HOME`, no `AppData`), the
default root does not exist and the catalog is simply disabled. An explicit root
does not depend on those variables.

Project catalogs and named catalog layers are rejected in v1. ADR-008 assigns
the catalog to the user; the project already owns `schema.toml` and can carry
its own overrides. A project catalog would add a second trust boundary and a
second identity without a measured need. A project directory may still be
passed explicitly as a root; it is then a user-selected root, not an automatic
project layer. A future ADR may add project roots as additional entries in the
ordered root list without changing the file format.

The personal manifest keeps its historical `$XDG_CONFIG_HOME`/`~/.config` path
on macOS and Windows. Aligning it with the platform config directory is a
separate compatibility change, not part of this decision.

### 3. Ordered roots and explicit overrides

Resolution order:

1. `--catalog <dir>`, repeatable; command-line order is priority order.
2. `DEPENGINE_CATALOG`, a list separated by `os.PathListSeparator`.
3. The default root above.

A flag or environment value replaces the default list; it does not append to it.
`--no-catalog` disables lookup and is mutually exclusive with `--catalog`. The
flags and environment variable are accepted by every command that loads project
or manifest configuration: `install`, `update`, `upgrade`, `validate`,
`check`, `status`, `graph` and `why`.

Paths are `~`-expanded (leading `~` or `~/` only), then required to be
absolute. Relative roots, empty list entries, and roots that are not directories
are errors when explicitly named. An absent default root means "no catalog"; an
absent explicit root is an error, so a typo cannot silently disable a catalog. A
default root that exists but is not a directory or is unreadable is an error.

Within the ordered list, the first root that contains `N.toml` wins. Roots are
not field-merged. A later root is shadowed for that name. `why --fields` names
the winning root and its index.

### 4. Precedence and participation

Layer precedence is unchanged: project schema, personal manifest, catalog roots
in order, derived sources. Merge order is catalog, manifest, project schema,
before identity is computed.

Catalog lookup applies to:

- top-level tool names declared by the project schema;
- names in the transitive `requires` closure of a recipe that participates.

It does not apply to:

- names that exist only in the personal manifest because `allow_new_tools` is
  set;
- names not declared or reachable from the project schema.

A manifest-only tool must be fully defined by the manifest. This keeps
`allow_new_tools` a manifest-only switch and prevents a catalog from widening
the project (ADR-008 D7).

A top-level name may be fully defined in the schema; the catalog still provides
a base recipe for it and the schema overrides conflicting fields. A transitive
name that resolves nowhere is a hard validation error, never a silent skip
(ADR-008 layer contract).

### 5. Name grammar and file mapping

A name is catalog-addressable only if it matches:

```
^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$
```

and is not a Windows reserved device name (`CON`, `PRN`, `AUX`, `NUL`,
`COM1`–`COM9`, `LPT1`–`LPT9`, case-insensitive, including a suffix such as
`CON.toml`) and does not end with `.`.

Consequences:

- ASCII only, so Unicode normalization cannot create a collision between macOS
  and Linux checkouts.
- No `/`, `\`, `:`, spaces or control characters; the name is a single path
  segment on every supported platform.
- Case is significant and preserved. `Foo` and `foo` are different names.
- 1–128 bytes leaves room for `.toml` and atomic-write suffixes under the
  common 255-byte filename limit.
- A name outside the grammar can still be defined in `schema.toml` or the
  manifest; it simply cannot come from the catalog.

The canonical file name is exactly `<name>.toml`; writers must use lowercase
`.toml`. On a case-insensitive filesystem an uppercase extension may resolve to
the same file, which is accepted. The recipe must carry exactly one declared
name (a top-level `name`, or the single key of its package table, as
`dep-ixd.3` defines) and it must equal the filename stem byte-for-byte. A
mismatch is a hard error. This turns a case-insensitive shadow or a renamed file
into a diagnosable error instead of installing the wrong recipe.

Case-fold collisions:

- On a case-insensitive filesystem, two names differing only by case cannot
  coexist. The opened file's declared name must equal the requested name;
  otherwise lookup fails closed with a collision diagnostic.
- On a case-sensitive filesystem, both files can exist and lookup by exact name
  is correct. `catalog list` or `catalog doctor` may warn about a case-fold
  collision for portability; the common lookup path does not scan and therefore
  does not warn.

### 6. Failure semantics

| Condition | Result |
|---|---|
| Default root absent | no catalog; not an error |
| Explicit root absent, not a directory, or unreadable | error (exit 2) |
| Default root exists but is not a directory or is unreadable | error |
| `<name>.toml` absent in a root | not found in that root; continue to the next |
| No root has the name, and the name is required from the catalog | hard error (unresolvable dependency) |
| No root has the name, but the name is locally defined | not an error |
| File unreadable, invalid TOML, invalid recipe, missing or unknown version, name mismatch | hard error, fail closed, with path and root index |
| Non-regular file (directory, FIFO, device) | hard error |
| Symlink recipe file | hard error (containment) |
| Symlink root | allowed; resolve the root once |
| Duplicate name in one root | impossible (same path) |
| Duplicate name across roots | first root wins; no error |
| Empty root | no recipes; not an error |

Errors name the root index, the resolved path and the requested name. No catalog
error is downgraded to a warning on the execution path. `validate` reports them
as validation errors; `install`, `update`, `upgrade`, `check`, `status`,
`why` and `graph` exit non-zero.

The symlink rule is enforced with `os.Lstat` on the recipe path; a symlinked
root is resolved once with `filepath.EvalSymlinks` or equivalent. The residual
TOCTOU between `Lstat` and open is accepted in v1 because the catalog is local
and user-owned; it is not a remote trust boundary.

### 7. Versioning and evolution

Every recipe declares `recipe_version = 1` at top level. v1 accepts only `1`;
unknown versions fail closed. The catalog has no version file and no index.
Per-recipe content identity is the digest from ADR-008 D1, and a format change
changes the digest. Adding optional fields within v1 is allowed only if the
strict parser accepts them under the existing unknown-field contract; removing
or reinterpreting a field requires `recipe_version = 2` and a new parser and
lock path.

A catalog revision may be recorded as a retrieval hint, never as verification
(ADR-008 D1). There is no whole-catalog pin.

### 8. Discovery and performance

No command in the common path scans the catalog. Lookup cost is O(number of
roots) file opens, independent of N. `catalog list`, `catalog doctor` and a
future `catalog search` may scan; they are explicit, read-only commands.

Target, with a warm page cache on one vCPU: 20 lookups at most 10 ms at N =
1,000 and N = 100,000; peak RSS bounded by the requested recipes, not N. The
ADR-008 spike measured 2.3–2.8 ms; the target leaves headroom for validation.
`dep-ixd.3` publishes the benchmark in `.dev/reports/`. No cache or index is
added before a measurement on the target platforms shows the directory failing;
that would require a new ADR.

### 9. Security

The layout provides no trust or integrity by itself. Every recipe read through
`Lookup` is parsed and validated as a single-tool recipe. ADR-008 decision 4
applies at lookup: a recipe that sets a lifecycle hook, a command-typed field,
or whose plan requires arbitrary code is rejected. The import integrity floor
(reject a missing checksum, flag `:auto`) belongs to `dep-ixd.8`; lookup
neither relaxes nor extends it. The catalog is local and user-owned in v1; there
is no remote fetch. Unreadable roots and files fail closed. No secrets belong in
the layout; logs carry paths and digests, not recipe bodies.

### 10. Impact

- `install`: resolves the catalog for schema-declared names and the transitive
  closure; merges catalog, manifest, schema; a catalog-sourced tool in a v1 lock
  fails closed; v2 records the recipe digest.
- `update`: same resolution; a whole-schema update re-resolves and records
  digests; a catalog recipe change with an unchanged `MethodsHash` is caught by
  the digest (`dep-ixd.6`); `--profile` keeps existing v1/v2 behavior.
- `validate`: uses the same resolver with no host facts; reports missing or
  invalid catalog recipes; a schema parsed with and without facts yields the
  same candidate sequence.
- `why --fields`: shows layer `catalog`, root index and path, recipe path,
  digest, inherited and overridden fields, and candidates considered and
  excluded; it does not scan the catalog.
- `graph`: includes catalog `requires` edges for the closure; no scan; output
  is unchanged when no catalog is present.
- `check` and `status`: use the same effective schema; verbose output names the
  catalog roots used and the recipes loaded.
- `allow_new_tools`: never set by the catalog; manifest-only names are not
  catalog-resolved; the catalog cannot widen the project.
- `remove`, `forget`, `undo`, `upgrade` and `sbom`: no layout-specific
  behavior; they consume state and lock. `upgrade` verifies the digest when
  re-resolving from a catalog.
- No behavior change when no catalog root exists. A catalog root that exists
  becomes active automatically; `--no-catalog` restores isolation for tests and
  scripts.

### 11. Alternatives considered

- **Project-only catalog.** Rejected: ADR-008 assigns the catalog to the user;
  the project schema already owns project intent; a project catalog would need
  a second trust boundary.
- **Both user and project, project over user.** Rejected for v1: it adds a
  precedence slot and a second identity without a corpus. It can be added later
  as another root without changing the file format.
- **Named layers.** Rejected for v1: names need a registry or index and ordering
  rules; ordered roots already provide precedence.
- **Single file, zip or dedicated index.** Rejected: it reintroduces eager
  parsing or a second source of truth. The measurement shows per-file lookup is
  flat. Reconsider only with a target-platform measurement.
- **Encoded or hashed file names.** Rejected: opaque for authors and tooling. A
  strict portable grammar plus a declared-name check is simpler.
- **Filename-derived name only.** Rejected: it cannot detect a renamed file or
  a case-insensitive shadow. The declared name is required and must match.
- **Directory scan to build an index.** Rejected: cost proportional to N and a
  cache to invalidate; the file system is the index.
- **Follow symlinks for recipe files.** Rejected: containment. Roots may be
  symlinks.

### 12. Test and migration plan

Tests for `dep-ixd.3`:

- path resolution: Linux XDG set and unset, relative XDG, macOS, Windows, no
  `HOME`/XDG, `~`, relative and empty values, explicit missing root,
  root-is-file, unreadable root;
- name grammar: valid, invalid, reserved, trailing dot, non-ASCII, length,
  case-fold pair;
- lookup: found, absent, invalid TOML, unreadable file, symlink file, directory,
  first-root-wins, name mismatch, version mismatch;
- precedence: catalog below manifest below schema; a manifest-only name is not
  looked up; the transitive closure is resolved;
- parity: lookup against the eager path at 1,000 and 100,000 recipes;
  `MethodsHash` and `SourceHash` equal; candidate sequence equal with and
  without host facts;
- CLI: `--catalog`, `DEPENGINE_CATALOG`, `--no-catalog`, mutual exclusion, on
  all eight commands;
- `why --fields` provenance and no-scan;
- lock: v1 plus catalog fails closed; v2 digest mismatch fails;
- no-catalog golden outputs unchanged for `install`, `update`, `validate`,
  `graph` and `why`.

Migration: none. No catalog root means no change. A user who creates a root opts
in by doing so. `init` does not create the catalog root. A future `catalog add`
writes atomically (temporary file plus rename) and validates before publishing.

### 13. Consequences

Positive: lookup cost independent of catalog size; a portable, discoverable
layout; no index to invalidate; deterministic precedence; one resolver for
validation and execution; clear failure semantics.

Negative: catalog names are restricted to ASCII and a small grammar; there is no
project-shared catalog in v1; roots are not field-merged; case-insensitive
collisions are detected at lookup but not enumerated without a scan; the
manifest path diverges from the catalog path on macOS and Windows until a
separate change.

### 14. Non-goals

- Recipe syntax beyond the name, version and lookup invariants.
- Importer, remote registry, download or signatures.
- Project catalogs and named layers.
- Index, cache, zip or binary format.
- Changes to `MethodsHash`, `SourceHash`, candidate order or the arbitrary-code
  gate.
- Lock changes beyond ADR-008 D1 and D6.
- Aligning the manifest path with the platform config directory.

## Verification

- `dep-ixd.3` implements `Lookup` with this contract and the tests above.
- `dep-ixd.7` surfaces the provenance fields in `why --fields`.
- `dep-ixd.6` persists and verifies the recipe digest in lock v2.
- `dep-ixd.8` applies the integrity floor at import.
- `dep-ixd.13` measures a real corpus and may reopen project catalogs or the
  name grammar with evidence.
