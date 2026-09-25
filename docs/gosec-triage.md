# gosec triage

Reviewed on 2026-09-25 with golangci-lint v2.13.2, without the
`new-from-rev` baseline or diagnostic caps. The original 2026-09-24 audit
reported 420 diagnostics: 72 in production and 348 in tests. Before the
owner-only storage changes in this pass, the current tree reported 409
diagnostics: 60 in production and 349 in tests. After those changes, the
uncapped scan reports 405 diagnostics: 56 in production and 349 in tests.
These are scanner diagnostics, not a count of confirmed vulnerabilities.

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
| G122 | `internal/httpdownload/archive_install.go`, `copyTreeStripped` | Current callers use private temporary staging directories. The reported race requires access to those staging trees; source and destination symlink containment is checked. No suppression added in this pass. |
| G703 | `internal/config/manifest.go`, `DefaultManifestPath` | Intentional operator-selected manifest path from environment/XDG configuration. The reported operation probes existence. No suppression added. |
| G703 | `internal/engine/facts.go`, detector selection | Intentional operator-selected executable override. An attacker-controlled process environment is a separate trust concern; the warning alone does not establish traversal. No suppression added. |
| G703 | `internal/state/snapshot.go`, snapshot write | Generated filename under the operator-selected local state root. No untrusted archive/document filename reaches this write. No suppression added. |

## Remaining priorities

- Review the remaining path, permission, subprocess, and unsafe-operation
  diagnostics at their call sites. The current production backlog is dominated
  by dynamic-path G304 findings and install/output permission findings; neither
  category should be excluded globally.
- Review fixture warnings independently. Temporary paths, intentional
  executable permissions, and fake credentials account for many diagnostics;
  do not blanket-disable gosec for tests.

The current uncapped count is 405 diagnostics: 56 in production and 349 in
tests. The count is not a severity score; rerun the uncapped command after
future changes to measure the current backlog.
