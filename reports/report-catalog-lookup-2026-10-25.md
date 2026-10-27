# Catálogo por lookup: backend, paridade e custo

> **Estado: implementação da issue `dep-ixd.3`. Não é ADR nem altera a
> semântica do lock.** Registra a medição que fecha o critério de performance e
> paridade do backend de catálogo.

- Data: 2026-10-25
- Issue: `dep-ixd.3` (P1), dependente de `dep-ixd.14` (ADR-009)
- Ambiente: Go 1.27.1 linux/amd64, AMD Ryzen 9 7900 (12 núcleos/24 threads),
  cache de página quente, 1 máquina.
- Artefatos: `internal/config/catalog.go`, `internal/config/catalog_test.go`,
  `internal/app/catalog_parity_test.go`, `internal/app/catalog_flags.go`.
- Relacionado: `report-spike-pyx-catalog-cost-2026-10-18.md` (custo do caminho
  eager), `report-spike-catalog-identity-2026-10-17.md` (identidade),
  `docs/design/adr-008-catalog-layer.md`, `docs/design/adr-009-catalog-layout.md`.

## Resumo

1. **20 lookups custam ~0,32–0,48 ms** com 1.000 e 100.000 receitas. O custo é
   independente de N, como o backend de um arquivo por receita previa.
2. **20 lookups + especialização por host custam ~0,59–0,73 ms** nas mesmas
   escalas. A segunda leitura é o preço de manter o lookup host-independente e
   ainda entregar placeholders expandidos ao executor.
3. **Paridade com o caminho eager passa em 1.000 e 100.000 receitas**:
   candidatos, configuração, `MethodsHash` e `SourceHash` iguais para as
   amostras comparadas.
4. O caminho eager de 100.000 receitas levou ~14,8 s neste fixture sintético
   (uma execução, sem repetição), contra ~0,5 ms para 20 lookups. A diferença
   confirma que o custo relevante é o parse integral, não o lookup.
5. A meta do ADR-009 (20 lookups ≤ 10 ms em 1.000 e 100.000) foi atendida com
   margem superior a 20×.

## Medições

Comando:

```sh
go test ./internal/app -run '^$' -bench '^BenchmarkCatalogLookup20$' \
  -benchtime=3x -count=1
```

O fixture cria N receitas `native` de uma linha com um `sources` cada e um
manifesto `[packages]` equivalente. `lookup` mede apenas `Catalog.Lookup`;
`resolve` mede `Lookup` + `Recipe.Specialize` com fatos fixos. Tempo por
operação é o custo das 20 consultas; a criação do fixture fica fora da medição.

| N | modo | ns/op (20 consultas) | ms/consulta | B/op | allocs/op |
|---:|---|---:|---:|---:|---:|
| 1.000 | lookup | 398.847 | 0,020 | 292.328 | 2.543 |
| 1.000 | resolve | 727.793 | 0,036 | 590.952 | 5.026 |
| 100.000 | lookup | 481.049 | 0,024 | 304.888 | 2.546 |
| 100.000 | resolve | 594.579 | 0,030 | 590.952 | 5.026 |

A variação entre 1.000 e 100.000 é ruído de uma única execução com
`-benchtime=3x`, não crescimento com N.

## Paridade

`TestCatalogLookupParityWithEager` compara 20 nomes de um fixture de 1.000
receitas. `TestCatalogLookupParityWithEager100K` faz o mesmo com 5 nomes e
100.000 receitas; é opt-in para não onerar o loop normal:

```sh
DEPENGINE_CATALOG_PARITY_100K=1 \
  go test ./internal/app -run '^TestCatalogLookupParityWithEager100K$' -count=1 -v
```

Resultado: `PASS` em 14,77 s. Para cada amostra, o teste compara:

- número e identidade (`Kind`/`Label`) dos candidatos;
- `MethodCandidate.Config` e `Sources` com `reflect.DeepEqual`;
- `MethodsHash` e `SourceHash` via `lock.SnapshotIntentMetadata`.

O fixture usa `native` com `pkg` e `sources = [{ kind = "brew-tap", ... }]`,
sem rede. `TargetArch`/`TargetOS` são iguais porque os dois caminhos recebem os
mesmos fatos.

## Limitações

- Uma máquina, cache quente, uma execução por célula; não é um benchmark de
  cauda nem de disco frio.
- O fixture é sintético e simples (uma receita de uma linha). Não cobre
  receitas com `distributions`, hooks, placeholders condicionais ou métodos
  múltiplos; a paridade estrutural desses casos é coberta pelos testes de
  `internal/config` e pelo caminho eager compartilhado.
- A criação de 100.000 arquivos não é medida; o benchmark mede consulta, não
  autoria ou importação.
- Identidade de conteúdo da receita no lock v2 e o fail-closed de lock v1 com
  catálogo continuam em `dep-ixd.6`.
