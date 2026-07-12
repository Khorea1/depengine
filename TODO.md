# depengine — Próximos passos

> **Data:** 2026-07-11
> **Status base:** `go test ./...` — 0 falhas (15/15 packages pass, 370+ testes)

> **Vet:** `go vet ./...` — limpo

---

## ✅ O que está feito (resumo)

6 campanhas concluídas — motor completo e polido.

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

_Última atualização: 2026-07-11 (após item 11). Base: 17 packages, 0 falhas._
