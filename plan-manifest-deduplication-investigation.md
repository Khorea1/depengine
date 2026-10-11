# Plano de investigação: reduzir redundância no manifesto sem refatorar o motor

> **Estado: investigação aberta; nenhuma sintaxe ou refatoração aprovada.**
>
> Data: 2026-10-11 · Área: configuração, merge de camadas, métodos e lock.
>
> Este documento registra hipóteses e critérios de decisão, **não** uma ADR aceita, especificação implementável ou autorização para modificar o schema. A fonte de verdade do comportamento continua sendo o código e a documentação versionada da branch de produto.

## Problema observado

Os manifests pessoais de `br` (beads_rust), `omp` (oh-my-pi) e `wt` (worktrunk) descrevem instalações corretas, mas repetem repositório, checksum, scope, entrypoints e opções de extração ao separar assets por OS/arquitetura/libc. No caso analisado: quatro candidatos GitHub para `br`, três para `omp` e dois para `wt`. As diferenças efetivas são bem menores do que as receitas completas.

**Não confundir redução de linhas TOML com redução da complexidade total.** Um mecanismo genérico de herança, seleção de variantes, novos placeholders condicionais ou adaptação do planner pode aumentar o número de estados e invariantes mais do que diminui o custo de manutenção do manifesto. Tampouco se deve eliminar diferenças genuínas de compatibilidade: Linux glibc, Linux musl, Windows e macOS podem demandar regras distintas.

O problema tem três dimensões distintas, que não devem ser confundidas:
1. **Autoria:** valores compartilhados são declarados repetidamente.
2. **Semântica:** candidatos distintos podem representar alternativas reais de instalação *ou* somente distribuições do mesmo release.
3. **Consistência:** configuração, planner, resolução, diagnóstico, estado e lock podem expressar a mesma decisão de maneiras diferentes.

É uma investigação sobre a primeira dimensão, controlando os efeitos nas outras duas; **não** uma autorização para redesenhar todos esses subsistemas.

## Restrições arquiteturais verificadas no snapshot auditado

- O merge de métodos utiliza `Kind + Label` como identidade: `internal/config/manifest.go` (`mergeMethodSlices`). Labels e quantidade de candidatos não são só detalhes visuais.
- O lock v1 usa chaves `<tool>/<method>/<index>`; o hash de métodos considera a sequência de `Kind` e `Label`: `internal/lock/lock.go`. Consolidar candidatos pode mudar o destino de pins.
- O lock universal v2 persiste uma projeção concreta do plano selecionado; equivalência sintática de TOML não implica equivalência de identidade de lock.
- `when` já possui semântica de elegibilidade. Introduzir `targets` ou `variants` com regras novas de precedência seria uma segunda linguagem de seleção.
- `internal/config/parse.go` distingue métodos escalares (`go = "x"`, que podem inferir fallback nativo) de tabelas explícitas (`go = { pkg = "x" }`, sem fallback implícito). É uma inconsistência/decisão de compatibilidade **separada**; não a corrigir silenciosamente na v1.
- O archmap em `architecture/` é um mapa semântico verificado contra revisão anterior: não atualizá-lo com propostas não implementadas.

Esses pontos são evidências do snapshot da auditoria, não garantias de que versões futuras mantenham as mesmas linhas ou contratos. Reconfirmar no HEAD antes da experiência.

## Opções a comparar

| Opção | Modelo | Benefício | Principal custo/risco |
|---|---|---|---|
| A. Manter os candidatos atuais | Sem mudança | Compatibilidade máxima; nenhuma regra nova | TOML redundante |
| B. Especialização por host | Receita-base + `targets`, selecionando um candidato para o host | Um candidato efetivo por método | Seleção adicional no carregamento; validação multiplataforma, merge e identidade/lock delicados |
| C. **Expansão estática de autoria** | Receita-base + `cases`, emitindo todos os candidatos convencionais antes do pipeline | Reduz repetição sem obrigar planner/adapters a compreender variantes | Possíveis colisões com o merge de camadas, labels/ordenação, presença de campos e validação |
| D. Templates/heurísticas/herança ampla | Regras genéricas de geração de configs | Sintaxe potencialmente muito curta | DSL implícita, maior superfície de ambiguidades e bugs |

**Hipótese preferida para prototipagem: C, expansão estática. Não é uma decisão de implementação.**
Se C introduzir regras de merge difíceis ou novas divergências de identidade, permanecer em A é resultado aceitável. B também deve ser testada para comparação justa; a conclusão não deve estar pré-escolhida.

### Exemplo experimental de C

A sintaxe abaixo **não existe** no Depengine atual e não é um contrato proposto como definitivo. `cases` e `label` são nomes provisórios.

```toml
[packages.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }

cases = [
  { label = "github-mac",
    when = { os = ["darwin"] } },
  { label = "github-linux",
    when = { os = ["linux"], libc = ["glibc"] } },
  { label = "github-musl",
    when = { os = ["linux"], libc = ["musl"] },
    asset = "br-{version}-{os_any}_musl_{arch_any}.tar.gz" },
  { label = "github-windows",
    when = { os = ["windows"], arch = ["x86_64"] },
    asset = "br-{version}-{os_any}_{arch_any}.zip",
    entrypoints = { br = "br.exe" } },
]
```

O *resultado esperado* para C são quatro candidatos existentes de tipo `github`, com seus próprios labels e `when`, e campos comuns expandidos; **não** um candidato novo especializado para o computador em execução. Cada candidato continua submetido à ordenação/fallback normal. O exemplo não promete compatibilidade de hashes/pins sem testes.

Ponto decisivo: **em que momento expandir?** Antes do merge pode permitir que `Kind + Label` preserve a identidade atual, mas pode também alterar o significado de overrides ou presença de campos vindos de schema, projeto e manifest. Depois do merge pode evitar algumas colisões, porém exigiria preservar uma representação própria mais tempo. Nenhuma resposta deve ser presumida correta.

## Invariantes exigidas para aprovar qualquer solução

1. Configurações v1 existentes continuam válidas com o mesmo comportamento observável.
2. Regras de `when`, `method_only`, `method_prefer`, merge por `Kind + Label` e fallback não ganham precedência implícita nova.
3. Os candidatos resultantes correspondem, campo por campo, às receitas originais em **todos** os hosts declarados, não só no host de desenvolvimento.
4. Ambiguidades, chaves desconhecidas e targets/cases inválidos falham explicitamente; nenhum caso Windows passa despercebido durante validação Linux.
5. `why`, planner e adapters veem uma única representação efetiva; não há interpretação independente da nova sintaxe fora da normalização.
6. Locks v1/v2 não aplicam um pin ao artefato errado; se atualização/migração for necessária, o procedimento é explícito e testável.
7. `observe` e `remove` continuam operando sobre a identidade e ownership persistidos, sem reescolher arbitrariamente um artefato.
8. A complexidade adicionada permanece localizada; o pipeline de execução não adquire conceitos de `cases` ou `targets`.
9. Não inferir suporte a plataformas não declaradas nem fundir estratégias realmente alternativas (stable/nightly, mirrors, fontes com políticas distintas).

## Prova de conceito mínima, antes de criar uma refatoração

**Experimento 0: baseline.** No HEAD atual, capturar fixtures/goldens de `br`, `omp` e `wt` usando as receitas originais. Abranger Linux glibc/musl, macOS x86_64/arm64, Windows x86_64 e um host explicitamente não suportado (incluindo Android quando pertinente). Capturar ordem, Kind, Label, When, Config, identidade efetiva, plano e projeções de lock. Sem instalação ou rede.

**Experimento 1: expansão estática (C).** Em protótipo isolado, transformar a sintaxe compacta em candidatos tradicionais e comparar contra os goldens **antes** e **depois** do merge schema + manifesto. Testar overrides parciais de base/case, labels duplicados, ausência/sobreposição de condições, métodos alternativos e manutenção dos pins. Nenhum adapter ou planner deve ser modificado para entender `cases`.

**Experimento 2: especialização por host (B).** Protótipo igualmente isolado. Medir como validar hosts inativos, preservar `when` e identidade de candidatos e evitar divergir entre plano, lock e estado. Comparar esforço e novos estados com C.

**Experimento 3: custo total e decisão.** Registrar tabela lado a lado: linhas/duplicações removidas, novas estruturas e regras, arquivos/subsistemas alterados, casos de incompatibilidade, testes adicionais e cenários de rollback. Incluir explicitamente a opção A. Selecionar a abordagem que reduz **complexidade operacional e de manutenção**, não a que produz o menor TOML.

### Critérios de parada (*no-go*)

- Necessidade de introduzir uma segunda semântica de seleção/fallback.
- Necessidade de mudar adapters ou reconstruir identidades no executor para atender à sintaxe compacta.
- Alteração de pins/locks sem migração confiável.
- Validação que depende apenas do host atual.
- Regras surpreendentes de herança, merge parcial de mapas ou ordem de declaração.
- Economia pequena de autoria diante de regras permanentes substanciais.

Se ocorrer qualquer um desses riscos sem mitigação simples e testada, **não promover a proposta para ADR, schema ou roadmap**.

## Escopo separado: problemas concretos da auditoria

As observações abaixo são independentes desta investigação e não justificam agrupar tudo em uma grande refatoração:

- **`dep-laz`**: permitir `.exe` portátil em `github/http` sem executar instaladores; avaliar reutilização explícita do campo `binary` e limites do contrato atual.
- **`dep-d1g`**: diferenciar verificação de integridade de confiança na origem do checksum; filtrar avisos pelo comando/ferramenta.
- **`dep-4x1`**: fazer `why` renderizar a identidade já resolvida, não recalcular overrides.
- **`dep-1n0`**: motivou a investigação sobre compactação de candidatos GitHub. O ID consta da auditoria local anterior; **não pressupor que essas quatro issues já estejam sincronizadas ao Beads da branch `dev-notes`**.

A semântica diferente entre atalhos escalares e tabelas explícitas constitui investigação distinta, potencialmente ligada a uma versão futura do formato. Não a alterar na v1 por acidente.

## Status e próxima decisão

**Decisão atual: nenhuma refatoração aprovada.** A expansão estática é uma hipótese com boas propriedades potenciais, mas não há prova de que preserve merge, identidade, locks e comportamento. A alternativa correta pode ser conservar as receitas atuais.

Próximo passo, *somente quando a investigação for retomada*: executar os experimentos, anexar os resultados comparativos e então aceitar ou rejeitar uma proposta pequena e delimitada. Não criar uma ADR de aprovação com base apenas nesta nota.

*Contexto de origem: auditoria do manifesto pessoal de 2026-10-11 e revisão crítica sucessiva das propostas `asset_variants`, `variants`, especialização por host e expansão estática.*
