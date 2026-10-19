# Escalabilidade de schema, catálogo e planejamento: relatório preliminar

> **Estado: preliminar. Não é ADR, não aprova sintaxe nem refatoração.** Registra medições,
> leituras de código e um veredito de trabalho para decidir o que implementar primeiro.
> Decisões marcadas como "mantenedor (conversa)" foram ditas em sessão, não formalizadas.

- Data: 2026-10-16 · Base: `master @ 4207205` (snapshot lido e medido; reconfirmar no HEAD)
- Ambiente: Go 1.27.2 linux/amd64, Xeon 2,8 GHz, **1 vCPU**, 4 GB. Medições únicas ou poucas
  iterações; a variação entre rodadas chegou a ~25%. Tratar como ordem de grandeza.
- Relacionado: `plan-github-distributions.md` (fronteira E aprovada),
  `plan-manifest-deduplication-investigation.md` (invariantes e critérios *no-go*).
- Artefato de código: `internal/exec/planning_bench_test.go` (novo; entregue como commit
  `test(exec): planning cost benchmarks as ruler...`, pendente de merge em `master`). Nenhum arquivo de produção foi alterado.
- Beads: nenhuma issue criada ou alterada.

## Resumo

1. **Caso normal (fatorado, 1 a 2 candidatos por ferramenta): o I/O domina.** O motor gasta
   ~40 µs de CPU por ferramenta; o planejamento dispara ~3 subprocessos por ferramenta
   (~1,1 ms cada no melhor caso, ~12 ms para `dpkg-query` aqui). CPU ≈ 0,1% a 1,2% do tempo.
2. **Caso enumerado (matriz plana): a CPU do motor escala com *todos* os candidatos, não só os
   elegíveis.** ~40 µs por candidato no executor contra 50 a 150 ns do `Match`. Uma ferramenta
   com 480 candidatos custa ~21 ms de CPU só para planejar. Parse do schema plano de 96 mil
   candidatos: 4,3 s e ~150 MB; o planejamento do mesmo schema custaria ~4 s adicionais
   (extrapolação linear, não medida).
3. **Causa do item 2 identificada e confirmada por experimento:** o portão de código arbitrário
   (`hasDangerousMethod`) monta e valida o plano completo de cada candidato, inclusive os que
   o `when` excluiria. Desligá-lo reduz o custo de ~37–46 µs para ~11–14 µs por candidato.
4. **O formato do arquivo (TOML vs binário) importa pouco** diante do índice: um formato
   compilado decodifica ~7× mais rápido, mas a economia é de dezenas de µs por ferramenta.
5. **O manifest hoje paga o catálogo inteiro e descarta quase tudo** (parse integral, depois
   filtro por nome). Para virar catálogo precisa de lookup por nome.

## Medições

### 1. Parse e validação do schema (`internal/config`)

Matriz sintética: 5 arquiteturas × 2 libc × 8 famílias de distro × 3 ambientes × 2 (android) =
480 candidatos `http` por ferramenta, um candidato por célula.

| Candidatos (forma plana) | TOML | Parse | Heap | `Match` de todos |
|---|---|---|---|---|
| 480 (1 ferramenta) | 0,1 MB | 8,7 ms | 0,8 MB | 23 µs |
| 4.800 | 1,4 MB | 99 ms | 7,5 MB | 366 µs |
| 24.000 | 6,9 MB | 0,57 s | 37,5 MB | 2,1 ms |
| 96.000 | 27,7 MB | 4,3 s | ~150 MB | n/m |

- ~1,5 KB de heap por candidato. De 24 mil para 96 mil candidatos (4×) o parse levou 7,5×:
  levemente superlinear, provavelmente pressão de GC.
- Perfil (aproximado, inclui toda a execução do teste): decode TOML ≈ 25%, `normalizeTools`
  ≈ 24%, alocação ≈ 31%. O custo é majoritariamente normalização de `map[string]any`, não
  a sintaxe TOML.

Mesma matriz expressa de forma fatorada (aliases por gerenciador + `github` com
`distributions` e `{arch_any}`; cada ferramenta vira 2 candidatos):

| Ferramentas | Candidatos | TOML | Parse | Heap | Especializar todos |
|---|---|---|---|---|---|
| 200 | 400 | 0,08 MB | 9,8 ms | 0,9 MB | 0,37 ms |
| 1.000 | 2.000 | 0,39 MB | 53 ms | 4,4 MB | 2,1 ms |
| 5.000 | 10.000 | 2,03 MB | 374 ms | 21,7 MB | 19,6 ms |

Para 200 ferramentas: plano 96.000 candidatos / 4,3 s / ~150 MB contra fatorado 400 candidatos /
10 ms / 0,9 MB.

### 2. Planejamento no executor (`internal/exec`, `--dry-run`, runner falso)

| Cenário | Resultado |
|---|---|
| `native` real, runner Debian simulado, 100 ferramentas | 27–35 µs/ferramenta, 3,06 chamadas/ferramenta |
| idem, 1.000 ferramentas (1 dependência compartilhada) | 39–41 µs/ferramenta, 3,0 chamadas/ferramenta; 9,9 MB e 60 mil alocações por plano |
| mock, 2 candidatos/ferramenta, 100 / 1.000 / 5.000 ferramentas | 97–124 / 117–130 / 126–151 µs/ferramenta (quase linear) |
| mock, 100 ferramentas, 1 / 16 / 64 / 480 candidatos (o elegível é o último) | 71 / 42 / 46 / 44 µs por candidato; 480 ⇒ 21 ms/ferramenta, ~500 MB e 6,2 mi de alocações por plano |

Ajuste grosseiro: CPU por ferramenta ≈ 30 µs + ~40 µs × candidatos.

Experimento do portão (100 ferramentas; 5 iterações):

| Candidatos | Portão ligado | Portão desligado (`WithAllowArbitraryCode`) |
|---|---|---|
| 16 | 37,5 µs/cand | 10,9 µs/cand |
| 480 | 46,4 µs/cand | 14,5 µs/cand |

Perfil com 480 candidatos: `hasDangerousMethod` 63% (via `candidatePlanIntent` →
`BuildValidatedCandidateIntent` → `plan.Validate`, com `validateCredentialFreeURL`/`net/url.Parse`
≈ 20–26%); tentativa e portão estático dos candidatos tentados ≈ 22–25%.

Leitura de código que explica: `rc.selectedMethods` → `config.SelectMethods` só ordena por
kind/label (sem `when`) e devolve todos os candidatos; `hasDangerousMethod` itera todos sem
chamar `When.Match`. O `when` aparece em `attempt.go` (por tentativa), `batch.go`, `explain.go`,
`lock_resolution.go` e `execute.go`, mas não no laço de `hasDangerousMethod`.

### 3. I/O: custo de um subprocesso real

| Comando | Custo (20 iterações, VM compartilhada) |
|---|---|
| `true` | 1,09 ms |
| `dpkg-query -W ... dpkg` | 11,9 ms |

Fração de CPU por ferramenta (aproximação: as 3 chamadas tratadas como o mesmo comando; na
prática 2 são `dpkg-query` e 1 é `apt-cache`, não medido):
`39 µs / (39 µs + 3 × 1,09 ms) ≈ 1,2%`; com 11,9 ms por chamada, ≈ 0,11%.

### 4. Formato compilado e armazenamento

- Uma ferramenta com 480 candidatos (TOML de 132 KB): TOML 7,0 ms; `gob` 1,0 ms (111 KB, ~7×);
  JSON 1,9 ms (311 KB, ~4×). O `gob` é régua grosseira, não proposta de formato.
- Com a forma fatorada, uma ferramenta custa ~50 µs em TOML; a economia de um formato
  compilado é de dezenas de µs por ferramenta.
- `zip` da stdlib com 100 mil entradas de ~400 B (sintético, cache quente): abrir 93 ms,
  primeira consulta +129 ms (≈ 220 ms de partida a frio), consultas seguintes ≈ 50 µs.
  Adequado até dezenas de milhares de receitas; acima disso, fragmentar por prefixo ou usar
  índice próprio com offsets.

## Achados de leitura de código (não medidos)

- **Manifest:** em `ResolveSchemaFromFiles`, `ParseManifest` lê o arquivo inteiro e só depois
  `FilterManifestTools` descarta o que o schema não nomeia. A semântica já é "catálogo pull-by-name"
  (o projeto nomeia, seus campos vencem); o mecanismo é eager. Não verifiquei se há outro caminho
  de carga fora dessa função.
- **`kernel` é igualdade exata** contra `uname -r` (ex.: `6.8.0-45-generic`). O exemplo da
  referência de schema (`kernel = ["6.7","6.8","6.9"]`, ≈ L1085) só casa se o host reportar
  exatamente `6.7`. Reconfirmar no HEAD; é no mínimo erro de documentação.
- **`libc` é por prefixo** (`glibc`, `musl`); não há comparação de versão. `distro_version` já
  tem min/max.
- **Versão do pacote não é dimensão do `when`**; é campo do adapter.
- **`distributions`:** fronteira E aprovada só para `github` (`plan-github-distributions.md`), com
  metadados de campos especializáveis por método em `internal/methodkind`. Estender a outros
  métodos seria decisão separada.
- Sem camada de registro, include de arquivos ou carga sob demanda (alinhado com `docs/alternatives.md`:
  o projeto não constrói seu próprio universo de pacotes).

## Veredito de trabalho (ordem proposta)

1. **Feito:** benchmark de planejamento ponta a ponta (este relatório).
2. **Decidir o portão de código arbitrário sobre candidatos inelegíveis (decisão de segurança).**
   Hoje falha fechado: uma ferramenta com candidato perigoso que o `when` exclui é bloqueada.
   Opções: (a) manter e aceitar o custo; (b) pular candidatos que o `when` exclui dentro do
   laço de `hasDangerousMethod`, o que não altera a lista de candidatos e portanto não toca a
   identidade do lock; (c) memoizar por candidato. (b) corta ~70% do custo em schemas
   enumerados, mas torna o portão menos conservador.
3. **Correções semânticas pequenas** (propostas novas, não aprovadas): comparação de versão em
   `kernel` e `libc`; corrigir o exemplo de `kernel` na referência.
4. **Catálogo por lookup de nome.** O manifest *aponta* para catálogos em vez de conter o
   catálogo; o filtro por nome vira consulta. Começar com um TOML por pacote (o sistema de
   arquivos como índice) atrás de um contrato `Lookup(nome) → receita`; `zip`/shards depois,
   sem mudar o contrato. Pin da revisão do catálogo no lock reaproveitando o padrão de
   revisão de tap/bucket.
5. **Import declarativo de manifests** (mantenedor, conversa: v1 só declarativo, sem hooks;
   dono do catálogo = usuário). O bead é o modelo tipado serializado, pinado por digest.
   Precedência: usuário > manifesto do desenvolvedor > fontes derivadas.

**Adiar de propósito:** linguagem própria (até existir manifesto real que o TOML não expresse),
formato binário otimizado, índice por dimensão, importador de Repology (não exclusivo; mantenedor,
conversa).

## Riscos de recriar o problema

- Catálogo carregado inteiro na partida é o mesmo O(catálogo) escondido. Teste de regressão:
  custo de um lookup não pode crescer entre catálogos de 1 mil e 100 mil entradas.
- Manifestos importados que enumeram células recriam a matriz plana de forma distribuída.
  Limitar candidatos por ferramenta, avisar na importação e oferecer templates.
- Camadas sem origem visível tornam o resultado imprevisível; estender `why --fields`.
- Hooks entrando como "dado": o validador de import deve rejeitar campos executáveis.
- **Especializar o catálogo por host reduz a lista de candidatos e esbarra nos critérios *no-go*
  de `plan-manifest-deduplication-investigation.md`:** lock v1 usa chaves
  `<tool>/<method>/<index>`, `MethodsHash` considera a sequência de `Kind`/`Label`, e a validação
  não pode depender só do host atual. Antes de qualquer pré-filtro estrutural é preciso provar que
  a identidade de candidato e os pins permanecem estáveis, e validar todos os hosts declarados
  (por exemplo, uma lista finita de hosts representativos em CI, custo pacotes × hosts).

## Limites

- Runner falso com latência zero: o I/O é modelado (chamadas × spawn medido), não medido de
  ponta a ponta. Resolução de releases e demais chamadas de rede não entram.
- `apt-cache` não foi medido isoladamente. O custo de `dpkg-query` aqui (11,9 ms) pode estar
  inflado por VM/contêiner.
- Dados sintéticos, uma máquina de 1 vCPU, poucas iterações. Não medidos: grafo com muitas
  arestas em 5.000 ferramentas, `update`/`lock`, `explain`/`why` com milhares de ferramentas,
  fases de estado e WAL com muitas ferramentas, e o caminho `install` real (fora de `--dry-run`).
- `maxJobs` começa em 1 em `New()`; não verifiquei se a CLI altera isso nem se a observação
  paraleliza.
- Não li `cmd/` para confirmar como a CLI monta a carga do manifest.

## Reprodução

```sh
# No checkout de master, com o arquivo planning_bench_test.go em internal/exec/
go test ./internal/exec -run '^$' -bench 'Planning|SubprocessSpawn' -benchmem
```

Os experimentos de parse/formato/`zip` eram testes e programas descartáveis (não versionados);
os parâmetros estão descritos nas tabelas acima. Este ambiente não alcança `proxy.golang.org`;
as dependências foram obtidas com `GOPROXY=direct GOSUMDB=off`.
