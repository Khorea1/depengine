# Plano de correção da PR #148

> **Estado: IMPORTADO em 2026-10-12 · Origem: texto do mantenedor (sessão de 2026-10-12).**
> Base: [PR #148, proposta E](https://github.com/Khorea1/depengine/pull/148), commit `d3913d2`.
> Objetivo: fechar as inconsistências entre especialização, resolução, execução, verificação e remoção, preservando a arquitetura E.
> Tracking: issues Beads filhas de `dep-1n0` (`dep-1n0.7`–`dep-1n0.13`).
> Estratégia: 7 tarefas de correção, com testes específicos e uma etapa final de integração. Adequado para execução por agentes independentes, com integração sequencial dos componentes que compartilham contratos.

## 1. Corrigir o contrato de regex e o matching (dep-1n0.7)

P1/P2 · Independente · Primeiro commit

Arquivos: `internal/ghrelease/assetmatch.go`, `internal/config/strict.go`, `internal/config/jsonschema.go`, `internal/config/jsonschema_test.go`, `internal/ghrelease/assetmatch_test.go`.

- Corrigir a ancoragem do regex para `^(?:EXPRESSÃO)$`, evitando que alternâncias escapem de `^` e `$`.
- Permitir `asset = { regex = "..." }` também no campo principal de métodos `github`. Atualmente o resolvedor aceita essa representação, mas a validação e o JSON Schema ainda tratam o campo principal como string.
- Reutilizar a definição do formato do seletor entre o campo principal e os overrides de distribuição.
- Restringir essa extensão ao `github`, sem alterar silenciosamente o contrato de `http` ou de outros métodos.
- Garantir que validação estrita, JSON Schema e resolvedor reconheçam exatamente os mesmos formatos.

Testes obrigatórios: alternância com prefixo/sufixo indevido, regex inválido, seletor principal com regex válido, regex numa distribuição e métodos que não aceitam essa representação.

Aceite: nenhuma correspondência parcial, e nenhuma configuração aceita pelo schema que depois seja rejeitada apenas por divergência de formato no parser.

## 2. Unificar o candidato efetivo na resolução e verificação (dep-1n0.8)

P1 · Depende apenas da arquitetura existente

Arquivos: `internal/exec/verification.go`, `internal/exec/lock_resolution.go`, testes de `internal/exec`.

Esta é a correção estrutural prioritária.

- Introduzir um helper interno de resolução que retorne o candidato efetivo e seu plano resolvido, preservando o candidato declarado e sua identidade.
- Fazer `ResolveCandidatePlan` continuar oferecendo sua API atual, como wrapper do helper.
- Fazer `ResolveAndVerifyCandidateAtVersion` usar o método efetivo ao chamar `VerifyResolvedCandidate`, em vez de voltar ao original.
- Reutilizar o mesmo candidato na verificação, no cálculo de identidade e na seleção de propriedades de instalação.
- Preservar `Kind`, `Label` e ordinal do candidato declarado.
- Manter a especialização pura, sem mutar o schema ou fazer novas consultas de rede.
- Não alterar a representação do lock nem introduzir outra identidade de distribuição.

Regressões obrigatórias:

- `br` no Windows: resolve `br.exe` e verifica `br.exe`, nunca o entrypoint herdado `br`.
- Linux glibc/musl: resolução e verificação recebem os respectivos overrides.
- Candidato sem `distributions`: comportamento idêntico ao anterior.
- Duas resoluções com os mesmos fatos: mesmo resultado, sem mutação da configuração original.
- `status` e `upgrade`: verificam exatamente o payload selecionado.

Aceite: o candidato usado para verificar não pode divergir daquele usado para construir o plano.

## 3. Corrigir ciclo de vida de artefatos regex (dep-1n0.9)

P1 · Depende da tarefa 2 para integração

Arquivos: `internal/httpdownload/adapter.go`, `internal/httpdownload/github_adapter.go`, `internal/httpdownload/resolver.go`, testes do adapter e de remoção.

- Auditar todos os caminhos que consultam `asset` presumindo `string`.
- Não usar o texto do seletor regex como indicador do formato do artefato.
- Na instalação resolvida, usar a URL concreta presente em `resolved.Artifacts`.
- Para remoção, priorizar o estado persistido da instalação e os metadados de ownership. Não reinterpretar o manifesto atual como autoridade sobre o payload antigo.
- Garantir que arquivos compactados regex sejam identificados como arquivos compactados, mesmo sem `entrypoints`.
- Preservar as proteções existentes contra remoção de diretórios compartilhados, payloads não pertencentes à ferramenta e instalações legacy sem evidência suficiente.
- Revisar `RequiresElevation` e `GitHubAdapter.Install` para que a representação map não altere acidentalmente o tratamento do formato.

Testes obrigatórios: instalar e remover um `.tar.gz` selecionado por regex sem entrypoints; instalar e remover um `.zip` com entrypoints; rejeitar remoção de payload sem ownership; preservar remoção de binários brutos; garantir que o caminho resolvido permaneça autoritativo.

Aceite: o formato e a propriedade do artefato, não a sintaxe usada para selecioná-lo, determinam seu ciclo de vida.

Restrição importante: não introduzir novos campos persistidos se `OwnsArchivePayload`, o estado do candidato e a evidência existente no filesystem forem suficientes.

## 4. Aplicar corretamente erros terminais e fallback (dep-1n0.10)

P1 · Depois das tarefas 1 e 2

Arquivos: `internal/ghrelease/resolver.go`, `internal/exec/attempt.go`, `internal/exec/lock_resolution.go`, `internal/exec/explain.go`, testes correspondentes.

- Classificar erros de matching com tipos ou sentinelas identificáveis por `errors.Is`/`errors.As`. Não decidir pelo texto do erro.
- Preservar a semântica `0 distribuições → fallback`, `1 → selecionar`, `2+ → erro terminal`.
- Aplicar a classificação às falhas de resolução de assets: zero correspondências ou múltiplas correspondências não podem causar fallback quando o contrato E determina erro terminal.
- Garantir que a mesma classificação seja respeitada pelo executor serial, pela resolução de lock e pela verificação de estado.
- Em `CheckDesiredState`, impedir que um método posterior satisfeito esconda uma ambiguidade terminal anterior.
- No `why`, manter o diagnóstico dos candidatos, mas sem representar como executável um fallback bloqueado por erro terminal.
- Separar falhas de disponibilidade recuperáveis das falhas de integridade. Um checksum inválido não deve permitir troca silenciosa de fonte quando isso contrariar o contrato.

Cuidado com compatibilidade: preservar o fallback histórico dos candidatos v1 sem `distributions`, salvo nos casos em que já existia uma regra fail-closed. Não converter indiscriminadamente todos os erros de download em falhas terminais.

Testes obrigatórios: nenhuma distribuição compatível com fallback bem-sucedido; duas distribuições compatíveis com fallback proibido; zero ou múltiplos assets no catálogo; checksum inválido; `check` com candidato posterior satisfeito; `ResolveLockCandidate` com ambiguidade.

Aceite: `install`, `check`, `lock` e `why` concordam sobre qual falha é recuperável e qual bloqueia o método ou a ferramenta.

## 5. Validar invariantes depois dos overrides (dep-1n0.11)

P2 · Independente das tarefas de executor

Arquivos: `internal/config/strict.go`, `internal/config/specialize.go`, `internal/config/distributions_test.go`.

- Reaproveitar as validações de `binary`, incluindo separadores, nomes reservados e prefixos de unidade.
- Validar combinações efetivas de `checksum`, `checksum_url`, `checksum_file_format`, `signature_url` e `signing_key`.
- Considerar valores herdados da base, e não apenas campos explicitamente declarados na distribuição.
- Detectar combinações inválidas introduzidas por merge entre schema e manifesto.
- Assegurar que as validações sejam realizadas sobre todas as distribuições, inclusive as que não correspondem ao host atual.
- Não duplicar regras de negócio em uma segunda tabela específica para distribuições.

Teste essencial: base com checksum automático e sidecar; manifesto sobrescrevendo o checksum para um valor fixo; distribuição herdando o sidecar. A configuração efetiva deve ser rejeitada antes da execução.

Aceite: qualquer configuração efetiva inválida é detectada na validação, independentemente do host selecionado.

## 6. Ajustar pré-verificação de elevação (dep-1n0.12)

P2 · Após tarefa 3

Arquivos: `internal/exec/execute.go`, `internal/httpdownload/adapter.go`, testes de elevação.

- Considerar o `when` do método e a compatibilidade das distribuições ao calcular necessidade de elevação.
- Não solicitar elevação por uma distribuição explicitamente incompatível com o host.
- Aplicar a configuração efetiva de `scope`, `extract_to` e `entrypoints` no cálculo.
- Para seletores regex cujo formato não está disponível no pré-planejamento, evitar inferências inseguras; utilizar os destinos conhecidos e uma política conservadora nos casos incertos.
- Não introduzir consulta ao GitHub apenas para decidir o prompt antecipado de elevação.
- Preservar o gate de elevação existente no momento da operação efetiva.

Aceite: instalações user-scoped não exigem privilégio desnecessariamente, e instalações system-scoped continuam sem contornar os controles de elevação.

## 7. Integração, regressões e fechamento da PR (dep-1n0.13)

Etapa final · Após todas as correções

Consolidar os testes das tarefas anteriores e executar uma matriz mínima:

| Cenário                           | Expectativa                            |
| --------------------------------- | -------------------------------------- |
| `br` em Linux glibc/musl          | Asset correto                          |
| `br` em macOS/Windows             | Asset e entrypoints corretos           |
| `omp` com checksum agregado       | Seleção e integridade corretas         |
| `wt` com AUR preferencial         | Prioridade e fallback preservados      |
| Android sem distribuição          | Método inelegível                      |
| Regex com zero/dois assets        | Erro terminal conforme contrato        |
| Arquivo compactado via regex      | Instalação e remoção seguras           |
| Configuração v1 sem distribuições | Sem regressões                         |
| Lock v1 antigo incompatível       | Erro explícito, sem migração implícita |

Executar:

```
go test ./...
go test -race ./...
go vet ./...
go generate ./internal/config
git diff --exit-code -- schema/depengine.schema.json
```

Além disso, executar o lint configurado no projeto e a matriz de CI, incluindo Windows e Android.

A geração do JSON Schema deve produzir exatamente o arquivo versionado, sem diferenças pendentes após a atualização intencional.

## Ordem de orquestração

Grafo de dependências

1. Regex e schema

4. Erros e fallback

2. Candidato efetivo

3. Lifecycle

6. Elevação

5. Validação

Independente; pode ser executada em paralelo

7. Integração e regressão final

A tarefa 4 depende conceitualmente do modelo efetivo da tarefa 2, mas seus tipos de erro e testes de matching podem ser desenvolvidos em paralelo.

## Restrições para os agentes

- Não substituir a proposta E por outra arquitetura.
- Não alterar `schema_version`, IDs de candidatos ou formato de lock.
- Não adicionar novas opções de autoria como `priority`, `extends`, `inherit` ou fallback por distribuição.
- Não fazer consultas de rede na especialização.
- Não ampliar a mudança a outros adapters sem uma necessidade concreta.
- Não corrigir os sintomas com condicionais dispersos. As decisões devem continuar centralizadas no contrato do método e nas fronteiras do executor.
- Cada correção deve incluir teste de regressão que falhe antes dela, sempre que reproduzível.

## Critério final de aprovação

A PR estará pronta quando três invariantes forem demonstradas:

1. Identidade consistente: a mesma distribuição determina o artefato resolvido, o plano, a execução e a verificação.
2. Ciclo de vida seguro: a remoção utiliza a instalação efetivamente registrada e suas evidências de propriedade, sem depender de uma nova interpretação do manifesto.
3. Falha determinística: uma configuração ambígua ou inválida produz o mesmo bloqueio em todas as operações relevantes, sem fallback indevido.

Não é necessário outro redesenho. O caminho mais eficiente é corrigir essas sete frentes, integrar os testes e encerrar `dep-1n0`.

Decisão de merge: bloquear até a conclusão das tarefas 1 a 5; executar 6 e 7 antes da aprovação final.
