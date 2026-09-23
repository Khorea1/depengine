# Compatibility and format versioning

Depengine currently has three independently versioned document families:

- manifest/schema (`schema_version`);
- lock data (`lock_version` in the adapter-neutral lock model);
- installed-state data (`version` in the state file).

These numbers are **format versions**, not depengine release versions, and they
are intentionally independent. A manifest format change must not force a lock
or state version bump, and vice versa.

## Current status: pre-freeze

All three formats are currently **pre-freeze**. The versioned
[v1 semantic freeze gate](specs/format-v1-freeze.md) has not been completed, so
version `1` is not yet a promise that
all future depengine releases will continue to accept every file written today.

While a format is pre-freeze:

- the current reader accepts exactly the current format version;
- unknown older or newer versions fail closed;
- there is no implicit legacy parser dispatch;
- there is no silent migration or reinterpretation of unknown data;
- breaking changes may still happen, but examples, documentation and tests must
  move with the contract.

Unimplemented compatibility paths are not documented as supported.

## Meaning of a frozen format version

Once the v1 freeze gate is satisfied, a version number identifies the semantic
major of that specific on-disk format. Compatible additive changes may remain
within the same major only when an older reader can safely reject or ignore the
new data according to that format's documented rules. A breaking change requires
an explicit new format version.

The version must be inspected before interpreting version-specific fields. An
unknown version must never be parsed as the current format merely because the
surrounding JSON/TOML is syntactically valid.

## Manifest policy after freeze

`schema_version` identifies the manifest grammar and semantics. After v1 is
frozen:

- `schema_version = 1` means the frozen v1 manifest contract, not "the current
  depengine release";
- a breaking grammar or semantic change requires `schema_version = 2` (or later);
- supporting multiple frozen majors requires explicit parser/semantic dispatch;
- migration, if provided, must be an explicit operation that produces a new
  manifest; normal reads must not silently rewrite user-authored files;
- deprecation within a frozen major must not silently change the meaning of an
  already-valid v1 document.

Whether a future major ships an automated migration tool is a release decision,
not something implied by the version field itself.

## Lock policy after freeze

Lock format compatibility is independent from manifest compatibility. A lock
records resolved identities and reproducibility metadata, so readers must reject
unknown lock versions before consuming entries.

When lock v1 freezes, future depengine releases may either retain a v1 reader or
provide an explicit, deterministic migration to a newer lock version. A lock
migration must not re-resolve mutable references as a side effect: migration is
format conversion, not dependency update.

## State policy after freeze

State format compatibility is also independent. State records observations and
ownership needed for lifecycle operations, so an unsupported state version must
not be treated as an empty state.

A future state migration may be automatic only when it is deterministic,
transactional and preserves the original data on failure. Otherwise migration
must be explicit. Unknown state versions fail closed rather than risking an
install/remove operation against misinterpreted ownership data.

## Freeze gate

No document in this repository should describe manifest, lock or state v1 as a
stable backwards-compatibility guarantee until the requirements in the
[v1 freeze gate](specs/format-v1-freeze.md) are satisfied and the freeze review is
accepted. The current numeric
value `1` therefore has a stable *purpose* (format-major identity), but not yet a
frozen compatibility promise.
