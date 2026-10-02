# Depengine — plano extensivo de correção das issues abertas

**Baseline auditada:** `754e0b25b69d08d05f24986eeb783c0ba86d4a54` (`master`)  
**Escopo:** issues `#98` a `#136`  
**Público-alvo:** agentes/LLMs de baixa capacidade executando tarefas pequenas e verificáveis  
**Princípio:** corrigir o defeito existente com a menor mudança arquitetural suficiente. Não aproveitar a tarefa para “melhorar” subsistemas adjacentes sem relação com o aceite.

## Progresso acumulado (`fix/open-issues-wave-b`)

Implementação iniciada sobre a baseline auditada do plano. A primeira onda foi aplicada como um lote de baixo conflito:

- [x] `#127` — gate de `gofmt` em CI e hooks; tree existente formatado.
- [x] `#109` — `distro_version_min/max` passam a ser decodificados como strings.
- [x] `#118` — erros não cíclicos do sorter não são mais reportados como `E_CYCLE`.
- [x] `#120` — validação de `distro_family` usa o vocabulário canônico de `platform` e a mesma normalização case-insensitive do runtime.
- [x] `#125` — selector de bucket é válido quando há interseção com ao menos um método declarado.
- [x] `#131` — hints/leftovers determinísticos; `Tool.Ecosystem` removido após confirmação de ausência de consumidores semânticos.
- [x] `#132` — discovery de schema ficou puro; ambiguity warning é emitido somente na invocação de comando que usa auto-discovery.

Onda B aplicada nesta branch:

- [x] `#110` — layering passou a usar presença declarada para defaults/tool/method metadata; `requires_when` usa map merge; project+manifest usam o mesmo fact map no loader host-aware.
- [x] `#126` — schema consumido por fluxos de projeto é resolvido para arquivo absoluto/canônico antes de parse, lock e persistência.
- [x] `#113` — `loadProject` centraliza facts, parse, manifest merge e validação para install/check/status/graph/why e fluxos adjacentes.
- [x] `#114` — `newProjectExecutor` liga o native adapter ao clan detectado nos caminhos read-only.
- [x] `#128` — provider AUR concreto é persistido no state; executores de projeto respeitam `aur_helper`; remove/undo reconstroem AUR pelo provider histórico e falham fechado quando ele é desconhecido.
- [x] `#129` — `--check-env` deriva executáveis nativos do registry via `ManagerExecutableNames`, preservando apenas language/system tools como suplementos explícitos.
- [x] `#133` — state version probes passam por `probeRunner`.
- [x] `#104` — finalização/bookkeeping usa o housekeeping context sem schema-secret env, inclusive em resolved upgrade candidate.

Validação local executada nesta branch: `scripts/check-gofmt.sh`, `git diff --check` e `go test ./internal/native`. O gate Go completo continua bloqueado porque o sandbox recebeu toolchain e `GOCACHE`, mas não o source module cache de `go-toml`, Cobra/pflag e não possui rede para materializá-lo. `deadcode ./...` alcançou a mesma limitação de imports externos. Portanto `go build ./...`, `go test -race ./...`, `go vet ./...` e `golangci-lint run` permanecem obrigatórios antes de integração.

---

## 0. Regras de execução para todos os agentes

Estas regras fazem parte do plano. Um agente que não as seguir não deve ter sua alteração integrada, mesmo que um teste isolado passe.

1. Trabalhar sempre sobre a baseline indicada acima ou sobre uma branch que contenha apenas as dependências explicitamente listadas neste documento.
2. Antes de editar, abrir os arquivos e símbolos indicados na seção da issue. Não assumir que o título da issue descreve integralmente o código atual.
3. Não criar um segundo mecanismo quando já existir uma abstração equivalente. Preferir estender helpers existentes, contratos de `methodkind`, `plan`, `state`, `run` e opções de `Executor`.
4. Não mover responsabilidade entre pacotes sem necessidade. Respeitar a direção arquitetural documentada em `AGENTS.md`: `config` não depende de `exec`; processos externos passam por `internal/run.Runner`; `state` continua dono de persistência/locking; `native` continua dono do conhecimento de package managers nativos.
5. Não enfraquecer validação ou testes para fazer uma alteração passar. Em especial, não transformar falha fechada em warning apenas para preservar compatibilidade.
6. Toda alteração de comportamento deve vir com teste de regressão que falhe na baseline e passe depois do fix.
7. Se a correção altera schema, estado, lock ou JSON público, adicionar teste de compatibilidade com documentos antigos quando o formato antigo continuar suportado.
8. Não misturar formatação/refactors não relacionados no mesmo patch, exceto a execução obrigatória de `gofmt` nos arquivos Go tocados.
9. Ao terminar uma issue, executar no mínimo:
   - testes do pacote alterado;
   - `go test -race ./...`;
   - `go vet ./...`;
   - `golangci-lint run`.
10. Depois de `#127`, qualquer patch também deve passar o gate de `gofmt` sem arquivos pendentes.
11. Um agente não deve “resolver” uma condição de corrida removendo concorrência, nem “resolver” um erro de rollback ignorando o erro.
12. Quando o plano abaixo disser **não fazer**, trate isso como restrição de escopo, não como sugestão.

### Convenção para snippets

Pseudocódigo usa nomes próximos aos símbolos atuais, mas não deve ser copiado cegamente. O agente deve ajustar assinaturas ao código real e manter imports/ciclos de pacote válidos.

---

## 1. Ordem recomendada de implementação

A ordem abaixo reduz conflitos e evita construir fixes em cima de comportamentos que serão removidos logo depois.

### Onda A — guardrails e parser simples

1. `#127` gofmt em CI/hook.
2. `#109` decode de `distro_version_min/max`.
3. `#118` falso `E_CYCLE`.
4. `#120` vocabulário de `distro_family`.
5. `#125` seletores de buckets.
6. `#131` diagnósticos determinísticos.
7. `#132` warning de múltiplos schemas.

### Onda B — layering e composição de comandos

8. `#110` manifest layering. **Fazer antes de #113/#128.**
9. `#126` path absoluto do schema.
10. `#113` loader compartilhado para status/graph/why.
11. `#114` binding do native adapter nos caminhos read-only.
12. `#128` `aur_helper` consistente.
13. `#129` `--check-env` derivado do registry.
14. `#133` `probeRunner` no version probe.
15. `#104` contexto sem secrets no bookkeeping.

### Onda C — execução, relatório e estado

16. `#111` closure de `requires_when`.
17. `#115` contadores de security-block.
18. `#112` exit code de execução.
19. `#116` cancelamento e scheduling.
20. `#117` `exec.ErrWaitDelay`.
21. `#103` lock cancelável e segundo sinal.
22. `#122` snapshot sob lock + publicação atômica. **Implementar depois de #103**, para usar a API contextual de lock nova.
23. `#121` erros de lockfile em status/SBOM.
24. `#119` fail-closed do lock v1 legado.

### Onda D — HTTP/GitHub, integridade e lifecycle

25. `#101` validação de `binary`.
26. `#99` integridade em `repo+asset`.
27. `#102` identidade do signer.
28. `#124` placeholders em `repo+asset`.
29. `#130` identidade GitHub owner/repo.
30. `#108` budget de download.
31. `#105` budget em `.bz2` standalone.
32. `#123` lifecycle package-aware de `.deb`.
33. `#135` propagação de rollback/cleanup.
34. `#106` ownership/mode de payload elevado. **Depois de #135**, porque falhas de normalização precisam usar o rollback já corrigido.
35. `#100` ownership seguro de `extract_to`. **Depois de #135/#106**, para não duplicar transaction handling.
36. `#107` fallback seguro para state/cache.
37. `#136` apenas cobertura de regressão/fechamento, salvo se a revalidação detectar regressão funcional; o comportamento principal já está presente na baseline atual.

### Onda E — refactor isolado de Scoop

38. `#98` boundary `ScoopRuntime`. Fazer por último ou em branch isolada, porque toca `internal/source` e `internal/exec` e pode conflitar com #103/#134.

### Onda transversal

39. `#134` output concorrente pode ser executada depois de `#116` e antes de `#98`. Evitar paralelismo com agentes alterando `internal/exec/preparation.go` ou `hooks.go`.

---

## 2. Mapa de arquivos com alto risco de conflito entre agentes

Não delegar simultaneamente issues da mesma linha sem branches/ordem explícitas.

| Arquivo/subsistema | Issues que colidem diretamente |
|---|---|
| `internal/config/parse.go`, `manifest.go`, `model.go`, `decode.go` | #109, #110, #124, #125, #131 |
| `internal/app/helpers.go`, loaders CLI | #110, #113, #126, #132 |
| `internal/app/status.go`, `graph_why.go`, `validate_check.go` | #113, #114, #121, #128 |
| `internal/exec/state.go` | #104, #128, #133 |
| `internal/exec/execute.go`, `run.go` | #111, #116, #134 |
| `internal/state/*` | #103, #107, #122 |
| `internal/httpdownload/adapter.go` | #99, #100, #101, #102, #108, #123 |
| `internal/httpdownload/archive_install.go` | #100, #106, #135 |
| `internal/httpdownload/archive_native.go` / `extract.go` | #105, #136 |
| `internal/source/manager.go` | #98, #103, #134 |

---

# Issue-by-issue

## #98 — Decouple Scoop semantics from the concrete `scoop.exe` runtime

### Objetivo

Separar a semântica lógica do método `scoop` da implementação oficial `scoop.exe`, sem adicionar suporte a `hok`, sem criar um framework genérico de providers e sem alterar a identidade pública do método `scoop`.

### Estado atual e pontos de entrada

- `internal/exec/win.go:14-355`: `winAdapter` contém branches `w.kind == "scoop"` e constrói diretamente `scoop list/install/uninstall`.
- `internal/source/manager.go:276-313`: presença de `scoop-bucket` chama diretamente `scoop bucket list`.
- `internal/source/manager.go:369-425`: resolução de repositório usa `scoop prefix scoop` e parsing do layout interno.
- `internal/source/manager.go:483-539`: add de bucket usa diretamente `scoop bucket add`.
- `internal/source/manager.go:598-618`: remove usa diretamente `scoop bucket rm`.
- `internal/exec/win_test.go` e `internal/source/manager_test.go` já possuem boa cobertura da superfície oficial. Aproveitar esses testes, não substituí-los por mocks vagos.

### Design obrigatório

Criar um pacote Scoop-específico sem dependência de `exec` ou `source`, por exemplo `internal/scoop`. Esse pacote contém o boundary concreto compartilhado pelos dois consumidores.

Estrutura recomendada:

```text
internal/scoop/
  runtime.go          // interface + requests/results/capabilities
  official.go         // tradução para scoop.exe
  official_test.go
```

A interface deve ser pequena e Scoop-específica. Exemplo conceitual:

```go
type Capabilities struct {
    ExactVersion     bool
    BucketSelection  bool
    Scope            bool
    Architecture     bool
    Removal          bool
    BucketMutation   bool
    BucketRepository bool
}

type PackageRequest struct {
    Package      string
    Version      string
    Bucket       string
    Scope        string
    Architecture string
}

type InstalledPackage struct {
    Present bool
    Package string
    Version string
    Bucket  string
    Scope   string
}

type Runtime interface {
    Available(ctx context.Context, rn run.Runner) bool
    Capabilities() Capabilities
    ObserveInstalled(ctx context.Context, rn run.Runner, req PackageRequest) (InstalledPackage, error)
    Install(ctx context.Context, rn run.Runner, req PackageRequest) error
    Remove(ctx context.Context, rn run.Runner, req PackageRequest) error
    BucketList(ctx context.Context, rn run.Runner) ([]Bucket, error)
    BucketAdd(ctx context.Context, rn run.Runner, bucket Bucket) error
    BucketRemove(ctx context.Context, rn run.Runner, name string) error
    BucketRepository(ctx context.Context, rn run.Runner, name string) (string, error)
}
```

Não é obrigatório usar exatamente esses tipos/nome, mas a separação de responsabilidade deve ser equivalente.

### Tarefas de implementação

1. Criar `internal/scoop` com tipos que não dependam de `config.MethodCandidate` nem de `exec.AdapterV2`.
2. Mover para `OfficialRuntime` toda construção/parsing de comandos exclusivos de Scoop hoje espalhada em `internal/exec/win.go` e `internal/source/manager.go`.
3. Mover `scoopPackageFromOutput` para o pacote Scoop ou encapsular o parsing dentro de `ObserveInstalled`.
4. Mover a lógica de `scoop prefix scoop` e derivação `<root>/buckets/<name>` para `OfficialRuntime.BucketRepository`.
5. Separar `scoop` de `winAdapter`. O caminho mais simples é manter `winAdapter` apenas para Chocolatey e criar um `scoopAdapter` que delega a `scoop.Runtime`.
6. `scoopAdapter` continua responsável por:
   - traduzir `config.Tool`/`MethodCandidate` para `scoop.PackageRequest`;
   - traduzir `InstalledPackage` para `plan.Observation`;
   - validar/reconciliar o `ResolvedInstallPlan`;
   - manter `Kind() == "scoop"`.
7. Fazer a seleção do runtime antes de qualquer mutação. Nesta issue existe apenas um runtime concreto, portanto a seleção default é `OfficialRuntime`.
8. Não implementar fallback de runtime após `Install`/`BucketAdd` começar. A API deve permitir no futuro selecionar outro runtime, mas o fluxo atual não deve conter retry cross-runtime.
9. Permitir que `source.Manager` receba o runtime selecionado. Preferir option explícita, por exemplo `source.WithScoopRuntime(rt)`, mantendo `OfficialRuntime` como default para call sites não migrados.
10. Garantir que o mesmo runtime selecionado pelo executor seja fornecido ao `source.Manager` do mesmo run. Se isso exigir um campo/opção no `Executor`, adicionar uma opção Scoop-específica em vez de usar variável global mutável.
11. Substituir os branches `scoop-bucket` de `source.Manager` por chamadas ao runtime. O manager ainda controla WAL, credentials, ownership e ordem de operações; o runtime apenas traduz a operação Scoop.
12. Implementar `Capabilities()` no runtime oficial cobrindo exatamente a superfície já suportada: versão exata, bucket, scope, architecture, remove e metadata/revision de bucket.
13. Validar capabilities antes da primeira mutação. Se uma futura implementação não suportar um campo requerido, rejeitar o candidato antes de `BucketAdd`/`Install`.
14. Não alterar schema, `methodkind.Kind`, lock identity ou state identity para registrar o nome do runtime. Runtime é detalhe de execução, não novo método.

### Testes obrigatórios

- Portar os testes de comando de `internal/exec/win_test.go` para `internal/scoop/official_test.go` quando testarem tradução do CLI, preservando testes de adapter para a tradução semântica.
- Preservar todos os casos atuais: version, bucket, `--global`, `--arch`, install, observe, remove.
- Portar testes de `scoop bucket list/add/rm/prefix` de `internal/source/manager_test.go` para o runtime oficial, mantendo testes no manager que provem que ele chama a abstração.
- Adicionar fake runtime que conte chamadas para provar que package install e source management usam o runtime injetado, não `scoop.exe` hard-coded.
- Teste de capability: runtime fake sem `Architecture`; candidato com architecture deve falhar antes de chamar `Install`.
- Teste de não-fallback: runtime selecionado retorna erro depois de iniciar install; nenhum segundo runtime deve ser consultado.

### Critérios de aceite

- `rg '"scoop"' internal/exec/win.go internal/source/manager.go` não deve encontrar construção de comandos Scoop fora do boundary novo, exceto nomes de kind/source e mensagens de erro.
- O runtime oficial reproduz exatamente os argv atuais.
- `scoop` continua sendo o único method kind exposto.
- Nenhuma dependência `internal/scoop -> internal/exec` ou `internal/scoop -> internal/source`.

### Não fazer

- Não adicionar `hok`.
- Não criar `Provider`, `Runtime` ou registry genérico para todos os adapters.
- Não alterar capabilities do método Scoop para caber em uma implementação hipotética.

---

## #99 — Verify declared integrity metadata for GitHub `repo+asset` downloads

### Objetivo

Fazer `checksum`, `checksum_url`, `checksum_file_format`, `signature_url` e `signing_key` sobreviverem ao planejamento de `github = { repo, asset, ... }` e chegarem intactos à verificação do asset resolvido.

### Estado atual

- `internal/planner/artifact.go:70-75`: quando não há `url` e existe `asset`, o planner adiciona somente a operação `resolve-artifact` e retorna sem criar `plan.Artifact`.
- `internal/httpdownload/adapter.go:44-64`: `resolveDownloadPlan` cria `plan.Artifact{URL: resolvedURL}` quando o intent não contém artifacts, descartando metadata de integridade declarada.
- `internal/planner/artifact_test.go` contém testes que hoje codificam a ausência de artifact em alguns casos `repo+asset`; essas expectations precisam ser atualizadas, não contornadas.

### Tarefas de implementação

1. Alterar `planner.applyArtifact` para sempre projetar um `plan.Artifact` para contratos remotos quando há `repo+asset`, mesmo antes de conhecer a URL concreta.
2. Para `repo+asset`, preencher o artifact com URL vazia e os campos já disponíveis:
   - `Checksum`;
   - `ChecksumURL`;
   - `ChecksumFileFormat`;
   - `SignatureURL`;
   - `SigningKey`.
3. Manter `resolve-artifact` em `p.Operations`; ele ainda representa a resolução read-only da URL.
4. Verificar `plan.Artifact.Validate`/`ResolvedInstallPlan.Validate`: URL vazia deve ser aceita apenas para intent ainda não resolvido quando a operação `resolve-artifact` existe. Se a validação atualmente exigir URL, ajustar a validação de forma estreita, sem permitir artifact sem origem em qualquer outro contexto.
5. Em `resolveDownloadPlan`, remover a dependência semântica de “zero artifacts significa repo+asset”. O caminho esperado passa a ser:

```text
intent.Artifacts[0] existe com metadata de integridade
ResolveArtifactDetails resolve URL/tag
clone intent
clone.Artifacts[0].URL = resolvedURL
retorna clone
```

6. Manter um fallback defensivo para intents legados sem artifact apenas se existirem call sites internos/testes antigos que ainda os fornecem diretamente. Esse fallback não deve ler metadata do `MethodCandidate` silenciosamente; idealmente ele deve ser marcado como compatibilidade e testado.
7. Garantir que `InstallResolved` continua consumindo exclusivamente o artifact resolvido, sem reconsultar config stale. O teste existente de `resolved_install_test.go` deve continuar provando isso.
8. Verificar lock v2: `plan.LockArtifact` já contém metadata de checksum/signature. Adicionar teste de round-trip para repo+asset com metadata para garantir que a projeção não a perde.

### Testes obrigatórios

- `internal/planner/artifact_test.go`: repo+asset com checksum literal gera um artifact com URL vazia + checksum.
- Mesmo teste com `checksum_url`, format, `signature_url`, `signing_key`.
- `internal/httpdownload/resolved_install_test.go`: após resolver repo+asset, URL muda para a URL concreta e os campos de integridade permanecem idênticos.
- Teste end-to-end do adapter com resolver fake/HTTP server onde checksum correto passa e incorreto falha.
- Teste de detached signature em repo+asset. Pode reutilizar seams de GPG existentes; não exigir GPG real se os testes atuais já mockam runner.
- Teste lock projection/round-trip com metadata.

### Critérios de aceite

- Não existe caminho normal de `repo+asset` que produza um resolved artifact sem metadata declarada.
- Literal `url` e `repo+asset` passam pela mesma função de verificação após resolução.
- Nenhuma re-leitura de `checksum`/signature do método original depois de existir um `ResolvedInstallPlan`.

---

## #100 — Do not recursively delete non-system `extract_to` directories on remove

### Objetivo

Proibir a remoção recursiva de diretórios cujo ownership pelo tool não seja comprovado. `extract_to` não deve ser considerado “tool-owned” apenas por não estar na allowlist de diretórios de sistema.

### Estado atual

- `internal/httpdownload/archive_install.go:70-74` rejeita apenas destinos reconhecidos por `isSharedDir`.
- `internal/httpdownload/adapter.go:635-660` mantém uma lista estreita de shared dirs.
- `internal/httpdownload/adapter.go:662-767` pode remover recursivamente `extract_to` quando não está nessa lista.
- Isso viola diretamente o contrato de `AGENTS.md`: remoção não pode apagar parent compartilhado.

### Design obrigatório

Adicionar prova explícita de ownership para payloads de archive, em vez de tentar ampliar indefinidamente `isSharedDir`.

Estratégia recomendada: marker de ownership criado no payload staging antes do commit. Exemplo interno, não configurável pelo usuário:

```json
{
  "format": 1,
  "tool": "ripgrep",
  "method_kind": "github"
}
```

Nome sugerido: `.depengine-owner.json` dentro da raiz do payload.

O marker não substitui state. Ele é apenas uma prova local necessária antes de `RemoveAll` daquele diretório.

### Tarefas de implementação

1. Criar helper pequeno em `internal/httpdownload`, por exemplo `payloadOwnershipMarker` + `writePayloadOwnership` + `verifyPayloadOwnership`.
2. O conteúdo mínimo deve identificar tool e method kind/candidate suficiente para impedir que um tool remova payload de outro. Não incluir secrets, URLs autenticadas ou dados mutáveis desnecessários.
3. Em `installArchive`, gravar o marker no `payload` staging **antes** de `commitPayload`.
4. Antes de substituir um `dest` já existente, se ele possuir marker:
   - marker correspondente ao mesmo tool/candidate: permitir update/replacement;
   - marker de outro tool: falhar antes de mutação.
5. Se `dest` preexistir sem marker, não assumir ownership. Falhar com mensagem orientando o usuário a escolher diretório dedicado ou remover/migrar manualmente. Não fazer backup/replace de diretório arbitrário.
6. Em `HTTPAdapter.Remove`:
   - se o alvo é payload archive e `verifyPayloadOwnership` confirma o tool, remover a raiz do payload;
   - se marker está ausente/malformado/diferente, retornar erro e não remover recursivamente;
   - entrypoint/link cleanup pode ocorrer somente para links cuja ownership já é validada pelos helpers existentes.
7. Para artefatos single-file/plain binary em `extract_to`, remover apenas o arquivo exato (`extract_to/<binary ou tool>`), nunca o diretório pai.
8. Para `.bz2` standalone, tratar como single-file.
9. Não usar `isSharedDir` como prova positiva de ownership. Ele pode continuar como early rejection para instalação em roots obviamente compartilhados, mas remoção recursiva exige marker.
10. Se houver install legado sem marker, falhar fechado na remoção recursiva. Não “migrar” marcando diretório existente automaticamente, pois isso transformaria conteúdo arbitrário em owned.
11. Documentar o comportamento de instalações legadas: remoção pode exigir reinstalação/migração ou cleanup manual.

### Testes obrigatórios

- Diretório `extract_to` com arquivo alheio + payload single-file: remove somente o payload, preserva arquivo alheio.
- Archive marcado para tool A: remove raiz inteira.
- Archive marcado para tool B: tool A recebe erro e nenhum arquivo é apagado.
- Diretório preexistente sem marker: `installArchive` falha antes de backup/mutation.
- Update do mesmo tool com marker válido mantém fluxo backup/commit/rollback.
- Marker corrompido: fail closed.
- Testes elevados devem verificar que a validação de marker acontece antes de `rm -rf`.

### Critérios de aceite

- Não existe `RemoveAll(extract_to)` baseado apenas em path heuristics.
- Toda remoção recursiva de payload HTTP/GitHub exige prova explícita de ownership.
- Shared parent nunca é apagado por um single-file install.

---

## #101 — Reject path traversal in HTTP/GitHub `binary` targets

### Objetivo

Definir `binary` como basename portátil e rejeitar qualquer valor que possa escapar de `extract_to` ou produzir interpretação diferente entre Unix e Windows.

### Estado atual

- `internal/httpdownload/extract.go:178+`: `copyBinary` constrói `filepath.Join(destDir, binaryName)` sem validação prévia suficiente.
- `internal/httpdownload/adapter.go:132-146`: `Check` usa o mesmo valor em join.
- `internal/httpdownload/adapter.go:662-733`: remove volta a construir o target a partir de `binary`.
- Archives passam por `requirePayloadFile/safeJoin`, mas plain binary e lifecycle ainda precisam de uma regra única.

### Contrato a implementar

`binary` é um nome de arquivo, não um path. Aceitar exemplos como `rg`, `foo.exe`, `node-20`; rejeitar:

- string vazia quando o campo foi explicitamente declarado;
- `.` e `..`;
- `/abs/path`;
- `../tool`, `a/b`;
- `..\\tool`, `a\\b`;
- `C:\\tool.exe`, `C:tool.exe`;
- NUL;
- qualquer `/` ou `\\` em qualquer plataforma.

Não “corrigir” um valor perigoso com `filepath.Base`. Rejeitar.

### Tarefas de implementação

1. Criar helper compartilhado em pacote abaixo de planner/adapter, preferencialmente `internal/artifact`, por exemplo `ValidatePortableBasename(name string) error`.
2. A validação deve ser independente do GOOS do host. Não usar apenas `filepath.IsAbs`, pois em Linux ele não reconhece todas as formas Windows.
3. Regras mínimas:

```text
name != "" quando configurado
name != "." && name != ".."
não contém NUL
não contém '/' nem '\\'
não possui prefixo de volume Windows tipo X:
```

4. Integrar no validation/planner para que schema inválido falhe antes de seleção/mutação.
5. Integrar também defensivamente em `copyBinary`, `Check` e `Remove`, pois adapters podem ser chamados diretamente em testes/uso interno sem passar pelo parser CLI.
6. Usar o valor validado sem normalização posterior.
7. Se `entrypoints` possuem contrato semelhante para o nome do launcher, não misturar a correção aqui; preservar a validação existente e só reutilizar o helper se as regras forem literalmente iguais.

### Testes obrigatórios

- Tabela cross-platform com todos os valores maliciosos acima.
- Valores válidos com `.`, `_`, `-` internos e extensão `.exe`.
- `ParseProjectSchema`/validation rejeita schema antes do executor.
- Chamada direta de `copyBinary` rejeita traversal e não cria arquivo fora do tempdir.
- `Remove` com config hostil não remove arquivo sentinela fora de `extract_to`.
- Executar testes em Unix e nos targets Windows existentes, porque essa validação deve ter semântica idêntica.

### Critérios de aceite

- Install/check/remove usam uma única regra de `binary`.
- Nenhum path hostil é normalizado para um target aparentemente seguro.

---

## #102 — Require signer identity when `signature_url` is configured

### Objetivo

Tornar `signature_url` fail-closed por default: detached signature sem identidade de signer não pode parecer uma verificação forte. Preservar compatibilidade somente mediante opt-in explícito e visível.

### Estado atual

- `internal/integrity/gpg.go:62-90`: `signingKey == ""` usa keyring default e aceita qualquer signer válido.
- `internal/validate/semantic.go:248-268`: isso gera apenas `W_SIGNATURE_NO_KEY`.
- `internal/methodkind/methodkind.go:129-139`: campos de artifact incluem `signature_url`/`signing_key`.
- `plan.Artifact` e lock projection já transportam metadata de assinatura.

### Decisão de contrato

Adicionar escape hatch explícito, sugerido `allow_unpinned_signer = true`, default `false`.

Esse nome pode ser ajustado, mas deve ser inequívoco. Não usar uma opção genérica como `insecure = true` que misture políticas diferentes.

Sem `signing_key`:

```text
signature_url presente + allow_unpinned_signer false/ausente => erro de validação
signature_url presente + allow_unpinned_signer true => warning visível + keyring default permitido
signature_url ausente => opção deve ser rejeitada ou ignorada com erro; preferir rejeitar configuração sem efeito
```

### Tarefas de implementação

1. Adicionar o campo bool ao contrato de methods artifact que suportam detached signature.
2. Marcar efeitos no `methodkind.Field` como `Validate | Execute` e `Resolve` se o valor for projetado para o plan.
3. Adicionar `AllowUnpinnedSigner bool` em `plan.Artifact` e no equivalente de lock projection. Usar `omitempty`; documento antigo sem campo significa `false`.
4. Projetar o valor em `planner.applyArtifact` tanto para `url` quanto `repo+asset`.
5. Atualizar conversion de `ResolvedInstallPlan` para config efetiva em `internal/httpdownload/adapter.go:204-223` para transportar a flag.
6. Atualizar `validateSignatureSecurity`:
   - substituir warning default por erro quando URL existe sem key e sem opt-in;
   - manter `W_SIGNATURE_NO_KEY` somente quando o opt-in explícito está presente;
   - rejeitar opt-in sem `signature_url` para impedir configuração morta.
7. Alterar `integrity.GPGVerify` para receber explicitamente a política. Não inferir permissão apenas porque `signingKey` está vazio.
8. Antes de chamar `gpg`, se key vazia e opt-in false, retornar erro. Isso é defesa em profundidade para call sites internos.
9. Somente o ramo opt-in usa `gpg --verify` no keyring compartilhado.
10. Atualizar wrapper `internal/httpdownload/gpg.go` e todos os call sites.
11. Atualizar docs/schema examples com destaque de que a exceção reduz garantia de identidade.
12. Garantir que a flag não contenha secret nem altere o resolved signer quando key existe.

### Testes obrigatórios

- `signature_url` sem key/opt-in => `E_INVALID_VALUE` (ou novo error code específico, se o projeto preferir) e exit 2 no validate.
- key presente => nenhum warning.
- opt-in true sem key => somente warning e plan contém flag.
- opt-in true sem `signature_url` => erro.
- `GPGVerify` direto não executa `gpg` quando política está ausente.
- `GPGVerify` com opt-in usa shared keyring.
- Lock v2 antigo sem flag continua desserializando como false.

### Não fazer

- Não aceitar “qualquer signer” silenciosamente.
- Não remover a verificação de fingerprint do caminho com `signing_key`.

---

## #103 — Make state lock acquisition cancellable and restore second-signal termination

### Objetivo

Permitir que espera por state lock respeite `context.Context`, e restaurar o comportamento padrão de SIGINT/SIGTERM após o primeiro sinal cancelar o comando.

### Estado atual

- `internal/state/lock_unix.go:27-66`: `flock` bloqueante, retry infinito de `EINTR`.
- `internal/state/lock_windows.go:59-110`: `LockFileEx` também bloqueante.
- `internal/state/state.go:211-251`: `LoadLocked`, `LoadShared`, `SaveLocked` não recebem contexto.
- `internal/exec/preparation.go:228-270`: source preparation segura lock exclusivo ao redor de mutação externa.
- `internal/exec/preparation.go:451-471`: já existe `suspend/resume`, portanto não criar mecanismo paralelo.
- `main.go:24-29`: `signal.NotifyContext` só chama `stop` no defer final.

### Parte A — API contextual de lock

1. Introduzir APIs:

```go
LoadLockedContext(ctx context.Context) (*LockedState, error)
LoadSharedContext(ctx context.Context) (*LockedState, error)
SaveLockedContext(ctx context.Context, st *State) error
```

2. Manter wrappers sem contexto temporariamente, se muitos testes/call sites internos dependem deles:

```go
func LoadLocked() (...) { return LoadLockedContext(context.Background()) }
```

3. Migrar paths de CLI/executor que já possuem contexto para as novas APIs. Prioridade: install, remove, status, undo, preparation/recovery.

### Parte B — Unix

4. Trocar flock bloqueante por tentativas `LOCK_NB`.
5. Em `EWOULDBLOCK/EAGAIN`, aguardar com ticker curto, por exemplo 50 ms, usando `select` entre ticker e `ctx.Done()`.
6. Em `EINTR`, verificar primeiro `ctx.Err()` e então repetir.
7. Fechar o fd ao cancelar/falhar.
8. Não usar goroutine que chama `flock` bloqueante e fica vazando depois do cancelamento.

Pseudocódigo:

```text
open lock file
loop:
  if ctx.Err != nil: close; return ctx.Err
  flock(mode|LOCK_NB)
  success => return lock
  EINTR => continue
  EWOULDBLOCK/EAGAIN => select ctx.Done or ticker
  outro => close; return erro
```

### Parte C — Windows

9. Definir `LOCKFILE_FAIL_IMMEDIATELY` e fazer polling equivalente com `LockFileEx` não bloqueante.
10. Tratar especificamente `ERROR_LOCK_VIOLATION` como “ocupado”; outros erros são fatais.
11. Garantir que shared/exclusive conservam a semântica atual.

### Parte D — duração do lock durante mutações

12. Auditar trechos que seguram lock durante subprocesso/rede. **Não liberar automaticamente um lock que protege um WAL em estado `applying`**, porque outro processo poderia entrar na recovery enquanto a mutação ainda está ocorrendo.
13. Reutilizar `sourcePreparationTransaction.suspend/resume` somente em regiões onde não há mutação WAL in-flight. O uso atual ao instalar method prerequisites é o modelo.
14. Para source `AddAuthenticated` entre `PlanPreparationApply` e `RecordPreparationApplied`, manter a exclusão se o protocolo atual não possui lease/owner que impeça recovery concorrente. A correção desta issue não deve quebrar atomicidade para reduzir tempo de lock.
15. Garantir que toda mutação externa mantida sob lock tenha timeout/context finito. Se algum call site usa `context.Background`, corrigi-lo.

### Parte E — segundo sinal

16. Em `main.go`, após o primeiro cancelamento, chamar `stop()` imediatamente para desregistrar o handler e restaurar default signal handling.
17. Forma segura:

```go
ctx, stop := signal.NotifyContext(...)
defer stop()
go func() {
    <-ctx.Done()
    stop()
}()
```

18. `stop()` é idempotente; não criar outro canal de signal concorrente.

### Testes obrigatórios

- Unix: processo A segura lock; processo B chama `LoadLockedContext` com timeout curto; recebe `context deadline exceeded` sem aguardar A liberar.
- Shared vs exclusive ainda obedecem regras.
- Cancelar enquanto polling não deixa fd/goroutine vazando.
- Windows: teste equivalente usando seam ou integração específica da plataforma.
- Processo real da CLI: segurar lock, iniciar comando que espera, enviar primeiro SIGINT e verificar cancelamento; enviar segundo sinal se ainda preso e verificar terminação imediata/default.
- Recovery/preparation existentes continuam passando sob `-race`.

### Critérios de aceite

- Nenhum path principal de CLI fica indefinidamente bloqueado apenas esperando lock depois de `ctx.Done()`.
- Segundo SIGINT/SIGTERM não é consumido pelo handler de graceful shutdown.
- WAL/recovery invariants não são enfraquecidos.

---

## #104 — Strip schema secret environment from state version probes

### Objetivo

Garantir que version probes executados durante persistência de estado herdem o mesmo contexto com env omitido usado no housekeeping do executor.

### Estado atual

- `internal/exec/execute.go:72-103`: cria `housekeepingCtx`, mas chama `finishRun(ctx, ...)` com o contexto original.
- `internal/exec/state.go:23-36`: `installedVersion` executa subprocesso via runner.
- `internal/exec/state.go:91-131`: `writeState` chama `toolStateForResult` e eventualmente `installedVersion`.

### Tarefas de implementação

1. Em `Executor.Execute`, passar `housekeepingCtx` para o caminho de finalização/persistência, não `ctx` original.
2. Auditar `ExecuteResolvedCandidate` em `internal/exec/resolved_candidate.go`: ele também cria housekeeping context e possui chamadas a `finishRun`; alinhar os dois caminhos.
3. Não remover secrets do ambiente global com `os.Unsetenv`. Continuar usando `run.WithOmittedEnv` no contexto.
4. Garantir que `writeState`, `toolStateForResult` e `installedVersion` recebem esse contexto até o subprocesso.
5. Implementar #133 antes ou junto: o version probe deve usar simultaneamente contexto scrubbed + `probeRunner`.
6. Não aplicar o housekeeping context ao subprocesso de instalação que legitimamente precisa de credential. A mudança é somente para bookkeeping/read-only probes.
7. Auditar outros probes disparados dentro de `finishRun` para garantir que nenhum troca de volta para `context.Background` ou contexto original.

### Testes obrigatórios

- Criar adapter `Versioner` de teste cujo `InstalledVersion` executa um helper subprocess/fake runner e captura env efetivo.
- Schema declara secret env que existe no processo.
- Durante install/mutation autorizada, provar que a credential continua disponível onde deve estar.
- Durante state version probe, provar que a variável está ausente.
- Provar que env não-secret permanece disponível.
- Rodar com LoggingRunner e FakeRunner para cobrir tanto context propagation quanto seam de unit test.

### Critérios de aceite

- Nenhum version probe de state recebe schema-secret env por herança.
- Install/source auth não perde credentials necessárias.

---
## #105 — Apply archive expansion limits to standalone `.bz2` extraction

### Objetivo

Aplicar ao fluxo `.bz2` standalone o mesmo limite de bytes expandidos e cancelamento já usado nos archives nativos, impedindo decompression bomb que consuma disco sem limite.

### Estado atual

- `internal/httpdownload/extract.go:89-109`: `extractBzip2` usa `io.Copy(tmp, bzip2.NewReader(in))` sem limite.
- `internal/httpdownload/archive_native.go:19-71`: existe `archiveExpansionLimit = 4 << 30` e `archiveExpansionBudget`, mas o writer assume o limite global diretamente.
- `contextReader` já existe em `archive_native.go:73-83` e deve ser reutilizado.

### Tarefas de implementação

1. Não criar um segundo limite específico com valor divergente. Usar `archiveExpansionLimit` como policy default.
2. Tornar `archiveExpansionBudget` testável com limite reduzido sem alocar/escrever gigabytes. Forma recomendada:
   - adicionar campo `limit int64`;
   - construtor `newArchiveExpansionBudget()` define `limit = archiveExpansionLimit`;
   - helper de teste pode criar budget com limite pequeno;
   - se `limit == 0`, não interpretar como “ilimitado” acidentalmente; usar default no construtor.
3. Atualizar todos os usos atuais de `archiveExpansionBudget{}` para o construtor ou inicialização com limite.
4. Em `extractBzip2`, envolver o output temp com `budget.writer(tmp)` e a origem com `contextReader{ctx, bzip2.NewReader(in)}`.
5. Não copiar primeiro para temp e checar tamanho depois. O writer deve abortar assim que ultrapassar o limite.
6. Em erro por budget/cancelamento:
   - fechar temp;
   - deixar defer remover temp;
   - não chamar `copyBinary`;
   - retornar erro contextualizado como `bzip2: decompress ...` preservando `errors.Is` do context quando aplicável.
7. Preservar o modo/semântica atual de `copyBinary` depois de decompression bem-sucedida.

Pseudocódigo:

```text
reader = bzip2.NewReader(in)
budget = newArchiveExpansionBudget()
_, err = io.Copy(budget.writer(tmp), contextReader{ctx, reader})
if err != nil:
    close tmp
    return wrapped err
close tmp
copyBinary(...)
```

### Testes obrigatórios

- Helper/test com budget pequeno, por exemplo 64 KiB, e stream bz2 que expande além do limite.
- Verificar que o erro contém “expansion exceeds” e que o target final não existe.
- Stream exatamente no limite deve passar.
- Context cancelado durante decompression retorna rapidamente e não instala output parcial.
- Testes existentes de tar/zip continuam usando o mesmo limite default.

### Critérios de aceite

- Nenhum `io.Copy` deslimitado permanece no fluxo `.bz2` standalone.
- O teste não precisa gerar 4 GiB.

---

## #106 — Normalize ownership and mode when committing elevated archive payloads

### Objetivo

Depois de mover um payload staging criado pelo usuário para um destino privilegiado, normalizar o payload para semântica de sistema: raiz traversable e ownership não pertencente ao usuário que executou o staging.

### Estado atual

- `internal/httpdownload/archive_install.go:80-97`: install elevado usa tempdir fora do destino privilegiado, portanto owned pelo usuário e normalmente criado com modo privado.
- `archive_install.go:162-189`: commit elevado é `sudo mv payload dest`; `mv` preserva owner/mode.
- `#135` corrige a capacidade de reportar rollback/cleanup; esta issue deve ser implementada depois dele.

### Semântica a fixar

Para payload elevado em Unix:

- a raiz final de `dest` deve ser owned por UID 0;
- raiz final deve ser ao menos `0755` para permitir traversal;
- modes internos extraídos do archive devem ser preservados, exceto quando a policy existente já os normaliza;
- symlinks não podem fazer uma operação de ownership escapar do payload;
- não forçar group `root`, pois o grupo administrativo equivalente varia entre sistemas. Alterar owner numérico é suficiente para a policy desta issue.

### Tarefas de implementação

1. Após `mv payload dest` e antes de criar launchers, executar uma etapa explícita `normalizeElevatedPayload`.
2. Evitar `chown -R` ingênuo se a implementação/plataforma puder seguir symlinks. Preferir uma dessas estratégias, nesta ordem:
   - enumerar os paths já materializados sob o payload e aplicar owner sem seguir symlink;
   - usar flags comprovadamente portáveis nos OS suportados que operem no link, não no target;
   - no mínimo, normalizar apenas root + paths regulares/dirs obtidos por traversal seguro `os.Root`, chamando uma primitiva elevada por path.
3. Como o processo não possui privilégio para `os.Chown`, a aplicação final deve usar o boundary de elevação (`run.RunElevated`) ou um helper dedicado que continua mockável.
4. Normalizar o modo da raiz para `0755` depois do move. Não fazer `chmod -R 0755`.
5. Se normalização falhar:
   - considerar o commit incompleto;
   - executar rollback para backup usando a API corrigida em #135;
   - retornar `errors.Join(normalizeErr, rollbackErr)` quando ambos falharem.
6. Se não havia payload anterior, rollback remove o novo destino.
7. Se havia backup, rollback restaura o backup.
8. Não criar launcher antes de normalização terminar; launchers só podem apontar para payload em estado final válido.
9. Para processo já root (`os.Geteuid()==0`), staging ocorre no parent e ownership tende a ser root; ainda assim garantir root mode explicitamente se o tempdir deixou `0700`.
10. Não alterar mode de executáveis internos que vieram do archive.

### Test seam

A lógica de commit/normalização precisa ser testável sem root. Se #135 introduzir `archiveCommitOps`, estender esse seam em vez de adicionar globals de função soltos.

### Testes obrigatórios

- Elevated fake runner observa sequência: backup -> commit move -> ownership normalize -> root chmod -> launcher.
- Falha de chown dispara rollback e não cria launcher.
- Falha de chmod dispara rollback.
- Falha de normalize + falha de rollback retorna ambos os erros.
- Teste prova que não é emitido `chmod -R 0755`.
- Teste com symlink no payload prova que a operação de ownership não é construída de forma a seguir target externo.
- Non-elevated path mantém comportamento anterior, exceto root mode final correto.

### Critérios de aceite

- Payload elevado final não fica 0700/user-owned por consequência do staging.
- Qualquer falha nessa transição é observável e não habilita fallback silencioso para outro candidate depois de mutação ambígua.

---

## #107 — Avoid shared `/tmp` fallbacks for state and download cache

### Objetivo

Remover fallbacks previsíveis `/tmp/.local/state/...` e `/tmp/.cache/...`, e recusar cache entries cuja ownership/permissões não correspondam ao contrato privado.

### Estado atual

- `internal/state/state.go:87-97`: falha de `os.UserHomeDir()` resulta em `home = "/tmp"`.
- `internal/downloadcache/cache.go:111-123`: mesma estratégia para cache.
- `downloadcache.Lookup:136-149` verifica apenas regular file, sem validar directory owner/mode nem owner/mode do entry.
- `ensurePrivateDir/ensurePrivateCacheDir` apertam permissões na criação/uso, mas `Lookup` ocorre antes de `Store` e pode observar path prepopulado.

### Decisão de design

Não usar path compartilhado previsível como fallback.

Como `DefaultPath()` e `CacheDir()` hoje retornam apenas `string`, a migração completa para APIs que retornam erro seria grande. Preferir helper de fallback privado por processo/usuário com segurança verificável e lifetime documentado.

Estratégia recomendada:

1. Tentar env específico (`XDG_STATE_HOME` / `XDG_CACHE_HOME`).
2. Tentar `os.UserHomeDir()`.
3. Tentar API de diretório por usuário da stdlib onde ela acrescentar informação útil (`os.UserCacheDir` para cache).
4. Como último recurso, criar uma raiz privada com `os.MkdirTemp(os.TempDir(), "depengine-<kind>-*")`, `0700`, cacheada por processo com `sync.Once` para manter consistência durante a execução.
5. Nunca construir `filepath.Join("/tmp", ".local", ...)` ou equivalente previsível.

Para state, o fallback temporário é excepcional e efêmero. Emitir warning quando ele for usado, pois estado persistido pode não sobreviver a outro processo. Se for viável sem refactor desproporcional, preferir retornar erro ao invés de estado efêmero para comandos de mutação. Não esconder essa decisão.

### Tarefas de implementação — root resolution

1. Criar helper comum ou dois helpers paralelos com mesma policy de fallback; não fazer `state` importar `downloadcache` nem vice-versa.
2. Guardar o temp fallback em `sync.Once` para que todas as chamadas de `DefaultPath()` de um processo apontem para a mesma raiz.
3. Criar diretório com 0700 e verificar via `Lstat` que não é symlink.
4. Em Unix, verificar owner do fallback usando UID efetivo.
5. Windows ignora bits Unix, mantendo ACL nativa do temp user-specific quando aplicável.

### Tarefas de implementação — cache trust

6. Antes de aceitar hit em `Lookup`, validar `CacheDir`:
   - existe e é directory;
   - não é symlink;
   - em Unix owner == effective UID;
   - sem group/world permission bits incompatíveis com 0700.
7. Validar entry:
   - `Lstat` regular, não symlink;
   - owner == effective UID em Unix;
   - mode não concede write para group/world; idealmente 0600.
8. Se qualquer validação falhar, tratar como cache miss. Não abrir/copiar entry suspeito.
9. `Store` deve garantir mode 0600 do entry depois de rename/copy. Rename de um download temp pode carregar mode mais permissivo; apertar explicitamente.
10. `Clear` e eviction só operam em entries reconhecidas sob cache root validado.

### Testes obrigatórios

- Forçar `UserHomeDir` failure via seam. Como stdlib não é substituível diretamente, encapsular resolução de home em variável/interface interna testável.
- Confirmar que fallback não contém literal `/tmp/.cache/depengine` ou `/tmp/.local/state/depengine`.
- Cache dir 0755 criado previamente: `Lookup` deve rejeitar ou apertar antes de usar; escolha uma policy e teste-a. Para segurança simples, rejeitar e deixar `Store` reparar.
- Cache entry symlink para arquivo externo => miss.
- Cache entry regular owned por outro UID via seam/stat metadata => miss.
- Cache entry world-writable => miss.
- Entry válido 0600 sob dir 0700 => hit.
- Windows tests não devem afirmar bits POSIX.

### Critérios de aceite

- Nenhum fallback previsível em shared `/tmp` permanece.
- Lookup nunca confia em entry antes de validar a cadeia de ownership relevante.

---

## #108 — Enforce a download size budget before caching HTTP artifacts

### Objetivo

Limitar o tamanho do artifact durante a transferência, antes de ocupar disco ilimitado e antes de entrar no cache.

### Estado atual

- `internal/httpdownload/backend.go:31+`: `GoDownloader` usa resposta HTTP sem policy explícita de tamanho.
- `CurlDownloader`/`WgetDownloader` também não oferecem garantia uniforme.
- `downloadcache` limita o total **depois** de o artifact já ter sido baixado.
- Expansion budgets de archive não protegem download bruto.

### Policy recomendada

Adicionar:

```text
defaultMaxArtifactBytes = 4 GiB
DEPENGINE_MAX_DOWNLOAD_BYTES=<bytes>
```

O valor pode ser ajustado se docs/requisitos existentes indicarem outro limite, mas a policy deve ser única, determinística e configurável. Override inválido deve retornar default, não “unlimited”. Se desejar permitir unlimited, exigir valor explícito e documentado, por exemplo `0`, exatamente como cache faz; não inferir de parse error.

### Tarefas de implementação

1. Criar `maxArtifactBytes()` em `internal/httpdownload`, com parse da env e testes.
2. Em `GoDownloader.Download`:
   - antes de copiar, se `Content-Length > limit`, abortar sem materializar body;
   - para chunked/unknown length, usar `io.LimitedReader` com `N = limit + 1`;
   - se bytes copiados > limit, retornar erro tipado ou reconhecível `download exceeds ...`;
   - remover output parcial em qualquer erro.
3. Garantir que destino só é considerado pronto depois de close/sync conforme contrato atual.
4. Resolver backend externo. Não basta baixar tudo via curl/wget e checar `Stat` depois. Duas opções aceitáveis:
   - implementar flags de limite somente se forem suportadas e semanticamente equivalentes em todas as versões suportadas;
   - preferível: tornar `GoDownloader` o backend default/obrigatório para artifacts limitados, mantendo curl/wget apenas como implementação não selecionada automaticamente ou fallback explicitamente incapaz.
5. Como Go `net/http` está sempre disponível, não existe necessidade funcional de escolher um backend externo que não consiga cumprir o budget.
6. Atualizar `selectCandidateDownloader` e testes de seleção conforme a decisão.
7. Sidecars (`checksum_url`, `signature_url`, key URL) também são downloads. No mínimo passar pelo mesmo helper bounded. Idealmente usar limite menor separado para sidecar/key, mas isso pode ser tarefa separada; não deixar sidecar completamente ilimitado se o refactor já centraliza transferência.
8. Garantir que `downloadcache.Store` só é chamado após sucesso completo do download.
9. Em cache hit, não baixar novamente; o budget de download não deve invalidar automaticamente cache antigo, mas #107 garante confiança no entry.
10. Não confundir `DEPENGINE_CACHE_MAX_BYTES` (total cache) com o novo máximo por artifact.

### Testes obrigatórios

- HTTP server com `Content-Length` acima do limite: erro antes de ler body completo e sem dest final.
- Server chunked que envia `limit+1`: aborta exatamente ao cruzar budget.
- Response exatamente no limite: sucesso.
- Cancelamento no meio: partial removido.
- Artifact acima do limite não aparece no cache.
- Override env válido/zero/inválido.
- Teste de backend selection prova que caminho automático não escolhe downloader incapaz de impor limite.

### Critérios de aceite

- Não há caminho automático normal de artifact remoto que possa escrever bytes ilimitados antes do cache.
- Mensagem de erro informa limite e URL de forma redacted/sem credentials.

---

## #109 — Preserve `distro_version_min/max` when decoding `when` conditions

### Objetivo

Fazer o decoder refletivo atribuir campos string da `Condition`, preservando min/max version declarados.

### Estado atual

- `internal/config/condition.go:14-23`: `DistroVersionMin` e `DistroVersionMax` são `string` com tags `cfg`.
- `internal/config/decode.go:19-49`: decoder atribui apenas `[]string` e `*bool`; keys string são marcadas como consumidas mas permanecem zero.
- `parseCondition` em `condition.go:197+` usa esse decoder.

### Fix mínimo recomendado

1. Estender `decodeStructFields` para `string`.
2. Não criar switch por nome de campo. A intenção do decoder é ser type-driven.
3. Para field `string`, atribuir somente se raw value for `string`.
4. Confirmar que `validateRawSchema`/strict validation já rejeita tipo errado antes de normalização. Se não rejeitar, corrigir lá para que tipo incompatível seja hard error; não deixar “consumido mas zero”.
5. Aproveitar #131: ordenar `leftover` antes de retorno, mas não misturar outras mudanças se #131 for patch separado. Se #131 já foi aplicado, usar a versão ordenada.

Pseudocódigo:

```go
switch fv.Kind() {
case reflect.String:
    s, ok := val.(string)
    if !ok { ...não engolir silenciosamente... }
    fv.SetString(s)
case reflect.Slice:
    ...
case reflect.Pointer:
    ...bool pointer...
}
```

### Testes obrigatórios

- Parse com apenas `distro_version_min = "12"`; field preservado.
- Parse com apenas max.
- Parse com min+max.
- `Condition.Match` com version abaixo/dentro/acima do range.
- Tipo errado (`distro_version_min = 12`) falha no parse/strict validation.
- Outras fields de Condition continuam decodificando.

### Critérios de aceite

- Nenhuma field reconhecida é marcada como consumida sem ser atribuída ou sem produzir erro de tipo.

---

## #110 — Fix manifest layering to honor field-level precedence and host expansion

### Objetivo

Transformar merge schema+manifest em merge dirigido por **presença declarada**, não por zero value do struct, e alinhar todos os campos com a tabela de merge/documentação.

### Estado atual

- `internal/config/manifest.go:118-168`: `Defaults` é copiado inteiro da camada mais específica.
- `manifest.go:214-230`: `fieldIsSet` usa zero-value e não distingue “omitido” de `false`, `[]`, `""` explicitamente declarados.
- `manifest.go:273-327`: tool merge depende desse detector.
- `manifest.go:373-430`: method merge:
  - hooks estão acidentalmente dentro do `if len(upper.Sources)==0` em `381-387`;
  - só alguns metadados possuem fallback;
  - config map merge conhece presença por key, mas metadata tipada não.
- `internal/config/model.go:44-56`: `RequiresWhen` está `merge:"overwrite"` embora a semântica documentada seja map merge.
- `internal/app/helpers.go:146-165`: manifest é parseado com `nil` facts.
- `config.ResolveSchemaFromFiles:62+` também parseia project/manifest sem fact map.

### Decisão arquitetural obrigatória: presença declarada

Adicionar metadata de presença interna ao modelo normalizado. Não tentar resolver explicit zero via reflect heuristics.

Estrutura sugerida:

```go
type fieldPresence map[string]bool

type Schema struct {
    ...
    defaultsPresence fieldPresence
}

type Tool struct {
    ...
    presence fieldPresence
}

type MethodCandidate struct {
    ...
    presence fieldPresence       // metadata typed: when/inferred/maps/hooks/requires/...
    configPresence map[string]bool // opcional; Config já preserva keys, portanto pode não ser necessário
}
```

Campos são unexported/`json:"-"` e não entram em state/lock/hash diretamente.

### Tarefas A — capturar presença no parse

1. Durante `extractDefaults`, registrar quais keys realmente estavam em `[defaults]` antes de aplicar parser defaults.
2. Durante `normalizeTools`, registrar keys tool-level presentes no TOML, incluindo explicit `false`, empty list/map e empty string se o strict schema permitir.
3. Durante `parseMethod`, registrar metadata declarada fora de `Config`: `when`, `inferred`, `requires`, `sources`, `pre_install`, `post_install`, `arch_map`, `os_map`, secret refs, label/kind quando pertinente.
4. `Config` já preserva distinção por key; não duplicar metadata se não necessário.
5. Atualizar clone helpers para copiar presence maps profundamente.

### Tarefas B — Defaults

6. Substituir `Defaults: layers[last].Defaults` por merge field-by-field baseado em `defaultsPresence`.
7. Para cada default:
   - se layer superior declarou, superior vence mesmo que valor seja explicit zero/empty;
   - se não declarou, herdar inferior;
   - depois do merge, aplicar engine defaults somente ao que permaneceu não declarado.
8. Evitar aplicar defaults em cada layer antes do merge de forma que perca a informação de ausência. Se parser precisar de defaults para normalizar methods, separar `declared defaults` de `effective defaults`.
9. Testar `aur_helper`, manager/method_prefer e arch/os maps individualmente.

### Tarefas C — Tool fields

10. Alterar merge `overwrite` para consultar `upper.presence[field]`, não `fieldIsSet`.
11. Explicit `dependency_only = false` no schema deve sobrescrever true do manifest.
12. Explicit empty `requires = []`, hooks vazios, `method_prefer=[]` etc. devem conseguir limpar lower layer quando o grammar permite declarar vazio.
13. `Tags` continua union se esse for o contrato documentado, mesmo quando upper vazio; não confundir empty union com overwrite.
14. `RequiresWhen` deve ser map merge:
   - copiar lower;
   - aplicar upper keys por cima;
   - key duplicada usa upper;
   - nenhum aliasing de map entre layers.
15. Adicionar estratégia `merge:"map"`/`MergeMapMerge` ao reflection merge, em vez de special-case pelo nome.

### Tarefas D — Method metadata

16. Corrigir imediatamente o nesting de hooks: `PreInstall`/`PostInstall` fallback não deve depender de `Sources` estar vazio.
17. Migrar o merge de metadata para uma tabela/presence-driven helper, cobrindo:
   - `Requires`;
   - `Sources`;
   - `PreInstall`;
   - `PostInstall`;
   - `When`;
   - `Inferred`;
   - `ArchMap`;
   - `OSMap`;
   - secret refs;
   - qualquer metadata persistida fora de `Config` em `MethodCandidate`.
18. Para pointer/map/slice, clone defensivamente.
19. Não herdar `ProjectRoot` da manifest; final merged methods devem continuar bound ao project schema root, como já faz `bindProjectRoot`.
20. `Config` merge continua key-based; para fields `MergeMapMerge`, deep merge de um nível com upper keys vencendo.

### Tarefas E — fatos/placeholder expansion

21. Alterar `mergeManifest` para receber o mesmo `facts/clan` usados no project parse, ou receber diretamente o `BuildMap`.
22. `loadSchemaWithManifest` deve:
   - gather facts uma vez;
   - construir map uma vez;
   - parse project com map;
   - parse manifest com o mesmo map;
   - merge;
   - validar merged schema.
23. Evitar parse project+validate antes de manifest se isso rejeitar uma configuração que só se completa pelo merge, a menos que o contrato atual exija cada layer isoladamente válido. Seguir docs/testes existentes.
24. `ResolveSchemaFromFiles` deve oferecer variante fact-aware ou deixar de ser usada nos caminhos host-aware; não manter uma segunda semântica silenciosa.
25. Coordenar com #124: repo+asset precisa preservar tokens adapter-owned e ainda expandir placeholders comuns. Não criar duas passes incompatíveis.

### Tarefas F — provenance

26. `provenanceCollector` deve usar presença real para atribuir source do field.
27. Para map merge, provenance deve indicar `both` e, se a UI suporta granularidade, key-level provenance; se não suporta, manter field-level mas com merged value correto.
28. Explicit zero do project deve aparecer como source `schema`, não como “manifest venceu”.
29. `why --fields` deve continuar deterministicamente ordenado.

### Testes obrigatórios

Criar tabela de regressão cobrindo cada finding original:

1. hooks herdam independentemente de sources;
2. defaults manifest herdam quando project omite;
3. project explicit default vence;
4. manifest placeholders usam facts;
5. lower `when` herda;
6. lower `inferred` herda quando omitido;
7. lower `arch_map`/`os_map` herda;
8. project explicit `false` vence true lower;
9. project explicit empty list limpa lower;
10. `requires_when` union com collision project-wins;
11. provenance de cada caso;
12. project root continua do schema, não manifest.

Adicionar também property-style test simples: merge não deve mutar nenhuma layer de entrada.

### Critérios de aceite

- `fieldIsSet` não é usado para decidir declaração/precedência de campos que admitem zero explícito.
- Toda field mergeable possui uma estratégia única e testada.
- Project e manifest são expandidos com a mesma host fact map.
- `go test -race ./internal/config ./internal/app/...` passa sem alias/race.

### Não fazer

- Não serializar presence metadata em lock/state.
- Não resolver o problema convertendo todos os fields para pointers públicos; isso espalharia presença por todo o domínio e causaria bloat desnecessário.

---

## #111 — Exclude `requires_when`-gated `dependency_only` tools from execution closure

### Objetivo

Construir a closure de execução somente depois de filtrar dependencies por facts.

### Estado atual

- `internal/exec/execute.go:275-296`: `rootTools` inicia nos roots e adiciona `tool.Requires` cru.
- `internal/exec/run.go:517-527`: depois disso aplica `config.FilteredTools(rootTools(...), facts)`.
- Um helper `dependency_only` já entrou no map antes da remoção da edge, então permanece executável.

### Fix recomendado

Filtrar o grafo primeiro e calcular closure depois.

### Tarefas de implementação

1. Em `sortExecutionLevels`, calcular uma única view:

```go
filtered := config.FilteredTools(s.Tools, ex.facts)
```

2. Validar o grafo completo usando essa view, preservando detecção de cycles/dangling pertinente ao host.
3. Passar `filtered` para `rootTools`.
4. `rootTools` passa a consumir `tool.Requires` já efetivo; não chamar raw requires original.
5. Se quiser tornar o helper mais robusto, renomear para `executionClosure` e documentar que o input já deve ser host-filtered. Não duplicar `EffectiveRequires` em vários níveis.
6. Garantir que `--only dependency_only` continua promovendo explicitamente o helper a root pelo fluxo de `filterTools` existente.
7. Method-level `requires` continuam lazy e candidate-scoped; não incluir todos eles eagerly no root closure.

### Testes obrigatórios

- Root A requer B, `RequiresWhen[B]` false, B `DependencyOnly=true`: levels contém A, não B.
- Mesmo cenário true: B aparece antes de A.
- Report de Execute não contém B no caso false.
- `--only B` continua executando B apesar de DependencyOnly.
- Dois roots, apenas um possui edge gated, não removem B se outra edge efetiva ainda o alcança.

### Critérios de aceite

- Nenhum node dependency-only fica executável sem path efetivo a partir de root, exceto promoção explícita por CLI.

---

## #112 — Return runtime exit code for install execution failures

### Objetivo

Usar código 3 para falhas runtime/executor e preservar código 2 para erro de schema/config.

### Estado atual

- `internal/app/install.go` no bloco após `ex.Execute`: todo erro vira `exitWithCode(2)`.
- `internal/app/helpers.go:168-177`: `exitCodeForError` já classifica `ExitError`, `ParseSchemaError` e default runtime 3.

### Tarefas de implementação

1. Substituir o `return exitWithCode(2)` do erro de `Executor.Execute` por classificação comum:

```go
return exitWithCode(exitCodeForError(err))
```

ou retornar `err` se já for `*ExitError` e a convenção do call site permitir. Preferir consistência com comandos existentes.
2. Não mudar `installExitForReport`: falha de tool representada em report sem erro estrutural continua com seu código de processo documentado (atualmente 1).
3. Distinguir:
   - parse/validation antes de Execute => 2;
   - report com tool failed => 1;
   - erro operacional de executor/state/runner sem report normal => 3.
4. Coordenar com #116: context cancellation devolvida como erro runtime deve sair 3, salvo contrato CLI explícito diferente já documentado.
5. Não classificar pela string do erro.

### Testes obrigatórios

- Schema inválido => 2.
- Executor/state persistence error => 3.
- Graph/runtime error que chega de Execute => 3.
- Tool install que produz report.Failed mas Execute completa => 1.
- Teste deve usar seam/in-process command existente, não subprocess real com comportamento não determinístico quando evitável.

### Critérios de aceite

- Nenhum `Executor.Execute` error genérico é convertido cegamente em 2.

---

## #113 — Use the shared validated, fact-expanded schema loader in `status` / `graph` / `why`

### Objetivo

Eliminar os loaders paralelos dos comandos read-only. Um schema deve significar a mesma coisa em `install`, `check`, `status`, `graph` e `why`: mesmos facts, placeholders, manifest e validação.

### Estado atual

- `internal/app/helpers.go:120` já contém `loadSchemaWithManifest`, mas a função ainda depende de `mergeManifest`, que hoje parseia o manifest sem facts; corrigir #110 primeiro.
- `internal/app/status.go:153` (`loadStatusSchema`) chama `config.ParseProjectSchema(schemaPath, nil)` e faz merge manual.
- `internal/app/graph_why.go:63` (`runGraphView`) e `:325` (`runWhy`) também mantêm fluxos próprios.
- `why` coleta facts só depois de parse/merge.

### Design a implementar

Criar em `internal/app/helpers.go` um único resultado de carregamento, para impedir que cada comando carregue metade dos dados:

```go
type loadedProject struct {
    Schema        *config.Schema
    Facts         *engine.Facts
    Clan          string
    SchemaPath    string
    ManifestPath  string
    ManifestCount int
    ManifestAuto  bool
}
```

O nome pode variar, mas a responsabilidade não: resolver caminho, coletar facts, montar o fact map, parsear projeto, parsear/mesclar manifest e validar uma vez.

### Tarefas de implementação

1. Implementar um helper central, por exemplo `loadProject(...)`, em `internal/app/helpers.go`.
2. O helper deve:
   1. resolver/canonicalizar o schema conforme #126;
   2. chamar `engine.GatherFacts` uma única vez;
   3. obter `clan := engine.ResolveFamily(facts)`;
   4. construir `factMap := config.BuildMap(facts, clan)`;
   5. parsear projeto e manifest com **o mesmo** `factMap`;
   6. aplicar `config.MergeLayers` somente depois dos dois parses;
   7. rodar `validate.ValidateSchema` uma única vez sobre o schema final;
   8. devolver warnings ao chamador via logging normal, sem duplicar validação.
3. Aceitar opções claras para `manifest`, `no-manifest` e, se necessário, proveniência do `why --fields`. Não adicionar booleans posicionais ambíguos.
4. Migrar `runGraphView` para o helper. Remover parse/merge/validation/facts duplicados do arquivo.
5. Migrar `runWhy` para o mesmo helper. `why` não deve reunir facts depois de já ter interpretado placeholders.
6. Migrar o caminho normal de `status` para o helper.
7. Preservar **explicitamente** a degradação especial de `status`: se o schema não puder ser carregado/validado, `status` pode continuar exibindo estado persistido, mas deve emitir warning e marcar comparação live como indisponível. Não implementar essa degradação simplesmente passando `nil` para o parser.
8. Não colocar carregamento de lock dentro do helper genérico. `status` e outras operações têm políticas de lock diferentes; #121 trata isso.
9. Depois da migração, remover helpers mortos de `status.go`/`graph_why.go` ou reduzi-los a wrappers triviais. Não manter dois caminhos equivalentes “por compatibilidade”.

### Testes obrigatórios

- Schema com `{arch}` / `{os}` produz o mesmo valor resolvido em `install`, `check`, `graph`, `why` e `status`.
- Manifest com placeholders host-dependent produz o mesmo resultado nos comandos acima.
- Schema estruturalmente inválido retorna a mesma classe de erro em `graph`/`why` que em `check`.
- `status` com schema inválido ainda entra somente na degradação documentada e imprime warning uma vez.
- Desired-state hash de `status` não diverge apenas porque o schema foi parseado sem facts.
- Manifest auto-detectado continua reportando seu merge uma vez, não uma vez por fase interna.

### Não fazer

- Não mover `config` para depender de `app` ou `exec`.
- Não introduzir um “loader para read-only” separado do loader normal.
- Não engolir erro de facts para fingir que `nil` facts são equivalentes a facts reais.

### Critérios de aceite

- Existe um único pipeline de carregamento validado/fact-expanded usado pelos comandos de projeto.
- As diferenças de política de `status` ficam explícitas no chamador, não escondidas em um parser alternativo.

---

## #114 — Bind read-only native operations to the detected host clan before candidate selection

### Objetivo

Fazer executores read-only usarem o mesmo native adapter ligado ao clan detectado que install/upgrade já usam.

### Dependência

Implementar depois de #113. Idealmente combinar com o helper de executor desta issue e com #128.

### Estado atual

- `internal/app/bootstrap.go:17` registra globalmente `exec.NewNativeAdapter("")`.
- `install` e `upgrade` sobrepõem esse adapter com `exec.NewNativeAdapter(clan)`.
- `status`, `check`, `graph` e `why` criam `exec.New()`, aplicam facts/order, mas não substituem o native adapter global.

### Tarefas de implementação

1. Criar em `internal/app` um construtor compartilhado para executor **de projeto**, por exemplo:

```go
func newProjectExecutor(
    schema *config.Schema,
    clan string,
    facts *engine.Facts,
    rn run.Runner,
) *exec.Executor
```

2. Esse helper deve, no mínimo:
   - `exec.New()`;
   - `WithRunner(rn)`;
   - `WithFacts(facts)`;
   - `WithDefaultMethodOrder(schema.Defaults.MethodOrder)`;
   - `WithAdapters(exec.NewNativeAdapter(clan))`;
   - adicionar AUR schema-local conforme #128.
3. Usar o helper em:
   - `internal/app/status.go`;
   - `internal/app/validate_check.go` (`runCheck`);
   - `internal/app/graph_view.go`;
   - `internal/app/graph_why.go` (`runWhy`).
4. Não usar PATH probing como autoridade de clan quando `facts` já foram resolvidos.
5. Manter `NewNativeAdapter("")` no bootstrap apenas como fallback para contextos que realmente não têm facts. Não tentar tornar o registry global mutável por projeto.
6. Verificar se `providerForMethodKind` e aliases nativos continuam produzindo o provider esperado quando o adapter é sobreposto.

### Testes obrigatórios

- Criar FakeRunner em que executáveis com nome sobreposto existem, mas facts apontam para outro clan.
- Caso Termux/FreeBSD: presença de `pkg` não pode fazer o executor escolher semântica FreeBSD quando facts resolvem `termux`, nem o inverso.
- `status`: testar especificamente a primeira row reconciliada.
- `check`, `why` e graph candidate view devem selecionar o mesmo manager/provider esperado para o mesmo facts fixture.
- Install e read-only devem concordar no candidate/provider em uma fixture compartilhada.

### Não fazer

- Não chamar `exec.Register` ou alterar registry global dentro de cada comando.
- Não derivar clan do nome do package manager encontrado no PATH.

### Critérios de aceite

- Todo executor fact-aware contém `NewNativeAdapter(clan)` antes da primeira seleção/observação de candidato.

---

## #115 — Do not count security-blocked tools as both skipped and failed

### Objetivo

Cada tool deve ocupar exatamente um bucket terminal do report.

### Estado atual

- `internal/exec/presentation.go`: `recordToolResult` incrementa `Skipped` para `StatusSkippedUnavailable`.
- `recordBlockedTool` registra exatamente esse status e em seguida incrementa `report.Failed` manualmente.

### Decisão de semântica

Tratar bloqueio pelo security gate (`--allow-arbitrary-code` ausente) como **falha terminal**, não skip. O usuário pediu uma execução que não pôde ser autorizada; a execução deve sair não-zero e o report deve dizer `failed` uma única vez.

### Tarefas de implementação

1. Alterar `recordBlockedTool` para criar:

```go
ToolResult{
    Tool:   toolName,
    Status: StatusFailed,
    Error:  "requires --allow-arbitrary-code ...",
}
```

2. Remover completamente o incremento manual de `report.Failed`.
3. Não criar um segundo contador específico apenas para consertar a soma.
4. Adicionar helper/invariante de teste que conte status terminais a partir de `report.Tools`.
5. Auditar outros locais por incrementos manuais de `Success`, `Already`, `Skipped`, `Failed` fora de `recordToolResult`. Só manter exceções justificadas.

### Testes obrigatórios

Para um único tool bloqueado:

```text
len(report.Tools) == 1
report.Failed == 1
report.Skipped == 0
report.Success == 0
report.Already == 0
```

Adicionar teste geral:

```text
Success + Already + Skipped + Failed + WouldInstall + Virtual(if counted separately)
```

deve ser consistente com os resultados reportados conforme o contrato atual. Não inventar soma incluindo status não contabilizados sem documentar isso.

Também testar que `installExitForReport` continua retornando falha para security block.

### Critérios de aceite

- Nenhum tool bloqueado incrementa dois buckets.
- Contadores são derivados de um único registro terminal.

---

## #116 — Report cancellation as cancellation and stop scheduling later levels

### Objetivo

Distinguir timeout próprio de tool de cancelamento do run e impedir que níveis posteriores sejam iniciados após cancelamento.

### Estado atual

- `internal/exec/execute.go`: `tryMethodsWithResolution` transforma qualquer `toolCtx.Done()` em `"tool timeout (...) exceeded"`.
- `Execute` itera todos os níveis sem testar `ctx.Err()` antes do próximo nível.

### Tarefas de implementação

1. Criar uma causa explícita para timeout do tool, por exemplo:

```go
var errToolTimeout = errors.New("tool timeout")
```

2. Ao criar timeout de tool, preferir `context.WithTimeoutCause` quando disponível no Go alvo. A causa deve distinguir deadline criado pelo executor de cancelamento herdado do pai.
3. Em `tryMethodsWithResolution`:
   - se `context.Cause(toolCtx)` / `ctx.Err()` indicar cancelamento do run pai, reportar `execution cancelled` ou equivalente;
   - se a causa for `errToolTimeout`, manter mensagem de tool timeout;
   - não inferir pela duração nem pela string de `context.DeadlineExceeded`.
4. Antes de cada `runLevel`, verificar `ctx.Err()`. Se cancelado, sair do loop imediatamente.
5. Dentro de execução paralela, cancelar workers em voo pelo mesmo contexto e aguardar seu encerramento. Não iniciar novos jobs após observação do cancelamento.
6. Não criar resultados falsos para tools de níveis nunca iniciados. Eles devem simplesmente não constar no report parcial, salvo se o contrato existente possuir estado explícito `not scheduled`.
7. Fazer `Execute` devolver o report parcial **junto com** o erro de cancelamento, se a assinatura atual permitir `(*ExecReport, error)` com ambos não-nil.
8. Ajustar `runInstall` para renderizar/preservar o report parcial antes de mapear o erro para saída runtime (#112).
9. Persistência parcial exige cuidado:
   - resultados já commitados não podem desaparecer só porque o pai foi cancelado;
   - não iniciar novas probes de versão com contexto cancelado;
   - persistir metadados/resultados já duráveis usando o caminho state/WAL existente;
   - se for necessário um contexto curto separado apenas para concluir persistência local já iniciada, usar deadline limitado e **não** iniciar mutation externa.
10. Não “corrigir” isso convertendo todo `context.Canceled` em sucesso.

### Testes obrigatórios

- Grafo em dois níveis: cancelar durante nível 1; nenhum tool do nível 2 é executado.
- Tool com timeout próprio: mensagem continua sendo timeout, não cancellation.
- Cancelamento do pai antes do timeout: mensagem é cancellation, não timeout.
- `--jobs > 1`: workers já iniciados recebem cancelamento; novos jobs não iniciam.
- Report parcial conserva tool que terminou antes do cancelamento.
- Estado de uma mutation já commitada permanece recuperável/persistido.
- CLI retorna classe runtime, não schema error.

### Critérios de aceite

- Cancelar o run não produz uma cascata de “fake timeouts”.
- Nenhum nível posterior é agendado após `ctx` cancelado.

---

## #117 — Handle `exec.ErrWaitDelay` separately from child command failure

### Objetivo

Separar o exit status do processo filho do problema de drenar pipes herdados por descendentes.

### Estado atual

- `internal/run/runner.go` normaliza `*exec.ExitError`, mas `exec.ErrWaitDelay` permanece em `Result.Err`.
- `CheckResult` interpreta qualquer `Result.Err` como falha da operação.
- `internal/run/lifecycle_test.go` já cobre um filho bem-sucedido que deixa pipe aberto e espera `ErrWaitDelay`.

### Design a implementar

Estender `run.Result` para representar duas dimensões:

```go
type Result struct {
    ExitCode int
    Stdout   string
    Stderr   string
    Err      error // spawn/signal/child execution failure
    WaitErr  error // pipe-drain / ErrWaitDelay / output lifecycle
}
```

O nome `WaitErr` pode variar. Não esconder essa informação em texto de stderr.

### Tarefas de implementação

1. Em `commandResult`, detectar `errors.Is(waitErr, exec.ErrWaitDelay)`.
2. Se o filho direto terminou com exit 0 e o único problema é `ErrWaitDelay`:
   - `ExitCode = 0`;
   - `Err = nil`;
   - `WaitErr = exec.ErrWaitDelay`.
3. Se o filho saiu não-zero, preservar o erro/exit normal; `WaitErr` pode coexistir se aplicável.
4. Atualizar `LoggingRunner`:
   - não logar `WaitErr` como se o processo tivesse falhado;
   - emitir warning/debug claro de output potencialmente truncado/abandonado.
5. Manter `CheckResult` padrão focado no sucesso do filho. Um exit 0 + `WaitErr` não deve virar automaticamente “install failed”.
6. Criar uma variante para operações que **dependem da completude do stdout**, por exemplo `CheckResultCompleteOutput` ou helper equivalente.
7. Auditar todos os consumidores que parseiam stdout para identidade/versão/listagem e fazê-los exigir output completo. Exemplos a pesquisar: JSON de package managers, `InstalledVersion`, source listing, release metadata via subprocess.
8. Não ignorar globalmente `ErrWaitDelay`: isso converteria truncamento de dados em sucesso silencioso.

### Testes obrigatórios

- Filho exit 0 + grandchild mantém pipe: `ExitCode=0`, `Err=nil`, `WaitErr=ErrWaitDelay`.
- `CheckResult` simples considera o comando executado com sucesso.
- Helper de output completo rejeita o mesmo resultado.
- Filho exit != 0 continua falhando independentemente de `WaitErr`.
- Logging diferencia `run failed` de `output drain incomplete`.

### Critérios de aceite

- O status do filho e a integridade/completude do output não dividem mais o mesmo campo de erro.

---

## #118 — Do not report dangling dependencies as `E_CYCLE`

### Objetivo

Emitir `E_CYCLE` somente para ciclos reais.

### Estado atual

`internal/validate/semantic.go:17-38` já usa `errors.As(err, *graph.CycleError)`, mas o branch `else` ainda adiciona **o mesmo** `ErrCycle` para qualquer outro erro retornado por `graph.Sort`, incluindo dependência ausente.

### Tarefas de implementação

1. Em `validateCycles`, manter somente:

```go
var cycleErr *graph.CycleError
if errors.As(err, &cycleErr) {
    // add ErrCycle
}
```

2. Para erro não-cíclico retornado por `graph.Sort`, não adicionar diagnóstico de ciclo.
3. Deixar dangling references exclusivamente com `validateDanglingReferences`.
4. Se existir erro interno de graph que não é cycle nem dangling, não o engolir silenciosamente numa API genérica. O ideal é tornar o sorter error types explícitos; para esta issue, não reutilizar `E_CYCLE`.

### Testes obrigatórios

- Schema com uma dependência ausente produz exatamente `E_DANGLING_REF` para esse problema e nenhum `E_CYCLE`.
- Schema com ciclo real continua produzindo `E_CYCLE` e inclui caminho do ciclo.
- Schema com dangling + ciclo independente produz ambos, cada um uma vez.

### Critérios de aceite

- Grep/teste nenhum caminho de erro não-`CycleError` gera `ErrCycle`.

---

## #119 — Fail closed when legacy install lock resolution fails before planning

### Objetivo

No caminho missing/v1, resolver identidade mutável uma única vez antes de mutation e persistir exatamente a mesma resolução depois.

### Estado atual

- `internal/app/install.go:245` `resolveInstallLock` valida v2 corretamente.
- No legacy path, `lock.ResolveLegacyV1` em torno de `:264` apenas gera warning em erro e deixa install continuar.
- `finishInstallRun` pode voltar ao fluxo de persistência legacy pós-execução.

### Tarefas de implementação

1. Alterar `resolveInstallLock` para retornar erro fatal quando:
   - o schema contém selector legacy que exige resolução;
   - `ResolveLegacyV1` falha.
2. O erro deve ocorrer antes de `state.SaveSnapshot()` e antes de `ex.Execute()`.
3. Quando a resolução legacy for bem-sucedida, guardar o lock resolvido no `installPlan` ou em um objeto explícito de resolução da run.
4. Passar esse mesmo valor a `finishInstallRun` / `saveLegacyInstallLock`.
5. Remover qualquer segunda chamada a `ResolveLegacyV1` para as mesmas identidades durante persistência.
6. Se for necessário mesclar com um v1 existente, fazer o merge **antes** da execução e persistir o resultado já mesclado depois.
7. Não promover automaticamente v1 para v2 nesta issue; `update` continua sendo o promotion point documentado.
8. Garantir que frozen behavior permaneça inalterado.

### Testes obrigatórios

- Resolver retorna erro antes de execução: FakeAdapter registra zero mutations e snapshot não é criado.
- Resolver fake retorna `v1` na primeira chamada e `v2` na segunda: a implementação deve chamar o resolver apenas uma vez e persistir `v1`.
- Existing legacy lock + fresh partial resolution preserva pins não re-resolvidos conforme `MergeLegacy...` atual.
- Existing v2 path não chama legacy resolver.
- `--frozen` mantém sua política atual.

### Critérios de aceite

- Nenhum selector lockable legacy chega à mutation após falha de pre-resolution.
- Persistência não redescobre identidade mutável depois da mutation.

---

## #120 — Align `distro_family` validation with runtime family matching

### Objetivo

Validação e runtime devem usar o mesmo vocabulário e a mesma normalização de family.

### Estado atual

- `internal/validate/validate.go` constrói `knownDistroFamilies` de `native.AllClans()` + `unknown`.
- `internal/platform/resolve.go` também pode retornar `android`.
- Runtime matching é case-insensitive; validação faz lookup exato.

### Tarefas de implementação

1. Criar no pacote `internal/platform` a fonte canônica, por exemplo:

```go
func KnownFamilies() []string
func NormalizeFamily(string) string
```

2. `KnownFamilies` deve conter **todo valor que `ResolveFamily` pode retornar**, incluindo `android` e `unknown`.
3. A lista pode incorporar native clans, mas `platform` não deve passar a depender de `internal/native` se isso inverter a arquitetura. Preferir manter a classificação/famílias no próprio `platform` e fazer testes de consistência com native.
4. `NormalizeFamily` deve refletir exatamente a semântica de `MatchesDistroFamily`, no mínimo `strings.ToLower` e, se necessário, trim somente se runtime também o fizer.
5. `validateUnknownDistroFamily` deve normalizar antes de consultar a tabela conhecida.
6. Não adicionar um manager nativo fictício para Android apenas para satisfazer validação.
7. Adicionar teste de consistência: para cada fixture de `ResolveFamily`, o valor resultante aparece em `KnownFamilies`.

### Testes obrigatórios

- `android` aceito.
- `Android`, `ANDROID` e forma mista aceitas se runtime também aceita.
- Cada clan existente aceito em mixed case.
- Valor realmente desconhecido continua produzindo `W_UNKNOWN_DISTRO_FAMILY`.
- `knownDistroFamilyList()` continua ordenada/determinística para mensagem.

### Critérios de aceite

- Não existe family que `ResolveFamily` produza e validation marque como desconhecida.
- Case behavior de validation e runtime é idêntico.

---

## #121 — Surface lockfile load errors in `status` and SBOM

### Objetivo

Lock ausente é uma situação normal; lock presente mas ilegível/corrompido/incompatível não pode ser tratado como “não existe”.

### Estado atual

- `internal/app/status.go:156`: `lk, _ = lock.Load(...)` descarta o erro.
- `internal/app/sbom.go:45`: só entra no branch se `lerr == nil`; qualquer erro some.

### Decisão de política

Para ambos os comandos, **falhar** quando um lock existente não pode ser carregado/decodificado. Esses comandos usam lock como entrada autoritativa para versão/identidade; degradar silenciosamente cria output aparentemente confiável.

Arquivo ausente deve continuar significando `lk == nil, err == nil` conforme o contrato de `lock.Load`.

### Tarefas de implementação

1. Alterar `loadStatusSchema` ou seu substituto de #113 para propagar erro de `lock.Load`.
2. Em `runStatus`, mapear o erro para exit runtime (3), com mensagem contendo o path do lock e a causa.
3. Não transformar erro de lock em erro de schema (2).
4. Em `runSBOM`, separar explicitamente:

```go
lk, err := lock.Load(path)
if err != nil {
    log.Default.Error("load lock for sbom", ...)
    return exitWithCode(3)
}
if lk != nil { ... }
```

5. Se `ProjectionDocument()` de v2 falhar, tratar também como erro fatal de lock, não como ausência de metadata.
6. Redigir paths/secrets segundo convenções existentes. Lock não deve conter secrets, mas não imprimir payload cru do arquivo.

### Testes obrigatórios

Para `status` e `sbom`:

- lock ausente => comando continua normalmente;
- JSON/TOML corrompido => exit 3 + mensagem;
- versão de lock não suportada => exit 3;
- v2 com projection inválida => exit 3;
- lock válido => comportamento atual preservado.

### Critérios de aceite

- Não existe `_ = lock.Load` nem `lerr == nil` que faça erro real parecer lock ausente nesses comandos.

---

## #122 — Take pre-install snapshots under the state lock and write them atomically

### Objetivo

Capturar snapshot de uma versão consistente do state e publicá-lo atomicamente.

### Dependência

Implementar depois, ou no mesmo PR coordenado, que #103. O snapshot deve usar a nova API cancellable de state lock em vez de criar outro mecanismo de lock.

### Estado atual

- `internal/state/snapshot.go:30` lê `state.json` diretamente, sem state lock.
- grava o snapshot final diretamente com `os.WriteFile`.
- `internal/state/state.go:105-184` já possui o padrão correto de temp file + fsync + rename + directory sync.

### Tarefas de implementação

1. Introduzir `SaveSnapshotContext(ctx context.Context)` em `internal/state/snapshot.go`.
2. A função deve adquirir o lock exclusivo via `LoadLockedContext(ctx)` de #103.
3. Não reler `state.json` fora do lock. Serializar/capturar o `State` obtido pelo `LockedState`.
4. Se state não existir, manter a semântica atual de snapshot vazio, mas produzir um state válido/representável segundo o formato esperado pelo undo. Não escrever bytes parcialmente formados.
5. Extrair de `state.Save` um helper interno de publicação atômica reutilizável, por exemplo:

```go
func atomicWritePrivateFile(path string, data []byte, mode fs.FileMode) error
```

Esse helper deve:
   - criar temp no mesmo diretório;
   - usar modo 0600;
   - write completo;
   - `Sync()` do arquivo;
   - `Close()`;
   - `Rename()`;
   - fsync do diretório em Unix;
   - remover temp em qualquer falha.
6. `Save` e snapshot devem usar o helper para não manter duas implementações quase iguais.
7. Publicar snapshot somente depois de todos os bytes estarem duráveis no temp.
8. Executar `PruneSnapshots` somente depois da publicação bem-sucedida. Manter coordenação sob lock para impedir dois writers de publicar/prunar simultaneamente.
9. `runInstall` deve chamar `SaveSnapshotContext(ctx)`.
10. Se snapshot falhar, decidir de modo explícito se install continua. Hoje é warning. Manter essa política nesta issue, salvo se contrato do projeto exigir snapshot para recovery. Não mudar policy de erro por acidente.

### Testes obrigatórios

- Dois processos/goroutines: writer segura lock, snapshot aguarda e captura estado após liberação.
- Cancelar contexto durante espera => snapshot retorna `context.Canceled`/causa e não cria arquivo.
- Injetar falha durante write/sync => nenhum arquivo final truncado aparece.
- Snapshot final tem modo 0600 e diretório privado.
- Pruning nunca vê temp files como snapshots.
- Snapshot contém exatamente o estado protegido pelo lock, não bytes de uma corrida.

### Critérios de aceite

- `SaveSnapshotContext` não faz leitura ou publicação fora da disciplina do state lock.
- O path final nunca aponta para conteúdo parcial.

---

## #123 — Give `.deb` HTTP installs package-aware check/remove semantics

### Objetivo

Transformar `.deb` direto em um lifecycle package-aware. Não usar semântica de arquivo para uma operação que altera o banco de pacotes dpkg.

### Decisão de design

Exigir identidade de pacote explícita no método para novos installs `.deb`, e verificar essa identidade contra o próprio arquivo `.deb` antes de mutation.

Não derivar identidade só depois de instalar. Não remover diretório/file como substituto de `dpkg --remove`.

### Tarefas de implementação

1. Adicionar suporte a campo `pkg` nos contratos HTTP/GitHub que podem resolver `.deb` em `internal/methodkind/methodkind.go`.
   - `pkg` aqui é identidade do pacote Debian, não nome do arquivo.
   - Reusar validação de package name existente.
2. Na validação/planner, detectar método cujo artifact declarado/resolvido é `.deb` e exigir `pkg` não vazio.
   - Para `repo+asset`, a extensão pode só ser conhecida pelo asset pattern/resolução. Se não for possível provar estaticamente que será `.deb`, a checagem runtime deve existir também.
3. Projetar `pkg` para `plan.ResolvedIdentity.Package` / metadata de plano apropriada.
4. Antes de `dpkg -i`, executar probe read-only no arquivo baixado:

```text
dpkg-deb --field <artifact> Package
```

5. Normalizar whitespace e exigir igualdade exata com o `pkg` configurado. Mismatch deve abortar **antes** de `dpkg -i`.
6. Se `dpkg-deb` não estiver disponível, falhar antes de mutation. Não instalar um `.deb` cuja identidade não pode ser verificada.
7. Implementar Observe/Check para `.deb` package-aware usando `dpkg-query -W` (ou comando já padronizado no projeto) e o package identity resolvido.
8. Quando disponível, extrair versão via `dpkg-query` para `InstalledVersion` e Observation. Não inventar versão se não for observável.
9. Implementar Remove para `.deb` como:

```text
sudo dpkg --remove -- <pkg>
```

ou forma equivalente aceita pelo runner, sem shell string.
10. O remove deve usar a identidade persistida/resolvida, não recalcular a partir do filename.
11. Instalações legacy sem `pkg` persistido/configurável:
    - não executar `RemoveAll` de `extract_to`;
    - falhar fechado com mensagem explicando que package identity é necessária;
    - se schema atual fornece `pkg` e coincide com método persistido, pode usá-lo após validação.
12. Ajustar `CanRemove`/capability apenas se necessário para refletir que `.deb` é removível quando identidade existe.
13. Não alterar o fluxo de `.deb` host compatibility já existente em `internal/httpdownload/compatibility.go`.

### Testes obrigatórios

- `.deb` com `pkg=foo`, `dpkg-deb` retorna `foo`: instalação prossegue.
- `dpkg-deb` retorna outro package: zero chamadas a `dpkg -i`.
- `dpkg-deb` indisponível: fail before mutation.
- Check/Observe usam `dpkg-query` e distinguem absent/present.
- InstalledVersion captura versão conhecida.
- Remove chama exatamente `dpkg --remove foo`; nunca `os.RemoveAll(extract_to)`.
- Legacy state sem package identity falha fechado.
- Debian/Termux compatibility tests atuais continuam passando.

### Não fazer

- Não inferir package name de `tool.Name` silenciosamente.
- Não aceitar basename do `.deb` como identidade do pacote.
- Não chamar shell para montar comando.

### Critérios de aceite

- Todo `.deb` instalado pelo adapter pode ser observado/removido pela mesma package identity verificada antes da instalação.

---

## #124 — Expand `{arch}` / `{os}` inside `repo+asset` method config

### Objetivo

Aplicar placeholders host-nativos a todos os campos elegíveis de `repo+asset`, sem destruir `{arch_any}` / `{os_any}` usados pelo matcher de assets.

### Estado atual

Em `internal/config/parse.go` o segundo passe de expansão encontra `repo`, grava `_current_arch`/`_current_os` e faz `continue`. Isso pula a expansão normal do restante do config.

### Tarefas de implementação

1. Refatorar o segundo passe para que `repo` **não** cause retorno/continue do método inteiro.
2. Construir o mapa de aliases/host placeholders uma vez por método.
3. Aplicar a expansão recursiva atual aos campos string elegíveis do config.
4. `{arch}` e `{os}` devem expandir normalmente também em métodos com `repo`.
5. `{arch_any}` e `{os_any}` devem permanecer literais para o matcher GitHub:
   - não adicionar essas chaves ao fact map;
   - não fazer replace por prefixo;
   - expansão deve reconhecer placeholder por token completo.
6. Metadados internos `_current_arch` / `_current_os` devem conter os valores correntes, não passar pelo mecanismo de expansão como user fields. Preferir escrevê-los depois da expansão ou excluí-los explicitamente.
7. Confirmar que mapas/listas nested usados em config continuam expandidos pelo helper já existente.
8. Não alterar semântica de `arch_map`/`os_map` nesta issue além do necessário para produzir aliases corretos.

### Testes obrigatórios

Criar repo+asset fixtures com:

- campo adicional contendo `{arch}`;
- campo adicional contendo `{os}`;
- nested map/list com placeholders;
- `asset = "tool-{os_any}-{arch_any}.tar.gz"` preservado literalmente;
- asset misto, por exemplo `tool-{os_any}-{arch}.tar.gz`, onde somente `{arch}` expande;
- aliases de `arch_map`/`os_map` quando aplicável.

### Critérios de aceite

- A presença de `repo` não desliga mais a expansão normal de host placeholders.
- Tokens `_any` especiais chegam intactos ao resolver de GitHub.

---

## #125 — Validate method bucket selectors against available bucket members

### Objetivo

Um selector de bucket é válido se **ao menos um** membro do bucket estiver declarado no tool.

### Estado atual

`internal/config/parse.go:976` expande `method_prefer`/`method_only` com `ExpandBuckets` e exige match para cada membro expandido. Isso rejeita `python` quando o tool só declara `pip`, embora runtime consiga selecionar `pip` corretamente.

### Tarefas de implementação

1. Não usar `ExpandBuckets(slice)` cegamente para validação.
2. Para cada selector original:
   - se `selector` existe em `methodkind.DefaultBuckets`, obter seus membros;
   - normalizar aliases nativos da mesma forma que runtime;
   - considerar válido se **qualquer** membro casa com qualquer `tool.Methods`.
3. Para selector que não é bucket:
   - manter semântica atual de label exato ou kind exato;
   - native aliases continuam normalizados conforme contrato existente.
4. Mensagem de erro de bucket deve mencionar o selector escrito pelo usuário, não um membro inexistente arbitrário.
5. Não alterar `SelectMethods` salvo se os testes revelarem divergência real. O runtime já deve ignorar membros ausentes.

### Testes obrigatórios

- Tool só com `pip`, `method_prefer=["python"]` => válido e seleciona pip primeiro.
- Tool só com `uv`, `method_only=["python"]` => válido e seleciona uv.
- Tool sem nenhum membro python, selector `python` => erro único.
- Label exato inexistente continua erro.
- Kind exato inexistente continua erro.
- Bucket com vários membros declarados preserva order runtime atual.

### Critérios de aceite

- Validation testa existência de interseção bucket ∩ candidates, não igualdade do bucket inteiro.

---

## #126 — Persist an absolute schema path in global state

### Objetivo

`State.SchemaPath` deve identificar o projeto instalado independentemente do CWD futuro.

### Tarefas de implementação

1. Criar helper em `internal/app/helpers.go`, por exemplo:

```go
func resolveSchemaFilePath(input string) (string, error)
```

2. O helper deve:
   - aceitar path de arquivo;
   - se input é diretório, resolver o arquivo de schema esperado conforme regra atual;
   - aplicar `filepath.Abs`;
   - aplicar `filepath.Clean`;
   - fazer `os.Stat` do **arquivo final**;
   - retornar path absoluto final.
3. Chamar esse helper no pipeline compartilhado de #113 antes de parse/lock/modtime.
4. `loadedProject.SchemaPath` deve carregar a forma absoluta.
5. `newInstallExecutor` deve passar esse path para `WithSchemaInfo`, não a spelling original da flag.
6. Upgrade e qualquer outro fluxo que persiste `SchemaPath` devem usar o mesmo path resolvido.
7. `lock.DefaultPath` deve receber o path final para que lock “alongside schema” continue correto.
8. Como defesa adicional, `exec.WithSchemaInfo` pode rejeitar/normalizar path relativo, mas não deve fazer discovery de schema nem `Stat`; essa é responsabilidade de app.
9. Não migrar automaticamente estados legacy nesta issue. Quando um estado antigo contém path relativo, `status` pode continuar usando a semântica legacy ou emitir warning; novos writes devem ser absolutos.

### Testes obrigatórios

- Rodar install in-process a partir de `project/`, persistir state, `chdir` para outro diretório e carregar `status`: schema original é encontrado.
- `--schema ./schema.toml` persiste absoluto.
- `--schema <diretório>` persiste `<abs-dir>/schema.toml` final, não o diretório.
- Lock path permanece ao lado do schema.
- Paths com `..` são limpos.

### Critérios de aceite

- Todo novo `State.SchemaPath` escrito é absoluto e aponta para o arquivo efetivamente parseado.

---

## #127 — Enforce `gofmt` in CI

### Objetivo

Tornar formatação uma condição mecânica de merge e limpar o tree atual.

### Estado atual revalidado

No commit base deste plano, `gofmt -l` ainda reporta arquivos. O conjunto exato deve ser recalculado no momento da implementação porque agentes anteriores podem tocar nesses arquivos.

### Tarefas de implementação

1. Em uma mudança dedicada, executar `gofmt -w` em **todos os `.go` tracked** que aparecem em `gofmt -l`.
2. Não misturar alterações semânticas nesse commit de formatação.
3. Adicionar step em `.github/workflows/ci.yml` antes dos testes/lint:

```sh
bad="$(git ls-files -z '*.go' | xargs -0 gofmt -l)"
if [ -n "$bad" ]; then
  printf 'gofmt required:\n%s\n' "$bad" >&2
  exit 1
fi
```

4. Se CI roda em shell/plataforma sem `xargs -0`, usar pequeno script Go/shell portável já compatível com a matriz. Não usar glob `**/*.go` dependente de shell options.
5. Se o repo possui pre-commit/pre-push script oficial, adicionar o mesmo gate ali sem duplicar lista de arquivos.
6. Não usar `go fmt ./...` como **checker**, pois ele modifica arquivos e não é um assertion claro.
7. Rodar a toolchain anexada/versão definida em `go.mod`/CI.

### Testes obrigatórios

- `gofmt -l $(git ls-files '*.go')` equivalente retorna vazio.
- Introduzir temporariamente fixture mal formatada no teste do script, se houver harness de scripts, e verificar exit != 0.
- CI normal continua passando após formatação.

### Critérios de aceite

- PR com qualquer `.go` mal formatado falha automaticamente.
- Tree no commit do fix está gofmt-clean.

---

## #128 — Honor `defaults.aur_helper` in `status` / `check` / `remove` / `undo`

### Objetivo

O helper AUR concreto usado para observar/remover deve ser o mesmo provider escolhido para instalar, inclusive quando o schema original não está disponível durante remove/undo.

### Dependências

- Usar o executor factory de #114 para `status`/`check`.
- A parte de persistência deve ser coordenada com qualquer mudança de state schema em andamento.

### Estado atual

- Install/upgrade já sobrepõem `ecosystem.NewAURAdapter(schema.Defaults.AurHelper)` localmente.
- Graph/why também fazem overlay schema-local.
- Status/check não fazem.
- Remove/undo reconstroem native adapter, mas não AUR helper.
- `exec.ToolResult` já possui `Provider`, porém `state.ToolState` em `internal/state/state.go:48-71` não possui provider persistido.

### Design a implementar

Persistir o **provider concreto** separado do method identity. Para AUR, `MethodKind` permanece `aur`; `Provider` pode ser `yay`, `paru`, etc. Isso não deve alterar hashes/lock identity do método lógico.

### Tarefas de implementação

1. Adicionar a `state.ToolState`:

```go
Provider string `json:"provider,omitempty"`
```

2. Em `toolStateForResult`, copiar `result.Provider` para state.
3. Garantir que resultados AUR recebam o helper concreto em `ToolResult.Provider`.
   - Preferir pequena interface opcional do adapter, por exemplo `ProviderName() string`, ou extensão localizada em `providerForMethodKind`.
   - Não fazer type assertion para struct privada em `app`.
4. `status` e `check` com schema disponível devem usar `newProjectExecutor` de #114, que instala `NewAURAdapter(schema.Defaults.AurHelper)`.
5. `remove` e `undo` devem escolher adapter AUR por **tool state**:
   - `MethodKind == "aur"` + `Provider != ""` => construir adapter com esse provider;
   - alias method kinds (`yay`, `paru`) devem respeitar o próprio kind conforme registry atual;
   - não sobrescrever um provider persistido com defaults atuais do schema.
6. Para state legacy com `MethodKind=aur` e `Provider` vazio:
   - se schema correspondente está disponível e identifica helper, usar esse helper;
   - caso contrário, falhar fechado com mensagem orientando a fornecer/restaurar schema/contexto;
   - **não** assumir `paru` só porque bootstrap usa `paru`.
7. Persisted provider não deve incluir path absoluto do executable nem dados secretos.
8. Atualizar state checksum/tests/JSON fixtures naturalmente através do campo `omitempty`.
9. Não tornar AUR helper parte do user-facing method label salvo onde o report já mostra provider deliberadamente.

### Testes obrigatórios

- Install com `aur_helper="yay"` persiste `Provider:"yay"`.
- Status/check usam runner esperando `yay`, não `paru`.
- Remove sem schema, usando state persistido, executa `yay`.
- Undo usa o mesmo provider persistido.
- State legacy sem provider + schema com `yay` usa `yay`.
- State legacy sem provider e sem schema falha fechado, zero mutation.
- Alterar defaults atuais de `yay` para `paru` após install não muda provider histórico usado na remoção do tool já instalado.

### Critérios de aceite

- AUR observation/removal nunca cai silenciosamente no helper bootstrap quando existe identidade mais específica do projeto/state.

---

## #129 — Derive `--check-env` native manager probes from the native registry

### Objetivo

Remover a segunda lista manual de package managers e fazer `validate --check-env` acompanhar automaticamente o registry nativo.

### Estado atual

- `internal/validate/envcheck.go:34-70` contém `envToolBinaries` hard-coded.
- `internal/native/registry.go:318` expõe `ManagerBinaryNames()` e `:367` expõe `ManagerNames()`.

### Tarefas de implementação

1. Dividir a lista atual em:
   - native managers derivados do registry;
   - ferramentas de ecossistema/linguagem explicitamente suplementares;
   - utilitários de sistema suplementares (`git`, `curl`, `wget`, etc.).
2. Importar `internal/native` em `internal/validate` se isso não criar ciclo. Confirmar com `go list`/build antes de avançar.
3. Para native, usar `native.ManagerBinaryNames()` como fonte preferencial de executáveis reais. Se `ManagerNames()` e binary name diferirem, probe o binary, não o nome lógico.
4. Construir a lista efetiva em runtime ou init de maneira determinística.
5. Deduplicar nomes entre native e supplemental mantendo uma única entry. Definir precedência de `Kind`: se um binary é native, classificar como `native`.
6. Ordenar por kind/name como hoje.
7. Remover entradas native hard-coded de `envToolBinaries`.
8. Não derivar language/ecosystem binaries do executor registry nesta issue. Isso ampliaria o escopo para outro source-of-truth problem.

### Testes obrigatórios

- Para cada `native.ManagerBinaryNames()`, existe exatamente um `EnvCheck`.
- `winget` aparece.
- `pkgin` aparece.
- Binary compartilhado por mais de um clan aparece uma vez.
- Supplemental tools continuam presentes.
- Ordem do JSON/text output permanece determinística.

### Critérios de aceite

- Adicionar um novo native manager no registry basta para ele aparecer em `--check-env` sem editar `envcheck.go`.

---

## #130 — Validate or escape GitHub owner/repo path segments before API requests

### Objetivo

Transformar `owner/repo` em uma identidade canônica antes de interpolar qualquer request path.

### Estado atual

- `internal/ghrelease/resolver.go:361-385` `splitRepo` verifica basicamente shape de duas partes.
- `fetchLatestTag` em `:114-161` e `fetchLatestRelease` em `:169-211` interpolam owner/repo diretamente.
- `fetchReleaseByTag` escapa apenas o tag em `:227`.

### Design a implementar

Criar um parser canônico interno, por exemplo:

```go
type repositoryID struct {
    Owner string
    Name  string
}

func parseRepositoryID(raw string) (repositoryID, error)
```

`splitRepo` e `githubRepoFromURL` devem delegar a ele ou desaparecer se deixarem de ser necessários.

### Regras de parsing

1. Aceitar os formatos já documentados:
   - `owner/repo`;
   - `github.com/owner/repo`;
   - `https://github.com/owner/repo`;
   - terminal `.git` simples quando já aceito.
2. Rejeitar:
   - owner ou repo vazio;
   - segmentos adicionais em bare input;
   - `.` ou `..` como segmento;
   - backslash;
   - NUL/control chars;
   - `?` / `#` em bare refs;
   - `%`/percent escapes ambíguos em bare refs, principalmente `%2f`, `%5c`, `%3f`, `%23`;
   - `.git` repetido/malformado que resulte em identidade diferente da aparente;
   - userinfo ou port não permitido em URL GitHub.
3. Não aplicar regex excessivamente restritiva que rejeite nomes GitHub legítimos sem evidência. O requisito de segurança é impedir alteração de path/query semantics.
4. Na construção da API, escapar **cada segmento separadamente** com `url.PathEscape` ou montar `url.URL` de forma equivalente:

```go
owner := url.PathEscape(id.Owner)
repo := url.PathEscape(id.Name)
```

5. Nunca escapar a string `owner/repo` inteira de uma vez, porque a slash separadora pertence ao path estrutural.
6. Cache keys devem usar a forma canônica não-URL-encoded para evitar duas representações da mesma repo.
7. Full GitHub URL e bare form da mesma repo devem canonicalizar para a mesma key.

### Testes obrigatórios

Tabela adversarial:

- `owner/repo?x=y` => reject;
- `owner/repo#fragment` => reject;
- `owner/re%2Fother` => reject;
- `owner/%2e%2e` => reject;
- `../repo`, `owner/..` => reject;
- `owner\\repo` => reject;
- `owner/repo.git` => accepted/canonical `repo`;
- `owner/repo.git.git` => reject ou comportamento explicitamente testado, não strip repetido;
- URL GitHub com query/fragment => manter regra intencional atual (hoje rejeitada por `splitRepo`), testar;
- malformed URL => reject.

Usar RoundTripper fake e assertar `req.URL.Path` exato para latest e release-by-tag.

### Critérios de aceite

- Nenhum input do usuário consegue acrescentar query/path segment ao endpoint `/repos/{owner}/{repo}/...`.
- Todas as chamadas GitHub usam a identidade canônica.

---

## #131 — Make parser diagnostics deterministic

### Objetivo

Mesmo schema + mesmo registry deve gerar exatamente os mesmos diagnostics em toda execução.

### Estado atual

- `internal/config/parse.go:907` cria `prefixHints` iterando `set` map e aceita o primeiro prefixo encontrado.
- `internal/config/decode.go:19-49` devolve `leftover` na ordem aleatória de map iteration.
- `Tool.Ecosystem` é atribuído durante iteração de bucket em `parse.go:431/440/453`; no tree auditado não há consumidor material conhecido além de parse/model.

### Tarefas de implementação

1. Extrair helper para hint de variant, por exemplo:

```go
func bestKnownKindPrefix(unknown string, known []string) (string, bool)
```

2. Regra determinística:
   - coletar todos os known kinds que são prefixo válido;
   - escolher o **mais longo**;
   - em empate de comprimento, ordem lexical.
3. Construir `known` a partir do set e ordenar uma única vez antes do loop por tools.
4. Em `decodeStructFields`, executar `sort.Strings(leftover)` antes de retornar.
5. Procurar `Ecosystem` no repo inteiro. Se realmente não houver reader semântico:
   - remover `Tool.Ecosystem`;
   - remover as três atribuições do parser;
   - atualizar testes/JSON/hash somente se esse campo aparecia em representations internas.
6. Se existir reader legítimo quando o agente executar o plano, **não remover**. Em vez disso, definir semântica determinística: bucket explicitamente escrito pelo usuário, com prioridade por ordem lexical/config source documentada. Não escolher “primeiro map”.
7. Revisar diagnostics adicionais que iteram maps antes de concatenar erros. Não ampliar para todo o projeto; limitar ao parser/decoder tocado pela issue.

### Testes obrigatórios

- Unknown spelling com dois prefixes possíveis escolhe sempre o prefixo mais longo.
- Executar a mesma validação 100 vezes e comparar string completa de warnings/errors.
- Leftover keys saem lexicalmente ordenadas.
- Se `Ecosystem` for removido, `rg 'Ecosystem' internal/config` não deve deixar reader quebrado.

### Critérios de aceite

- Nenhum diagnostic afetado pela issue depende de Go map iteration order.

---

## #132 — Emit the multiple-schema warning once per invocation

### Objetivo

Discovery de schema não deve imprimir warnings durante construção da árvore Cobra.

### Estado atual

`internal/app/helpers.go:61` `defaultSchemaPath()` detecta múltiplos arquivos e escreve em stderr. Command constructors chamam essa função ao definir defaults, então um comando que nem usa schema pode imprimir o aviso várias vezes.

### Design a implementar

Separar **discovery puro** de **presentation**:

```go
type schemaDiscovery struct {
    Selected string
    Found    []string
}

func discoverDefaultSchema() schemaDiscovery // sem I/O
```

### Tarefas de implementação

1. Tornar `defaultSchemaPath()` puro, sem `fmt.Fprintf`/logger.
2. Preferencialmente fazer `defaultSchemaPath()` apenas retornar `discoverDefaultSchema().Selected`.
3. Emitir ambiguity warning somente no comando que efetivamente consome schema e somente se:
   - schema veio do auto-detect;
   - há mais de um candidate;
   - o usuário não passou `--schema` explicitamente.
4. Evitar copiar o mesmo bloco de warning para sete comandos. Criar helper `warnAmbiguousAutoSchema(cmd, discovery)` ou integrar no loader de #113.
5. `version`, completion e comandos que não leem schema não devem emitir warning.
6. Explicit `--schema` deve silenciar warning mesmo que existam outros candidates no CWD.
7. Preservar priority order `schema.toml`, `depengine.toml`, `depends.toml` existente.
8. Não cachear globalmente discovery entre testes/CWDs sem invalidation. Um `sync.Once` process-global é perigoso porque testes e embedded/in-process commands mudam CWD.

### Testes obrigatórios

Com dois candidates no diretório:

- `depengine version` => zero ambiguity warning;
- comando que usa auto schema => exatamente uma warning;
- `validate --schema schema.toml` => zero warning;
- selected file mantém priority existente;
- dois comandos construídos in-process em CWDs diferentes não compartilham cache stale.

### Critérios de aceite

- Command construction é side-effect-free em relação a schema ambiguity.

---

## #133 — Route state version probes through `probeRunner`

### Objetivo

Version discovery durante persistência deve ter a mesma classificação de logging que outras probes read-only.

### Estado atual

`internal/exec/state.go:23-36` chama:

```go
versioner.InstalledVersion(probeCtx, ex.rn, tool, method)
```

O `probeRunner` correto existe em `internal/exec/executor.go:263`.

### Tarefas de implementação

1. Substituir runner cru por:

```go
probe := ex.probeRunner(tool.Name, result.MethodKind)
version, err := versioner.InstalledVersion(probeCtx, probe, tool, method)
```

2. Se report usa display method diferente de `MethodKind`, decidir conscientemente qual string entra no contexto de log. Preferência: technical method kind para consistência com LookupAdapter.
3. Combinar com #104: `probeCtx` deve derivar do housekeeping/secret-scrubbed context.
4. Não mudar a política atual de `installedVersion` retornar vazio em erro nesta issue. O objetivo é classificação de probe, não tornar version probe fatal.

### Testes obrigatórios

- Usar `run.LoggingRunner` + logger capturável.
- Adapter Versioner faz subprocess/probe que retorna negative/nonzero esperado.
- Verificar que não aparece warning de mutation-style; contexto contém `probe=true`/equivalente.
- Erro de state persistence propriamente dito continua visível como erro, para não mascarar falha real.

### Critérios de aceite

- Nenhum `Versioner.InstalledVersion` chamado por state persistence recebe `ex.rn` diretamente.

---

## #134 — Serialize and attribute hook/source-preparation output under `--jobs`

### Objetivo

Cada write live deve ser atômico no nível de linha/chamada e dizer qual tool o produziu.

### Estado atual

- `Executor.outputf` escreve no writer sem mutex próprio.
- `recordToolResult` é serializado via report mutex, mas hooks/preparation chamam `outputf` fora dele.
- `internal/exec/hooks.go` imprime `pre-install`, `post-install`, etc. sem tool name consistente.
- `internal/exec/preparation.go` imprime `prepare: would add source ...` sem tool name.

### Tarefas de implementação

1. Adicionar `outputMu sync.Mutex` ao `Executor`.
2. Em `outputf`, adquirir `outputMu` em volta de **uma chamada** a `fmt.Fprintf`.
3. Não reutilizar `report.mu` para output. Report state e I/O são responsabilidades distintas.
4. Auditar ordem de locks:
   - `recordToolResult` hoje segura `report.mu` e chama `outputf`;
   - nenhum caminho pode segurar `outputMu` e depois tentar `report.mu`;
   - documentar essa regra para evitar deadlock futuro.
5. Alterar output de lifecycle hook para incluir tool name, por exemplo:

```text
    demo: pre-install: would run ...
    demo: post-install: skipped (...)
```

6. Alterar preparation lines para:

```text
    demo: prepare: would add source brew-tap vendor/tools
```

7. Localizar todas as chamadas `outputf` em `hooks.go` e `preparation.go` e manter formato consistente para success/skip/failure.
8. Não serializar o trabalho inteiro de cada tool. Só o write precisa ser sincronizado; paralelismo continua existindo.
9. Não prometer ordem determinística do progresso live sob concorrência. A garantia é linha íntegra + attribution; report final continua determinístico pela ordenação existente.
10. Se `outWriter` pode fazer short writes/concurrent callbacks em teste, mutex deve cobrir o `Fprintf` inteiro.

### Testes obrigatórios

- Writer fake que detecta chamadas concorrentes: `--jobs > 1` não produz concurrent Write.
- Dois tools com hooks/preparation em paralelo: cada linha contém exatamente um tool identificável.
- Nenhuma linha fica byte-interleaved.
- Report final conserva ordenação existente.
- `--quiet` semantics continuam corretas: não reintroduzir output que quiet deveria suprimir, salvo mensagens já intencionais.

### Critérios de aceite

- Live output pode intercalar **linhas**, jamais bytes/fragmentos, e toda linha de hook/preparation identifica o tool.

---

## #135 — Propagate archive rollback and backup cleanup failures

### Objetivo

Nenhuma falha que deixe estado de filesystem diferente do esperado deve desaparecer. Ao mesmo tempo, uma cleanup pós-commit não pode induzir fallback para outro candidate e causar dupla mutation.

### Estado atual

`internal/httpdownload/archive_install.go` contém vários descartes:

- `commitPayload`: `_ = os.RemoveAll(backup)`;
- commit failure: restore `_ = os.Rename(backup, dest)`;
- elevated clear/restore usa `_ = run.CheckResult(...)`;
- `rollbackPayload` e `removeOwned` retornam `void` e descartam todos os erros;
- launcher failure chama rollback/cleanup e retorna somente o erro original;
- success cleanup do backup também é descartado.

### Design a implementar

Separar três classes:

1. **pré-mutation cleanup/precondition**: falha é fatal antes de continuar;
2. **rollback/recovery após failure**: executar tudo que for possível e retornar `errors.Join(primary, recovery...)`;
3. **cleanup depois de commit bem-sucedido**: reportar claramente, mas não devolver um erro que faça executor tentar outro candidate como se install não tivesse ocorrido.

### Tarefas de implementação

1. Alterar:

```go
func rollbackPayload(...) error
func removeOwned(...) error
```

2. Em non-elevated `commitPayload`:
   - falha ao remover backup stale antes do commit => retornar erro antes de renomear dest;
   - se rename payload→dest falha e restore backup também falha, retornar `errors.Join` contendo os dois.
3. Em elevated path:
   - `rm -rf backup` prévio deve ser checado;
   - restore após failed `mv payload dest` deve ser juntado ao erro de commit.
4. Em launcher failure:
   - executar rollback do payload;
   - tentar remover **todos** launchers já criados, mesmo se um cleanup falhar;
   - acumular erros em slice;
   - retornar `errors.Join(launcherErr, rollbackErr, cleanupErrs...)`.
5. Após install + launchers totalmente commitados:
   - tentar remover backup;
   - se falhar, emitir warning explícito com path e causa;
   - manter a instalação como committed/success para impedir candidate fallback ou reinstall;
   - opcionalmente registrar debt/recovery metadata somente se já existir mecanismo apropriado. Não inventar WAL novo para isso nesta issue.
6. Cleanup de staging em defer também deve ser revisado:
   - se run já é success e remover raw/payload staging falha, warning;
   - não mudar success para recoverable candidate failure depois de host mutation commitada.
7. Criar seam de filesystem/command operations **instance/local ao pacote/teste**, evitando monkey-patch globals concorrentes. Pode ser struct `archiveOps` com `rename/removeAll/stat` + runner para elevated commands.
8. Mensagens devem preservar tanto primary error quanto recovery error. Usar `errors.Join` para `errors.Is/As` continuar útil.

### Testes obrigatórios

Non-elevated:

- stale backup cleanup falha => nenhum rename de dest/payload ocorre;
- commit falha + restore falha => erro contém ambos;
- launcher falha + rollback falha => erro contém ambos;
- dois launcher cleanups falham => ambos aparecem.

Elevated:

- `sudo rm backup` falha antes de commit => aborta;
- `sudo mv payload dest` falha + `sudo mv backup dest` falha => ambos aparecem;
- launcher rollback elevated propaga falha.

Pós-commit:

- backup cleanup falha depois de instalação completa => reporta warning, não dispara fallback/reinstall.

### Critérios de aceite

- Nenhum `_ =` resta em operações de rollback/restore/backup que podem mudar o estado final, exceto cleanup estritamente best-effort com warning explícito.

---

## #136 — Drain or abort external archive decoder output before `Wait`

### Revalidação importante

No commit base `754e0b25b69d`, o mecanismo principal descrito pela issue **já parece corrigido**:

- `internal/httpdownload/archive_native.go:374` chama `extractTarStream` antes de `pipe.Wait()`;
- `extractTarStream` em `:461-463` chama `verifyCompressedTarTrailer(ctx, r)` após o EOF lógico do TAR;
- `internal/httpdownload/tar_reader.go:65-77` drena o stream físico via `io.Copy(io.Discard, LimitedReader)` sob `contextReader`, com limite de 16 MiB;
- em erro, `extractExternalTar` chama `pipe.Abort()` antes de `Wait()`.

Isso resolve exatamente o deadlock “decoder tenta escrever trailing bytes, ninguém lê, Wait nunca termina”, desde que o comportamento esteja coberto por teste real do pipe externo.

### Objetivo desta issue agora

Adicionar a regressão ausente e fechar a issue se o teste confirmar o comportamento atual. Não reimplementar drain logic que já existe.

### Tarefas de implementação

1. Criar teste em `internal/httpdownload/archive_compressed_test.go` ou arquivo adjacente específico para external decoder.
2. Usar um `run.Runner`/`PipeRunner` fake compatível com `OpenStdoutPipe` que:
   - produz um TAR válido mínimo;
   - depois escreve trailing decompressed bytes em quantidade maior que um pipe buffer típico (por exemplo 256 KiB ou 1 MiB), mas menor que `maxTarTrailingBytes`;
   - só consegue concluir o writer se o consumer continuar drenando após EOF lógico.
3. Executar `extractExternalTar` com timeout curto de teste.
4. Assertar:
   - retorna sem deadline/deadlock;
   - `Wait()` ocorre somente depois do drain físico;
   - arquivo TAR válido foi extraído.
5. Adicionar segundo teste com trailing bytes `> maxTarTrailingBytes`:
   - deve retornar erro de trailing-data limit;
   - decoder deve ser abortado/reaped;
   - teste não pode deixar goroutine/processo pendurado.
6. Se o primeiro teste **já passa sem mudança de produção**, não alterar `archive_native.go` nem `tar_reader.go` apenas para produzir diff. Fechar #136 como completed com referência ao teste e às linhas do drain existente.
7. Se o primeiro teste falhar, corrigir somente o ordering/lifecycle necessário:
   - garantir drain/abort antes de `Wait`;
   - preservar limite/context cancellation existentes;
   - não introduzir um segundo goroutine drainer em paralelo com `verifyCompressedTarTrailer`.

### Critérios de aceite

- Teste reproduz a condição de pipe-pressure e prova ausência de deadlock.
- Decoder sempre é esperado/reaped ou abortado.
- Trailing output permanece bounded.
- Se produção já passar o teste, a implementação desta issue é apenas regressão + fechamento.

---

## 3. Checklist final obrigatório para cada agente

Antes de declarar uma issue concluída, o agente deve devolver evidência objetiva:

1. listar os arquivos modificados;
2. explicar em 3–8 linhas qual invariant mudou;
3. listar os testes novos/alterados pelo nome;
4. executar os testes focados do pacote tocado;
5. executar `go test -race` nos pacotes diretamente afetados quando viável;
6. executar `go vet` nos pacotes afetados;
7. executar `gofmt` nos arquivos Go tocados;
8. executar `git diff --check`;
9. confirmar que não alterou API/config format fora do escopo;
10. apontar qualquer teste global que falhou e separar claramente falha pré-existente de regressão.

Para mudanças transversais (#103, #110, #113, #116, #117, #128), o integrador deve depois rodar o gate global do repositório:

```sh
go build ./...
go test -race ./...
go vet ./...
golangci-lint run
```

Se a toolchain anexada for usada, manter `GOROOT`, `PATH` e cache conforme o bundle/toolchain fornecido pelo projeto em vez de baixar dependências ou ferramentas aleatórias da internet.

## 4. Critério de conclusão do programa de fixes

O conjunto completo só pode ser considerado encerrado quando:

- todas as issues #98–#136 possuem teste de regressão específico ou justificativa documentada de fechamento sem alteração de produção (#136 pode cair neste caso);
- nenhuma correção introduz registry/config/state global mutável por projeto;
- state/lock/rollback continuam fail-closed nas ambiguidades destrutivas;
- read-only commands observam exatamente o mesmo resolved project intent que install;
- todos os formatos/paths/diagnostics que precisam ser determinísticos têm teste determinístico;
- o gate global acima passa no commit integrado.
