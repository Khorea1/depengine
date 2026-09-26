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
  before/after). They run only for the selected plan and matching transition.
- Pre-hook failure aborts the transition before mutation; post-hook failure
  reports against an already-committed transition and never triggers implicit
  compensating uninstall/remove.
- Hooks are serialized in the selected plan and always require the
  arbitrary-code capability.
- Durable "ensure this state exists" behavior is a separate checked primitive
  (`EnsureAction`: explicit read-only check plus mutating apply). Hooks are
  events only and never substitute for idempotent state.

## Alternatives considered

- Candidate/method-local hook declarations: deferred — only with a strong use
  case; declarative primitives are preferred over more hook surface.
- Treating hooks as idempotent state with implicit re-run: rejected — status
  must not report a tool healthy merely because a one-time hook once ran.

## Open work

- Manifest/parser integration of the candidate-local plan model.
- Production planner/executor integration (hook cannot leak across candidates).
- Status integration (checked `EnsureAction` vs one-time hook runs).
