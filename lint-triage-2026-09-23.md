# Lint triage — full backlog, uncapped (2026-09-23)

Method: `golangci-lint v2.13.2` with `.golangci.yml` minus `new-from-rev`,
`max-same-issues: 0`, `max-issues-per-linter: 0`.
Temp config + raw log live outside the repo (`/tmp/opencode/golangci-full.yml`,
`/tmp/opencode/lint-full-uncapped.log`). No tracked file touched;
`git status` clean; `go build ./...` exit 0; `go test ./internal/plan/` ok.

## Totals (uncapped)

| Linter | Count | Capped run (`max-issues-per-linter: 50`) |
| --- | --- | --- |
| errcheck | 159 (76 test / 83 prod) | 50 |
| gosec | 368 (296 test / 72 prod) | 50 |
| staticcheck | 19 | 19 (complete) |
| errorlint | 11 | 11 (complete) |
| unused | 1 | 1 (complete) |
| govet, ineffassign | 0 | 0 |

Total: **558**. Roadmap's 2026-09-22 audit (142: errcheck 50, gosec 50,
staticcheck 21, errorlint 11, unused 10) was itself capped at 50/category and
is now stale: staticcheck 21→19, unused 10→1 improved via recent cleanups
(`d46d519`, `40b0bcf`, `d36333c`); errcheck/gosec true counts are ~3x/~7x the
quoted numbers.

## Verdicts

- **errcheck 159 → VERMELHO (policy decision needed).** Top patterns:
  `fmt.Fprintf` 31 + `fmt.Fprintln` 9 + `fmt.Fprint` 1 (CLI output,
  best-effort); `defer *.Close` ~60 (`ls.Close` 25 across `internal/app/*`,
  `f.Close` 10, `resp.Body.Close` 4, `gz.Close` 2, lock/state/prepare closes);
  `os.Setenv/Unsetenv` 15; `os.RemoveAll` 8; `json.Unmarshal` 6;
  `os.WriteFile` 5; `os.Remove` 3; `os.Stderr.WriteString` 2.
  Hot files: `downloadcache/cache_test.go` 23, `state/preparation_transaction_test.go`
  13, `ghrelease/resolver_test.go` 10, `app/graph_why.go` 10, `app/diff.go` 10.
  Fixing means choosing per-site semantics (ignore explicitly vs propagate);
  precedent `0d4a534` handled best-effort returns. Not autonomous work.
- **gosec 368 → VERMELHO (security policy needed).** Test files hold 296
  (G304 file-inclusion-via-var on `t.TempDir()` paths, G306/G301/G302
  permissions, G101 fake `user:secret@example.test` fixture URLs).
  The 72 prod findings concentrate where risk is real:
  `internal/httpdownload/*` (G304 path use, G301/G306 perms, G305 zip
  traversal in `extract.go:398`, G122 WalkDir TOCTOU in
  `archive_install.go:280`, G110 decompression bomb in
  `localartifact/install.go:579`, G115 int-overflow conversions,
  G401/G501/G505 md5/sha1 in `checksum.go` — checksum-compat question),
  `internal/state/*` + `internal/config/*` + `internal/engine/facts.go:36`
  (G703 taint path-traversal trio), `internal/run/runner.go:204` G204
  (subprocess-with-variable is the runner's job; needs documented accept),
  `internal/exec/report_unix.go:26` G103 unsafe. Each needs a human
  accept/fix/`#nosec`-with-reason call. Not autonomous work.
- **staticcheck 19 → VERDE-com-diff, exceto 2 AMARELO.**
  Verde (behavior-preserving idioms): S1040 `contract_test.go:22`; S1011
  `app/helpers.go:230,234`, `config/model.go:208`; QF1012 x2 in
  `app/init.go:253,274` (strings.Builder; errcheck-clean);
  QF1001 x3 (`containerref/ref.go:82`, `downloadcache/cache.go:145`,
  `plan/placement.go:204`); S1016 `plan/lock.go:985`.
  Exceção verificada em 2026-09-23 (branch agent/lint/apply-greenlist-fixes):
  QF1012 x6 em `state/hash.go` foi revertido — `fmt.Fprintf(h, …)` troca
  staticcheck por 6 novos errcheck que o gate `new-from-rev` bloqueia, enquanto
  `h.Write(…)` bare passa limpo. Fica no backlog até haver política errcheck.
  Amarelo: SA1019 x2 `localartifact/install.go:626,642` (`tar.TypeRegA`
  removal changes GNU-tar compat — check test coverage first); ST1005
  `plan/plan.go:456` (error-string capitalization — grep tests asserting the
  message first).
- **errorlint 11 → 9 VERDE-com-diff (test-only `errors.As`/`errors.Is`
  conversions), 2 VERMELHO (prod error-chain semantics):
  `internal/exec/preparation.go:334`, `internal/httpdownload/gpg.go:196`.**
- **unused 1 → VERDE:** `internal/localartifact/localartifact.go:203`
  `checksumFile` (deadmono agrees; only name collisions with unrelated locals;
  no reflective use). Removal + `go test ./internal/localartifact/` is the
  next-queue item.

## Green list for next queue (diff mínimo, auditoria leve)

1. Remove `checksumFile` (`internal/localartifact/localartifact.go:203`).
2. 9 errorlint test-only conversions.
3. staticcheck idiom fixes except SA1019/ST1005.
4. Dedup double NUL check in `ClaimResource`
   (`internal/plan/preparation.go:427-432`, found during prep mapping;
   not linter-flagged, behavior-preserving).

## Refresh (2026-09-24)

Method repeated: `golangci-lint v2.13.2`, `.golangci.yml` minus `new-from-rev`,
`max-same-issues: 0`, `max-issues-per-linter: 0`, run against the trunk at
`roadmap-p0-finish @ bfdbfb8` and reproduced with identical totals from
`test/plan-normalization-fuzz` (`bfdbfb8` + three lint-clean commits), so the
numbers below do not depend on either branch's additions. Config and raw logs
stay outside the repo (`/tmp/opencode/golangci-full.yml`,
`/tmp/opencode/lint-full-uncapped-2026-09-24.log`,
`/tmp/opencode/lint-full-uncapped-worktree.log`). `git status` clean before
and after.

| Linter | 2026-09-24 | 2026-09-23 |
| --- | --- | --- |
| errcheck | 158 (76 test / 82 prod) | 159 |
| gosec | 363 (298 test / 65 prod) | 368 |
| staticcheck | 9 | 19 |
| errorlint | 2 | 11 |
| unused | 0 | 1 |
| govet, ineffassign | 0 | 0 |
| **Total** | **532** | 558 |

### Green list: consumed

All four next-queue items landed: `checksumFile` removal (`8aa84c6`,
`unused` 1 → 0), the 9 test-only `errorlint` conversions (`errorlint` 11 → 2),
the behaviour-preserving `staticcheck` idiom fixes (19 → 9 minus the deferred
batch below), and the `ClaimResource` dedup (`248b39c`). Nothing green is left
in the queue; what remains is policy, yellow, or blocked work.

### What remains and why

- **errcheck 158 → policy still required** (verdict unchanged). Hot files and
  patterns are stable: `downloadcache/cache_test.go` 23,
  `state/preparation_transaction_test.go` 13, `ghrelease/resolver_test.go` 10,
  `app/graph_why.go` 10, `app/diff.go` 10; top patterns `fmt.Fprintf` 31,
  `ls.Close` 25, `w.Write` 21, `fmt.Fprintln` 9, `f.Close` 9, `os.Unsetenv` 8,
  `os.RemoveAll` 8, `os.Setenv` 7, `json.Unmarshal` 6, `os.WriteFile` 5.
- **gosec 363 → security policy still required** (verdict unchanged). Rule
  split now: G306 146, G304 91, G301 86, G101 13, G302 11, G204 8, G703 5,
  G115 2, G122 1. Test files hold 298 of them; production is down to 65.
- **staticcheck 9 = yellow + a blocked batch whose blocker is now gone:**
  - `SA1019` ×2 `internal/localartifact/install.go:668,687` (`tar.TypeRegA`;
    removal changes GNU-tar compat — check coverage first);
  - `ST1005` ×1 `internal/plan/plan.go:456` (grep tests asserting the message
    first);
  - `QF1012` ×6 `internal/state/hash.go:40,88,107,196,198,207` — **approach
    verified 2026-09-24, not applied**: rewriting each site as
    `_, _ = fmt.Fprintf(h, …)` keeps the gate green
    (`golangci-lint run ./internal/state/` → 0 issues) and
    `go test ./internal/state/` passes, so the errcheck objection that forced
    the revert on `agent/lint/apply-greenlist-fixes` does not hold for the
    explicit-discard form. This is the next queue item; the diff was reverted
    from `test/plan-normalization-fuzz` after verification to keep that branch
    focused.
- **errorlint 2 = both production sites, still red:**
  `internal/exec/preparation.go:373` formats two errors with `%v` in one
  message, and switching both to `%w` changes what `errors.Is`/`errors.As`
  matches for callers; `internal/httpdownload/gpg.go:196` formats `res.Err`
  where that field can be `nil` (the branch is entered on `ExitCode != 0`
  alone), so a naive `%w` would print `%!w(<nil>)` and needs a nil guard first.

## Refresh (2026-09-26)

Same method: `golangci-lint v2.13.2`, `.golangci.yml` minus `new-from-rev`,
`max-same-issues: 0`, `max-issues-per-linter: 0`, run against clean `master` at
`12e1f9a` (PR #67 merged). Raw log:
`/tmp/opencode/lint-full-uncapped-2026-09-26.log`. No timeout or analyzer
errors; issue count matches the summary exactly (506).

| Linter | 2026-09-26 | 2026-09-24 | 2026-09-23 |
| --- | --- | --- | --- |
| errcheck | 154 (76 test / 78 prod) | 158 (76 / 82) | 159 (76 / 83) |
| gosec | 341 (298 test / 43 prod) | 363 (298 / 65) | 368 (296 / 72) |
| staticcheck | 9 | 9 | 19 |
| errorlint | 2 | 2 | 11 |
| unused | 0 | 0 | 1 |
| govet, ineffassign | 0 | 0 | 0 |
| **Total** | **506** | **532** | **558** |

Production/test split overall: 132 production, 374 test.

### What changed

- **gosec 363 → 341 (production 65 → 43)** across PRs #55–#67: the root-scoped
  archive copies, owner-only storage tightening, and Git artifact-copy hardening.
  `G115` (2) and `G122` (1) no longer appear at all — both were closed per
  [`docs/gosec-triage.md`](../docs/gosec-triage.md). Remaining rule split,
  total (production): `G306` 144 (5), `G304` 83 (22), `G301` 80 (11),
  `G101` 13 (0), `G302` 11 (3), `G204` 6 (0), `G703` 4 (2).
  Production is now dominated by `G304` + `G301`, matching that doc's
  "remaining priorities".
- **errcheck 158 → 154**, pattern shape unchanged: `Close` 50, `Fprintf` 31,
  `Write` 21, `Fprintln` 9, `Unsetenv` 8, `RemoveAll` 8, `Setenv` 7,
  `Unmarshal` 6, `WriteFile` 5. Still blocked on the per-site policy decision.
- **staticcheck 9 = the documented set, unchanged.** No new findings.
  - `SA1019` ×2 `internal/localartifact/install.go:713,736` (drifted from
    `:668,687`; `tar.TypeRegA` GNU-tar compat question unchanged);
  - `ST1005` ×1 `internal/plan/plan.go:456`;
  - `QF1012` ×6 `internal/state/hash.go:40,88,107,196,198,207` — line numbers
    identical to 2026-09-24, so the verified `_, _ = fmt.Fprintf(h, …)` recipe
    applies verbatim.
- **errorlint 2, both production sites, unchanged.**
  `internal/exec/preparation.go:373` (drifted from `:334`) and
  `internal/httpdownload/gpg.go:196` (unchanged).

### Note for the tracked doc

`docs/gosec-triage.md` records its last full uncapped count as 401 diagnostics
(52 production / 349 tests, reviewed 2026-09-25) and explicitly asks for a
rerun before adopting a new repository total. This 2026-09-26 run is that
rerun: **gosec 341 (298 test / 43 production)**. The drop from 401 is
consistent with PRs #55–#67 landing after that review. Treat this as the
candidate figure for whoever next updates the tracked doc — same method, one
day newer.
