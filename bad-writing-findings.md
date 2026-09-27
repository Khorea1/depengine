# Auditoria de Findings de Escrita Subótima

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

A frase comprime várias transições de estado diferentes em uma única construção e perde paralelismo sintático na parte:

> each rollback compensation crossing symmetric `rollback_applying`/`rollback_applied` boundaries

É necessário reler o parágrafo para reconstruir quais estados antecedem quais mutações e quais estados confirmam conclusão.

O problema não é a terminologia (`applying`, `committing`, `rolling_back`, etc.), que é necessária e corresponde ao modelo transacional. O problema é a estrutura da frase.

### Recomendação

Separar as transições em frases curtas e paralelas.

Exemplo:

> Each prepare mutation is persisted as `applying` before the host mutation and
> confirmed afterward. Commit and rollback are likewise persisted as
> `committing` and `rolling_back` before host-side work. Each rollback
> compensation crosses `rollback_applying` → `rollback_applied`. Terminal
> `committed`/`rolled_back` is reached only after host mutations complete.

### Severidade

**Baixa a média.**

Não há ambiguidade arquitetural grave, mas a redação dificulta desnecessariamente a leitura de uma parte importante da máquina de estados.

______________________________________________________________________

## Finding 2 — ADR-002: referência temporal e agência pouco claras na preparação de sources

**Arquivo:** `docs/design/adr-002-transactional-preparation.md`

### Trecho

> An absent declared source enters durable preparation, availability is
> revalidated after, and an unavailable target compensates the source before
> fallback.

### Problema

Há duas ambiguidades concretas:

1. **`availability is revalidated after`** não especifica claramente _after what_.
1. **`an unavailable target compensates the source`** faz do target indisponível o agente gramatical da compensação, embora a operação aparentemente pertença ao executor/preparation logic.

Também não fica imediatamente claro se `availability` se refere à source, ao candidate ou ao target.

### Recomendação

Tornar explícitos o momento da revalidação, o objeto revalidado e o agente da compensação.

Exemplo:

> After preparing an absent declared source, the executor revalidates candidate
> availability. If the target remains unavailable, the executor compensates the
> source before fallback.

### Severidade

**Média.**

A frase descreve comportamento transacional e fallback; portanto, agência e sequência temporal devem ser inequívocas.

______________________________________________________________________

## Finding 3 — ADR-005: `Status: Proposed` conflita com a taxonomia documental do repositório

**Arquivo:** `docs/design/adr-005-resolved-install-plan-projection.md`

### Problema

O ADR-005 está marcado como:

> `Status: Proposed`

Entretanto, a documentação do próprio repositório descreve `docs/design/adr-*.md` como local para decisões de design aceitas e orienta o uso de ADR quando o artefato relevante é uma decisão e sua justificativa.

Isso cria uma inconsistência entre a taxonomia documental declarada e o conteúdo efetivamente armazenado em `docs/design/adr-*`.

### Recomendação

Escolher uma das seguintes abordagens:

- mover a proposta para uma área de research/design notes até que a decisão seja aceita; ou
- ajustar a convenção documental para permitir ADRs em estado `Proposed`; ou
- promover o ADR-005 para um estado compatível quando a decisão estiver de fato tomada.

### Observação adicional

As seções `Consequences` e `Migration order` do ADR-005 são mais genéricas do que as ADRs anteriores. Frases como:

> command behavior becomes consistent

e passos abstratos de migração poderiam citar invariantes, comandos, tipos ou paths concretos.

Isso é, porém, **polimento de baixa prioridade**, não um finding independente.

### Severidade

**Baixa a média.**

O problema é de consistência e governança da documentação, não de correção técnica.

______________________________________________________________________

Podem ser alterados por preferência editorial, mas não constituem escrita objetivamente defeituosa.

______________________________________________________________________

# Resultado final

Após a auditoria, restam **3 findings acionáveis**:

1. **ADR-002:** parágrafo das transições do journal sintaticamente sobrecarregado.
1. **ADR-002:** referência temporal e agência ambíguas na frase sobre source preparation/fallback.
1. **ADR-005:** estado `Proposed` inconsistente com a taxonomia declarada para ADRs.
