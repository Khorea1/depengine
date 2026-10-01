# Depengine — Simplification Log

## Status: CLOSED — 2026-09-30

SIM-01–SIM-08 encerrados: o Executor retém configuração reutilizável; clan,
gerenciador nativo e ordem efetiva pertencem ao runContext. Seleção e resolução
read-only recebem clan explícito, sem preparação temporal do host. SIM-04 foi
revisado sem extrair canonicalizer comum: não há fronteira compartilhada segura.
SIM-09 e SIM-10 permanecem guardrails, sem novo framework ou módulos.

Validação concluída no worktree research-log-closure: `go build -o /tmp/depengine-research-closure .`,
`go test -race ./...`, `go vet ./...`, `golangci-lint run`, `git diff --check`,
`gofmt -e -l <arquivos Go alterados>` e `gopls check <arquivos Go alterados>`.
`tests/integration/run_test.sh` passou; Docker Compose build e run --rm passaram
para debian, arch, fedora e alpine em tests/integration/docker-compose.yml e
tests/crossplatform/docker-compose.yml (DEPENGINE_BIN=/tmp/depengine-closure).
O binário final passou os scripts native-lifecycle.sh (native-hello.toml) e
native-failure.sh (native-missing.toml) em Debian isolado, com instalação real,
status, reinstalação idempotente, remoção e ausência de tracking após falha.
Cross-build GOOS darwin/windows/freebsd/openbsd/netbsd, GOARCH=amd64 passou.
Os jobs de runtime macOS, BSD, Windows e Android dependem dos runners/VMs da CI;
não foram executados localmente e não são considerados aprovados por cross-build.

O restante deste log preserva a análise original.

**Objetivo:** registrar somente propostas que reduzam custo de manutenção, ambiguidade de estado, duplicação, acoplamento ou risco operacional no Depengine.

**Escopo analisado:** corpus `DEPENGINE-FRs` (32 notas de pesquisa + `SEMANTIC_GROUPS.md`) e o estado atual do repositório `Khorea1/depengine` em `master`, árvore Git `cd64e1193eeccff609b4c8fb77ef1d082e9beb2b`, consultada em 2026-09-30.

Este arquivo deliberadamente não contém propostas cujo valor principal seja adicionar comandos, novos ecossistemas, novas superfícies públicas, novos formatos persistidos ou subsistemas de cache/distribuição. A régua aqui é simples: se a proposta exige explicar muita infraestrutura nova antes de explicar qual complexidade existente ela remove, ela não pertence neste log.

## Critério de inclusão

Uma proposta entra aqui apenas quando satisfaz a maior parte destes critérios:

- preserva o comportamento externo atual ou muda apenas contratos internos/documentação;
- reduz estado mutável implícito;
- elimina ou consolida fontes de verdade duplicadas;
- transforma convenções importantes em invariantes verificáveis;
- melhora isolamento de testes e previsibilidade operacional;
- escala melhor com o número de adapters, comandos e contribuidores;
- pode ser implementada incrementalmente, com diffs pequenos e reversíveis;
- não exige um novo subsistema para justificar a própria existência.

---

## SIM-01 — Separar estado de uma execução do `Executor`

**Decisão:** incluir. **Prioridade:** alta.

### Problema atual

`internal/exec.Executor` mistura dependências/políticas relativamente estáveis com dados que pertencem a uma única execução. No estado atual do repositório, o mesmo tipo contém, entre outros:

- runner, secret resolver, timeouts, logger e adapters;
- `schema`, `report`, `sources`;
- `recoveredCommits` e `dependencies`;
- `clan`, `nativeManagerName` e `defaultMethodOrder` efetivo;
- facts, lock document e informações do schema usadas na execução.

Ao mesmo tempo, `internal/exec/run.go` já possui um `runContext` com parte do estado run-scoped, incluindo `schema`, `report` e `failed`. Isso mostra que a fronteira já começou a existir, mas ficou incompleta: parte do lifecycle está no contexto da execução e parte continua no `Executor`.

Isso funciona para o processo CLI de uma execução, mas deixa ownership e reutilização implícitos. O tipo não deixa claro o que pode ser reutilizado com segurança e o que precisa nascer e morrer junto com uma chamada de execução.

### Proposta simplificada

Evoluir o `runContext` existente para ser a fronteira única de estado mutável/run-scoped. Se o nome `executionSession` comunicar melhor o ownership, renomear o tipo existente; **não criar `executionSession` em paralelo com `runContext`**.

Regra de ownership:

```text
Executor         = dependências + configuração/política reutilizável
executionSession = estado mutável e valores efetivos de uma única execução
```

Não transformar isso em API pública e não aproveitar a refatoração para redesenhar o executor inteiro.

### Primeiro corte recomendado

1. Consolidar no contexto/sessão existente o que já está parcialmente duplicado:
   - `schema`;
   - `report`;
   - `failed`.
2. Remover as cópias equivalentes do `Executor` quando os call sites permitirem.
3. Em seguida, mover somente:
   - `sources`;
   - `recoveredCommits`;
   - `dependencies` e seu mutex.

Não mover `clan`, `nativeManagerName` ou `defaultMethodOrder` no primeiro patch.

Antes de mover `defaultMethodOrder`, separar explicitamente os dois conceitos que hoje podem se sobrepor:

```text
configured default = configuração reutilizável do Executor
effective order    = valor derivado/selecionado para uma execução
```

Só o segundo pertence naturalmente à sessão. Aplicar a mesma régua a clan/native manager: migrar apenas quando ficar demonstrado que são valores efetivamente run-scoped e que a mudança reduz acoplamento nos call sites existentes.

### Por que simplifica

- elimina duplicação entre `Executor` e o contexto de execução já existente;
- reduz risco de vazamento de estado entre execuções;
- torna o lifecycle legível no tipo, em vez de depender de conhecimento tribal;
- facilita testes que reutilizam um `Executor` sem estado residual;
- cria um lugar natural para dados de concorrência/recovery que já são run-scoped;
- evita que o `Executor` continue crescendo como um recipiente genérico conforme `internal/exec` escala.

### Critério de parada

Se a migração começar a exigir um segundo contexto de execução, novos pacotes, interfaces públicas ou uma hierarquia de sessões/subsessões, parar. O ganho vem de consolidar uma fronteira que já existe, não de criar um framework de lifecycle.

**Fontes de pesquisa:** `research/08-general-patterns/otp-supervision-patterns.md`, Proposal 1.  
**Evidência atual:** `internal/exec/executor.go`, `internal/exec/execute.go`, `internal/exec/run.go`.

---

## SIM-02 — Formalizar um único modelo de estado e suas invariantes

**Decisão:** incluir. **Prioridade:** alta e barata.

### Problema atual

O comportamento de desired state está espalhado de forma legítima entre `docs/architecture.md`, `docs/support-boundary.md`, `internal/plan`, `internal/lock`, `internal/state`, `internal/source` e fluxos de `install/status/check/update/upgrade/remove/undo`.

A implementação já possui conceitos fortes, mas um mantenedor ainda precisa reconstruir mentalmente o caminho completo entre intenção declarada, resolução, lock, preparação, observação e ownership persistido.

### Proposta simplificada

Criar um documento normativo curto, preferencialmente `docs/design/state-model.md`, com apenas o fluxo canônico:

```text
schema + manifest
    ↓
declared intent
    ↓
resolved install plan
    ↓
lock projection
    ↓
prepared sources / prerequisites
    ↓
observed machine state
    ↓
recorded ownership state
```

Adicionar uma matriz dizendo, para cada fronteira:

- qual é a invariante;
- quem pode ler;
- quem pode mutar;
- o que acontece quando a invariante quebra;
- quando a resposta é `absent`, `drifted`, `unknown` ou `broken`.

O documento deve referenciar os textos já autoritativos em vez de copiar detalhes de lock/schema para uma segunda fonte.

### Por que simplifica

- reduz divergência entre comandos que operam sobre o mesmo estado;
- dá uma referência única para revisão de mudanças em lock, ownership e reconciliation;
- transforma bugs de “cada comando assumiu uma coisa” em violações de uma matriz explícita;
- melhora onboarding sem adicionar runtime code;
- permite que testes sejam organizados por invariantes em vez de apenas por comando.

### Critério de parada

Não criar um novo formato, state machine executável ou framework de workflow. O artefato necessário é uma especificação pequena e testável do modelo que o código já implementa.

**Fontes de pesquisa:** `research/01-dependency-resolution/golang-dep-lessons.md`, Candidate 1 e Candidate 6.  
**Evidência atual:** `docs/architecture.md`, `docs/support-boundary.md`, `internal/plan`, `internal/lock`, `internal/state`.

---

## SIM-03 — Tornar failure domains um contrato explícito

**Decisão:** incluir. **Prioridade:** alta e barata.

### Problema atual

O executor já possui concorrência por níveis, dependências, fallback e recovery. A política correta existe em código e testes, mas parte dela continua implícita: o que deve continuar quando uma ferramenta falha, o que deve ser bloqueado e quando uma mutação pode ou não ser repetida.

Essa ambiguidade cresce com `--jobs`, lazy dependencies e recovery, justamente onde “parece funcionar” costuma ser a fase anterior a uma tarde pouco produtiva.

### Proposta simplificada

Documentar e testar esta regra:

```text
Failure domain = dependency closure

Se X falha:
- irmãos independentes podem continuar;
- descendentes que dependem de X são bloqueados;
- ramos não relacionados continuam;
- mutações não são repetidas sem evidência durável do resultado anterior;
- estado externo ambíguo continua fail-closed.
```

Adicionar a regra a `docs/architecture.md` ou ao state model de SIM-02. Reforçar apenas testes que cubram sibling continuation, dependent blocking e recovery ambíguo.

### Por que simplifica

- reduz decisões ad hoc em futuros ajustes de concorrência;
- dá uma regra única para install/recovery/batch/lazy dependency;
- evita retries/replays perigosos sendo introduzidos como “robustez”;
- facilita review: uma mudança preserva ou altera explicitamente o failure domain.

### Critério de parada

Não introduzir actors, supervisor trees, restart policies ou um scheduler novo. O valor está em formalizar o comportamento atual, não em imitar OTP.

**Fonte de pesquisa:** `research/08-general-patterns/otp-supervision-patterns.md`, Proposal 2.  
**Evidência atual:** `internal/exec/batch.go`, `internal/exec/run.go`, `internal/exec/preparation.go`, testes de lazy dependency/recovery.

---

## SIM-04 — Centralizar canonicalização de identidade de origem, sem criar um fetch manager

**Decisão:** incluir. **Prioridade:** média-alta.

### Problema atual

O repositório já tem mais de uma implementação de normalização ou parsing de origem:

- `internal/source/manager.go` possui `canonicalSourceURL`;
- `internal/ghrelease/resolver.go` possui parsing próprio de `owner/repo`, URL GitHub e remoção de `.git`;
- adapters Git/ecosystem precisam interpretar referências de repositório e revisão;
- `internal/source/identity.go` cobre identity de package sources, mas não é uma canonicalização geral de Git/GitHub/artifact URL.

Essas funções têm propósitos próximos, mas isso **não significa que tenham a mesma semântica**. Por exemplo, canonicalizar uma URL para comparação e validar quais formas de referência um resolver aceita são operações diferentes, mesmo quando compartilham regras de parsing.

O risco atual é duplo: manter duplicação realmente equivalente ou, na tentativa de removê-la, unificar comportamentos que deveriam continuar distintos.

### Proposta simplificada

Extrair somente funções puras de identidade/canonicalização quando houver pelo menos dois call sites com semântica comprovadamente igual.

Antes de extrair uma função, verificar explicitamente que os consumidores compartilham:

- a mesma definição de equivalência;
- o mesmo conjunto relevante de inputs aceitos/rejeitados;
- a mesma política para `.git`, scheme, host, casing e formas abreviadas;
- o mesmo significado de erro ou referência inválida;
- o mesmo nível de confiança exigido para a comparação.

Só então colocar a função em uma fronteira estreita, seja em um `internal/sourceid` pequeno ou em um local já existente que não crie ciclo de imports.

Candidatos a consolidação, conforme duplicação equivalente for comprovada:

- identidade de GitHub repo (`owner/repo`, `github.com/owner/repo`, URL HTTPS);
- Git remote URL para comparação segura;
- artifact URL quando canonicalização for realmente necessária;
- package-source URL já usada para verificar Brew/Scoop.

Requisitos:

- zero acesso à rede;
- zero cache;
- zero download;
- zero alteração de comportamento no primeiro patch além de substituir duplicação comprovada;
- canonicalização conservadora: equivalência sintática não deve virar equivalência de trust por acidente;
- nenhuma função comum deve ampliar silenciosamente os inputs aceitos por um resolver/adapter.

### Por que simplifica

- elimina regras de parsing realmente duplicadas em adapters/resolvers;
- reduz drift entre comparação de source, lock identity e diagnostics onde a semântica for a mesma;
- centraliza testes de edge cases compartilhados;
- evita falsa reutilização entre parsers apenas superficialmente parecidos;
- melhora consistência sem criar uma abstração de transporte genérica.

### Critério de parada

Se não for possível demonstrar equivalência semântica entre pelo menos dois consumidores, manter as implementações separadas. E, se o pacote começar a conhecer download, cache, mirrors, materialization ou seleção de candidatos, ele passou da função de simplificação. Esses problemas devem permanecer separados.

**Fonte de pesquisa:** `research/01-dependency-resolution/golang-dep-lessons.md`, Candidate 2.  
**Evidência atual:** `internal/source/manager.go`, `internal/source/identity.go`, `internal/ghrelease/resolver.go`, `internal/git`, `internal/ecosystem`.

---

## SIM-05 — Eliminar mutação global do registry depois do bootstrap

**Decisão:** incluir, incrementalmente. **Prioridade:** média.

### Problema atual

`internal/exec.Registry` já é injetável e cada `Executor` tira um snapshot do registry padrão. Porém, a API global ainda permite `exec.Replace()` depois do bootstrap.

Há usos reais dessa mutação global, por exemplo:

- reconfiguração do helper AUR;
- substituição do native adapter em remove/undo depois de detectar o host.

O resultado é um lifecycle parcialmente explícito e parcialmente temporal: o comportamento depende de quando o `Executor` foi construído em relação ao `Replace()`.

### Proposta simplificada

Migrar esses poucos pontos para construção explícita antes do uso:

```text
resolver fatos/configuração
    ↓
construir conjunto efetivo de adapters
    ↓
construir Executor
    ↓
registry default não muda mais
```

Depois que os call sites de `Replace()` forem removidos, selar conceitualmente o registry padrão após `InitAdapters()` e manter overrides somente por instância (`WithAdapters` / registry injetado).

### Por que simplifica

- remove comportamento dependente de ordem temporal global;
- torna testes mais isolados;
- deixa o composition root determinístico;
- evita que novos comandos aprendam o padrão “mude o singleton e torça para ninguém já ter tirado snapshot”.

### Critério de parada

Não criar um container de dependency injection nem uma camada de lifecycle para registry. Basta remover mutabilidade pós-bootstrap e continuar com construção explícita.

**Fonte de pesquisa:** `research/08-general-patterns/otp-supervision-patterns.md`, Proposal 4.  
**Evidência atual:** `internal/exec/registry.go`, `internal/app/bootstrap.go`, `internal/app/remove.go`, `internal/app/undo.go`, `internal/ecosystem/registry.go`.

---

## SIM-06 — Fazer `methodkind.Contract` ser a autoridade de metadata declarativa/schema-semantic dos métodos

**Decisão:** incluir como trabalho de consolidação. **Prioridade:** média.

### Problema atual

O projeto já possui uma estrutura declarativa forte em `internal/methodkind.Contract`: kind, aliases, fields, capabilities, scopes, environment, artifact/checksum contracts, package metadata e regras de validação.

Ao mesmo tempo, partes dessa metadata declarativa ainda aparecem naturalmente em outros lugares: bootstrap explícito, implementação do adapter, documentação e testes. A pesquisa sugere criar um novo `AdapterSpec`, mas no estado atual isso corre o risco de virar uma segunda autoridade sobre informações que `methodkind.Contract` já representa.

Nem toda informação sobre um adapter pertence a esse contrato. Construção concreta, dependências runtime, integração com processos externos e detalhes operacionais continuam sendo responsabilidade de outras camadas.

### Proposta simplificada

Não adicionar uma camada `AdapterSpec` agora. Em vez disso:

1. inventariar metadata **declarativa/schema-semantic** duplicada fora de `internal/methodkind`;
2. quando a informação já existir em `Contract`, fazer consumidores derivarem dela;
3. manter em `bootstrap.go` apenas composição/construção concreta de adapters;
4. manter no adapter comportamento runtime e metadata operacional que não pertence ao schema/capability contract;
5. reforçar testes que provem que contratos e adapters registrados não divergem nas informações que realmente compartilham.

### Por que simplifica

- reduz número de lugares editados ao adicionar/manter um método;
- evita uma nova abstração paralela;
- deixa claro qual tipo de metadata possui uma fonte de verdade central;
- usa infraestrutura que já existe e já tem contract tests;
- preserva o bootstrap explícito, que é simples de auditar.

### Critério de parada

Se uma informação não é declarativa ou não pertence ao schema/capability contract, não forçar sua entrada em `methodkind.Contract`. “Uma fonte de verdade” significa uma autoridade por conceito, não “um arquivo para tudo”.

**Fonte de pesquisa:** `research/08-general-patterns/otp-supervision-patterns.md`, Proposal 3, aplicada de forma mais estreita ao código atual.  
**Evidência atual:** `internal/methodkind`, `internal/app/bootstrap.go`, `internal/exec/conformance_test.go`.

---

## SIM-07 — Criar um contrato curto para autoria de adapters

**Decisão:** incluir. **Prioridade:** média e barata.

### Problema atual

O número de adapters e contratos já é grande o bastante para que a qualidade de manutenção dependa de regras que hoje estão distribuídas entre arquitetura, support boundary, conformance tests e implementações existentes.

O problema não pede mais abstrações. Pede uma checklist autoritativa.

### Proposta simplificada

Criar `docs/adapter-authoring.md` com perguntas/invariantes, não um tutorial longo.

Cada adapter deve declarar claramente:

- **Discovery:** como `Available()` funciona e se é read-only;
- **Observation:** quando o resultado é present/absent/unknown/broken;
- **Resolution:** quais campos chegam ao `ResolvedInstallPlan` e quais dependem de host/rede;
- **Execution:** quais operações mutam, exigem elevation ou arbitrary code;
- **Idempotência:** o que pode ser repetido com segurança;
- **Parsing:** JSON vs output humano, locale e exit codes;
- **Security:** secrets em argv/URL/output e gates necessários;
- **Removal:** o que prova ownership suficiente para remover.

Cada seção deve apontar para contract tests e tipos existentes em vez de inventar um novo mecanismo.

### Por que simplifica

- reduz tempo para revisar adapters novos;
- impede que cada implementação redescubra invariantes de segurança e lifecycle;
- melhora consistência quando a quantidade de métodos cresce;
- concentra conhecimento de manutenção sem aumentar o runtime surface.

### Critério de parada

Não duplicar `docs/schema-reference.md` nem listar configuração de cada manager. Este documento é sobre invariantes de implementação.

**Fonte de pesquisa:** `research/08-general-patterns/topgrade-workflow-patterns.md`, FR-07.  
**Evidência atual:** `internal/exec/adapter.go`, `internal/methodkind`, `internal/exec/conformance_test.go`, `docs/architecture.md`.

---

## SIM-08 — Expandir documentation-as-contract somente dentro da infraestrutura de testes existente

**Decisão:** incluir com escopo pequeno. **Prioridade:** baixa-média.

### Problema atual

O repositório já possui bons contract tests, inclusive para documentação/CLI e contratos públicos. Ainda assim, exemplos TOML e snippets podem divergir do parser/schema se não forem exercitados.

Criar outra suíte seria exatamente o tipo de solução que deixa o mantenedor com duas máquinas para testar a mesma coisa, um clássico hobby da nossa espécie.

### Proposta simplificada

Estender os testes já existentes para validar apenas exemplos autoritativos e marcados explicitamente, por exemplo:

- `README.md`;
- `docs/schema-reference.md`;
- `docs/cheatsheet.md`;
- `manifest.example.toml`;
- `schema.example.toml`.

Snippets parciais devem continuar não executáveis; usar marcação explícita para blocos que fazem parte do contrato.

### Por que simplifica

- reduz drift docs/código;
- dá feedback imediato a contribuidores;
- reutiliza a suíte existente;
- diminui suporte causado por exemplos que já não correspondem ao parser.

### Critério de parada

Não transformar Markdown em linguagem de teste nem extrair automaticamente todo bloco cercado por ```toml```. Só exemplos declarados como contract fixtures devem ser testados.

**Fonte de pesquisa:** `research/08-general-patterns/otp-supervision-patterns.md`, Proposal 5.  
**Evidência atual:** `internal/app/cli_docs_test.go`, `internal/methodkind/public_docs_test.go`, `contract_test.go`, exemplos TOML do repositório.

---

## SIM-09 — Preservar um único módulo Go; escalar por boundaries internos, não por fragmentação do repositório

**Decisão:** registrar como restrição de manutenção. **Prioridade:** contínua.

### Proposta simplificada

Manter um único `go.mod` enquanto não existir uma necessidade concreta de versionamento/release independente entre componentes.

A estrutura atual já possui boundaries em `internal/*`; usar esses boundaries para controlar dependências é mais simples do que introduzir múltiplos módulos, versões internas e pipelines de release.

### Por que simplifica

- uma árvore de dependências interna;
- uma versão/release do produto;
- uma configuração de toolchain;
- refactors cross-package continuam atômicos;
- CI e tooling permanecem simples.

### Critério de mudança

Só reabrir a decisão se houver pelo menos um componente com consumidor, versão e ciclo de release realmente independentes. “O diretório ficou grande” não é motivo suficiente.

**Fonte de pesquisa:** `research/08-general-patterns/otp-supervision-patterns.md`, rejeição explícita de umbrella-style repository split.  
**Evidência atual:** `go.mod`, boundaries descritos em `docs/architecture.md`.

---

## SIM-10 — Manter `AdapterV2` uniforme e extrair interfaces novas apenas quando a duplicação estiver provada

**Decisão:** registrar como guardrail de arquitetura. **Prioridade:** contínua.

### Proposta simplificada

Preservar `AdapterV2` como caminho uniforme de resolução/observação/execução/remoção/availability/compatibility. Não decompor o contrato em uma coleção de interfaces opcionais apenas para ganhar “flexibilidade”.

Da mesma forma, não criar `FetchManager`, resolver genérico ou provider framework antecipadamente. Quando duas ou mais implementações tiverem duplicação concreta e semanticamente igual, extrair a menor interface possível ao redor daquela operação.

### Por que simplifica

- o executor continua com um fluxo previsível;
- dry-run, why/status e install continuam compartilhando a mesma resolução;
- adapters não exigem capability negotiation espalhada pelo executor;
- abstrações nascem de duplicação real, não de cenários hipotéticos.

### Critério de mudança

Uma interface nova precisa remover duplicação ou resolver um problema de correctness demonstrável. “Pode ser útil no futuro” não é critério suficiente.

**Fontes de pesquisa:** `research/08-general-patterns/otp-supervision-patterns.md` e `research/05-artifacts-security/content-addressed-artifacts.md` (extração estreita somente quando duplicação for demonstrada).  
**Evidência atual:** `internal/exec/adapter.go`, `internal/planner`, `internal/plan`.

---

# Ordem recomendada

A sequência abaixo maximiza redução de ambiguidade antes de tocar em código amplo:

1. **SIM-02** — state model + matriz de invariantes.
2. **SIM-03** — failure domains no mesmo contrato + testes focados.
3. **SIM-07** — adapter authoring contract.
4. **SIM-05** — remover `exec.Replace()` pós-bootstrap e estabilizar o registry lifecycle.
5. **SIM-01** — evoluir `runContext` para a fronteira única de estado por execução, em um primeiro corte pequeno.
6. **SIM-04** — consolidar canonicalização de source identity somente onde houver equivalência semântica comprovada.
7. **SIM-06** — eliminar metadata declarativa/schema-semantic duplicada em favor de `methodkind.Contract`.
8. **SIM-08** — ampliar documentation contract tests apenas onde houver exemplos autoritativos.

SIM-09 e SIM-10 são restrições contínuas de design, não tarefas que precisam virar uma refatoração por si mesmas.

# Resultado esperado

Se estas propostas forem aplicadas sem ampliar o escopo, o Depengine tende a ficar com:

- menos estado implícito dentro de `Executor`;
- menos dependência de mutação global e ordem de inicialização;
- uma definição clara de state/failure invariants para todos os comandos;
- menos parsing/canonicalização duplicados entre adapters;
- uma autoridade clara para metadata declarativa/schema-semantic de métodos;
- uma checklist clara para escalar o número de adapters;
- documentação mais difícil de deixar obsoleta;
- boundaries internos fortes sem transformar o projeto em uma coleção de frameworks internos.

O ponto comum é deliberadamente pouco glamouroso: **consolidar o que já existe antes de adicionar novas camadas**. Para software de infraestrutura, isso costuma envelhecer bem melhor do que a alternativa.
