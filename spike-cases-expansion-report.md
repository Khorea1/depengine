# Spike A vs C: relatório (dep-1n0.1)

- Data: 2026-10-11 · Branch: `spike/cases-expansion` @ `a4124e8` · Base: `master 11bf4d4`
- Issue: `dep-1n0.1` (filha de `dep-1n0`) · Ref: `plan-manifest-deduplication-investigation.md`
- Escopo cumprido: só `internal/config/spike/` (novo, isolado); nenhum arquivo de produção alterado.

## O que foi construído

- Baseline A: `a_br.toml` (4 github), `a_omp.toml` (3), `a_wt.toml` (2) — reconstruções fiéis à
  auditoria `archive/auditoria-manifestos-2026-10-11.md §2`, não cópias de manifests pessoais.
- Protótipo C: `c_*.toml` (base + `cases`) + `expand.go` (260 linhas, macro TOML→TOML, zero
  dependências internas: `go list -deps` mostra só o próprio pacote; imports de
  `config/lock/validate` existem só no harness de teste, não no protótipo).
- Regras do protótipo: `label`+`when` obrigatórios, allowlist
  (`asset, entrypoints, binary, strip_components, extract_to`), base sem `when/label/kind`,
  sem herança recursiva, sem prioridade entre cases, expansão total em todo host.
- Goldens: `testdata/goldens/{br,omp,wt}.json` (candidatos canônicos + `MethodsHash`).

## Equivalência (todos os testes passam; `go test -race`, `go vet`, `golangci-lint` limpos)

| Verificação | Resultado |
|---|---|
| 9 candidatos campo a campo (Kind, Label, When, Config, Inferred, ArchMap/OSMap, Requires, Sources) + ordem do parser | idênticos A×C |
| Merge schema+manifesto (`mergeMethodSlices` inalterado) | idêntico; override de `checksum` aplicado; candidato novo anexado |
| `MethodsHash` e `SourceHash` (`lock.SnapshotIntentMetadata`) | iguais |
| `validate.ValidateSchema` erros+warnings | iguais |
| Matriz 7 hosts (linux glibc/musl, mac x86_64/arm64, windows x86_64, android-termux, freebsd) | elegíveis idênticos; android/freebsd = zero candidatos |
| Fail-closed (8 casos: dup label, sem label/when, override proibido repo/scope/checksum, campo desconhecido, base com when) | todos rejeitados |
| Determinismo (3 re-expansões) | byte-idêntico |
| Overlap os/arch/libc/is_android nas fixtures | zero pares sobrepostos (disjunção real, não assumida) |

## Limites verificados (não cobertos offline)

- Projeções v2 e `ResolveLegacyV1` exigem rede (GitHub API); comparar `MethodsHash`/`SourceHash`
  + planos efetivos num e2e com rede antes de qualquer aprovação de implementação.
- `entrypoints` e demais chaves (exceto `pkg_overrides`) são `MergeOverwrite` em
  `internal/config/manifest.go` (`MethodConfigFieldStrategy`): cases sempre emitem mapas
  completos; override parcial de mapa não existe — comportamento preservado, não criado.
- Manifesto standalone exige candidato completo (`repo+asset`); override parcial só existe
  pós-merge. O spike não muda isso.
- `br`/Windows em ZIP + `br.exe` não toca `dep-laz` (bloqueio é sobre o artefato direto,
  `internal/artifact/contract.go:158`): sem dependência entre os dois trabalhos.

## Custo (passo 7)

- Economizado: 107 → 64 linhas TOML (~40%) em 3 tools.
- Adicionado: `expand.go` 260 + testes 440 + fixtures/goldens; mais gramática, validação,
  docs e política de sobreposição permanentes se implementado.
- Fora de `internal/config`: nada. Proveniência/merge especial: nenhuma necessária
  (merge atual reutilizado sem alteração — confirma a decisão de não adotar `cases` atômico).

## Decisão (passo 8): aprovar C para planejamento, não para implementação

Critérios de aprovação técnica atendidos: equivalência demonstrada, expansão localizada,
nenhuma mudança no pipeline. C **é** uma macro simples — a incerteza arquitetural está eliminada.
Mas economia de dezenas de linhas não justifica sozinha regra permanente: abrir tarefa de
implementação separada somente com e2e (rede), goldens v2 e contrato fechado
(allowlist final, `when` restrito a os/arch/libc/is_android ou sem garantia de exclusividade).
Sem isso, permanece A.

## Entregas (passo 9)

Protótipo isolado + testes + goldens em `spike/cases-expansion` (não promovido a `master`).
Próximo: tarefa de implementação só após aprovação explícita do relatório.
