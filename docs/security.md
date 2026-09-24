# Security

depengine can verify some facts about an install. It cannot prove that software
from a trusted-looking source is safe.

## Arbitrary code

Hooks and build commands execute arbitrary code. String commands use a shell;
`run = ["program", "arg"]` keeps argument boundaries explicit, but the program
still has the permissions of the depengine process.

Execution paths marked as arbitrary code require the explicit permission gate.

`--dry-run` does not run install adapters or hooks and does not write state,
lockfiles, package-source changes, or cache entries. Planning may still perform
read-only checks and network resolution.

## Sources are not trust

Fields such as `source`, `registry`, `remote`, `bucket`, `channel`, revision,
and digest tell depengine what to request. They do not make that source safe.
Package-manager signing and trust policies still belong to the underlying
ecosystem.

Prefer immutable identities when available: checksums, container digests, and
Git commits are easier to replay and audit than branches, tags, channels, or
mutable URLs.

## Checksums and signatures

Artifact checksums are verified before installation when declared. Signature
verification is only available on methods that model it explicitly.

A signature is only useful if you trust the signing key. Supplying a key to
depengine does not establish that trust by itself.

HTTP(S) artifact and signing-key URLs reject embedded credentials.

## Release artifacts

Project releases publish several verification inputs:

- SHA-256 checksums for release archives;
- keyless cosign signatures and certificates for the checksum file;
- GitHub build provenance for the checksum file;
- SPDX SBOMs for release archives.

For stronger release verification, check the archive checksum, verify the
cosign signature against this repository's release workflow identity, and
verify the GitHub provenance against `Khorea1/depengine`.

```sh
cosign verify-blob \
  --certificate <checksums>.pem \
  --signature <checksums>.sig \
  --certificate-identity-regexp '^https://github.com/Khorea1/depengine/\.github/workflows/release\.yml@refs/tags/v.+$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  <checksums>

gh attestation verify <checksums> --repo Khorea1/depengine
```

## Credentials

Do not put credentials in schema or manifest URLs. Use the authentication
mechanism provided by the package manager or service. A `github` method may
declare an env-backed `secret_ref`; the reference participates in static
planning while execution reports and persisted state omit it. The value is
resolved only in process for release resolution and asset download. When no
typed reference is declared, GitHub release access can still use
`GITHUB_TOKEN`, `GH_TOKEN`, or existing `gh` authentication.

Typed `http` methods can reference env-backed credentials with `secret_ref`,
`checksum_secret_ref`, and `signature_secret_ref`. Typed `github` methods can
use `secret_ref` for release API resolution and GitHub asset transport. Secret
values are not placed in command argv, plans, lockfiles, state, reports,
diagnostics, or logs. Authenticated HTTP requests use the Go backend even when
curl or wget is available.

Typed `git` methods can use `secret_ref` only with a credential-free HTTPS
repository URL. The Bearer token is passed through a per-child environment
override and Git URL-scoped configuration for clone, revision fetch, and
recursive submodule operations. It is not inherited by build commands or
persisted. Redirects are disabled on the credential-scoped origin to prevent
cross-origin forwarding. Without `secret_ref`, Git retains its existing
credential-helper behavior; an explicit reference never falls back when
resolution fails.

Git-backed Cargo methods can also use `secret_ref` with a credential-free HTTPS
`git` URL. depengine performs the authenticated clone and revision fetch first,
then invokes `cargo install --path` on the temporary local checkout without the
token-bearing Git environment. This keeps the credential out of Cargo itself,
including crate build scripts and procedural macros. An explicit reference
fails closed when it cannot be resolved.

Bearer credentials are retained across same-origin redirects and removed before
following a cross-origin redirect. The primary artifact credential is never
implicitly reused for checksum or signature sidecars. A checksum credential is
sent only to an explicitly configured `checksum_url`; inferred checksum URLs
remain anonymous. A signature credential is sent only to `signature_url`.

depengine redacts common token/password flags, Authorization/Cookie headers,
and URL userinfo from subprocess output where possible. State persistence also
rejects values that look like secrets. Redaction is a fallback, not a safe way
to store credentials in configuration.

## Privilege escalation

Some package managers and installer formats need elevated privileges. Keep the
privileged operation as narrow as the adapter allows. Running an installer as
root or Administrator does not make the installer trustworthy.

Opaque vendor installers and arbitrary scripts provide less inspectable
ownership and rollback behavior than package managers or verified archives.

## Package sources and keys

Adding a repository, tap, bucket, or signing key changes host configuration
beyond one package. depengine tracks these changes per candidate where the
adapter supports it and avoids deleting shared resources without ownership
evidence.

Shared-source reference counting is still incomplete, so source cleanup should
be conservative.

## Lockfile

`depengine.lock` improves reproducibility; it is not a trust root. Review lock
changes like source changes and protect the repository that stores them.

`--frozen-lockfile` prevents implicit lock updates. It cannot tell you whether
an upstream registry, package, signing key, or mutable reference is trustworthy.

Lock coverage is not universal yet. See
[support boundaries](support-boundary.md) for the current limits.

## Reporting vulnerabilities

Do not include tokens, private manifests, or other credentials in public bug
reports. Follow the repository's `SECURITY.md` instructions when available.
