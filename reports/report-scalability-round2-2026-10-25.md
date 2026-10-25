# Segunda rodada de escalabilidade: update/lock, why e grafo denso

> **Estado: preliminar. Não é ADR, não aprova refatoração nem altera comportamento.**
> Registra medições para a issue P4 `dep-bench-round2-update-why-graph-h8c` e fecha a lacuna
> deixada pela primeira rodada. Nenhum código de produção foi alterado nesta rodada.

- Data: 2026-10-25 · Base: `master @ 7bd93b5` (benchmarks) sobre `04fad1e` (produto)
- Ambiente: Go 1.27.2 linux/amd64, AMD Ryzen 9 7900 (12 núcleos/24 threads), 1 máquina.
- Issue: `dep-bench-round2-update-why-graph-h8c` (P4, opcional).
- Artefato de código: benchmarks test-only em `internal/app/bench_update_why_graph_test.go`,
  `internal/exec/explain_bench_test.go`, `internal/lock/bench_test.go` e
  `internal/graph/dense_bench_test.go` (commit `7bd93b5`). Nenhum arquivo de produção foi
  alterado.
- Relacionado: `report-scalability-planning-2026-10-16.md` (primeira rodada),
  `plan-manifest-deduplication-investigation.md` e ADR-008 (catálogo).

## Resumo

1. **`update` com 5.000 ferramentas custa ~497 ms a frio e ~645 ms a quente** (~99 µs e
   ~129 µs por ferramenta). As três etapas de projeção v2 — `BuildLockDocument`,
   `NewUniversal` e `Save`+`Load` — somam ~302 ms, ou **61% do update a frio**. O restante
   (~195 ms) é parse/validação do schema, resolução de candidatos e leitura de estado.
2. **`why` com 5.000 ferramentas custa ~132 ms por invocação**, mas explicar uma ferramenta
   é barato: `ExplainTool` custa ~15 µs por ferramenta e explicar as 5.000 ferramentas custa
   ~75 ms. O custo do comando é dominado por carregar e validar o schema inteiro, não pela
   explicação do candidato selecionado.
3. **Grafo denso de 5.000 nós com ~25 mil arestas**: `BuildDeclaredGraph` ~8 ms,
   `SortGraph` ~191 ms, `Analyze` ~197 ms, DOT ~5 ms, texto ~3 ms. O comando `graph`
   completo custa ~559 ms. Com fan-in 20 (~100 mil arestas), build cresce ~4×, mas
   sort/analyze crescem apenas ~16–24%: neste formato, a profundidade/número de nós domina
   sobre o número de arestas.
4. **O comando `graph` escala de forma superlinear de 1.000 para 5.000 ferramentas**
   (49,7 ms → 558,8 ms, ~11,2× para 5× de ferramentas). O núcleo do grafo explica ~204 ms
   em 5.000 nós; o restante (~355 ms) está em carga/validação/projeção/output.
5. **Hot spots candidatos, não implementados**: validação/reprojeção repetida no caminho v2
   do lock (`BuildLockDocument` + `NewUniversal` + `Save/Load`); `SortGraph` e `Analyze`;
   e carga integral do schema em `why`/`graph` quando só uma ferramenta ou um grafo é
   necessário. Nenhuma otimização foi aprovada por este relatório.

## Medições

### 1. `depengine update` de schema inteiro

Schema sintético de N ferramentas `http` com URL e checksum literais (sem rede), sem
dependências. "Frio" remove o lock antes de cada iteração (migração/gravação v2); "quente"
reutiliza o lock v2 existente. Mediana de 9 amostras (3 execuções × 3 iterações).

| N | frio ns/op | frio µs/tool | frio B/op | frio allocs | quente ns/op | quente µs/tool | quente B/op | quente allocs |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 100 | 12.436.713 | 124,4 | 6.039.170 | 68.606 | 12.825.457 | 128,3 | 7.943.045 | 80.887 |
| 1.000 | 85.458.167 | 85,5 | 60.322.600 | 680.164 | 106.797.230 | 106,8 | 77.641.856 | 801.408 |
| 5.000 | 496.803.774 | 99,4 | 307.510.061 | 3.397.037 | 644.880.327 | 129,0 | 413.945.120 | 4.002.530 |

O caminho quente é ~30% mais caro em 5.000 ferramentas: ele lê e valida o lock v2 anterior,
decodifica a projeção e ainda executa a reconstrução. O caminho frio inclui a mesma
reconstrução e a gravação inicial.

### 2. Núcleo de lock

Mesmo schema sintético; runner falso e sem rede. Valores medianos de 9 amostras.

| Benchmark | N=100 | N=1.000 | N=5.000 | Custo em 5.000 |
|---|---:|---:|---:|---:|
| `SnapshotIntentMetadata` | 18.411 ns (184 ns/tool) | 221.500 ns (222 ns/tool) | 1.473.872 ns (295 ns/tool) | 1,5 ms |
| `ResolveLegacyV1` | 50.502 ns (505 ns/tool) | 593.466 ns (594 ns/tool) | 3.005.477 ns (601 ns/tool) | 3,0 ms |
| `BuildLockDocument` | 1.388.736 ns (13,9 µs/tool) | 14.413.280 ns (14,4 µs/tool) | 67.702.747 ns (13,5 µs/tool) | 67,7 ms |
| `NewUniversal` | 2.468.620 ns (24,7 µs/tool) | 25.775.248 ns (25,8 µs/tool) | 127.081.513 ns (25,4 µs/tool) | 127,1 ms |
| `Save` + `Load` v2 | 1.267.841 ns (12,7 µs/tool) | 14.082.828 ns (14,1 µs/tool) | 107.454.042 ns (21,5 µs/tool) | 107,5 ms |

Comparação com o update a frio de 5.000 ferramentas (496,8 ms):

| Etapa | Tempo | Fração do update frio |
|---|---:|---:|
| `BuildLockDocument` | 67,7 ms | 13,6% |
| `NewUniversal` | 127,1 ms | 25,6% |
| `Save` + `Load` | 107,5 ms | 21,6% |
| **Subtotal de projeção v2** | **302,3 ms** | **60,8%** |
| Parse/validação/resolução/estado | ~194,6 ms | ~39,2% |

`ResolveLegacyV1` (caminho v1) é barato neste schema: ~3 ms para 5.000 ferramentas. O custo
está na projeção universal, que valida e revalida a mesma identidade em etapas separadas.

### 3. `why` e `ExplainTool`

| N | `runWhy` ns/op | `runWhy` por invocação | `ExplainTool` em todas as tools | `ExplainTool` ns/tool |
|---:|---:|---:|---:|---:|
| 100 | 4.336.450 | 4,34 ms | 1.823.730 ns | 18.237 |
| 1.000 | 20.275.360 | 20,28 ms | 15.760.728 ns | 15.761 |
| 5.000 | 132.442.183 | 132,44 ms | 75.093.124 ns | 15.019 |

O comando `why` explica uma ferramenta, mas paga o carregamento e a validação do schema
inteiro. Explicar todas as 5.000 ferramentas no núcleo custa ~75 ms; uma única invocação de
`why` custa ~132 ms. A diferença (~57 ms) é carga/validação/output do comando, não a
explicação em si.

### 4. Grafo denso de 5.000 nós

Grafo acíclico em cadeia: cada nó depende dos próximos `fanIn` nós. `fanIn=5` gera ~24.985
arestas; `fanIn=20` gera ~99.810 arestas.

| Operação | fan-in 5 | fan-in 20 | Variação com ~4× arestas |
|---|---:|---:|---:|
| `BuildDeclaredGraph` | 8,23 ms | 32,78 ms | +298% |
| `SortGraph` | 190,52 ms | 220,58 ms | +16% |
| `Analyze` | 196,87 ms | 243,79 ms | +24% |
| `RenderDOTGraph` | 5,38 ms | 18,12 ms | +237% |
| `RenderTextGraph` | 2,69 ms | 4,42 ms | +64% |

O comando `graph` completo (declarado, sem filtros) em 5.000 ferramentas:

| Formato | N=1.000 | N=5.000 | µs/tool em 5.000 |
|---|---:|---:|---:|
| `dot` | 49,7 ms | 558,8 ms | 111,8 |
| `text` | 49,5 ms | 556,0 ms | 111,2 |

Para `dot` em 5.000 nós, o núcleo do grafo (build + sort + render) soma ~204 ms; o comando
custa ~559 ms. Os ~355 ms restantes estão em parse/validação do schema, projeção da view e
output. O crescimento 1.000 → 5.000 é ~11,2× para 5× de ferramentas, indicando um componente
superlinear fora do núcleo medido isoladamente.

## Leitura e limites

- **O que está medido**: caminhos reais de `update`, `why` e `graph` com schema gerado,
  detecção de host determinística e saída descartada. Nenhuma rede, package manager ou
  mutação de host é exercitada.
- **O que não está medido**: `update --profile`, fallback v1 para schemas native-only,
  estado/WAL com muitas ferramentas, `install` real, resolução de releases/registries,
  `graph --format graph` (renderer de terminal), `why --fields` e concorrência.
- **Forma do grafo**: a cadeia com fan-in testa muitas arestas, mas mantém 5.000 níveis. Um
  grafo largo (muitos nós no mesmo nível) pode ter custo diferente; não foi medido.
- **Ruído**: uma máquina, mediana de 9 amostras, sem intervalo de confiança. A primeira
  rodada observou ~25% de variação; aqui os valores de sort/analyze foram estáveis (~1–2%),
  mas os de update/why tiveram mais variação.
- **Nenhuma alteração de produção**: os arquivos adicionados são benchmarks test-only.
  As conclusões são candidatas a trabalho futuro, não autorização de refatoração.

## Reprodução

```sh
go test ./internal/lock -run '^$' \
  -bench 'SnapshotIntentMetadata|ResolveLegacyV1|BuildLockDocument|NewUniversal|SaveLoadUniversal' \
  -benchmem -benchtime=3x -count=3

go test ./internal/exec -run '^$' \
  -bench 'ExplainAllTools' -benchmem -benchtime=3x -count=3

go test ./internal/graph -run '^$' \
  -bench 'DenseGraph' -benchmem -benchtime=3x -count=3

go test ./internal/app -run '^$' \
  -bench 'UpdateCommand|WhyCommand|GraphCommand' -benchmem -benchtime=3x -count=3
```
