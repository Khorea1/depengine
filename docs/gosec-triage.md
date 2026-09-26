# gosec triage

Reviewed on 2026-09-26 with golangci-lint v2.13.2, without the
`new-from-rev` baseline or diagnostic caps. The original 2026-09-24 audit
reported 420 diagnostics: 72 in production and 348 in tests. Successive
hardening passes reduced production findings through 56, 52, and 43. The
current uncapped scan reports 348 diagnostics, all in tests: production code
has no unsuppressed `gosec` diagnostics. These are scanner diagnostics, not a
count or severity score of confirmed vulnerabilities.

Reproduce with a temporary configuration:

```yaml
version: "2"
run:
  timeout: 5m
  tests: true
linters:
  default: none
  enable:
    - gosec
issues:
  max-same-issues: 0
  max-issues-per-linter: 0
```

```sh
golangci-lint run --config /tmp/depengine-gosec.yml \
  --output.json.path /tmp/gosec.json
```

The command exits unsuccessfully while diagnostics remain. Keep the general
lint baseline until the complete backlog is resolved.

## Reviewed cases

| Diagnostic | Location | Disposition |
|---|---|---|
| G401/G501/G505 | `internal/httpdownload/checksum.go` | Explicit MD5/SHA-1 support is part of the schema contract. Four narrow suppressions document legacy checksum compatibility; security documentation explains the collision-resistance limitation. |
| G305 | `internal/httpdownload/extract.go`, `validateTarSafety` | Fixed the validation gap: TAR hard-link targets are checked from the archive root, while symlink targets are normalized from the member directory. A narrow suppression covers that normalization because `safeJoin` immediately checks containment. External tar implementations may independently reject unsafe links; this is not evidence of an exploit on every backend. |
| G110 | `internal/localartifact/install.go`, archive extraction | Closed for local/offline ZIP and TAR materialization. Extraction is bounded by 4 GiB of aggregate regular-file bytes, 100,000 entries, 4,096-byte entry names, and 256 path components. The destination is replaced only after extraction succeeds. A narrow suppression documents the bounded stream; remote archive backends are outside this finding and retain their separate contracts. |
| G115 | `internal/localartifact/install.go`, TAR modes | Closed. Signed TAR mode values are checked to lie in `0..07777` before masking to `0777` and converting to `os.FileMode`; negative and oversized values have regression coverage. Permission validation after conversion is unchanged. |
| G204 | `internal/run/runner.go`, `internal/run/session.go` | Reviewed as the intentional process-launch boundary. `OSExecRunner` accepts runtime-selected executable/argv from validated adapters or explicitly authorized hook execution and passes argv directly to `os/exec` without shell-string construction. The elevation helper always launches literal `sudo`; its variable args are internal session controls. Narrow suppressions document these two reviewed sites instead of excluding G204 globally. |
| G301 | `internal/state` state/snapshot/lock directories; `internal/downloadcache/cache.go` | Fixed. Per-user state, snapshots, lock storage, and downloaded-artifact cache directories are now owner-only (`0700`). Existing permissive directories are tightened when used on Unix rather than only applying the safer mode to fresh directories. Windows keeps native ACL semantics. Regression tests start with `0755` directories and require the tightening step. |
| G122 | `internal/httpdownload/archive_install.go`, `copyTreeStripped` | Fixed. The stripped-copy phase now opens source and destination as `os.Root` handles, walks the source through `os.Root.FS()`, and performs `Lstat`, `Readlink`, directory creation, file open/create, and symlink creation relative to those roots. Symlink targets must remain contained both before and after component stripping, and non-regular staged entries are rejected. This closes the path-validation/use race instead of suppressing G122. |
| G304 | config, lock, state, checksum, download/cache, and local-artifact path boundaries | Reviewed. These call sites intentionally accept operator-selected paths or consume paths generated inside depengine-owned temporary/private roots. Narrow call-site suppressions document the trust boundary instead of disabling G304 globally. The local-artifact open still revalidates file identity after open. |
| G301/G302/G306 | project files, installed payload roots, launcher directories, desktop entries, and executable shims | Reviewed. Shared project artifacts remain `0644`, installed/launcher directories remain traversable, and executable shims remain executable. Narrow suppressions document those semantics. The state lock file was tightened from `0644` to `0600`; copied temporary GPG key material was tightened from `0644` to `0600`. |
| G304 | `internal/state/snapshot.go`, snapshot enumeration | Hardened. Snapshot listing now rejects symlink directory entries before opening them; the remaining dynamic read is constrained to non-symlink `state-*.json` entries under the owner-only snapshot directory and is narrowly suppressed. |
| G703 | `internal/config/manifest.go`, `DefaultManifestPath` | Intentional operator-selected manifest path from environment/XDG configuration. The reported operation only probes existence; a narrow suppression now records that reviewed trust boundary. |
| G703 | `internal/engine/facts.go`, detector selection | Intentional operator-selected executable override. An attacker-controlled process environment is a separate trust concern; the warning alone does not establish traversal. No suppression added. |
| G703 | `internal/state/snapshot.go`, snapshot write | Generated timestamp filename under the operator-selected owner-only state root. No untrusted archive/document filename reaches this write; a narrow suppression records that reviewed boundary. |

## Remaining priorities

- Review the remaining 348 test-only diagnostics independently. Temporary
  paths, intentional executable permissions, fake credentials, and deliberately
  permissive fixtures account for much of the backlog; do not blanket-disable
  `gosec` for tests.
- Prefer changing fixture permissions to owner-only where the test does not
  exercise permission semantics. Where a broader mode, dynamic test path,
  synthetic credential, subprocess, or intentionally ignored cleanup error is
  part of the fixture contract, use a narrow call-site suppression with a
  reason.

The current uncapped count is 348 diagnostics, all in tests. Production code
has zero unsuppressed `gosec` diagnostics under the command above. Keep the
general lint baseline until the test fixture backlog is also resolved.
