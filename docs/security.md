# Security and threat model

This document describes the trust boundaries depengine can enforce. It is not a
claim that software installed through depengine is trustworthy.

## Arbitrary code

Build commands, hooks and equivalent script execution are arbitrary code. They
are gated by the explicit arbitrary-code permission where the method contract
marks that capability. Structured argv reduces shell parsing risk, but it does
not make an untrusted command safe.

`--dry-run` is planning mode. Read-only resolution and availability probes may
still execute and may use the network; mutation adapters, hooks, package-source
changes, state/lock writes and cache population must not run.

## Package and source trust

depengine can preserve a requested source, registry, remote, bucket, channel,
revision or digest when an adapter models it. That only identifies the source;
it does not establish that the source is benign. Repository signing and package
manager trust policies remain part of the underlying ecosystem's security
model.

Prefer immutable identities where available: content checksums, container
digests and Git commits provide stronger replay properties than mutable URLs,
tags, channels or branches.

## Checksums and signatures

Artifact checksums are verified before installation when declared. Signature
verification is supported only by methods that explicitly model it. A signature
is meaningful only relative to a trusted key; depengine does not automatically
establish the trustworthiness of a supplied key.

Signing-key URLs and artifact URLs reject embedded HTTP(S) credentials. Secret
material must not be stored in a manifest merely to make a download work.

## Credentials and redaction

Private registry/artifact authentication should use the authentication
mechanism of the underlying client or a future typed secret reference, not
credentials embedded in URLs or generic command arguments. Secure auth
references are not yet a universal depengine capability.

The subprocess logging boundary redacts common token/password flags,
Authorization/Cookie headers and URL userinfo from argv, stderr and surfaced
execution errors. State persistence separately rejects fields and command
values that look like secrets. Redaction is defense in depth, not permission to
put secrets in schema files.

## Downloader and transport selection

Artifact adapters use their modeled downloader/transport path. A manifest must
not be able to smuggle an arbitrary downloader command through a generic args
bag. HTTPS authenticates the transport endpoint according to the platform TLS
trust store; it does not replace checksum/signature verification of mutable
artifacts.

## Elevation and installer boundaries

Native package managers and installer formats may request or require elevated
privileges according to their platform semantics. depengine should keep the
privileged operation narrow and typed. Elevation does not make an installer
safe.

EXE/vendor installers and arbitrary scripts have inherently weaker inspectable
semantics than package managers or verified archives. Treat them as unsafe
escape hatches unless their identity, verification and ownership are modeled
explicitly.

## Source and key ownership

Adding a package source, repository, tap, bucket or signing key mutates host
configuration beyond a single package. Such mutations must be candidate-scoped
and idempotent, and cleanup must not remove externally-owned/shared resources
without ownership evidence. Shared-source refcounting is an area where support
is still incomplete; see `.dev/TODO.md`.

## Lockfile integrity

`depengine.lock` is a reproducibility input, not a trust root. Review lockfile
changes like source-code changes and protect the repository that stores it.
`--frozen-lockfile` prevents implicit lock changes, but it cannot prove that an
upstream package, signing key, package-manager index or mutable identity is
trustworthy.

Lock coverage is method-specific today. Where a method is not locked to an
immutable identity, the manifest and underlying package manager still govern
what can be selected.

## Reporting security issues

Do not include credentials, private tokens or confidential manifests in public
bug reports. Follow the repository's `SECURITY.md` process for vulnerability
reports when present.
