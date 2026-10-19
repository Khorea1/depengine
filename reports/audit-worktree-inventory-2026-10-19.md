# Auditoria de worktrees: o que já está integrado (2026-10-19)

Estado de `master` no momento da auditoria: `099cdc4` (após #151 e #152).

## Objetivo

Confirmar, worktree por worktree, quais árvores de trabalho locais já tiveram o
conteúdo integrado em `master` via PR e podem ser removidas, e quais carregam
trabalho real ainda não integrado.

## Método

1. `git worktree list` cruzado com `git rev-list --count origin/master..B` e
   `B..origin/master`, árvores sujas (`git status --porcelain`) e PR por branch
   (`gh pr list --head B --state all`).
2. **Contenção de conteúdo**: para cada branch, `M = merge-base(origin/master,B)`
   e, para cada arquivo de `git diff --name-only M..B`, mede-se quantas linhas
   adicionadas "distintivas" (len >= 12) **não** existem literalmente em
   `origin/master:<arquivo>`. Script: `/tmp/opencode/audit_branches.py`.
3. **Direção da divergência**: `git diff origin/master..B` distingue "falta na
   branch" (apenas atrasada) de "falta no master" (trabalho real).
4. Fechamento: nomes `Test*`/`Benchmark*` adicionados por cada branch, testados
   com `git grep -w` em `origin/master`.

O teste de merge (`git merge-tree --write-tree`) foi descartado: quase todas as
branches conflitam apenas em `CHANGELOG.md` (seção Unreleased), o que não
informa se o conteúdo já foi integrado.

## Resultado

| # | Branch | Commits ≠ master | PR | Veredito |
|---|--------|------------------|----|----------|
| 1 | `feat/github-distributions-E` | 30 | #148 (merged) | integrada |
| 2 | `fix/native-inventory-authority` | 4 | #149 (merged) | integrada |
| 3 | `investigate/recovery-old-identity` | 0 | (#149) | integrada |
| 4 | `fix/rpm-inventory-authority` | 1 | (#150) | integrada |
| 5 | `fix/git-observed-revision` | 5 | (#150) | integrada |
| 6 | `test/adapterv2-conformance` | 1 | (#150) | integrada |
| 7 | `test/platform-adversarial-validation` | 1 | (#150) | integrada |
| 8 | `fix/github-recovery-installing` | 28 | (#148) | integrada |
| 9 | `spike/cases-expansion` | 4 | — | protótipo isolado |
| 10 | `docs/installer-script-boundary` | 1 | — | não integrada |
| 11 | `fix/brew-tap-locked-install` | 1 | — | não integrada |
| 12 | `fix/macports-observation-hardening` | 2 | — | não integrada |
| 13 | `fix/beads-front-b` | 11 | — | não integrada |
| 14 | `fix/front-a-artifacts` | 9 | — | não integrada (+5 arquivos sujos) |

## Bloco A — integradas (removidas em 2026-10-19)

Contenção de conteúdo: #1 100% (4013/4013), #2 97% (resíduos são linhas que o
master reescreveu depois, já com `IsRpmClan`), #3 exata, #4–#7 100%, #8 100%
(resíduo: variante antiga e permissiva de `internal/config/strict.go`, substituída
pela guarda final do #148). Nenhum nome `Test*` adicionado ficou ausente em
`master` (0/0 em #3 e #7, 1/1 nos demais).

Removidas com `wt remove --force-delete`, mais os ramos remotos
`origin/feat/github-distributions-E` e `origin/fix/native-inventory-authority`.

Nota: os beads `dep-ua4` (conformance AdapterV2/executor), `dep-ua4.1` (Git
Observe do commit instalado) e `dep-rwx` (replay adversarial entre plataformas)
seguem abertos, mas o código correspondente já está em `master` — provavelmente
basta fechá-los após conferir os critérios.

## Bloco B — trabalho real não integrado (mantido)

- `docs/installer-script-boundary` (1 commit): parágrafo "Hooks are transition
  callbacks, not managed installation methods…" em `docs/schema-reference.md` e
  troca do exemplo `curl | sh` por forma argv. Nada disso existe em `master`
  (busca por "transition callbacks" = 0). Corresponde ao bead `dep-mhp` (blocked):
  scripts `curl|bash` / `irm|iex` não têm método equivalente.
- `fix/brew-tap-locked-install` (1 commit): `HOMEBREW_NO_AUTO_UPDATE=1` via
  `run.RunWithEnv` quando há fonte `brew-tap` com `Revision` resolvido
  (`runNativeInstallPackageResolved` + `resolvedHasPinnedBrewTap`), mais testes.
- `fix/macports-observation-hardening` (2 commits): MacPorts como manager
  explícito no macOS (`macports` no registry, `BuildManagerInstallCmd`,
  inventário fail-closed). 463/494 linhas ausentes em `master`.
- `fix/beads-front-b` (11 commits, 745/814 linhas ausentes): identidade de
  backend mise (asdf), aliases de registry (`omp@npm:...`), `why` com pacote
  nativo efetivo, MacPorts lifecycle + hardening, docs de lock v2 e gates de
  retirada do lock v1. Sobrepõe-se ao item anterior (código MacPorts duplicado).
- `fix/front-a-artifacts` (9 commits, 1041 linhas ausentes): GitHub
  distributions, executáveis portáteis Windows (`portable_executable` ausente em
  `master`), ciclo de vida de `.deb` dedicado (pacote `internal/deb/` ausente),
  escopo dos avisos de checksum, erros de assinatura de terminal. **Árvore suja**:
  `docs/schema-reference.md`, `internal/exec/terminal_test.go`,
  `internal/httpdownload/compatibility.go`, `internal/validate/structural_test.go`
  (M) e `internal/validate/auto_checksum_warning_test.go` (D).
- `spike/cases-expansion` (4 commits, 0% em `master`): protótipo isolado
  `internal/config/spike/` (expansão estática base+cases, goldens); ramo remoto
  `origin/spike/cases-expansion` existe. Já documentado em
  `spike-cases-expansion-2026-10-11.md`.

## Observações

- Nenhuma feature do Bloco B aparece em `docs/roadmap.md` nem em
  `docs/support-boundary.md` de `master`: não houve substituição por outra via.
- Todas as 14 worktrees estavam em commits de 11–14/out, anteriores a #151/#152.
- Só `fix/front-a-artifacts` tinha árvore suja.
- Branch local órfã `feat/macports-explicit` (`89cd848`, sem worktree): é
  **ancestral** de `fix/macports-observation-hardening`, ou seja, versão anterior
  e subsumida; pode ser apagada sem perda.
- Beads abertos após a auditoria: `dep-gate-arbitrary-code-ineligible-iyn`,
  `dep-gate-lean-operations-bit-7j4`, `dep-spike-catalog-host-specialization-pyx`,
  `dep-ua4`, `dep-ua4.1`, `dep-ua4.2`, `dep-mhp` (blocked), `dep-wbg` (blocked),
  `dep-docs-kernel-exact-example-smk`, `dep-rwx`,
  `dep-bench-round2-update-why-graph-h8c`.
