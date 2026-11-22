# DX Audit: Onboarding e camada de catálogo do depengine

**Perspective**: Developer Experience Engineer
**Date**: 2026-11-15
**Plan**: `docs/design/adr-008-catalog-layer.md`, `docs/design/adr-009-catalog-layout.md` e `docs/roadmap.md` (epic `dep-ixd`)
**Scope**: fluxos CLI existentes de criação de esquema e importação/inspeção de receitas. Revisão estática; não é uma auditoria exaustiva da CLI.

## Plan Summary

O plano permite declarar as ferramentas do projeto e completar receitas reutilizáveis a partir de um catálogo local, com precedência explícita, identidade de conteúdo no lock v2 e diagnóstico de proveniência. Para o desenvolvedor, a jornada é `init` → `validate` → `import --dry-run`/`import` → `why --fields` → `install`; a auditoria confrontou as interfaces já implementadas com essa jornada.

## Files Reviewed

| File | DX Role | Risk |
|---|---|---|
| `docs/design/adr-008-catalog-layer.md` | Contrato e confiança das receitas | Baixo: decisão clara; execução ainda em fases |
| `docs/design/adr-009-catalog-layout.md` | Localização, precedência e erros do catálogo | Baixo: escolhas explícitas |
| `docs/roadmap.md` | Plano de evolução | Baixo: catalog layer ainda listada como trabalho aberto |
| `README.md` | Primeiro contato e exemplos | Baixo: sequência de início documentada |
| `docs/cli-reference.md` | Flags e contratos públicos gerados | Alto: promete `init --dry-run` sem gravação |
| `internal/app/init.go` | Scaffold, modo interativo e efeitos de `--dry-run` | Alto: preview ignorado; EOF grava arquivo |
| `internal/app/import.go` | Importação, preview, conflito e saída | Médio: preview conflita com destino existente; sem JSON |
| `internal/app/import_test.go` | Proteção existente contra substituição | Médio: cobre `--force`, não o preview de destino existente |
| `internal/app/init_adopt_test.go` | Preview de adoção | Médio: cobre `--dry-run` no ramo de adoção, não no scaffold comum |
| `internal/app/cli.go` | Help e roteamento da CLI | Baixo: Cobra e grupos explícitos |
| `internal/app/doctor.go` | Diagnóstico e JSON | Baixo: modelo positivo para outras operações |
| `internal/config/catalog.go` | Regras de resolução/erros dos roots | Baixo: recusa root explícita inválida |

## Verdict: APPROVE WITH CHANGES

A direção do catálogo é favorável à DX: `Lookup(name)` evita exigir que cada projeto replique métodos; `why --fields` torna a proveniência inspecionável; `doctor --json` atende a automações. Mas **o contrato de ausência de efeitos colaterais no `init` deve ser corrigido antes de tratar o onboarding como confiável**. Não proponho alterações de modelo nem novos ADRs.

## Critical Issues (Must Fix)

| # | Issue | Impact on Developer | Fix |
|---|---|---|---|
| C1 | `depengine init --add nvim --dry-run` aceita `--dry-run` mas escreve `schema.toml`. `newInitCmd` passa `DryRun` só ao ramo `runInitAdopt` (`internal/app/init.go:70-79`); o ramo normal chama `runInit` sem esse parâmetro, e `runInit` grava em disco (`:95`, `:150` aprox.). A flag anuncia explicitamente que não grava (`:89`, `docs/cli-reference.md`). | Quebra o contrato de preview e pode criar um arquivo inesperado em scripts e CI; a saída ainda indica sucesso de criação. | Fazer `--dry-run` valer para todos os ramos, sem `MkdirAll` nem `WriteFile`; imprimir o TOML resultante em stdout. Testar `--add`, scaffold padrão, `--schema` e `--interactive` ou rejeitar explicitamente combinações não suportadas antes de escrever. |
| C2 | `init --interactive` trata EOF/erro ao ler entradas como término normal e depois grava arquivo (`internal/app/init.go:183-208`, `:312-326`). | Em stdin fechado, cancelamento ou interrupção de entrada, pode surgir um esquema vazio/incompleto com código de sucesso, quando a intenção era apenas interromper o assistente. | Distinguir entrada vazia voluntária de EOF/erro, propagar cancelamento sem gravar, e testar stdin EOF desde a primeira pergunta e no meio de um método. Validar TTY ou oferecer erro com `--add` como alternativa para CI. |

## Recommendations

| # | Issue | Why | Fix |
|---|---|---|---|
| R1 | `import --dry-run` reporta erro de destino existente quando não há `--force` (`internal/app/import.go:145-160`). O relatório ainda é impresso, mas o retorno é 2 e falta o marcador final de preview. | Prejudica o fluxo natural de comparar uma receita nova com a instalada antes de autorizar a substituição; `--force` deveria ser necessário apenas para gravar. | Em dry-run, mostrar a colisão como aviso/efeito previsto e concluir com sucesso quando o conteúdo for válido; manter erro e proteção para gravação sem `--force`. Cobrir ambos os ramos com testes. |
| R2 | `depengine import` não expõe `--json`/`--format` (`internal/app/import.go:26-53`, `docs/cli-reference.md`), embora já compute `importReport` estruturado. | Scripts precisam analisar texto humanizado para obter digest, candidatos e avisos; outras operações, como `doctor` e `why`, já dispõem de saída estruturada. | Planejar um contrato JSON estável para preview e resultado com estado `previewed`/`written`/`rejected` e erros no stderr, sem quebrar saída humana existente. Testar stdout/stderr e código de saída. |
| R3 | `newImportCmd` ignora os writers configurados em Cobra ao chamar `runImport` que usa diretamente `os.Stdout`/`os.Stderr` (`internal/app/import.go:42-43`, `:86-90`). | Testes de comando e embedders que configuram `SetOut`/`SetErr` podem não capturar o resultado; difere de `doctor`. | Passar `cmd.OutOrStdout()` e `cmd.ErrOrStderr()` a `runImportTo`; testar roteamento sem depender de descritores globais. |

## Nice-to-Haves

| # | Suggestion | Rationale |
|---|---|---|
| N1 | Acrescentar à ajuda de importação um exemplo `import ... --dry-run` seguido de `import ... --force`, explicitando quando a autorização de substituição é necessária. | Reduz dúvida no caminho de atualização de receitas sem exigir uma interface nova. |
| N2 | Reaproveitar `why --fields` e `doctor --json` como exemplos de boas interfaces de observabilidade. | Ambos apresentam origem/diagnóstico antes de uma mutação, respeitando o modelo de camadas. |

## Error Message Audit

| Error Scenario | Current Message | What Developer Needs | Suggested Message |
|---|---|---|---|
| `init --add nvim --dry-run` | `✓ schema.toml created with 1 tools` (mensagem montada em `internal/app/init.go:158-164`; não indica preview) | Afirmar que nada foi escrito e apresentar o esquema para inspeção. | `Preview: schema.toml (not written)` seguido do TOML em stdout. |
| `init --interactive` com EOF | `⚠ schema.toml created — no tools added yet...` no ramo sem ferramentas (`:317-325`) | Cancelamento sem arquivo e instrução para CI. | `error: interactive input ended; schema.toml was not written. Next: depengine init --add nvim` |
| `import --dry-run` com destino existente | `error: <path> already exists; pass --force to replace it` (`internal/app/import.go:145-156`) | O preview deve avisar o efeito futuro sem falhar por ainda não haver escrita. | `Would replace: <path> (preview only; no files written). Next: depengine import <recipe.toml> --force` |
| `import` sem root | `error: no catalog root configured; pass --catalog or set DEPENGINE_CATALOG` (`internal/app/import.go:129-131`) | Ação específica para corrigir ambiente. | **Manter:** diagnóstico já oferece alternativas executáveis. |
| `import` com receita executável | `recipe is not declarative: ... Remove the executable fields or keep the recipe out of the catalog` (`internal/app/import.go:251+`) | Entender por que uma receita não pode ser importada. | **Manter:** mensagem tem causa e correção. |

As mensagens acima foram extraídas ou inferidas diretamente dos ramos de código indicados; **não foram capturadas de uma execução real do binário**.

## Developer Journey

**First use:** `depengine init --add ...` cria o scaffold; `validate` verifica o esquema. `init --dry-run` oferece uma promessa falsa de preview; `init --interactive` pode confundir interrupção de entrada com conclusão e gravar o arquivo. Os exemplos no README tornam o caminho principal descobrível.

**Daily use:** `import` valida declaratividade e integridade antes de gravar, com digest/candidatos/origens visíveis; `why --fields` e `doctor` esclarecem a resolução. O preview de uma receita que já existe retorna erro de conflito mesmo sem gravar.

**Power use:** `doctor --json` e `why --json` ajudam em scripts. `import` ainda só produz texto e não respeita os writers da instância Cobra. Um formato JSON explícito deve ser uma evolução compatível, não uma troca silenciosa de streams.

## Verdict Details

- **Error Quality: CONCERNS** — bons diagnósticos de integridade, mas EOF é interpretado como conclusão e o erro de colisão aparece no preview.
- **Defaults & Config: CONCERNS** — `init --dry-run` não honra o contrato da flag; precedência e validação de roots do catálogo estão bem especificadas.
- **Keyboard Experience: CONCERNS** — terminal sem entrada/EOF não cancela corretamente o assistente.
- **Discoverability: PASS** — README, CLI reference, `explain`, `why` e `doctor` documentam os caminhos principais.
- **Workflow Integration: CONCERNS** — `import` sem JSON e writers configuráveis; demais comandos já oferecem padrões reutilizáveis.

## Verification and limits

Auditoria feita por leitura de código e documentação do snapshot restaurado, incluindo alterações locais já presentes. **Nenhum binário ou teste funcional foi executado nesta revisão**. Os problemas C1, C2 e R1 decorrem diretamente do fluxo de controle; antes de corrigir, adicionar os testes correspondentes e rodar a suíte de `internal/app` quando as dependências do módulo estiverem disponíveis.

Nenhum código funcional foi modificado por esta auditoria. Não houve mudanças de arquitetura, criação de ADR ou execução de ações destrutivas.
