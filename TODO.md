> **Projeto:** depengine  
> **Data:** 2026-07-10  
> **Status dos testes:** ✅ `go test ./...` — 0 falhas (13/13 packages pass, 353 testes)  
> **Build:** ✅ `go build -o depengine .` — limpo  
> **Vet:** ✅ `go vet ./...` — limpo  
> **Próximo release:** `v0.2.0` — Campanha 5 (Polimento & Release)

Depois de quatro campanhas implementadas (Fundação, Executor+Adapters, Logging, Schema Validation) e a **expansão pós-Campanha-4** (lifecycle state tracking + Remover interface + CLI status/remove), o motor está completo e funcional com **~23 métodos de instalação** e **353 testes automatizados**.

## 🔧 Architecture Refactor — 5 Candidates (CONCLUÍDO)

**pkg/validate** refatorado para eliminar fontes de verdade duplicadas e
centralizar padrões comuns. Mudanças quebram zero testes:

1. **Placeholders unificados**: `KnownPlaceholders()` deriva de `BuildMap` —
   `pkg/validate` não mantém mais cópia manual
2. **Distro families unificadas**: `knownDistroFamilies` constrói-se de
   `native.AllClans()` + "unknown" — sem lista estática duplicada
3. **Helpers realocados**: `fieldPath` e `truncateStr` movidos para
   `validate.go` (mesmo pacote, melhor coesão arquivo-responsabilidade)
4. **`runValidate` extraída**: `case "validate":` 90 linhas → função
   nomeada em `main.go`, padronizando com as demais helpers
5. **`run.LookPath` centralizado**: substitui 3 clones de `which {binary}`
   em `pkg/lang/base.go`, `pkg/validate/envcheck.go` e
   `pkg/exec/native_adapter.go`

# ✅ CAMPANHA 0: Fundação (CONCLUÍDA)

**7 pacotes implementados:**

| Pacote | O que faz |
|--------|-----------|
| `pkg/run` | Runner interface, OSExecRunner, FakeRunner, trace ID propagation |
| `pkg/engine` | Facts (1:1 do detect_os.sh), GatherFacts, ResolveFamily (12 clans) |
| `pkg/native` | Registry declarativo de **15 distros**, Manager lookup, BuildCmd |
| `pkg/schema` (parser) | ParseSchema — 3 formas de declaração, method_order, when |
| `pkg/schema` (placeholders) | Expand/BuildMap — substitui 10 placeholders em todo schema |
| `schema.toml` | Template de referência com 8 casos de declaração |
| `main.go` | CLI demonstrativa: fatos, resolução, comandos nativos |

---

# ✅ CAMPANHA 1: Executor + Adapters (P0 — CONCLUÍDA) ⚡ EXPANSÃO

Todas as 6 fases implementadas e testadas (91+ testes, 10/10 pacotes). Escopo **expandido**:
dos 6 adapters de linguagem planejados para **~19 métodos de instalação** adicionais:
10 BaseAdapter (npm, gem, yarn, composer, apm, flatpak, snap, vscode, cask, mas) +
4 especializados (sdkman, steamcmd, yarn-berry, pacstall) + 3 managers nativos
(winget, mint, opkg) + aliases AUR (paru, yay). Bug fix: `findClanByManager` (mapa reverso).

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
- **Logging** estruturado via `log/slog` com `--log-level` e `--diagnose`

---

## FASE A: Fundação — ✅ interface + graph + mocks

- `pkg/exec/adapter.go` — interface Adapter (Kind/Available/Check/Install)
- `pkg/exec/types.go` — StatusEnum, ToolResult, MethodAttempt, ExecReport
- `pkg/exec/report.go` — Summary/Detail/JSON formatadores
- `pkg/exec/registry.go` — Register/Lookup/RegisteredKinds (global + per-instance)
- `pkg/graph/sort.go` — topo-sort com Kahn + CycleError (7 testes)
- `pkg/exectest/adapter.go` — MockAdapter para testes

## FASE B: Adapters Core — ✅ 17 adapters + 15 managers nativos

- **NativeAdapter**: auto-detect de clan + wrapper pkg/native (NeedsSync, sudo, pkg_overrides)
- **BaseAdapter** (genérico): 10 kinds (npm, gem, yarn, composer, apm, flatpak, snap, vscode, cask, mas)
- **CargoAdapter**: suporte a `--git` via mc.Config["git"] + stderr em erros
- **AURAdapter**: helper configurável + aliases paru/yay
- **Especializados**: SDKManAdapter, SteamCMDAdapter, YarnBerryAdapter, PacstallAdapter
- **Expansão native**: winget, mint, opkg + bug fix `findClanByManager`
- **Code review**: 5 findings aplicados e testados
- 16 testes em pkg/lang, 5 em pkg/exec/native, 3 em pkg/native

## FASE C: Adapters Complexos — ✅ Git + HTTP

- **GitAdapter**: clone shallow + build + extract_to + {latest} (8 testes)
- **HTTPAdapter**: download (Go > curl > wget) + extração (tar/zip/deb/binário) + checksum sha256 + {latest} resolver GitHub API (13 testes)

## FASE D: Executor Central — ✅ orquestrador completo

- `pkg/exec/executor.go` — loop principal (graph → methods → fallback → postinstall)
- `pkg/exec/sync.go` — SyncManager (NeedsSync, uma vez por execução)
- `pkg/exec/executor_test.go` — 9+ cenários (fallback, when, dry-run, timeout, order topológica)
- Opções: WithRunner/WithLogger/WithAdapters/WithDryRun/WithSortBy/WithOutput
- Timeout por tool e método, context cancellation

## FASE E: CLI — ✅ install / check / version

- `depengine install [--dry-run] [--verbose] [--json] [--log-level] [--diagnose] [--sort-by] [--only/--skip]`
- `depengine check <tool>` — verifica instalação de ferramenta
- Exit codes: 0 (ok), 1 (falha), 2 (schema error), 3 (runtime error)

## FASE F: Testes de Integração — ✅ Docker (4 distros)

- `tests/integration/` com docker-compose + Debian/Arch/Fedora/Alpine
- Schema de teste com 15 tools (native, cargo, go, git, http, when, requires, postinstall)
- 10 cenários validados (dry-run, fallback, when, requires, JSON, check)

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
  B.3-B.22 17 adapters ────────┤                    │   EXECUTOR       │
                              │                    │   D.1 Core       │
                              ├──── D.1 precisa ──▶│   D.2 Sync       │
                              │    de TODOS os     │   D.3 Testes     │
                              │    adapters        │   D.4 Dry-Run    │
                              └─────┬─────────────┘   ────┬──────────┘
                                    │                     │
FASE C (Adapters Complexos)         │                     │
  C.1 GitAdapter ─────────────────────────────────────────▶│
  C.2 HTTPAdapter ────────────────────────────────────────▶│
                                                           ▼
                                                  ┌──────────────────┐
                                                  │   FASE E         │
                                                  │   CLI            │
                                                  │   install/check  │
                                                  └────────┬─────────┘
                                                           ▼
                                                  ┌──────────────────┐
                                                  │   FASE F         │
                                                  │   INTEGRAÇÃO     │
                                                  │   (Docker)       │
                                                  └──────────────────┘
```

### Paralelizável:
- **A.1 + A.2 + A.3**: independentes
- **B.1 + B.2**: independentes entre si
- **B.3 → B.22**: após B.2, independentes entre si
- **C.1 + C.2**: independentes entre si
- **F.1 + F.2**: após A-E, roteamento de containers em paralelo

### Sequencial:
- B depende de A, C depende de A, D depende de A+B+C, E depende de D, F depende de E

---

## ⚡ Estimativa de Esforço

| Fase | Esforço | Status |
|------|---------|--------|
| A.1 Interface + Tipos | ⭐ | ✅ |
| A.2 Graph | ⭐⭐ | ✅ |
| A.3 MockAdapter | ⭐ | ✅ |
| B.1 NativeAdapter | ⭐⭐ | ✅ |
| B.2 BaseAdapter | ⭐⭐ | ✅ |
| B.3-B.8 (6 adapters originais) | ⭐⭐⭐ | ✅ expandido |
| B.8-B.22 (10 BaseAdapter + 4 especializados + AUR aliases) | ⭐⭐⭐⭐ | ✅ |
| Bug fix + Code Review | ⭐⭐ | ✅ |
| Expansão native (winget, mint, opkg) | ⭐ | ✅ |
| C.1 GitAdapter | ⭐⭐ | ✅ |
| C.2 HTTPAdapter | ⭐⭐⭐⭐ | ✅ |
| D.1-D.3 Executor | ⭐⭐⭐ | ✅ |
| E.1-E.3 CLI | ⭐⭐ | ✅ |
| F.1-F.2 Integração | ⭐⭐⭐ | ✅ |
| **Extra:** Expansão de escopo | ⭐⭐⭐ | ✅ |
| **Debug refinements L1/L4/L5** | ⭐⭐ | ✅ |

> ⭐ = rápido (<30min), ⭐⭐ = médio (30-60min), ⭐⭐⭐ = complexo (1-2h), ⭐⭐⭐⭐ = muito complexo (2-4h)
>
> **Total realizado**: ~24-36h (inclui expansão de escopo e debug refinements)

---

# ✅ CAMPANHA 2: Logging Estruturado (P1) — CONCLUÍDA

### 2.1 Infraestrutura (`pkg/log`)
Logger baseado em `log/slog` (stdlib) com níveis ERROR/WARN/INFO/DEBUG, `LogContext` (TraceID, Tool, Method, Distro, Family, Phase), helper de teste `TestCapture` com `AssertContains`/`AssertNotContains`.

### 2.2 Instrumentação do Motor
- `main.go`: logger com trace_id, `--log-level` flag
- `facts.go`: DEBUG com campos do Facts
- `resolve.go`: DEBUG do clan resolvido
- `executor.go`: DEBUG/INFO/WARN em cada passo do ciclo (init, sync, graph, tool install, fallback, postinstall)
- `run.LoggingRunner`: wrapper decorator que loga todo subprocesso (cmd, args, exit code, duração, stderr), com suporte a `WithContext(Tool, Method)` para correlação adapter ↔ subprocesso

### 2.3 Modo Diagnóstico (`--diagnose`)
Ativa DEBUG + dry-run + verbose implícito. Loga Facts, Schema normalizado, ordem de métodos. `ExecReport` como JSON no final.

---

## 3.1 Validação Estrutural (`pkg/validate/structural.go`)

- [x] Validar tipos: `defaults.manager` string, `method_order` []string
- [x] Validar whitelist de métodos
- [x] Validar campos obrigatórios por método: `git.url`, `http.url`
- [x] Validar `when`: chaves permitidas
- [x] Validar duplicidade `simple` vs `[tools.X]`
- [x] Validar placeholders conhecidos (flagiar `{archh}`)
- [x] Testdata com TOMLs válidos e inválidos (8+ casos)

## 3.2 Validação Semântica (`pkg/validate/semantic.go`)

- [x] Ciclos em `requires` (compartilhar com `pkg/graph`)
- [x] Referências dangling: tool em `requires` que não existe
- [x] Método sem candidato viável
- [x] URL malformada
- [x] `when.distro_family` com clan não-reconhecido

## 3.3 Validação de Ambiente (`depengine validate --check-env`)

- [x] Verificar managers nativos no PATH
- [x] Verificar `git`, `curl`/`wget` se schema os usa
- [x] Tudo warning (schema válido em outra máquina)

## 3.4 CLI de Validação

- [x] `depengine validate [schema.toml]` — saída estilo compilador
- [x] `--format=json` — saída estruturada
- [x] `--strict` — warnings viram erros
---

# 🚀 CAMPANHA 5: Polimento & Release (P2)

- [ ] **CI/CD**: GitHub Actions compila para linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- [ ] **Autocomplete** bash/zsh/fish para comandos e flags
- [ ] **Man page / `--help`** detalhado
- [ ] **Cheatsheet** para criar schema.toml
- [ ] **Testes cross-plataforma** em containers Docker
- [ ] **Benchmarks**: parse de schema.toml 200 linhas < 100ms
- [ ] **Integração com dotfiles**: `depengine install ~/.dotfiles/motor/schema.toml`

---

# 📋 Análise de Maturidade — Campanha 5 (Polimento & Release)

**Status:** ✅ Projeto maduro para Campanha 5. Ciclo de vida core completo.

## Itens entregues que fecham o ciclo

| Domínio | O que foi implementado |
|---------|----------------------|
| **Instalação** | Executor com fallback, when, requires, postinstall, timeout, dry-run |
| **Check** | `depengine check <tool>` — verifica se ferramenta está instalada |
| **Status** | `depengine status` — compara schema vs state file |
| **Remoção** | `depengine remove <tool>` — via Remover interface |
| **Validação** | `depengine validate` — estrutural + semântica + ambiente |
| **Logging** | slog estruturado, --log-level, --diagnose, LoggingRunner |
| **State tracking** | XDG state file + flock + definition hash |
| **Adaptadores** | ~23 kinds (native, cargo, go, pip, npm, git, http, …) |
| **Testes** | 353 testes, 13/13 packages, 29 integration tests (Docker) |

## O que falta para v0.2.0 (Campanha 5)

| Item | Prioridade | Esforço estimado |
|------|-----------|-----------------|
| **CI/CD** — GitHub Actions (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64) | P2 | ⭐⭐ |
| **Autocomplete** — bash/zsh/fish para comandos e flags | P2 | ⭐⭐ |
| **Man page / `--help` detalhado** | P2 | ⭐⭐ |
| **Cheatsheet** para schema.toml | P2 | ⭐ |
| **Benchmarks** — parse 200 linhas < 100ms | P2 | ⭐ |
| **Cross-platform tests** em containers Docker | P2 | ⭐⭐ |
| **Dotfiles integration** — `depengine install ~/.dotfiles/schema.toml` | P2 | ⭐ |

> **Veredito:** O motor está maduro. As features restantes são **polimento e release engineering**, não funcionalidade core. Pode-se começar a Campanha 5 imediatamente — o trabalho é paralelizável (CI/CD, autocomplete, docs podem rodar em paralelo).

---

# 🗺️ Roadmap Visual

```
CAMPANHA 0 (✅ CONCLUÍDA)
  └── Fundação: run + engine + native + schema/parser + schema/placeholders

CAMPANHA 1 (🟢 IMPLEMENTADA — P0) ⚡ ESCOPO EXPANDIDO
  ├── ✅ Fase A: Fundação (interface + graph + mocks)
  ├── ✅ Fase B: Adapters Core (17 adapter kinds + 15 native managers)
  ├── ✅ Fase C: Adapters Complexos (Git + HTTP)
  ├── ✅ Fase D: Executor Central (core + sync + fallback + dry-run)
  ├── ✅ Fase E: CLI (install, check, dry-run, verbose, json)
  ├── ✅ Fase F: Testes de integração (Docker)
  └── ✅ Code review + debug refinements L1/L4/L5

CAMPANHA 2 (✅ IMPLEMENTADA — P1)
  ├── ✅ 2.1 Infraestrutura pkg/log (slog + LogContext + testlog)
  ├── ✅ 2.2 Instrumentação do motor (main, facts, resolve, executor)
  └── ✅ 2.3 Modo --diagnose + --log-level + adapter context correlation

CAMPANHA 3 (✅ IMPLEMENTADA — P1)
  ├── ✅ 3.1 Validação estrutural (pkg/validate)
  ├── ✅ 3.2 Validação semântica (pkg/validate)
  ├── ✅ 3.3 Validação de ambiente (--check-env)
  └── ✅ 3.4 CLI validate + testes

CAMPANHA 4 (✅ IMPLEMENTADA — P0)
  ├── ✅ pnpm — BaseAdapter (npm-like)
  ├── ✅ dnf5 — alias native fedora
  ├── ✅ conda — adapter específico
  ├── ✅ asdf/mise — universal version manager adapter
  ├── ✅ vscodium — codium binary (vscode-like)
  └── ✅ bun — fast JS runtime/package manager
```

PÓS-CAMPANHA 4: Lifecycle State Tracking (✅ IMPLEMENTADA)
  ├── ✅ `pkg/state` — state file XDG + flock locking + definition hash
  ├── ✅ `exec.Remover` — interface opcional (Remove + CanRemove)
  ├── ✅ `depengine status` — compara tools do schema contra state file
  ├── ✅ `depengine remove <tool>` — desinstala via adapter
  ├── ✅ NativeByManagerAdapter + BaseAdapter com suporte Remover
  └── ✅ CLI refactoring: runInstall/runCheck/runStatus/runRemove extraídos

## Features implementadas desde a última atualização:

- [x] **Lifecycle State Tracking** — `pkg/state` com XDG state file, flock locking, hash de definição
- [x] **Remover interface** — `exec.Remover` (opcional) com `Remove()` + `CanRemove()`
- [x] **`depengine status`** — exibe status de instalação de todas as tools do schema
- [x] **`depengine remove <tool>`** — desinstala ferramenta via adapter que a instalou
- [x] **NativeByManagerAdapter suporta Remover** — delega para `apt remove`, `pacman -R`, etc.
- [x] **Native adapter simplificado** — lógica de install/check separada, stderr em erros
- [x] **CLI refactoring** — `runInstall`, `runCheck`, `runStatus`, `runRemove` extraídos de `main()`
- [x] **Log helpers consolidados** — método `log()` único no executor
- [x] **Design doc** — `docs/plans/designs/lifecycle-state-tracking.md`
> **Última atualização:** 2026-07-10. Campanhas 0–4 ✅ + Lifecycle State Tracking ✅. Testes: 13/13 packages, 353 tests, pass.

---

# 📊 Status Atual Detalhado


| Componente | Status | Testes |
|-|--------|--------|
| `pkg/run` — subprocess seam | ✅ | 8 testes |
| `pkg/engine` — facts + resolve | ✅ | 25 testes |
| `pkg/native` — native managers (15 distros) | ✅ | 37 testes |
| `pkg/schema` — parser TOML | ✅ | 12 testes |
| `pkg/schema` — placeholders | ✅ | 9 testes |
| `pkg/exec` — adapter interface + registry + Remover | ✅ | 5 testes |
| `pkg/exec` — executor central | ✅ | 25 testes |
| `pkg/exec` — native adapter | ✅ | 14 testes |
| `pkg/exec` — NativeByManager adapter | ✅ | 10 testes |
| `pkg/graph` — dependency resolver | ✅ | 7 testes |
| `pkg/lang` — 23 adapter kinds | ✅ | 40 testes |
| `pkg/git` — git adapter | ✅ | 8 testes |
| `pkg/httpdownload` — http adapter | ✅ | 21 testes |
| `pkg/log` — structured logging | ✅ | 22 testes |
| `pkg/state` — lifecycle state tracking | ✅ | 9 testes |
| `pkg/validate` — schema validation | ✅ | 72 testes |
| CLI `install`/`check`/`version`/`status`/`remove` | ✅ | — |
| CLI `validate` (--check-env, --format=json, --strict) | ✅ | 29 integration tests |
| **Total** | **13 packages** | **353 testes** |

---


## ✅ Implementado (cobre 80% dos cenários de debug)

| Cenário | Como debuggar |
|---------|---------------|
| "Qual distro foi detectada?" | `--diagnose` → facts DEBUG |
| "Qual clan foi resolvido?" | `--diagnose` → resolve DEBUG |
| "Quais tools estão no schema?" | `--diagnose` → schema DEBUG |
| "Qual método está sendo tentado?" | `executor --log-level=debug` → cada método tool=X method=Y |
| "Método foi pulado por when?" | `executor DEBUG` → skip_when + distro_family requerida |
| "Adapter não está disponível?" | `executor DEBUG` → skip_no_adapter / skip_unavailable |
| "Tool já estava instalada?" | `executor DEBUG` → already_installed |
| "Comando exato executado?" | `LoggingRunner` → DEBUG com cmd + args + tool/method |
| "Quanto tempo cada subprocesso levou?" | `LoggingRunner` DEBUG → duration |
| "Qual foi o exit code + stderr?" | `LoggingRunner` DEBUG/WARN → exit_code + stderr |
| "Qual adapter/disparou o comando?" | `LoggingRunner` DEBUG → tool=X method=Y (correlação L1) |
| "Qual a ordem topológica?" | `graph.Sort` DEBUG → cada nível com tools (L5) |
| "Resumo final?" | `executor INFO` → success/failed/skipped/already/duration |
| "Modo JSON programático?" | `--json` + `DEPENGINE_LOG_JSON=1` |

## ⚠️ Lacunas restantes (candidatas a refinamento futuro)

| # | Lacuna | Impacto | Status |
|---|--------|---------|--------|
| L1 | **Adapter log context** | Médio | ✅ implementado via `LoggingRunner.WithContext()` |
| L2 | **Schema parse details não logados** | Baixo | 🔄 pendente |
| L3 | **Per-tool duration ausente** | Baixo | ✅ resolvido |
| L4 | **stderr em erros de adapter** | Médio | ✅ implementado (CargoAdapter git + BaseAdapter) |
| L5 | **Graph sort logging** | Baixo | ✅ implementado via `WithLogger` |
| L6 | **Depengine check sem logging** | Baixo | 🔄 pendente |
| L7 | **ENV vars de configuração** | Baixo | 🔄 pendente |

## Prioridade de implementação (atualizada — julho/2026)

```
CAMPANHA 3:  Schema validation (P1) — estrutural + semântica + CLI                          ✅
CAMPANHA 4:  Novos adaptadores (P0)                                                         ✅
  ▸ pnpm, dnf5, conda, asdf/mise, vscodium, bun
PÓS-CAMPANHA 4: Lifecycle State Tracking (P0) — pkg/state + Remover + status/remove CLI      ✅
══════════════════════════════════════════════════════════════════════════════════════════════
BAIXA (debug): L2 (schema parse logging), L6 (depengine check logging), L7 (env vars)
══════════════════════════════════════════════════════════════════════════════════════════════
NEXT: Campanha 5 — Polimento & Release (P2)
```
---

# 📝 Notas de Implementação

- **Logging**: `pkg/log` com slog (stdlib) implementado na Campanha 2. `run.LoggingRunner` decorator permite tracing de todo subprocesso com contexto de tool/method. Para JSON: `DEPENGINE_LOG_JSON=1`.
- **Adicionar um adapter novo** = implementar a interface `Adapter` + chamar `Register()` num `init()`. O executor é genérico.
- **Placeholders**: a substituição já acontece no `ParseSchema()` via `BuildMap(facts, clan)`. Adapters recebem strings já expandidas e só precisam lidar com `{pkg}` (substituído pelo adapter) e `{latest}` (resolvido pelo adapter HTTP/Git).
- **Extensibilidade**: o executor não precisa saber detalhes de cada método — só chama `adapter.Available()`, `adapter.Check()`, `adapter.Install()`.
- **Separação clara**: `pkg/native` é sobre **gerenciadores de pacote do sistema**. `pkg/lang` é sobre **gerenciadores de ecossistema de linguagem**. Não se misturam.
- **Registry global**: adapters se registram com `exec.Register()` em `init()`. O executor é desacoplado dos pacotes de adapter.
- **Native auto-detect**: `NativeAdapter` detecta o clan dinamicamente via `which {manager}` — sem necessidade de configurar clan manualmente.
- **Manager aliases**: adapters para `apt`, `pacman`, `dnf`, `brew`, etc. são registrados automaticamente por `RegisterNativeManagerAliases()`.
- **State tracking**: `pkg/state` persiste em `~/.local/state/depengine/state.json` (XDG). Lock via flock. Hash SHA256 da definição da tool para detectar mudanças no schema entre instalações.
- **Remover interface**: `exec.Remover` é opcional — adapters que não implementam `Remove()` geram instrução de remoção manual. `NativeByManagerAdapter` e `BaseAdapter` suportam remoção nativa.

---
_Última atualização: 2026-07-10. Campanhas 0–4 ✅ + Lifecycle State Tracking ✅. 13/13 packages, 353 testes, 0 falhas. Próximo release: v0.2.0 — Campanha 5 (CI/CD, autocomplete, man page, benchmarks)._
