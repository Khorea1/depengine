# Auditoria de findings de escrita subótima

## Finding 1 — ADR-002: descrição das transições do journal está sintaticamente sobrecarregada

**Arquivo:** `docs/design/adr-002-transactional-preparation.md`

### Trecho

> Each prepare mutation crosses a persisted `applying` boundary before host
> mutation and is confirmed afterward; commit planning persists `committing`
> before host-side commit, rollback planning persists `rolling_back` before
> compensation, each rollback compensation crossing symmetric
> `rollback_applying`/`rollback_applied` boundaries. Terminal
> `committed`/`rolled_back` is reached only after host mutations complete.

### Problema

A frase comprime várias transições de estado diferentes em uma única construção
e perde paralelismo sintático na parte:

> each rollback compensation crossing symmetric `rollback_applying`/`rollback_applied` boundaries

É necessário reler o parágrafo para reconstruir quais estados antecedem quais
mutações e quais estados confirmam conclusão.

### Recomendação

Separar as transições em frases curtas e paralelas:

> Each prepare mutation is persisted as `applying` before the host mutation and
> confirmed afterward. Commit and rollback are likewise persisted as
> `committing` and `rolling_back` before host-side work. Each rollback
> compensation crosses `rollback_applying` → `rollback_applied`. Terminal
> `committed`/`rolled_back` is reached only after host mutations complete.

### Severidade

**Baixa a média.**

## Finding 2 — ADR-002: referência temporal e agência pouco claras na preparação de sources

**Arquivo:** `docs/design/adr-002-transactional-preparation.md`

### Trecho

> An absent declared source enters durable preparation, availability is
> revalidated after, and an unavailable target compensates the source before
> fallback.

### Problema

A frase não especifica claramente após qual etapa a disponibilidade é
revalidada, nem deixa explícito que a lógica de preparação/executor executa a
compensação. Também não identifica se `availability` se refere à source, ao
candidate ou ao target.

### Recomendação

> After preparing an absent declared source, the executor revalidates candidate
> availability. If the target remains unavailable, the executor compensates the
> source before fallback.

### Severidade

**Média.**

## Estado

Os findings acima permanecem acionáveis. O finding anterior sobre
`ADR-005` foi removido porque o documento agora está marcado como `Accepted`,
em conformidade com a taxonomia atual de `docs/design/adr-*.md`.
