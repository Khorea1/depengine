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

## Follow-up da higiene (2026-10-19, `master @ 452ed48`)

As quatro branches do Bloco A que ainda tinham worktree foram integradas e
removidas:

| Branch | PR | Merge em `master` |
|--------|----|-------------------|
| `fix/ua4.2-opaque-git-lock` | #155 | `e065a13` |
| `fix/docs-kernel-exact-example` | #156 | `bae3519` |
| `fix/ua4.1-git-upgrade-cli` | #157 | `265afc0` |
| `fix/android-termux-sources` | #159 | `ff77068` |

Também foram removidos: a worktree temporária detached
`/tmp/opencode/depengine-4207205`, a branch órfã `feat/macports-explicit`
(ancestral de `fix/macports-observation-hardening`) e os refs remotos das três
branches squash-merged que ainda existiam no `origin`.

Trabalho real mantido: `fix/brew-tap-locked-install` (PR #154 aberto),
`fix/beads-front-b`, `fix/front-a-artifacts` (5 alterações locais preservadas) e
`fix/macports-observation-hardening`. `spike/cases-expansion` continua
preservada deliberadamente como experimento, conforme o
`plan-github-distributions` arquivado.

Beads abertos após esta rodada: `dep-mhp` (blocked; a fronteira documental foi
integrada em `f673b32`/#153, mas o contrato de ciclo de vida continua ausente),
`dep-wbg` (blocked) e `dep-bench-round2-update-why-graph-h8c` (P4 opcional).

## Follow-up da higiene (2026-10-25, `master @ 0620968`)

`git fetch --prune` removeu seis refs remotos já integrados. Rodada executada:
plano de issues arquivado (`plans/plan-open-issues-fix.md` →
`archive/plans/2026-10-25_...`, único item aberto era `#117`, fechado em
2026-10-03 e coberto por `internal/run`), WIP de `fix/front-a-artifacts`
commitado (`83d3b26`) e worktree/ramo `fix/portable-windows-exe` removidos
(espelho obsoleto de `efb32a4`/`424b12f`; arquivos de teste idênticos).

### `fix/beads-front-b` está superseded, não apenas divergente

A medição antiga (26% de linhas "ausentes em master") superestimava trabalho
pendente porque `master` reescreveu os mesmos trechos: a branch é a variante
**mais antiga**. Evidências, arquivo por arquivo:

- `internal/native/command.go`: a branch reintroduz `CheckCmd` para dpkg/RPM,
  removido de propósito quando a observação estruturada (`dpkg_observe.go`,
  `rpm_observe.go`) virou autoridade.
- `719b275` (MacPorts lifecycle, 446 linhas) foi substituído por `f985f67`/#161
  com implementação maior (`native_adapter.go` 998 linhas contra 864).
- `2d8867a` documentava o gate de `DEPENGINE_DETECT_SCRIPT` em
  `internal/engine`, pacote retirado por `dfce852`/#160; o texto sobrevive em
  `docs/compatibility.md` atualizado para `internal/platform`.
- `a543a9c` reescrevia trechos de `docs/support-boundary.md` hoje mais novos
  (auto-update de brew em #154, protocolo RPM em #150).
- `4dc1cc3`, `c61b551`, `fa49b3e`, `5bd32d3`, `8f3c050`, `9259f42` já estão
  100% contidos em `master`.

Único conteúdo real ainda ausente foi portado para `master` em `205f76d`:
(i) a descrição de `depengine update` e o hint de falha dizendo que v2 exige
identidade imutável por ferramenta, com README, `docs/compatibility.md`,
`docs/cli-reference.md`, página man e roadmap alinhados; (ii) testes para
`formatWhyAttemptIntent` (formatação de pacote nativo efetivo, sem cobertura
antes) e para a estabilidade de lock de plano nativo. Rebase direto da branch
teria **regredido** `master` nos pontos acima; o ramo pode ser descartado.

Estado após a rodada: `fix/front-a-artifacts` (trabalho real: `internal/deb`,
executáveis portáteis Windows, erros de assinatura), `fix/beads-front-b`
(superseded, aguardando remoção), `spike/cases-expansion` (experimento) e
`dev-notes`. Beads abertos: apenas `dep-bench-round2-update-why-graph-h8c` (P4).
