# Spike pyx (rodada 2): custo do manifest × tamanho do catálogo, `Lookup(nome)` e caminhos de carga

> **Estado: preliminar. Não é ADR e não altera nenhum bead.** Responde ao reescopo da
> `dep-spike-catalog-host-specialization-pyx` (custo × tamanho do catálogo; D2 do
> `plan-adr-008-catalog-layer.md`). Nenhum arquivo de produção foi alterado: o harness é um teste
> descartável (`pyx_spike_catalog_cost_test.go`, não incluído em nenhum patch).

- Data: 2026-10-18 · Base: `master @ 4207205` · Go 1.27.2, Xeon 2,1 GHz, **1 vCPU**, 4 GB.
- Relacionado: `report-spike-catalog-identity.md`, `plan-adr-008-catalog-layer.md`, ADR-007.
- **Medições com cache de página quente.** Custo de disco frio não foi medido (não consigo esvaziar o cache no sandbox).

## 1. Critérios declarados antes de medir

Cenário: o schema pede k = 20 ferramentas; o catálogo tem N receitas da forma E com 4 `distributions`
(~730 bytes cada, a forma de `br`). N ∈ {1 000; 10 000; 100 000}. Os limiares abaixo são **proposta minha**,
fixada antes da primeira medição; o mantenedor deve confirmá-los ou trocá-los.

- **V1** eager em N=1 000 custa > 50 ms ou > 20 MB de heap ⇒ o custo já é problema na escala realista de hoje.
- **V2** eager em N=100 000 custa > 1 s ou > 1 GB de RSS ⇒ eager é inviável nessa escala; lookup é necessário.
- **V3** lookup (k arquivos, um TOML por pacote) em N=100 000 custa > 10% do eager, ou varia > 2× entre 1 000 e
  100 000 ⇒ "sistema de arquivos como índice" não resolve; investigar zip/shards.
- **V4** a sequência de candidatos, o `MethodsHash` e o `SourceHash` do lookup diferem do eager para as mesmas k
  ferramentas ⇒ no-go incondicional (quebra identidade; ADR-007).

## 2. Resultados

"eager" = `ParseManifest` (arquivo único) + `FilterManifestTools` + validações da camada + `MergeLayers`, o que
`loadProject` faz hoje. "lookup" = k × `ParseManifest` de um TOML por pacote + o mesmo filtro e merge.
Ambos incluem `ParseProjectSchema` do schema de 20 ferramentas.

| N | eager (mediana) | alocado | pico RSS | lookup (mediana) | pico RSS |
|---|---|---|---|---|---|
| 1 000 | 109–139 ms (n=5; 2 execuções) | 25,4 MiB | 35 MB | 3,9–4,9 ms | 13 MB |
| 10 000 | 2 276–2 302 ms (n=5; 2 execuções) | 254 MiB | 235 MB | 2,3–2,5 ms | 14 MB |
| 100 000 | 327,6 s e 401,0 s (**n=1 cada**, 2 execuções) | 2 526 MiB | 1,57–1,66 GB | 2,3–2,8 ms (n=7 e n=1) | 14 MB |

Crescimento do eager: 1k→10k = 16–21× para 10× de entradas; 10k→100k ≈ 144–176× para 10×. É quadrático,
não linear. O lookup é plano (dentro de 2× entre 1k e 100k).

| Critério | Veredito | Evidência |
|---|---|---|
| V1 | **disparou** | 109–139 ms > 50 ms; 25,4 MiB *alocados* > 20 MiB (é volume alocado, não heap vivo; o critério de tempo basta). |
| V2 | **disparou** | 328–401 s ≫ 1 s; pico 1,57 GB > 1 GB. |
| V3 | **não disparou** | lookup ≈ 0,001% do eager em 100k; variação entre 1k e 100k < 2×. |
| V4 | **não disparou** | candidatos deep-equal (módulo `TargetArch`/`TargetOS`), `MethodsHash` e `SourceHash` iguais nas 20 ferramentas em N = 1k, 10k e 100k. |

## 3. Causa do custo quadrático

Perfil de CPU em N=10 000: ~37% do tempo em `go-toml/v2/internal/tracker.(*SeenTracker).find`. Lendo a fonte
(v2.2.4, `seen.go`), `find` percorre uma lista encadeada com **todos** os irmãos da tabela pai a cada cabeçalho
de tabela, para detectar chaves duplicadas. Com N tabelas `[packages.<nome>.github]` irmãs sob `packages`,
cada cabeçalho custa O(N). O resto do tempo (`ExpandAll`, `normalizeTools`, alocação) é linear.

Consequência: o problema é do **formato de uma tabela larga** decodificada pelo go-toml, não de "ler um
arquivo grande". Um TOML por pacote o evita por construção. Não testei alternativas que mantenham um arquivo
único (por exemplo, vários manifests menores); são hipóteses, não resultado.

## 4. D2: caminhos de carga (verificado em `master @ 4207205`)

| Caminho | Quem usa | Mapa de placeholders |
|---|---|---|
| `app.loadProject` | `status`, `upgrade`, `check`, `graph`, `why`, `install`, `update` (e o helper de `helpers.go:189`) | **fatos do host** (`BuildMap`) |
| `app.runValidate` → `mergeManifest` | `validate` | **vazio** (`map{}` / `nil`) |
| `config.ResolveSchemaFromFiles` | nenhum chamador de produção (só testes) | `nil` |
| `cmd/schema-gen` | gera JSON Schema; não carrega schema de usuário | n/a |

Implicações: (a) um `Lookup(nome)` precisa entrar em **dois** pontos, `loadProject` e o fluxo de `validate`, ou
`validate` passa a validar menos do que executa; (b) `ResolveSchemaFromFiles` é código sem uso em produção,
candidato a remoção ou a virar só auxiliar de teste (não verifiquei referências fora do repositório).

**Interação com o achado do N3** (`method_order` com `{placeholders}`): com o mapa vazio, `validate` enxerga a
ordem `[github-fallback arch debian]` (hash `3320018712…`). Ela coincide com a de um host cuja família não casa com
nenhum rótulo (fedora no fixture), mas difere da de arch (`cf9bc96908…`) e da de debian (`1d4984926a…`). Ou seja:
para esses schemas, `validate` verifica a sequência de alguns hosts e não a de outros.

## 5. Respostas às perguntas do bead

1. **Índices de candidato estáveis?** Sim para lookup por nome **sem fatos do host** (V4: sequência deep-equal em
   três escalas). Não testei catálogo especializado por host; o ADR-007 o proíbe e o teste de invariância (N3) é o guarda.
2. **Pins v1 estáveis?** Implícito: a chave é função de nome, `Kind` e posição, e a sequência é igual. O harness
   **não** assertou as chaves de pin nem exercitou um arquivo de lock.
3. **`MethodsHash` estável?** Sim (V4, 20 ferramentas, três escalas).
4. Terceiro no-go do bead (validação que só passa no host que gerou o catálogo): **não testado**; exige catálogo
   especializado por host.

## 6. O que continua em aberto

- **Custo não é mais a razão decisiva para recusar o lookup**; a pergunta passa a ser D1 (unidade do pin),
  D3 (orçamento), D5 (confiança do import) e a escala em que alguém realmente chega a 1 000 entradas num manifest.
- Não medi: disco frio; diretório plano de 100 mil arquivos em NTFS/APFS; custo de **construir/atualizar** o
  catálogo; operações que precisam de todos os nomes (busca/listagem); `zip`/shards (o esboço cita ~220 ms a frio
  em 100 mil, sintético, não comparável com estas medições quentes).
- As amostras de 100 000 são n=1 por execução (a execução custa 5–7 min em 1 vCPU); a variação entre as duas
  execuções foi de 22%.
- O catálogo sintético tem receitas idênticas em forma; um corpus real pode ter entradas maiores ou menores.

## 7. Reprodução

`gen_pyx_catalog.py N` gera `n<N>/{manifest.toml,catalog/*.toml,schema.toml,names.txt}`;
`pyx_spike_catalog_cost_test.go` vai em `internal/lock/` (descartável) e roda com
`PYX_DIR=… PYX_MODE=eager|lookup|both PYX_REPS=… go test -run TestPyxSpike -v`. Rode cada modo em processo
separado para ter o RSS limpo.
