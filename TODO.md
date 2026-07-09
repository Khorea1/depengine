# TODO.md — depengine

> **Projeto:** depengine  
> **Data:** 2026-07-09  
> **Status dos testes:** ✅ `go test ./...` — 0 falhas (12/12 packages pass, ~90+ testes)  
> **Build:** ✅ `go build -o depengine .` — limpo  
> **Próximo release:** `v0.2.0` — schema validation + debug refinements + arch refactor

Depois de três campanhas implementadas (Fundação, Executor+Adapters, Logging), o motor está completo e funcional. A **Campanha 3 (Validação de Schema)** foi concluída — estruturais, semânticas e ambientais com CLI `validate`.

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

## Features implementadas desde a última atualização:

- [x] **`--sort-by`** (name/status/method) no CLI `install`
- [x] **ToolResult.Duration** — duração individual por tool
- [x] **NativeByManagerAdapter** — aliases de managers nativos como métodos
- [x] **Synced output API** — `WithOutput(io.Writer)` + `WithLogger(*slog.Logger)`
- [x] **pkg_overrides** — suporte a package name por clan no NativeAdapter
- [x] **L1: Adapter log context** — `LoggingRunner.WithContext()` correlaciona subprocessos com tool/method
- [x] **L4: stderr em erros** — `CargoAdapter` git mode inclui stderr na mensagem
- [x] **L5: Graph sort logging** — `graph.Sort` com `WithLogger` loga cada nível em DEBUG
- [x] **Schema validation** — `pkg/validate` com 50+ testes (unit + integration)
- [x] **`depengine validate`** — CLI com `--check-env`, `--format=json`, `--strict` (27 testes integração)
- [x] **Integration tests** — `tests/integration/validate_test.go` (build+exec real contra 20+ TOMLs)
> **Última atualização:** 2026-07-09. Campanha 3 ✅. Testes reais: ~90+ testes, 12/12 packages, integração CLI com TOMLs reais.

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
| `pkg/exec` — native adapter | ✅ | 5 testes |
| `pkg/graph` — dependency resolver | ✅ | 7 testes |
| `pkg/lang` — 17 adapter kinds | ✅ | 16 testes |
| `pkg/git` — git adapter | ✅ | 8 testes |
| `pkg/httpdownload` — http adapter | ✅ | 13 testes |
| `pkg/log` — structured logging | ✅ | 11 testes |

---

| `pkg/validate` — schema validation | ✅ | 50+ testes (unit + CLI integration) |
| CLI `install`/`check`/`version` (com --dry-run, --verbose, --json, --diagnose, --log-level, --sort-by, --only, --skip) | ✅ | — |
| CLI `validate` (--check-env, --format=json, --strict) | ✅ | 27 integration tests |

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
CAMPANHA 3:  Schema validation (P1) — estrutural + semântica + CLI
MÉDIA (debug): L2 (schema parse logging), L7 (env vars logging)
BAIXA (debug):  L6 (depengine check logging)
═══════════════════════════════════════════════════════════════════
🔴 PRIORIDADE MÁXIMA
CAMPANHA 4:  Novos adaptadores (P0)
  ▸ pnpm         — BaseAdapter, 1 entry no Configs map
  ▸ dnf5         — alias para fedora no managerNameToClan
  ▸ conda        — adapter específico (check/install via conda)
  ▸ asdf / mise  — universal version manager adapter
  ▸ vscodium     — cópia do vscode com binary "codium"
═══════════════════════════════════════════════════════════════════
CAMPANHA 5:  CI/CD, autocomplete, man page, benchmarks (P2)
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

---
_Última atualização: 2026-07-09. Campanhas 0–3 ✅. Refactor 5/5 ✅. Campanha 4 (novos adaptadores) 🔴 P0 pendente. Campanha 5 (CI/CD) P2 pendente. Próximo release: v0.2.0._
