# Author-variant audit (2026-09-23)

Scope: read-only. No `.mailmap` change, no history rewrite.
Key finding up front: **`.mailmap` already exists, is tracked, and is
complete** — the roadmap line "audit author variants before adding
`.mailmap`" is stale. This audit verifies it instead.

## Variant table (HEAD history; `--all` adds 6 commits on agent branches, same 7 identities)

| Identity | Commits | Range | Evidence |
| --- | --- | --- | --- |
| `khorea1 <khorea@disroot.org>` (canonical) | 233 | 2026-09-14..09-23 | Canonical per `.mailmap:1` + AGENTS.md commits convention |
| `khorea <khorea@disroot.org>` | 141 | 2026-07-22..09-14 | Same email as canonical; differs only in `user.name` case |
| `khorea <khorea@motor.local>` | 136 | 2026-07-09..07-19 | Earliest history incl. `1f92622` Initial commit; ends exactly where disroot identity starts (07-19 → 07-22): machine without `user.email` configured, then configured |
| `Khorea1 <141942237+Khorea1@users.noreply.github.com>` | 36 | 2026-09-22..09-23 | All sampled commits are PR-merge/branch-sync commits on own repo (`91ee193` Merge PR #12, `cc08018` #8, `617c910` #5…); local part embeds owner's username (numeric prefix not independently verified) |
| `Khorea1 <khorea@disroot.org>` | 17 | 2026-09-04..09-14 | Same email as canonical modulo `user.name` case |
| `Khorea1 <Khorea@disroot.org>` | 4 | 2026-09-05..09-10 | Same email modulo case in both name and domain |
| `khorea <khorea@localhost>` | 1 | 2026-09-21 | `d26539a` feat: normal project commit; `user@localhost` fallback on a machine without `user.email` |

## Verification performed

- `git check-mailmap` on all 6 aliases → resolves to
  `khorea1 <khorea@disroot.org>`. No unmapped variant.
- `git shortlog -sne --all` → single line: `574 khorea1 <khorea@disroot.org>`.
- `git log --all --format='%an <%ae>' | sort -u` → exactly the 7 identities
  above; **no 8th identity** hiding on agent branches
  (`agent/*` remotes hold the 6 extra commits, all under known identities).
- No other contributor identities exist in history.

## Conclusion

All 6 aliases confirmed as same-person variants with per-row evidence above.
`.mailmap` entries match 1:1. No action taken (per plan + roadmap constraint).
Roadmap/TODO wording should drop "before adding `.mailmap`" since the file
exists — suggested follow-up is docs-only.
