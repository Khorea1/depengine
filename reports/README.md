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
| `spike-cases-expansion-2026-10-11.md` | Spike A vs C (`dep-1n0.1`): expansão estática de receitas vs manter candidatos; base do `plan-manifest-deduplication-investigation` |
