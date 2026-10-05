# ADR-006: Keep Scoop semantics behind one runtime boundary

- Status: Accepted

## Context

The logical `scoop` package method and `scoop-bucket` source kind both need to
execute Scoop operations. Before this change, package lifecycle commands lived
in the Windows adapter while bucket commands and the official installation
layout lived independently in source management. Selecting another compatible
runtime would therefore split package installs from bucket observation,
revision checks, and rollback.

## Decision

`internal/scoopruntime` owns the Scoop-specific runtime contract and the
currently supported official `scoop.exe` protocol. The Windows package adapter
and source manager both call that contract; neither assembles Scoop commands
or derives its bucket path independently.

The boundary returns semantic installed-package and bucket records; all official
Scoop text decoding and repository-layout interpretation stays inside
`internal/scoopruntime`. The adapter remains responsible for reconciled package
identity, and source management remains responsible for ownership, configured
origin/revision validation, and rollback. Both composition helpers accept the
same selected runtime instance, while their existing constructors keep the
official runtime default. All subprocesses still use `internal/run.Runner`,
including scoped credentials and dry-run blocking.

Runtime choice is an internal implementation detail, not part of the user-facing
`scoop` method identity or schema. The official runtime is selected before
any operation. A failed mutation is returned to the existing executor/source
transaction path; the runtime does not retry it through another implementation.
No alternative implementation is selected because no second runtime currently
satisfies the full declared capability set.

## Alternatives considered

- Adding a user-facing runtime selector or a `hok` method: rejected. There is no
  complete alternate implementation, and provider identity should not change
  method identity.
- A project-wide provider framework: rejected. Scoop is the only current use
  case; a Scoop-local interface is sufficient.
- Keeping source-management commands local to `internal/source`: rejected.
  That would preserve the package/source protocol split and official filesystem
  assumptions.

## Consequences

- Both package and bucket operations now share one concrete protocol boundary
  without changing manifest semantics or official Scoop command behavior.
- A future runtime must satisfy each exact-version, bucket-selection, scope,
  architecture, removal, and revision/location capability required by an
  individual operation before the mutation is invoked.
- Runtime selection can be shared by the package adapter and source manager at
  their composition point without changing manifest identities.
