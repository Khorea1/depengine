# depengine — Agent Configuration

**Stack:** Go 1.26.4, CLI binary, TOML schema
**Tests:** 353+ tests across 13 packages, all passing (`go test ./...`)
**Build:** `go build -o depengine .`
**gopls MCP:** available — load `skill://gopls-mcp` for Go workspace workflows

## Architecture

### Entry point
`main.go` — CLI with `install`, `check`, `list`, `status`, `remove`, `validate`,
`forget`, `completion` subcommands. Loads schema, gathers facts, resolves clan,
delegates to executor.

### Package layout
```
depengine/
├── main.go                    # CLI entry point
├── schema.toml                # Reference template
├── .omp/
│   └── skills/
│       └── gopls-mcp/SKILL.md # Go MCP workspace intelligence
├── scripts/
│   └── detect_os.sh           # Distro detection (POSIX sh)
├── pkg/
│   ├── engine/                # Facts, GatherFacts, ResolveFamily, MatchesDistroFamily
│   ├── native/                # Native manager registry (15 distros)
│   ├── schema/                # TOML parser, placeholder expansion, Condition
│   ├── exec/                  # Executor, Adapter interface, report, registry, sync
│   ├── exectest/              # Mock adapter for tests
│   ├── ghrelease/             # GitHub {latest} release resolver
│   ├── git/                   # Git clone+build adapter
│   ├── httpdownload/          # HTTP download adapter + checksum + {latest} resolver
│   ├── graph/                 # Topological sort + cycle detection
│   ├── lang/                  # Language adapters (cargo, go, pip, pipx, uv, aur, npm, gem, yarn, etc.)
│   ├── log/                   # Structured logging with trace IDs (default: INFO)
│   ├── run/                   # Runner interface (OS exec, fake for tests)
│   ├── state/                 # Persistent state + file locking (LOCK_SH/LOCK_EX) + DefinitionHash
│   └── validate/              # Schema structural validation
└── tests/
    └── integration/           # Integration tests (Docker-based, TODO)
```

### Key contracts
- **`pkg/exec.Adapter`**: `Kind()`, `Available()`, `Check()`, `Install()` — every install method implements this.
- **`pkg/run.Runner`**: Interface abstracting subprocess execution; `OSExecRunner` for prod, `FakeRunner` for tests.
- **`pkg/schema.Schema`**: Normalized form of the TOML; `Tool` + `MethodCandidate` + `Condition`.
- **`pkg/engine.Facts`**: 1:1 mirror of `detect_os.sh --json` output.
- **`pkg/exec.ExecReport`**: Structured result of a full install run (ToolResult, MethodAttempt).
- **`pkg/state.LockedState`**: Exclusive (LOCK_EX) or shared (LOCK_SH) file lock + state JSON; `LoadLocked()`, `LoadShared()`, `Save()`.
- **`pkg/state.DefinitionHash`**: SHA256 hash covering tool name, requires, postinstall, and every method's kind + config + when condition.
- **`pkg/graph.Sort`**: Topological sort with cycle detection; respects `Requires` edges.

### gopls MCP integration
The gopls MCP server is available for Go workspace intelligence. Before editing any
`.go` file, agents SHOULD load `skill://gopls-mcp` for the canonical read/edit
workflow using `go_workspace`, `go_diagnostics`, `go_symbol_references`,
`go_search`, `go_file_context`, `go_package_api`, and `go_vulncheck`.

### Agent conventions
- **Project-level agents**: placed in `.omp/agents/` at project root.
- **Project-level skills**: placed in `.omp/skills/<name>/SKILL.md`; loaded via `skill://<name>`.
- **Prefer built-in framework agents** (`explore`, `plan`, `reviewer`, `test-engineer`, `task`, `sonic`) over custom ones when they fit.
- **Skills** already loaded by the system cover Go/TypeScript general patterns; add project-specific skills only when a built-in or global agent doesn't cover a recurrent pain point.
- **Model assignment**: use `smol` for mechanical/exploration tasks (glob searches, grep, simple edits). Use default/strong models for deep reasoning (architecture, design, complex refactors).

### Built-in agents available (framework)
| Agent | Purpose |
|-------|---------|
| `explore` | Read-only codebase scout |
| `plan` | Architecture decisions, multi-file design |
| `designer` | UI/UX (not applicable to CLI) |
| `reviewer` | Code review, quality, security |
| `test-engineer` | Test strategy, writing, coverage |
| `librarian` | External library/API research |
| `task` | General-purpose worker |
| `sonic` | Mechanical updates, bulk data collection |
| `oracle` | Deep reasoning agent |

### Project skills
| Skill | When to load |
|-------|--------------|
| `gopls-mcp` | Before editing or investigating any `.go` file; provides Go workspace MCP workflows |

### User agents available (`~/.omp/agent/agents/`)
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
| `security-auditor` | OWASP/dependency/configuration security |
| `web-performance-auditor` | Core Web Vitals & loading optimization |
| `bash-pro`, `posix-shell-pro` | Shell scripting specialists |
| `smart-test-selector`, `test-scenario-designer` | Test analysis & generation |
| `functional-reviewer`, `release-analyzer` | Feature & release QA |
| `browser-validator`, `environment-manager` | Live browser & env setup |
| `agent-orchestration-context-manager` | Multi-agent context orchestration |
| `application-performance-*` (3 agents) | Frontend, observability, perf engineering |
| `codebase-cleanup-*` (2 agents) | Test automation, code review |
| `code-refactoring-*` (2 agents) | Code review, legacy modernization |
| `comprehensive-review-*` (3 agents) | Architecture, code review, security |
| `debugging-toolkit-*` (2 agents) | Debugger, DX optimization |
| `distributed-debugging-*` (2 agents) | Error detective, devops troubleshooter |
| `error-debugging-*` (2 agents) | Debugger, error detective |
| `git-pr-workflows-code-reviewer` | PR-focused code review |
| `monorepo-architect` | Multi-project build systems |
| `orchestrator`, `manual-validator`, `bug-reporter`, `automation-writer` | QA pipeline |

### Current gaps
- **No adapter-specific test agent**: Tests for individual adapters (lang, git, http) require deep domain knowledge. Built-in `test-engineer` works generically but doesn't know adapter patterns.
- **No integration test runner**: Docker-based integration tests (TODO) need orchestration. `task`/`explore` suffice but no dedicated agent exists.
- **No schema validation agent**: TOML schema parsing is complex (3 declaration forms, conditions, placeholders). Could benefit from a specialist, but built-in `reviewer` + `test-engineer` + `explore` cover this.

### Rules
1. Never commit generated files unless explicitly asked.
2. Run `go test ./...` before marking any task complete.
3. Use `go vet ./...` for static analysis.
4. Load `skill://gopls-mcp` at the start of any Go editing session.
5. Integration tests need Docker; confirm Docker is running before attempting.
6. All agent definitions in `.omp/agents/` use YAML frontmatter + Markdown body.
7. All skill definitions in `.omp/skills/` use YAML frontmatter + Markdown body.
