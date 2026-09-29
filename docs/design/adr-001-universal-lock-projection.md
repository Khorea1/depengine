# ADR-001: Lockfile as immutable-resolution projection of the resolved plan

- Status: accepted and implemented (2026-09)
- Origin: implementation working notes, condensed into this ADR

## Context

The lock resolver pinned release/artifact placeholders and checksums, while
package-manager, language-manager, Git branch, container tag, and
version-manager installs stayed unpinned — yet user-facing language promised
"same tools, same versions".

## Decision

The lockfile is the immutable-resolution projection of `ResolvedInstallPlan`:

- The adapter-neutral `LockProjection` captures concrete version/revision/
  digest, source/registry, artifact URL/path plus checksum and
  checksum/signature verification metadata, scope/environment, and target
  identity. New projections also retain the normalized *requested* version
  intent, so a manifest branch/tag/constraint/channel change cannot hide
  behind an accidentally identical concrete resolution.
- Lock generation is pure: `ProjectLock`/`BuildLockDocument` project over
  already-resolved plans — no execution, network, or mutation — and document
  bytes are invariant to input-plan order.
- Managers without stable resolution project explicitly as `unavailable`;
  universal document generation fails instead of emitting partial pins.
- The projection never stores credentials: URL userinfo and sensitive query
  parameters are stripped, manual serialization is defensively redacted, and
  credential-bearing persisted identity is rejected on read.
- The projection/document is versioned and rejects unknown versions.
  `VerifyResolvedPlanAgainstLock` rejects identity, requested-intent, and
  tool-set drift against an immutable `LockDocument`.
- Persisted lock v2 embeds the validated projection. `update` writes it only
  when every selected plan is immutable and the document covers the whole
  install closure: a `--profile` run over a v1 lock refreshes that profile's
  v1 pins instead of persisting a partial projection, because the tools it
  left out have no entry for the v2 consumer to replay. `install` and
  `upgrade` materialize and verify the pinned plan before observation or
  mutation.
- Lock v1 remains readable for compatibility and is migrated by the next
  successful whole-schema update. It retains only its documented
  method-specific guarantees.

## Operational boundary

Adapters that do not expose a concrete identity remain usable without a frozen
v2 lock, but they cannot produce one: update fails closed instead of persisting
an incomplete projection. Exact package pins still do not lock transitive
registry dependency graphs. Git-backed Brew/Scoop sources with explicit URLs
require a concrete local HEAD revision; frozen replay verifies that revision
before package installation. Signing-key trust is not inferred or serialized
unless an adapter supplies an operational trust identity.
