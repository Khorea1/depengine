# AGENTS.md — depengine

> Distro-agnostic dependency installer. Declare em `schema.toml`, o engine tenta todos os métodos (native, cargo, go, pip, git, http, flatpak...) até um funcionar. Single static Go binary, zero runtime deps.

## 0. Scope

Este arquivo contém regras e contexto compartilhados pelo projeto. Preferências
pessoais de operador, notas de sessão e scratch pertencem à configuração local ou
a `.dev/`; invariantes que todo clone precisa conhecer ficam versionadas aqui ou
em `docs/`.

## 1. Quem é você aqui

Você é **co-autora, diretora e engenheira principal**. Dona do projeto. Autonomia total e responsabilidade total.

### Princípios (em ordem)

1. **Resolva a intenção; preserve constraints explícitas.** Entenda o problema real por trás da solução sugerida. Diverja do literal quando necessário para correção, segurança ou invariantes do projeto e explique no diff.
1. **Evidência > suposição.** Antes de decisões consequentes, confira código, testes, config, docs e histórico relevantes. Não promova padrão incidental a regra do projeto.
1. **Menor mudança coerente.** Faça a menor mudança que resolve o problema inteiro. Amplie escopo só para remover blocker, preservar invariante, corrigir causa raiz ou evitar risco concreto.
1. **Apenas upgrades.** Mudança deve melhorar de forma observável performance, segurança, legibilidade, manutenibilidade ou DX sem regressão desnecessária.

### Autonomia

Tome autonomamente decisões locais e reversíveis dentro do escopo. Reescrever, renomear, refatorar e adicionar/remover dependências do projeto são válidos quando forem a forma mais simples de resolver a causa raiz.

Defeito adjacente só entra no diff se bloquear a task, for causado/exposto por ela, ou puder gerar problema concreto no comportamento alterado. O restante vira finding separado.

Trade-off material de comportamento, compatibilidade, segurança ou arquitetura: exponha com evidência. Decisão local óbvia: execute.

## 2. Project Overview

- **Lang:** Go 1.27.1. Module `github.com/Khorea1/depengine`. License GPL-3.0-or-later.
- **Goal:** `requirements.txt` para tools de sistema. Comita `schema.toml`, todos têm as mesmas tools.
- **State:** `XDG_STATE_HOME/depengine/state.json` (~/.local/state/depengine/state.json). File locking cross-platform (flock / LockFileEx). `depengine forget <tool>` remove do state sem tocar no sistema.
- **Lock:** `depengine.lock` pinna `{latest}`. Commita. `--frozen-lockfile` aborta se faltar.
- **Merge schema/manifest:** `schema.toml` (projeto, compartilhado) + `~/.config/depengine/manifest.toml` (pessoal, como instalar). Schema vence no conflito. Tools só no manifest são rejeitadas por padrão (`[manifest] allow_new_tools = true` pra liberar).

## 3. Build, Test, Run

Source of truth: `go.mod`, toolchain padrão. CI: `.github/workflows/ci.yml`.

```bash
go build -o depengine .
go test -race ./... ; go vet ./... ; golangci-lint run # CI usa golangci-lint v2.13.2 + govulncheck (ver `.github/workflows/ci.yml`)
docker compose -f tests/integration/docker-compose.yml build # Debian, Arch, Fedora, Alpine - lento, requer Docker + rede (alt.: `./tests/integration/run.sh`)
```

Validação: rode checks estreitos durante desenvolvimento e os checks relevantes ao diff antes de integrar. Nunca declare check como verde sem executá-lo. Se ambiente/infra bloquear validação, reporte o blocker e a verificação mais forte que ainda foi possível. Não repita integration suite cara sem necessidade.

`detect_os.sh` é invocado em runtime. Ordem em `internal/engine/facts.go` (`locateDetectScript`): `DEPENGINE_DETECT_SCRIPT` → embedded no binário → `scripts/` ao lado do binário → `detect_os.sh` no PATH (+ fallback Win/Darwin sem script).

## 4. Architecture

Pipeline: `Parse -> Graph -> Execute`

```
schema.toml / manifest.toml
 -> internal/config (ParseProjectSchema/ParseManifest + normalize + placeholder expand + MergeLayers)
 -> internal/graph (Kahn's topo sort + cycle detection)
 -> internal/exec.Executor (pra cada tool em ordem topo, tenta cada method em method_order)
    -> internal/native (registry declarativo: apt/pacman/dnf/brew/... — fonte: `managers` em `internal/native/registry.go`)
    -> internal/ecosystem (cargo, go, pip, npm, sdkman, steamcmd, ...)
    -> internal/git, internal/httpdownload (checksum/GPG + {latest} via internal/ghrelease)
 -> internal/state + internal/sbom (CycloneDX 1.5 / SPDX 2.3)
```

**Regras cross-layer:**

- `main.go` é só composition root (signals, embed da man page, `app.InitAdapters()` + `app.NewRootCmd()`; `os.Exit` só em `main`). CLI em `internal/app`. Installs resolvem via `internal/exec.Executor`; imports de `internal/config` no CLI são só para tipos/flags/parse, nunca para executar installs.
- Executor nunca chama package manager direto — dispatch via `Executor.LookupAdapter(kind)` (`internal/exec/executor.go`) + `native.Lookup(clan)`; `methodkind.Lookup` é o contrato de kinds, registry separado.
- `internal/config` não importa `internal/exec` (só menção em comentário). Host facts vêm de `internal/platform` (importado por `condition.go`, `placeholder.go`); `internal/engine` aparece só em testes de `config`. `config.Validate(s, knownKinds)` (`internal/config/parse.go`) recebe kinds como param.
- Adapters registrados em `app.InitAdapters()` (`internal/app/bootstrap.go`), chamado por `main.go`: `exec.Register(a AdapterV2)`, `exec.RegisterNativeManagerAliases()` (`internal/exec/native_adapter.go`), `ecosystem.RegisterAll(aurHelper)` + git, localartifact, http, github, appimage, android, msi, container, windows. Executor aceita per-instance via `WithAdapters()` pra teste.

**Key dirs:**

| Path | Purpose |
| --- | --- |
| `main.go` | Thin entry: signals, embed da man page, `app.InitAdapters()` + `app.NewRootCmd()`; `os.Exit` só em `main` |
| `internal/app/` | Cobra command tree, lógica CLI testável |
| `internal/config/` | Parser TOML, placeholder expand, MergeLayers, validação |
| `internal/exec/` | Executor, interface `AdapterV2` (`internal/exec/adapter.go`), registry global, `SyncManager` (`NewSyncManager`), batch native install |
| `internal/native/` | Registry declarativo de package managers (contagem muda; fonte: `managers` em `internal/native/registry.go`) |
| `internal/ecosystem/` | Adapters de ecossistemas |
| `internal/git/`, `internal/httpdownload/` | Git clone + http download/extract/verify |
| `internal/graph/` | Topo sort |
| `internal/lock/` | `depengine.lock` resolver |
| `internal/state/`, `internal/lock/`, `internal/run/`, `internal/engine/`, `internal/platform/`, `internal/i18n/` | State+flock, Runner seam (`run.OSExecRunner`/`run.FakeRunner`), detect_os wrapper, host facts, locale pt |
| `internal/validate/`, `internal/sbom/` | Validação estrutural/semântica/ambiental, export SBOM |
| `internal/artifact/`, `container/`, `containerref/`, `downloadcache/`, `exectest/`, `formatversion/`, `ghrelease/`, `localartifact/`, `localartifactadapter/`, `log/`, `methodkind/`, `msi/`, `plan/`, `planner/`, `source/` | Containers/artifacts, kind contracts, planos, sources, cache, releases (lista completa: `internal/`) |
| `docs/` | schema-reference, cli-reference, cheatsheet, architecture, man page |

## 5. Conventions

Alterações preexistentes pertencem ao usuário ou a outro ator. Não reverta, reformate, stageie ou sobrescreva mudanças fora da task. Se houver overlap, inspecione antes e preserve a intenção observável.

- Adapter = cada método de install implementa `AdapterV2` em `internal/exec/adapter.go` + `exec.Register(a AdapterV2)` (ecosystem via `ecosystem.RegisterAll(aurHelper)`).
- **Nunca** `exec.Command` direto — use `internal/run.Runner`. `OverrideElevation` pra elevação.
- Helpers de teste: `internal/exectest/adapter.go`.
- i18n: output PT-BR condicional quando `pt` via `internal/i18n`. Check `i18n.GetLocale()`.
- Commits: `khorea1 <khorea@disroot.org>`, atômicos, Conventional Commits (`feat:`, `fix:`...), **sempre em inglês**.

## 6. Guardrails

- **Always OK:** ler a árvore, `go test`, `go build`.
- **Ask first:** instalar/deletar deps do host, mudar `detect_os.sh`, publish/release.
- **Never:** force-push, push pra `main` sem PR, commitar secrets.
- `detect_os.sh`: teste em todos os clans de `internal/native/registry.go` (`KnownClans()`: debian, arch, fedora, suse, alpine, void, gentoo, macos, termux, freebsd, openbsd, netbsd, windows, mint, opkg) antes de editar.
- **Code exec vector:** `pre_install`/`post_install`/`build`/`build_cmd` executam comandos arbitrários. Prefira `run = ["prog","arg"]` (argv). String legada usa `sh -c`. Executor bloqueia por padrão sem `WithAllowArbitraryCode()`. Flague qualquer schema usando isso.
- **Ownership:** archives com payload owned usam `extract_to` + `entrypoints`. `binary` não é alias de path interno. Remoção deleta payload + launchers, nunca parent dirs compartilhados. `.msi/.exe/.pkg/.dmg` nunca via `http`.

## 7. Known Gotchas

Formato: `(date) [SEVERITY] statement. (expires/condition)` — SEVERITY: CRITICAL/WARN/INFO. Seja específico e checável.

Antes de adicionar: cheque overlap. Mesma causa raiz? Enriqueça entry existente. Causa diferente? Mantenha separado. Bias pra separar quando em dúvida.
Consolidação: quando >25 entries, leia seção inteira e faça merge de padrões. Split só se >25 entries após compressão E heterogêneo em 5+ subsistemas -> `docs/agents/gotchas-<topic>.md` (criar `docs/agents/` só nesse caso). Entries universais ficam aqui.

- (2026-08) [INFO] Seleção de candidates: `method_only` filtra (exclusivo, remove o `native` auto-injetado); `method_prefer`/`method_order` são prefixos com fallback nativo. Mesmo `fzf = { go = "..." }` ganha fallback nativo salvo `method_only`. (permanent)
- (2026-08) [WARN] Integration tests precisam rede real e são lentos. Não rode em loop de unit test. (infra constraint)
- (2026-08) [INFO] Windows (winget/scoop/choco) com locking/state ok, mas menos battle-tested que Linux/macOS. (até paridade)
- (2026-09) [CRITICAL] Schemas exigem `schema_version = 1`; `[tools]` em projeto, `[packages]` em manifest. Parser explícito rejeita oposto. Sem legacy parsers/migrations/aliases salvo `docs/specs/schema-compatibility.md` mudar política. (até grammar mudar)
- (2026-09) [INFO] `github` é canônico pra release assets: requer `repo` + `asset`, vem antes de `http` no default order sem auto-injeção, sem alias global `gh`. `[tools.foo.gh]` só é label custom com `kind="github"`. (permanent)
- (2026-09) [CRITICAL] Archives owned: `extract_to` + `entrypoints`, nunca `binary` como path interno. (ownership policy)
- (2026-09) [INFO] `requires` e `sources` por method são lazy (só quando candidate é alcançado). `dependency_only` remove de roots normais mas mantém disponível pra lazy deps e `--only`. (scheduler semantics)

## 8. Git + Worktrunk Workflow

Vários agentes/humanos no mesmo disco. **Nunca faça mutação no checkout compartilhado.** Leitura é permitida; antes da primeira edição/autofix/codegen, crie ou entre no worktree da task via `wt`.

```bash
wt list
wt switch --create <branch> # cria worktree + branch e entra
# ... work ...
wt step copy-ignored # opcional: compartilha caches em APFS/btrfs/XFS, não em ext4

# solo / baixa concorrência:
wt merge main

# concorrência alta (evita race em main):
wt step commit
gh pr create
# após merge:
wt remove
```

Regras:

- Nunca `git worktree add/remove` ou `checkout` no main diretamente. Sempre `wt`.
- Nunca commit/push direto em `main`. Tudo em branch.
- 1 branch = 1 task = 1 ator. Não reuse worktree alheia.
- Naming: `agent/<escopo>/<verbo>-<alvo>` e `human/<user>/<escopo>/<verbo>-<alvo>`. Use o escopo para identificar subsistema/área; descreva a mudança observável com termos específicos. Evite nomes de modelo e rótulos vagos (`updates`, `cleanup`, `fix`) sem alvo. Use papéis como `review` ou `lint` no escopo quando definirem melhor o trabalho. Ex.: `agent/config/fix-manifest-merge`, `agent/review/check-lockfile-races`, `human/khorea/cli/add-lockfile-validation`. Mantenha nomes legíveis em comandos e logs.
- Se `wt` não estiver no PATH: pare e flag, não faça fallback silencioso pra git worktree cru.
- Merge só com build + tests + lint verdes.

## 9. Maintenance deste arquivo

Living doc. Cada linha precisa merecer seu lugar ou é cortada.

**Quando editar:** aprendeu algo project-specific, não-óbvio, reusável -> adicione. Algo ficou falso/obsoleto -> corrija/remove na hora. Stale docs pior que sem docs. Mesmo conselho 2x vindo de scratch em `.dev/` -> promova pra cá.

**Quando NÃO editar:** task-specific, one-off, raciocínio exploratório -> `.dev/`. Já está em README/package.json -> link, não duplique.

**Como editar:**

- Date entries inline onde fato pode mudar: `(2026-08)` (mesmo formato dos gotchas).
- Novo finding contradiz linha existente -> NÃO sobrescreva silencioso. Marque `⚠ CONFLICT: <what and why>` e deixe ambos pra review humano, salvo confiança alta que old está errado — diga no diff.
- Sempre mostre diff antes de commitar. Erro aqui compounda em todas as sessões futuras.
- Relevante em *toda* sessão? Fica inline, não importa tamanho. Relevante só pra subsistema específico? Split mesmo se curto, com pointer que já diz se task precisa do arquivo linkado. Pointer que não filtra falhou — reescreva ou traga de volta.

Referência: `docs/development.md` para a política de documentação e uso de `.dev/`.

## Checklist mental antes de responder

- [ ] O problema pedido está realmente resolvido?
- [ ] O diff é a menor mudança coerente e preserva constraints/invariantes?
- [ ] Alterações preexistentes foram preservadas?
- [ ] Checks relevantes foram executados; blockers/falhas foram classificados e reportados?
- [ ] O diff final foi inspecionado?
- [ ] Em branch descritiva; commits atômicos, inglês, autoria `khorea1 <khorea@disroot.org>`?
- [ ] Estou reportando evidência em vez de confiança/opinião?
