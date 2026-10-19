# Esboço: ADR-008 (camada de catálogo e import de manifests)

> **Estado (2026-10-18): superado pelo rascunho `docs/design/adr-008-catalog-layer.md` (Proposed),
> que traz a origem de cada decisão e as pendências. Este arquivo fica como registro das decisões
> da conversa (T1–T5) e das correções abaixo; apague-o quando o ADR for aceito.**
> Não é contrato e não autoriza sintaxe. Depende de ADR-007.

## Correções feitas pela investigação de 2026-10-18

- **Item 3 do esqueleto estava errado.** "Mudar a receita muda o `MethodsHash`" é falso: o hash cobre só
  `Kind` e `Label` em ordem (ADR-001 já dizia). Uma receita com conteúdo alterado e a mesma sequência não o
  move; a identidade concreta vem do `LockDocument` v2. O catálogo precisa de identidade própria por isso
  (o ADR-007, Consequences, tinha a mesma frase; o patch 07 a corrige).
- **D2 fechada com evidência.** Todos os comandos dependentes de host passam por `app.loadProject`; `validate`
  tem caminho próprio com mapa de placeholders vazio; `ResolveSchemaFromFiles` não tem chamador de produção.
  Ver `report-spike-pyx-catalog-cost.md`.
- **D6 fechada.** `dep-41b` está fechada (2026-10-12): o gate de aposentadoria do lock v1 existe. A
  identidade do catálogo vai só no lock v2; o catálogo não cria obrigação para o esquema de chaves v1.
- **D1: o `update` foi verificado** (teste descartável, `update_retention_spike_test.go`): `update` completo
  adota receita alterada sem aviso; entrada retida por `--profile` não é refrescada e o install a rejeita
  ("artifact identity changed") enquanto `ValidateFrozen` passa; mudar só `extract_to`/`link_dir`/`entrypoints`
  não altera o plano resolvido nem o lock. O lock fixa o que a receita resolve, não a receita.
- **Nova D7:** `allow_new_tools` em camada de catálogo (mantém todas as ferramentas, sem sentido de "puxar
  por nome") e tratamento de `requires` transitivo; este último não foi verificado.
- **Pré-requisito novo:** `[defaults].method_order` com placeholders viola o ADR-007 (decisão 1). Ver
  `TestKnownViolationMethodOrderPlaceholderReordersCandidates`.

## Pergunta que o ADR responderia

Como o depengine passa a conhecer muitas receitas de instalação sem que o custo de parse,
planejamento e manutenção dependa do tamanho do catálogo, sem quebrar identidade de candidato
(ADR-007), lock e o modelo de confiança de hooks?

## Decisões da conversa (T1–T5; nenhuma é contrato de formato)

| # | Decisão | Origem |
|---|---|---|
| T1 | Dono do catálogo = o usuário | mantenedor, conversa |
| T2 | Import de manifests na v1: **somente declarativo** (sem hooks) | mantenedor, conversa |
| T3 | **Cancelada** a linguagem própria de instalação (reescrita do motor e sintaxe própria). O formato compilado do "bead" (serialização) segue. Reabre **somente** se existir um manifesto real e documentado que o TOML não consiga expressar | mantenedor, 2026-10-17 |
| T4 | Fontes derivadas (ex.: Repology) não são a única forma de declarar | mantenedor, conversa |
| T5 | Precedência: **usuário > (manifest + fontes derivadas)**, como o merge já faz (valores do projeto vencem). Dentro da camada de baixo, uma **lista ordenada de fontes** no manifest (a primeira vence), sem terceiro nível de precedência | mantenedor, 2026-10-17 |

## Esqueleto proposto

1. **Contrato de lookup** (`Lookup(nome) → receita completa`, sem fatos do host; ADR-007 §4).
   Esconde o armazenamento. Começar por um TOML por pacote (o sistema de arquivos como índice);
   `zip`/shards ou índice próprio só se a medição pedir (100 mil entradas: ~220 ms de partida a
   frio em `zip`, sintético).
2. **Camadas**: catálogo < manifest < schema, mescladas antes do cálculo de identidade. O filtro
   por nome de `FilterManifestTools` deixa de ser descarte pós-parse e vira consulta.
3. **Pin do catálogo no lock.** Mudar a receita muda o `MethodsHash`; o catálogo precisa de
   identidade fixada (revisão ou digest), reaproveitando o padrão de revisão de tap/bucket
   (`[ABERTO]` D1; recomendação do rascunho: digest por receita para verificar, revisão do catálogo só para recuperar).
4. **Import declarativo**: o "bead" é o modelo tipado serializado, versionado e endereçado por
   conteúdo. O validador de import **rejeita campos executáveis** (hooks, comandos) na v1.
5. **Orçamento de candidatos por ferramenta**: limite aplicado na construção/import, com aviso e
   templates para o caso comum. Evita recriar a matriz plana de forma distribuída.
6. **Proveniência visível**: estender `why --fields` para mostrar a camada de origem (catálogo,
   import, manifest, schema).

## Decisões abertas `[ABERTO]`

- **D1. Unidade de identidade do pin:** revisão do catálogo inteiro, digest por receita, ou os
  dois? Afeta o tamanho do lock e o que "drift" significa quando só uma receita muda.
- **D2. [FECHADA]** Lookup único, chamado por `loadProject` e pelo caminho de `validate`.
- **D3. Orçamento numérico** de candidatos por ferramenta (valor e se bloqueia ou só avisa).
- **D4. `distributions` além de `github`** (ex.: `http`) ou aceitar candidatos enumerados nessas
  receitas. Sem corpus, não há dado para decidir.
- **D5. Segurança do import**: de onde vem a confiança (digest pinado por quem importa, assinatura
  do desenvolvedor, ambos) e o que acontece quando a v2 permitir hooks.
- **D6. [FECHADA]** `dep-41b` já define o gate; identidade do catálogo só no lock v2.

## O que este esboço **não** diz

- Não define sintaxe de importação, formato binário do "bead" nem layout de diretórios.
- Não propõe comparação de versão em `kernel`/`libc` (rebaixada; `dep-docs-kernel-exact-example-smk`
  corrige só o exemplo da documentação).
- Não resolve a otimização do gate (`dep-gate-*`), que é independente.
