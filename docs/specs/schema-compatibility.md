# Schema compatibility policy

The latest schema contract is the only supported contract while the format remains
pre-freeze.

- `schema_version` identifies that contract and rejects every other version; it
  does not select legacy parsers, migrations, aliases, or compatibility paths.
- Schema changes may intentionally break old files. Update examples, documentation,
  generated JSON Schema, and validation tests in the same change.
- Do not add code whose only purpose is backward compatibility. Versioned
  compatibility becomes valid only when this specification is explicitly revised.

The broader freeze criteria for schema, lock, and state formats are defined in
[`format-v1-freeze.md`](format-v1-freeze.md).
