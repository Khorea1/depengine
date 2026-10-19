# Spike pyx: identidade de candidato sob especialização por host (+ medição dos forks do gate)

> **Estado: preliminar. Não é ADR e não altera nenhum bead.** Responde às perguntas da
> `dep-spike-catalog-host-specialization-pyx` por leitura de código e por um experimento
> executável, e acrescenta números que faltavam às decisões pendentes de `dep-gate-*`.
> Análise e experimentos rodaram numa **cópia descartável** do seu tarball; nenhum arquivo de
> produção foi alterado.

- Data: 2026-10-17 · Base: `master @ 4207205` · Relacionado: `report-scalability-planning.md`,
  `plan-github-distributions.md`, ADR-001, ADR-003, ADR-005, `docs/specs/github-distributions.md`
- Ambiente das medições: Go 1.27.2, Xeon 2,8 GHz, **1 vCPU**; variação por célula de até ~±25%.
- Rascunhos derivados: `plan-adr-007-host-independent-candidate-sequence.md` (proposto) e
  `plan-adr-008-catalog-layer.md` (esboço com decisões abertas).

## Resumo

1. **Especializar o catálogo *removendo candidatos* da lista por host quebra os três critérios
   da issue** (índices, pins v1, `MethodsHash`). Por construção e demonstrado em código real.
2. **Especializar *dentro* do candidato (o padrão da proposta E, já no HEAD) preserva os três.**
   `SpecializeDistribution` devolve uma cópia e nunca altera a lista.
3. **O bloqueio "esperar o item 8.1" está obsoleto** (`dep-1n0` fechada, E em #148). O que
   restou é mais forte: um invariante que já decorre de ADR-001/005 e da spec E.
4. **Consequência:** o catálogo devolve a *receita completa e host-independente* do nome. O
   custo se controla por fatoração (distributions, orçamento de candidatos), não por fatiar a
   lista. A pergunta do spike muda de "especializar por host?" para "lookup + pin do catálogo".
5. **Gate:** só o fork **A** (agressivo) cruza o corte de 50% do custo marginal; o fork **B**
   (conservador) não. Detalhes e ressalvas abaixo. A decisão continua sendo sua.

## 1. Critérios no-go e respostas

Critérios usados são os "mínimos propostos" da própria issue, sem alteração:

| Pergunta | Lista de candidatos filtrada por host | Especialização dentro do candidato (E) |
|---|---|---|
| Índices de candidato estáveis? | **Não** | Sim |
| Pins v1 (`<tool>/<kind>/<idx>`) estáveis? | **Não** | Sim |
| `MethodsHash` estável entre hosts? | **Não** | Sim |

O terceiro critério da issue ("validação que só passa no host que gerou o catálogo") **não foi
exercitado** aqui. A spec E já o cobre para `distributions` (item 5: checksum/sidecar validados em
todas as distribuições, inclusive as que não casam com o host); não testei isso.

### Evidência (código lido)

- `computeMethodsHash(tool.Methods)` (`internal/lock/lock.go:270`): SHA-256 da sequência ordenada
  de `Kind`+`Label` de **todos** os métodos da ferramenta.
- Chave v1 = `toolKey(nome, kind, idx)` com `idx` = contagem corrente por `Kind` na lista
  (`lock.go:236`, `:332`). Filtrar a lista renumera.
- `FindMethodCandidate(tool, kind, label)` (`config/candidate_lookup.go`) reencontra o candidato
  rastreado por `kind`+`label` sobre a lista da ferramenta (ADR-005, fluxo de `upgrade`).
- ADR-005: o intent de cada candidato é "validated, host-independent"; ADR-001: `MethodsHash`
  "identifies candidate kind/label intent".

### Evidência (experimento)

Ferramenta `br` com 4 candidatos `github` (mac, linux-glibc, linux-musl, windows) vs a mesma
ferramenta na forma E (1 candidato, 4 `distributions`). Hashes truncados.

| Host | `MethodsHash`, lista filtrada | = lista completa? | Chave v1 resultante | `MethodsHash`, forma E | Asset efetivo (forma E) |
|---|---|---|---|---|---|
| linux-glibc | `61c82b7d0b` | não | `br/github/0 [github-linux]` | `510e044ce3` (= completo) | `br-{version}-linux.tar.gz` |
| linux-musl | `d38ca05a6c` | não | `br/github/0 [github-musl]` | `510e044ce3` (= completo) | `br-{version}-linux-musl.tar.gz` |
| darwin | `34da5f3b52` | não | `br/github/0 [github-mac]` | `510e044ce3` (= completo) | `br-{version}-darwin.tar.gz` |

(Lista completa enumerada: `c44045d83e`, 4 candidatos.) Leitura:

- O hash diverge por host: um lock gerado num host acusaria drift em outro, ou no mesmo host se
  os fatos mudarem (container/WSL, versão da distro).
- **A chave `br/github/0` passa a nomear candidatos diferentes em hosts diferentes.** O pior caso
  é o que o ADR-001 já adverte: se os rótulos não distinguirem, o hash coincide e o pin de um
  artefato é aplicado a outro.
- `FindMethodCandidate("github","github-musl")` falha na lista filtrada de linux-glibc e darwin e
  acha na completa. Um candidato rastreado "some" quando os fatos do host mudam.

Limites: um schema pequeno e três hosts sintéticos; não exercitei lock v2 (`LockDocument`) nem
`status`/`remove`, e a leitura do ADR-005 para o v2 é por texto, não por teste.

## 2. O bloqueio do item 8.1

`dep-1n0` está fechada desde 2026-10-12 e o contrato E foi implementado no #148 (já no HEAD). O
conflito de identidade que a issue cita como pré-requisito foi resolvido *fixando* o invariante:
especializar dentro do candidato, nunca trocar método. Sugestão: reescrever o critério de
aceitação de `pyx` removendo a dependência e adotando as perguntas da seção 1.

## 3. Implicações de desenho

- **Contrato do catálogo:** `Lookup(nome) → receita completa`, sem fatos do host. Camadas
  (catálogo < manifest < schema) mesclam antes de `SnapshotIntentMetadata`.
- **Mudar a receita muda o hash.** Se o upstream do catálogo acrescenta um candidato, o
  `MethodsHash` de quem o usa muda. Logo o catálogo precisa de **pin** no lock (revisão/digest),
  reaproveitando o padrão de revisão de tap/bucket. Isso é o centro do ADR-008.
- **Custo controlado por fatoração:** se cada ferramenta tem poucos candidatos (E +
  aliases por gerenciador), o custo por candidato do executor é irrelevante (caso fatorado:
  0,1–1,2% de CPU). A forma enumerada deve ser tratada como anti-padrão **do catálogo** (limite
  de candidatos por ferramenta + aviso no import).
- **Lacuna de cobertura:** `distributions` só existe em `github`. Ferramentas `http` por
  OS/arquitetura continuariam enumeradas. Estender `distributions` é decisão separada e não
  aprovada; vale reavaliar quando houver corpus real.

## 4. Gate: decomposição e forks (insumo para `dep-gate-lean-operations-bit-7j4`)

**Decomposição** (perfil de `PlanningCandidates/cands=64`, 15 iterações; atribuição
proporcional pelos callees): `hasDangerousMethod` ≈ 65% do CPU de planejamento.
Dentro dele: `BuildCandidateIntent` (inclui `p.Validate` e `net/url.Parse`) ≈ 39%,
`CheckRequirements` → `PlanCapabilities` ≈ 26%, `PlanCapabilities` do próprio gate ≈ 32%,
resto ≈ 3%. O benchmark `ZZOff` isola o resto do motor.

**Três braços** (protótipo descartável, `DEP_GATE_MODE`; 100 ferramentas; mediana de 6 rodadas
de 4 iterações; ns/candidato):

| Candidatos/ferramenta | Gate desligado | Atual | Fork B | Fork A |
|---|---|---|---|---|
| 16 | ~6,5 k | 27,1 k | 22,1 k | 16,7 k |
| 64 | ~9,5 k | 33,2 k | 26,4 k | 18,4 k |
| **Economia sobre o custo marginal do gate** | n/a | n/a | **24% / 29%** | **50% / 62%** |
| Economia sobre o planejamento total | n/a | n/a | 19% / 20% | 38% / 45% |

- **Fork A** = `BuildCandidateIntent` + bit de operações, preservando o ramo de kinds não
  registrados (o canto real que o conselho #2 achou). **Fork B** = mantém `CheckRequirements`,
  troca só o segundo `PlanCapabilities` pelo bit.
- Com o limiar do J2 ("≥ 50% do custo marginal"), **só A passa**; em 16 candidatos A fica
  *no limite*, dentro do ruído. B economiza cerca de um quarto e não passa.
- **Piso:** depois de A restam ~9–10 µs/candidato acima do gate desligado (a validação do
  próprio intent). Só (b) elimina isso, e (b) depende da guarda de inelegibilidade conclusiva.
- O protótipo passou os testes existentes do gate nos três modos, mas essa seleção por nome
  (`Dangerous|ArbitraryCode|PlanIntent|Conformance|Hook`) é **evidência fraca**. A matriz de
  caracterização que o conselho pede não foi escrita.

## 5. Pendências do conselho #2: minha recomendação (você decide)

| Pendência | Recomendação | Base |
|---|---|---|
| Fork A vs B | **A**, com ramo de kinds não registrados preservado e matriz permanente; B só se o corte cair | seção 4 |
| Limiar de materialidade | adotar o do J2: ≥ 50% do custo marginal (`-count=10` + benchstat) e sem regressão > 5% na forma fatorada | A passa no limite; pré-registrar evita ajustar depois |
| Gatilho (ii) de (b) | reescrever como "não mensurável hoje; revive por (i)+(iii)", com métrica restrita a superfícies method-level | hooks de ferramenta tripam o gate sem `when` (ADR-003) |
| Bools `IsWSL`/`IsContainer`/`IsAndroid` | **não mudar `Facts` agora.** Se (b) voltar, a guarda deve ser um helper tri-estado (casa / não casa / inconclusivo), com "inconclusivo" quando a detecção falhou | `Match` com `facts == nil` devolve `c.IsZero()`; bools não representam "desconhecido" |

Higiene dos beads (conferida no JSONL): `pyx` ainda traz o critério obsoleto do 8.1; a descrição
do `7j4` ainda não incorpora as emendas do conselho #2 (fork ambíguo, matriz estendida, protocolo
de régua); nenhum bead tem assignee. Corrigi nada disso, só registro.

## 6. Limites e o que não li

- Medições em 1 vCPU, poucas iterações; o ruído por célula chega a ±25%. Os percentuais de A
  e B precisam do protocolo de 3 braços com benchstat antes de virarem critério.
- A decomposição do perfil usa atribuição proporcional entre callers; pode errar alguns pontos.
- Não li `open-issues-fix-plan.md`, `plan-pr148-corrections.md`, `AGENTS.local.md`, nem as
  demais worktrees (excluí `.worktrees/` e `.git/wt` ao extrair). "Suas análises" que li foram
  os comentários e critérios dos beads (`iyn`, `7j4`, `pyx`, `h8c`, `smk`).
- Os conselhos se declaram de um único modelo; os números acima são medições minhas, mas o
  protótipo e o experimento não passaram por revisão independente.

## 7. Reprodução

- `spike_identity_test.go` (copiar para `internal/lock/`): `go test ./internal/lock -run TestSpikeCandidateIdentity -v`.
  Hoje só imprime a divergência; vira asserção na issue N3 (ver `beads-changes.md`).
- A régua `planning_bench_test.go` agora vai no commit `test(exec): planning cost benchmarks as ruler...`
  (patch `01-master-...`), com o braço `PlanningCandidatesGateOff`.
- `gate-fork-prototype.diff` (aplicar em cópia; **não** é patch de produção; os forks entram pela
  `dep-gate-lean-operations-bit-7j4`):
  `DEP_GATE_MODE={,B,A} go test ./internal/exec -run '^$' -bench PlanningCandidates -benchtime=4x -count=6`.
- Sem acesso a `proxy.golang.org`; dependências via `GOPROXY=direct GOSUMDB=off`.

## 8. Desfecho (2026-10-17, decisões do mantenedor)

| Questão | Decisão |
|---|---|
| Fork do gate | **A** (com ramo de kinds não registrados preservado e matriz de caracterização permanente). **B não será feito.** Limiar ≥ 50% do custo marginal, medido com `-count=10` + benchstat; se A ficar abaixo, fecha com resultado negativo |
| Linguagem própria | **Cancelada** (não adiada). Reabre só com manifesto real e documentado que o TOML não expresse |
| Precedência | usuário > (manifest + fontes derivadas); lista ordenada de fontes na camada de baixo |
| ADR-007 | **Aceito com emendas**: removida a decisão 5 (skip por `when` conclusivo, vai para o item (b)); Context corrigido (o invariante é sobre a sequência de candidatos, não sobre o conteúdo do intent); verificação vira teste de invariância do loader. Emenda de uma frase no ADR-005 |
