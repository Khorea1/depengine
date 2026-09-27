# Active TODO

See [`docs/roadmap.md`](../docs/roadmap.md) for the complete unfinished backlog.
Lint work is tracked with per-linter verdicts in
[`.dev/lint-triage-2026-09-23.md`](lint-triage-2026-09-23.md); security findings
in [`docs/gosec-triage.md`](../docs/gosec-triage.md).

## Lint

- [ ] Triage the full-repository lint backlog (open policy decisions on
  `errcheck`/`gosec`; see lint-triage verdicts).
- [ ] Convert the 2 prod `errorlint` `%w` sites: `internal/exec/preparation.go:373`
  and `internal/httpdownload/gpg.go:196` — check message assertions first
  (`gpg.go:196` formats `res.Err`, which can be nil on the `ExitCode != 0`
  branch, so it needs a nil guard before `%w`).
- [ ] Apply the verified `staticcheck` `QF1012` batch: 6 sites in
  `internal/state/hash.go:40,88,107,196,198,207` using the explicit-discard
  form `_, _ = fmt.Fprintf(h, …)` (recipe verified 2026-09-24, gates green).

## go-critic

- [ ] `main.go:36` `exitAfterDefer` — `os.Exit(exitErr.Code)` runs before
  `defer stop()` (line 29), so the signal-context cancel never fires on the
  `ExitError` path. Low impact (the process is exiting anyway); still, both
  `os.Exit` sites (`:36`, `:39`) bypass the defer.
- [ ] `internal/source/manager.go:250` `ifElseChain` — `apt-ppa`/`dnf-copr`
  branch reads as a switch.
- [ ] `internal/app/validate_check.go:236` `ifElseChain`.
- [ ] `internal/run/logging.go:177` `ifElseChain`.

## Ideas

- [ ] alias `--yolo` para `--allow-arbitrary-code`, ou renomear completamente.
