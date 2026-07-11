> **Projeto:** depengine
> **Data:** 2026-07-11
> **Status dos testes:** ✅ `go test ./...` — 0 falhas (13/13 packages pass, 353+ testes)
> **Build:** ✅ `go build -o depengine .` — limpo
> **Vet:** ✅ `go vet ./...` — limpo
> **Próximo release:** `v0.2.0` — Campanha 5 (Polimento & Release) + Campanhas 6-7 (Auditoria & Higiene)

---

# ✅ Campanhas Concluídas (resumo)

| Campanha | Escopo | Status |
|----------|--------|--------|
| **0 — Fundação** | `pkg/run`, `pkg/engine`, `pkg/native` (15 distros), `pkg/schema` (parser + placeholders), `schema.toml` referência, CLI demonstrativa | ✅ |
| **1 — Executor + Adapters** | Interface Adapter, graph topo-sort, 17 adapters de linguagem (cargo, go, pip, pipx, uv, npm, pnpm, bun, gem, yarn, yarn-berry, composer, apm, flatpak, snap, vscode, vscodium, cask, mas) + 4 especializados (sdkman, steamcmd, pacstall, conda, asdf) + AUR (paru, yay) + 15 managers nativos + GitAdapter + HTTPAdapter + Executor central (fallback, when, requires, postinstall, timeout, dry-run) + CLI install/check/version + testes de integração Docker (4 distros) | ✅ |
| **2 — Logging** | `pkg/log` com slog, níveis ERROR/WARN/INFO/DEBUG, `LogContext` (TraceID, Tool, Method), `LoggingRunner`, modo --diagnose | ✅ |
| **3 — Schema Validation** | `pkg/validate`: validação estrutural + semântica + ambiente (`--check-env`), CLI `validate` com `--format=json` e `--strict` | ✅ |
| **4 — Novos Adaptadores** | pnpm, dnf5, conda, asdf/mise, vscodium, bun | ✅ |
| **Pós-4 — Lifecycle State** | `pkg/state` (XDG + flock + hash), `exec.Remover`, CLI `status`/`remove`/`forget` | ✅ |

**Total:** 13 packages, 353 testes, ~28 métodos de instalação.

---

# 🚀 CAMPANHA 5: Polimento & Release (P2)

- [ ] **CI/CD**: GitHub Actions compila para linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- [ ] **Autocomplete** bash/zsh/fish para comandos e flags
- [ ] **Man page / `--help`** detalhado
- [ ] **Cheatsheet** para criar schema.toml
- [ ] **Benchmarks**: parse de schema.toml 200 linhas < 100ms
- [ ] **Dotfiles integration**: `depengine install ~/.dotfiles/motor/schema.toml`

---

# 🚀 CAMPANHA 6: Correções & Auditoria (P0/P1)

### 6.1 — `filterTools` ignora fechamento transitivo de `requires` (P0)
✅ `filterTools` agora expande o fechamento transitivo de `Requires` quando `--only` é usado. BFS a partir da ferramenta alvo adiciona todas as dependências necessárias ao mapa filtrado, evitando o falso erro "requires X, which is not in schema".

### 6.2 — Nível de log padrão diverge da documentação (P1)
✅ `pkg/log.Default` alterado de `slog.LevelWarn` para `slog.LevelInfo`. Comentário atualizado. Logs informativos ("installing X", "tool Y check ok") agora aparecem por default.

### 6.3 — `DefinitionHash` não cobre Requires, PostInstall, When (P1)
✅ `DefinitionHash` agora inclui `Requires`, `PostInstall`, e `When.DistroFamily` de cada método. `methodConfig` indexado por kind + ordinal intra-kind em vez de chaveamento único por Kind, eliminando sobrescrita silenciosa de métodos duplicados.

### 6.4 — `runRemove --all` ignora falhas individuais (P1)
✅ `removeTool` agora retorna `bool`. Falhas em modo `--all` e modo single são rastreadas; `os.Exit(1)` é chamado se alguma remoção falhar.

### 6.5 — Lock exclusivo para operação read-only (P1)
✅ Adicionado `lockShared()` (LOCK_SH) em `pkg/state/lock.go` e `LoadShared()` em `pkg/state/state.go`. `runStatus` usa `LoadShared()` em vez de `LoadLocked()`, permitindo concorrência com instalações/remoções simultâneas.

### 6.6 — Revisão promessa de "binário estático único" (P2)
(pendente — requer `go:embed` de `detect_os.sh` ou port para Go nativo)

---

# 🚀 CAMPANHA 7: Higiene Técnica & Dívida (P1/P2)

### 7.1 — Comentários corrompidos e desatualizados
- `pkg/native/registry.go:10`: `"whichugh"` — artefato de edição, deve ser `"which"` ou similar
- `pkg/native/registry.go:15-18`: comenta que `"windows"` é "placeholder futuro sem manager funcional", mas `windows-winget` já existe e é testado (`native_test.go`)

### 7.2 — README desatualizado
- Afirma "23 métodos suportados" — hoje são ~28
- Não lista `pnpm`, `bun`, `vscodium`, `conda`, `asdf`, `yarn-berry`
- A seção de placeholders menciona `{latest}` como "resolvido via GitHub API" mas sem detalhes de formato

### 7.3 — Testes com asserção "de mentirinha"
✅ Substituída por `t.Errorf` real: verifica que campo `Tool` vazio não renderiza `tool=` na saída.

### 7.4 — Testes de validação usam `t.Logf` em vez de `t.Errorf`
Vários testes em `pkg/validate/structural_test.go` registram condições potencialmente incorretas com `t.Logf` em vez de `t.Errorf`/`t.Fatalf`. Comportamento anômalo não quebra o CI — só fica no log. Converter para asserções reais.

### 7.5 — `.goreleaser.yaml` com regra windows morta
`format_overrides` define formato `zip` para `goos: windows`, mas `builds.goos` só lista `linux` e `darwin` — a regra nunca dispara. Remover ou adicionar `windows` a `goos`.

### 7.6 — `release.github.owner/name: depengine/depengine`
Parece placeholder de template. Confirmar se é o repositório real ou atualizar.

### 7.7 — Import não-alfabético em `main.go`
✅ `"sort"` movido para depois de `"path/filepath"`, obedecendo ordenação alfabética da stdlib Go.

### 7.8 — Nomenclatura confusa em `pkg/native`
`ManagerNames()`, `ManagerBinaryNames()` e `ManagerNamesForClan()` têm responsabilidades sobrepostas e nomes parecidos. Documentar a diferença ou consolidar.

---

# 📊 Status Atual Detalhado

| Componente | Status | Testes |
|------------|--------|--------|
| `pkg/run` — subprocess seam | ✅ | 8 |
| `pkg/engine` — facts + resolve | ✅ | 25 |
| `pkg/native` — managers (15 distros) | ✅ | 37 |
| `pkg/schema` — parser TOML | ✅ | 12 |
| `pkg/schema` — placeholders | ✅ | 9 |
| `pkg/exec` — adapter interface + registry + Remover | ✅ | 5 |
| `pkg/exec` — executor central | ✅ | 25 |
| `pkg/exec` — native adapter | ✅ | 14 |
| `pkg/graph` — dependency resolver | ✅ | 7 |
| `pkg/lang` — 23 adapter kinds | ✅ | 40 |
| `pkg/git` — git adapter | ✅ | 8 |
| `pkg/httpdownload` — http adapter | ✅ | 21 |
| `pkg/log` — structured logging | ✅ | 22 |
| `pkg/state` — lifecycle state tracking | ✅ | 9 |
| `pkg/validate` — schema validation | ✅ | 72 |
| CLI (install/check/status/remove/validate/forget) | ✅ | 29 integration |
| **Total** | **13 packages** | **353** |

---

# 📝 Notas de Implementação

- **Logging**: `pkg/log` com slog (stdlib). `run.LoggingRunner` decorator permite tracing de subprocesso com contexto tool/method. JSON: `DEPENGINE_LOG_JSON=1`.
- **Adicionar adapter** = implementar `exec.Adapter` + chamar `exec.Register()` num `init()`.
- **Placeholders**: substituídos em `ParseSchema()` via `BuildMap(facts, clan)`. Adapters recebem strings expandidas; `{pkg}` substituído pelo adapter; `{latest}` resolvido pelo adapter HTTP/Git via `ghrelease.ResolveLatest`.
- **Extensibilidade**: executor chama `adapter.Available()` → `Check()` → `Install()` — sem conhecer detalhes do método.
- **Separação**: `pkg/native` = gerenciadores de pacote do sistema. `pkg/lang` = gerenciadores de ecossistema de linguagem.
- **Registry global**: adapters registram-se via `init()` + `exec.Register()`. Per-instance `WithAdapters()` adiciona adapters ao executor sem substituir os globais.
- **Native auto-detect**: `NativeAdapter` detecta clan via `which {manager}` sem config manual.
- **State tracking**: `pkg/state` em `~/.local/state/depengine/state.json` (XDG + flock + SHA256 hash).
- **Remover interface**: `exec.Remover` opcional — adapters sem `Remove()` geram instrução manual.

---

_Última atualização: 2026-07-11. Campanhas 0–4 ✅ + Lifecycle Tracking ✅. 13/13 packages, 353+ testes. Campanha 6: 5/6 itens ✅ (P0/P1 feitos). Campanha 7: 2/8 itens ✅. Em andamento: Campanha 5 (Polimento) + resto de Campanha 6-7._
