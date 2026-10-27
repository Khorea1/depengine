# reports/

Active reports: audits, reviews, verification records, spikes, and
council/investigation output. Naming: `<kind>-<slug>-<YYYY-MM-DD>.<ext>` with
`kind` in `report` `audit` `verification` `spike` `council` `investigation`.
Finished or superseded reports move to
[`../archive/reports/`](../archive/reports/) with a date-first name.

| Report | Purpose |
|--------|---------|
| `report-scalability-planning-2026-10-16.md` | Custo de parse/planejamento por candidato, CPU vs I/O, achado do portão de código arbitrário sobre candidatos inelegíveis e veredito de trabalho para catálogo/import (preliminar, não é ADR) |
| `report-spike-catalog-identity-2026-10-17.md` | Identidade de candidato sob especialização por host (spike `pyx`), medição dos forks do gate e desfecho das decisões do mantenedor (preliminar, não é ADR) |
| `report-spike-pyx-catalog-cost-2026-10-18.md` | Custo do manifest × tamanho do catálogo (eager quadrático por go-toml vs lookup plano), inventário dos caminhos de carga (D2) e identidade do lookup (spike `pyx`, rodada 2) |
| `report-update-projection-2026-10-25.md` | Refactor de `dep-ixd.10`: remove decodificações redundantes da projeção v2 no `update`; ganho de ~8–11% em 5.000 ferramentas, sem mudança de lock (preliminar, não é ADR) |
| `report-graph-frontier-2026-10-27.md` | `dep-ixd.12`: `SortGraph`/`Analyze` passam a usar fronteira incremental de grau zero; ganho de 1,7× a 23,7× em 1.000/5.000 nós e fan-in 5/20, com oráculo de equivalência da varredura anterior (preliminar, não é ADR) |
| `report-scalability-round2-2026-10-25.md` | Segunda rodada de escalabilidade: custo de `update`/`lock` (v1 vs projeção v2) e de `why`/`explain` com 5.000 ferramentas, além de grafo denso (5.000 nós, fan-in 5 e 20); hot spots candidatos sem otimização aprovada (preliminar, não é ADR) |
| `audit-worktree-inventory-2026-10-19.md` | Quais worktrees locais já estavam integradas em `master` e quais carregam trabalho real não integrado; inclui as rodadas de 2026-10-19 e 2026-10-25, esta última com `fix/beads-front-b` classificada como superseded e o trabalho de `fix/front-a-artifacts` encerrado nos PRs #165/#166 |
