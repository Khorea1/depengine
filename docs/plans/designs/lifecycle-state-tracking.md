# Lifecycle State Tracking — Design Document

**Status:** Aprovado  
**Data:** 2026-07-09  
**Projeto:** depengine  
**Motivação:** Fechar o ciclo de vida do gerenciamento de dependências — trackear o que foi instalado, permitir remoção e consulta de status.

---

## Abordagem Escolhida

**A1 → A2** (State Tracking primeiro, Full Lifecycle depois com base no state).

### Decisões

| Decisão | Opção |
|---------|-------|
| Local do state | `~/.local/state/depengine/state.json` (XDG) |
| Remove() na interface | Opcional via type assertion (`Remover` interface) |
| Lock de concorrência | flock em `state.lock` adjacente |

---

## Seção 1 — Formato do State File

**Arquivo:** `~/.local/state/depengine/state.json`

```json
{
  "version": 1,
  "schema_path": "/home/user/projetos/motor/schema.toml",
  "schema_modified_at": "2026-07-09T12:00:00-03:00",
  "tools": {
    "nvim": {
      "method": "pacman",
      "adapter_kind": "native",
      "installed_at": "2026-07-09T10:00:00Z",
      "postinstall_done": true,
      "definition_hash": "sha256:abc123"
    }
  }
}
```

- `version`: schema version do state (para migrações futuras)
- `schema_path`: caminho absoluto do schema usado (para `status` saber qual schema comparar)
- `schema_modified_at`: timestamp de modificação do schema
- `tools`: map de tool name → metadata
  - `method`: método que funcionou (ex: `pacman`)
  - `adapter_kind`: qual adapter (`native`, `lang`, `git`, `http`)
  - `installed_at`: timestamp ISO8601
  - `postinstall_done`: se o hook rodou
  - `definition_hash`: SHA256 da definição do tool no schema

---

## Seção 2 — Comando `depengine status`

### Uso

```sh
depengine status                    # tabela formatada
depengine status --json             # JSON estruturado
depengine status --orphans          # só tools instalados que não estão mais no schema
depengine status --schema <path>    # override do schema
```

### Lógica

1. Lê `state.json`
2. Lê schema do caminho no state (ou `--schema` override)
3. Para cada tool no schema:
   - Se está no state → roda `Check()` do adapter → `installed` ou `missing`
   - Se não está no state → `missing`
   - Se `definition_hash` diferente → `outdated`
4. Tools no state que não estão no schema → `orphan`

### Flags

| Flag | Efeito |
|------|--------|
| `--schema <path>` | override do schema |
| `--json` | saída JSON |
| `--orphans` | só tools órfãos |

---

## Seção 3 — Comando `depengine remove <tool>`

### Uso

```sh
depengine remove nvim          # remove tool específico
depengine remove --all         # remove todos os tools do state
```

### Interface Remover (opcional)

```go
type Remover interface {
    Remove(ctx context.Context, runner run.Runner, tool *schema.Tool, method *schema.MethodCandidate) error
}
```

### Comportamento

1. Lê state → descobre adapter + método que instalou
2. Type assertion pra `Remover`
3. Se implementa → executa `Remove()`, atualiza state
4. Se não → mostra instrução textual pro usuário

### Adapters que implementarão Remove (primeiro lote)

| Adapter | Comando |
|---------|---------|
| NativeAdapter | `apt remove -y`, `pacman -R --noconfirm`... |
| CargoAdapter | `cargo uninstall` |
| GoAdapter | `go clean -i` |
| PipAdapter | `pip uninstall -y` |
| PipxAdapter | `pipx uninstall` |
| NpmAdapter | `npm uninstall -g` |
| FlatpakAdapter | `flatpak uninstall -y` |

---

## Seção 4 — Integração com `depengine install`

### Fluxo

```
depengine install (schema.toml)
  │
  ├─ 1. load schema, gather facts, resolve clan
  ├─ 2. executor.Execute(ctx, schema, clan)
  ├─ 3. (NOVO)  adquirir state.lock (flock, timeout 30s)
  ├─ 4. (NOVO)  escrever state.json
  │      └─ tools com StatusInstalled/StatusAlready:
  │           method, adapter_kind, installed_at, postinstall_done, definition_hash
  └─ 5. (NOVO)  liberar lock
```

### Regras

- Sobrescreve state anterior (reflete último `install` completo)
- Se `install` falha no meio, state **não** é atualizado
- Definition hash: SHA256 do bloco TOML do tool + métodos
- Idempotente: rodar duas vezes produz mesmo state

---

## Seção 5 — Pacote: `pkg/state`

Novo pacote para toda lógica de state:

| Arquivo | Responsabilidade |
|---------|-----------------|
| `pkg/state/state.go` | Load, Save, ToolState, State struct |
| `pkg/state/lock.go` | Lock/Unlock com flock |
| `pkg/state/hash.go` | DefinitionHash do tool |

### API

```go
package state

type State struct {
    Version          int                  `json:"version"`
    SchemaPath       string               `json:"schema_path"`
    SchemaModifiedAt string               `json:"schema_modified_at"`
    Tools            map[string]ToolState `json:"tools"`
}

type ToolState struct {
    Method           string `json:"method"`
    AdapterKind      string `json:"adapter_kind"`
    InstalledAt      string `json:"installed_at"`
    PostinstallDone  bool   `json:"postinstall_done"`
    DefinitionHash   string `json:"definition_hash"`
}

func Load() (*State, error)          // lê do XDG state dir
func Save(s *State) error            // escreve com lock
func Lock() (io.Closer, error)      // flock acquire
func DefinitionHash(tool *schema.Tool) string  // SHA256
func DefaultPath() string            // ~/.local/state/depengine/state.json
```

---

## Dependências entre pacotes

```
main.go → pkg/state → pkg/exec (para Check)
pkg/exec → (interface Remover opcional, nenhuma dependência de state)
```

`pkg/state` importa apenas stdlib (encoding/json, crypto/sha256, os, path/filepath) + `pkg/schema` para `DefinitionHash`.

---

## Pendências para implementação

- [ ] `pkg/state/` — structs, load, save, lock, hash
- [ ] `pkg/exec/adapter.go` — interface `Remover` + type assertion helpers
- [ ] Adaptadores: implementar `Remove()` nos 7 do primeiro lote
- [ ] `main.go` — comando `status` (nova função `runStatus`)
- [ ] `main.go` — comando `remove` (nova função `runRemove`)
- [ ] `executor.go` — escrita de state pós-install (injetar callback ou integrar no fluxo)
- [ ] Testes: `pkg/state/state_test.go`, adapters com Remover mock, status/remove CLI tests
