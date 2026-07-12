# depengine — Próximos passos

> **Data:** 2026-07-12
> **Status base:** `go test ./...` — 0 falhas (16 packages com testes, 0 falhas)

> **Vet:** `go vet ./...` — limpo

---

## ✅ O que está feito (resumo)

12 campanhas concluídas — motor completo e polido. + 16 issues identificados em revisão de código.

| Campanha | Escopo |
|----------|--------|
| **0 — Fundação** | `pkg/run`, `pkg/engine` (Facts, GatherFacts, ResolveFamily — 12 clans), `pkg/native` (15 distros), `pkg/schema` (parser TOML + placeholders), CLI básica |
| **1 — Executor + Adapters** | Interface Adapter + graph (topo-sort Kahn) + mock; **23 adapters** (native, cargo, go, pip, pipx, uv, npm, pnpm, bun, gem, yarn, yarn-berry, composer, apm, vscode, vscodium, flatpak, snap, cask, mas, sdkman, steamcmd, pacstall, aur, conda, asdf, git, http); executor central com fallback/when/requires/postinstall/timeout/dry-run; docker integration tests (4 distros) |
| **2 — Logging** | `pkg/log` (slog + LogContext + testlog); instrumentação do motor; `--diagnose` modo verboso |
| **3 — Validação** | `pkg/validate` estrutural + semântica + ambiente (`--check-env`); CLI `validate` com `--format=json --strict` |
| **4 — Expansão** | pnpm, dnf5, conda, asdf/mise, vscodium, bun |
| **5 — Polimento & Release** | CI/CD (4 platforms), autocomplete bash/zsh/fish, man page, cheatsheet, benchmarks (<1ms parse), cross-platform Docker |
| **Lifecycle State** | `pkg/state` (XDG + flock + definition hash); `exec.Remover`; `depengine status/remove` |
| **6 — Graph** | `depengine graph --format=mermaid|dot|text`, `pkg/graph/render.go`, `pkg/graph/render_test.go`, CLI integrada |
| **7 — Paralelismo** | `--jobs=N` no executor, worker pool por nível topológico, `sync.Mutex` no `ExecReport` |
| **8 — Segurança** | `--allow-arbitrary-code`, `hasDangerousMethod`, TOFU warning no `sha256:auto`, aviso na primeira execução de build scripts |
| **9 — Perfis** | `--profile`, campo `Tags` no schema.Tool, filtro `filteredByTags`, renderização no `graph` |
| **10 — Rollback** | `depengine undo`, `state.SaveSnapshot/LoadSnapshot/ListSnapshots`, snapshot antes de cada install, remoção via Remover |
| **11 — SBOM** | `depengine sbom --format=cyclonedx|spdx`, `pkg/sbom/export.go` com CycloneDX 1.5 e SPDX 2.3, PURL, version extraction |
| **12 — i18n** | `pkg/i18n` com locale detection (LANG/LC_MESSAGES), traduções EN/PT embutidas via `//go:embed`, split do `printUsage()` em PT/EN, man page bilingue |

---

---

## 🐛 Issues identificados por revisão de código (2026-07-11)

Issues verificados contra o código, organizados por área. Alguns têm PRs/discussões
abertas em paralelo.

### 🐞 Bugs funcionais

| # | Issue | Arquivo | Severidade |
|---|-------|---------|------------|
| **B1** | `sha256:auto` sempre quebrado: `verifyChecksum` busca por `"download.tar.gz"` no .sha256, mas o asset original tem nome diferente (ex: `fastfetch-linux-amd64.deb`). | `pkg/httpdownload/adapter.go` | 🔴 Crítico |
| **B2** | Lockfile não salvo quando `report.Failed > 0`: `os.Exit(1)` executa **antes** do bloco que resolve/salva o `schema.lock`. Tools instaladas com sucesso na mesma execução perdem o pin. | `main.go` (runInstall, ~linha 201) | 🟠 Alto |
| **B3** | `release-notes.sh`: backticks em `echo "- Detailed man page and \`--help\` output."` viram substituição de comando no shell. | `scripts/release-notes.sh` | 🟠 Alto |
| **B4** | `remove --schema` documentado no man page (`docs/depengine.1`) mas não existe em `runRemove` (só `--all`, `--dry-run`). Inverso: `status --schema` existe no código mas não documentado. | `docs/depengine.1`, `main.go` | 🟡 Médio |

### ⚠️ Problemas de design

| # | Issue | Arquivo | Severidade |
|---|-------|---------|------------|
| **D1** | `defaults.aur_helper` do schema.toml é ignorado: `initAdapters()` chama `lang.RegisterAll("paru")` com string fixa **antes** do schema ser parseado. O campo documentado no schema não tem efeito. | `main.go` (~linha 1045) | 🟠 Alto |
| **D2** | `depengine status` usa `loadSchema()` (que spawna `detect_os.sh`) sendo comando read-only, ao passo que `check`/`graph` usam `ParseSchemaNoFacts`. Inconsistência de performance. | `main.go` (runStatus) | 🟡 Médio |
| **D3** | `DefinitionHash` é calculado e persistido em `state.json` mas **nunca lido/comparado** — a funcionalidade de detectar "outdated tools" no `status` nunca foi implementada. | `pkg/state/hash.go` + `main.go` | 🟡 Médio |
| **D4** | Ordem dos resultados é não-determinística com `--jobs > 1` (`executeLevelParallel` drena um channel), quebrando a suposição implícita de ordem topológica nos relatórios. | `pkg/exec/executor.go` | 🟡 Médio |
| **D5** | Aviso de "arbitrary code" (`hasDangerousMethod`) só verifica chaves `build`/`build_cmd`/`build_command` (adapter `git`). AUR, pacstall, `.deb` (post-install dpkg) executam código arbitrário e não disparam o aviso — falsa sensação de segurança. | `pkg/exec/executor.go` | 🟡 Médio |
| **D6** | `--jobs > 1` não documenta limitação: rodar `native` para várias tools em paralelo pode colidir com lock do apt/dpkg/pacman. | `docs/` | 🟢 Leve |
| **D7** | `HTTPAdapter.Available()` roda `SelectDownloader(ctx, rn)` (2 subprocessos `which`) toda vez só pra "aquecer" — descarta o resultado. Sem cache, é desperdício puro. | `pkg/httpdownload/adapter.go` | 🟢 Leve |

### 📄 CLI / Documentação

| # | Issue | Arquivo | Severidade | Status |
|---|-------|---------|------------|--------|
| **C1** | `depengine graph` não aparece no bloco "Uso:" — adicionado | `main.go` (printUsage) | 🟡 Médio | ✅ |
| **C2** | `--profile` flag do `install` não aparecia no help inline — adicionado | `main.go` (printUsage) | 🟡 Médio | ✅ |
| **C3** | Seção "Flags (validate):" vazia — preenchida com `--schema`, `--check-env`, `--format`, `--strict` | `main.go` (printUsage) | 🟡 Médio | ✅ |

**3/3 implementados.** Help inline completo para todos os comandos e flags.

### 🧹 Qualidade / Manutenção

| # | Issue | Arquivo | Severidade | Status |
|---|-------|---------|------------|--------|
| **Q1** | `showNativeCommands()` código morto — removido | `main.go` | 🟢 Leve | ✅ |
| **Q2** | PT/EN mixing: mensagens de CLI em inglês convertidas para português | `main.go` | 🟢 Leve | ✅ |
| **Q3** | Validação de tipos: `commonStringKeys` (pkg, cask, app, source, repo, formula, …) | `pkg/validate/structural.go` | 🟢 Leve | ✅ |
| **Q4** | `syscall.Flock` em Windows — build tag + stub | `pkg/state/lock_unix.go` + `lock_windows.go` | 🟢 Leve | ✅ |
|
| **4/4 implementados.** Dead code removido, mensagens consistentes em PT, validação de tipos estendida, suporte Windows documentado via build tags.

---

## 🗺️ Próximas features

Priorizadas do mais fácil/rápido para o mais complexo. Estimativa: ⭐ rápido (<1h), ⭐⭐ médio (1-2h), ⭐⭐⭐ complexo (2-4h), ⭐⭐⭐⭐ grande (4h+).

---

### ⭐ Quick wins

#### 1. JSON Schema para schema.toml
Publicar um `schema.json` que descreve a estrutura do `schema.toml`. Editores com extensão TOML (taplo) usam isso para autocomplete, validação inline e hover docs — sem precisar rodar `depengine validate`.
- **Esforço:** ⭐
- **Dependências:** nenhuma
- **Artefato:** `schema/depengine.schema.json` + link no README

#### 2. Fuzzing no parser
Go tem fuzzing nativo (`go test -fuzz`). `ParseSchema` + `Expand` com placeholders são candidatos clássicos a panic em entrada malformada.
- **Esforço:** ⭐
- **Artefato:** `*_test.go` com `fuzz.Fuzz` para `ParseSchema` e `Expand`

---

### ⭐⭐ Médios

#### 3. Suíte de conformidade de adapters
Teste genérico que roda contra qualquer `exec.Adapter` registrado e verifica invariantes: Kind único, Check nunca instala, Available reflete o ambiente, Install com FakeRunner falha com erro esperado. Evita que um novo adapter quebre contratos silenciosamente.
- **Esforço:** ⭐⭐
- **Motivação:** 23 adapters é superfície grande de manutenção; garantir que todos respeitam o mesmo contrato sem precisar testar um por um
- **Possível formato:** `pkg/exectest/conformance.go` — `TestAdapterConformance(t *testing.T, a exec.Adapter)`

#### 4. `depengine why <tool>` — ✅ Implementado
Expor a resolução de métodos como comando dedicado — mostra por que cada método foi pulado (when não bateu, adapter indisponível, já instalado, falhou), similar a `apt-cache policy` ou `cargo tree`. O executor já calcula isso internamente no `executeTool`; agora exposto como `depengine why <tool>` (texto e JSON) via `Executor.ExplainTool()`.

#### 5. Lockfile (`schema.lock`) — ✅ Implementado
Hoje `{latest}` é resolvido on-the-fly a cada `install` — rodar hoje vs mês que vem pode dar binários diferentes. Um `schema.lock` (estilo Cargo.lock/package-lock) grava a versão/checksum resolvido na primeira execução. `install` subsequente usa o lock a menos que rode `depengine update`.

**Implementado:** `pkg/lock` (ResolveAll, Apply, Load, Save), auto-save no `install`, `--frozen-lockfile` (CI), `depengine update [--schema]` para re-resolver placeholders.

#### 6. Visualização do grafo de dependências — ✅ Implementado
`depengine graph --format=mermaid|dot|text` via `pkg/graph/render.go`, integrado na CLI.

#### 7. Paralelismo no mesmo nível topológico — ✅ Implementado
`--jobs=N` no executor, worker pool por nível topológico, `sync.Mutex` no `ExecReport`.

---

### ⭐⭐⭐ Complexos

#### 8. Segurança de supply chain — ✅ Implementado
- `checksum = "sha256:auto"` é TOFU — calcula e aceita, sem verificação contra fonte confiável
- `git adapter` com `build = "make && sudo make install"` é execução arbitrária com sudo vindo de um TOML — se schema.toml vem de um repo compartilhado, é "curl | sudo bash" com verniz declarativo
- Flag `--allow-arbitrary-code` ou aviso na primeira execução de tool com `build`/`http` executável
- **Esforço:** ⭐⭐⭐
- **Modelo:** Homebrew com casks que rodam scripts pós-install

#### 9. Perfis / tags — ✅ Implementado
Suporte a `--profile minimal|desktop|server` selecionando subconjuntos de tools por tag no schema (`tags = ["desktop"]`), similar a tags do Ansible. Resolve schema único para múltiplos tipos de máquina sem duplicar arquivos.
- **Esforço:** ⭐⭐⭐
- **Artefato:** campo `tags` em Tool, filtro por `--profile` no `filterTools`

#### 10. Rollback / snapshot (`depengine undo`) — ✅ Implementado
`state.go` já rastreia o que foi instalado e por qual método; `exec.Remover` já existe. `depengine undo` para desfazer o último install ou reverter para um snapshot anterior é uma extensão natural.
- **Esforço:** ⭐⭐⭐
- **Arquitetura:** salvar snapshot do state antes de cada `install`; `undo` restaura o snapshot anterior e chama `Remove` para tools que sumiram

#### 11. SBOM export — ✅ Implementado
`state.json` já é um bill-of-materials. Exportar como CycloneDX ou SPDX abre integração com scanners de vulnerabilidade (grype/trivy).
- **Esforço:** ⭐⭐⭐
- **Artefato:** `depengine sbom --format=cyclonedx|spdx`, `pkg/sbom/export.go` (CycloneDX 1.5 + SPDX 2.3)

---

### ⭐⭐⭐⭐ Grandes / Ambiciosos

#### 12. Repensar adapters de linguagem
23 adapters é superfície grande de manutenção e teste. Vale explorar delegar versionamento de toolchain para ferramentas que já resolvem isso bem (`mise`, `asdf`) em vez de reimplementar `cargo`/`go`/`pip`/`npm`/`gem`/... um por um.
- **Esforço:** ⭐⭐⭐⭐
- **Risco:** alta — mudança arquitetural que pode quebrar schemas existentes
- **Alternativa:** manter adapters dedicados para os mais usados e delegar o resto para mise/asdf

#### 13. Registry comunitário de schemas
`depengine install --from github.com/fulano/dotfiles` puxando schema remoto (com pin via lockfile+checksum). Transforma o projeto de "ferramenta pessoal" em algo como "Homebrew, mas você é o mantenedor da fórmula".
- **Esforço:** ⭐⭐⭐⭐
- **Depende de:** lockfile primeiro

#### 14. Diff entre máquinas
Comparar `state.json` sanitizado entre duas máquinas para responder "o que a máquina A tem que a B não tem". Encaixe direto com o caso de uso de dotfiles multi-máquina.
- **Esforço:** ⭐⭐⭐⭐
- **Artefato:** `depengine diff <path/to/state.json>` ou pipe entre máquinas via SSH

---

## Recomendação pessoal

Se fosse escolher só **3** para priorizar agora:

1. **Self-update / release automation** (⭐⭐⭐) — `depengine self-update` usando Goreleaser artifacts, ou pelo menos um script de update
2. **Cachê de downloads** (⭐⭐⭐) — compartilhar downloads HTTP entre instalações para evitar baixar o mesmo .deb/.AppImage duas vezes
3. **Hooks (pre/post-install)** (⭐⭐⭐) — scripts que rodam em eventos do ciclo de vida, equivalentes aos hook scripts do apt/pacman

_Última atualização: 2026-07-12 (após item 12 — i18n + C1-C3). Base: 16 packages com testes, 0 falhas._
