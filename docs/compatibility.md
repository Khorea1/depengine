# File format compatibility

depengine stores three independently versioned formats:

| File | Version field |
|---|---|
| project schema / personal manifest | `schema_version` |
| lockfile | `version` |
| installed state | `version` |

The universal projection embedded in `depengine.lock` carries its own separate
version field, `lock_version`.

These are file-format versions. They are not depengine release numbers, and a
change to one format does not require the others to change.

## Current status

All three formats are still pre-freeze. The schema/manifest uses version `1`,
installed state uses version `4`, and `depengine.lock` is written as version
`1` or `2` (`depengine update` promotes it to `2`). None of these numbers is
yet a promise that every future depengine release will read every file written
today.

Until the [v1 freeze gate](specs/format-v1-freeze.md) is complete:

- readers accept only the versions they are written for — today the lockfile
  accepts `1` and `2`, the schema/manifest `1`, and state `4` — and reject
  every other version;
- unknown older or newer versions are rejected;
- there are no implicit legacy parsers;
- normal reads do not silently migrate files;
- a breaking format change may still happen, but code, tests, examples, and
  documentation must change together.

## After a format is frozen

Once a format is frozen, its version becomes a compatibility contract. A
breaking grammar or meaning change requires a new version.

Readers must inspect the version before interpreting version-specific fields.
Syntactically valid TOML or JSON is not enough to treat an unknown version as
the current format.

### Schema and manifest

After schema v1 freezes, `schema_version = 1` means the frozen v1 grammar and
semantics. A breaking change requires `schema_version = 2` or later.

If depengine supports multiple frozen versions, parser dispatch must be
explicit. Any migration should produce a new file instead of silently rewriting
user-authored configuration during a normal read.

### Lockfile

Lock compatibility is independent of schema compatibility. A lock records
resolved identities, so an unknown lock version must be rejected before entries
are consumed.

Reading a lock never rewrites it. `depengine update` is the only operation that
promotes a version `1` lock to version `2`; an install that rewrites the lock
keeps the version it read.

A future lock migration must convert representation without re-resolving
mutable dependencies. Re-resolution is an update, not a format migration.

### State

State compatibility is also independent. Unknown state must not be treated as
empty state because lifecycle operations depend on recorded ownership.

Automatic state migration is only safe when it is deterministic,
transactional, and preserves the original data on failure. Otherwise migration
should be explicit.

## Freeze gate

Public docs should not describe v1 as a stable backward-compatibility guarantee
until the criteria in [`specs/format-v1-freeze.md`](specs/format-v1-freeze.md)
are complete and the freeze review is accepted.
