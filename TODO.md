# Active TODO

See [`docs/roadmap.md`](../docs/roadmap.md) for the authoritative unfinished
backlog. Keep this file for session-specific work only; durable project work
belongs in the roadmap or the relevant design/spec document.

## Ideas

- [ ] Consider whether `--yolo` should alias `--allow-arbitrary-code`, or
  whether the shorter name should replace it. This remains an unvetted CLI
  design idea; do not implement without a compatibility decision.

## Research-log review findings

- [ ] Review retained executor configuration on sequential reuse: `internal/exec/run.go` only replaces `defaultMethodOrder` when the next schema supplies an order, and `nativeManagerName` when the next clan resolves. A later omitted order or unsupported clan retains the preceding selection. The Research Log Execution Plan §9 explicitly excludes these fields from the run-state cutover; its new reuse regressions cover reports, failures, dependency identities, recovery, and source observations, not host/order reconfiguration.
