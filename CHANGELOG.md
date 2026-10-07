# Changelog

## Unreleased
- Redact sensitive output for direct and empty-environment subprocess calls.
- Drain external TAR decoder stdout after the end marker within the existing bounded, context-aware trailer policy.
- Expand native `{arch}` and `{os}` placeholders in GitHub `repo` + `asset` configuration fields while preserving `{arch_any}` and `{os_any}` asset-match tokens.
- Archive installs fail closed on rollback/launcher failures while post-commit backup/staging cleanup is warning-only; elevated payload trees normalize owner uid 0 without dereferencing symlinks, keep inner modes, and set the root mode to 0755.
- Standalone `.bz2` extraction now honors the archive expanded-byte limit and context cancellation before installing decompressed output.
- HTTP artifact downloads use the bounded in-process backend with a configurable 4 GiB default ingress limit before cache storage; the curl/Wget backends were removed because their available size controls are not reliably per-file streaming limits.
- Serialize concurrent hook and source-preparation progress output and include the owning tool name.
- Detached GPG signatures now require an explicit `signing_key`; verification rejects missing signer identity instead of trusting any key in the ambient keyring.

- Reject HTTP/GitHub `.deb` lifecycles before download or mutation: package identity is not persisted for safe observation and removal.
- Reject unsafe HTTP/GitHub `binary` paths before planning or filesystem access.
- Persist HTTP archive payload ownership in state and in a payload-local ownership marker; replacement/removal requires both durable ownership intent and matching filesystem evidence, and arbitrary `extract_to` paths are never treated as exclusive.
- Route Scoop package lifecycle and bucket source operations through one Scoop-local semantic runtime boundary; official CLI decoding is runtime-owned and capability checks precede requested mutations, without changing manifest method identity.
- Report global tool and method timeout failures with the correct timeout scope and configured duration.
- Replacement recovery now reconstructs the configured candidate intent and pins it to the persisted immutable plan before observing or mutating the host; retries resolve method-scoped removal credentials and use the same timeout, environment omission, and elevation safeguards.
- Upgrade replacement now requires and clones the discovery-time tracked state, then compares it with durable state under lock before writing replacement WAL or removing the old installation. The previous method is resolved by `config.FindMethodCandidate` using its exact configured kind and label.
- Lock migration no longer treats matching `MethodsHash` values as proof that a historical v1 checksum belongs to a newly resolved artifact. Checksum carry-forward is limited to an exact artifact match in a prior v2 projection.
- Profiled v2 updates refresh retained entries when method or source metadata is missing or drifted, parse source keys without truncating slash-containing tool names, and build projections through `lock.NewUniversal`.
- SBOM version selection prefers installed state, then a matching-method immutable v2 projection entry, then an explicit v1 pin. A missing lock remains optional; malformed or unreadable lockfiles fail export. Unknown versions export as `0.0.0`.
- Status and SBOM now surface malformed, unsupported, and unreadable lockfile errors instead of silently treating them as absent. A missing lock remains optional.
- Non-frozen installs resolve legacy lock identity before execution, fail if resolution fails, and persist that same preflight identity instead of resolving mutable selectors again after installation.
- State-tracked upgrades persist exact resource claims with replacement intent, atomically commit verified installed state and ownership while retaining the replacement WAL for hook recovery, and persist a boundary before the after-upgrade hook so uncertain hook outcomes are never blindly replayed.
- GitHub release assets retain declared checksum and detached-signature metadata through resolution and fail verification before extraction or installation mutates the payload.
- When the home and XDG storage bases are unavailable, state and download cache use a per-user fallback instead of shared or relative paths: a private system-temp root on Unix (owner-only directories and files, with ownership/permission checks) or the OS-provided per-user cache directory on Windows and other platforms (trusting the OS-managed user ACL). Resolution fails closed when the fallback cannot be resolved or secured.
