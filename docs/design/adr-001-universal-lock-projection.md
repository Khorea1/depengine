# ADR-001: Lockfile as immutable-resolution projection of the resolved plan

- Status: decided, partially implemented (2026-09)
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
- Until universal locking exists, product claims stay qualified to the
  methods actually pinned (P3.6).

## Open work

- Per-adapter concrete fields (native/ecosystem versions, Go/Cargo,
  Git SHAs, container digests, artifact URLs, version-manager pins,
  Snap/Flatpak channels).
- `depengine update` meaningful pins for every mutable method class.
- Install/upgrade wiring consuming the pinned plan and verifier.
- `update` CLI migration to the pure projection path.
- Persisted legacy `depengine.lock` migration policy.
