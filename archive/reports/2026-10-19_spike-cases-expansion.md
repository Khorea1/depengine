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

---

## Rodada 2 — correções da auditoria (2026-10-11, commit `dfa021c`)

Auditoria independente encontrou 6 lacunas (ver seção 1–5 do relatório de auditoria);
todas corrigidas sem tocar produção:

- **Achado A ([packages] + simple)**: `ExpandCompact` agora processa `[tools]` e `[packages]`,
  preserva listas simples (`simple = [...]` gera uma tool por entrada — verificado) e
  candidatos não-github (aur/native/nix/cargo/conda) intactos. Fixtures reais vivem em
  `[packages]`, como no manifesto pessoal; `br` em `[tools]` mantém cobertura da outra seção.
- **Achado B (colisões)**: label de case colidindo com qualquer chave declarada da tool
  (candidato explícito ou chave de nível de tool) é erro; duplicata entre cases tem erro
  próprio (`duplicate` vs `collides`); base com `cases` fora de github é rejeitada
  (`github-only`). Teste inclui o exemplo exato da auditoria (`github-linux` explícito).
- **Achado C (fixtures fiéis)**: `omp` com binários brutos, `checksum_url` + `sha256sum`,
  fallbacks aur/native/nix e `method_only`; `wt` com `.tar.xz`, `strip_components = 1`,
  dois entrypoints, `method_only` + `post_install` + fallbacks. `method_prefer = ["aur"]`
  inválido foi removido (a fixture agora declara o candidato `aur`).
- **Merge (br 4+1=5)**: caminho real schema vazio + manifesto + camada de override, com
  overrides nas duas tools (`br` checksum, `omp` scope); `omp` incluído nos hashes.
- **Validação estrita**: fundidos `merged-A`/`merged-C` exigem **zero erros** (não apenas
  contagens iguais); warnings comparados. Capturou de verdade um erro de fixture
  (`nix` fora da ordem sem `method_prefer` — igual ao `depends.toml` real).
- **Contratos persistidos**: `MethodsHash` + `SourceHash` para br/omp/wt; identidade via
  `FindMethodCandidate` (o que `upgrade`/`remove`/`why` usam); goldens obrigatórios —
  ausência falha, criação só com `SPIKE_WRITE_GOLDENS=1`.
- **Limite restante (assumido)**: projeções v2 com resolução remota exigem rede/e2e;
  os insumos da projeção são integralmente comparados. Contratos `dep-1n0` sobre
  exclusividade mútua ficam como decisão explícita (fallback preservado, sem garantia nova).

Veredito revisto: equivalência demonstrada nas receitas reais, no caminho real
`[packages]`→merge, com validação fail-closed. Confiança em C sobe de "hipótese viável"
para "macro correta nas condições testadas"; implementação segue dependendo de e2e com
rede + decisão de contrato sobre exclusividade (atualizar critérios de `dep-1n0` para o
contrato "vários candidatos convencionais", não "método único com variantes exclusivas").

---

## Rodada 3 — lacunas da segunda auditoria (2026-10-11, commit `febcf18`)

Quatro lacunas, nenhuma exigindo redesign — todas fechadas:

- **P1 (pós-merge)**: `TestMergeEquivalence` agora compara ordem (`assertSameOrder`) e
  `MethodsHash`/`SourceHash` via `SnapshotIntentMetadata(ma)` vs `(mc)` nos schemas
  fundidos, fechando a brecha de compatibilidade do lock v1.
- **P2 (metadados)**: `canonicalCandidate` inclui `preInstall`, `postInstall`, `sources`
  (conteúdo) e as três secret refs — cobre `wt.post_install` e `omp` brew-tap/nix.
- **P2 (diagnósticos)**: validação compara o `Result` completo (erros + warnings) dos
  fundidos, não contagens.
- **P3 (simple pelo expansor)**: `c_simple.toml` (`simple` + `cases` no mesmo documento);
  o teste prova `zsh`/`bat` sobrevivendo ao `ExpandCompact` junto à expansão de `wt`.

Decisão de escopo registrada: overlap de `when` permanece informativo (fallback normal),
coerente com macro transparente; a interpretação "um único asset elegível" de `dep-1n0`
exigiria nova semântica de seleção e fica explicitamente fora — atualizar os critérios
de aceitação de `dep-1n0` para o contrato de vários candidatos antes de implementar.

---

## Rodada 4 — terceira auditoria (2026-10-11, commit `7bc62bf`)

Dois pontos, sem redesign:

- **A (hooks de tool)**: `canonicalTool` compara `Pre/PostInstall` com comandos e
  condições, `Requires`/`RequiresWhen`, seletores e tags — cobre `wt.post_install`
  integralmente, não por contagem.
- **B (entre camadas)**: `TestCrossLayerBaseOverride` prova que redeclarar
  `[packages.br.github]` na camada superior anexa um 5º candidato em vez de atualizar
  os 4 expandidos; semântica documentada em `spike/doc.go`. Herança base→cases entre
  camadas não existe por desenho; overrides usam identidade `Kind+Label` convencional.

Pendente (decisão de produto, fora do spike): revisar critérios de `dep-1n0` para o
contrato de vários candidatos e avaliar o custo permanente de `cases`.
