# Changelog

## Unreleased
- Drain external TAR decoder stdout after the end marker within the existing bounded, context-aware trailer policy.
- Expand native `{arch}` and `{os}` placeholders in GitHub `repo` + `asset` configuration fields while preserving `{arch_any}` and `{os_any}` asset-match tokens.
- Archive installs report rollback, launcher, backup, and staging cleanup failures; elevated payload roots use uid/gid 0 and mode 0755 while inner modes are preserved.
- Standalone `.bz2` extraction now honors the archive expanded-byte limit and context cancellation before installing decompressed output.
- HTTP artifact downloads now use the bounded in-process backend with a configurable 4 GiB default ingress limit before cache storage; curl/Wget selection is bypassed because their available size controls are not reliably per-file streaming limits.
- Serialize concurrent hook and source-preparation progress output and include the owning tool name.
- Report global tool and method timeout failures with the correct timeout scope and configured duration.
- Replacement recovery now reconstructs the configured candidate intent and pins it to the persisted immutable plan before observing or mutating the host; retries resolve method-scoped removal credentials and use the same timeout, environment omission, and elevation safeguards.
- Upgrade replacement now requires and clones the discovery-time tracked state, then compares it with durable state under lock before writing replacement WAL or removing the old installation. The previous method is resolved by `config.FindMethodCandidate` using its exact configured kind and label.
- Lock migration no longer treats matching `MethodsHash` values as proof that a historical v1 checksum belongs to a newly resolved artifact. Checksum carry-forward is limited to an exact artifact match in a prior v2 projection.
- Profiled v2 updates refresh retained entries when method or source metadata is missing or drifted, parse source keys without truncating slash-containing tool names, and build projections through `lock.NewUniversal`.
- SBOM version selection prefers installed state, then a matching-method immutable v2 projection entry, then an explicit v1 pin. Malformed or unavailable lock data remains best-effort; unknown versions export as `0.0.0`.
- State-tracked upgrades persist exact resource claims with replacement intent, atomically commit verified installed state and ownership while retaining the replacement WAL for hook recovery, and persist a boundary before the after-upgrade hook so uncertain hook outcomes are never blindly replayed.
- GitHub release assets retain declared checksum and detached-signature metadata through resolution and fail verification before extraction or installation mutates the payload.
