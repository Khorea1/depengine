---
completed: 2026-09-30
---

______________________________________________________________________


# Depengine — Plano de execução do `Research-Log-Implementation.md`

> Baseline analisado: `cd64e1193eeccff609b4c8fb77ef1d082e9beb2b` (`cd64e11`, `test: harden verification and preparation invariants (#97)`).\
> Documento de origem: `.dev/Research-Log-Implementation.md` (~linhas 1–508).\
> Escopo deste plano: implementar SIM-01 … SIM-08 de forma incremental, mantendo SIM-09 e SIM-10 como guardrails obrigatórios em todas as fases.\
> As referências de linha abaixo são deliberadamente aproximadas e correspondem ao baseline acima. Elas irão deslocar conforme os patches forem aplicados.

______________________________________________________________________

## 1. Objetivo operacional

O objetivo não é “implementar dez ideias” como dez mudanças independentes. O objetivo é reduzir fontes reais de complexidade do Depengine sem alterar desnecessariamente seu comportamento externo, usando a ordem recomendada no próprio research log (`.dev/Research-Log-Implementation.md:~480-493`) e transformando invariantes já existentes em fronteiras mais explícitas.

O resultado final deve produzir, de forma verificável:

1. uma definição normativa curta para os estados e suas transições;
1. um contrato explícito para failure domains;
1. uma checklist operacional para autoria de adapters;
1. registry global estável depois do bootstrap, com overrides por instância;
1. `Executor` contendo configuração estável, não estado mutável de uma execução;
1. canonicalização de identidade compartilhada **somente** onde houver equivalência semântica comprovada;
1. `methodkind.Contract` como fonte de verdade da metadata que realmente pertence ao contrato declarativo/schema-semantic;
1. exemplos autoritativos da documentação validados pela infraestrutura de testes já existente;
1. nenhuma fragmentação do módulo Go e nenhuma decomposição especulativa de `AdapterV2`.

### 1.1. Não objetivos

Este plano **não** autoriza:

- nova arquitetura de scheduler, actors, workers persistentes ou event bus;
- container de DI ou framework de providers;
- `FetchManager`, “universal resolver”, “provider framework” ou camada genérica de transporte;
- split do repositório em vários módulos Go;
- decomposição de `AdapterV2` em uma nuvem de interfaces opcionais;
- mudança de schema, lock format, manifest format ou semântica externa para facilitar o refactor;
- reescrita ampla de `internal/exec` antes de os invariantes estarem documentados e cobertos;
- transformar `.dev/` em fonte normativa de produto. O plano mora em `.dev/`, mas os contratos duráveis devem ser promovidos para `docs/`.

Esses limites vêm diretamente de SIM-09/SIM-10 (`.dev/Research-Log-Implementation.md:~427-476`) e dos contratos de projeto em `AGENTS.md:~17-26`.

______________________________________________________________________

## 2. Restrições e fontes de verdade a preservar

Antes de qualquer patch, toda revisão deve manter os seguintes contratos:

| Contrato | Fonte principal | Consequência para este plano |
| -------------------------------------------------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------- |
| Schema/manifest e precedência | `AGENTS.md:~7-15`, `docs/schema-reference.md` | Refactors não podem mudar parsing ou merge como efeito colateral. |
| Parse → Graph → Execute | `AGENTS.md:~22-24`, `docs/architecture.md:~88-101` | State model e session refactor não podem misturar parsing/configuração com execução. |
| Execução atrás de `Executor` + adapters | `AGENTS.md:~24-25` | Overrides de AUR/native devem entrar por composição do executor, não por comandos ad hoc na CLI. |
| Todo subprocesso via `run.Runner` | `AGENTS.md:~25`, `docs/architecture.md:~68-69` | Nenhuma simplificação pode introduzir chamadas diretas a subprocessos. |
| `internal/state` é autoridade do estado persistido | `AGENTS.md:~26` | O state model deve apontar para o package, não reimplementar suas regras em prose duplicada. |
| Lock boundary | `docs/support-boundary.md:~22-135`, ADR-001 | SIM-02 deve referenciar a cobertura real do lock, sem prometer reprodutibilidade além do que existe. |
| Reconciliation states | `internal/plan/verification.go:~16-203` | `satisfied/absent/drifted/unknown/broken` são vocabulário canônico. |
| Transactional preparation | ADR-002, `internal/state/preparation.go`, `internal/plan/preparation.go` | Failure domain deve respeitar WAL, recovery e ownership atuais. |
| Resolved-plan boundary | ADR-005, `internal/plan/resolution.go:~9-50` | Resolução pode enriquecer identidade, não reescrever intenção estável. |

### 2.1. Validação canônica

Conforme `AGENTS.md:~28-46`, cada PR deve terminar, no mínimo, com:

```sh
go build -o depengine .
go test -race ./...
go vet ./...
golangci-lint run
```

Durante desenvolvimento, usar loops focados por package. Rodar integração em containers apenas se o patch alterar comportamento real de instalação (`AGENTS.md:~46`). A maior parte deste plano é documentação, composição e refactor interno, portanto suites de integração não devem ser um ritual automático sem evidência de necessidade.

Também preservar a regra local de formatar **somente arquivos tocados**, porque há arquivos preexistentes que `gofmt -l .` denuncia fora do escopo (`AGENTS.local.md`, “Project Findings”).

______________________________________________________________________

## 3. Estado atual observado no baseline

O research log enumera dez itens, mas operacionalmente há oito entregas e dois guardrails:

- SIM-01: refactor de estado por execução;
- SIM-02: documentação normativa de state model;
- SIM-03: failure domains + testes;
- SIM-04: canonicalização de source identity, com gate de equivalência;
- SIM-05: registry imutável após bootstrap;
- SIM-06: `methodkind.Contract` como autoridade declarativa;
- SIM-07: contrato de autoria de adapters;
- SIM-08: documentation-as-contract incremental;
- SIM-09: guardrail de módulo único;
- SIM-10: guardrail de `AdapterV2` uniforme.

### 3.1. Dependências reais entre os itens

A ordem proposta no research log é correta, mas merece tornar as dependências explícitas:

```text
SIM-02 state model
   ↓
SIM-03 failure domain
   ↓
SIM-07 adapter authoring
   ↓
SIM-05 registry lifecycle
   ↓
SIM-01 run-scoped state
   ↓
SIM-04 source identity gate
   ↓
SIM-06 method metadata authority
   ↓
SIM-08 documentation contracts

SIM-09 + SIM-10: guardrails sobre TODAS as fases
```

Razões:

- SIM-01 move estado dentro de `internal/exec`; fazê-lo antes de nomear as invariantes aumenta o risco de preservar acidentalmente detalhes errados.
- SIM-05 deve preceder SIM-01 porque retira uma forma de mutabilidade process-wide antes de reorganizar mutabilidade run-scoped.
- SIM-04 e SIM-06 são consolidações e devem ocorrer depois que lifecycle/state boundaries estiverem claros.
- SIM-08 deve validar documentação **já estabilizada**, não ficar perseguindo textos que ainda estão mudando nas fases anteriores.

______________________________________________________________________

# FASE 0 — Baseline, inventário e gates de mudança

## 4. Objetivo

Criar uma linha de base reproduzível antes dos patches, sem alterar runtime.

## 4.1. Arquivos a consultar

- `.dev/Research-Log-Implementation.md:~1-508`
- `AGENTS.md:~7-46`
- `AGENTS.local.md` — Operating Principles, Validation e Project Findings
- `.dev/architecture/index.yaml`
- `.dev/architecture/README.md`
- `.dev/architecture/units/{cli,reconciliation,planning,state-and-lock,adapters}.yaml`
- `.dev/architecture/flows/{install,desired-state-observation,update}.yaml`

O archmap do baseline está verificado contra revisão anterior (`8d9d2cc`), portanto deve ser usado como mapa de navegação, não como substituto da árvore atual. As alegações consequenciais deste plano foram cruzadas com o código atual.

## 4.2. Passos

1. Registrar o commit baseline e garantir árvore de código limpa, ignorando os diretórios `docs/research/*` já não rastreados no arquivo fornecido.
1. Rodar a suíte canônica antes do primeiro patch quando o toolchain estiver materializado.
1. Registrar falhas preexistentes, se houver, sem “consertá-las por tabela”.
1. Para cada fase, usar primeiro teste focado do package e só depois a suíte completa.

## 4.3. Gate de entrada para todo PR

Antes de começar qualquer fase, responder internamente:

1. Este patch reduz uma fonte de estado/verdade duplicada existente?
1. O patch preserva comportamento externo ou documenta explicitamente uma mudança de contrato?
1. Há um lugar já existente onde a mudança cabe?
1. Estou criando uma abstração porque há **dois consumidores semanticamente equivalentes**, ou porque ela parece elegante?
1. O patch pode ser revertido isoladamente sem exigir desmontar quatro outros subsistemas?

Se a resposta para 3 ou 4 for ruim, o patch provavelmente virou aquilo que o research log pediu para evitar.

______________________________________________________________________

# FASE 1 — SIM-02: formalizar um único modelo de estado e invariantes

## 5. Objetivo

Criar `docs/design/state-model.md` como referência normativa curta para os estados e fronteiras já implementados. A proposta está em `.dev/Research-Log-Implementation.md:~96-149`.

A intenção é **nomear** as invariantes existentes, não criar um novo subsystem de state machine.

## 5.1. Arquivos principais e linhas aproximadas

### Documentação que já define partes do contrato

- `docs/architecture.md:~88-117`
  - fluxo de install;
  - desired-state observation;
  - distinções de reconciliation.
- `docs/support-boundary.md:~151-172`
  - `unknown`/`broken` e fail-closed de operações destrutivas;
  - package source ownership e origem Brew/Scoop.
- `docs/design/adr-001-universal-lock-projection.md:~1-55`
  - lock como projeção imutável do resolved plan.
- `docs/design/adr-002-transactional-preparation.md:~1-70`
  - WAL, recovery, ownership e “não adivinhar” resultado de mutação.
- `docs/design/adr-005-resolved-install-plan-projection.md:~1-55`
  - resolved plan como projeção compartilhada por install/status/remove/upgrade/why.

### Código que representa as fronteiras

- `internal/planner/intent.go:~1-95`
  - `BuildCandidateIntent` e `BuildValidatedCandidateIntent`.
- `internal/plan/resolution.go:~9-52`
  - `ValidateResolution`: resolução pode enriquecer version/revision/digest/source/artifacts, mas não reescrever intenção estável.
- `internal/plan/verification.go:~16-203`
  - `VerificationState`, `PresenceState`, `Observation`, `VerificationResult`, `Reconcile`.
- `internal/plan/lock.go:~787-1040`
  - `LockDocument`, `BuildLockDocument`, `VerifyCoverage`.
- `internal/lock/lock.go:~77-150`
  - estrutura persistida do lock e validação de versões.
- `internal/lock/universal.go:~9-39`
  - lock v2 e `ProjectionDocument`.
- `internal/state/state.go:~31-70, ~106-238`
  - `State`, `ToolState`, save/load sob locking.
- `internal/state/preparation.go:~11-218`
  - write-ahead boundaries de preparation/commit.
- `internal/plan/preparation.go:~772-826, ~1206-1251`
  - decisões de recovery, commit e finalização.

## 5.2. Estrutura proposta de `docs/design/state-model.md`

O documento deve ser curto o suficiente para ser lido numa review, mas preciso o suficiente para impedir interpretações incompatíveis. Estrutura recomendada:

### 5.2.1. Propósito e escopo

Definir logo no início:

- este documento descreve **fronteiras de autoridade e invariantes**;
- não duplica formatos completos de schema/lock/state;
- detalhes de formato continuam nas referências existentes;
- ausência de observação não equivale a ausência de alvo.

### 5.2.2. Pipeline canônico de estado

Usar exatamente a cadeia proposta pelo research log:

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
recorded ownership / installed state
```

Acrescentar uma nota importante: isso é uma **ordem conceitual de autoridade**, não significa que todos os comandos materializam todas as etapas. `status`, `why`, `remove` e `update` usam subsets diferentes.

### 5.2.3. Matriz normativa

Tabela recomendada:

| Fronteira | Invariante | Leitores | Mutadores/produtores | Se quebrar |
| ------------------------ | ------------------------------------------------------------------------------- | ------------------------------------ | ------------------------------------------------------- | ----------------------------------------------------------------- |
| Declared intent | representa intenção normalizada sem probes/mutação de host | planner, executor, explain | config/planner | erro antes de resolução |
| Resolved plan | preserva intent e só enriquece dimensões permitidas | verify/install/lock/report | adapter `ResolvePlan` validado por `ValidateResolution` | rejeitar candidato |
| Lock projection | contém identidade imutável e cobertura exigida para o escopo suportado | install/upgrade/update | projeção/build do lock | frozen/update fail-closed |
| Preparation | mutação possui WAL persistido antes do host; resultado ambíguo não é inferido | recovery/executor | state transaction APIs | bloquear nova mutação/recovery |
| Observation | read-only e descreve presença + identidade conhecida | reconciliation/status/remove/upgrade | adapter `Observe` | converter erro/inconsistência em broken/unknown conforme contrato |
| Reconciled verification | distingue satisfied/absent/drifted/unknown/broken | executor, destructive flows | `plan.Reconcile` | operação segue política explícita por estado |
| Recorded state/ownership | só registra commit/recovery com evidência suficiente; ownership governa cleanup | remove/status/sbom/diff/recovery | state package sob lock | falha de persistência torna run incompleto |

### 5.2.4. Semântica dos cinco estados

Usar `internal/plan/verification.go:~16-203` como autoridade:

- `satisfied`: observação suficiente confirma identidade desejada;
- `absent`: ausência é observada de forma autoritativa;
- `drifted`: alvo existe, mas campo conhecido diverge;
- `unknown`: faltam observações autoritativas para decidir identidade/presença;
- `broken`: observação/verificação é inválida ou falhou de forma que impede reconciliação segura.

Não escrever `unknown == absent`. `docs/support-boundary.md:~151-155` já proíbe essa interpretação em operações destrutivas.

### 5.2.5. Autoridade e precedência

Adicionar uma regra curta:

1. schema/manifest declara intenção;
1. resolver pode enriquecer, não redefinir intenção;
1. lock pode restringir a identidade concreta onde há cobertura;
1. observation descreve o host, não redefine desired state;
1. state persistido registra o que depengine conseguiu provar/possuir, não “a verdade absoluta do sistema”.

Esse parágrafo evita boa parte dos futuros bugs de “qual desses objetos ganha?”.

### 5.2.6. Ausência, drift e ambiguidade

Documentar explicitamente:

- ausência comprovada pode permitir release de ownership conforme fluxo de remove;
- drift não deve ser reclassificado como ausência;
- unknown/broken bloqueiam transições destrutivas;
- commit externo ambíguo não é repetido só porque o processo reiniciou.

### 5.2.7. Links em vez de cópia

O novo documento deve apontar para:

- ADR-001 para lock projection;
- ADR-002 para transactional preparation/ownership;
- ADR-005 para resolved plan;
- `docs/support-boundary.md` para limites de suporte;
- `docs/schema-reference.md` para declaração de usuário.

Não copiar tabelas inteiras desses documentos.

## 5.3. Alterações adicionais de docs

### `docs/architecture.md:~103-117`

Adicionar uma frase/link dizendo que a semântica normativa de state/reconciliation vive em `docs/design/state-model.md`.

### `docs/support-boundary.md:~151-155`

Opcionalmente adicionar link cruzado para o state model. Não reescrever a seção.

## 5.4. Pseudocódigo em linguagem natural

> Ao processar uma declaração, primeiro construa intenção estática sem tocar no host. Quando um candidato for alcançado, deixe o adapter resolver apenas os campos concretos que ele tem autoridade para enriquecer. Valide que essa resolução não alterou intenção estável. Observe o alvo de forma read-only. Reconcile observação e identidade desejada em um dos cinco estados. Só então escolha uma transição. Se uma mutação preparatória for necessária, persista a intenção de mutar antes de executar o comando. Após mutação, só registre ownership/estado quando houver evidência suficiente de commit. Em qualquer ponto de ambiguidade que possa levar a replay destrutivo, falhe fechado.

## 5.5. Critérios de aceitação

- [ ] `docs/design/state-model.md` existe e tem uma única matriz de invariantes.
- [ ] Os cinco estados estão definidos sem contradizer `plan.Reconcile`.
- [ ] Lock, preparation e ownership são referenciados, não duplicados.
- [ ] `docs/architecture.md` aponta para o documento normativo.
- [ ] Nenhuma linha de runtime é necessária nesta fase.
- [ ] `go test -race ./...` continua verde, mesmo que o diff seja só docs, para manter baseline de integração da árvore.

## 5.6. Commit sugerido

`docs: formalize state model and invariants`

Um commit único é adequado porque a mudança é documental e independente.

______________________________________________________________________

# FASE 2 — SIM-03: tornar failure domains um contrato explícito

## 6. Objetivo

Transformar as regras de propagação de falha já implícitas em `internal/exec` e no recovery em um contrato explícito e coberto por testes focados. Research log: `.dev/Research-Log-Implementation.md:~153-192`.

O contrato é:

1. o domínio de falha de X é a closure de dependentes de X;
1. siblings independentes continuam;
1. descendentes dependentes são bloqueados;
1. branches não relacionados continuam;
1. mutações não são replayadas sem evidência durável;
1. estado externo ambíguo permanece fail-closed.

## 6.1. Código existente que já implementa partes da regra

- `internal/exec/run.go:~19-29`
  - `runContext` mantém `failed` por execução.
- `internal/exec/run.go:~165-209`
  - `runLevel` exclui recovered commits e `blockFailedRequires` bloqueia dependentes.
- `internal/exec/run.go:~335-369`
  - execução de remanescentes e registro de falhas do nível.
- `internal/exec/execute.go:~300-376`
  - lazy `method.requires`, deduplicação de dependency run e bloqueio recursivo.
- `internal/exec/executor_test.go:~1241-1283`
  - teste existente de dependente bloqueado por requisito gated.
- `internal/exec/executor_test.go:~920-958`
  - ambiguity em source preparation interrompe fallback.
- `internal/exec/preparation_test.go:~213-225`
  - unresolved commit em run posterior falha fechado antes de nova host mutation.
- `internal/exec/preparation_test.go:~846-866`
  - recovery drifted fica fail-closed.
- `internal/exec/preparation_test.go:~914-930`
  - recovered commit não replaya host commands.
- `internal/exec/preparation_test.go:~933-1040`
  - recovered dependency-only commit não é replayado lazy.

## 6.2. Mudança documental

Adicionar ao `docs/design/state-model.md` uma seção “Failure domains”. Não criar outro documento para uma regra que cabe naturalmente na mesma semântica de execução/recovery.

Conteúdo mínimo:

```text
Failure(X) bloqueia apenas operações cuja correção depende de X.
Dependências declaradas e lazy prerequisites propagam falha para os descendentes.
Siblings e branches independentes não herdam a falha.
Um resultado ambíguo de mutação não autoriza replay automático.
Recovery confirmado pode satisfazer a dependência sem reexecutar a mutação.
```

## 6.3. Novo teste principal: sibling continuation

### Local recomendado

Preferência: `internal/exec/failure_domain_test.go`.

Motivo: não é nova abstração runtime; é agrupamento de um contrato transversal. Evita esconder o teste normativo no já muito grande `executor_test.go`.

### Caso de teste

Construir três ferramentas:

- `root-fail`: candidato que termina em falha controlada;
- `child`: `requires = ["root-fail"]`;
- `sibling-ok`: independente e instalável por adapter fake.

Assertar:

- `root-fail` termina Failed;
- `child` não chama adapter de install e termina Failed com razão `requires failed dependency`;
- `sibling-ok` ainda é executado e termina Installed/Already conforme fixture;
- `report.Failed` inclui root + child, mas não o sibling;
- nenhum abort global encerra o nível/graph prematuramente.

### Variação de concorrência

Rodar o mesmo contrato, se o fake adapter for deterministicamente seguro, em tabela com:

- `maxJobs = 1`;
- `maxJobs = 2`.

A semântica de failure domain não deve depender da escolha serial/paralela. Se isso tornar o teste frágil por ordem de scheduling, manter o teste normativo serial e adicionar apenas uma cobertura paralela independente, sem afirmar ordem de eventos.

## 6.4. Reusar em vez de duplicar recovery tests

Não criar mais quatro testes de “unknown/drifted/recovered”. Os existentes já cobrem isso. Nesta fase:

1. renomear comentários ou nomes **somente se** isso tornar o contrato mais explícito;
1. adicionar assertions faltantes apenas quando houver uma lacuna concreta;
1. manter os testes de preparation como evidência do failure domain de recovery.

Uma revisão útil é garantir que o teste de ambiguity demonstre explicitamente que o fallback não é invocado (`internal/exec/executor_test.go:~951-957` já faz isso).

## 6.5. Pseudocódigo em linguagem natural

> Ao terminar um nível, registre quais ferramentas não produziram um resultado utilizável. Antes de executar o próximo nível, examine apenas os `requires` efetivos de cada ferramenta. Se algum requisito estiver no conjunto de falhas, marque essa ferramenta como bloqueada e não execute seu candidato. Não marque outros nós do nível como falhos apenas porque compartilham o mesmo nível. Para lazy dependencies, deduplique a execução por nome e compartilhe o resultado; se a dependência já foi recuperada como commit confirmado, consuma esse resultado terminal sem repetir host mutation. Se recovery não consegue provar se uma mutação aplicou, interrompa o caminho afetado antes de iniciar uma nova mutação que poderia duplicar efeitos.

## 6.6. Critérios de aceitação

- [ ] Failure domain documentado em `state-model.md`.
- [ ] Teste explícito prova sibling independente continuando.
- [ ] Teste explícito/proven existing prova dependent blocking.
- [ ] Recovery ambiguity permanece fail-closed.
- [ ] Recovered commit permanece “consume result, do not replay”.
- [ ] Nenhuma alteração de scheduler/worker architecture.

## 6.7. Validação focada

```sh
go test -race ./internal/exec/ -run 'FailureDomain|BlocksDependent|Preparation|Recovered'
go test -race ./internal/plan/ ./internal/state/
```

Depois, matriz completa.

## 6.8. Commit sugerido

`test: codify executor failure domains`

Se a doc da fase 1 já foi mergeada, este commit pode tocar apenas `state-model.md` + testes.

______________________________________________________________________

# FASE 3 — SIM-07: contrato curto de autoria de adapters

## 7. Objetivo

Criar `docs/adapter-authoring.md`, orientado a manutenção e review, não um tutorial de schema. Research log: `.dev/Research-Log-Implementation.md:~346-385`.

## 7.1. Fontes técnicas para a checklist

- `internal/exec/adapter.go:~39-95`
  - `AdapterV2` uniforme;
  - `ElevationRequirer` e `RemovalElevationRequirer` como hooks de suporte de sessão privilegiada, não capability negotiation de semântica.
- `internal/methodkind/methodkind.go:~58-77, ~226-229, ~425-470`
  - contrato declarativo, capabilities e autoridade central.
- `internal/methodkind/field_semantics.go:~3-93`
  - semântica e efeitos dos fields.
- `internal/planner/intent.go:~16-95`
  - criação/validação do candidate intent.
- `internal/plan/resolution.go:~9-52`
  - restrições de resolver.
- `internal/plan/verification.go:~84-203`
  - observation/reconcile.
- `internal/exec/attempt.go:~77-353`
  - gates, source preparation, hooks, prerequisites e transition selection.
- `internal/run/` + `AGENTS.md:~25`
  - subprocessos somente por `Runner`.
- `contract_test.go:~11-35`
  - conformance entre adapters registrados e method contracts.
- `internal/methodkind/public_docs_test.go:~14-94`
  - docs públicas alinhadas à vocabulary de contracts.

## 7.2. Estrutura recomendada de `docs/adapter-authoring.md`

### 7.2.1. “Antes de escrever código”

Checklist:

- O método já existe em `methodkind.Contract`?
- O novo comportamento pertence a um adapter existente ou é realmente um method kind novo?
- Quais capabilities declarativas são necessárias?
- Qual identidade o adapter consegue observar autoritativamente?
- Existe remoção segura? Se não, não fingir `CanRemove`.

### 7.2.2. Discovery

Regras:

- `Available()` é read-only;
- ausência de binary não deve mutar host;
- discovery não instala prereq silenciosamente;
- probes rodam via `run.Runner`.

### 7.2.3. Observation

- `Observe` não muta;
- distinguir `PresenceAbsent` de erro/unknown;
- preencher `KnownFields` apenas para campos realmente observados;
- `unknown` não é shortcut para “não instalado”.

### 7.2.4. Resolution

- candidate intent é construído antes do adapter;
- `ResolvePlan` só enriquece dimensões autorizadas;
- `ValidateResolution` deve continuar aprovando;
- resolver não muda tool, method, requested intent, hooks ou ensure actions.

### 7.2.5. Execution

- `InstallResolved` executa o plano resolvido recebido;
- não refazer lookup/resolution que possa escolher outro alvo;
- mutation runner deve ser usado;
- dry-run não pode escapar pela implementação do adapter.

### 7.2.6. Idempotência e recovery

- checks/probes repetíveis são read-only;
- mutações preparatórias entram no WAL existente quando aplicável;
- nunca repetir uma mutação de resultado ambíguo só por conveniência;
- recovered commit confirmado é terminal.

### 7.2.7. Parsing

- preferir output estruturado/JSON quando o CLI externo oferece;
- evitar dependência de locale/human output;
- tratar exit code como parte do contrato, não como detalhe incidental;
- sanitizar erros antes de reportar secrets.

### 7.2.8. Security

- segredo não deve entrar em state/lock/report;
- minimizar secret em argv/URL;
- respeitar redaction compartilhada;
- hooks/build/code arbitrário continuam atrás do gate existente.

### 7.2.9. Removal

- declarar capability apenas se remoção puder mirar o mesmo target observado/resolvido;
- `unknown`/`broken` não autorizam remoção destrutiva;
- ownership/refcount do state continua governando shared resources.

### 7.2.10. Definition of done de um adapter PR

- contract atualizado, se necessário;
- bootstrap explícito atualizado;
- adapter implementa `AdapterV2` completo;
- conformance tests passam;
- tests focados cobrem resolution/observe/install/remove relevantes;
- docs públicas de methods continuam alinhadas por testes existentes;
- nenhum `exec.Command` direto.

## 7.3. Link arquitetural

Atualizar `docs/architecture.md:~57-69` com um link curto para `docs/adapter-authoring.md`.

Não colocar a checklist em `AGENTS.md`; o arquivo já aponta para as fontes de verdade e deve continuar pequeno.

## 7.4. Critérios de aceitação

- [ ] Documento cabe como checklist de review, não como livro de schema.
- [ ] Cada seção aponta para tipos/funções existentes.
- [ ] Explica que `AdapterV2` permanece uniforme.
- [ ] Não introduz nova interface ou runtime code.

## 7.5. Commit sugerido

`docs: add adapter authoring contract`

______________________________________________________________________

# FASE 4 — SIM-05: eliminar mutação global do registry após bootstrap

## 8. Objetivo

Fazer o registry global servir apenas como **seed de composição no bootstrap**, enquanto configuração variável por schema/host entra via executor instance. Research log: `.dev/Research-Log-Implementation.md:~260-303`.

Hoje a infraestrutura já possui a direção correta:

- `exec.New()` snapshots `defaultRegistry` (`internal/exec/executor.go:~225-243`);
- `WithAdapters(...)` aplica overrides por instância (`internal/exec/executor.go:~181-196`);
- mas o package ainda expõe `exec.Replace` global (`internal/exec/registry.go:~121-127`);
- AUR e native ainda usam essa mutação em runtime.

## 8.1. Registry atual

### `internal/exec/registry.go:~8-127`

- `Registry` já é um conjunto injetável/local;
- `Registry.Replace` é mutação de uma instância do registry;
- `defaultRegistry` é o seed process-wide;
- package-level `Register`, `Lookup`, `RegisteredKinds`, `Replace` delegam ao global.

### Decisão deste plano

- manter `Registry.Replace` **somente** como operação de um registry explicitamente possuído/local, se os testes dele ainda justificarem;
- remover package-level `exec.Replace` ao final desta fase;
- manter package-level `Register` para o composition root enquanto `InitAdapters` é o bootstrap explícito;
- não adicionar flag `sealed bool`: remover a API de mutação pós-bootstrap é mais simples e mais forte que simular uma “trava conceitual” em runtime.

## 8.2. Composition root

- `internal/app/bootstrap.go:~16-36`
  - registra adapters uma vez.
- `internal/ecosystem/registry.go:~195-228`
  - `RegisterAll` registra AUR com helper bootstrap + aliases.

Após esta fase, comentários devem deixar claro:

> O registry global contém defaults/constructors de bootstrap. Cada `Executor` tira snapshot no `New`; fatos do host e defaults do schema que alterem um adapter são overrides explícitos da instância.

## 8.3. Call sites de AUR a migrar

### 8.3.1. Install

**Atual:**

- `internal/app/install.go:~356-368` chama `ecosystem.ReconfigureAUR`.
- `newInstallExecutor` em `internal/app/install.go:~201-235` já usa `WithAdapters` para git/http/container/native.

**Mudança:**

1. remover `ReconfigureAUR` de `runInstall`;
1. em `newInstallExecutor`, se `s.Defaults.AurHelper != ""`, acrescentar `ecosystem.NewAURAdapter(s.Defaults.AurHelper)` ao conjunto de overrides da instância;
1. manter aliases explícitos `paru` e `yay` intocados. `defaults.aur_helper` só deve substituir kind `aur`, não mudar o significado de um schema que escreveu explicitamente `yay`.

Pseudocódigo natural:

> Construa o executor a partir do snapshot global. Sobrescreva o adapter `native` com o clan detectado. Se o schema escolher um `aur_helper`, sobrescreva apenas o kind `aur` com um `AURAdapter` configurado para aquele helper. Os adapters nomeados `paru`/`yay` permanecem os aliases fixos registrados no bootstrap.

### 8.3.2. Update

**Atual:** `internal/app/update.go:~65-75` muta AUR global.

**Observação importante:** a projeção universal chama `newInstallExecutor` em `internal/app/universal_lock.go:~20-25`.

**Mudança:** remover a mutação de `runUpdate`. Ao ensinar `newInstallExecutor` a aplicar override de AUR pelo schema, o universal lock recebe a configuração correta naturalmente.

Esse é um caso onde apagar código é melhor que inventar propagação adicional.

### 8.3.3. Upgrade

**Atual:**

- `internal/app/upgrade.go:~151-183` constrói executor com override apenas do native;
- comentário em `~162-164` depende explicitamente de reconfiguração global;
- `runUpgrade` muta AUR global em `~564-566`.

**Mudança:**

1. remover `ReconfigureAUR` de `runUpgrade`;
1. em `buildUpgradeExecutor`, adicionar `ecosystem.NewAURAdapter(s.Defaults.AurHelper)` como override quando configurado;
1. reescrever comentário `~162-164` para refletir snapshot + instance overrides.

**Teste existente a reescrever:** `internal/app/upgrade_test.go:~848-872`.

Ele hoje prova que reconfiguração process-wide chega ao executor. O novo teste deve provar o contrário:

- um schema com `Defaults.AurHelper = "yay"` produz executor cujo AUR usa `yay`;
- outros adapters do registry continuam presentes;
- o native continua clan-specific.

Não compare ponteiro com `exec.Lookup("aur")`, porque o objetivo novo é justamente que sejam instâncias distintas quando há override.

### 8.3.4. Graph view

**Atual:** `internal/app/graph_view.go:~134-171`, com global AUR mutation em `~141-143`.

**Mudança:** após `exec.New()`, aplicar `WithAdapters(ecosystem.NewAURAdapter(helper))` se necessário.

Não criar um registry alternativo para graph. Ele só precisa de um override local para o mesmo executor que chama `ExplainTool`.

### 8.3.5. `why`

**Atual:** `internal/app/graph_why.go:~352-379`, global mutation em `~357-359`.

**Mudança:** aplicar AUR override no `ex` criado em `~376`.

Também aplicar `WithDefaultMethodOrder(s.Defaults.MethodOrder)` se esse fluxo já deve respeitar defaults de método e ainda não o faz. Isso deve ser verificado como comportamento existente antes de incluir no mesmo diff; não “corrigir” comportamento não relacionado escondido sob SIM-05.

## 8.4. Native adapter em remove/undo

### Remove

- `internal/app/remove.go:~68-104` constrói sessão/executor.
- `ensureRemoveNativeAdapter` em `~169-183` gather facts e chama `exec.Replace` global.

Refactor recomendado:

1. renomear a função para refletir o que realmente deve fazer, por exemplo `gatherRemoveFacts`;
1. ela apenas chama `engine.GatherFacts` e retorna facts/nil + warning existente;
1. `runRemove` cria `exec.New()`;
1. se houver facts:
   - resolve clan;
   - `WithFacts(facts)`;
   - `WithDefaultMethodOrder(...)`;
   - `WithAdapters(exec.NewNativeAdapter(clan))`;
1. se gather falhar, não há override e o default `native` bootstrap continua oferecendo o fallback antigo de PATH probing.

Pseudocódigo natural:

> Tente descobrir facts. Se conseguiu, construa o executor e sobrescreva `native` somente nessa instância com o clan conhecido. Se não conseguiu, mantenha exatamente o adapter native default que veio do snapshot e emita o warning já existente. Nunca altere o registry do processo.

### Undo

- `internal/app/undo.go:~128-135` constrói executor;
- `ensureUndoNativeAdapter` em `~231-249` chama global `exec.Replace`.

Aplicar o mesmo padrão do remove.

Não extrair um “HostAwareExecutorFactory” genérico. Se depois de install/upgrade/remove/undo houver uma função mínima já claramente repetida, avaliar separadamente; SIM-05 não precisa de factory framework.

## 8.5. Testes que hoje dependem de mutação global

### `internal/app/remove_command_inprocess_test.go:~12-88`

Os três testes chamam `exec.Replace(adapter)`.

Quando `exec.Replace` for removido, preservar teste de orchestration por **injeção por instância**, não por outro singleton de teste.

Refactor testável recomendado:

- separar construção do executor/sessão do corpo que executa removal;
- permitir ao teste fornecer um `*exec.Executor` já configurado com `WithAdapters(recordingRemoveAdapter)`;
- `runRemove` de produção continua construindo seu executor normalmente e chama o helper interno;
- o helper de teste cobre state load/target/removal/persistência sem host package manager.

Uma forma possível, sem fixar nomenclatura exata:

```text
runRemove(...)
  → gather facts
  → buildRemoveExecutor(facts)
  → runRemoveWithExecutor(..., executor, facts)

Teste:
  executor := exec.New()
  WithAdapters(recording adapter)(executor)
  runRemoveWithExecutor(..., executor, nil)
```

O importante é não substituir um global mutável por outro “test hook” global.

### `internal/app/upgrade_test.go:~848-872`

Reescrever para schema-local AUR override, como descrito acima.

### `internal/exec/registry_test.go:~83-100`

Esses testes exercitam `Registry.Replace` em instância local. Eles podem permanecer se a API local continuar útil. Remover apenas o teste da função package-level `Replace`, se existir.

## 8.6. Remoções finais

Após migrar **todos** os call sites:

1. remover `ecosystem.ReconfigureAUR` em `internal/ecosystem/registry.go:~230-239`;
1. remover package-level `exec.Replace` em `internal/exec/registry.go:~121-127`;
1. atualizar comentário de `Registry` em `internal/exec/registry.go:~8-16`, que hoje menciona `Register/Lookup/Replace/RegisteredKinds` globais;
1. atualizar `docs/architecture.md:~57-69` e `~119-130`:
   - bootstrap registra seed global uma vez;
   - executor snapshots no `New`;
   - configurações por schema/host entram via instance override.

## 8.7. Invariante a provar

Após `app.InitAdapters()`:

> Nenhum comando de produção altera o conjunto/default adapters do processo. Dois executors construídos para schemas/hosts diferentes podem coexistir sem que a configuração de um altere a do outro.

## 8.8. Teste de isolamento recomendado

Adicionar um teste pequeno em `internal/exec/registry_test.go` ou `internal/app`:

1. construir executor A com override AUR fake/config A;
1. construir executor B sem override ou com config B;
1. provar que lookup em A e B retorna suas próprias instâncias;
1. provar que `exec.Lookup("aur")`/novo executor C ainda vê o seed bootstrap, não A/B.

Esse teste vale mais que um `sealed bool` porque prova a propriedade que importa.

## 8.9. Critérios de aceitação

- [ ] `rg 'exec\.Replace|ReconfigureAUR' internal` retorna zero call sites de produção; idealmente zero ocorrências depois de remover as APIs.
- [ ] install/update/upgrade/graph/why respeitam `defaults.aur_helper` via instance override.
- [ ] remove/undo usam native clan via instance override.
- [ ] teste de remove não depende de registry global mutável.
- [ ] `defaultRegistry` é bootstrap seed, não runtime configuration bus.
- [ ] nenhum DI framework novo.

## 8.10. Validação focada

```sh
go test -race ./internal/exec/
go test -race ./internal/app/ -run 'Install|Update|Upgrade|Graph|Why|Remove|Undo|Registry|AUR'
go test -race ./internal/ecosystem/ -run 'AUR|Registry'
```

Depois suíte completa.

## 8.11. Estratégia de commits

Recomendado dividir em dois commits bisectáveis:

1. `refactor: configure runtime adapters per executor`
   - migrar AUR/native call sites + testes;
   - APIs globais ainda podem existir temporariamente sem callers.
1. `refactor: remove post-bootstrap registry mutation API`
   - apagar `ReconfigureAUR` e package-level `exec.Replace`;
   - docs/comments finais.

Assim um `git bisect` nunca cai num commit onde call sites apontam para uma API já removida.

______________________________________________________________________

# FASE 5 — SIM-01: separar estado de uma execução do `Executor`

## 9. Objetivo

Evoluir o `runContext` existente para ser a fronteira única de estado mutável de uma chamada a `Execute`, sem introduzir uma segunda hierarquia paralela. Research log: `.dev/Research-Log-Implementation.md:~24-92`.

O ponto crítico é fazer isso em cortes pequenos. `Executor` hoje mistura configuração estável com state de execução em `internal/exec/executor.go:~20-57`.

### Campos run-scoped atuais no `Executor`

- `schema` (`~50`)
- `report` (`~51`)
- `sources` (`~52`)
- `recoveredCommits` (`~54`)
- `dependencyMu` (`~55`)
- `dependencies` (`~56`)

### Campos que **não** devem ser movidos no primeiro corte

- `clan` (`~21`)
- `defaultMethodOrder` (`~37`)
- `nativeManagerName` (`~38`)
- `facts`, lock/schema metadata e opções estáveis.

Há uma nuance importante: `clan/nativeManagerName` são mutados por `SetHostContext` (`internal/exec/explain.go:~341-352`), então podem merecer revisão futura, mas movê-los agora ampliaria demasiadamente o diff. O próprio research log manda parar antes disso (`~68-76`).

## 9.1. Estado duplicado já existente

`internal/exec/run.go:~19-29` contém:

```text
runContext:
  ctx
  schema
  report
  failed
```

Porém `initializeRun` (`~34-54`) copia `schema/report` de volta para `Executor`, e `Execute` só cria `runContext` depois de recovery (`internal/exec/execute.go:~72-104`). Isso é a duplicação a remover primeiro.

______________________________________________________________________

## 9.2. Corte A — `schema`, `report`, `failed` como estado da sessão

### Objetivo

Remover `Executor.schema` e `Executor.report` sem ainda mover source manager, recovered commits ou dependency synchronization.

### Arquivos

- `internal/exec/executor.go:~20-57`
- `internal/exec/run.go:~19-100`
- `internal/exec/execute.go:~72-105, ~300-376`
- `internal/exec/preparation.go:~520-550`
- testes que constroem `runContext` diretamente em `internal/exec/batch_planning_test.go:~140-230`.

### Sequência

1. criar `runContext` **antes** de `initializeRun` em `Execute`;
1. `runContext` recebe `ctx`, `schema`, `report`, `failed` no começo da execução;
1. alterar `initializeRun` para receber `rc` em vez de `s` + `report` separados, ou ao menos parar de gravar esses dois campos em `Executor`;
1. `recoverAndRecord` recebe `rc` para acessar report;
1. `recoveryCandidate` recebe o schema/sessão explicitamente em vez de `ex.schema`;
1. `executeDependency` recebe `rc` e usa `rc.schema`/`rc.report`;
1. remover `schema` e `report` de `Executor`.

### Pseudocódigo natural

> No início de `Execute`, crie um objeto de sessão contendo o context da chamada, o schema imutável daquela chamada, o report que será preenchido e um mapa vazio de falhas. Passe essa mesma sessão para inicialização, recovery, level execution e lazy dependencies. Qualquer helper que precise do schema/report da execução recebe a sessão explicitamente. O `Executor` deixa de armazenar cópias desses objetos.

### Assinaturas aproximadas

Não é necessário fixar exatamente os nomes, mas a direção deve ficar semelhante a:

```text
Execute(ctx, schema, clan):
    report = novo report
    session = novo runContext(ctx, schema, report)
    initializeRun(housekeepingCtx, session, clan)
    recoverAndRecord(housekeepingCtx, session)
    ...
    runLevel(session, level)

executeDependency(session, name)
recoveryCandidate(session, key)
```

Evitar `context.WithValue` para esconder `runContext`. O objetivo é tornar dependência de state explícita, não trocar field global por state implícito no context.

### Teste importante

Adicionar teste de reuse do mesmo Executor:

1. `ex := New()`;
1. executar schema A que falha uma ferramenta;
1. executar schema B independente com o **mesmo** `ex`;
1. provar que report/failure/schema da primeira execução não afetam a segunda.

Esse teste deve existir antes de mover o restante, porque ele define a propriedade que SIM-01 pretende ganhar.

______________________________________________________________________

## 9.3. Corte B — `recoveredCommits`, `dependencies` e mutex

### Referências atuais

- `internal/exec/run.go:~38-39, ~90-100, ~170-188`
- `internal/exec/execute.go:~330-376`
- `internal/exec/preparation.go:~640-668`
- `internal/exec/preparation_test.go:~383-389, ~474-475, ~813-826`

### Mudança

Expandir `runContext`:

```text
runContext
  ctx
  schema
  report
  failed
  recoveredCommits
  dependencyMu
  dependencies
```

Inicializar esses maps por execução.

Alterar:

- `recoverAndRecord` para ler `rc.recoveredCommits`;
- `runLevel` para pular recovered commits via `rc`;
- `recoverPreparationTransaction` para registrar commit recuperado em `rc.recoveredCommits`;
- `executeDependency` para deduplicar via `rc.dependencies` e `rc.dependencyMu`;
- testes de preparation que hoje fazem `ex.recoveredCommits = make(...)` devem criar/receber uma sessão de teste.

### Pseudocódigo natural

> Cada execução possui seu próprio conjunto de commits recuperados e sua própria tabela de lazy dependencies em andamento. Quando duas goroutines da mesma execução solicitam a mesma lazy dependency, elas compartilham o `dependencyRun` daquela sessão. Uma execução posterior começa com tabela vazia, exceto pelos commits que seu próprio startup recovery confirmar a partir do state persistido.

### Atenção à concorrência

O mutex deve ficar junto do map que ele protege. Não mover `dependencies` para session e deixar o mutex no Executor, porque isso cria ownership dividido do estado concorrente.

______________________________________________________________________

## 9.4. Corte C — mover `sources` sem quebrar `ExplainTool`/lock projection

Este é o trecho que merece cuidado especial.

### Estado atual

- `Executor.sources` em `internal/exec/executor.go:~52`;
- inicializado por `initializeRun` (`internal/exec/run.go:~37`);
- usado extensivamente em `internal/exec/preparation.go:~129-425, ~699-758`;
- `ExplainTool` cria/reusa esse mesmo field em `internal/exec/explain.go:~190-205`;
- `Executor.SourceRevisions()` em `internal/exec/executor.go:~246-252` expõe as revisões capturadas;
- o único consumidor fora de `internal/exec` é `internal/app/universal_lock.go:~41-63`, que chama `ExplainTool` e depois `SourceRevisions()`.

Isto significa que `sources` hoje possui **dois lifecycles distintos**:

1. source manager de uma execução real/recovery;
1. source manager temporário de uma explicação read-only usada para construir lock projection.

Simplesmente mover o field para `runContext` e deixar `ExplainTool` sem solução seria regressão. Deixá-lo no Executor “só para ExplainTool” manteria mutabilidade residual e permitiria revisions de uma chamada vazarem para outra.

### Solução recomendada

#### Execução real

Adicionar `sources *source.Manager` ao `runContext` e fazer toda preparation/recovery receber a sessão ou source manager da sessão.

#### Explain path

Tornar source manager **call-scoped** em `ExplainTool`.

Implementação mínima possível:

1. extrair corpo interno para helper que recebe `*source.Manager` explicitamente;
1. `ExplainTool` cria manager read-only local (`source.NewManager(ex.rn, true)`) e retorna apenas attempts, como hoje;
1. adicionar uma variante estreita para o universal lock que retorna também a snapshot de revisions observadas nessa chamada;
1. migrar `resolveUniversalLockDocument` para usar essa variante;
1. remover `Executor.SourceRevisions()` e `Executor.sources`.

Nomes são secundários. Uma forma possível:

```text
ExplainTool(ctx, tool, clan):
    manager = new read-only source manager
    attempts = explainTool(ctx, tool, clan, manager)
    return attempts

ExplainToolWithSourceRevisions(ctx, tool, clan):
    manager = new read-only source manager
    attempts = explainTool(ctx, tool, clan, manager)
    return attempts, manager.SourceRevisions()
```

O método adicional é preferível a reestruturar todos os callers de `ExplainTool` para um novo result type só por causa de um consumidor.

### `universal_lock.go`

No loop por ferramenta (`internal/app/universal_lock.go:~36-64`):

> Explique a ferramenta com um source manager temporário; selecione o primeiro resolved candidate válido; aplique ao resolved plan somente as source revisions retornadas para aquela explicação; clone e adicione o plano ao documento.

Isso ainda preserva a captura de HEAD de Brew/Scoop, mas elimina state acumulado no Executor entre ferramentas/chamadas.

### Preparation path

Modificar helpers como:

- `probeCandidateSources` (`preparation.go:~129`);
- `prepareCandidateSources` (`~170`);
- `recoverPreparationTransaction` (`~699`);
- `sourcePreparationTransaction`, se necessário;

para obter `sources` da sessão/transaction explicitamente.

Uma opção limpa é o `candidateAttempt` carregar uma referência à sessão:

```text
candidateAttempt
  run *runContext
  toolCtx
  tool
  method
  ...
```

Então:

> Candidate phases consultam `ac.run.sources`; lazy prerequisites usam `ac.run` para chegar à mesma dependency table; o Executor continua contendo apenas configuração/serviços estáveis.

Evitar espalhar `*runContext` em adapters. A sessão é interna ao orchestration layer, não faz parte do `AdapterV2` contract.

______________________________________________________________________

## 9.5. Possível renome de `runContext`

Só depois dos três cortes, avaliar `runContext` → `executionSession`.

Critério:

- se o tipo agora contém lifecycle/state suficiente para “session” comunicar melhor seu papel, renomear em commit mecânico separado;
- se `runContext` continua claro e o rename gera centenas de linhas sem ganho, não fazer.

O research log explicitamente permite evoluir o tipo existente e não exige novo nome.

## 9.6. Campos que permanecem no Executor

Após SIM-01, o `Executor` deve continuar possuindo:

- runner e secret resolver;
- timeouts e flags;
- adapters instance-scoped;
- logger/output;
- method order configurado;
- facts/lock document/schema metadata necessários como configuração da operação;
- host context por enquanto (`clan/nativeManagerName`).

Uma futura revisão pode separar “configured default method order” de “effective per-run order”, mas isso **não** deve ser puxado para este patch sem necessidade concreta.

## 9.7. Testes de regressão obrigatórios

### Reuse

- mesmo executor em duas execuções sequenciais não compartilha report, failed, recovered dependencies ou source state.

### Recovery

- recovered commit continua não replayando host command;
- unresolved/ambiguous recovery continua fail-closed.

### Lazy dependency

- duas solicitações concorrentes na mesma execução deduplicam;
- execução seguinte não herda `dependencyRun` concluído da anterior.

### Explain/lock

- `ExplainTool` repetido não acumula source revisions de chamada anterior;
- universal lock ainda inclui revisão de source Git-backed correta;
- graph/why continuam funcionando com ExplainTool normal.

## 9.8. Critério estrutural final

Ao final:

```sh
rg -n 'schema\s+\*config.Schema|report\s+\*ExecReport|sources\s+\*source.Manager|recoveredCommits|dependencyMu|dependencies' internal/exec/executor.go
```

Não deve encontrar os fields run-scoped no `Executor`.

## 9.9. Validação focada

```sh
go test -race ./internal/exec/ -run 'Executor|Preparation|Dependency|Explain|Source|Recovery'
go test -race ./internal/app/ -run 'UniversalLock|Graph|Why|Install'
go test -race ./internal/source/
```

Depois suíte completa.

## 9.10. Estratégia de commits

Recomendado **três** commits, não um megapatch:

1. `refactor: keep schema and report in run context`
1. `refactor: scope recovery and lazy dependency state per run`
1. `refactor: scope source managers to execution and explain calls`

Opcional quarto commit mecânico:

1. `refactor: rename run context to execution session`

Somente se o rename justificar o churn.

______________________________________________________________________

# FASE 6 — SIM-04: canonicalização de source identity com gate de equivalência

## 10. Objetivo

Eliminar parsing/canonicalização realmente duplicado, mas **não** criar uma função genérica que silenciosamente mude inputs aceitos. Research log: `.dev/Research-Log-Implementation.md:~196-256`.

Esta fase é deliberadamente um **gate de decisão**, não uma promessa de que um novo package será criado.

## 10.1. Inventário observado

### Package sources

`internal/source/manager.go`:

- `brewTapPresent`: `~316-355`;
- `scoopBucketPresent`: `~398-425`;
- `sameSourceURL`: `~459-461`;
- `canonicalSourceURL`: `~463-480`.

Semântica aproximada atual:

- trim;
- parse URL quando possível;
- lowercase scheme/host;
- trim trailing slash;
- remove `.git` em hosts Git conhecidos;
- usa isso para impedir adoção de Brew/Scoop same-name com origem diferente.

`internal/source/identity.go` deve permanecer separado: sua identidade persistida é de recurso/ownership e não uma canonicalização universal de URL. Não incluir URL em `ResourceIdentity` só para “reusar” uma função.

### GitHub release

`internal/ghrelease/resolver.go`:

- `splitRepo`: `~359-385`;
- `IsGitHubURL`: `~438-445`;
- `githubRepoFromURL`: `~447-468`.

Essas funções **parecem** duplicadas, mas não têm a mesma semântica:

- `splitRepo` valida uma referência de repo resolvível, com exatamente owner/repo;
- `githubRepoFromURL`/`IsGitHubURL` aceita URL sob um repo para decidir contexto GitHub/auth e pode aceitar path adicional.

Logo: **não unificar essas funções só porque ambas chamam `url.Parse` e removem `.git`.**

## 10.2. Etapa A — tabela de verdade antes do código

Criar uma matriz de caracterização durante implementação, preferencialmente como testes, não como um documento permanente enorme.

Entradas a comparar:

- `owner/repo`
- `github.com/owner/repo`
- `https://github.com/owner/repo`
- `https://github.com/owner/repo.git`
- trailing slash
- uppercase/lowercase host
- userinfo
- default/non-default port
- query/fragment
- subpaths `/releases/download/...`
- GitLab/Codeberg quando relevante
- SCP-like Git refs, se algum consumer realmente aceita
- inválidos (`owner`, `owner/repo/extra`, malformed URL)

Para cada consumer, registrar:

- aceita/rejeita;
- canonical output;
- erro vs false;
- se a função está validando **identidade**, **equivalência**, **origem confiável** ou apenas **classificação**.

## 10.3. Gate de extração

Só extrair helper compartilhado quando houver **dois ou mais consumers** com a mesma tabela de verdade relevante.

Se não houver, SIM-04 termina validamente com:

- characterization tests melhores;
- funções existentes mantidas separadas;
- comentário curto explicando por que semânticas parecidas não são equivalentes.

Isso satisfaz o critério de parada do próprio research log e evita “DRY por estética”.

## 10.4. Se houver equivalência comprovada

Preferir um helper **semanticamente nomeado**, não `NormalizeURL`.

Exemplos aceitáveis:

- `CanonicalGitRemoteForComparison`
- `CanonicalPackageSourceURL`
- `ParseGitHubRepoReference`

Nome deve explicar o nível de confiança e uso.

### Pseudocódigo conservador para comparação de remote

> Remova apenas whitespace externo. Parseie a forma URL que o contrato já aceita. Normalize scheme e hostname apenas onde comparação case-insensitive já é válida. Remova um trailing slash que hoje já é ignorado. Remova o sufixo terminal `.git` somente para os hosts já tratados assim. Preserve porta, path, query e userinfo a menos que o contrato atual explicitamente os ignore. Se o parse falhar, mantenha a semântica de erro/valor do consumer original; não transforme input inválido em válido por fallback permissivo.

## 10.5. Onde colocar o helper

Ordem de preferência:

1. manter dentro do package que já possui os consumers, se forem do mesmo domínio;
1. só criar um package pequeno (`internal/...`) se pelo menos dois packages reais compartilharem exatamente a mesma regra;
1. não criar um package “identity” genérico para uma única função.

## 10.6. Testes

### `internal/source/manager_test.go`

Caracterizar equivalência Brew/Scoop:

- `.git` vs sem `.git` nos hosts suportados;
- host case;
- trailing slash;
- origem realmente diferente continua erro.

### `internal/ghrelease/*_test.go`

Caracterizar separadamente:

- repo reference estrita;
- URL GitHub para auth/classificação com subpaths.

Se os testes demonstrarem semânticas diferentes, **esse é um resultado útil** e bloqueia a extração.

## 10.7. Critérios de aceitação

- [ ] Nenhum input novo aceito acidentalmente.
- [ ] Nenhum trust boundary relaxado.
- [ ] Nenhuma rede/cache/download adicionada.
- [ ] Helper comum só existe com dois consumidores equivalentes.
- [ ] `source.ResourceIdentity`/ownership não é confundido com URL canonicalization.
- [ ] Não existe `FetchManager` nem provider layer.

## 10.8. Estratégia de commits

1. `test: characterize source identity equivalence`
1. **somente se gate passar:** `refactor: share <semantics-specific> source canonicalization`

Se gate falhar, o segundo commit não existe. Isso é sucesso, não tarefa incompleta.

______________________________________________________________________

# FASE 7 — SIM-06: `methodkind.Contract` como autoridade declarativa

## 11. Objetivo

Eliminar duplicação apenas da metadata que pertence ao contrato declarativo/schema-semantic. Research log: `.dev/Research-Log-Implementation.md:~307-342`.

## 11.1. Autoridade atual

`internal/methodkind/methodkind.go`:

- `Contract`: `~58-77`;
- comentário explícito “single source of truth”: `~226-229`;
- AUR declara aliases `paru`/`yay`: `~331`;
- `finalizeContracts`: `~425-470`;
- default order derivada de Contracts: `~498-511`;
- known kinds inclui aliases: `~513-533`;
- `Lookup`: `~549-562`.

`internal/methodkind/field_semantics.go:~3-93` também centraliza efeitos/semântica de fields.

## 11.2. Primeiro alvo comprovado: aliases AUR duplicados

`internal/ecosystem/aur_alias.go:~18-26` hardcodeia:

```text
["paru", "yay"]
```

mas a mesma informação já está em `methodkind.Contract` para `aur` (`methodkind.go:~331`). Esse é o exemplo ideal de duplicação declarativa real.

### Mudança

`RegisterAURAliases` deve:

1. obter contract de `aur` via `methodkind.Lookup("aur")`;
1. iterar `contract.Aliases`;
1. registrar `AURByNameAdapter` para cada alias;
1. falhar cedo/panic apenas se a composição estiver internamente inconsistente, seguindo estilo atual de bootstrap.

### Teste

Atualizar/adicionar teste em `internal/ecosystem/aur_v2_test.go`:

- todos os aliases do contract possuem adapter registrado/constructible;
- nenhum alias esperado é hardcoded no teste, idealmente o teste itera `contract.Aliases`;
- cada alias adapter retorna `Kind() == alias` e usa helper correspondente.

Isso prova que adicionar futuro alias em `Contract` não exige editar registry + docs + testes em quatro lugares.

## 11.3. Inventário de metadata: classificar antes de consolidar

### Pertence a `methodkind.Contract`

- kind e aliases;
- default order;
- schema fields;
- capabilities declarativas;
- scopes/environment/architecture semantics;
- shorthand admission;
- metadata usada para validar candidate intent.

### **Não** pertence ao Contract

- binary concreto usado em runtime;
- command templates;
- parsing de output de um CLI externo;
- constructor concreto do adapter;
- runner/timeouts;
- peculiaridades operacionais do host;
- composição do bootstrap.

Isso é especialmente relevante para `internal/ecosystem/registry.go:~8-182`: `Configs` contém `KindName`, `Binary`, `CheckTmpl`, `InstallTmpl`, `RemoveTmpl`, etc. Embora `KindName` repita a key, o restante é runtime operational metadata e deve continuar local ao adapter.

## 11.4. `BaseConfig.KindName`: avaliar, não remover automaticamente

`internal/ecosystem/base.go:~28-29` e `registry.go:~15-182` repetem kind em map key + `KindName`.

Parece um alvo simples, mas `KindName` também permite fixtures de teste ad hoc (`internal/ecosystem/ecosystem_test.go`, vários casos) e é usado profundamente em mensagens/comportamento de `BaseAdapter`.

Plano:

1. medir o diff necessário para remover essa duplicação;
1. verificar se a alternativa apenas troca “duplicação explícita” por constructor mais complexo;
1. só remover se a API fica objetivamente menor, por exemplo constructor recebe kind explicitamente e produção deriva da map key sem criar outro source of truth;
1. não puxar esse refactor para SIM-06 se ele inflar o patch sem reduzir lugares editados por method real.

O primeiro alvo AUR já entrega valor suficiente para provar a direção.

## 11.5. Conformance tests

`contract_test.go:~11-35` já verifica:

- kind registrado possui contract;
- adapter existe;
- `CanRemove` corresponde ao contract;
- todo contract kind é registrado.

Reforços possíveis, apenas para fatos compartilhados reais:

- aliases declarados têm adapter/comportamento esperado quando aliases são runtime kinds;
- capability remove pode ser comparada via `contract.Supports(CapabilityRemove)` em vez de consumidores novos consultarem `CanRemove` declarativo diretamente.

Não tentar provar runtime templates a partir do Contract, porque eles não pertencem à mesma autoridade.

## 11.6. Bootstrap deve permanecer explícito

`internal/app/bootstrap.go:~16-36` continua listando constructors concretos.

Não gerar bootstrap automaticamente de `methodkind.Contracts`. Isso esconderia dependências e exigências de construction atrás de reflection/factory registry, exatamente o tipo de complexidade que SIM-06 quer evitar.

## 11.7. Pseudocódigo em linguagem natural

> Ao adicionar um método, declare vocabulário e capabilities no Contract. O bootstrap continua escolhendo qual implementation concreta registrar. O adapter continua dono de binaries, comandos, parsing e comportamento de host. Se um consumidor precisa saber algo que já está no Contract, ele consulta o Contract em vez de manter uma segunda lista. Se a informação não descreve schema/capability declarativa, ela fica no adapter.

## 11.8. Critérios de aceitação

- [ ] Aliases AUR não aparecem como lista duplicada no ecosystem registry.
- [ ] `methodkind.Contract` continua sendo a única autoridade para aliases/schema semantics.
- [ ] Runtime operational metadata permanece local ao adapter.
- [ ] Bootstrap explícito não vira factory framework.
- [ ] Nenhum `AdapterSpec` paralelo é criado.

## 11.9. Validação focada

```sh
go test -race ./internal/methodkind/
go test -race ./internal/ecosystem/
go test -race ./... -run 'RegisteredAdaptersMatchMethodContracts|InstallMethodsMatchContracts'
```

## 11.10. Commit sugerido

`refactor: derive declarative method metadata from contracts`

Se `BaseConfig.KindName` for mexido, fazer em commit separado depois do alvo AUR, para não misturar uma prova simples com um refactor maior.

______________________________________________________________________

# FASE 8 — SIM-08: ampliar documentation-as-contract sem criar outra infraestrutura

## 12. Objetivo

Expandir validação apenas para exemplos **autoritativos e executáveis**. Research log: `.dev/Research-Log-Implementation.md:~389-423`.

O baseline já implementa boa parte da proposta. Logo, esta fase deve começar por reconhecer o que já existe, em vez de duplicá-lo porque o log foi escrito antes ou de forma mais ampla.

## 12.1. Infraestrutura existente

### Root contract tests

`example_contract_test.go`:

- constants dos quatro exemplos completos: `~17-43`;
- `TestShippedExamplesMatchRuntimeContract`: `~46-109`;
  - parse `schema.example.toml` e `manifest.example.toml`;
  - manifest layer validation;
  - semantic validation;
  - method contract coverage.
- `TestSchemaReferenceCompleteExamplesMatchRuntimeContract`: `~111-148`;
  - garante que docs contêm exatamente os fixtures testados;
  - parse + semantic validation dos quatro complete documents.

### Schema JSON tests

`internal/config/jsonschema_test.go:~50-65` também garante que os dois shipped example files continuam parseáveis. Não é necessário ampliar isso para Markdown.

### Method vocabulary docs tests

`internal/methodkind/public_docs_test.go:~14-94` já valida as listas de methods no README e cheatsheet contra Contracts.

## 12.2. `docs/schema-reference.md` já está coberto

`docs/schema-reference.md:~32-76` contém seção explicitamente intitulada “Complete copy-paste documents” e diz em `~34` que os exemplos são validados pela suíte.

Esses quatro exemplos já estão vinculados exatamente a fixtures no root test. Não criar segundo parser/marker para eles sem necessidade.

## 12.3. README Quick Start: principal lacuna de alto valor

`README.md:~55-70` contém um schema completo de Quick Start:

- `schema_version = 1`;
- `[tools]`;
- simple list;
- native manager override;
- ecosystem bucket.

Esse bloco é copy-pasteable e deveria ser testado como contrato.

### Implementação recomendada

Adicionar um teste ao `example_contract_test.go` que:

1. localiza a seção `## Quick start`;
1. extrai **o primeiro fenced TOML block dentro daquela seção**, usando helper local e estreito;
1. escreve fixture temporário;
1. `config.ParseProjectSchema`;
1. `validate.ValidateSchema(..., exec.RegisteredKinds())`;
1. falha com mensagem que menciona README Quick Start.

Não criar parser Markdown geral. A regra é intencionalmente específica ao bloco autoritativo.

Alternativa ainda mais estável: armazenar uma constant fixture e exigir ```` strings.Contains("```toml\n"+fixture+"```") ````, igual ao schema reference. Isso é simples, mas duplica o exemplo no test source. O padrão já existe e é aceitável para exemplos curtos. Escolher a opção que produzir menor código e erro mais legível.

## 12.4. Cheatsheet: **não** parsear fragmentos como documentos

`docs/cheatsheet.md:~45-48` diz explicitamente:

> “This is a fragment. Complete project schemas also need `schema_version = 1`.”

Portanto:

- não parsear todos os fenced TOML blocks;
- não “consertar” o cheatsheet adicionando boilerplate a cada fragmento só para satisfazer teste;
- manter `public_docs_test.go` para vocabulary dos methods;
- somente validar um trecho TOML do cheatsheet como schema completo se ele for explicitamente marcado/documentado como completo no futuro.

## 12.5. Shipped files

- `schema.example.toml`
- `manifest.example.toml`

Já são validados em `example_contract_test.go:~46-109` e parseados também em `internal/config/jsonschema_test.go:~50-65`.

Não adicionar terceira validação idêntica. Se for desejável reduzir duplicação futura, isso deve ser avaliado por ownership de tests, não empurrado para SIM-08 sem problema concreto.

## 12.6. Marker mechanism: só se houver segundo caso real

O research log admite “explicitamente marcado como autoritativo”. Não criar uma sintaxe como:

```html
<!-- depengine-contract: schema -->
```

apenas para um bloco. Se depois houver vários documentos autoritativos espalhados, aí sim uma marker convention local ao test helper pode reduzir manutenção.

Até lá, heading + bloco conhecido é suficiente.

## 12.7. Critérios de aceitação

- [ ] README Quick Start completo é parseado + semanticamente validado.
- [ ] schema-reference mantém cobertura existente sem suite paralela.
- [ ] shipped examples mantêm cobertura existente.
- [ ] cheatsheet fragments permanecem fragments, não são artificialmente parseados.
- [ ] nenhum generic Markdown parser.
- [ ] nenhum teste tenta executar todos os fenced TOML do repositório.

## 12.8. Validação focada

```sh
go test -race . -run 'Example|Document|README|SchemaReference'
go test -race ./internal/methodkind/ -run 'Readme|Cheatsheet'
go test -race ./internal/config/ -run 'ExamplesMatchRuntimeGrammar'
```

## 12.9. Commit sugerido

`test: validate authoritative documentation examples`

______________________________________________________________________

# FASE 9 — SIM-09 e SIM-10 como guardrails permanentes

## 13. SIM-09: um único módulo Go

Fonte: `.dev/Research-Log-Implementation.md:~427-450`.

Baseline:

- `go.mod:~1-21` define um único módulo `github.com/Khorea1/depengine`;
- `docs/architecture.md:~34-55` já separa responsabilidades por packages internos.

### Regra de review em todas as fases

Nenhuma fase deste plano precisa de novo `go.mod`.

Um split de módulo só seria justificável quando existir consumer independente com:

- ciclo de release próprio;
- compatibilidade/versionamento próprio;
- utilidade real fora do binário principal;
- benefício superior ao custo de versionar e coordenar módulos.

Nada em SIM-01…08 atinge esse limiar.

### Critério verificável

```sh
find . -name go.mod -not -path './vendor/*'
```

Deve continuar retornando apenas o root module.

______________________________________________________________________

## 14. SIM-10: `AdapterV2` uniforme

Fonte: `.dev/Research-Log-Implementation.md:~454-476`.

`internal/exec/adapter.go:~39-78` define um contrato único para:

- Kind;
- Available;
- Check/Resolve/Observe;
- InstallResolved;
- removal capability/operation;
- host compatibility.

`ElevationRequirer`/`RemovalElevationRequirer` em `~80-95` já existem como interfaces auxiliares de preparação de privilege session. Elas não devem ser usadas como precedente para quebrar cada capability semântica em uma interface opcional diferente.

### Guardrail

Antes de criar nova interface opcional, exigir:

1. pelo menos dois adapters com a mesma operação duplicada;
1. semântica realmente idêntica;
1. executor ficaria **mais simples**, não com mais capability negotiation;
1. não quebrar alinhamento entre dry-run/why/status/install na resolução comum.

Caso contrário, manter método no adapter existente ou helper local.

### Proibições explícitas durante este plano

- `Fetcher`, `ResolverProvider`, `PackageProvider`, `SourceProvider` genéricos sem dois consumers reais;
- capability negotiation espalhada pelo executor;
- adapter runtime graph construído dinamicamente de dezenas de microinterfaces.

______________________________________________________________________

# 15. Plano de PRs/commits recomendado

A sequência abaixo otimiza review, reversibilidade e `git bisect`.

## PR 1 — Contratos antes de refactor

### Commit 1

`docs: formalize state model and invariants`

Arquivos prováveis:

- `docs/design/state-model.md` novo;
- `docs/architecture.md` link;
- possivelmente `docs/support-boundary.md` link cruzado.

### Commit 2

`test: codify executor failure domains`

Arquivos prováveis:

- `docs/design/state-model.md` failure domain section;
- `internal/exec/failure_domain_test.go` novo;
- pequenas melhorias em testes existentes apenas se necessárias.

### Commit 3

`docs: add adapter authoring contract`

Arquivos:

- `docs/adapter-authoring.md` novo;
- `docs/architecture.md` link.

**Merge gate:** docs + failure semantics estabilizadas antes de state refactor.

______________________________________________________________________

## PR 2 — Registry lifecycle

### Commit 1

`refactor: configure runtime adapters per executor`

Arquivos prováveis:

- `internal/app/install.go`
- `internal/app/update.go`
- `internal/app/upgrade.go`
- `internal/app/graph_view.go`
- `internal/app/graph_why.go`
- `internal/app/remove.go`
- `internal/app/undo.go`
- testes correspondentes.

### Commit 2

`refactor: remove post-bootstrap registry mutation API`

Arquivos:

- `internal/ecosystem/registry.go`
- `internal/exec/registry.go`
- `internal/exec/registry_test.go`
- docs/comments.

**Merge gate:** `rg 'exec\.Replace|ReconfigureAUR' internal` limpo.

______________________________________________________________________

## PR 3 — Run-scoped executor state

### Commit 1

`refactor: keep schema and report in run context`

### Commit 2

`refactor: scope recovery and lazy dependency state per run`

### Commit 3

`refactor: scope source managers to execution and explain calls`

### Commit 4 opcional

`refactor: rename run context to execution session`

**Merge gate:** mesmo `Executor` reutilizado em duas runs sem state leakage.

______________________________________________________________________

## PR 4 — Source identity equivalence

### Commit 1

`test: characterize source identity equivalence`

### Commit 2 condicional

`refactor: share <specific> source canonicalization`

**Importante:** se a matriz provar semânticas diferentes, fechar o PR apenas com characterization/clarification. Não inventar uma extração para justificar o número SIM-04.

______________________________________________________________________

## PR 5 — Method metadata authority

### Commit 1

`refactor: derive AUR aliases from method contract`

### Commit 2 opcional

`refactor: remove proven duplicate method metadata`

Somente para duplicação adicional que passe o inventário.

______________________________________________________________________

## PR 6 — Docs as contract

### Commit 1

`test: validate authoritative documentation examples`

Foco em README Quick Start e qualquer outro bloco explicitamente completo que exista naquele momento.

______________________________________________________________________

# 16. Matriz de arquivos por proposta

| SIM | Arquivos primários | Arquivos secundários/testes |
| ------ | ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| SIM-01 | `internal/exec/executor.go`, `run.go`, `execute.go`, `preparation.go`, `explain.go` | `attempt.go`, `preparation_test.go`, `executor_test.go`, `lazy_dependency_test.go`, `internal/app/universal_lock.go` |
| SIM-02 | novo `docs/design/state-model.md` | `docs/architecture.md`, `docs/support-boundary.md`, ADR-001/002/005 como referências |
| SIM-03 | `docs/design/state-model.md`, `internal/exec/run.go` apenas se teste revelar bug | novo `failure_domain_test.go`, existing executor/preparation tests |
| SIM-04 | `internal/source/manager.go`, `internal/ghrelease/resolver.go` somente após gate | respectivos `*_test.go`; `internal/source/identity.go` como boundary a não misturar |
| SIM-05 | `internal/exec/registry.go`, `internal/ecosystem/registry.go`, app command builders | `upgrade_test.go`, `remove_command_inprocess_test.go`, `registry_test.go`, docs architecture |
| SIM-06 | `internal/methodkind/methodkind.go`, `internal/ecosystem/aur_alias.go` | `contract_test.go`, ecosystem/methodkind tests; talvez `registry.go` inventário |
| SIM-07 | novo `docs/adapter-authoring.md` | `docs/architecture.md` link; adapter/methodkind/plan files como referências |
| SIM-08 | `example_contract_test.go` | `README.md`, `docs/schema-reference.md`, `docs/cheatsheet.md`, existing jsonschema/public_docs tests |
| SIM-09 | nenhum diff necessário | `go.mod`, architecture boundaries como review gate |
| SIM-10 | nenhum diff necessário | `internal/exec/adapter.go` como review gate |

______________________________________________________________________

# 17. Riscos principais e mitigação

## 17.1. Risco: “state model” virar documentação duplicada

**Sintoma:** começar a copiar formato de lock, schema e state field-by-field.

**Mitigação:** documento só define autoridade/invariantes e aponta para ADR/spec para detalhes.

______________________________________________________________________

## 17.2. Risco: registry refactor quebrar AUR em `update`

`runUpdate` não executa install diretamente, mas chama a universal lock projection, que usa `newInstallExecutor` (`internal/app/universal_lock.go:~20-25`).

**Mitigação:** colocar o AUR override dentro do builder `newInstallExecutor`; então install e universal lock recebem mesma configuração sem outro canal global.

______________________________________________________________________

## 17.3. Risco: remover global registry mutation e perder test seam

`remove_command_inprocess_test.go` hoje usa `exec.Replace` como injeção.

**Mitigação:** tornar executor uma dependência explícita do helper de orchestration usado pelo teste. Não introduzir outro singleton/factory global.

______________________________________________________________________

## 17.4. Risco: SIM-01 aumentar acoplamento por passar session em toda função

**Mitigação:** session só atravessa orchestration interno. Não passa por `AdapterV2`, config ou plan. Onde helper precisa apenas de uma dependência específica, considerar passar esse objeto, mas evitar gerar cinco parâmetros soltos que são sempre usados juntos.

Uma referência a `runContext` dentro de `candidateAttempt` é aceitável porque ambos são tipos privados do executor lifecycle.

______________________________________________________________________

## 17.5. Risco: `ExplainTool` perder source revisions

**Mitigação:** migrar universal lock para uma variante de Explain com revisions **antes** de remover `Executor.SourceRevisions`.

Ordem segura:

1. implementar helper/variant local de Explain;
1. migrar universal lock;
1. testar source revision projection;
1. remover method/field antigos.

______________________________________________________________________

## 17.6. Risco: canonicalizer ampliar trust semantic

Exemplo: tratar `https://github.com/owner/repo` e URL com userinfo/query como “mesma origem” sem que o consumer original fizesse isso.

**Mitigação:** characterization table + helper semanticamente nomeado + não remover components não ignorados no contrato atual.

______________________________________________________________________

## 17.7. Risco: `methodkind.Contract` virar depósito de runtime config

**Mitigação:** pergunta de ownership:

> Este dado descreve o que o schema/candidate **significa**, ou como um programa externo é **invocado**?

Primeiro caso: Contract. Segundo: adapter.

______________________________________________________________________

## 17.8. Risco: docs tests ficarem frágeis por parsing de Markdown

**Mitigação:** localizar seções/blocos explicitamente autoritativos, não todos os fences. Fragments continuam fragments.

______________________________________________________________________

# 18. Checkpoints de revisão arquitetural

Após cada PR, conferir:

## Depois de PR 1

- É possível explicar `absent/drifted/unknown/broken` sem ler cinco commands diferentes?
- Failure domain diz claramente quem continua e quem é bloqueado?
- Adapter authoring aponta para contracts existentes, sem inventar “best practices” desconectadas do runtime?

## Depois de PR 2

- Dois schemas com `aur_helper` diferentes podem construir executors sem interferência?
- remove/undo não alteram process registry?
- `defaultRegistry` está efetivamente bootstrap-only?

## Depois de PR 3

- `Executor` pode ser reutilizado sem state leakage?
- source manager de Explain é call-scoped?
- recovery/lazy dependency concurrency pertence à sessão certa?

## Depois de PR 4

- Houve alguma aceitação de input nova? Se sim, parar e justificar separadamente.
- O helper extraído representa uma equivalência semântica, não só código parecido?

## Depois de PR 5

- Quantos arquivos precisam mudar para adicionar um alias declarativo novo?
- Bootstrap continua explícito?
- Runtime command templates continuam onde pertencem?

## Depois de PR 6

- Todo exemplo marcado/entendido como “complete/copy-paste” é testado?
- Nenhum fragment foi transformado artificialmente em documento só para satisfazer teste?

______________________________________________________________________

# 19. Definition of Done global

O trabalho do Research Log pode ser considerado concluído quando todos os itens abaixo forem verdadeiros.

## State e failure

- [ ] `docs/design/state-model.md` é a referência normativa curta.
- [ ] `satisfied/absent/drifted/unknown/broken` possuem semântica coerente com `plan.Reconcile`.
- [ ] failure domain prova sibling continuation + dependent blocking.
- [ ] ambiguous external mutation continua fail-closed e sem replay.

## Adapters

- [ ] `docs/adapter-authoring.md` existe e referencia o contrato real.
- [ ] `AdapterV2` permanece uniforme.
- [ ] não há nova capability-interface sem duplicação comprovada.

## Registry

- [ ] não há `exec.Replace` package-level.
- [ ] não há `ecosystem.ReconfigureAUR`.
- [ ] AUR/native runtime configuration é instance-scoped.
- [ ] tests não dependem de alteração global temporal do registry.

## Executor state

- [ ] `Executor` não guarda schema/report/source manager/recovery map/dependency map de uma run.
- [ ] mesma instância de Executor pode executar duas runs sem vazamento.
- [ ] Explain/lock source revisions têm lifecycle explícito e isolado.

## Source identity

- [ ] equivalências compartilhadas são cobertas por characterization tests.
- [ ] helpers só foram extraídos se semântica for realmente comum.
- [ ] nenhuma abstraction de fetch/provider foi criada.

## Method metadata

- [ ] aliases AUR derivam de `methodkind.Contract`.
- [ ] outras duplicações removidas apenas quando declarativas/schema-semantic.
- [ ] bootstrap e operational metadata continuam explícitos/localizados.

## Documentation contracts

- [ ] README Quick Start completo é validado.
- [ ] schema-reference complete docs continuam validados.
- [ ] shipped example TOMLs continuam validados.
- [ ] cheatsheet fragments não são tratados como documentos completos.

## Repository shape

- [ ] continua havendo um único `go.mod`.
- [ ] nenhum novo framework interno foi introduzido para cumprir o plano.

## Validation final

- [ ] `go build -o depengine .`
- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `golangci-lint run`
- [ ] `gofmt -e -l <arquivos Go tocados>` vazio para os arquivos alterados.

______________________________________________________________________

# 20. Ordem de execução condensada

Para uso durante implementação, a sequência operacional é:

1. **Baseline**: confirmar commit + testes canônicos.
1. **SIM-02**: escrever `state-model.md`; linkar architecture/support boundary.
1. **SIM-03**: declarar failure domain no mesmo doc; adicionar sibling-continuation test; reutilizar recovery tests existentes.
1. **SIM-07**: escrever `adapter-authoring.md`; linkar architecture.
1. **SIM-05/A**: migrar AUR de install/update/upgrade/graph/why para `WithAdapters` por instância.
1. **SIM-05/B**: migrar native remove/undo para override por instância e migrar tests de remove para injection explícita.
1. **SIM-05/C**: remover `ReconfigureAUR` + package-level `exec.Replace`; atualizar docs/comments; adicionar isolamento test.
1. **SIM-01/A**: criar `runContext` no início de Execute; mover `schema/report`; testar executor reuse.
1. **SIM-01/B**: mover `recoveredCommits/dependencies/mutex` para session; migrar recovery/lazy tests.
1. **SIM-01/C**: tornar execution source manager session-scoped; tornar Explain source manager call-scoped; migrar universal lock; remover `SourceRevisions()` do Executor.
1. **SIM-04/A**: characterization matrix para source/GitHub identity.
1. **SIM-04/B condicional**: extrair apenas helper com semântica comprovadamente comum.
1. **SIM-06/A**: aliases AUR derivados do Contract.
1. **SIM-06/B condicional**: remover outras duplicações declarativas comprovadas, sem mexer em operational metadata.
1. **SIM-08**: validar README Quick Start; preservar cobertura existente; não criar Markdown parser genérico.
1. **Guardrail audit**: um `go.mod`, `AdapterV2` uniforme, nenhuma abstraction especulativa.
1. **Validação canônica completa**.
1. **Review final contra os dez critérios do research log**, não apenas contra diff compilando.

______________________________________________________________________

# 21. Resultado arquitetural esperado

Ao final, a arquitetura continua reconhecivelmente a mesma, o que é uma virtude neste trabalho:

```text
Config/manifest
    ↓
Planner + method contracts
    ↓
ResolvedInstallPlan
    ↓
Executor (configuração estável + adapter snapshot/overrides)
    ↓
runContext / execution session (estado mutável de UMA run)
    ↓
AdapterV2 uniforme
    ↓
Runner / host

Lock projection ─────┐
Preparation WAL ─────┼─> state model normativo + ownership/recovery
Observed identity ───┘
```

O que desaparece:

- mutation temporal do registry global;
- schema/report/recovery/dependency/source state acumulado no Executor;
- lista duplicada de aliases declarativos onde Contract já é autoridade;
- ambiguidade documental sobre failure propagation;
- parte do drift entre exemplos copy-paste e parser/runtime.

O que **não** aparece, deliberadamente:

- novo framework;
- novo módulo;
- fetch manager;
- provider hierarchy;
- scheduler rewrite;
- dezenas de capability interfaces.

Essa ausência é parte do resultado, não falta de ambição. O projeto fica mais simples porque algumas responsabilidades passam a ter um único dono, não porque ganhou mais nomes para as mesmas coisas.
