# ADR-004: Scope supplies platform-native placement; explicit absolute paths override

- Status: decided (2026-09-22)
- Source: `.dev/TODO.md` P1.5 working notes (condensed here; the backlog keeps only open items)

## Context

User/system/global behavior was encoded through method-specific flags or raw
paths, and artifact defaults differed between archives and raw binaries —
making the same install intent platform-dependent and surprising, and forcing
normal manifests to know `~/.local/bin` or `ProgramFiles`.

## Decision

- Portable scope vocabulary (`user`/`system`; manager-specific distinctions
  only where semantically necessary) is canonical; adapter aliases map onto it
  only when semantically equivalent.
- Scope supplies placement defaults resolved to platform-native paths in the
  planner, host-independently: Unix follows the XDG Base Directory model
  (executables use the conventional `~/.local/bin`, since XDG defines no
  `XDG_BIN_HOME`); Windows uses explicit `LocalAppData`/`ProgramFiles` roots
  including UNC, never borrowing Unix defaults.
- Explicit install/link paths remain as advanced overrides and win
  independently — but must be absolute in the target OS path model
  (drive-relative roots, dot components, reserved device names, and ADS-like
  components are rejected before placement).
- Per-method portable scope mappings are declared as capabilities with
  fail-closed checks, so unsupported scope is detected at planning time.

## Alternatives considered

- Keeping Unix paths in the normal authoring path: rejected — platform
  assumptions leak into every manifest and raw-binary vs archive defaults
  diverge.
- Fully manager-specific scope vocabularies: rejected in favor of one portable
  core, extended only where a manager distinction is semantically real.

## Open work (remains in `.dev/TODO.md` P1.5)

- Adapter wiring for remaining blocked adapters.
- Removing Unix paths from the normal authoring path once scope suffices.
- Proving user-scoped raw binary and archive installs resolve consistently.
