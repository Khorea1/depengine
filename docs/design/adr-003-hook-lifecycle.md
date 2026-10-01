# ADR-003: Hooks run during candidate transitions

- Status: decided (2026-09-22)
- Origin: implementation working notes, condensed into this ADR

## Context

Hooks were transition events confused with durable desired state: a global
`pre_install` could run before the winning candidate was known, or run for a
method it was never meant for (e.g. an apt-only hook firing when the GitHub
fallback wins). Post-install coverage was also partial across transitions,
and a one-time hook run could masquerade as ongoing health.

## Decision

- Hooks are candidate-local transition events bound to the selected plan and
  one exact transition (`install`/`upgrade`/`repair`/`remove` ×
  before/after). Tool-level hooks are generic declarations projected onto the
  selected candidate; method-local hooks belong only to that candidate. They
  run only for the selected plan and matching concrete transition.
- Pre-hook failure aborts the transition before mutation; post-hook failure
  reports against an already-committed transition and never triggers implicit
  compensating uninstall/remove.
- Hooks are serialized in the selected plan and always require the
  arbitrary-code capability.
- Durable "ensure this state exists" behavior is a separate checked primitive
  (`EnsureAction`: explicit read-only check plus mutating apply). Hooks are
  events only and never substitute for idempotent state.

## Alternatives considered

- Tool-level hooks only: rejected. A hook that is meaningful only for one
  candidate (for example native/apt preparation) must not fire when another
  fallback candidate wins. Candidate-local declarations provide that boundary;
  declarative primitives remain preferred for durable state.
- Treating hooks as idempotent state with implicit re-run: rejected — status
  must not report a tool healthy merely because a one-time hook once ran.
## Integration status

- Manifest/parser, planner, executor, dry-run, and native-batch paths carry
  candidate-local hooks through the selected `ResolvedInstallPlan`.
- Resolvers must preserve lifecycle hooks from candidate intent; before-hooks
  run only after the candidate has survived the availability gates needed to
  select it, and the exact reconciliation result selects install vs upgrade.
- Status treats hooks as events rather than desired state. State stores a
  hook-free desired-state hash for drift detection and never uses historical
  hook completion as health evidence.
- State-tracked replacement persists `PostHookRunning` before an after-upgrade
  hook. The postinstall-completion flag and replacement-WAL removal are saved
  together, including when the hook reports failure. A restart in that phase
  blocks rather than replaying a hook whose side effects may have completed; a
  returned hook failure leaves the replacement installed and does not trigger
  compensation. The CLI upgrade flow uses this resolved replacement path.

## Open work

- Expose checked `EnsureAction` declarations through the manifest/status UX if
  a durable "ensure this state exists" surface is needed.
