# Working-context cleanup

Use this procedure for the orphaned `.dev/` working-context branch. It keeps
scratch material useful without quietly turning it into a second source of truth.

## Routine

1. Classify each file as active work, reusable working context, cached external
   material, or historical residue.
2. Delete completed or superseded scratch notes unless their history has a
   plausible future use. Archive only the latter.
3. Keep the current state at the top of long-lived files. Remove completed TODOs
   instead of preserving a diary beneath them.
4. Refresh freshness-sensitive external material before relying on it. Cached
   material should retain its source, retrieval date, and relevant version.
5. Remove generated files, test caches, virtual environments, and other data that
   can be reconstructed cheaply.

## Promotion

Before promoting a statement out of `.dev/`, verify it against the current code
or authoritative project document. Then move it to the narrowest durable home:

- unfinished product or engineering work -> `docs/roadmap.md`;
- design rationale or a durable decision -> the relevant design document or ADR;
- stable repository-wide agent guidance -> `AGENTS.md`, sparingly;
- expensive-to-rederive semantic boundaries, invariants, or flows ->
  `architecture/`, with evidence and a recorded code revision.

Delete the scratch duplicate after promotion. Two authoritative-looking copies
are not redundancy; they are merely a future disagreement with extra steps.

## Archive policy

Use `archive/` only when chronology itself is likely to matter later. Archived
files must make their non-current status obvious near the top and must not appear
in active indexes or TODO lists.
