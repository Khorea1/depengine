# TODO.md — depengine

> **Projeto:** depengine  
> **Data:** 2026-07-09  
> **Status dos testes:** ✅ `go test ./...` — 0 falhas (121 testes, 10/12 pacotes com testes)  
> **Build:** ✅ `go build -o depengine .` — limpo  
> **Próximo release:** `v0.2.0` — schema validation + debug refinements

Depois de três campanhas implementadas (Fundação, Executor+Adapters, Logging), o motor está completo e funcional. O foco imediato é **Campanha 3 (Validação de Schema)** e as **lacunas de debug** identificadas na última revisão.

# ✅ CAMPANHA 0: Fundação (CONCLUÍDA)

Tudo abaixo já está implementado e testado (`go test ./...` passa limpo):

| Pacote | Status | O que faz |
|--------|--------|-----------|
| `pkg/run` | ✅ | Runner interface, OSExecRunner, FakeRunner, propagação de `DEPENGINE_TRACE_ID` |
| `pkg/engine` | ✅ | Facts (espelho 1:1 do `detect_os.sh --json`), GatherFacts, ResolveFamily (12 clans), MatchesDistroFamily, timeoutCtx |
| `pkg/native` | ✅ | Registry declarativo de **15 distros** (12 originais + windows-winget, mint, opkg), Manager, Lookup, KnownClans, BuildInstallCmd/BuildCheckCmd/BuildSyncCmd com `{pkg}` + sudo |
| `pkg/schema` (parser) | ✅ | ParseSchema — normaliza as 3 formas de declaração (simple list, inline table, bloco `[tools.X]`) em Tool + MethodCandidate; ordenação por `method_order`; parser completo de `when = { distro_family = [...] }` |
| `pkg/schema` (placeholders) | ✅ | Expand, BuildMap, ExpandAll — substitui `{id}`, `{distro_family}`, `{arch}`, `{os}`, `{kernel}`, `{libc}`, `{init_system}`, `{is_wsl}`, `{is_container}`, `{is_android}` em **todas** as strings do schema; preserva `{pkg}` e `{latest}` intocados |
| `schema.toml` | ✅ | Template de referência com todos os 8 casos de declaração |
| `main.go` | ✅ | CLI demonstrativa: fatos, resolução, comandos nativos, avaliação de `when` |

---

# ✅ CAMPANHA 1: Executor + Adapters (P0 — CONCLUÍDA) ⚡ EXPANSÃO

O motor agora **parseia o schema, resolve fatos, gera comandos E instala automaticamente**.
Todas as 6 fases foram implementadas e testadas — `go test ./...` passa limpo (91 testes, 10/10 pacotes).

> **Escopo expandido durante implementação:** Além dos 6 adapters de linguagem planejados
> (cargo, go, pip, pipx, uv, aur), foram adicionados **~19 novos métodos de instalação**:
> 10 BaseAdapter (npm, gem, yarn, composer, apm, flatpak, snap, vscode, cask, mas),
> 4 adapters especializados (sdkman, steamcmd, yarn-berry, pacstall), 3 novos managers
> nativos (winget, mint, opkg), e aliases AUR (paru, yay). O `findClanByManager` teve
> bug corrigido com o mapa reverso `managerNameToClan`.

```
                    CAMPANHA 1 — EXECUTOR (visão geral)

    ┌──────────────────────────────────────────────────────────────────┐
    │                    FASE D: EXECUTOR CENTRAL                      │
    │  ┌──────────────────────────────────────────────────────────┐    │
    │  │  D.1 Core: method_order + fallback + postinstall + sync  │    │
    │  │  D.2 Report: resultados estruturados por tool            │    │
    │  │  D.3 Timeout: contexto + deadline por método             │    │
    │  └──────────────────────┬───────────────────────────────────┘    │
    └──────┬──────────┬───────┴───────┬──────────┬──────────────────────┘
           ▼          ▼               ▼          ▼
    ┌──────────┐ ┌──────────┐ ┌────────────┐ ┌──────────────┐
    │FASE B:   │ │FASE B:   │ │FASE C:     │ │FASE C:       │
    │Native    │ │Lang      │ │Git adapter │ │HTTP adapter  │
    │adapter   │ │adapters  │ │(clone +    │ │(download +   │
    │(wraps    │ │(cargo,go,│ │ build)     │ │ extrair +    │
    │pkg/native│ │ pip,pipx,│ │            │ │ {latest} +   │
    │+ which)  │ │ uv,aur)  │ │            │ │ checksum)    │
    └──────────┘ └──────────┘ └────────────┘ └──────────────┘
           ▲            ▲              ▲              ▲
           │            │              │              │
    ┌──────────────────────────────────────────────────────────────────┐
    │              FASE A: FUNDAÇÃO (compartilhada)                    │
    │  ┌──────────┐  ┌──────────┐  ┌──────────────────────────────┐   │
    │  │A.1 Adapter│  │A.2 Graph │  │A.3 Tipos compartilhados     │   │
    │  │interface  │  │topo-sort │  │    (InstallResult,          │   │
    │  │           │  │+ ciclo   │  │     MethodResult, ExecReport)│   │
    │  └──────────┘  └──────────┘  └──────────────────────────────┘   │
    └──────────────────────────────────────────────────────────────────┘

    ┌──────────────────────────────────────────────────────────────────┐
    │  FASE E: CLI (depengine install/check/dry-run/--verbose)        │
    └──────────────────────────────────────────────────────────────────┘

    ┌──────────────────────────────────────────────────────────────────┐
    │  FASE F: Testes de Integração (Docker)                          │
    └──────────────────────────────────────────────────────────────────┘
```

---

## 📐 Princípios da Arquitetura

```
Executor.orquestrar(schema, facts, clan)
  │
  ├─ 1. Resolver ordem topológica das tools (pkg/graph)
  │
  ├─ 2. Para cada tool (na ordem do grafo):
  │     │
  │     ├─ 2a. Para cada método (na ordem de method_order):
  │     │      ├─ when bate?  → não → PULA (log skip)
  │     │      ├─ adapter.Available()? → não → PULA (log unavailable)
  │     │      ├─ adapter.Check() ok?  → sim → PULA (já instalado)
  │     │      ├─ adapter.Install()    → ok  → ✅ SUCESSO
  │     │      └─ Install() falhou     → FALHA → tenta próximo método
  │     │
  │     ├─ 2b. Se algum método sucedeu → executa postinstall (se houver)
  │     └─ 2c. Se todos falharam       → log erro, CONTINUA (não aborta)
  │
  └─ 3. Reportar resumo (sucessos, falhas, pulados)
```

- **Cada tool tem timeout próprio** (não deixa apt-get ou cargo build travar)
- **Sync nativo** roda no máximo **uma vez por execução** (NeedsSync)
- **Nenhum adapter faz I/O real em testes** — tudo passa por `run.FakeRunner`
- **Logging**: usa `fmt.Fprintf(os.Stderr, ...)` por enquanto; migra para `pkg/log` na Campanha 2

---

## FASE A: Fundação (bloqueia B, C, D)

> **O quê**: interface Adapter, tipos de retorno, resolvedor de dependências.
> **Paralelizável**: A.1 + A.2 + A.3 podem ser feitos em qualquer ordem (dependem só de `pkg/run`, `pkg/schema`, `pkg/engine`, `pkg/native` que já existem).

---

### A.1 Interface Adapter + Tipos Compartilhados (`pkg/exec`)

- [ ] **Criar `pkg/exec/adapter.go`** — interface `Adapter` e tipos de resultado:
  ```go
  // Adapter é o contrato que todo método (native, cargo, git, http...)
  // implementa. O executor é genérico — conhece só esta interface.
  type Adapter interface {
      // Kind devolve o identificador do método: "native", "cargo", "git"...
      Kind() string

      // Available true se o sistema tem o runtime/binary necessário.
      // Ex: cargo → which cargo; native → which apt/pacman/etc.
      Available(ctx context.Context, rn run.Runner) bool

      // Check true se a ferramenta já está instalada por este método.
      Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool

      // Install executa a instalação e devolve erro se falhar.
      Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error
  }
  ```

- [ ] **Criar `pkg/exec/types.go`** — tipos de resultado do executor:
  ```go
  // StatusEnum representa o resultado da instalação de uma tool.
  type StatusEnum int
  const (
      StatusInstalled  StatusEnum = iota // instalado agora
      StatusAlready                       // já estava instalado
      StatusSkipped                       // pulado (when não bateu)
      StatusUnavailable                   // nenhum adapter disponível
      StatusFailed                        // todos métodos falharam
  )

  // ToolResult é o resultado de UMA tool.
  type ToolResult struct {
      Tool    string
      Status  StatusEnum
      Method  string          // método que sucedeu (ou último que falhou)
      Error   string          // preenchido só se StatusFailed
      Methods []MethodAttempt // histórico de tentativas (para log --verbose)
  }

  // MethodAttempt registra uma tentativa de método.
  type MethodAttempt struct {
      Kind   string
      Status string // "skip_when" | "skip_unavailable" | "skip_already" | "success" | "failed"
      Error  string // preenchido só se failed
  }

  // ExecReport é o resumo completo da execução, devolvido pelo executor.
  type ExecReport struct {
      Tools    []ToolResult
      Success  int
      Failed   int
      Skipped  int
      Already  int
      Duration time.Duration
  }
  ```
  > **Por que `StatusEnum` em vez de string?** — para o executor poder fazer
  > `switch` sem parse de string, e o report/formatter decidir a apresentação.

- [ ] **Criar `pkg/exec/report.go`** — formatadores de `ExecReport`:
  - [ ] `Report.Summary() string` — 1 linha: `"5 installed, 1 failed, 2 skipped, 3 already"`
  - [ ] `Report.Detail() string` — tabelinha com tool + status + método
  - [ ] `Report.JSON() string` — JSON estruturado para consumo programático
  - [ ] Testar cada formato com cenário típico

- [ ] **Criar `pkg/exec/registry.go`** — registro global de adapters:
  ```go
  var adapters = map[string]Adapter{}

  // Register insere um adapter no mapa. Panic se duplicado (detecção
  // precoce de conflito, não erro de runtime).
  func Register(a Adapter)

  // Lookup devolve o adapter pelo nome do método, ou nil se não existe.
  func Lookup(kind string) Adapter

  // RegisteredKinds devolve os kinds registrados (para debug/validação).
  func RegisteredKinds() []string
  ```
  - [ ] `Register` panic se kind já existir (fail-fast em init())
  - [ ] `Lookup` nil-safe: caller usa `if ad := Lookup(kind); ad != nil { ... }`
  - [ ] Testes: registra mock, lookup, duplicado detectado

- [ ] **Criar `pkg/exec/registry_test.go`**:
  - [ ] registra adapter mock → lookup devolve o mesmo adapter
  - [ ] registrar dois com mesmo kind → panic
  - [ ] lookup de kind inexistente → nil

---

### A.2 Resolvedor de Dependências (`pkg/graph`)

- [ ] **Criar `pkg/graph/sort.go`** — ordenação topológica + níveis:
  ```go
  // Sort devolve as tools em ordem topológica (níveis de profundidade).
  // tools é o mapa do schema (schema.Tools).
  // Retorna slice de slices: nível 0 (sem dependências), nível 1, etc.
  // Erro se ciclo for detectado — CycleError nomeia as tools envolvidas.
  func Sort(tools map[string]*schema.Tool) ([][]string, error)

  // CycleError é devolvido quando um ciclo é detectado.
  type CycleError struct {
      Cycle []string // tools no ciclo, na ordem do ciclo
  }
  func (e *CycleError) Error() string
  ```

- [ ] **Detecção de ciclos** com `CycleError` nomeando as tools envolvidas:
  ```go
  // Algoritmo: DFS com 3 cores (branco/cinza/preto).
  // Ao encontrar cinza → ciclo detectado, backtrace para construir Cycle.
  ```

- [ ] **Dependências compartilhadas** resolvidas uma vez só (já vem do grafo: a tool aparece no nível mais raso onde todas as suas dependências estão satisfeitas)

- [ ] **Criar `pkg/graph/graph_test.go`** — 6 cenários:
  - [ ] **Grafo linear**: `a → b → c` → `[[c] [b] [a]]` (c primeiro, pois a depende de b que depende de c)
  - [ ] **DAG complexo**: `a → [b,c], b → d, c → d` → `[[d] [b c] [a]]`
  - [ ] **Sem dependências**: 3 tools isoladas → um único nível com todas
  - [ ] **Ciclo detectado**: `a → b → c → a` → erro `CycleError` com `["a", "b", "c"]`
  - [ ] **Auto-dependência**: `a → a` → erro (ciclo trivial)
  - [ ] **Tools sem requires**: todas nível 0

---

### A.3 Helpers de Teste Compartilhados

- [ ] **Criar `pkg/exectest/adapter.go`** — adapter mock configurável:
  ```go
  // MockAdapter é um adapter controlado por teste. Cada chamada fica
  // registrada para assert.
  type MockAdapter struct {
      KindValue     string
      AvailableFunc func() bool
      CheckFunc     func(tool string) bool
      InstallFunc   func(tool string) error
      Calls         []MockCall
  }

  type MockCall struct {
      Method string // "Available" | "Check" | "Install"
      Tool   string
  }
  ```
  - Mock implementa `Adapter` — usado em todos os testes do executor
  - Cada chamada append em `Calls` para assert de ordem

- [ ] **Helper `NewInstallSchema`** para criar schemas mínimos em testes sem arquivo:
  ```go
  func NewInstallSchema(tools ...string) *schema.Schema {
      // Gera schema de teste inline (sem toml real)
  }
  ```

---

## FASE B: Adapters Core (depende de A.1)

> **O quê**: Native adapter + Language adapters (cargo, go, pip, pipx, uv, aur).
> **Paralelizável**: B.1 + B.2 podem começar juntos. Dentro de B.2,
> cada adapter é independente após a base estar pronta.

---

### B.1 Adapter Nativo (`pkg/exec/native.go`)

> ⚠️ **Não é trivial** — mesmo que `pkg/native` já exista, precisa de:
> `Available()` (verificar se o binário do manager existe), integração
> com `NeedsSync`, mapeamento de `MethodCandidate.Config["pkg"]` e
> tratamento de erro.

- [ ] **Criar `pkg/exec/native_adapter.go`** — adapter que wraps `pkg/native`:
  ```go
  type NativeAdapter struct {
      clan string // resolvido uma vez, reusado em toda chamada
  }

  func (a *NativeAdapter) Kind() string { return "native" }

  // Available: verifica se o manager existe no PATH.
  // Usa `which {manager_binary}` via runner.
  // Ex: clan=arch → `which pacman`
  func (a *NativeAdapter) Available(ctx context.Context, rn run.Runner) bool

  // Check: executa BuildCheckCmd e verifica exit 0.
  // Lê pkg de mc.Config["pkg"] (já expandido pelo parser).
  func (a *NativeAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool

  // Install: executa BuildInstallCmd.
  // Se clan tem NeedsSync true E sync ainda não rodou nesta execução:
  //   executa BuildSyncCmd primeiro (flag de sessão no adapter).
  func (a *NativeAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error
  ```

- [ ] **`NeedsSync` — sincronização única por sessão**:
  - NativeAdapter tem flag `synced bool` setada após primeiro `BuildSyncCmd`
  - Reseta ao criar novo adapter (nova execução = nova sessão)
  - Log do sync: `"native: syncing package index (apt-get update)..."`

- [ ] **Mapeamento de `mc.Config["pkg"]`**:
  - Lê `mc.Config["pkg"]` como string (já expandido pelo parser)
  - Se não existir, usa `tool.Name` como fallback (para compatibilidade com `simple`)

- [ ] **Criar `pkg/exec/native_adapter_test.go`**:
  - [ ] `Available()` com manager existente → true
  - [ ] `Available()` com manager inexistente → false
  - [ ] `Check()` exit 0 → true
  - [ ] `Check()` exit 1 → false
  - [ ] `Install()` com sucesso → nil
  - [ ] `Install()` com erro → erro
  - [ ] `Install()` com NeedsSync → sync executado antes (e apenas uma vez)

- [ ] **Registrar no registry**: `func init() { Register(NewNativeAdapter("")) }`

---

### B.2 Language Adapters — Estrutura Base (`pkg/lang`)

> Todos seguem o mesmo pattern: check via which/binary, install via comando.
> A estrutura base evita repetição.

- [ ] **Criar `pkg/lang/base.go`** — adapter base reusável:
  ```go
  // BaseConfig descreve um adapter de linguagem típico.
  type BaseConfig struct {
      KindName    string   // "cargo", "go", "pip"...
      Binary      string   // binário que precisa existir no PATH (ex: "cargo")
      CheckCmd    []string // comando com {pkg} para verificar instalação
      InstallCmd  []string // comando com {pkg} para instalar
      NeedsBinary string   // binário alternativo para Available() (ex: "pip3")
  }

  // BaseAdapter implementa Adapter com BaseConfig.
  // Cada adapter concreto só precisa passar a config no construtor.
  type BaseAdapter struct {
      config BaseConfig
  }

  func (a *BaseAdapter) Kind() string { return a.config.KindName }
  func (a *BaseAdapter) Available(ctx, rn) bool  // which a.config.Binary
  func (a *BaseAdapter) Check(ctx, rn, tool, mc) bool  // executa CheckCmd
  func (a *BaseAdapter) Install(ctx, rn, tool, mc) error  // executa InstallCmd
  ```
  > **Por que base?** — 6 adapters com o mesmo pattern. A base reduz
  > cada adapter a ~10 linhas + registro.

- [ ] **Resolver `{pkg}` no adapter base**:
  - `CheckCmd` e `InstallCmd` contêm `{pkg}` no template
  - Substituir por `mc.Config["pkg"]` (ou `tool.Name` se ausente)
  - Usar `strings.ReplaceAll` igual `pkg/native/command.go`

- [ ] **Criar `pkg/lang/registry.go`** — registro específico de lang adapters:
  ```go
  // RegisterAll insere todos os adapters de linguagem no registry global
  // (pkg/exec.Register). Chamado uma vez em init().
  func RegisterAll()
  ```
  - Separado do registry global para testes poderem limpar e registrar só o necessário

- [ ] **Criar `pkg/lang/base_test.go`** — testes do BaseAdapter:
  - [ ] `Available()` com binário mockado → true (FakeRunner exit 0)
  - [ ] `Available()` sem binário → false (FakeRunner com erro "not found")
  - [ ] `Check()` com `{pkg}` substituído corretamente
  - [ ] `Install()` comando correto e propagação de erro

---

### B.3 — B.19 Adapters Concretos (`pkg/lang/*.go`)

> **Escopo expandido:** O plano original previa 6 adapters (B.3-B.8). Durante a
> implementação, foram adicionados **17 adapter kinds** em `pkg/lang`: 10 via
> BaseAdapter genérico, 1 CargoAdapter (com suporte a git), 1 AURAdapter (com
> aliases nomeados paru/yay), 4 especializados (não-BaseAdapter).

#### BaseAdapter (genérico — `pkg/lang/base.go` + `Configs` map)

| # | Adapter | `Kind()` | Binary | CheckCmd | InstallCmd | Notas |
|---|---------|----------|--------|----------|------------|-------|
| B.3 | **cargo** | `"cargo"` | `cargo` | `cargo install --list` | `cargo install {pkg}` | `{pkg}` vira `--git url` se `mc.Config["git"]` setado; usa adapter próprio (CargoAdapter) |
| B.4 | **go** | `"go"` | `go` | `which {pkg}` | `go install {pkg}@latest` | `{pkg}` é import path |
| B.5 | **pip** | `"pip"` | `pip`/`pip3` | `pip show {pkg}` | `pip install {pkg}` | `AvailableExtra: "pip3"` — fallback se `pip` não existir |
| B.6 | **pipx** | `"pipx"` | `pipx` | `pipx list` | `pipx install {pkg}` | check: grep pelo nome |
| B.7 | **uv** | `"uv"` | `uv` | `uv tool list` | `uv tool install {pkg}` | check: grep pelo nome |
| B.8 | **npm** | `"npm"` | `npm` | `npm ls -g --depth=0 {pkg}` | `npm install -g {pkg}` | — |
| B.9 | **gem** | `"gem"` | `gem` | `gem list {pkg}` | `gem install {pkg}` | — |
| B.10 | **yarn** | `"yarn"` | `yarn` | `yarn global list --depth=0 \| grep -q {pkg}` | `yarn global add {pkg}` | — |
| B.11 | **composer** | `"composer"` | `composer` | `composer global show --locked {pkg}` | `composer global require {pkg}` | — |
| B.12 | **apm** | `"apm"` | `apm` | `apm list --installed --bare \| grep -q {pkg}` | `apm install {pkg}` | Atom package manager |
| B.13 | **flatpak** | `"flatpak"` | `flatpak` | `flatpak info {pkg}` | `flatpak install -y flathub {pkg}` | — |
| B.14 | **snap** | `"snap"` | `snap` | `snap list {pkg}` | `snap install {pkg}` | — |
| B.15 | **vscode** | `"vscode"` | `code` | `code --list-extensions \| grep -q {pkg}` | `code --install-extension {pkg}` | `AvailableExtra: "code-insiders"` |
| B.16 | **cask** | `"cask"` | `brew` | `brew list --cask {pkg}` | `brew install --cask {pkg}` | macOS via Homebrew Cask |
| B.17 | **mas** | `"mas"` | `mas` | `mas list \| grep -q {pkg}` | `mas install {pkg}` | Mac App Store |

#### Especializados (não-BaseAdapter — implementam `exec.Adapter` diretamente)

| # | Adapter | `Kind()` | Binary | Notas |
|---|---------|----------|--------|-------|
| B.18 | **aur** | `"aur"` | lê `defaults.aur_helper` | `{helper} -Qi {pkg}` / `{helper} -S --noconfirm {pkg}` |
| | **→ paru** | `"paru"` | `paru` | alias AUR que delega ao AURAdapter com helper=paru |
| | **→ yay** | `"yay"` | `yay` | alias AUR que delega ao AURAdapter com helper=yay |
| B.19 | **sdkman** | `"sdkman"` | `sdk` | SDKMAN via `~/.sdkman/bin/sdk`; check por diretório de candidato |
| B.20 | **steamcmd** | `"steamcmd"` | `steamcmd` | check por diretório de instalação; guard contra `os.Stat("")` |
| B.21 | **yarn-berry** | `"yarn-berry"` | `yarn` | Yarn v2+; version gating com `parseMajorVersion()` (>=2); install é project-local |
| B.22 | **pacstall** | `"pacstall"` | `pacstall` | AUR-style para Debian; `-Ci` para check |
  - [x] ✅ **Todos os 17 adapters de `pkg/lang` estão implementados e registrados** via `RegisterAll()` em `pkg/lang/registry.go`
  - [x] BaseAdapter genérico cobre npm, gem, yarn, composer, apm, flatpak, snap, vscode, cask, mas
  - [x] CargoAdapter com suporte a `mc.Config["git"]` → `cargo install --git {url}`
  - [x] AURAdapter com helper configurável + aliases nomeados paru/yay via `aur_alias.go`
  - [x] SDKManAdapter, SteamCMDAdapter, YarnBerryAdapter, PacstallAdapter (especializados)

##### Expansão de Managers Nativos (`pkg/native`)

Além dos 12 clans originais, foram adicionados:

  - [x] **windows-winget** — manager nativo para Windows (`winget install --id {pkg}`)
  - [x] **mint** — Linux Mint (usa `apt` mas com clan distinto)
  - [x] **opkg** — Embedded Linux (OpenWrt, LEDE)
  - [x] **gentoo → "emerge"** — `Manager.Name` alterado de `"portage"` para `"emerge"` (corresponde ao binário real)

##### Bug Fix: `findClanByManager`

  - [x] **Criado mapa reverso `managerNameToClan`** em `pkg/native/registry.go` para resolver binário → clan
  - [x] Emerge/portage agora resolvem corretamente para o clan gentoo
  - [x] `"pkg"` (compartilhado entre termux e freebsd) propositalmente ausente do mapa reverso — fallback via iteração de KnownClans lida com ambos
  - [x] yum mapeado para fedora (symlink para dnf em sistemas modernos)

##### Code Review (5 findings — todos aplicados e testados)

  - [x] **Fix 1** — `pacstall.Check`: `-Qi` → `-Ci` (flag correta para pacotes instalados)
  - [x] **Fix 2** — `managerNameToClan`: entrada `"pkg"` removida (binário compartilhado entre termux e freebsd)
  - [x] **Fix 3** — `yarnberry.go`: `version[0] >= '2'` → `parseMajorVersion()` com `strconv.Atoi` (manuseia v-prefix, multi-digit, non-numeric)
  - [x] **Fix 4** — `steamcmd.go`: guard contra `os.Stat("")` quando `installDirFromConfig` retorna vazio
  - [x] **Fix 5** — `yarnberry.go`: docstring NOTE documentando que Berry installs são project-local

##### Testes de Regressão

  - [x] **`pkg/lang/adapters_review_test.go`** — 4 testes novos: `TestParseMajorVersion`, `TestYarnBerryAvailableVersionGating`, `TestPacstallCheckUsesCorrectFlag`, `TestSteamCMDCheckWithEmptyInstallDir`
  - [x] **`pkg/exec/native_adapter_test.go`** — subtest para `"pkg"` fallback (resolves to termux ou freebsd)
  - [x] **`pkg/native/native_test.go`** — testes para gentoo, mint, opkg, windows-winget

---

## FASE C: Adapters Complexos (depende de A.1)

> **O quê**: Git adapter (clone + build) e HTTP adapter (download + extrair + {latest} + checksum).
> **Paralelizável**: C.1 e C.2 são independentes.

---

### C.1 Adapter Git (`pkg/git`)

- [ ] **Criar `pkg/git/adapter.go`**:

  ```go
  type GitAdapter struct{}

  func (a *GitAdapter) Kind() string { return "git" }

  // Available: `which git` via runner.
  func (a *GitAdapter) Available(context.Context, run.Runner) bool

  // Check: `test -d {extract_to}/.git` OU verifica binário no PATH.
  // Se extract_to existe e contém .git, considera instalado.
  func (a *GitAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool

  // Install:
  //   1. Cria temp dir (ou usa mc.Config["extract_to"] se definido)
  //   2. `git clone --depth 1 {url} {dir}` (shallow clone)
  //   3. Se mc.Config["build"] existe: executa build no dir clonado
  //   4. Se mc.Config["extract_to"]: copia artefatos
  //   5. Cleanup temp dir se tudo ok
  func (a *GitAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error
  ```

- [ ] **`clone` com `{url}`**:
  - URL vem de `mc.Config["url"]` (já expandido pelo parser)
  - Se `mc.Config["depth"]` existe, usa esse valor (default: 1)
  - Se `mc.Config["branch"]` existe, append `--branch {branch}`

- [ ] **`build` step**:
  - Comando de build vem de `mc.Config["build"]` (string já expandida)
  - Executa via runner no diretório do clone
  - stdout/stderr capturados para log

- [ ] **`extract_to`**:
  - Se definido, copia o binário/artefato para `extract_to` após build
  - Usa `cp` ou `install -m 755`

- [ ] **`{latest}` no git**:
  - Se URL contém `{latest}`, resolve via `git ls-remote --tags` + pega a tag mais recente (semântica semver)
  - **Simplificação v0.1**: `{latest}` no git = usar o default branch (main/master). Resolução via tags fica para v0.2.

- [ ] **Criar `pkg/git/adapter_test.go`**:
  - [ ] `Available()` com git instalado → true (FakeRunner)
  - [ ] `Available()` sem git → false
  - [ ] `Check()` com diretório existente → simula `test -d`
  - [ ] `Install()` gera comando `git clone --depth 1` correto
  - [ ] `Install()` com `build` step → executa build após clone
  - [ ] `Install()` com `extract_to` → cp após build
  - [ ] `Install()` com `branch` → `--branch` incluso
  - [ ] Todos via FakeRunner — sem git real

---

### C.2 Adapter HTTP/Download (`pkg/httpdownload`)

> O adapter mais complexo: download, extração, checksum, resolução de {latest}.

- [ ] **Criar `pkg/httpdownload/adapter.go`**:
  ```go
  type HTTPAdapter struct{}

  func (a *HTTPAdapter) Kind() string { return "http" }

  // Available: `which curl` → true. Fallback: `which wget`. Se nenhum,
  // tenta Go net/http (sempre disponível em binário Go compilado).
  // Retorna true se pelo menos um backend está disponível.
  func (a *HTTPAdapter) Available(context.Context, run.Runner) bool

  // Check: verifica se o binário ou diretório de extração existe.
  // Critério: mc.Config["extract_to"] existe e não está vazio → true.
  // Senão: mc.Config["binary"] no PATH → true.
  func (a *HTTPAdapter) Check(ctx, rn, tool, mc) bool

  // Install:
  //   1. Resolve URL: substitui {pkg} se presente; resolve {latest} se houver
  //   2. Determina backend (Go net/http > curl > wget)
  //   3. Download para arquivo temporário
  //   4. Verifica checksum (se configurado)
  //   5. Extrai baseado no tipo (.tar.gz, .zip, .deb, binário)
  //   6. Se .deb: executa dpkg -i (via runner com sudo)
  //   7. Se binário: chmod +x e copia para extract_to
  //   8. Cleanup arquivo temporário
  //   9. Se extract_to definido: copia artefatos extraídos
  func (a *HTTPAdapter) Install(ctx, rn, tool, mc) error
  ```

- [ ] **Criar `pkg/httpdownload/backend.go`** — seleção de backend:
  ```go
  // Downloader baixa URL para um caminho local.
  type Downloader interface {
      Download(ctx context.Context, url, dest string) error
  }

  // GoDownloader usa net/http padrão (stdlib, sem dep externa).
  type GoDownloader struct{}

  // CurlDownloader usa `curl -fsSL -o {dest} {url}`.
  type CurlDownloader struct{ rn run.Runner }

  // WgetDownloader usa `wget -q -O {dest} {url}`.
  type WgetDownloader struct{ rn run.Runner }

  // SelectDownloader escolhe o melhor backend disponível:
  // Go net/http (sempre disponível em binário Go) > curl > wget
  func SelectDownloader(ctx, rn) Downloader
  ```
  > **Go net/http como primário**: binário Go compilado sempre tem net/http.
  > curl/wget são fallback para quando o Go stdlib encontra problemas
  > (certificados, proxies exóticos).

- [ ] **Criar `pkg/httpdownload/resolver.go`** — resolução de `{latest}`:
  ```go
  // ResolveLatest substitui `{latest}` em url pela versão mais recente.
  // Suporta:
  //   - GitHub API: /repos/{owner}/{repo}/releases/latest → tag_name
  //   - GitHub API: /repos/{owner}/{repo}/tags → última tag semver
  //   - Futuro: GitLab, self-hosted (v0.2)
  //
  // Usa Go net/http com timeout 10s. Não depende de nenhuma lib externa.
  func ResolveLatest(ctx context.Context, url string) (resolved string, err error)
  ```
  - [ ] Parse de URL GitHub para extrair owner/repo
  - [ ] Chamada à API GitHub (`/repos/{owner}/{repo}/releases/latest`)
  - [ ] Fallback: `/repos/{owner}/{repo}/tags` → última por semver
  - [ ] Cache: mesmos owner/repo na mesma execução não refazem HTTP
  - [ ] Testes com HTTP mock (sem rede real)

- [ ] **Criar `pkg/httpdownload/extract.go`** — extração de arquivos:
  ```go
  // Extract descomprime src em dest baseado no tipo.
  // Tipos suportados: .tar.gz, .tgz, .tar.bz2, .tar.xz, .zip
  // Binário direto (sem extensão de archive): copia para dest e chmod +x.
  // .deb: executa dpkg -i (via runner com sudo).
  //
  // Usa Go stdlib (archive/tar, archive/zip, compress/gzip, compress/bzip2,
  // compress/xz via github.com/ulikunitz/xz — ou chama tar/bsdtar externo).
  func Extract(src, dest, fileType string, rn run.Runner) error
  ```
  - [ ] Detecção de tipo por extensão `.tar.gz`, `.tgz`, `.tar.bz2`, `.tar.xz`, `.zip`
  - [ ] Sem extensão conhecida → trata como binário (copia + chmod +x)
  - [ ] `.deb` → delega para `dpkg -i` (runner com sudo)
  - [ ] **Simplificação v0.1**: usa `tar` externo para tgz (em vez de Go stdlib) para evitar dependências. Go stdlib archive fica para v0.2.
  - [ ] `extract_to` do schema: diretório de destino da extração
  - [ ] Testes: cada tipo de archive (cria archive pequeno em testdata)

- [ ] **Criar `pkg/httpdownload/checksum.go`** — verificação de checksum:
  ```go
  // Checksum verifica a integridade de um arquivo baixado.
  //
  // mc.Config["checksum"] pode ser:
  //   "sha256:<hash>"  → calcula sha256 do arquivo e compara
  //   "sha256:auto"    → baixa arquivo .sha256 junto com o asset e valida
  //   ausente          → skip (sem verificação)
  //
  // Em caso de mismatch: erro (não tenta instalar arquivo corrompido).
  func VerifyChecksum(filePath, checksum string) error

  // SHA256File calcula o hash SHA-256 de um arquivo.
  func SHA256File(path string) (string, error)
  ```
  - [ ] `sha256:auto`: baixa `<url>.sha256` ou `<url>.sha256sum` e parseia
  - [ ] Erro claro: `"checksum mismatch: esperado abc123, obtido def456"`
  - [ ] Testes: hash correto, hash errado, sha256:auto

- [ ] **Criar `pkg/httpdownload/adapter_test.go`**:
  - [ ] `Available()` com curl → true
  - [ ] `Available()` sem curl → true (fallback Go net/http)
  - [ ] `Install()` gera comando curl correto
  - [ ] `Check()` com binário no PATH → true
  - [ ] `Install()` com checksum válido → sucesso
  - [ ] `Install()` com checksum inválido → erro
  - [ ] `ResolveLatest()` com URL GitHub → URL resolvida (mock HTTP)
  - [ ] `Extract()` .tar.gz → arquivos extraídos (usa testdata)
  - [ ] `Extract()` binário direto → copiado + chmod +x

---

## FASE D: Executor Central (depende de A + B + C)

> **O quê**: Orquestrador que junta tudo: schema → graph → adapters → install.
> **Depende de**: Fases A, B, C completas.
> **Não paralelizável**: D.1 → D.2 → D.3 são sequenciais.

---

### D.1 Core Executor (`pkg/exec/executor.go`)

- [ ] **Criar `pkg/exec/executor.go`** — orquestrador principal:
  ```go
  // Executor orquestra a instalação de todas as tools do schema.
  type Executor struct {
      facts *engine.Facts
      clan  string
      rn    run.Runner
      // timeouts por tool e por método (configurável)
      toolTimeout   time.Duration
      methodTimeout time.Duration
  }

  // New cria um executor com configurações padrão.
  func New(facts *engine.Facts, clan string) *Executor

  // Option é uma função de configuração funcional.
  type Option func(*Executor)
  func WithRunner(rn run.Runner) Option
  func WithToolTimeout(d time.Duration) Option
  func WithMethodTimeout(d time.Duration) Option

  // Execute roda todas as tools do schema na ordem do grafo.
  func (ex *Executor) Execute(ctx context.Context, schema *schema.Schema, opts ...Option) (*ExecReport, error)
  ```

- [ ] **Fluxo interno do `Execute`:**
  1. **Resolver grafo**: `graph.Sort(schema.Tools)` → níveis
     - Se ciclo: retorna erro com `CycleError` (exit code 2)
  2. **Para cada nível** (nível 0 primeiro):
     - Tools em paralelo ou serial? **Serial v0.1**: uma por vez (simplifica logging e diagnóstico. Paralelo v0.2)
  3. **Para cada tool no nível**:
     ```
     a. Para cada método em tool.Methods (ordenado por method_order):
        ├─ when não bate?           → log SKIP (when), próx método
        ├─ adapter = Lookup(kind)?  → não → log SKIP (unknown), próx método
        ├─ !adapter.Available()     → log SKIP (unavailable), próx método
        ├─ adapter.Check() ok?      → log ALREADY, break (próxima tool)
        ├─ adapter.Install()
        │  ├─ sucesso               → log SUCCESS, break
        │  └─ erro                  → log FAIL, próx método
        └─ (fim do loop)
     
     b. Se algum método sucedeu E tool.PostInstall != "":
        ├─ Executa postinstall via runner
        ├─ stdout/stderr logados
        └─ Se falhar: log WARN (não aborta)
     
     c. Nenhum método sucedeu:
        ├─ log ERROR + ToolResult{Status: StatusFailed}
        └─ CONTINUA (não aborta execução)
     ```
  4. **Montar `ExecReport`** com todos os `ToolResult`
  5. **Log resumo final**: `"5 installed, 1 failed, 2 skipped, 3 already"`

- [ ] **Contexto com timeout por ferramenta**:
  - Cada tool ganha timeout próprio (default: 5min por tool)
  - Cada método dentro da tool ganha timeout (default: 2min por método)
  - `WithToolTimeout` e `WithMethodTimeout` sobrescrevem defaults
  - Context filho criado para cada tool: `ctxTool, cancel := context.WithTimeout(ctx, ex.toolTimeout)`
  - Context neto criado para cada método: `ctxMethod, cancel := context.WithTimeout(ctxTool, ex.methodTimeout)`
  - Timeout cancelado após sucesso (defer cancel)

- [ ] **Criar `pkg/exec/sync.go`** — gerenciador de sync nativo:
  ```go
  // SyncManager gerencia a sincronização de índice de pacotes nativos.
  // Roda sync no máximo uma vez por execução.
  type SyncManager struct {
      rn         run.Runner
      clan       string
      synced     bool
  }

  // NeedsSync devolve true se o clan tem NeedsSync.
  func (sm *SyncManager) NeedsSync() bool

  // Sync executa o sync se necessário e ainda não tiver rodado.
  // Loga o comando e resultado.
  func (sm *SyncManager) Sync(ctx context.Context) error
  ```
  - [ ] Integrado no executor: antes do loop de tools, `SyncManager.Sync(ctx)` se necessário
  - [ ] Se sync falhar: log WARN, continua (pode estar offline, mas tenta install)

---

### D.2 Testes do Executor (`pkg/exec/executor_test.go`)

- [ ] **Criar `pkg/exec/executor_test.go`**:

  - [ ] **Fallback**: método A falha → método B sucede:
    - Tool com 2 métodos: mock A falha, mock B sucede
    - Assert: report mostra sucesso via método B

  - [ ] **`when` pula método inaplicável**:
    - Tool com método com `when = {distro_family = ["debian"]}` em sistema arch
    - Assert: método pulado, report mostra skipped

  - [ ] **Todas falham → resumo correto**:
    - Tool com 2 métodos, ambos falham
    - Assert: `StatusFailed`, report contém erros

  - [ ] **`postinstall` executado após sucesso**:
    - Tool com postinstall e método que sucede
    - Assert: comando postinstall executado via runner

  - [ ] **Já instalado (Check) → skip**:
    - Tool com método que retorna Check=true
    - Assert: `StatusAlready`, Install não chamado

  - [ ] **Adapter unavailable → pula método**:
    - Tool com método em que adapter.Available() = false
    - Assert: método pulado

  - [ ] **NeedsSync executado antes do install**:
    - Schema com clan debian (NeedsSync=true)
    - Assert: sync executado antes do primeiro install

  - [ ] **Ordem topológica respeitada**:
    - Tools A (requires: [C]) e B (requires: [C, D]) e C, D (sem requires)
    - Assert: ordem D → C → A → B (ou D → C → B → A) — C antes de A e B, D antes de B

  - [ ] **Dry-run não executa install**:
    - Executor em modo dry-run
    - Assert: nenhum Install chamado, Check chamado, report completo

---

### D.3 Dry-Run Mode

- [ ] **Implementar dry-run no executor**:
  ```go
  func WithDryRun() Option
  ```
  - Quando ativo: chama `Available()` e `Check()` normalmente, mas **nunca chama `Install()`**
  - Log: `"[DRY-RUN] would install {tool} via {method}"`
  - Report: mostra `StatusWouldInstall` para cada tool não instalada
  - Útil para: `depengine --dry-run schema.toml` mostrar o plano

---

## FASE E: CLI (depende de D)

> **O quê**: Comandos `install`, `check`, flags `--dry-run`, `--verbose`.

---

### E.1 Comando `install`

- [ ] **Atualizar `main.go`** — estrutura de subcomandos:
  ```go
  // Estrutura do CLI:
  //   depengine install [flags] [schema.toml]
  //   depengine check [flags] <tool>
  //   depengine version

  // flag --schema (default: ./schema.toml)
  // flag --dry-run (default: false)
  // flag --verbose (default: false, -v shorthand)
  // flag --only (default: "", instala só uma tool específica)
  // flag --skip (default: "", pula tools específicas, separadas por vírgula)
  ```

- [ ] **Fluxo do `depengine install`**:
  1. Parse flags
  2. GatherFacts
  3. ResolveFamily → clan
  4. BuildMap(facts, clan)
  5. ParseSchema(caminho, buildMap) → schema
  6. NewExecutor(facts, clan)
  7. Se --dry-run: `WithDryRun()`
  8. Se --verbose: log detalhado (fmt.Fprintf stderr)
  9. Execute(ctx, schema)
  10. Se --verbose/--dry-run: print Detail() do report
  11. Se --json: print JSON()
  12. Print Summary() no final
  13. Exit code: 0 se tudo ok, 1 se alguma falhou, 2 se erro de schema

- [ ] **Exit codes**:
  - `0`: tudo instalado com sucesso (ou já instalado)
  - `1`: alguma tool falhou (mas o processo continuou)
  - `2`: erro de schema (parse, ciclo, etc.)
  - `3`: erro de runtime (detect_os.sh não encontrado, etc.)

- [ ] **Syscall para sudo**:
  - Comandos com `sudo` prefix rodam via runner com `sudo`
  - Se `sudo` não existe no container/termux: log warning, tenta sem
  - `DEPENGINE_NOSUDO=1` desativa sudo (para containers)

---

### E.2 Comando `check`

- [ ] **`depengine check <tool>`** — verifica se uma ferramenta está instalada:
  1. GatherFacts + ResolveFamily
  2. ParseSchema (opcional: se não passar schema, usa heurística)
  3. Para a tool específica, testa cada método:
     - adapter.Available()?
     - adapter.Check()?
  4. Output: "✓ tool (installed via {method})" ou "✗ tool (not found)"
  5. Exit code: 0 se instalado, 1 se não

---

### E.3 Flag `--verbose` / `-v`

- [ ] Log detalhado em stderr:
  ```
  [depengine] facts: clan=arch, distro=archlinux, arch=x86_64
  [depengine] schema: 12 tools, 3 com requires
  [depengine] graph: 4 níveis de profundidade
  [depengine] tool=zsh: trying native... available=true, installed=false
  [depengine] tool=zsh: native install: sudo pacman -S --noconfirm --needed zsh
  [depengine] tool=zsh: native install → success (3.2s)
  [depengine] tool=bat: trying native... available=true, installed=true → skip
  [depengine] tool=starship: trying native... pkg='starship' not in apt
  [depengine] tool=starship: trying cargo... available=true
  [depengine] tool=starship: cargo install starship
  [depengine] tool=starship: cargo install → success (45.2s)
  [depengine] tool=starship: postinstall → success
  ```

- [ ] **Tabela final em --verbose**:
  ```
  Tool         Status    Method     Duration
  ──────────────────────────────────────────
  zsh          installed native     3.2s
  bat          already   native     0.1s
  starship     installed cargo      45.2s
  lazygit      failed    —          (all 2 methods failed)

  Summary: 2 installed, 1 failed, 0 skipped, 1 already
  ```

---

## FASE F: Testes de Integração — ✅ Docker

> **O quê**: Testes reais em Docker que validam o motor end-to-end.
> **Status**: Implementado. Script `tests/integration/run.sh` cobre todos os cenários.

---

### F.1 Infraestrutura de Testes ✅

- [x] **Criar `tests/integration/docker-compose.yml`**:
  ```yaml
  services:
    debian:   # build: Dockerfile.debian
    arch:     # build: Dockerfile.arch
    fedora:   # build: Dockerfile.fedora
    alpine:   # build: Dockerfile.alpine
  ```

- [x] **Criar `tests/integration/Dockerfile.debian`** — Debian bookworm com Go + git + curl
- [x] **Criar `tests/integration/Dockerfile.arch`** — Arch Linux com Go + git + curl
- [x] **Criar `tests/integration/Dockerfile.fedora`** — Fedora com Go + git + curl
- [x] **Criar `tests/integration/Dockerfile.alpine`** — Alpine com Go + git + curl (testa fallback)
- [x] **Criar `tests/integration/test_schema.toml`** — 15 tools cobrindo todos os métodos:
  - native (curl, git, zsh), fallback entre distros (fd com apt/pacman/dnf)
  - `when` (debian-only-tool, when=debian)
  - `requires` (tool-a → tool-b → tool-c)
  - cargo (lazygit), go (fzf), git (fff), http (shellcheck)
  - postinstall (hello-world)

- [x] **Criar `tests/integration/run.sh`** que:
  - Compila binário localmente
  - Builda imagens Docker (debian, arch, fedora, alpine)
  - Roda dry-run em cada distro e verifica output
  - Testa fallback cargo em Alpine
  - Testa `when` (debian-only em Debian vs Arch)
  - Testa `requires` (ordem topológica)
  - Testa `--json` output
  - Testa `depengine check <tool>`
  - Reporta summary (pass/fail)

---

### F.2 Cenários de Integração ✅

- [x] **Dry-run em Debian**: zsh, git, curl — "already installed" via apt
- [x] **Dry-run em Arch**: zsh, git, curl — "already installed" via pacman
- [x] **Dry-run em Fedora**: zsh, git, curl — "already installed" via dnf
- [x] **Dry-run em Alpine**: sem manager nativo → fallback (cargo, go, git)
- [x] **Fallback nativo→cargo**: lazygit via cargo em Alpine
- [x] **`when`**: debian-only-tool executado em Debian, pulado em Arch
- [x] **`requires`**: tool-c → tool-b → tool-a na ordem topológica
- [x] **JSON output**: `--json` produz JSON válido com summary e tools
- [x] **`depengine check <tool>`**: verifica git instalado
- [x] **Dry-run não modifica**: todas as ferramentas rodam com --dry-run

---

## 📊 Mapa de Dependências entre Fases

```
FASE A (Fundação)
  A.1 Interface + Tipos ──────┬──────────────────────────────┐
  A.2 Graph (topo-sort) ──────┤                              │
  A.3 MockAdapter (testes) ───┘                              │
                                                             ▼
FASE B (Adapters Core)                              ┌──────────────────┐
  B.1 NativeAdapter ───────────────────────────────▶│                  │
  B.2 BaseAdapter (pattern) ───┐                    │   FASE D         │
  B.3 cargo  ──────────────────┤                    │   EXECUTOR       │
  B.4 go     ──────────────────┤                    │   D.1 Core       │
  B.5 pip    ──────────────────┼──── D.1 precisa ──▶│   D.2 Sync       │
  B.6 pipx   ──────────────────┤    de TODOS os     │   D.3 Testes     │
  B.7 uv     ──────────────────┤    adapters        │   D.4 Dry-Run    │
  B.8 aur    ──────────────────┘                   └────────┬─────────┘
                                                             │
FASE C (Adapters Complexos)                                   │
  C.1 GitAdapter ────────────────────────────────────────────▶│
  C.2 HTTPAdapter ───────────────────────────────────────────▶│
                                                             ▼
                                                    ┌──────────────────┐
                                                    │   FASE E         │
                                                    │   CLI            │
                                                    │   E.1 install    │
                                                    │   E.2 check      │
                                                    │   E.3 flags      │
                                                    └────────┬─────────┘
                                                             ▼
                                                    ┌──────────────────┐
                                                    │   FASE F         │
                                                    │   INTEGRATION    │
                                                    │   (Docker)       │
                                                    └──────────────────┘
```

### O que pode ser paralelo:
- **A.1 + A.2 + A.3**: independentes
- **B.1 + B.2**: independentes entre si
- **B.3 → B.8**: após B.2, todos independentes entre si
- **C.1 + C.2**: independentes entre si
- **F.1 + F.2**: após A-E, roteamento de containers em paralelo

### O que é sequencial:
- **B depende de A** (precisa da interface Adapter)
- **C depende de A** (precisa da interface Adapter)
- **D depende de A + B + C** (precisa de todos os adapters registrados)
- **E depende de D** (CLI chama o executor)
- **F depende de E** (testes end-to-end usam o CLI)

---

## ⚡ Estimativa de Esforço

| Fase | Arquivos | Testes | Esforço | Status |
|------|----------|--------|---------|--------|
| A.1 Interface + Tipos | 3 | 2 | ⭐ | ✅ |
| A.2 Graph | 1 | 1 | ⭐⭐ | ✅ |
| A.3 MockAdapter | 1 | — | ⭐ | ✅ |
| B.1 NativeAdapter | 1 | 1 | ⭐⭐ | ✅ |
| B.2 BaseAdapter + Configs map | 2 | 1 | ⭐⭐ | ✅ |
| B.3-B.8 (6 adapters originais) | — | — | ⭐⭐⭐ | ✅ + ⚡ expandido |
| B.8-B.22 (10 BaseAdapter + 4 especializados + AUR aliases) | 11 | 16 | ⭐⭐⭐⭐ | ✅ feito |
| Bug fix `findClanByManager` + Code Review | 3 | 4 (novos) | ⭐⭐ | ✅ |
| Expansão native (winget, mint, opkg) | 1 | 3 (novos) | ⭐ | ✅ |
| C.1 GitAdapter | 1 | 8 | ⭐⭐ | ✅ |
| C.2 HTTPAdapter | 4 | 13 | ⭐⭐⭐⭐ | ✅ |
| D.1-D.3 Executor | 2 | 9 | ⭐⭐⭐ | ✅ |
| E.1-E.3 CLI | 1 | — | ⭐⭐ | ✅ |
| F.1-F.2 Integração | 5 | — | ⭐⭐⭐ | ✅ |
| **Extra:** Expansão de escopo | +8 lang adapters + 3 native + code review | +8 testes | ⭐⭐⭐ | ✅ |

> ⭐ = rápido (<30min), ⭐⭐ = médio (30-60min), ⭐⭐⭐ = complexo (1-2h), ⭐⭐⭐⭐ = muito complexo (2-4h)
>
> **Total realizado**: ~16-24h de implementação + ~8-12h de testes = ~24-36h (inclui expansão de escopo)

---

# ✅ CAMPANHA 2: Logging Estruturado (P1)

> **Pode começar em paralelo com Campanha 1** — não bloqueia, mas acelera debug.

## 2.1 Infraestrutura (`pkg/log`)

- [x] **Adotar `log/slog`** (stdlib, zero dependências)
- [x] **Criar `pkg/log/logger.go`** com níveis ERROR, WARN, INFO, DEBUG
- [x] **Contexto semântico** em todo log via `LogContext` struct:
  ```go
  type LogContext struct {
      TraceID string // propagado via DEPENGINE_TRACE_ID
      Tool    string
      Method  string
      Distro  string
      Family  string
      Phase   string // "parse" | "resolve" | "install" | "postinstall"
  }
  ```
- [x] **`pkg/log/testlog.go`** — helper `TestCapture` com `AssertContains`, `AssertNotContains`, `Lines`, `Reset`
- [x] **`pkg/log/logger_test.go`** — 11 testes do logger (níveis, contexto, attrs vazios omitidos, reset)

## 2.2 Instrumentação do Motor

- [x] `main.go`: logger com trace_id, logs INFO de init/resumo, flag `--log-level`
- [x] `facts.go`: log DEBUG com campos do Facts ao gather
- [x] `resolve.go`: log DEBUG do clan resolvido (direct match, id_like, unknown)
- [x] `executor.go`: log DEBUG/INFO/WARN em cada passo (init, sync, graph, tool install, fallback, postinstall) via `WithLogger(*slog.Logger)`
- [x] `run.LoggingRunner`: wrapper decorator que loga todo comando executado (args, exit code, duração, erro)

## 2.3 Modo Diagnóstico (`--diagnose`)

- [x] Ativa DEBUG + dry-run + verbose implícito
- [x] Loga: Facts, Schema normalizado, ordem de métodos
- [x] Flags: `--diagnose` e `--log-level` (debug, info, warn, error)
- [x] `ExecReport` logado como JSON no final (resumo completo)

---

# 📐 CAMPANHA 3: Validação de Schema (P1)

> **Pré-requisito para release estável.** Depende de Campanha 1 completa.

## 3.1 Validação Estrutural (`pkg/validate/structural.go`)

- [ ] Validar tipos: `defaults.manager` string, `method_order` []string
- [ ] Validar whitelist de métodos
- [ ] Validar campos obrigatórios por método: `git.url`, `http.url`
- [ ] Validar `when`: chaves permitidas
- [ ] Validar duplicidade `simple` vs `[tools.X]`
- [ ] Validar placeholders conhecidos (flagiar `{archh}`)
- [ ] Testdata com TOMLs válidos e inválidos (8+ casos)

## 3.2 Validação Semântica (`pkg/validate/semantic.go`)

- [ ] Ciclos em `requires` (compartilhar com `pkg/graph`)
- [ ] Referências dangling: tool em `requires` que não existe
- [ ] Método sem candidato viável
- [ ] URL malformada
- [ ] `when.distro_family` com clan não-reconhecido

## 3.3 Validação de Ambiente (`depengine validate --check-env`)

- [ ] Verificar managers nativos no PATH
- [ ] Verificar adapters de linguagem no PATH
- [ ] Verificar `git`, `curl`/`wget` se schema os usa
- [ ] Tudo warning (schema válido em outra máquina)

## 3.4 CLI de Validação

- [ ] `depengine validate [schema.toml]` — saída estilo compilador
- [ ] `--format=json` — saída estruturada
- [ ] `--strict` — warnings viram erros

---

# 🚀 CAMPANHA 4: Polimento & Release (P2)

- [ ] **CI/CD**: GitHub Actions compila para linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- [ ] **Autocomplete** bash/zsh/fish para comandos e flags
- [ ] **Man page / `--help`** detalhado
- [ ] **Cheatsheet** para criar schema.toml
- [ ] **Testes cross-plataforma** em containers Docker
- [ ] **Benchmarks**: parse de schema.toml 200 linhas < 100ms
- [ ] **Integração com dotfiles**: `depengine install ~/.dotfiles/motor/schema.toml`

---

# 🗺️ Roadmap Visual

```
CAMPANHA 0 (✅ CONCLUÍDA)
  └── Fundação: run + engine + native + schema/parser + schema/placeholders

CAMPANHA 1 (🟢 IMPLEMENTADA — P0) ⚡ ESCOPO EXPANDIDO
  ├── ✅ Fase A: Fundação (interface + graph + mocks)
  ├── ✅ Fase B: Adapters Core
  │   ├── Native (15 managers: 12 originais + winget, mint, opkg)
  │   ├── Language (17 adapter kinds: cargo, go, pip, pipx, uv, npm, gem, yarn,
  │   │   yarn-berry, composer, apm, vscode, flatpak, snap, cask, mas, sdkman,
  │   │   steamcmd, pacstall) + AUR (aur + paru + yay)
  │   └── Bug fix: findClanByManager com managerNameToClan reverse map
  ├── ✅ Fase C: Adapters Complexos (Git + HTTP)
  ├── ✅ Fase D: Executor Central (core + sync + fallback + dry-run)
  ├── ✅ Fase E: CLI (install, check, dry-run, verbose, json)
  ├── ✅ Fase F: Testes de integração (Docker)
  └── ✅ Code review: 5 findings aplicados e testados (estréia: adapters_review_test.go)

CAMPANHA 2 (✅ IMPLEMENTADA — P1)
  ├── ✅ 2.1 Infraestrutura pkg/log (slog + LogContext + testlog)
  ├── ✅ 2.2 Instrumentação do motor (main, facts, resolve, executor)
  └── ✅ 2.3 Modo --diagnose + --log-level

CAMPANHA 3 (🟡 DEPOIS — P1)
  ├── 3.1 Validação estrutural
  ├── 3.2 Validação semântica
  ├── 3.3 Validação de ambiente (--check-env)
  └── 3.4 CLI validate + testes

## Features implementadas desde a última atualização:
  
- [x] **`--sort-by`** (name/status/method) no CLI `install` — `pkg/exec.SortField` + `ExecReport.SortBy()`
- [x] **ToolResult.Duration** — duração individual por tool no executor
- [x] **NativeByManagerAdapter** — aliases de managers nativos (apt, pacman, dnf, brew…) como métodos no schema
- [x] **Synced output API** — `WithOutput(io.Writer)` + `WithLogger(*slog.Logger)` no executor
- [x] **pkg_overrides** — suporte a package name por clan no NativeAdapter

> **Última atualização:** 2026-07-09 (adapter logging gaps L1, L4, L5)
---

# 📊 Status Atual Detalhado

| Componente | Status | Testes |
|-----------|--------|--------|
| `pkg/run` — subprocess seam | ✅ | 8 testes (4 runner + 4 logging) |
| `pkg/engine` — facts + resolve | ✅ | 7 testes |
| `pkg/native` — native managers (15 distros) | ✅ | 5 testes |
| `pkg/schema` — parser TOML | ✅ | 12 testes |
| `pkg/schema` — placeholders | ✅ | 9 testes |
| `pkg/exec` — adapter interface + registry | ✅ | 3 testes |
| `pkg/exec` — executor central | ✅ | 17 testes |
| `pkg/exec` — native adapter (auto-detect + `findClanByManager`) | ✅ | 5 testes |
| `pkg/graph` — dependency resolver | ✅ | 7 testes |
| `pkg/lang` — 17 adapter kinds | ✅ | 16 testes (12 base + 4 review) |
| `pkg/git` — git adapter | ✅ | 8 testes |
| `pkg/httpdownload` — http adapter | ✅ | 13 testes |
| `pkg/log` — structured logging | ✅ | 11 testes |
| `pkg/validate` — schema validation | ❌ | — |
| CLI `install`/`check`/`version` (com --dry-run, --verbose, --json, --diagnose, --log-level, --sort-by, --only, --skip) | ✅ | — |
| CLI `validate` | ❌ | — |


---

# 🔍 Análise de Lacunas de Debug (pós-Campanha 2)

> Revisão crítica do sistema de logging: o que ajuda a descobrir bugs vs o que ainda é opaco.

## ✅ Implementado (cobre 70% dos cenários de debug)

| Cenário | Como debuggar |
|---------|---------------|
| "Qual distro foi detectada?" | `--diagnose` → facts DEBUG |
| "Qual clan foi resolvido?" | `--diagnose` → resolve DEBUG |
| "Quais tools estão no schema?" | `--diagnose` → schema DEBUG |
| "Qual método está sendo tentado?" | `executor --log-level=debug` → cada método tool=X method=Y |
| "Método foi pulado por when?" | `executor DEBUG` → skip_when + distro_family requerida |
| "Adapter não está disponível?" | `executor DEBUG` → skip_no_adapter / skip_unavailable |
| "Tool já estava instalada?" | `executor DEBUG` → already_installed |
| "Comando exato executado?" | `LoggingRunner` (run/logging.go) → DEBUG com cmd + args |
| "Quanto tempo cada subprocesso levou?" | `LoggingRunner` DEBUG → duration |
| "Qual foi o exit code?" | `LoggingRunner` DEBUG → exit_code + stderr |
| "Resumo final?" | `executor INFO` → success/failed/skipped/already/duration |
| "Modo JSON programático?" | `--json` + `DEPENGINE_LOG_JSON=1` |

## ⚠️ Lacunas identificadas (não implementadas — candidatas a refinamento)

| # | Lacuna | Impacto | Solução proposta |
|---|--------|---------|------------------|
| L1 | **Adapter log context correlation** | ✅ **RESOLVIDO** — `LoggingRunner` agora aceita `Context{Tool, Method}` via `WithContext()`. O executor wraps o runner com tool/method antes de chamar `adapter.Install()`, correlacionando todos os logs de subprocesso com o adapter que os originou. | `run.LoggingRunner.WithContext()` + `executor.go` runner wrapping |
| L2 | **Schema parse details não logados** | Baixo — qual `method_order` foi usado, quais ferramentas tiveram `when` collapsing, quantos métodos por tool. Útil para validar que o parse do TOML está correto | `ParseSchema` aceitar logger opcional e logar DEBUG da struct normalizada |
| L3 | **Per-tool duration ausente** | ✅ **RESOLVIDO** — `ToolResult.Duration` populado no executor desde julho/2026 | — |
| L4 | **Falha de método sem detalhes** | ✅ **RESOLVIDO** — `CargoAdapter` git mode agora inclui stderr na mensagem de erro (`cargo --git: install exited 1: E: Unable to locate package`). `BaseAdapter` e `LoggingRunner` já incluíam. | `cargo.go` linha 36-37 |
| L5 | **Graph/dependências não logado** | ✅ **RESOLVIDO** — `graph.Sort()` ganhou `WithLogger(*slog.Logger)` via functional option. Cada nível do Kahn's algorithm loga DEBUG com as tools daquele nível. | `sort.go` + `WithLogger` + `executor.go` passando `graph.WithLogger(ex.logger)` |
| L6 | **Depengine check sem logging** | Baixo — `depengine check git` só printa "✓" ou "✗", sem structured log | Adicionar log INFO no check |
| L7 | **ENV vars de configuração** | Baixo — `DEPENGINE_NOSUDO`, `DEPENGINE_DETECT_SCRIPT`, `DEPENGINE_TRACE_ID` não são logadas. Se algo falha por env errado, não tem trail | Log DEBUG das env vars ativas no init do executor |

## Prioridade de implementação (atualizada — julho/2026 após refinamentos)

```
CAMPANHA 3:  Schema validation (P1) — estrutural + semântica + CLI
MÉDIA (debug): L2 (schema parse logging), L7 (env vars logging)
BAIXA (debug):  L6 (depengine check logging)
CAMPANHA 4:  CI/CD, autocomplete, man page, benchmarks (P2)
```

---

# 📝 Notas de Implementação

- **Logging**: `pkg/log` com slog (stdlib) implementado na Campanha 2. `LogWriter`/`logf()` em `pkg/exec` mantido para compatibilidade de testes. `run.LoggingRunner` decorator permite tracing de todo subprocesso. Para JSON: `DEPENGINE_LOG_JSON=1`.
- **Adicionar um adapter novo** = implementar a interface `Adapter` + chamar `Register()` num `init()`. O executor é genérico.
- **Placeholders**: a substituição já acontece no `ParseSchema()` via `BuildMap(facts, clan)`. Adapters recebem strings já expandidas e só precisam lidar com `{pkg}` (substituído pelo adapter) e `{latest}` (resolvido pelo adapter HTTP/Git).
- **Extensibilidade**: o executor não precisa saber detalhes de cada método — só chama `adapter.Available()`, `adapter.Check()`, `adapter.Install()`.
- **Separação clara**: `pkg/native` é sobre **gerenciadores de pacote do sistema**. `pkg/lang` é sobre **gerenciadores de ecossistema de linguagem**. Não se misturam.
- **Registry global**: adapters se registram com `exec.Register()` em `init()`. O executor é desacoplado dos pacotes de adapter.
- **Native auto-detect**: `NativeAdapter` detecta o clan dinamicamente via `which {manager}` — sem necessidade de configurar clan manualmente.
- **Manager aliases**: adapters para `apt`, `pacman`, `dnf`, `brew`, etc. são registrados automaticamente por `RegisterNativeManagerAliases()`.

---

_Última atualização: 2026-07-09. Campanhas 0–2 ✅. Lacunas L1/L4/L5 ✅. Próximo passo: **Campanha 3 — Schema Validation** ou refinamentos L2/L6/L7._
