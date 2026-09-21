# TODO-0 — remaining work from review 2026-09-21 (PRIORITY over `.dev/TODO.md`)

> Source: static review of `master` (510bb83) + `origin/dev-termux` (d26539a).
> Status as of 2026-09-21 session. `.dev/TODO.md` keeps the full P0–P3 backlog;
> this file tracks only what is still open, in execution order. Ground rules
> still apply: descriptive branches, atomic Conventional Commits (EN, author
> `khorea1 <khorea@disroot.org>`), merge only with build+tests+lint green,
> P2 scope frozen until P0/P1 close.

## Done (do not reopen)

- Phase 0 (dev-termux merge, resolved-plan compatibility), S.1 (process
  lifecycle: `WaitDelay`/process groups/`NotifyContext`/bounded output/WAL),
  S.4 (`pkg/` → `internal/`, config/graph decoupled), S.3 bulk (thin
  `main.go`, single `os.Exit`, `internal/app`), S.5 (pins, `-race`,
  coverage, 5 linters with `new-from-rev` baseline, cosign+provenance+SBOMs),
  S.6 (`ghrelease.Resolver`, `run.Elevator`, `exec.Registry`), S.7
  (ADRs 001/002, completed-item collapse, P2 freeze banners).

## 0. Merge/inventory (first)

- [ ] `fix/resolved-plan-compatibility` — 0 commits ahead of `master`: merged, delete branch.
- [ ] `refactor/internal-packages` — 0 commits ahead of `master`: merged, delete branch.
- [ ] `chore/supply-chain-and-global-state` — 7 commits (S.5+S.6): CI green, merge to `master`.
- [ ] `chore/todo-hygiene` — 2 commits (S.7): merge to `master` (docs/`.dev` only, no code risk).
- [ ] `refactor/cli-application-layer` — 3 commits, S.3 tail below still open: finish then merge.

## 1. S.3 tail — CLI structure (P1, in flight)

- [ ] Split oversized functions: `runUpgrade` (~624 LOC), `runRemove`
  (~433), `runStatus`, `runInstall`, `runUndo`, `Executor.Execute`
  (~341), `Executor.tryMethods` (~308).
- [ ] Acceptance: commands unit-testable without spawning the binary; no `os.Exit` outside `main`.

## 2. S.2 — `AdapterV2` strangler migration (P1, biggest item left)

- [ ] Read `internal/plan`, `internal/exec/execute.go`, all adapters in full first.
- [ ] Define `AdapterV2` over `plan.Operation`; `Check` returns `VerificationResult`, not `bool`.
- [ ] Shim for legacy adapters; one adapter per commit; delete legacy interface at the end.
- [ ] Fold `Remover`, `AvailabilityChecker`, `PlanResolver`,
  `HostCompatibilityChecker` into the new interface (no new optional interfaces meanwhile).
- [ ] Per-adapter conformance tests; no adapter migrates without them.
- [ ] Acceptance: one method-specific resolution path per candidate; `why`, dry-run, install agree.

## 3. S.7 remainder (low priority, opportunistic)

- [ ] Partial sections still carry long `[x]` parenthetical prose (P0.3, P0.4, P1.1–P1.3, P1.5, P1.6, P1.8, P1.10, P1.11, P3.3, P3.4): strip or promote to ADRs as touched.
- [ ] Do not bulk-strip: evidence notes are still the record until code/tests fully speak.

## 4. Blocked until P0/P1 close

- [ ] P2 method fidelity/ergonomics (frozen — see banners in `.dev/TODO.md`).
- [ ] P3 adversarial fixture matrix, conformance suites, v1 freeze gate.
