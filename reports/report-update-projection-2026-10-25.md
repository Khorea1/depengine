# Update: eliminar reprojeção redundante do lock v2

> **Estado: preliminar. Não é ADR e não aprova otimizações adicionais.**
> Mede o refactor da issue `dep-ixd.10` sobre `master @ ee55543`.

- Data: 2026-10-25 · Ambiente: Go 1.27.2 linux/amd64, AMD Ryzen 9 7900 (12 núcleos/24 threads), 1 máquina.
- Issue: `dep-ixd.10` (P2).
- Benchmark: `BenchmarkUpdateCommand` em `internal/app/bench_update_why_graph_test.go`, schema sintético, sem rede ou instalação real.
- Comando: `go test ./internal/app -run '^$' -bench 'BenchmarkUpdateCommand' -benchmem -benchtime=3x -count=3`.
- Mediana de 3 amostras de 3 iterações; valores em ns/op.

## O que era redundante

No caminho v2, `runUpdate` decodificava a projeção anterior duas vezes e a projeção recém-gerada uma vez:

1. `oldLock.ProjectionDocument()` em `runUpdate`, para decidir quais ferramentas precisavam ser re-resolvidas.
2. `oldLock.ProjectionDocument()` novamente dentro de `resolveUniversalLockDocument`, para retenção e carry-forward de checksum.
3. `newLock.ProjectionDocument()` depois de `NewUniversal`, só para contar pins e imprimir o dry-run.

O refactor decodifica a projeção anterior uma vez e passa o documento já decodificado para a resolução; o documento recém-projetado é reutilizado para contagem e dry-run. `BuildLockDocument`, `EncodeLockDocument` e a gravação do lock continuam acontecendo uma vez.

## Resultados

| Caso | Antes (mediana) | Depois (mediana) | Variação |
|---|---:|---:|---:|
| 100 ferramentas, cold | 11,44 ms | 11,50 ms | +0,5% (ruído) |
| 100 ferramentas, warm | 13,61 ms | 11,63 ms | −14,6% |
| 1.000 ferramentas, cold | 91,72 ms | 86,04 ms | −6,2% |
| 1.000 ferramentas, warm | 113,67 ms | 106,23 ms | −6,5% |
| 5.000 ferramentas, cold | 511,66 ms | 469,51 ms | −8,2% |
| 5.000 ferramentas, warm | 663,46 ms | 592,59 ms | −10,7% |

Memória e alocações, 5.000 ferramentas:

| Caso | B/op antes | B/op depois | Allocs antes | Allocs depois |
|---|---:|---:|---:|---:|
| cold | 307,8 MB | 277,5 MB (−9,9%) | 3.406.997 | 3.211.822 (−5,7%) |
| warm | 414,5 MB | 353,6 MB (−14,7%) | 4.012.581 | 3.622.181 (−9,7%) |

## Leitura

- O ganho cresce com o número de ferramentas porque as decodificações eliminadas eram proporcionais ao tamanho da projeção.
- O caso de 100 ferramentas cold ficou dentro do ruído; a diferença aparece a partir de 1.000.
- O custo restante dominante é a construção e a codificação da projeção (`BuildLockDocument` + `EncodeLockDocument`), não a decodificação. Uma próxima otimização deve perfilar essas etapas antes de mexer no formato.
- Nenhuma mudança de comportamento: o lock resultante é o mesmo; fallback v1 nativo, cobertura v2 e all-or-nothing foram preservados e a suíte completa passou.

## Limites

- Uma máquina, um schema sintético e três amostras; não é uma comparação entre versões com cargas reais.
- O benchmark usa runner falso e não inclui rede, resolução real de pacotes ou instalação.
- A variação de ±5% entre amostras é plausível; os ganhos de 5.000 ferramentas são maiores que esse ruído, mas 1.000 cold está próximo dele.

## Reprodução

```sh
go test ./internal/app -run '^$' -bench 'BenchmarkUpdateCommand' \
  -benchmem -benchtime=3x -count=3
```
