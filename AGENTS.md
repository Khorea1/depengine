# depengine — Agent Configuration

**Stack:** Go 1.26.4, CLI binary, TOML schema
**Tests:** 91 tests across 10 packages, all passing (`go test ./...`)
**Build:** `go build -o depengine .`

## Architecture

### Entry point
`main.go` — CLI with `install`, `check`, `list` subcommands. Loads schema, gathers facts, resolves clan, delegates to executor.

### Package layout
```
depengine/
├── main.go                    # CLI entry point
├── schema.toml                # Reference template
├── scripts/
│   └── detect_os.sh           # Distro detection (POSIX sh)
├── pkg/
│   ├── engine/                # Facts, GatherFacts, ResolveFamily, MatchesDistroFamily
│   ├── native/                # Native manager registry (15 distros)
│   ├── schema/                # TOML parser, placeholder expansion
│   ├── exec/                  # Executor, Adapter interface, report, registry, sync
│   ├── lang/                  # Language adapters (cargo, go, pip, pipx, uv, aur, npm, gem, yarn, etc.)
│   ├── git/                   # Git clone+build adapter
│   ├── httpdownload/          # HTTP download adapter + checksum + {latest} resolver
│   ├── graph/                 # Topological sort + cycle detection
│   ├── run/                   # Runner interface (OS exec, fake for tests)
│   ├── log/                   # Structured logging with trace IDs
│   └── exectest/              # Mock adapter for tests
└── tests/
    └── integration/           # Integration tests (Docker-based, TODO)
```

### Key contracts
- **`pkg/exec.Adapter`**: `Kind()`, `Available()`, `Check()`, `Install()` — every install method implements this.
- **`pkg/run.Runner`**: Interface abstracting subprocess execution; `OSExecRunner` for prod, `FakeRunner` for tests.
- **`pkg/schema.Schema`**: Normalized form of the TOML; `Tool` + `MethodCandidate` + `Condition`.
- **`pkg/engine.Facts`**: 1:1 mirror of `detect_os.sh --json` output.
- **`pkg/exec.ExecReport`**: Structured result of a full install run (ToolResult, MethodAttempt).

### Agent conventions
- **Project-level agents**: placed in `.omp/agents/` at project root.
- **Prefer built-in framework agents** (`explore`, `plan`, `reviewer`, `Tester`, `task`, `sonic`) over custom ones when they fit.
- **Skills** already loaded by the system cover Go/TypeScript general patterns; add project-specific skills only when a built-in or global agent doesn't cover a recurrent pain point.
- **Model assignment**: use `smol` for mechanical/exploration tasks (glob searches, grep, simple edits). Use default/strong models for deep reasoning (architecture, design, complex refactors).

### Built-in agents available (framework)
| Agent | Purpose |
|-------|---------|
| `explore` | Read-only codebase scout |
| `plan` | Architecture decisions, multi-file design |
| `designer` | UI/UX (not applicable to CLI) |
| `reviewer` | Code review, quality, security |
| `librarian` | External library/API research |
| `Tester` | Authoritative test writer |
| `task` | General-purpose worker |
| `sonic` | Mechanical updates, bulk data collection |

### Global agents available (`~/.config/opencode/agents/`)
| Agent | Purpose |
|-------|---------|
| `code-reviewer` | Focused code review with smell detection |
| `code-simplifier` | Simplify recently changed code |
| `refactoring` | Safe behavior-preserving refactoring |
| `deep-thinker` | Structured thinking on complex problems |
| `discuss-code` | Critical code discussion |
| `discuss-task` | Requirements clarification |
| `requirements-analyzer` | Feature spec analysis |
| `architecture-reviewer` | Architecture review with `architecture-review` skill |
| `git-manager` | Git workflow management |
| `web-researcher` | Internet research via Exa |
| `pattern-learner` | Extract reusable patterns |
| `prompt-simplifier` | Simplify prompts and instructions |
| `skill-creator` | Create new skills |
| `talk` | Primary-mode discussion partner |

### Current gaps
- **No adapter-specific test agent**: Tests for individual adapters (lang, git, http) require deep domain knowledge. Built-in `Tester` works generically but doesn't know adapter patterns.
- **No integration test runner**: Docker-based integration tests (mentioned in TODO) need orchestration. The `task`/`explore` agents suffice but no dedicated integration test agent exists.
- **No schema validation agent**: TOML schema parsing is complex (3 declaration forms, conditions, placeholders). Could benefit from a specialist, but built-in `reviewer` + `Tester` cover this.

### Rules
1. Never commit generated files unless explicitly asked.
2. Run `go test ./...` before marking any task complete.
3. Use `go vet ./...` for static analysis.
4. Integration tests need Docker; agent must confirm Docker is running before attempting.
5. All agent definitions in `.omp/agents/` use YAML frontmatter + Markdown body, matching the global format.
