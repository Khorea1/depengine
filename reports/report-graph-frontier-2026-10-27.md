# Grafo: fronteira incremental em SortGraph/Analyze

> **Estado: implementação da issue `dep-ixd.12`.** Registra a hipótese, a
> medição antes/depois e a evidência de equivalência. Não é ADR.

- Data: 2026-10-27
- Issue: `dep-ixd.12` (P3)
- Ambiente: Go 1.27.1 linux/amd64, AMD Ryzen 9 7900 (12 núcleos/24 threads),
  cache de página quente, 1 máquina.
- Artefatos: `internal/graph/sort.go`, `internal/graph/sort_frontier_test.go`,
  `internal/graph/dense_bench_test.go`.

## Hipótese (antes da mudança)

`SortGraph` montava, a cada nível, a lista de nós com grau de entrada zero
varrendo **todos** os nós restantes. O fixture denso encadeia cada nó aos
próximos `fanIn` nós, então a profundidade cresce com `nós/fanIn`: 1.000 níveis
em 5.000 nós com fan-in 5. O custo virava `O(nós × profundidade)` de lookups de
mapa (~5 milhões por chamada), e `Analyze` pagava o mesmo porque chama
`SortGraph` primeiro.

Manter uma fronteira incremental (apenas os nós que passaram a ter grau zero)
produz exatamente os mesmos conjuntos de nível, com custo `O(nós + arestas)`
mais a ordenação de cada nível.

## Mudança

`SortGraph` inicializa uma fila ordenada de nós de grau zero e, ao processar um
nível, decrementa os sucessores e enfileira os que chegam a zero. Cada nível é
ordenado antes de ser emitido, preservando a ordem determinística e o
`logger.Debug` por nível. O caminho de ciclo reconstrói o conjunto não agendado
somente ao falhar, para que `extractCycle` reporte o mesmo ciclo de antes.

Nenhum formato de saída, código de saída ou assinatura mudou.

## Medições

Comando (mediana de 5 execuções, `-benchtime=20x`):

```sh
go test ./internal/graph -run '^$' \
  -bench 'BenchmarkDenseGraph(Sort|Analyze)' -benchtime=20x -count=5
```

| Benchmark | nós | fan-in | antes (ms) | depois (ms) | ganho |
|---|---:|---:|---:|---:|---:|
| Sort | 1.000 | 5 | 9,05 | 1,63 | 5,6× |
| Sort | 1.000 | 20 | 12,14 | 4,55 | 2,7× |
| Sort | 5.000 | 5 | 193,32 | 8,17 | 23,7× |
| Sort | 5.000 | 20 | 225,42 | 26,46 | 8,5× |
| Analyze | 1.000 | 5 | 10,53 | 3,69 | 2,9× |
| Analyze | 1.000 | 20 | 17,25 | 10,24 | 1,7× |
| Analyze | 5.000 | 5 | 200,96 | 17,92 | 11,2× |
| Analyze | 5.000 | 20 | 250,92 | 51,12 | 4,9× |

Alocações ficaram estáveis (`Sort` 5.000/fan-in 5: 45.186 → 45.171
allocs/op; `B/op` inalterado). O ganho vem da remoção das varreduras, não de
trocar alocação por tempo.

O caso de fan-in 20 tem profundidade menor (250 níveis em 5.000 nós), por isso
ganha menos que fan-in 5. `Analyze` mantém o custo de ordenar listas de
vizinhança e de percorrer componentes, que ficou fora deste escopo.

## Equivalência

`internal/graph/sort_frontier_test.go` mantém a implementação por varredura
como oráculo e compara, nível a nível:

- DAGs aleatórios determinísticos com 1, 2, 7, 40 e 250 nós e até 3
  dependências por nó, três sementes por tamanho;
- um ciclo de agendamento `a → b → c → a` com um nó `d` que depende de `a`,
  comparando o `CycleError.Cycle` reportado.

Os testes de projeção, renderização e o golden de texto/DOT existentes rodam
sem alteração e continuam verdes.

## Limitações

- Uma máquina, cache quente, uma única fixture sintética (cadeia com fan-in
  fixo). Não cobre grafos com profundidade pequena e largura grande, onde a
  varredura antiga já era barata.
- `Analyze` ainda ordena as listas de vizinhança por nó e refaz uma
  canonicalização dentro de `SchedulingProjection`; medir e remover esse custo
  seria outra mudança, não esta.
